package diagnose

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/topo"
)

// Request 排障请求：应用视角（appId）或中间件视角（type + addr）。
type Request struct {
	TenantID       string `json:"tenantId"`
	AppID          string `json:"appId"`
	MiddlewareType string `json:"middlewareType"`
	Addr           string `json:"addr"`
	Symptom        string `json:"symptom"`
	Env            string `json:"env"`
	Date           string `json:"date"`
	ViewMode       string `json:"viewMode"`
	LogPath        string `json:"logPath"`   // 可选：显式指定日志文件路径，缺省按 LOG_PATH_TEMPLATE 推导
	WindowMin      int    `json:"windowMin"` // 可选：观测时间窗（分钟），缺省取 DIAG_WINDOW_MIN
}

// Engine 排障引擎：topo 影响面 → MCP 证据采集 → 根因候选排序。
// 每一步都写入 Steps（即证据链），前端通过 SSE 轮询增量下发。
type Engine struct {
	topo  topo.Provider
	store store.Store
	mcp   *mcp.Gateway
	logs  *logClient
	cfg   config.Config
	reg   *identity.Registry

	mu       sync.RWMutex
	sessions map[string]*domain.DiagnosisSession
}

func New(tp topo.Provider, st store.Store, gw *mcp.Gateway, cfg config.Config, reg *identity.Registry) *Engine {
	e := &Engine{topo: tp, store: st, mcp: gw, cfg: cfg, reg: reg, sessions: map[string]*domain.DiagnosisSession{}}
	if cfg.LogAnalyzerEnabled && cfg.LogAnalyzerURL != "" {
		e.logs = newLogClient(cfg.LogAnalyzerURL, cfg.LogAnalyzeTimeout, cfg.LogAnalyzePoll)
	}
	return e
}

// Start 创建会话并异步执行，返回可立即用于 SSE 订阅的会话。
func (e *Engine) Start(req Request) (*domain.DiagnosisSession, error) {
	req.AppID = strings.TrimSpace(req.AppID)
	req.Addr = strings.TrimSpace(req.Addr)
	if req.AppID == "" && (req.MiddlewareType == "" || req.Addr == "") {
		return nil, fmt.Errorf("必须提供 appId，或同时提供 middlewareType 与 addr")
	}
	if req.Symptom == "" {
		req.Symptom = "未提供症状描述"
	}
	sess := &domain.DiagnosisSession{
		ID:             uuid.NewString(),
		TenantID:       req.TenantID,
		AppID:          req.AppID,
		MiddlewareType: req.MiddlewareType,
		Addr:           req.Addr,
		Symptom:        req.Symptom,
		Status:         "investigating",
		Steps:          []domain.Step{},
		CreatedAt:      time.Now(),
	}
	e.mu.Lock()
	e.sessions[sess.ID] = sess
	e.mu.Unlock()
	e.persist(sess)

	go e.execute(sess, req)
	return e.snapshot(sess), nil
}

// Get 会话详情：内存优先，重启后回退文件/PG。
func (e *Engine) Get(id string) (domain.DiagnosisSession, bool) {
	e.mu.RLock()
	sess, ok := e.sessions[id]
	var cp domain.DiagnosisSession
	if ok {
		cp = copySession(sess)
	}
	e.mu.RUnlock()
	if ok {
		return cp, true
	}
	var loaded domain.DiagnosisSession
	if store.GetInto(e.store, "diagnosis_session", id, &loaded) {
		return loaded, true
	}
	return domain.DiagnosisSession{}, false
}

// SubmitFeedback 人工回填根因准确率（T10）。重复标注以最后一次为准。
func (e *Engine) SubmitFeedback(id string, fb domain.DiagnosisFeedback) (domain.DiagnosisSession, bool) {
	fb.At = time.Now()
	e.mu.Lock()
	sess, ok := e.sessions[id]
	if !ok {
		e.mu.Unlock()
		var loaded domain.DiagnosisSession
		if !store.GetInto(e.store, "diagnosis_session", id, &loaded) {
			return domain.DiagnosisSession{}, false
		}
		loaded.Feedback = &fb
		_ = e.store.Put("diagnosis_session", loaded.ID, loaded)
		return loaded, true
	}
	sess.Feedback = &fb
	cp := copySession(sess)
	e.mu.Unlock()
	_ = e.store.Put("diagnosis_session", cp.ID, cp)
	return cp, true
}

// List 最近的会话（按创建时间倒序）。
func (e *Engine) List(limit int) []domain.DiagnosisSession {
	e.mu.RLock()
	out := make([]domain.DiagnosisSession, 0, len(e.sessions))
	for _, s := range e.sessions {
		out = append(out, copySession(s))
	}
	e.mu.RUnlock()
	if len(out) == 0 {
		out = store.ListInto[domain.DiagnosisSession](e.store, "diagnosis_session")
	}
	sortByCreatedDesc(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (e *Engine) execute(sess *domain.DiagnosisSession, req Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	env := firstNonEmpty(req.Env, e.cfg.DefaultEnv)
	date := req.Date
	if date == "" {
		if dates := e.topo.Dates(); len(dates) > 0 {
			date = dates[len(dates)-1]
		}
	}
	viewMode := req.ViewMode
	if viewMode == "" {
		viewMode = e.cfg.DefaultViewMode
	}

	// 身份映射：把 appId / 应用名 / Pinpoint 名统一到同一应用身份
	appID, appName, pinpointID, pinpointName := e.resolveAppIdentity(req)

	// 0) 拓扑影响面：先算出候选主机与被影响应用，供第 1 步发现与第 2 步定界复用
	impact, topoErr := e.collectImpact(req, appID, date, env, viewMode)
	step := domain.Step{
		Source: "topo",
		Tool:   "impact",
		Title:  "拓扑影响面",
		Status: "ok",
		Args:   map[string]any{"appId": req.AppID, "resolvedAppId": appID, "middlewareType": req.MiddlewareType, "addr": req.Addr, "env": env, "date": date, "viewMode": viewMode},
	}
	start := time.Now()
	if topoErr != nil {
		step.Status = "error"
		step.Error = topoErr.Error()
	} else {
		e.mu.Lock()
		sess.Impact = impact
		e.mu.Unlock()
		step.Result = map[string]any{
			"syncDate":    impact.SyncDate,
			"level":       impact.Level,
			"target":      impact.Target,
			"totalApps":   impact.TotalApps,
			"apps":        headApps(impact.Apps, 50),
			"middlewares": impact.Middlewares,
			"note":        impact.Note,
		}
	}
	step.LatencyMs = time.Since(start).Milliseconds()
	e.appendStep(sess, step)

	// 用影响面回填更准确的应用标识
	if impact != nil && impact.Level == "app" && len(impact.Apps) > 0 {
		for _, a := range impact.Apps {
			if !a.IsTarget {
				continue
			}
			if a.AppID != "" {
				appID = a.AppID
			}
			if a.AppName != "" {
				appName = a.AppName
			}
		}
	}
	if appName == "" {
		appName = appID
	}

	// 证据集：第 4 步取证产出，供第 5 步定因按「来源数 + 条数」计算置信度。
	ev := newEvidence()
	if impact != nil {
		ev.topoApps = impact.TotalApps
		if impact.TotalApps > 0 {
			ev.add("topo", 1)
		}
	}

	// 1) 发现的前置：数据源验真。先确认 Prometheus 自身可用、候选主机确实被采集，
	//    再解释信号，避免把「未纳管 / 采集异常」误判成「主机宕机」。
	hosts := e.candidateHosts(req, impact)
	registered := e.collectVerify(ctx, sess, hosts)

	// 2) 发现：Prometheus 活跃告警 + 目标主机 up（同时产出 host → up 的取值）
	upByHost, alertHits, alertOK := e.collectPrometheus(ctx, sess, req, hosts, registered)
	ev.alertOK, ev.alertHits = alertOK, alertHits
	ev.upKnown = len(upByHost)
	for host, v := range upByHost {
		if v <= 0 {
			ev.upDown = append(ev.upDown, host)
		}
	}
	sort.Strings(ev.upDown)
	if len(registered) > 0 {
		for _, host := range hosts {
			if !registered[host] {
				ev.unmanaged = append(ev.unmanaged, host)
			}
		}
		sort.Strings(ev.unmanaged)
	}
	if alertHits > 0 {
		ev.add("promthus", alertHits)
	}
	if len(ev.upDown) > 0 {
		ev.add("promthus", len(ev.upDown))
	}

	// 3) 确认与定界：把 Prometheus / 拓扑拿到的 host 传导成资产（机房 / 在线状态 / 负责人）
	assets := e.collectCMDB(ctx, sess, hosts, upByHost)
	if assets != nil {
		ev.cmdbMatched, ev.cmdbUnmatched = assets.Matched, len(assets.Unmatched)
		if assets.Matched > 0 {
			ev.add("cmdb", assets.Matched)
		}
	}

	// 4) 定位：应用清单是否收录 + 上下游服务地图（决定调用链证据是否可得）
	pinpointOK := false
	if req.AppID != "" || appID != "" {
		pinpointOK = e.collectPinpoint(ctx, sess, req, appID, appName, pinpointID, pinpointName)
	} else {
		e.appendStep(sess, domain.Step{
			Source: "pinpoint", Tool: "list_applications_tool", Title: "Pinpoint 证据采集",
			Status: "skipped", Error: "本次为中间件视角排障，未指定应用，跳过调用链证据",
		})
	}
	ev.pinpointOK = pinpointOK
	if pinpointOK {
		ev.add("pinpoint", 1)
	}

	// 5) 取证：指标基线（range_query）、调用链（drag_heatmap → get_transaction_trace_tool）、
	//    日志（优先 ES search，失败回退本地 log-analyzer）。任一步缺失都不阻断定因。
	window := req.WindowMin
	if window <= 0 {
		window = e.cfg.DiagWindowMin
	}
	if window <= 0 {
		window = 5
	}
	ev.rangePoints, ev.rangeRising = e.collectRangeBaseline(ctx, sess, hosts)
	if ev.rangePoints > 0 {
		ev.add("promthus", 1)
	}
	ev.traceID, ev.traceSpans, ev.traceHot, ev.traceShare = e.collectTraceEvidence(ctx, sess, appName, pinpointOK, window)
	if ev.traceSpans > 0 {
		ev.add("pinpoint", 1)
	}
	ev.logErrors, ev.logOK = e.collectLogEvidence(ctx, sess, req, hosts)
	if ev.logOK {
		ev.add("log", 1)
	}

	// 6) 问题单：把第 2 步的确认结果固化，作为第 3~5 步的准入依据。
	ticket := e.buildTicket(req, impact, ev, assets, sess.SourceHealth, appID, appName, hosts, window)
	e.mu.Lock()
	sess.Ticket = ticket
	e.mu.Unlock()

	// 7) 定因：根因候选排序（拓扑 + 调用链 + 告警 + 资产 + 数据源健康度），
	//    置信度由证据集推导，无有效证据时强制降级为「疑似」。
	e.rank(sess, req, impact, ev, assets)

	e.mu.Lock()
	sess.FinishedAt = time.Now()
	switch {
	case ev.sourceCount() == 0:
		sess.Status = "no_data"
	case sess.Confidence >= 0.6:
		sess.Status = "root_caused"
	default:
		sess.Status = "investigating"
	}
	e.mu.Unlock()
	e.persist(sess)
}

func (e *Engine) collectImpact(req Request, appID, date, env, viewMode string) (*domain.Impact, error) {
	if req.AppID != "" || appID != "" {
		if appID == "" {
			appID = req.AppID
		}
		return e.topo.ImpactByApp(appID, appID, date, env, viewMode)
	}
	return e.topo.ImpactByMiddleware(req.MiddlewareType, req.Addr, date, env)
}

// resolveAppIdentity 把请求里的 appId 统一成应用身份：返回规范 appId、应用名、Pinpoint ID / 名称。
func (e *Engine) resolveAppIdentity(req Request) (appID, appName, pinpointID, pinpointName string) {
	appID = strings.TrimSpace(req.AppID)
	if appID == "" || e.reg == nil {
		return appID, appID, "", ""
	}
	a, ok := e.reg.ResolveApp(appID)
	if !ok {
		return appID, appID, "", ""
	}
	if a.ApolloAppID != "" {
		appID = a.ApolloAppID
	}
	appName = firstNonEmpty(a.ApolloAppName, a.PPApplication, a.ApolloAppID, appID)
	pinpointID = a.PinpointAppID
	pinpointName = a.PinpointAppName
	return appID, appName, pinpointID, pinpointName
}

func (e *Engine) collectPinpoint(ctx context.Context, sess *domain.DiagnosisSession, req Request, appID, appName, pinpointID, pinpointName string) bool {
	if !e.mcp.Enabled() {
		e.appendStep(sess, domain.Step{
			Source: "pinpoint", Tool: "list_applications_tool", Title: "Pinpoint 应用清单",
			Status: "skipped", Error: "上游服务未启用（MCP_ENABLED=false），未接入 Pinpoint",
		})
		return false
	}

	start := time.Now()
	raw, err := e.mcp.Call(ctx, "pinpoint", "list_applications_tool", map[string]any{"service_name": "DEFAULT"})
	step := domain.Step{
		Source: "pinpoint", Tool: "list_applications_tool", Title: "Pinpoint 应用清单",
		Args: map[string]any{"service_name": "DEFAULT"},
	}
	if err != nil {
		step.Status = "error"
		step.Error = err.Error()
		step.LatencyMs = time.Since(start).Milliseconds()
		e.appendStep(sess, step)
		return false
	}

	// 用统一解析层匹配应用，不再对整段 JSON 做 strings.Contains。
	matchedApp := mcp.PinpointApplication{}
	matched := false
	inventory := 0
	apps, parseErr := mcp.PinpointApplications(raw)
	if parseErr == nil {
		inventory = len(apps)
		matchedApp, matched = mcp.FindPinpointApplication(apps, []string{pinpointID, appID}, []string{pinpointName, appName, appID})
	}

	step.Status = "ok"
	step.LatencyMs = time.Since(start).Milliseconds()
	step.Result = mcp.ResultOf(raw).Preview(3000)
	step.Result["matched"] = matched
	step.Result["inventory"] = inventory
	if parseErr != nil {
		step.Result["parseError"] = parseErr.Error()
	}
	if matched {
		step.Result["matchedId"] = matchedApp.ID
		step.Result["matchedName"] = firstNonEmpty(matchedApp.Name, appName)
	} else {
		step.Result["matchedName"] = appName
	}
	e.appendStep(sess, step)

	if !matched {
		e.appendStep(sess, domain.Step{
			Source: "pinpoint", Tool: "get_server_map_tool", Title: "Pinpoint 服务地图",
			Status: "skipped",
			Error:  "Pinpoint 应用清单中未匹配到该应用，调用链证据缺失（按 NO_DATA 口径处理，不等于无异常）",
		})
		return false
	}

	// 时间窗透传给 Pinpoint：与告警 / 排障窗口对齐。
	window := req.WindowMin
	if window <= 0 {
		window = e.cfg.DiagWindowMin
	}
	if window <= 0 {
		window = 5
	}
	to := time.Now()
	from := to.Add(-time.Duration(window) * time.Minute)
	args := map[string]any{
		"application_name": firstNonEmpty(matchedApp.Name, appName, appID),
		"from_ts":          from.UnixMilli(),
		"to_ts":            to.UnixMilli(),
		"caller_range":     1,
		"callee_range":     1,
		"bidirectional":    true,
	}

	start = time.Now()
	raw, err = e.mcp.Call(ctx, "pinpoint", "get_server_map_tool", args)
	step2 := domain.Step{
		Source: "pinpoint", Tool: "get_server_map_tool", Title: "Pinpoint 服务地图（上下游）",
		Args: args,
	}
	step2.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		step2.Status = "error"
		step2.Error = err.Error()
		e.appendStep(sess, step2)
		return false
	}
	step2.Status = "ok"
	step2.Result = mcp.ResultOf(raw).Preview(4000)
	step2.Result["windowMin"] = window
	if sm, perr := mcp.PinpointServerMapOf(raw); perr == nil {
		step2.Result["nodes"] = len(sm.Nodes)
		step2.Result["middlewares"] = len(sm.Middlewares)
	}
	e.appendStep(sess, step2)
	return true
}

// verifyTimeout 单次验真调用的超时预算。
func (e *Engine) verifyTimeout() time.Duration {
	if e.cfg.DiagVerifyTimeout > 0 {
		return e.cfg.DiagVerifyTimeout
	}
	return 2 * time.Second
}

// setSourceHealth 写入数据源验真结果，供前端展示与第 6 步定因使用。
func (e *Engine) setSourceHealth(sess *domain.DiagnosisSession, health []domain.SourceHealth) {
	e.mu.Lock()
	sess.SourceHealth = health
	e.mu.Unlock()
}

// collectVerify 第 2 步「确认与定界」的前置校验：先确认数据源本身可用、
// 候选主机是否真的被采集，再解释信号，避免把「未纳管 / 采集异常」误判成「主机宕机」。
// 结果写入 sess.SourceHealth；返回 host → 是否可查到序列（nil 表示验真不可用）。
func (e *Engine) collectVerify(ctx context.Context, sess *domain.DiagnosisSession, hosts []string) map[string]bool {
	now := time.Now()
	if !e.cfg.DiagVerifyEnabled {
		e.setSourceHealth(sess, []domain.SourceHealth{{
			Source: "promthus", Status: "skipped", At: now,
			Detail: "DIAG_VERIFY_ENABLED=false，跳过数据源验真",
		}})
		return nil
	}
	if !e.mcp.Enabled() {
		e.setSourceHealth(sess, []domain.SourceHealth{{
			Source: "promthus", Status: "skipped", At: now,
			Detail: "上游服务未启用（MCP_ENABLED=false），未接入 Prometheus",
		}})
		return nil
	}

	vctx, cancel := context.WithTimeout(ctx, e.verifyTimeout())
	defer cancel()
	health := []domain.SourceHealth{}

	// 1) Prometheus 自证健康：healthy 失败时后续信号一律不可信。
	ph := domain.SourceHealth{Source: "promthus", At: now, Checks: []string{"healthy"}}
	if raw, err := e.mcp.Call(vctx, "prometheus", "healthy", map[string]any{}); err != nil {
		ph.Status, ph.Detail = "unavailable", "healthy 调用失败: "+err.Error()
	} else {
		var body struct {
			Data struct {
				Health string  `json:"health"`
				Uptime float64 `json:"uptime"`
			} `json:"data"`
		}
		if derr := mcp.ResultOf(raw).Decode(&body); derr != nil {
			ph.Status, ph.Detail = "degraded", "healthy 返回无法解析: "+derr.Error()
		} else {
			switch strings.ToLower(strings.TrimSpace(body.Data.Health)) {
			case "", "ok", "up", "healthy", "success":
				ph.Status = "ok"
			default:
				ph.Status = "degraded"
			}
			ph.Detail = fmt.Sprintf("health=%s uptime=%.0fs", firstNonEmpty(body.Data.Health, "unknown"), body.Data.Uptime)
		}
	}
	health = append(health, ph)
	if ph.Status == "unavailable" {
		e.setSourceHealth(sess, health)
		return nil
	}

	// 2) 指标元数据：确认 up 指标已被纳管（否则 up=0 无法解释为主机宕机）。
	mh := domain.SourceHealth{Source: "promthus", At: now, Checks: []string{"targets_metadata"}}
	if raw, err := e.mcp.Call(vctx, "prometheus", "targets_metadata", map[string]any{}); err != nil {
		mh.Status, mh.Detail = "degraded", "targets_metadata 调用失败: "+err.Error()
	} else if names := metricNamesOf(raw); len(names) == 0 {
		mh.Status, mh.Detail = "degraded", "targets_metadata 未返回任何指标元数据"
	} else if containsString(names, "up") {
		mh.Status, mh.Detail = "ok", fmt.Sprintf("已纳管 up 指标（元数据 %d 项）", len(names))
	} else {
		mh.Status, mh.Detail = "degraded", fmt.Sprintf("元数据中缺少 up 指标（共 %d 项）", len(names))
	}
	health = append(health, mh)

	// 3) 序列存在性：区分「未纳管」与「采集异常 / 宕机」。
	seriesOK := false
	registered := map[string]bool{}
	sh := domain.SourceHealth{Source: "promthus", At: now, Checks: []string{"series"}}
	if raw, err := e.mcp.Call(vctx, "prometheus", "series", map[string]any{"match": []string{"up"}}); err != nil {
		sh.Status, sh.Detail = "degraded", "series 调用失败: "+err.Error()
	} else if got := hostSetOf(raw); len(got) > 0 {
		for _, h := range hosts {
			if got[h] {
				registered[h] = true
			}
		}
		seriesOK = true
		sh.Status = "ok"
		sh.Detail = fmt.Sprintf("up 序列实例 %d 个，候选主机命中 %d/%d", len(got), len(registered), len(hosts))
	} else {
		sh.Status, sh.Detail = "degraded", "series 未返回任何 up 序列"
	}
	health = append(health, sh)

	// 4) ES 只服务第 5 步取证，启用时顺带确认分片可用。
	if e.cfg.ESMCPURL != "" {
		eh := domain.SourceHealth{Source: "es", At: now, Checks: []string{"get_shards"}}
		if raw, err := e.mcp.Call(vctx, "es", "get_shards", map[string]any{"index": firstNonEmpty(e.cfg.ESLogIndex, "*")}); err != nil {
			eh.Status, eh.Detail = "unavailable", "get_shards 调用失败: "+err.Error()
		} else if n := countArrayOf(raw); n > 0 {
			eh.Status, eh.Detail = "ok", fmt.Sprintf("分片信息 %d 条", n)
		} else {
			eh.Status, eh.Detail = "degraded", "get_shards 未返回分片信息"
		}
		health = append(health, eh)
	}

	e.setSourceHealth(sess, health)
	if !seriesOK {
		return nil
	}
	return registered
}

// collectPrometheus 采集活跃告警与目标主机 up，返回归一主机 → up 取值（供 CMDB / 排序复用）、
// 以及告警命中数与采集是否成功（供第 6 步定因使用，避免 rank 反查步骤列表）。
// registered 来自 collectVerify：nil 表示未做验真，非 nil 时用于区分「未纳管」与「采集异常」。
func (e *Engine) collectPrometheus(ctx context.Context, sess *domain.DiagnosisSession, req Request, hosts []string, registered map[string]bool) (up map[string]float64, alertHits int, alertOK bool) {
	up = map[string]float64{}
	if !e.mcp.Enabled() {
		e.appendStep(sess, domain.Step{
			Source: "promthus", Tool: "list_alerts", Title: "Prometheus 活跃告警",
			Status: "skipped", Error: "上游服务未启用（MCP_ENABLED=false），未接入 Prometheus",
		})
		return up, 0, false
	}

	start := time.Now()
	raw, err := e.mcp.Call(ctx, "prometheus", "list_alerts", map[string]any{})
	step := domain.Step{Source: "promthus", Tool: "list_alerts", Title: "Prometheus 活跃告警"}
	step.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		step.Status = "error"
		step.Error = err.Error()
		e.appendStep(sess, step)
		return up, 0, false
	}
	// 统一解析告警结构：按 labels / annotations 精确命中关键字，不再对整段 JSON 做包含匹配。
	alerts, parseErr := mcp.PrometheusAlerts(raw)
	hits := 0
	if parseErr == nil {
		kws := keywords(req)
		for _, al := range alerts {
			if alertMatches(al, kws) {
				hits++
			}
		}
	}
	step.Status = "ok"
	step.Result = mcp.ResultOf(raw).Preview(4000)
	step.Result["alerts"] = len(alerts)
	step.Result["matchedKeywords"] = hits
	if parseErr != nil {
		step.Result["parseError"] = parseErr.Error()
	}
	e.appendStep(sess, step)

	if len(hosts) == 0 {
		e.appendStep(sess, domain.Step{
			Source: "promthus", Tool: "query", Title: "Prometheus 目标存活（up）",
			Status: "skipped", Error: "未解析到目标主机（addr 与影响面中间件地址均为空）",
		})
		return up, hits, true
	}
	promql := identity.UpQuery(hosts)
	if promql == "" {
		return up, hits, true
	}
	start = time.Now()
	raw, err = e.mcp.Call(ctx, "prometheus", "query", map[string]any{"query": promql})
	step = domain.Step{
		Source: "promthus", Tool: "query", Title: "Prometheus 目标存活（up）",
		Args: map[string]any{"query": promql, "hosts": hosts},
	}
	step.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		step.Status = "error"
		step.Error = err.Error()
	} else {
		res, queryErr := mcp.PrometheusQuery(raw)
		// 按 instance 展开 up，供 CMDB 交叉验证与排序使用。
		if samples, sErr := mcp.PrometheusSamples(raw); sErr == nil {
			for _, s := range samples {
				// 只认 up 指标，避免 mock / 混合返回把其它指标当成心跳。
				if name := s.Metric["__name__"]; name != "" && name != "up" {
					continue
				}
				if h := identity.NormalizeHost(s.Metric["instance"]); h != "" {
					up[h] = s.Value
				}
			}
		}
		step.Status = "ok"
		step.Result = mcp.ResultOf(raw).Preview(3000)
		step.Result["series"] = len(res.Result)
		step.Result["upSeries"] = len(up)
		step.Result["up"] = len(up) > 0
		if queryErr != nil {
			step.Result["parseError"] = queryErr.Error()
		}
		// 用验真结果补齐缺失样本：能查到序列但没返回值 → 采集异常 / 宕机（按 up=0 处理）；
		// 查不到序列 → 未纳管，不能当成宕机。
		if registered != nil {
			unmanaged := []string{}
			missing := []string{}
			for _, h := range hosts {
				if _, ok := up[h]; ok {
					continue
				}
				if registered[h] {
					up[h] = 0
					missing = append(missing, h)
				} else {
					unmanaged = append(unmanaged, h)
				}
			}
			if len(missing) > 0 {
				step.Result["upMissing"] = missing
			}
			if len(unmanaged) > 0 {
				step.Result["unmanaged"] = unmanaged
			}
		}
	}
	e.appendStep(sess, step)
	return up, hits, true
}

// cmdbMaxHosts 单次排障最多向 CMDB 反查的主机数，避免证据采集把会话拖长。
const cmdbMaxHosts = 5

// cmdbAsset 单个主机在 CMDB 中的资产归属。
type cmdbAsset struct {
	Host      string `json:"host"`
	Found     bool   `json:"found"`
	HostName  string `json:"hostName,omitempty"`
	Node      string `json:"node,omitempty"`
	State     int    `json:"state"`
	StateText string `json:"stateText,omitempty"`
	Owner     string `json:"owner,omitempty"`
	UsingBy   string `json:"usingBy,omitempty"`
	Env       string `json:"environment,omitempty"`
	Spec      string `json:"specification,omitempty"`
	UpKnown   bool   `json:"upKnown"`
	Up        bool   `json:"promUp"`
}

// cmdbEvidence CMDB 资产证据：既写入证据链，也供 rank 做跨源判断。
type cmdbEvidence struct {
	Queried   int         `json:"queried"`
	Matched   int         `json:"matched"`
	Online    int         `json:"online"`
	Offline   int         `json:"offline"`
	Unmatched []string    `json:"unmatched,omitempty"`
	Nodes     []string    `json:"nodes,omitempty"`
	Owners    []string    `json:"owners,omitempty"`
	Assets    []cmdbAsset `json:"assets,omitempty"`
}

// collectCMDB 把 Prometheus / 拓扑得到的主机标识传导到 CMDB：
// 主机 → 资产记录 → 机房 / 在线状态 / 负责人，并与 Prometheus up 交叉验证。
func (e *Engine) collectCMDB(ctx context.Context, sess *domain.DiagnosisSession, hosts []string, up map[string]float64) *cmdbEvidence {
	ev := &cmdbEvidence{}
	step := domain.Step{
		Source: "cmdb", Tool: "cmdb_server_query",
		Title: "CMDB 资产归属与在线状态",
		Args:  map[string]any{"hosts": hosts, "source": "prometheus.up + topo.middlewares"},
	}
	if !e.mcp.Enabled() {
		step.Status = "skipped"
		step.Error = "上游服务未启用（MCP_ENABLED=false），未接入 CMDB"
		e.appendStep(sess, step)
		return ev
	}
	if len(hosts) == 0 {
		step.Status = "skipped"
		step.Error = "未解析到需要反查的主机"
		e.appendStep(sess, step)
		return ev
	}

	// IP 优先：CMDB 的 ip 字段是 IP，域名排前面会白白占掉查询名额。
	targets := append([]string{}, hosts...)
	sort.SliceStable(targets, func(i, j int) bool {
		return identity.IsIP(targets[i]) && !identity.IsIP(targets[j])
	})
	if len(targets) > cmdbMaxHosts {
		targets = targets[:cmdbMaxHosts]
	}
	start := time.Now()
	var firstErr error
	for _, h := range targets {
		ev.Queried++
		raw, err := e.mcp.Call(ctx, "cmdb", "cmdb_server_query", map[string]any{
			"ip": h, "currentPage": 1, "pageSize": 5,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		page, perr := mcp.CMDBRecords(raw)
		if perr != nil {
			if firstErr == nil {
				firstErr = perr
			}
			continue
		}
		rec := e.reg.MatchRecord(page.Records, h)
		if rec == nil {
			ev.Unmatched = append(ev.Unmatched, h)
			ev.Assets = append(ev.Assets, cmdbAsset{Host: h})
			continue
		}
		asset := cmdbAsset{
			Host:     h,
			Found:    true,
			HostName: strField(rec, "hostName", "host_name", "hostname"),
			Node:     strField(rec, "node", "idc", "room"),
			State:    intField(rec, "state"),
			Owner:    strField(rec, "operA", "owner", "manager"),
			UsingBy:  strField(rec, "usingBy", "using_by"),
			Env:      strField(rec, "environment", "env"),
			Spec:     strField(rec, "specification", "spec"),
		}
		asset.StateText = cmdbStateText(asset.State)
		if v, ok := up[identity.NormalizeHost(h)]; ok {
			asset.UpKnown, asset.Up = true, v > 0
		}
		ev.Matched++
		if asset.State == 0 {
			ev.Online++
		} else {
			ev.Offline++
		}
		ev.Nodes = appendUniqueStr(ev.Nodes, asset.Node)
		ev.Owners = appendUniqueStr(ev.Owners, asset.Owner)
		ev.Assets = append(ev.Assets, asset)
	}
	step.LatencyMs = time.Since(start).Milliseconds()
	if ev.Queried > 0 && ev.Matched == 0 && firstErr != nil {
		step.Status = "error"
		step.Error = "CMDB 查询失败：" + firstErr.Error()
	} else {
		step.Status = "ok"
	}
	step.Result = map[string]any{
		"summary": ev,
		"note":    fmt.Sprintf("对前 %d 个候选主机反查 CMDB 资产；state=0 为在线", len(targets)),
	}
	e.appendStep(sess, step)
	return ev
}

func cmdbStateText(state int) string {
	switch state {
	case 0:
		return "在线"
	case 4:
		return "已报废"
	case 6:
		return "待清退"
	default:
		return fmt.Sprintf("非在线(%d)", state)
	}
}

func strField(rec map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := rec[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func intField(rec map[string]any, keys ...string) int {
	for _, k := range keys {
		switch v := rec[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return int(n)
			}
		}
	}
	return 0
}

func appendUniqueStr(list []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func headStrings(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return list[:n]
}

// alertMatches 判断告警的 labels / annotations 是否命中任一关键字。
func alertMatches(al mcp.PromAlert, kws []string) bool {
	if len(kws) == 0 {
		return false
	}
	var b strings.Builder
	for k, v := range al.Labels {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(v)
		b.WriteByte(' ')
	}
	for k, v := range al.Annotations {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(v)
		b.WriteByte(' ')
	}
	b.WriteString(al.State)
	text := strings.ToLower(b.String())
	for _, kw := range kws {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// isHostLike 判断是否是可放进 PromQL instance 匹配的主机形态（纯 ASCII，排除中文资产名）。
func isHostLike(s string) bool {
	for _, r := range s {
		if r > 127 || r == ' ' || r == '"' || r == '{' || r == '}' {
			return false
		}
	}
	return true
}

// candidateHosts 汇总请求地址与影响面中间件地址，经身份映射补全 IP / hostName 后去重。
func (e *Engine) candidateHosts(req Request, impact *domain.Impact) []string {
	out := []string{}
	seen := map[string]struct{}{}
	add := func(v string) {
		n := identity.NormalizeHost(v)
		if n == "" || !isHostLike(n) {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		if len(out) >= 20 {
			return
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	add(req.Addr)
	if e.reg != nil {
		if h, ok := e.reg.ResolveHost(req.Addr); ok {
			add(h.Host)
			for _, v := range h.Addrs {
				add(v)
			}
			for _, v := range h.IPs {
				add(v)
			}
			for _, v := range h.HostNames {
				add(v)
			}
		}
	}
	if impact != nil {
		for _, mw := range impact.Middlewares {
			add(mw.Addr)
			if e.reg == nil {
				continue
			}
			if h, ok := e.reg.ResolveHost(mw.Addr); ok {
				add(h.Host)
				for _, v := range h.Addrs {
					add(v)
				}
				for _, v := range h.IPs {
					add(v)
				}
				for _, v := range h.HostNames {
					add(v)
				}
			}
		}
	}
	return out
}

// evidence 第 4 步取证产出的证据集：来源 → 有效证据条数。
// 第 5 步定因不再写死置信度，而是按「证据来源数 + 证据条数」推导基础置信度，
// 再乘以各规则的强度系数；无任何有效证据时强制降级为「疑似」。
type evidence struct {
	sources map[string]int

	topoApps      int
	pinpointOK    bool
	alertOK       bool
	alertHits     int
	upKnown       int
	upDown        []string
	unmanaged     []string
	cmdbMatched   int
	cmdbUnmatched int
	rangePoints   int
	rangeRising   []string
	traceID       string
	traceSpans    int
	traceHot      string
	traceShare    float64
	logErrors     int
	logOK         bool
}

func newEvidence() *evidence { return &evidence{sources: map[string]int{}} }

func (ev *evidence) add(source string, n int) {
	if ev == nil || n <= 0 {
		return
	}
	ev.sources[source] += n
}

func (ev *evidence) sourceCount() int {
	if ev == nil {
		return 0
	}
	return len(ev.sources)
}

func (ev *evidence) items() int {
	if ev == nil {
		return 0
	}
	total := 0
	for _, n := range ev.sources {
		total += n
	}
	return total
}

func (ev *evidence) list() []string {
	if ev == nil {
		return nil
	}
	out := make([]string, 0, len(ev.sources))
	for k := range ev.sources {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// confidence 基础置信度：0.25 起步，每类证据来源 +0.10，每条证据 +0.02（最多计 10 条），封顶 0.9。
func (ev *evidence) confidence() float64 {
	if ev == nil || ev.sourceCount() == 0 {
		return 0.2
	}
	items := ev.items()
	if items > 10 {
		items = 10
	}
	c := 0.25 + 0.10*float64(ev.sourceCount()) + 0.02*float64(items)
	if c > 0.9 {
		c = 0.9
	}
	return round2(c)
}

func (e *Engine) rank(sess *domain.DiagnosisSession, req Request, impact *domain.Impact, ev *evidence, assets *cmdbEvidence) {
	threshold := e.cfg.ExplosionThreshold
	if threshold <= 0 {
		threshold = 10
	}
	base := ev.confidence()
	cands := []domain.Candidate{}
	add := func(title, reason, source string, strength float64) {
		cands = append(cands, domain.Candidate{
			Title:      title,
			Reason:     reason,
			Source:     source,
			Confidence: round2(base * strength),
		})
	}

	if impact != nil && impact.TotalApps >= threshold {
		add(
			fmt.Sprintf("共享中间件爆炸半径过大（影响 %d 个应用）", impact.TotalApps),
			"目标中间件被大量应用共享，单点故障会横向扩散，优先检查该中间件实例的健康度",
			"topo", 1.0,
		)
	}
	if impact != nil && impact.Level == "app" && len(impact.Middlewares) == 1 {
		add(
			"应用仅依赖单一中间件实例",
			fmt.Sprintf("该应用只关联 1 类中间件实例（%s），无旁路，故障时无降级路径", impact.Middlewares[0].Type),
			"topo", 0.7,
		)
	}
	if impact != nil && impact.Level == "middleware" && impact.TotalApps > 0 && impact.TotalApps < 3 {
		add(
			"影响面收敛，故障大概率在应用侧",
			fmt.Sprintf("依赖该中间件的应用仅 %d 个，若告警只集中在单个应用，应优先排查应用自身", impact.TotalApps),
			"topo", 0.6,
		)
	}
	if req.AppID != "" && !ev.pinpointOK {
		add(
			"调用链证据缺失（Pinpoint 未覆盖）",
			"Pinpoint 未收录该应用或采集失败，无法通过调用链定位耗时瓶颈，结论需人工补充",
			"pinpoint", 0.45,
		)
	}
	if ev.alertOK && ev.alertHits > 0 {
		add(
			"存在与目标相关的活跃告警",
			fmt.Sprintf("Prometheus 活跃告警中包含 %d 个目标关键字，需按告警时间线对齐故障起始时刻", ev.alertHits),
			"promthus", 0.95,
		)
	}
	// 调用链：单 span 占比 > 50% 视为该 span 是瓶颈。
	if ev.traceSpans > 0 && ev.traceShare >= 0.5 {
		add(
			fmt.Sprintf("调用链瓶颈集中（%s 单 span 占比 %.0f%%）", ev.traceHot, ev.traceShare*100),
			fmt.Sprintf("Pinpoint trace %s 共 %d 个 span，最长 span 占整个事务 %.0f%%，优先核对该方法的慢查询 / 锁 / 下游超时", ev.traceID, ev.traceSpans, ev.traceShare*100),
			"pinpoint", 1.0,
		)
	}
	// 日志：错误计数 > 0 才构成证据。
	if ev.logErrors > 0 {
		add(
			"日志错误激增",
			fmt.Sprintf("时间窗内命中 %d 条 error 日志，需结合报文样本确认异常类型与起始时刻", ev.logErrors),
			"log", 0.85,
		)
	}
	// 指标基线：前后段对比持续上升，视为资源劣化。
	if len(ev.rangeRising) > 0 {
		add(
			"主机负载持续劣化",
			fmt.Sprintf("%d 台候选主机的 node_load1 在观测窗内上升超过 30%%：%s", len(ev.rangeRising), strings.Join(headStrings(ev.rangeRising, 3), "、")),
			"promthus", 0.8,
		)
	}
	// CMDB 资产维度：把 Prometheus 心跳与资产台账交叉验证，补齐「机器是谁的、在哪、是否还在线」。
	if assets != nil {
		stale := []string{}
		dead := []string{}
		for _, a := range assets.Assets {
			if !a.Found || a.State == 0 {
				continue
			}
			if a.UpKnown && a.Up {
				stale = append(stale, a.Host)
			} else {
				dead = append(dead, a.Host)
			}
		}
		if len(stale) > 0 {
			add(
				"CMDB 资产状态过期（台账非在线，监控仍有心跳）",
				fmt.Sprintf("%d 个候选主机在 CMDB 非在线，但 Prometheus up=1 仍采集到心跳：%s，资产状态需校正", len(stale), strings.Join(headStrings(stale, 3), "、")),
				"cmdb", 0.85,
			)
		}
		if len(dead) > 0 {
			add(
				"故障主机在 CMDB 已非在线且无监控心跳",
				fmt.Sprintf("%d 个候选主机 CMDB 状态非在线且 Prometheus 无心跳：%s，故障大概率来自实例已下线但仍被依赖", len(dead), strings.Join(headStrings(dead, 3), "、")),
				"cmdb", 1.0,
			)
		}
		if len(assets.Unmatched) > 0 {
			add(
				"依赖实例未纳入 CMDB（账实不符）",
				fmt.Sprintf("%d 个候选主机在 CMDB 查无资产：%s，需确认是否未纳管或拓扑数据过期", len(assets.Unmatched), strings.Join(headStrings(assets.Unmatched, 3), "、")),
				"cmdb", 0.7,
			)
		}
	}
	if len(cands) == 0 {
		add(
			"证据不足，需人工补充",
			"拓扑与上游服务均未给出强信号，建议补充告警原文、时间窗与业务现象描述",
			"rule", 0.35,
		)
	}
	sortCandidates(cands)

	// 无任何有效证据时，所有结论强制降为「疑似」，不得标记为已定因。
	suspected := ev.sourceCount() == 0
	if suspected {
		for i := range cands {
			if cands[i].Confidence > 0.2 {
				cands[i].Confidence = 0.2
			}
			cands[i].Reason += "；本次无任何有效证据，结论仅为疑似"
		}
		sortCandidates(cands)
	}
	// 拓扑无数据且 Pinpoint 未匹配时，关键证据链缺失：
	// 即使告警关键字命中，也把最高置信度压到 root_caused 阈值以下，避免假阳性直接开高优工单。
	weakEvidence := (impact == nil || impact.TotalApps == 0) && !ev.pinpointOK
	if weakEvidence && cands[0].Confidence >= 0.6 {
		cands[0].Confidence = 0.5
		cands[0].Reason += "；拓扑与调用链均无有效数据，置信度已下调"
		sortCandidates(cands)
	}
	// 数据源验真未通过时，基于该源的结论不能直接判为 root_caused：置信度一律封顶 0.5。
	if degraded := degradedSources(sess.SourceHealth); len(degraded) > 0 {
		capped := false
		for i := range cands {
			if cands[i].Confidence > 0.5 {
				cands[i].Confidence = 0.5
				capped = true
			}
		}
		if capped {
			sortCandidates(cands)
		}
		cands[0].Reason += fmt.Sprintf("；数据源验真未通过（%s），置信度已下调", strings.Join(degraded, "、"))
	}

	rootCause := cands[0].Title + "（待人工确认）"
	if suspected {
		rootCause = "证据不足，暂无法定因（疑似）"
	}
	e.mu.Lock()
	sess.Candidates = cands
	sess.RootCause = rootCause
	sess.Confidence = cands[0].Confidence
	ticket := sess.Ticket
	e.mu.Unlock()

	e.appendStep(sess, domain.Step{
		Source: "rule",
		Tool:   "rank",
		Title:  "根因候选排序",
		Status: "ok",
		Result: map[string]any{
			"candidates":   cands,
			"weakEvidence": weakEvidence,
			"suspected":    suspected,
			"evidence": map[string]any{
				"sources":        ev.list(),
				"items":          ev.items(),
				"baseConfidence": base,
				"upKnown":        ev.upKnown,
				"upDown":         ev.upDown,
				"unmanaged":      ev.unmanaged,
				"cmdbMatched":    ev.cmdbMatched,
				"cmdbUnmatched":  ev.cmdbUnmatched,
				"rangePoints":    ev.rangePoints,
				"rangeRising":    ev.rangeRising,
				"traceId":        ev.traceID,
				"traceSpans":     ev.traceSpans,
				"traceShare":     round2(ev.traceShare),
				"logErrors":      ev.logErrors,
			},
			"ticket": ticket,
			"note":   "置信度由证据集（来源数 × 条数）推导；接入 kagent + HolmesGPT 后由 LLM 复核并生成处置建议",
		},
	})
}

// buildTicket 把第 2 步「确认与定界」的结果固化为问题单。
func (e *Engine) buildTicket(req Request, impact *domain.Impact, ev *evidence, assets *cmdbEvidence, health []domain.SourceHealth, appID, appName string, hosts []string, windowMin int) *domain.DiagnosisTicket {
	if windowMin <= 0 {
		windowMin = 5
	}
	now := time.Now()
	t := &domain.DiagnosisTicket{
		Entity: domain.TicketEntity{
			AppID:          appID,
			AppName:        appName,
			MiddlewareType: req.MiddlewareType,
			Addr:           req.Addr,
		},
		Signals: ev.list(),
		Window: domain.TicketWindow{
			Start:   now.Add(-time.Duration(windowMin) * time.Minute),
			End:     now,
			Minutes: windowMin,
		},
		Impact: domain.TicketImpact{
			Hosts:    len(hosts),
			HostList: headStrings(hosts, 10),
		},
	}
	if impact != nil {
		t.Impact.Apps = impact.TotalApps
		t.Impact.Middlewares = len(impact.Middlewares)
		names := make([]string, 0, len(impact.Apps))
		for _, a := range impact.Apps {
			names = append(names, firstNonEmpty(a.AppName, a.AppID))
		}
		t.Impact.AppNames = headStrings(names, 10)
	}
	if assets != nil {
		t.Impact.Nodes = headStrings(assets.Nodes, 5)
	}
	// 主实体主机：优先取已确认宕机/失联的主机，否则取第一个候选主机。
	primary := ""
	if len(ev.upDown) > 0 {
		primary = ev.upDown[0]
	} else if len(hosts) > 0 {
		primary = hosts[0]
	}
	if primary != "" {
		if identity.IsIP(primary) {
			t.Entity.IP = primary
		} else {
			t.Entity.HostName = primary
		}
		if assets != nil {
			for _, a := range assets.Assets {
				if a.Host != primary {
					continue
				}
				if a.HostName != "" {
					t.Entity.HostName = a.HostName
				}
				if t.Entity.IP == "" && identity.IsIP(a.Host) {
					t.Entity.IP = a.Host
				}
				t.Entity.Node = a.Node
				t.Entity.Owner = a.Owner
				break
			}
		}
	}

	offline := 0
	if assets != nil {
		offline = assets.Offline
	}
	switch {
	case len(ev.upDown) > 0 || offline > 0 || (ev.traceSpans > 0 && ev.traceShare >= 0.5):
		t.Severity = "critical"
	case ev.alertHits > 0 || ev.logErrors > 0 || ev.cmdbUnmatched > 0 || len(ev.rangeRising) > 0:
		t.Severity = "warning"
	default:
		t.Severity = "info"
	}

	degraded := degradedSources(health)
	switch {
	case ev.sourceCount() == 0:
		t.Confidence = "no_data"
		t.Reason = "所有数据源均未返回有效数据，问题单按 no_data 结案，不进入定位"
	case len(degraded) > 0:
		t.Confidence = "degraded"
		t.Reason = fmt.Sprintf("有效证据 %d 条（来源：%s）；数据源验真未通过：%s", ev.items(), strings.Join(ev.list(), "、"), strings.Join(degraded, "、"))
	default:
		t.Confidence = "confirmed"
		t.Reason = fmt.Sprintf("有效证据 %d 条（来源：%s）", ev.items(), strings.Join(ev.list(), "、"))
	}
	return t
}

// collectRangeBaseline 取证：用 range_query 取故障窗前的指标基线。
// 采样点 < 2 视为无数据，不参与趋势判断；前后 1/3 均值比 ≥ 1.3 记为持续劣化。
func (e *Engine) collectRangeBaseline(ctx context.Context, sess *domain.DiagnosisSession, hosts []string) (points int, rising []string) {
	step := domain.Step{
		Source: "promthus", Tool: "range_query",
		Title: "Prometheus 指标基线（range_query）",
	}
	if !e.mcp.Enabled() {
		step.Status = "skipped"
		step.Error = "上游服务未启用（MCP_ENABLED=false），跳过指标基线"
		e.appendStep(sess, step)
		return 0, nil
	}
	if len(hosts) == 0 {
		step.Status = "skipped"
		step.Error = "未解析到候选主机，无法建立指标基线"
		e.appendStep(sess, step)
		return 0, nil
	}
	window := e.cfg.DiagWindowMin
	if window <= 0 {
		window = 5
	}
	lookback := 2 * window
	targets := headStrings(hosts, 3)
	step.Args = map[string]any{"hosts": targets, "metric": "node_load1", "minutes": lookback, "step": "60s"}

	start := time.Now()
	perHost := map[string]any{}
	for _, host := range targets {
		promql := fmt.Sprintf(`node_load1{instance=~"%s"}`, identity.PromQLInstanceRegex(host))
		raw, err := e.mcp.Call(ctx, "prometheus", "range_query", map[string]any{
			"query":      promql,
			"start_time": start.Add(-time.Duration(lookback) * time.Minute).Format(time.RFC3339),
			"end_time":   start.Format(time.RFC3339),
			"step":       "60s",
		})
		if err != nil {
			perHost[host] = "error: " + err.Error()
			continue
		}
		m, perr := mcp.PrometheusMatrix(raw)
		if perr != nil {
			perHost[host] = "parse_error: " + perr.Error()
			continue
		}
		n := len(m.Values)
		perHost[host] = n
		if n < 2 {
			continue
		}
		points += n
		k := n / 3
		if k < 1 {
			k = 1
		}
		first := meanFloat(m.Values[:k])
		last := meanFloat(m.Values[n-k:])
		if first > 0 && last/first >= 1.3 {
			rising = append(rising, host)
		}
	}
	step.LatencyMs = time.Since(start).Milliseconds()
	step.Status = "ok"
	step.Result = map[string]any{
		"samples":     points,
		"risingHosts": rising,
		"perHost":     perHost,
		"note":        "采样点 < 2 视为无数据，不参与趋势判断",
	}
	e.appendStep(sess, step)
	return points, rising
}

// collectTraceEvidence 取证：drag_heatmap 收敛热点事务 → get_transaction_trace_tool 下钻 span 树。
// 单 span 耗时占整个事务 ≥ 50% 视为瓶颈。缺少 traceId 时明确跳过，不伪造结论。
func (e *Engine) collectTraceEvidence(ctx context.Context, sess *domain.DiagnosisSession, appName string, pinpointOK bool, windowMin int) (traceID string, spans int, hot string, share float64) {
	heat := domain.Step{
		Source: "pinpoint", Tool: "drag_heatmap_tool",
		Title: "调用链热点（drag_heatmap）",
	}
	if !e.mcp.Enabled() {
		heat.Status = "skipped"
		heat.Error = "上游服务未启用（MCP_ENABLED=false），跳过调用链取证"
		e.appendStep(sess, heat)
		return
	}
	if !pinpointOK {
		heat.Status = "skipped"
		heat.Error = "Pinpoint 未匹配到应用，无 traceId 可取"
		e.appendStep(sess, heat)
		return
	}
	now := time.Now()
	from := now.Add(-time.Duration(windowMin) * time.Minute)
	heat.Args = map[string]any{"application_name": appName, "from": from.UnixMilli(), "to": now.UnixMilli()}
	start := time.Now()
	raw, err := e.mcp.Call(ctx, "pinpoint", "drag_heatmap_tool", map[string]any{
		"application_name": appName,
		"from":             from.UnixMilli(),
		"to":               now.UnixMilli(),
	})
	heat.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		heat.Status = "skipped"
		heat.Error = "调用链热点缺失：" + err.Error()
		e.appendStep(sess, heat)
		return
	}
	traceID = firstTraceID(raw)
	heat.Status = "ok"
	heat.Result = mcp.ResultOf(raw).Preview(1500)
	heat.Result["traceId"] = traceID
	e.appendStep(sess, heat)
	if traceID == "" {
		e.appendStep(sess, domain.Step{
			Source: "pinpoint", Tool: "get_transaction_trace_tool",
			Title:  "调用链明细",
			Status: "skipped",
			Error:  "drag_heatmap 未返回 traceId（需 exemplar_query 或人工指定），无法下钻 span",
		})
		return
	}

	tr := domain.Step{
		Source: "pinpoint", Tool: "get_transaction_trace_tool",
		Title: "调用链明细（trace span）",
		Args:  map[string]any{"application_name": appName, "trace_id": traceID},
	}
	start = time.Now()
	raw, err = e.mcp.Call(ctx, "pinpoint", "get_transaction_trace_tool", map[string]any{
		"application_name": appName,
		"trace_id":         traceID,
	})
	tr.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		tr.Status = "skipped"
		tr.Error = "调用链明细缺失：" + err.Error()
		e.appendStep(sess, tr)
		return
	}
	spans, hot, share = summarizeTrace(raw)
	tr.Status = "ok"
	tr.Result = map[string]any{
		"traceId":    traceID,
		"spans":      spans,
		"bottleneck": hot,
		"share":      round2(share),
		"note":       "单 span 耗时占整个事务 ≥ 50% 视为瓶颈",
	}
	e.appendStep(sess, tr)
	return traceID, spans, hot, share
}

// collectLogEvidence 取证：优先用 ES MCP 的 search 统计时间窗内错误日志；
// 未接入 ES 或查询失败时回退本地 log-analyzer。任何一侧失败都降级为 skipped，不阻断定因。
func (e *Engine) collectLogEvidence(ctx context.Context, sess *domain.DiagnosisSession, req Request, hosts []string) (int, bool) {
	if n, ok := e.collectLogES(ctx, sess, req, hosts); ok {
		return n, true
	}
	return e.collectLogLocal(ctx, sess, req)
}

// collectLogES 走 ES MCP：按 index / 时间窗 / level=error / 候选主机统计错误条数。
// 日志是主机维度的证据，未解析到候选主机时直接跳过，避免把无关日志算成故障信号。
func (e *Engine) collectLogES(ctx context.Context, sess *domain.DiagnosisSession, req Request, hosts []string) (int, bool) {
	if e.cfg.ESMCPURL == "" || !e.mcp.Enabled() {
		return 0, false
	}
	if len(hosts) == 0 {
		return 0, false
	}
	step := domain.Step{
		Source: "log", Tool: "search",
		Title: "日志证据（ES search）",
	}
	window := e.cfg.DiagWindowMin
	if window <= 0 {
		window = 5
	}
	timeField := firstNonEmpty(e.cfg.ESLogTimeField, "@timestamp")
	levelField := firstNonEmpty(e.cfg.ESLogLevelField, "level")
	hostField := firstNonEmpty(e.cfg.ESLogHostField, "host")
	index := firstNonEmpty(e.cfg.ESLogIndex, "logs-*")

	terms := []string{}
	for _, h := range headStrings(hosts, 5) {
		terms = append(terms, fmt.Sprintf(`%s:"%s"`, hostField, h))
	}
	body := map[string]any{
		"size":             0,
		"track_total_hits": true,
		"query": map[string]any{"bool": map[string]any{
			"filter": []any{
				map[string]any{"range": map[string]any{timeField: map[string]any{"gte": fmt.Sprintf("now-%dm", window)}}},
				map[string]any{"match": map[string]any{levelField: "error"}},
			},
			"must": []any{map[string]any{"query_string": map[string]any{"query": strings.Join(terms, " OR ")}}},
		}},
	}
	step.Args = map[string]any{"index": index, "queryBody": body, "hosts": headStrings(hosts, 5), "windowMinutes": window}

	start := time.Now()
	raw, err := e.mcp.Call(ctx, "es", "search", map[string]any{"index": index, "query_body": body})
	step.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		step.Status = "error"
		step.Error = fmt.Sprintf("ES search 失败：%v（回退本地日志分析）", err)
		e.appendStep(sess, step)
		return 0, false
	}
	n, perr := esTotalHits(raw)
	if perr != nil {
		step.Status = "error"
		step.Error = fmt.Sprintf("ES search 结果无法解析：%v（回退本地日志分析）", perr)
		e.appendStep(sess, step)
		return 0, false
	}
	step.Status = "ok"
	step.Result = map[string]any{
		"index":         index,
		"errors":        n,
		"windowMinutes": window,
		"levelField":    levelField,
		"note":          "错误计数取 total；relation=gte 时标注为下限",
		"raw":           mcp.ResultOf(raw).Preview(800),
	}
	e.appendStep(sess, step)
	return n, true
}

// collectLogLocal 回退路径：调用本地 log-analyzer（原 T10 实现）。
func (e *Engine) collectLogLocal(ctx context.Context, sess *domain.DiagnosisSession, req Request) (int, bool) {
	step := domain.Step{
		Source: "log", Tool: "log_analyzer",
		Title: "日志证据（错误 TopN / 异常模式）",
	}
	if e.logs == nil {
		step.Status = "skipped"
		step.Error = "未配置 ES 上游服务与日志分析服务（ES_MCP_URL / LOG_ANALYZER_URL），跳过日志证据"
		e.appendStep(sess, step)
		return 0, false
	}
	path := e.resolveLogPath(req)
	if path == "" {
		step.Status = "skipped"
		step.Error = "未解析到日志路径（可通过 logPath 指定，或配置 LOG_PATH_TEMPLATE）"
		e.appendStep(sess, step)
		return 0, false
	}
	step.Args = map[string]any{"path": path, "windowMinutes": 10}

	start := time.Now()
	lev, err := e.logs.Analyze(ctx, path)
	step.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		step.Status = "skipped"
		step.Error = fmt.Sprintf("日志证据缺失：%v（不阻断排障）", err)
		e.appendStep(sess, step)
		return 0, false
	}

	step.Status = "ok"
	step.Result = map[string]any{
		"path":          lev.Path,
		"taskId":        lev.TaskID,
		"files":         lev.Files,
		"lines":         lev.Lines,
		"errors":        lev.Errors,
		"warnings":      lev.Warnings,
		"topTypes":      lev.TopTypes,
		"topClasses":    lev.TopClasses,
		"keyIssues":     lev.KeyIssues,
		"patterns":      lev.Patterns,
		"windowMinutes": 10,
		"note":          "关联告警触发前 10 分钟窗口；日志分析服务按文件全量分析（接口暂不支持时间段裁剪）",
	}
	e.appendStep(sess, step)
	return lev.Errors, true
}

// esTotalHits 从 ES search 返回中提取命中总数：优先 "Total results: N"，其次 hits.total.value。
func esTotalHits(raw json.RawMessage) (int, error) {
	text := string(raw)
	if m := regexp.MustCompile(`Total results:\s*(\d+)`).FindStringSubmatch(text); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n, nil
	}
	var v struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &v); err == nil && v.Hits.Total.Value > 0 {
		return v.Hits.Total.Value, nil
	}
	if data, ok := payloadData(raw).(map[string]any); ok {
		if hits, ok := data["hits"].(map[string]any); ok {
			if total, ok := hits["total"].(map[string]any); ok {
				if n := int(floatField(total, "value")); n > 0 {
					return n, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("响应中未找到命中总数")
}

// firstTraceID 从 drag_heatmap 等返回中提取 traceId，未找到时返回空串（不伪造）。
func firstTraceID(raw json.RawMessage) string {
	data := payloadData(raw)
	switch t := data.(type) {
	case map[string]any:
		for _, k := range []string{"traceIds", "trace_ids", "traceId"} {
			switch v := t[k].(type) {
			case string:
				if v != "" {
					return v
				}
			case []any:
				for _, it := range v {
					if s, ok := it.(string); ok && s != "" {
						return s
					}
				}
			}
		}
		if dots, ok := t["dots"].([]any); ok {
			for _, it := range dots {
				if m, ok := it.(map[string]any); ok {
					if s := strAny(m["traceId"]); s != "" {
						return s
					}
				}
			}
		}
	case []any:
		for _, it := range t {
			if m, ok := it.(map[string]any); ok {
				if s := strAny(m["traceId"]); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// summarizeTrace 解析 Pinpoint 事务：返回 span 数、最耗时子 span、该 span 占事务耗时的比例。
// 根 span 代表整个事务，不参与瓶颈计算；单 span 占比 ≥ 50% 才算瓶颈。
func summarizeTrace(raw json.RawMessage) (spans int, hot string, share float64) {
	root, ok := payloadData(raw).(map[string]any)
	if !ok {
		return 0, "", 0
	}
	elapsed := floatField(root, "elapsed", "elapsedTime")
	stack, _ := root["callStack"].([]any)
	if len(stack) == 0 {
		return 0, "", 0
	}
	total := 0.0
	maxChild := 0.0
	// Pinpoint 的 callStack 有两种形态：嵌套（callStack 子树）与扁平（parentId 关联）。
	// 两种都要能识别根 span，避免把根自身算成瓶颈或漏掉扁平结构的下游 span。
	var walk func(items []any, depth int)
	walk = func(items []any, depth int) {
		for i, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			spans++
			v := floatField(m, "executionMilliseconds", "executionTime", "elapsed")
			total += v
			rootSpan := depth == 0 && i == 0
			if _, has := m["parentId"]; has {
				if pid := floatField(m, "parentId"); pid >= 0 {
					rootSpan = false
				}
			} else if _, has := m["parentSpanId"]; has {
				if pid := floatField(m, "parentSpanId"); pid >= 0 {
					rootSpan = false
				}
			}
			if rootSpan {
				if elapsed <= 0 {
					elapsed = v
				}
			} else if v > maxChild {
				maxChild = v
				hot = firstNonEmpty(strAny(m["rpc"]), strAny(m["method"]), strAny(m["api"]), strAny(m["application"]))
			}
			if child, ok := m["callStack"].([]any); ok {
				walk(child, depth+1)
			}
		}
	}
	walk(stack, 0)
	den := elapsed
	if den <= 0 {
		den = total
	}
	if den > 0 && maxChild > 0 {
		share = maxChild / den
		if share > 1 {
			share = 1
		}
	}
	return spans, hot, share
}

// resolveLogPath 优先使用请求显式指定的 logPath，否则按 LOG_PATH_TEMPLATE 用 host/app 推导。
func (e *Engine) resolveLogPath(req Request) string {
	if p := strings.TrimSpace(req.LogPath); p != "" {
		return p
	}
	tpl := strings.TrimSpace(e.cfg.LogPathTemplate)
	if tpl == "" {
		return ""
	}
	host := hostOf(req.Addr)
	if strings.Contains(tpl, "{host}") && host == "" {
		return ""
	}
	p := strings.ReplaceAll(tpl, "{host}", host)
	p = strings.ReplaceAll(p, "{app}", req.AppID)
	return strings.TrimSpace(p)
}

func (e *Engine) appendStep(sess *domain.DiagnosisSession, s domain.Step) {
	e.mu.Lock()
	s.No = len(sess.Steps) + 1
	if s.At.IsZero() {
		s.At = time.Now()
	}
	sess.Steps = append(sess.Steps, s)
	cp := copySession(sess)
	e.mu.Unlock()
	_ = e.store.Put("diagnosis_session", cp.ID, cp)
}

func (e *Engine) persist(sess *domain.DiagnosisSession) {
	e.mu.RLock()
	cp := copySession(sess)
	e.mu.RUnlock()
	_ = e.store.Put("diagnosis_session", cp.ID, cp)
}

func (e *Engine) snapshot(sess *domain.DiagnosisSession) *domain.DiagnosisSession {
	e.mu.RLock()
	defer e.mu.RUnlock()
	cp := copySession(sess)
	return &cp
}

func copySession(s *domain.DiagnosisSession) domain.DiagnosisSession {
	cp := *s
	cp.Steps = append([]domain.Step{}, s.Steps...)
	cp.Candidates = append([]domain.Candidate{}, s.Candidates...)
	cp.SourceHealth = append([]domain.SourceHealth{}, s.SourceHealth...)
	return cp
}

func headApps(apps []domain.ImpactApp, n int) []domain.ImpactApp {
	if len(apps) <= n {
		return apps
	}
	return apps[:n]
}

func keywords(req Request) []string {
	out := []string{}
	for _, v := range []string{req.AppID, req.MiddlewareType, req.Addr, hostOf(req.Addr)} {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func hostOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// payloadData 取 MCP 返回的业务体中的 data 字段；没有 data 时退回整个 body。
func payloadData(raw json.RawMessage) any {
	var v any
	if err := json.Unmarshal(mcp.ResultOf(raw).Payload, &v); err != nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		if d, ok := m["data"]; ok {
			return d
		}
	}
	return v
}

// metricNamesOf 从 Prometheus 元数据返回中提取指标名（数组取 metric/__name__/name，字典取 key）。
func metricNamesOf(raw json.RawMessage) []string {
	out := []string{}
	switch t := payloadData(raw).(type) {
	case []any:
		for _, it := range t {
			m, _ := it.(map[string]any)
			for _, k := range []string{"metric", "__name__", "name"} {
				if v, ok := m[k].(string); ok && v != "" {
					out = append(out, v)
					break
				}
			}
		}
	case map[string]any:
		for k := range t {
			out = append(out, k)
		}
		sort.Strings(out)
	}
	return out
}

// hostSetOf 从 Prometheus 序列返回中提取归一主机集合。
func hostSetOf(raw json.RawMessage) map[string]bool {
	out := map[string]bool{}
	switch t := payloadData(raw).(type) {
	case []any:
		for _, it := range t {
			m, _ := it.(map[string]any)
			for _, k := range []string{"instance", "target", "host", "node", "ip"} {
				if v, ok := m[k].(string); ok && v != "" {
					if h := identity.NormalizeHost(v); h != "" {
						out[h] = true
					}
				}
			}
		}
	case map[string]any:
		for k := range t {
			if h := identity.NormalizeHost(k); h != "" {
				out[h] = true
			}
		}
	}
	return out
}

// countArrayOf 返回业务体数组 / 字典的元素个数，用于判断分片信息是否为空。
func countArrayOf(raw json.RawMessage) int {
	switch t := payloadData(raw).(type) {
	case []any:
		return len(t)
	case map[string]any:
		return len(t)
	}
	return 0
}

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

// degradedSources 返回验真未通过（degraded / unavailable）的数据源名。
func degradedSources(health []domain.SourceHealth) []string {
	out := []string{}
	for _, h := range health {
		if h.Status == "degraded" || h.Status == "unavailable" {
			out = append(out, h.Source)
		}
	}
	return out
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func meanFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func strAny(v any) string {
	s, _ := v.(string)
	return s
}

// floatField 依次取第一个可解析为数字的字段，兼容字符串数字。
func floatField(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case json.Number:
			f, _ := v.Float64()
			return f
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

func sortCandidates(c []domain.Candidate) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].Confidence > c[j-1].Confidence; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

func sortByCreatedDesc(list []domain.DiagnosisSession) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].CreatedAt.After(list[j-1].CreatedAt); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}
