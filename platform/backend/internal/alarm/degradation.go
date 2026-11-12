package alarm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/mcp"
)

// DegradationResult 一轮指标劣化扫描的统计结果。
type DegradationResult struct {
	Scanned      int       `json:"scanned"`
	Degraded     []string  `json:"degraded"`
	Created      int       `json:"created"`
	Resolved     int       `json:"resolved"`
	AutoDiag     int       `json:"autoDiag"`
	WindowMin    int       `json:"windowMin"`
	ThresholdPct float64   `json:"thresholdPct"`
	At           time.Time `json:"at"`
	Error        string    `json:"error,omitempty"`
}

// 最近一轮扫描结果落库，供劣化独立页展示。
const (
	degradationKind = "degradation_scan"
	degradationKey  = "latest"
)

// degTarget 一个待扫描的监控目标（按 host 去重）。
type degTarget struct {
	Host       string
	Instance   string
	Middleware string
}

// StartDegradation 启动后台劣化扫描循环。阻塞直到 ctx cancel。
func (e *Engine) StartDegradation(ctx context.Context) {
	if !e.cfg.AlarmEnabled || !e.cfg.DegradationEnabled {
		return
	}
	interval := e.cfg.DegradationInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.SweepDegradation(ctx)
		}
	}
}

// SweepDegradation 扫描 topo 目标，用 range_query 算趋势，命中的写入 degradation 告警。
// 与 Prometheus 告警共用 alarm_event 表与自动排障准入，但生命周期独立维护。
func (e *Engine) SweepDegradation(ctx context.Context) DegradationResult {
	res := DegradationResult{
		Scanned:      0,
		Degraded:     []string{},
		WindowMin:    e.cfg.DegradationWindowMin,
		ThresholdPct: e.cfg.DegradationThresholdPct,
		At:           time.Now(),
	}
	defer e.saveDegradationScan(&res)
	if e.gw == nil || !e.gw.Enabled() {
		res.Error = "统一接入网关未启用"
		return res
	}
	targets := e.degradationTargets()
	res.Scanned = len(targets)
	if len(targets) == 0 {
		res.Error = "topo 快照中没有可扫描的中间件地址"
		return res
	}

	now := time.Now()
	end := now
	start := now.Add(-time.Duration(e.cfg.DegradationWindowMin) * time.Minute)
	step := fmt.Sprintf("%ds", e.cfg.DegradationStepSec)
	if e.cfg.DegradationStepSec <= 0 {
		step = "300s"
	}

	seen := map[string]bool{}
	var firing []AlarmEvent
	for _, t := range targets {
		// 主机名按 PromQL 安全正则转义，避免 IP / 点号 / 特殊字符破坏匹配。
		query := strings.ReplaceAll(e.cfg.DegradationQuery, "{host}", identity.PromQLLiteral(t.Host))
		if strings.TrimSpace(query) == "" {
			continue
		}
		args := map[string]any{
			"query":      query,
			"start_time": start.Format(time.RFC3339),
			"end_time":   end.Format(time.RFC3339),
			"step":       step,
		}
		raw, err := e.gw.CallSilent(ctx, "prometheus", "range_query", args)
		if err != nil {
			continue
		}
		metric, values := parseMatrix(raw)
		if len(values) < 4 {
			continue
		}
		growth := growthPct(values)
		if growth < e.cfg.DegradationThresholdPct {
			continue
		}

		name := metric["__name__"]
		if name == "" {
			name = "metric"
		}
		labelKey := name
		if inst := metric["instance"]; inst != "" {
			labelKey = name + "@" + inst
		}
		fp := "degradation|" + t.Host + "|" + labelKey
		seen[fp] = true
		res.Degraded = append(res.Degraded, fmt.Sprintf("%s %s 上升 %.1f%%", t.Host, name, growth))

		ev := e.upsertDegradation(fp, t, name, metric, growth, values[len(values)-1], now)
		firing = append(firing, ev)
	}

	// 恢复：本轮未命中的 degradation 事件，连续 miss 超阈值后置为 resolved
	for _, ev := range e.List("firing") {
		if ev.Source != "degradation" || seen[ev.Fingerprint] {
			continue
		}
		ev.MissCount++
		if ev.MissCount >= e.cfg.AlarmMissTolerance {
			t := now
			ev.State = "resolved"
			ev.ResolvedAt = &t
			res.Resolved++
			if tk, ok := e.tickets.Get(ev.TicketID); ok && tk != nil {
				tk.Findings = append(tk.Findings, fmt.Sprintf("劣化告警已恢复（%s），建议人工确认处置", now.Format("2006-01-02 15:04:05")))
				_ = e.st.Put("ticket", tk.ID, tk)
			}
		}
		_ = e.st.Put(Kind, ev.Fingerprint, ev)
	}

	if len(firing) > 0 {
		res.AutoDiag = e.autoDiagnoseEvents(ctx, firing)
	}
	res.Created = len(firing)
	sort.Strings(res.Degraded)
	return res
}

// saveDegradationScan 保存最近一轮扫描结果。
func (e *Engine) saveDegradationScan(res *DegradationResult) {
	if res == nil {
		return
	}
	if res.At.IsZero() {
		res.At = time.Now()
	}
	_ = e.st.Put(degradationKind, degradationKey, *res)
}

// LastDegradationScan 最近一轮扫描结果，nil 表示尚未扫描。
func (e *Engine) LastDegradationScan() *DegradationResult {
	b, ok := e.st.Get(degradationKind, degradationKey)
	if !ok {
		return nil
	}
	var res DegradationResult
	if err := json.Unmarshal(b, &res); err != nil {
		return nil
	}
	return &res
}

// upsertDegradation 写入或刷新一条 degradation 事件。
func (e *Engine) upsertDegradation(fp string, t degTarget, metricName string, metric map[string]string, growth, last float64, now time.Time) AlarmEvent {
	var ev AlarmEvent
	if b, ok := e.st.Get(Kind, fp); ok {
		_ = json.Unmarshal(b, &ev)
	}
	summary := fmt.Sprintf("指标劣化：%s 近 %d 分钟上升 %.1f%%（当前 %.3f）",
		metricName, e.cfg.DegradationWindowMin, growth, last)
	labels := map[string]string{
		"source":         "degradation",
		"metric":         metricName,
		"middlewareType": t.Middleware,
	}
	if t.Middleware != "" {
		labels["type"] = t.Middleware
	}
	for k, v := range metric {
		if k != "__name__" {
			labels[k] = v
		}
	}

	if ev.Fingerprint == "" {
		ev = AlarmEvent{
			Fingerprint: fp,
			Source:      "degradation",
			AlertName:   "MetricDegradation",
			Instance:    t.Instance,
			Host:        t.Host,
			Severity:    "warning",
			Labels:      labels,
			Summary:     summary,
			State:       "firing",
			FirstSeenAt: now,
			LastSeenAt:  now,
			FireCount:   1,
		}
	} else {
		ev.Source = "degradation"
		ev.State = "firing"
		ev.LastSeenAt = now
		ev.MissCount = 0
		ev.ResolvedAt = nil
		ev.Severity = "warning"
		ev.Labels = labels
		ev.Summary = summary
	}
	_ = e.st.Put(Kind, fp, ev)
	return ev
}

// degradationTargets 从最新 topo 快照取中间件地址，按 host 去重并限量。
func (e *Engine) degradationTargets() []degTarget {
	if e.topo == nil {
		return nil
	}
	date := ""
	if ds := e.topo.Dates(); len(ds) > 0 {
		date = ds[len(ds)-1]
	}
	rows, err := e.topo.Rows(date, e.cfg.DefaultEnv)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]degTarget, 0, len(rows))
	for _, r := range rows {
		addr := strings.TrimSpace(r.ConnectAddr)
		if addr == "" {
			continue
		}
		host := hostOf(addr)
		if host == "" || seen[strings.ToLower(host)] {
			continue
		}
		seen[strings.ToLower(host)] = true
		out = append(out, degTarget{Host: host, Instance: addr, Middleware: r.MiddlewareType})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	if max := e.cfg.DegradationMaxTargets; max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// parseMatrix 从 range_query 响应里解出第一条时间序列（metric + 值数组）。
// 统一走 mcp.PrometheusMatrix，兼容 envelope / 业务 JSON / 双重编码字符串。
func parseMatrix(raw json.RawMessage) (map[string]string, []float64) {
	m, err := mcp.PrometheusMatrix(raw)
	if err != nil {
		return nil, nil
	}
	return m.Metric, m.Values
}

// growthPct 用首尾三分之一的均值对比计算涨幅（%）。
func growthPct(values []float64) float64 {
	n := len(values)
	if n < 4 {
		return 0
	}
	k := n / 3
	if k < 1 {
		k = 1
	}
	first := mean(values[:k])
	last := mean(values[n-k:])
	if math.Abs(first) < 1e-6 {
		if last > 0 {
			return 100
		}
		return 0
	}
	return (last - first) / math.Abs(first) * 100
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}
