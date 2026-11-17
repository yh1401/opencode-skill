package inspect

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/mcp"
)

// T11 主机指标阈值（Prometheus host_metrics_realtime）。
//
// 每台主机一次调用拿到 CPU/内存/磁盘/连接数，按规则参数阈值判定；
// 无指标数据或调用失败判 NO_DATA。指标阈值是“容量类”风险，默认 P2，
// 使用率 ≥95% 或连接数 ≥2 倍阈值时升为 P1。

type metricCheck struct {
	Key       string
	Label     string
	Unit      string
	Threshold float64
}

func (e *Engine) ruleMetricThreshold(ctx context.Context, hosts []scopeHost, cache *hostMCPCache, params map[string]any) []domain.Finding {
	const code, title = "TOPO_T11", "主机指标阈值"
	if !cache.mcpReady() {
		return []domain.Finding{mcpNoData(code, title, "上游服务未启用（INSPECT_MCP_ENABLED / MCP_ENABLED=false），无法查询 Prometheus 主机指标")}
	}
	if len(hosts) == 0 {
		return []domain.Finding{mcpNoData(code, title, "巡检范围内没有可识别的中间件主机地址")}
	}

	checks := []metricCheck{
		{Key: "cpuUsage", Label: "CPU 使用率", Unit: "%", Threshold: paramFloat(params, "cpuThreshold", 85)},
		{Key: "memUsage", Label: "内存使用率", Unit: "%", Threshold: paramFloat(params, "memThreshold", 85)},
		{Key: "diskUsage", Label: "磁盘使用率", Unit: "%", Threshold: paramFloat(params, "diskThreshold", 90)},
		{Key: "netConnections", Label: "TCP 连接数", Unit: "个", Threshold: paramFloat(params, "connThreshold", 5000)},
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
		evidence := map[string]any{"host": h.Host, "depApps": len(h.Apps), "middlewareTypes": sortedKeys(h.Types)}
		raw, err := cache.hostMetrics(ctx, h)
		if err != nil {
			f.Verdict = VerdictNoData
			f.Detail = "Prometheus 主机指标查询失败，无法判断: " + err.Error()
			f.Evidence = evidence
			out = append(out, f)
			continue
		}
		if len(raw.Metrics) == 0 {
			f.Verdict = VerdictNoData
			f.Detail = "Prometheus 未返回该主机的指标数据（可能未纳管 node_exporter 或实例标签不一致）"
			f.Evidence = evidence
			out = append(out, f)
			continue
		}

		values := map[string]float64{}
		for _, c := range checks {
			if v, ok := raw.Metrics[c.Key]; ok {
				values[c.Key] = v
			}
		}
		evidence["metrics"] = values
		thresholds := map[string]float64{}
		for _, c := range checks {
			thresholds[c.Key] = c.Threshold
		}
		evidence["thresholds"] = thresholds
		if len(values) == 0 {
			f.Verdict = VerdictNoData
			f.Detail = "Prometheus 指标中缺少 cpuUsage / memUsage / diskUsage / netConnections 字段"
			f.Evidence = evidence
			out = append(out, f)
			continue
		}

		hits := make([]metricCheck, 0, len(checks))
		for _, c := range checks {
			v, ok := values[c.Key]
			if ok && v > c.Threshold {
				hits = append(hits, c)
			}
		}
		if len(hits) == 0 {
			f.Verdict = VerdictNotTriggered
			f.Detail = fmt.Sprintf("主机指标均在阈值内（CPU %s%% / 内存 %s%% / 磁盘 %s%% / 连接 %s）",
				formatMetric(values["cpuUsage"]), formatMetric(values["memUsage"]),
				formatMetric(values["diskUsage"]), formatMetric(values["netConnections"]))
			f.Evidence = evidence
			out = append(out, f)
			continue
		}

		parts := make([]string, 0, len(hits))
		p1 := false
		for _, c := range hits {
			v := values[c.Key]
			parts = append(parts, fmt.Sprintf("%s %s%s > %s%s", c.Label, formatMetric(v), c.Unit, formatMetric(c.Threshold), c.Unit))
			if c.Key == "netConnections" && v >= c.Threshold*2 {
				p1 = true
			}
			if c.Key != "netConnections" && v >= 95 {
				p1 = true
			}
		}
		if p1 {
			f.Severity = "P1"
		}
		sort.Strings(parts)
		f.Verdict = VerdictRisk
		f.Detail = fmt.Sprintf("%d 项指标超阈值：%s；被 %d 个应用依赖", len(hits), strings.Join(parts, "；"), len(h.Apps))
		f.Evidence = evidence
		f.Impact = map[string]any{"host": h.Host, "depApps": len(h.Apps), "metrics": values}
		out = append(out, f)
	}
	return out
}

func formatMetric(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.1f", v)
}

// hostMetrics 查询单台主机的实时指标；命中批次缓存直接返回。
func (c *hostMCPCache) hostMetrics(ctx context.Context, h scopeHost) (mcp.PromHostMetrics, error) {
	if r, ok := c.metrics[h.Key]; ok {
		return r.metrics, r.err
	}
	cctx, cancel := context.WithTimeout(ctx, c.e.mcpTimeout)
	raw, err := c.e.mcp.Call(cctx, "prometheus", "host_metrics_realtime", map[string]any{"node": h.Host})
	cancel()
	var out mcp.PromHostMetrics
	if err == nil {
		out, err = mcp.PromHostMetricsOf(raw)
	}
	c.metrics[h.Key] = hostMetricsResult{metrics: out, err: err}
	return out, err
}
