package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/mcp"
)

// T10 日志错误（Elasticsearch）。
//
// 数据源为 elasticsearch-mcp（search / list_indices）。批量巡检按主机逐台统计
// 近 N 分钟 ERROR/FATAL 日志条数；索引不存在、查询失败一律 NO_DATA，不能当作健康。

var esTotalHitsRe = regexp.MustCompile(`Total results:\s*(\d+)`)

func (e *Engine) ruleLogErrors(ctx context.Context, hosts []scopeHost, cache *hostMCPCache, params map[string]any) []domain.Finding {
	const code, title = "TOPO_T10", "日志错误（ES）"
	if !cache.mcpReady() {
		return []domain.Finding{mcpNoData(code, title, "上游服务未启用（INSPECT_MCP_ENABLED / MCP_ENABLED=false），无法查询 ES 日志")}
	}
	if strings.TrimSpace(e.cfg.ESMCPURL) == "" {
		return []domain.Finding{mcpNoData(code, title, "未配置 ES 上游服务地址（ES_MCP_URL），ES 日志巡检未接入")}
	}
	if len(hosts) == 0 {
		return []domain.Finding{mcpNoData(code, title, "巡检范围内没有可识别的中间件主机地址")}
	}

	lookback := paramInt(params, "lookbackMin", e.cfg.InspectESLookbackMin)
	if lookback <= 0 {
		lookback = 15
	}
	threshold := paramInt(params, "errorThreshold", e.cfg.InspectESErrorThreshold)
	if threshold <= 0 {
		threshold = 20
	}

	// 先确认索引存在，避免逐台主机重复得到 "index_not_found" 噪音。
	idxRaw, err := e.callES(ctx, "list_indices", map[string]any{"index_pattern": e.cfg.ESLogIndex})
	if err != nil {
		return []domain.Finding{mcpNoData(code, title, "ES 索引列表查询失败: "+err.Error())}
	}
	if len(esIndexNames(idxRaw)) == 0 {
		return []domain.Finding{mcpNoData(code, title, fmt.Sprintf("ES 中不存在匹配 %s 的日志索引", e.cfg.ESLogIndex))}
	}

	out := make([]domain.Finding, 0, len(hosts))
	for _, h := range hosts {
		f := domain.Finding{
			RuleCode:   code,
			Title:      title,
			ObjectType: "host",
			ObjectKey:  h.Host,
			Severity:   "P2",
		}
		evidence := map[string]any{
			"host": h.Host, "index": e.cfg.ESLogIndex, "lookbackMin": lookback,
			"errorThreshold": threshold, "depApps": len(h.Apps),
		}
		raw, err := e.callES(ctx, "search", map[string]any{
			"index":      e.cfg.ESLogIndex,
			"query_body": e.esErrorQuery(h.Host, lookback),
		})
		if err != nil {
			f.Verdict = VerdictNoData
			f.Detail = "ES 日志查询失败，无法判断: " + err.Error()
			f.Evidence = evidence
			out = append(out, f)
			continue
		}
		total, err := esTotalHits(raw)
		if err != nil {
			f.Verdict = VerdictNoData
			f.Detail = "ES 日志结果无法解析: " + err.Error()
			f.Evidence = evidence
			out = append(out, f)
			continue
		}
		evidence["errorLogs"] = total
		if total > threshold {
			if total >= threshold*5 {
				f.Severity = "P1"
			}
			f.Verdict = VerdictRisk
			f.Detail = fmt.Sprintf("近 %d 分钟主机 %s 出现 %d 条 ERROR/FATAL 日志，超过阈值 %d；被 %d 个应用依赖",
				lookback, h.Host, total, threshold, len(h.Apps))
			f.Impact = map[string]any{"host": h.Host, "errorLogs": total, "depApps": len(h.Apps)}
		} else {
			f.Verdict = VerdictNotTriggered
			f.Detail = fmt.Sprintf("近 %d 分钟主机 %s 有 %d 条 ERROR/FATAL 日志，未超过阈值 %d", lookback, h.Host, total, threshold)
		}
		f.Evidence = evidence
		out = append(out, f)
	}
	return out
}

// callES 调用 ES MCP 工具（单次超时，共享批次预算）。
func (e *Engine) callES(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	cctx, cancel := context.WithTimeout(ctx, e.mcpTimeout)
	defer cancel()
	return e.mcp.Call(cctx, "es", tool, args)
}

// esErrorQuery 构造按主机统计 ERROR/FATAL 日志条数的 Query DSL（size=0，只看 total）。
func (e *Engine) esErrorQuery(host string, lookbackMin int) map[string]any {
	timeField := e.cfg.ESLogTimeField
	if timeField == "" {
		timeField = "@timestamp"
	}
	levelField := e.cfg.ESLogLevelField
	if levelField == "" {
		levelField = "level"
	}
	hostField := e.cfg.ESLogHostField
	if hostField == "" {
		hostField = "host"
	}
	return map[string]any{
		"size":             0,
		"track_total_hits": true,
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []any{
					map[string]any{"range": map[string]any{
						timeField: map[string]any{"gte": fmt.Sprintf("now-%dm", lookbackMin)},
					}},
				},
				"must": []any{
					map[string]any{"query_string": map[string]any{
						"query":   fmt.Sprintf(`%s:(ERROR OR FATAL OR error OR fatal)`, levelField),
						"lenient": true,
					}},
					map[string]any{"query_string": map[string]any{
						"query":   fmt.Sprintf(`%s:%s`, hostField, luceneQuote(host)),
						"lenient": true,
					}},
				},
			},
		},
	}
}

func luceneQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// esIndexNames 解析 list_indices 返回：JSON 数组（mock / ES MCP）或 "Found N indices:" 文案。
func esIndexNames(raw json.RawMessage) []string {
	payload, ok := mcp.ContentJSON(raw)
	if !ok {
		return nil
	}
	var list []map[string]any
	if err := json.Unmarshal(payload, &list); err == nil {
		out := make([]string, 0, len(list))
		for _, item := range list {
			if name := firstStringOf(item, "index", "index_name", "name"); name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	var single map[string]any
	if err := json.Unmarshal(payload, &single); err == nil {
		if name := firstStringOf(single, "index", "index_name", "name"); name != "" {
			return []string{name}
		}
	}
	return nil
}

// esTotalHits 解析 search 的命中总数：优先 "Total results: N" 文案，其次 hits.total。
func esTotalHits(raw json.RawMessage) (int, error) {
	res := mcp.ResultOf(raw)
	if res.IsError {
		msg := strings.TrimSpace(res.Text)
		if msg == "" {
			msg = "ES 返回 isError"
		}
		return 0, fmt.Errorf("%s", msg)
	}
	if m := esTotalHitsRe.FindStringSubmatch(res.Text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n, nil
		}
	}
	payload, ok := mcp.ContentJSON(raw)
	if !ok {
		return 0, fmt.Errorf("ES 返回中没有命中数")
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return 0, err
	}
	hits, _ := body["hits"].(map[string]any)
	switch total := hits["total"].(type) {
	case float64:
		return int(total), nil
	case map[string]any:
		if v, ok := total["value"].(float64); ok {
			return int(v), nil
		}
	}
	return 0, fmt.Errorf("ES 返回中没有 hits.total")
}

func firstStringOf(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
