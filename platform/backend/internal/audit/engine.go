package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/topo"
)

// Engine 资源稽核引擎（A.10）：
//
//	C1 存在性——topo 关系中的中间件 host 是否在 CMDB 有资产记录
//	C2 状态一致性——CMDB 资产 state 是否为在线（0）
//	C3 反向孤儿——CMDB 在线服务器是否被 topo 关系引用
//	C4 指标佐证——对风险 host 用 Prometheus up 指标交叉验证
//
// 数据源：topo 快照（本地）+ cmdb-mcp-server + prometheus-mcp。
type Engine struct {
	topo  topo.Provider
	mcp   *mcp.Gateway
	store store.Store
	reg   *identity.Registry
}

func New(tp topo.Provider, gw *mcp.Gateway, st store.Store, reg *identity.Registry) *Engine {
	return &Engine{topo: tp, mcp: gw, store: st, reg: reg}
}

type hostRef struct {
	Host string
	Type string
	Apps map[string]struct{}
}

// Run 执行一次稽核（同步，host 数量由 maxHosts 限流，默认 20）。
// 可选传入 host 白名单（完整地址或 host 部分皆可）：传了则只稽核匹配到的 host。
func (e *Engine) Run(date, env string, maxHosts int, hostFilter ...string) (*domain.AuditRun, error) {
	if maxHosts <= 0 {
		maxHosts = 20
	}
	if date == "" {
		dates := e.topo.Dates()
		if len(dates) == 0 {
			return nil, fmt.Errorf("拓扑数据源无可用快照")
		}
		date = dates[len(dates)-1]
	}
	if env == "" {
		env = "PRO"
	}
	run := &domain.AuditRun{
		ID:        uuid.NewString(),
		Date:      date,
		Env:       env,
		Status:    "running",
		Sources:   []string{"topo", "cmdb", "prometheus"},
		StartedAt: time.Now(),
	}
	e.persist(run)

	rows, err := e.topo.Rows(date, env)
	if err != nil {
		run.Status = "failed"
		run.Message = "读取拓扑快照失败: " + err.Error()
		run.FinishedAt = time.Now()
		e.persist(run)
		return run, nil
	}

	// 1) 汇总唯一 host（按被依赖应用数排序，优先稽核爆炸半径大的）
	hosts := collectHosts(rows)
	// host 白名单：前端从拓扑节点发起稽核时传单个 host / 完整地址，仅保留命中的
	if len(hostFilter) > 0 {
		allow := map[string]struct{}{}
		for _, f := range hostFilter {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			allow[normHost(hostOf(f))] = struct{}{} // 完整地址也只取 host 部分
			allow[normHost(f)] = struct{}{}         // 纯 host 直接纳入
		}
		kept := hosts[:0]
		for _, h := range hosts {
			if _, ok := allow[normHost(h.Host)]; ok {
				kept = append(kept, h)
			}
		}
		hosts = kept
	}
	sort.Slice(hosts, func(i, j int) bool { return len(hosts[i].Apps) > len(hosts[j].Apps) })
	if len(hosts) > maxHosts {
		hosts = hosts[:maxHosts]
	}

	findings := make([]domain.AuditFinding, 0, len(hosts)+4)
	add := func(f domain.AuditFinding) {
		f.ID = fmt.Sprintf("%s-%s", run.ID[:8], strings.ToLower(f.CheckCode)+"-"+sanitize(f.ObjectKey))
		f.RunID = run.ID
		f.CreatedAt = time.Now()
		findings = append(findings, f)
	}

	referenced := map[string]struct{}{}
	ctx := context.Background()

	// 2) C1 + C2 + C4：正向逐 host 对账 CMDB
	for _, h := range hosts {
		referenced[normHost(h.Host)] = struct{}{}
		recs, raw, err := e.cmdbQuery(ctx, map[string]any{"ip": h.Host, "currentPage": 1, "pageSize": 5})
		if err != nil {
			add(domain.AuditFinding{
				CheckCode: "AUDIT_C1", ObjectType: "middleware", ObjectKey: h.Host,
				Severity: "P3", Verdict: domain.AuditVerdictNoData,
				Detail:   "CMDB 查询失败，无法对账",
				Evidence: map[string]any{"error": err.Error(), "middlewareType": h.Type},
			})
			continue
		}
		rec := e.matchRecord(recs, h.Host)
		if rec == nil {
			add(domain.AuditFinding{
				CheckCode: "AUDIT_C1", ObjectType: "middleware", ObjectKey: h.Host,
				Severity: "P1", Verdict: domain.AuditVerdictRisk,
				Detail: fmt.Sprintf("CMDB 未收录该 IP（%s 被 %d 个应用依赖），资产账实不符或拓扑数据异常", h.Host, len(h.Apps)),
				Evidence: map[string]any{
					"ip": h.Host, "middlewareType": h.Type, "depApps": len(h.Apps),
					"cmdbRespBytes": len(raw), "cmdbRecords": len(recs),
				},
			})
			continue
		}
		state, _ := rec["state"].(float64)
		if state == 0 {
			add(domain.AuditFinding{
				CheckCode: "AUDIT_C2", ObjectType: "middleware", ObjectKey: h.Host,
				Severity: "P3", Verdict: domain.AuditVerdictNotTriggered,
				Detail:   "CMDB 状态为在线，与拓扑引用一致",
				Evidence: map[string]any{"ip": h.Host, "state": 0, "hostName": rec["hostName"], "node": rec["node"]},
			})
		} else {
			sev := "P2"
			if state == 4 || state == 6 { // 已报废 / 待清退 仍被引用
				sev = "P1"
			}
			add(domain.AuditFinding{
				CheckCode: "AUDIT_C2", ObjectType: "middleware", ObjectKey: h.Host,
				Severity: sev, Verdict: domain.AuditVerdictRisk,
				Detail: fmt.Sprintf("CMDB 状态=%d（非在线）但仍有 %d 个应用依赖该实例", int(state), len(h.Apps)),
				Evidence: map[string]any{
					"ip": h.Host, "middlewareType": h.Type, "state": int(state), "depApps": len(h.Apps),
					"hostName": rec["hostName"], "node": rec["node"],
				},
			})
			// C4 指标佐证：CMDB 说非在线，Prometheus 是否仍有心跳
			if up, err := e.promUp(ctx, h.Host); err == nil {
				verdict := domain.AuditVerdictNotTriggered
				detail := "Prometheus 无该主机心跳，与 CMDB 非在线状态一致"
				if up {
					verdict = domain.AuditVerdictRisk
					detail = "Prometheus 仍采集到该主机 up=1 心跳，CMDB 状态疑似过期"
				}
				add(domain.AuditFinding{
					CheckCode: "AUDIT_C4", ObjectType: "server", ObjectKey: h.Host,
					Severity: sev, Verdict: verdict, Detail: detail,
					Evidence: map[string]any{"ip": h.Host, "middlewareType": h.Type, "promUp": up, "cmdbState": int(state)},
				})
			}
		}
	}

	// 3) C3 反向孤儿：CMDB 在线服务器是否被 topo 引用（抽样）
	//    hostFilter 白名单模式下跳过——C3 是全量盘点语义，单 host 稽核不适用
	if len(hostFilter) > 0 {
		add(domain.AuditFinding{
			CheckCode: "AUDIT_C3", ObjectType: "snapshot", ObjectKey: date,
			Severity: "P3", Verdict: domain.AuditVerdictNotApplicable,
			Detail: "本次按 host 白名单定向稽核，未执行反向孤儿全量盘点",
		})
	} else if recs, _, err := e.cmdbQuery(ctx, map[string]any{"state": "0", "currentPage": 1, "pageSize": 20}); err == nil {
		orphans := 0
		for _, rec := range recs {
			ip := firstIP(asString(rec["ip"]))
			if ip == "" {
				continue
			}
			if _, ok := referenced[ip]; !ok {
				orphans++
				if orphans <= 10 {
					add(domain.AuditFinding{
						CheckCode: "AUDIT_C3", ObjectType: "server", ObjectKey: ip,
						Severity: "P3", Verdict: domain.AuditVerdictRisk,
						Detail: "CMDB 在线服务器未出现在拓扑依赖关系中，可能为待纳管/闲置资产",
						Evidence: map[string]any{
							"ip": ip, "hostName": rec["hostName"], "node": rec["node"],
							"usingBy": rec["usingBy"],
						},
					})
				}
			}
		}
		if orphans == 0 {
			add(domain.AuditFinding{
				CheckCode: "AUDIT_C3", ObjectType: "snapshot", ObjectKey: date,
				Severity: "P3", Verdict: domain.AuditVerdictNotTriggered,
				Detail: "抽样的 CMDB 在线服务器均被拓扑关系引用",
			})
		}
	} else {
		add(domain.AuditFinding{
			CheckCode: "AUDIT_C3", ObjectType: "snapshot", ObjectKey: date,
			Severity: "P3", Verdict: domain.AuditVerdictNoData,
			Detail: "CMDB 在线服务器抽样失败: " + err.Error(),
		})
	}

	// 4) 统计落盘
	for _, f := range findings {
		_ = e.store.Put("audit_finding", f.ID, f)
		switch f.Verdict {
		case domain.AuditVerdictRisk:
			run.Stats.Risk++
		case domain.AuditVerdictNotTriggered:
			run.Stats.NotTriggered++
		case domain.AuditVerdictNoData:
			run.Stats.NoData++
		case domain.AuditVerdictNotApplicable:
			run.Stats.NotApplicable++
		}
	}
	run.Stats.Items = len(findings)
	run.Status = "succeeded"
	run.Message = fmt.Sprintf("稽核完成：对账 %d 个中间件主机，发现 %d 项风险", len(hosts), run.Stats.Risk)
	run.FinishedAt = time.Now()
	e.persist(run)
	return run, nil
}

// cmdbQuery 调用 cmdb-mcp-server 的 cmdb_server_query，解析 data.records。
// 统一走 mcp.CMDBRecords，兼容 envelope / 双重编码字符串。
func (e *Engine) cmdbQuery(ctx context.Context, args map[string]any) ([]map[string]any, []byte, error) {
	if e.mcp == nil || !e.mcp.Enabled() {
		return nil, nil, fmt.Errorf("上游服务未启用")
	}
	raw, err := e.mcp.Call(ctx, "cmdb", "cmdb_server_query", args)
	if err != nil {
		return nil, nil, err
	}
	page, err := mcp.CMDBRecords(raw)
	if err != nil {
		return nil, raw, err
	}
	return page.Records, raw, nil
}

// promUp 查询 Prometheus up{instance=~"host(:port)?"} 是否有心跳。
// host 先经身份映射归一，再按 PromQL 安全正则转义。
func (e *Engine) promUp(ctx context.Context, host string) (bool, error) {
	if e.mcp == nil || !e.mcp.Enabled() {
		return false, fmt.Errorf("上游服务未启用")
	}
	promql := fmt.Sprintf(`up{instance=~"%s"}`, identity.PromQLInstanceRegex(host))
	raw, err := e.mcp.Call(ctx, "prometheus", "query", map[string]any{"query": promql})
	if err != nil {
		return false, err
	}
	samples, err := mcp.PrometheusSamples(raw)
	if err != nil {
		return false, err
	}
	for _, s := range samples {
		if s.Value >= 1 {
			return true, nil
		}
	}
	return false, nil
}

func collectHosts(rows []domain.TopoRow) []hostRef {
	byHost := map[string]*hostRef{}
	for _, r := range rows {
		host := hostOf(r.ConnectAddr)
		if host == "" || r.MiddlewareType == "" {
			continue
		}
		h := byHost[host]
		if h == nil {
			h = &hostRef{Host: host, Type: r.MiddlewareType, Apps: map[string]struct{}{}}
			byHost[host] = h
		}
		if r.ApolloAppID != "" {
			h.Apps[r.ApolloAppID] = struct{}{}
		}
	}
	out := make([]hostRef, 0, len(byHost))
	for _, h := range byHost {
		out = append(out, *h)
	}
	return out
}

// matchRecord 按身份映射别名在 CMDB 记录中命中目标 host。
func (e *Engine) matchRecord(recs []map[string]any, host string) map[string]any {
	return e.reg.MatchRecord(recs, host)
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

func normHost(h string) string { return strings.ToLower(strings.TrimSpace(h)) }

func firstIP(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ","); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func sanitize(k string) string {
	k = strings.NewReplacer("/", "_", "|", "_", ":", "_", " ", "_", ".", "_").Replace(k)
	if len(k) > 60 {
		k = k[:60]
	}
	return strings.ToLower(k)
}

func (e *Engine) persist(run *domain.AuditRun) {
	_ = e.store.Put("audit_run", run.ID, run)
}

// ListRun 最近批次。
func (e *Engine) ListRun(limit int) []domain.AuditRun {
	out := store.ListInto[domain.AuditRun](e.store, "audit_run")
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// GetRun 批次详情。
func (e *Engine) GetRun(id string) (*domain.AuditRun, bool) {
	var r domain.AuditRun
	if store.GetInto(e.store, "audit_run", id, &r) {
		return &r, true
	}
	return nil, false
}

// ListFindings 批次发现。
func (e *Engine) ListFindings(runID string) []domain.AuditFinding {
	all := store.ListInto[domain.AuditFinding](e.store, "audit_finding")
	out := make([]domain.AuditFinding, 0)
	for _, f := range all {
		if f.RunID == runID {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Verdict != out[j].Verdict {
			return out[i].Verdict == domain.AuditVerdictRisk
		}
		return out[i].ObjectKey < out[j].ObjectKey
	})
	return out
}
