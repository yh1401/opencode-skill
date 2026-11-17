package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/mcp"
)

// T8 调用链异常（Pinpoint）。
//
// 批量巡检没有 traceId，无法逐条拉取调用链明细；这里用 Pinpoint 服务地图的
// linkData（from → to 的调用量与错误量）做聚合判断：错误数或平均耗时超阈值的
// 上下游链路判 RISK。单条 trace 的 exception 仍由排障会话按 traceId 取证。

// scopeApp 巡检范围内的应用（按 appId 去重）。
type scopeApp struct {
	ID    string
	Name  string
	Env   string
	Hosts map[string]struct{}
}

// collectScopeApps 从拓扑行抽取应用，按依赖主机数降序并限制批量上限。
func (e *Engine) collectScopeApps(rows []domain.TopoRow) []scopeApp {
	byID := map[string]*scopeApp{}
	for _, r := range rows {
		id := strings.TrimSpace(r.ApolloAppID)
		if id == "" {
			continue
		}
		a := byID[id]
		if a == nil {
			a = &scopeApp{ID: id, Name: strings.TrimSpace(r.ApolloAppName), Env: r.Env, Hosts: map[string]struct{}{}}
			byID[id] = a
		}
		if h := strings.TrimSpace(r.ConnectAddr); h != "" {
			a.Hosts[h] = struct{}{}
		}
	}
	out := make([]scopeApp, 0, len(byID))
	for _, a := range byID {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Hosts) != len(out[j].Hosts) {
			return len(out[i].Hosts) > len(out[j].Hosts)
		}
		return out[i].ID < out[j].ID
	})
	max := e.cfg.InspectMCPMaxApps
	if max <= 0 {
		max = 10
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func (e *Engine) ruleTrace(ctx context.Context, rows []domain.TopoRow, params map[string]any) []domain.Finding {
	const code, title = "TOPO_T8", "调用链异常（Pinpoint）"
	if !e.cfg.InspectMCPEnabled || e.mcp == nil || !e.mcp.Enabled() {
		return []domain.Finding{mcpNoData(code, title, "上游服务未启用（INSPECT_MCP_ENABLED / MCP_ENABLED=false），无法读取 Pinpoint 调用链")}
	}
	apps := e.collectScopeApps(rows)
	if len(apps) == 0 {
		return []domain.Finding{mcpNoData(code, title, "巡检范围内没有可识别的应用（Apollo appId）")}
	}

	errThreshold := paramInt(params, "errorCountThreshold", 20)
	latencyThreshold := paramInt(params, "latencyThresholdMs", 1000)

	out := make([]domain.Finding, 0, len(apps))
	for _, a := range apps {
		f := domain.Finding{
			RuleCode:   code,
			Title:      title,
			ObjectType: "application",
			ObjectKey:  a.ID,
			Severity:   "P2",
		}
		raw, err := e.pinpointServerMap(ctx, a)
		if err != nil {
			f.Verdict = VerdictNoData
			f.Detail = "Pinpoint 调用链查询失败，无法判断: " + err.Error()
			f.Evidence = map[string]any{"appId": a.ID, "appName": a.Name, "depHosts": len(a.Hosts)}
			out = append(out, f)
			continue
		}
		sm, err := mcp.PinpointServerMapOf(raw)
		if err != nil {
			f.Verdict = VerdictNoData
			f.Detail = "Pinpoint 调用链解析失败: " + err.Error()
			f.Evidence = map[string]any{"appId": a.ID, "appName": a.Name}
			out = append(out, f)
			continue
		}
		evidence := map[string]any{
			"appId": a.ID, "appName": a.Name, "depHosts": len(a.Hosts),
			"nodes": len(sm.Nodes), "middlewares": len(sm.Middlewares), "links": len(sm.Links),
			"errorCountThreshold": errThreshold, "latencyThresholdMs": latencyThreshold,
		}
		if len(sm.Nodes) == 0 && len(sm.Links) == 0 {
			f.Verdict = VerdictNoData
			f.Detail = "Pinpoint 未找到该应用的节点与调用链数据（可能未接入 Pinpoint Agent 或应用名不一致）"
			f.Evidence = evidence
			out = append(out, f)
			continue
		}

		errLinks := make([]mcp.PinpointLink, 0)
		slowLinks := make([]mcp.PinpointLink, 0)
		for _, l := range sm.Links {
			if l.ErrorCount >= errThreshold {
				errLinks = append(errLinks, l)
			}
			if l.ResponseTime >= latencyThreshold {
				slowLinks = append(slowLinks, l)
			}
		}
		if len(errLinks) == 0 && len(slowLinks) == 0 {
			f.Verdict = VerdictNotTriggered
			f.Detail = fmt.Sprintf("Pinpoint 返回 %d 条调用链，无错误数 ≥%d 或耗时 ≥%dms 的链路", len(sm.Links), errThreshold, latencyThreshold)
			f.Evidence = evidence
			out = append(out, f)
			continue
		}

		sort.Slice(errLinks, func(i, j int) bool {
			if errLinks[i].ErrorCount != errLinks[j].ErrorCount {
				return errLinks[i].ErrorCount > errLinks[j].ErrorCount
			}
			return errLinks[i].Source < errLinks[j].Source
		})
		topErr := 0
		if len(errLinks) > 0 {
			topErr = errLinks[0].ErrorCount
		}
		if topErr >= errThreshold*5 {
			f.Severity = "P1"
		}
		f.Verdict = VerdictRisk
		f.Detail = fmt.Sprintf("调用链异常：%d 条链路的错误数 ≥%d，%d 条链路平均耗时 ≥%dms",
			len(errLinks), errThreshold, len(slowLinks), latencyThreshold)
		evidence["errorLinks"] = len(errLinks)
		evidence["slowLinks"] = len(slowLinks)
		evidence["topLinks"] = topLinks(errLinks, 5)
		f.Evidence = evidence
		f.Impact = map[string]any{"appId": a.ID, "appName": a.Name, "depHosts": len(a.Hosts), "errorLinks": len(errLinks)}
		out = append(out, f)
	}
	return out
}

// pinpointServerMap 先用 apolloAppName 查询（Pinpoint 应用名），无数据再回退 appId，最多 2 次调用。
func (e *Engine) pinpointServerMap(ctx context.Context, a scopeApp) (json.RawMessage, error) {
	names := []string{a.Name, a.ID}
	var lastErr error
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		cctx, cancel := context.WithTimeout(ctx, e.mcpTimeout)
		to := time.Now()
		args := map[string]any{
			"application_name": name,
			"from_ts":          to.Add(-5 * time.Minute).UnixMilli(),
			"to_ts":            to.UnixMilli(),
			"caller_range":     1,
			"callee_range":     1,
			"bidirectional":    true,
		}
		raw, err := e.mcp.Call(cctx, "pinpoint", "get_server_map_tool", args)
		cancel()
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if sm, err := mcp.PinpointServerMapOf(raw); err == nil && (len(sm.Nodes) > 0 || len(sm.Links) > 0) {
			return raw, nil
		}
		lastErr = fmt.Errorf("Pinpoint 微服务 %s 无调用链数据", name)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("应用缺少可用的 Pinpoint 标识")
	}
	return nil, lastErr
}

func topLinks(links []mcp.PinpointLink, max int) []map[string]any {
	if len(links) > max {
		links = links[:max]
	}
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		out = append(out, map[string]any{
			"from": l.Source, "to": l.Target,
			"count": l.Count, "errorCount": l.ErrorCount, "responseTime": l.ResponseTime,
		})
	}
	return out
}
