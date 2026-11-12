package alarm

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/tickets"
	"aiops-platform/backend/internal/topo"
)

// AlarmEvent 告警实体（store kind = "alarm_event"，key = fingerprint）。
type AlarmEvent struct {
	Fingerprint   string            `json:"fingerprint"`
	Source        string            `json:"source,omitempty"` // prometheus | degradation（空值按 prometheus 兼容）
	AlertName     string            `json:"alertName"`
	Instance      string            `json:"instance"`
	Host          string            `json:"host"`
	Severity      string            `json:"severity"`
	Labels        map[string]string `json:"labels"`
	Summary       string            `json:"summary"`
	State         string            `json:"state"` // firing | resolved
	FirstSeenAt   time.Time         `json:"firstSeenAt"`
	LastSeenAt    time.Time         `json:"lastSeenAt"`
	FireCount     int               `json:"fireCount"` // 累计次数（resolved→firing 时 +1）
	MissCount     int               `json:"missCount"` // 采集窗口未出现次数（恢复防抖）
	LastDiagAt    *time.Time        `json:"lastDiagAt,omitempty"`
	DiagSessionID string            `json:"diagSessionId,omitempty"`
	TicketID      string            `json:"ticketId,omitempty"`
	ResolvedAt    *time.Time        `json:"resolvedAt,omitempty"`
	SilencedUntil *time.Time        `json:"silencedUntil,omitempty"`
	SilenceReason string            `json:"silenceReason,omitempty"`
	// SuppressedBy 上游 Alertmanager 抑制原因：silenced | inhibited（空表示未抑制）。
	// 与手动静默（SilencedUntil）相互独立，二者任一存在都不自动开排障。
	SuppressedBy string `json:"suppressedBy,omitempty"`
}

// IsSuppressed 是否被上游 Alertmanager 静默 / 抑制。
func (e AlarmEvent) IsSuppressed() bool { return e.SuppressedBy != "" }

// IsSilenced 判断给定时刻是否处于手动静默期。
func (e AlarmEvent) IsSilenced(now time.Time) bool {
	return e.SilencedUntil != nil && now.Before(*e.SilencedUntil)
}

// Engine 告警中心引擎：采集 + 去重 + 自动排障 + 恢复闭环。
type Engine struct {
	cfg     config.Config
	gw      *mcp.Gateway
	st      store.Store
	topo    topo.Provider
	diag    *diagnose.Engine
	tickets *tickets.Engine
	reg     *identity.Registry

	mu sync.Mutex
}

func New(cfg config.Config, gw *mcp.Gateway, st store.Store, tp topo.Provider, diag *diagnose.Engine, tk *tickets.Engine, reg *identity.Registry) *Engine {
	return &Engine{cfg: cfg, gw: gw, st: st, topo: tp, diag: diag, tickets: tk, reg: reg}
}

// Kind store kind 常量，集中一处防拼写漂移。
const Kind = "alarm_event"

// Start 启动后台采集循环。阻塞直到 ctx cancel。
func (e *Engine) Start(ctx context.Context) {
	if !e.cfg.AlarmEnabled {
		return
	}
	ticker := time.NewTicker(e.cfg.AlarmPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = e.PollOnce(ctx)
		}
	}
}

// PollOnce 手动触发一轮采集 / 幂等入库 / 恢复检测 / 自动排障准入。
// 返回本轮 upsert 的事件数 + 自动触发的诊断数。
func (e *Engine) PollOnce(ctx context.Context) (upserted int, autoDiag int) {
	mcpEnabled := e.gw != nil && e.gw.Enabled()
	if !mcpEnabled && !e.cfg.AlertmanagerEnabled {
		return 0, 0
	}

	// ① 双通道采集：list_alerts（Prometheus）+ /api/v2/alerts（Alertmanager）
	// 同一 fingerprint 视为同一条告警，Alertmanager 仅补全抑制态与分组。
	var alerts []alertView
	loaded := false
	if mcpEnabled {
		if raw, err := e.gw.CallSilent(ctx, "prometheus", "list_alerts", map[string]any{}); err == nil {
			if parsed, err := parseAlerts(raw); err == nil {
				alerts = parsed
				loaded = true
			}
		}
	}
	if e.cfg.AlertmanagerEnabled {
		if am, err := e.fetchAlertmanager(ctx); err == nil {
			alerts = mergeAlerts(alerts, am)
			loaded = true
		}
	}
	if !loaded {
		return 0, 0
	}

	// ② 幂等入库；被上游静默 / 抑制的告警仍入库（保证恢复检测不误判），但不进自动排障。
	seen := make(map[string]bool, len(alerts))
	diagnosable := make([]alertView, 0, len(alerts))
	fresh := make([]AlarmEvent, 0, 4)
	for _, a := range alerts {
		ev, isNew := e.upsertAlert(a)
		seen[ev.Fingerprint] = true
		upserted++
		if isNew && a.Suppressed == "" {
			fresh = append(fresh, ev)
		}
		if a.Suppressed == "" {
			diagnosable = append(diagnosable, a)
		}
	}

	// ③ 恢复检测（在 seen 之外）；degradation / inspection 由各自扫描器维护生命周期
	for _, ev := range e.List("") {
		if ev.State != "firing" {
			continue
		}
		if ev.Source == "degradation" || ev.Source == "inspection" {
			continue
		}
		if seen[ev.Fingerprint] {
			continue
		}
		ev.MissCount++
		if ev.MissCount >= e.cfg.AlarmMissTolerance {
			ev.State = "resolved"
			now := time.Now()
			ev.ResolvedAt = &now
			// 自动追加工单备注（不改 tickets.Engine 状态机）
			if tk, ok := e.tickets.Get(ev.TicketID); ok && tk != nil {
				tk.Findings = append(tk.Findings, fmt.Sprintf("告警已恢复（%s），建议人工确认处置", now.Format("2006-01-02 15:04:05")))
				_ = e.st.Put("ticket", tk.ID, tk)
			}
		}
		_ = e.st.Put(Kind, ev.Fingerprint, ev)
	}

	// ④ 自动排障准入（P1：只处理 firing、未被抑制且尚未关联 diagSession 的告警）
	if e.cfg.AlarmAutoDiag {
		autoDiag = e.runAutoDiagnose(ctx, diagnosable)
	}
	// ⑤ 通知出站（T11）：本轮新出现的未抑制告警，推送一次
	e.NotifyNew(ctx, fresh)
	return upserted, autoDiag
}

// upsertAlert 幂等写入单条告警，返回入库后的事件与「是否新一轮触发」。
// 「新一轮触发」= 首次出现或 resolved → firing（死灰复燃），用于通知去重。
func (e *Engine) upsertAlert(a alertView) (AlarmEvent, bool) {
	return e.upsertAlertWithFP(fingerprint(a), a)
}

// upsertAlertWithFP 使用调用方指定的幂等键写入告警。
// 巡检等批量来源需要把任务维度纳入指纹，避免不同任务互相覆盖。
func (e *Engine) upsertAlertWithFP(fp string, a alertView) (AlarmEvent, bool) {
	var ev AlarmEvent
	if b, ok := e.st.Get(Kind, fp); ok {
		_ = json.Unmarshal(b, &ev)
	}
	isNew := false

	labels := a.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	if a.Group != "" {
		labels["group"] = a.Group
	}
	source := a.Source
	if source == "" {
		source = "prometheus"
	}
	now := time.Now()

	switch {
	case ev.Fingerprint == "":
		ev = AlarmEvent{
			Fingerprint:  fp,
			Source:       source,
			AlertName:    a.AlertName,
			Instance:     a.Instance,
			Host:         a.Host,
			Severity:     a.Severity,
			Labels:       labels,
			Summary:      a.Summary,
			State:        "firing",
			FirstSeenAt:  now,
			LastSeenAt:   now,
			FireCount:    1,
			SuppressedBy: a.Suppressed,
		}
		isNew = true
	case ev.State == "resolved":
		// 死灰复燃
		ev.State = "firing"
		ev.FireCount++
		ev.FirstSeenAt = now
		ev.LastSeenAt = now
		ev.MissCount = 0
		ev.ResolvedAt = nil
		ev.AlertName = a.AlertName
		ev.Instance = a.Instance
		ev.Host = a.Host
		ev.Severity = a.Severity
		ev.Labels = labels
		ev.Summary = a.Summary
		ev.Source = source
		ev.SuppressedBy = a.Suppressed
		isNew = true
	default:
		// 持续 firing：只刷 lastSeenAt，FireCount 不累加
		ev.LastSeenAt = now
		ev.MissCount = 0
		ev.Severity = a.Severity
		ev.Labels = labels
		if ev.Source != "degradation" && ev.Source != "inspection" {
			ev.Source = source
		}
		// 上游解除静默 → 视为恢复可处置，补发一次通知
		if ev.SuppressedBy != "" && a.Suppressed == "" {
			isNew = true
		}
		ev.SuppressedBy = a.Suppressed
	}
	_ = e.st.Put(Kind, fp, ev)
	return ev, isNew
}

// runAutoDiagnose 对本轮 firing 的告警做准入检查，自动开排障。
// 返回实际新建的诊断会话数。
func (e *Engine) runAutoDiagnose(ctx context.Context, alerts []alertView) int {
	events := make([]AlarmEvent, 0, len(alerts))
	for _, a := range alerts {
		if b, ok := e.st.Get(Kind, fingerprint(a)); ok {
			var ev AlarmEvent
			if json.Unmarshal(b, &ev) == nil {
				events = append(events, ev)
			}
		}
	}
	return e.autoDiagnoseEvents(ctx, events)
}

// autoDiagnoseEvents 对已入库的告警事件做四道准入检查并自动开排障。
// 返回实际新建的诊断会话数。
func (e *Engine) autoDiagnoseEvents(ctx context.Context, events []AlarmEvent) int {
	started := 0
	cooldown := e.cfg.AlarmCooldown
	maxAuto := e.cfg.AlarmMaxAutoDiag
	now := time.Now()

	for _, ev := range events {
		if started >= maxAuto {
			break
		}

		// 准入 1：severity 必须 critical / warning
		if ev.Severity != "critical" && ev.Severity != "warning" {
			continue
		}
		if ev.State != "firing" {
			continue
		}

		// 准入 1.5：手动静默期内不自动开排障（人工强制仍可用）
		if ev.IsSilenced(now) {
			continue
		}
		// 准入 1.6：上游 Alertmanager 静默 / 抑制同样不开排障
		if ev.IsSuppressed() {
			continue
		}

		// 准入 2：冷却
		if ev.LastDiagAt != nil && now.Sub(*ev.LastDiagAt) < cooldown {
			continue
		}

		// 准入 3：同 host 已有活跃会话
		if e.hasActiveSessionForHost(ev.Host) {
			continue
		}

		// 准入 4：已有关联 diagSession（死灰复燃才进得到这里，正常新告警 diagSession 为空）
		if ev.DiagSessionID != "" {
			continue
		}

		req := e.buildDiagnoseRequest(ev)
		sess, err := e.diag.Start(req)
		if err != nil {
			continue
		}
		ev.DiagSessionID = sess.ID
		t := now
		ev.LastDiagAt = &t
		_ = e.st.Put(Kind, ev.Fingerprint, ev)
		started++
	}
	return started
}

// buildDiagnoseRequest 把 alarm_event 映射成排障 Request。
// 三层策略：labels.appId → labels.type → topo 匹配到的类型。
func (e *Engine) buildDiagnoseRequest(ev AlarmEvent) diagnose.Request {
	req := diagnose.Request{
		Symptom: fmt.Sprintf("%s：%s", ev.AlertName, ev.Summary),
		Env:     e.cfg.DefaultEnv,
	}

	// 策略 1：labels.appId 存在 → 应用视角（经身份映射归一成规范 Apollo AppID）
	if appId := ev.Labels["appId"]; appId != "" {
		if e.reg != nil {
			if a, ok := e.reg.ResolveApp(appId); ok && a.ApolloAppID != "" {
				appId = a.ApolloAppID
			}
		}
		req.AppID = appId
		req.Addr = ev.Instance
		return req
	}

	// 策略 2：host 命中身份映射（跨 MCP 归一的中间件 / 应用归属）
	if e.reg != nil {
		if h, ok := e.reg.ResolveHost(ev.Host); ok {
			if len(h.MiddlewareTypes) > 0 {
				req.MiddlewareType = h.MiddlewareTypes[0]
				req.Addr = ev.Instance
				return req
			}
			if len(h.Apps) > 0 {
				req.AppID = h.Apps[0]
				req.Addr = ev.Instance
				return req
			}
		}
	}

	// 策略 3：host 在 topo 中间件列表中 → topo 匹配到的类型
	if mwType := e.resolveMwTypeFromTopo(ev.Host); mwType != "" {
		req.MiddlewareType = mwType
		req.Addr = ev.Instance
		return req
	}

	// 策略 4：labels.type 兜底
	if mwType := ev.Labels["type"]; mwType != "" {
		req.MiddlewareType = mwType
		req.Addr = ev.Instance
		return req
	}

	// 策略 5：退化为 host 视角（MiddlewareType 空 → diagnose 会拒绝，所以给个占位）
	req.MiddlewareType = "unknown"
	req.Addr = ev.Instance
	req.Symptom = fmt.Sprintf("%s：%s（host %s 未匹配到中间件类型）", ev.AlertName, ev.Summary, ev.Host)
	return req
}

// resolveMwTypeFromTopo 在 topo 快照里找 host 对应的 middlewareType。
func (e *Engine) resolveMwTypeFromTopo(host string) string {
	if e.topo == nil || host == "" {
		return ""
	}
	date := ""
	if ds := e.topo.Dates(); len(ds) > 0 {
		date = ds[len(ds)-1]
	}
	rows, err := e.topo.Rows(date, e.cfg.DefaultEnv)
	if err != nil {
		return ""
	}
	for _, r := range rows {
		if strings.EqualFold(hostOf(r.ConnectAddr), host) && r.MiddlewareType != "" {
			return r.MiddlewareType
		}
	}
	return ""
}

func (e *Engine) hasActiveSessionForHost(host string) bool {
	sessions := e.diag.List(200)
	for _, s := range sessions {
		if s.Status != "investigating" && s.Status != "root_caused" {
			continue
		}
		if !strings.EqualFold(hostOf(s.Addr), host) {
			continue
		}
		if s.FinishedAt.IsZero() {
			return true // 同主机仍在跑
		}
		if time.Since(s.FinishedAt) < 30*time.Minute {
			return true
		}
	}
	return false
}

// List 列出告警事件，按 lastSeenAt 倒序。state 为空则不过滤。
func (e *Engine) List(state string) []AlarmEvent {
	out := store.ListInto[AlarmEvent](e.st, Kind)
	if state != "" {
		filtered := out[:0]
		for _, v := range out {
			if v.State == state {
				filtered = append(filtered, v)
			}
		}
		out = filtered
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out
}

// ListQuery 告警列表查询条件（服务端筛选 + 分页）。
type ListQuery struct {
	State    string
	Severity string
	Q        string
	Page     int
	PageSize int
}

// Query 按条件筛选并分页，返回当前页数据与筛选后的总数。
func (e *Engine) Query(q ListQuery) ([]AlarmEvent, int) {
	kw := strings.ToLower(strings.TrimSpace(q.Q))
	filtered := make([]AlarmEvent, 0, 32)
	for _, v := range store.ListInto[AlarmEvent](e.st, Kind) {
		if q.State != "" && v.State != q.State {
			continue
		}
		if q.Severity != "" && v.Severity != q.Severity {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(v.AlertName + " " + v.Instance + " " + v.Host + " " + v.Summary)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		filtered = append(filtered, v)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].LastSeenAt.After(filtered[j].LastSeenAt) })

	total := len(filtered)
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	start := (q.Page - 1) * q.PageSize
	if start >= total {
		return []AlarmEvent{}, total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total
}

// TrendPoint 单小时趋势点。
type TrendPoint struct {
	Hour     string `json:"hour"`     // 本地时区 yyyy-mm-dd HH:00
	Created  int    `json:"created"`  // 该小时新增（首次出现）告警数
	Resolved int    `json:"resolved"` // 该小时恢复告警数
}

// Trend 按小时聚合最近 hours 小时的新增 / 恢复告警数。
func (e *Engine) Trend(hours int) []TrendPoint {
	if hours <= 0 || hours > 168 {
		hours = 24
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()).
		Add(-time.Duration(hours-1) * time.Hour)
	pts := make([]TrendPoint, hours)
	index := map[string]int{}
	for i := 0; i < hours; i++ {
		t := start.Add(time.Duration(i) * time.Hour)
		key := t.Format("2006-01-02T15")
		index[key] = i
		pts[i] = TrendPoint{Hour: key}
	}
	for _, ev := range store.ListInto[AlarmEvent](e.st, Kind) {
		if i, ok := index[ev.FirstSeenAt.Local().Format("2006-01-02T15")]; ok {
			pts[i].Created++
		}
		if ev.ResolvedAt != nil {
			if i, ok := index[ev.ResolvedAt.Local().Format("2006-01-02T15")]; ok {
				pts[i].Resolved++
			}
		}
	}
	return pts
}

// Silence 设置手动静默。until 为零值表示永久静默。
func (e *Engine) Silence(fp string, until time.Time, reason string) (*AlarmEvent, error) {
	ev, ok := e.Get(fp)
	if !ok {
		return nil, fmt.Errorf("告警 %s 不存在", fp)
	}
	if until.IsZero() {
		forever := time.Now().AddDate(100, 0, 0)
		until = forever
	}
	ev.SilencedUntil = &until
	ev.SilenceReason = reason
	if err := e.st.Put(Kind, fp, *ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// Unsilence 取消手动静默。
func (e *Engine) Unsilence(fp string) (*AlarmEvent, error) {
	ev, ok := e.Get(fp)
	if !ok {
		return nil, fmt.Errorf("告警 %s 不存在", fp)
	}
	ev.SilencedUntil = nil
	ev.SilenceReason = ""
	if err := e.st.Put(Kind, fp, *ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// AutoTickets24h 统计近 24h 由告警自动排障生成（sourceType=diagnosis）的工单数。
func (e *Engine) AutoTickets24h() int {
	since := time.Now().Add(-24 * time.Hour)
	n := 0
	for _, t := range e.tickets.List() {
		if t.SourceType == "diagnosis" && t.CreatedAt.After(since) {
			n++
		}
	}
	return n
}

// Get 按指纹取单条。
func (e *Engine) Get(fp string) (*AlarmEvent, bool) {
	var ev AlarmEvent
	if store.GetInto(e.st, Kind, fp, &ev) {
		return &ev, true
	}
	return nil, false
}

// Stats 聚合按 severity + state 的计数（供 Dashboard）。
func (e *Engine) Stats() map[string]map[string]int {
	out := map[string]map[string]int{
		"critical": {"firing": 0, "resolved": 0},
		"warning":  {"firing": 0, "resolved": 0},
		"info":     {"firing": 0, "resolved": 0},
		"other":    {"firing": 0, "resolved": 0},
	}
	for _, ev := range store.ListInto[AlarmEvent](e.st, Kind) {
		key := ev.Severity
		if _, ok := out[key]; !ok {
			key = "other"
		}
		out[key][ev.State]++
	}
	return out
}

// ForceDiagnose 人工强制开排障（忽略冷却）。返回新建的会话。
func (e *Engine) ForceDiagnose(ctx context.Context, fp string) (*domain.DiagnosisSession, error) {
	ev, ok := e.Get(fp)
	if !ok {
		return nil, fmt.Errorf("告警 %s 不存在", fp)
	}
	if ev.DiagSessionID != "" {
		// 已有会话，直接返回（幂等）
		if sess, ok := e.diag.Get(ev.DiagSessionID); ok {
			return &sess, nil
		}
	}
	req := e.buildDiagnoseRequest(*ev)
	sess, err := e.diag.Start(req)
	if err != nil {
		return nil, err
	}
	ev.DiagSessionID = sess.ID
	now := time.Now()
	ev.LastDiagAt = &now
	_ = e.st.Put(Kind, fp, ev)
	return sess, nil
}

// SyncTickets 把已有关联 diagSessionID 的告警补上 ticketID（轮询时调用）。
func (e *Engine) SyncTickets() {
	tickets := e.tickets.List()
	bySource := map[string]string{} // sourceID → ticketID
	for _, t := range tickets {
		if t.SourceType == "diagnosis" && t.SourceID != "" {
			bySource[t.SourceID] = t.ID
		}
	}
	for _, ev := range e.List("") {
		if ev.DiagSessionID == "" || ev.TicketID != "" {
			continue
		}
		if tid, ok := bySource[ev.DiagSessionID]; ok {
			ev.TicketID = tid
			_ = e.st.Put(Kind, ev.Fingerprint, ev)
		}
	}
}
