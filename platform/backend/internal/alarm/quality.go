package alarm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// T13 质差分析：区间指标趋势打分 + 拓扑影响面根因归因。
const (
	qualityReportKind   = "quality_report"
	qualityLatestPrefix = "quality_latest_"
	qualityScanKind     = "quality_scan"
	qualityScanKey      = "latest"
	qualitySource       = "quality"
)

// QualityMetric 单个质量指标的前后段对比。
type QualityMetric struct {
	Name      string  `json:"name"`
	Baseline  float64 `json:"baseline"`
	Recent    float64 `json:"recent"`
	GrowthPct float64 `json:"growthPct"`
	Points    int     `json:"points"`
}

// QualityRootCause 质差根因候选。
type QualityRootCause struct {
	Type   string  `json:"type"` // database|cache|mq|gateway|application
	Target string  `json:"target"`
	Addr   string  `json:"addr"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// QualityReport 一次质差分析报告（store kind=quality_report，key=uuid）。
type QualityReport struct {
	ID               string            `json:"id"`
	AppID            string            `json:"appId"`
	At               time.Time         `json:"at"`
	WindowMin        int               `json:"windowMin"`
	Level            string            `json:"level"` // normal | suspected | degraded
	Score            float64           `json:"score"`
	Metrics          []QualityMetric   `json:"metrics"`
	RootCause        *QualityRootCause `json:"rootCause"`
	Evidence         []string          `json:"evidence"`
	AlarmFingerprint string            `json:"alarmFingerprint,omitempty"`
}

// QualityScanResult 质差扫描概要。
type QualityScanResult struct {
	At       time.Time `json:"at"`
	AppID    string    `json:"appId"`
	ReportID string    `json:"reportId"`
	Level    string    `json:"level"`
	Score    float64   `json:"score"`
	Breaches int       `json:"breaches"`
}

// QualityOverview 质差页总览。
type QualityOverview struct {
	Latest  *QualityReport  `json:"latest"`
	History []QualityReport `json:"history"`
	Firing  []AlarmEvent    `json:"firing"`
}

type qualityScanSummary struct {
	At    time.Time           `json:"at"`
	Items []QualityScanResult `json:"items"`
}

// AnalyzeQuality 对指定应用做窗口区间质差分析，并维护 quality 告警生命周期。
func (e *Engine) AnalyzeQuality(ctx context.Context, appID string, windowMin int) (*QualityReport, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, fmt.Errorf("appId 不能为空")
	}
	if e.gw == nil || !e.gw.Enabled() {
		return nil, fmt.Errorf("统一接入网关未启用")
	}
	if windowMin <= 0 {
		windowMin = e.cfg.QualityWindowMin
	}
	if windowMin <= 0 {
		windowMin = 60
	}

	now := time.Now()
	end := now
	start := now.Add(-time.Duration(windowMin) * time.Minute)
	stepSec := 300
	step := fmt.Sprintf("%ds", stepSec)
	window := fmt.Sprintf("%ds", stepSec)

	report := &QualityReport{
		ID:        uuid.NewString(),
		AppID:     appID,
		At:        now,
		WindowMin: windowMin,
		Metrics:   make([]QualityMetric, 0, 3),
		Evidence:  make([]string, 0, 8),
	}

	type series struct {
		metric QualityMetric
		growth float64
	}
	results := make([]series, 0, 3)
	for _, metricName := range []string{"error_rate", "tp99", "request_total"} {
		q := bizTemplate(metricName, appID, window)
		raw, err := e.gw.CallSilent(ctx, "prometheus", "range_query", map[string]any{
			"query":      q,
			"start_time": start.Format(time.RFC3339),
			"end_time":   end.Format(time.RFC3339),
			"step":       step,
		})
		if err != nil {
			report.Evidence = append(report.Evidence, fmt.Sprintf("%s 查询失败：%v", metricName, err))
			continue
		}
		_, values := parseMatrix(raw)
		if len(values) < 3 {
			report.Evidence = append(report.Evidence, fmt.Sprintf("%s 数据点不足（%d）", metricName, len(values)))
			continue
		}
		k := len(values) / 3
		if k < 1 {
			k = 1
		}
		baseline := mean(values[:k])
		recent := mean(values[len(values)-k:])
		growth := growthBetween(baseline, recent)
		m := QualityMetric{Name: metricName, Baseline: baseline, Recent: recent, GrowthPct: growth, Points: len(values)}
		report.Metrics = append(report.Metrics, m)
		results = append(results, series{metric: m, growth: growth})
		report.Evidence = append(report.Evidence, fmt.Sprintf("%s：基线 %.4f → 近期 %.4f（%+.1f%%）", metricName, baseline, recent, growth))
	}

	impact := e.qualityImpact(appID)
	hasMw := impact != nil && len(impact.Middlewares) > 0

	// 打分：错误率 0.35 / TP99 0.35 / 请求量下跌 0.2 / 中间件耦合 0.1
	var errScore, tpScore, dropScore, mwScore float64
	for _, s := range results {
		switch s.metric.Name {
		case "error_rate":
			errScore = 0.35 * clampPct(s.growth, 50)
		case "tp99":
			tpScore = 0.35 * clampPct(s.growth, 30)
		case "request_total":
			dropScore = 0.2 * clampPct(-s.growth, 30)
		}
	}
	if hasMw {
		mwScore = 0.1
		report.Evidence = append(report.Evidence, fmt.Sprintf("影响面包含 %d 个下游中间件，存在级联风险", len(impact.Middlewares)))
	}
	report.Score = math.Min(1, errScore+tpScore+dropScore+mwScore)

	breaches := 0
	for _, v := range []float64{errScore, tpScore, dropScore, mwScore} {
		if v > 0 {
			breaches++
		}
	}

	switch {
	case report.Score >= 0.7:
		report.Level = "degraded"
	case report.Score >= 0.4:
		report.Level = "suspected"
	default:
		report.Level = "normal"
	}

	report.RootCause = pickRootCause(impact, errScore, tpScore, dropScore)
	if report.RootCause != nil {
		report.Evidence = append(report.Evidence, fmt.Sprintf("根因候选：%s %s（%.2f）%s",
			report.RootCause.Type, report.RootCause.Addr, report.RootCause.Score, report.RootCause.Reason))
	}

	_ = e.st.Put(qualityReportKind, report.ID, *report)
	_ = e.st.Put(qualityLatestPrefix+appID, "latest", *report)
	e.appendQualityScan(QualityScanResult{
		At: report.At, AppID: appID, ReportID: report.ID,
		Level: report.Level, Score: report.Score, Breaches: breaches,
	})

	// 告警：degraded 写 quality 事件并自动排障；其余情况维护恢复。
	if report.Level == "degraded" {
		severity := "warning"
		if report.Score >= 0.85 {
			severity = "critical"
		}
		fp := "quality|" + appID
		report.AlarmFingerprint = fp
		_ = e.st.Put(qualityReportKind, report.ID, *report)
		_, _, _ = e.IngestExternalEvents(ctx, qualitySource, []ExternalItem{{
			Fingerprint: fp,
			AlertName:   "AppQualityDegraded",
			Instance:    appID,
			Host:        appID,
			Severity:    severity,
			Summary:     fmt.Sprintf("应用质差：%s 评分 %.2f（%s）", appID, report.Score, report.Level),
			Labels: map[string]string{
				"appId":          appID,
				"level":          report.Level,
				"score":          fmt.Sprintf("%.2f", report.Score),
				"reportId":       report.ID,
				"source":         qualitySource,
				"middlewareType": "application",
			},
		}})
	}
	e.ResolveExternal(ctx, qualitySource, e.degradedQualityFingerprints())

	return report, nil
}

// ListQualityReports 按应用筛选报告，时间倒序。
func (e *Engine) ListQualityReports(appID string, limit int) []QualityReport {
	appID = strings.TrimSpace(appID)
	out := make([]QualityReport, 0)
	for _, r := range store.ListInto[QualityReport](e.st, qualityReportKind) {
		if appID != "" && r.AppID != appID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// GetQualityReport 按 ID 取报告。
func (e *Engine) GetQualityReport(id string) (*QualityReport, bool) {
	var r QualityReport
	if !store.GetInto(e.st, qualityReportKind, id, &r) {
		return nil, false
	}
	return &r, true
}

// QualityOverview 质差页总览。
func (e *Engine) QualityOverview(appID string) QualityOverview {
	appID = strings.TrimSpace(appID)
	out := QualityOverview{History: []QualityReport{}, Firing: []AlarmEvent{}}
	if appID != "" {
		var latest QualityReport
		if store.GetInto(e.st, qualityLatestPrefix+appID, "latest", &latest) {
			out.Latest = &latest
		}
	}
	history := e.ListQualityReports(appID, 20)
	out.History = history
	if out.Latest == nil && len(history) > 0 {
		out.Latest = &history[0]
	}
	for _, ev := range e.List("firing") {
		if ev.Source != qualitySource {
			continue
		}
		if appID != "" && ev.Labels["appId"] != appID {
			continue
		}
		out.Firing = append(out.Firing, ev)
	}
	return out
}

// appendQualityScan 把本轮概要追加到 quality_scan latest，仅保留最近 20 条。
func (e *Engine) appendQualityScan(item QualityScanResult) {
	sum := qualityScanSummary{At: item.At, Items: []QualityScanResult{}}
	if b, ok := e.st.Get(qualityScanKind, qualityScanKey); ok {
		_ = json.Unmarshal(b, &sum)
	}
	sum.At = item.At
	sum.Items = append(sum.Items, item)
	if len(sum.Items) > 20 {
		sum.Items = sum.Items[len(sum.Items)-20:]
	}
	_ = e.st.Put(qualityScanKind, qualityScanKey, sum)
}

// degradedQualityFingerprints 当前应保持 firing 的 quality 指纹集合（各应用最新报告为 degraded）。
func (e *Engine) degradedQualityFingerprints() map[string]bool {
	keep := map[string]bool{}
	for _, r := range e.ListQualityReports("", 0) {
		if r.Level != "degraded" {
			continue
		}
		// 只保留每个应用最新一条报告的状态
		if b, ok := e.st.Get(qualityLatestPrefix+r.AppID, "latest"); ok {
			var latest QualityReport
			if json.Unmarshal(b, &latest) == nil && latest.Level == "degraded" {
				keep["quality|"+r.AppID] = true
			}
		}
	}
	return keep
}

// qualityImpact 取应用影响面，失败时返回 nil。
func (e *Engine) qualityImpact(appID string) *domain.Impact {
	if e.topo == nil {
		return nil
	}
	date := ""
	if ds := e.topo.Dates(); len(ds) > 0 {
		date = ds[len(ds)-1]
	}
	im, err := e.topo.ImpactByApp(appID, appID, date, e.cfg.DefaultEnv, e.cfg.DefaultViewMode)
	if err != nil {
		return nil
	}
	return im
}

// pickRootCause 按 (类型匹配 0.6 + 影响应用数占比 0.4) 打分取 Top1。
func pickRootCause(im *domain.Impact, errScore, tpScore, dropScore float64) *QualityRootCause {
	if im == nil || len(im.Middlewares) == 0 {
		return nil
	}
	preferred := map[string]bool{}
	signal := "错误率上升"
	switch {
	case errScore >= tpScore && errScore >= dropScore && errScore > 0:
		preferred = map[string]bool{"database": true, "cache": true}
		signal = "错误率上升"
	case tpScore >= dropScore && tpScore > 0:
		preferred = map[string]bool{"mq": true, "gateway": true, "database": true}
		signal = "TP99 上升"
	case dropScore > 0:
		preferred = map[string]bool{"application": true, "gateway": true}
		signal = "请求量骤降"
	default:
		preferred = map[string]bool{"database": true, "cache": true}
	}

	total := im.TotalApps
	if total <= 0 {
		total = len(im.Apps)
	}
	var best *QualityRootCause
	for _, mw := range im.Middlewares {
		category := mwCategory(mw.Type)
		typeMatch := 0.0
		if preferred[category] {
			typeMatch = 1
		}
		ratio := 0.0
		if total > 0 {
			ratio = float64(mw.DepApps) / float64(total)
		}
		if ratio > 1 {
			ratio = 1
		}
		score := 0.6*typeMatch + 0.4*ratio
		if best != nil && score <= best.Score {
			continue
		}
		best = &QualityRootCause{
			Type:   category,
			Target: im.Target,
			Addr:   mw.Addr,
			Score:  math.Round(score*100) / 100,
			Reason: fmt.Sprintf("与%s特征匹配，影响应用占比 %.0f%%", signal, ratio*100),
		}
	}
	return best
}

// mwCategory 把拓扑中间件类型归并到根因类型。
func mwCategory(t string) string {
	s := strings.ToLower(t)
	switch {
	case strings.Contains(s, "mysql"), strings.Contains(s, "postgres"), strings.Contains(s, "oracle"),
		strings.Contains(s, "mongo"), strings.Contains(s, "db"), strings.Contains(s, "tidb"):
		return "database"
	case strings.Contains(s, "redis"), strings.Contains(s, "cache"), strings.Contains(s, "memcache"):
		return "cache"
	case strings.Contains(s, "rocketmq"), strings.Contains(s, "kafka"), strings.Contains(s, "rabbit"),
		strings.Contains(s, "mq"):
		return "mq"
	case strings.Contains(s, "gateway"), strings.Contains(s, "nginx"), strings.Contains(s, "kong"):
		return "gateway"
	default:
		return "application"
	}
}

// growthBetween 计算近期相对基线的涨幅（%）。
func growthBetween(baseline, recent float64) float64 {
	if math.Abs(baseline) < 1e-9 {
		if recent > 0 {
			return 100
		}
		return 0
	}
	return (recent - baseline) / math.Abs(baseline) * 100
}

// clampPct 把涨幅/跌幅映射到 0~1 的权重（达到 fullAt 记满）。
func clampPct(v, fullAt float64) float64 {
	if v <= 0 || fullAt <= 0 {
		return 0
	}
	r := v / fullAt
	if r > 1 {
		r = 1
	}
	return r
}
