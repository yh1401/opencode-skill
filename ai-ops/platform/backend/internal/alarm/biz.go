package alarm

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/store"
)

// T12 外部业务接口监控：按规则查询 PromQL，命中阈值发布 biz 告警。
const (
	bizRuleKind = "biz_rule"
	bizScanKind = "biz_scan"
	bizScanKey  = "latest"
	bizSource   = "biz"
)

// BizRule 业务接口监控规则（store kind=biz_rule，key=ruleId）。
type BizRule struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	AppID      string    `json:"appId"`
	Metric     string    `json:"metric"`     // error_rate | request_total | tp99
	Expr       string    `json:"expr"`       // 可选自定义 PromQL；为空时按 Metric 生成模板
	Comparator string    `json:"comparator"` // gt | lt，默认 gt
	Threshold  float64   `json:"threshold"`
	Severity   string    `json:"severity"` // critical | warning，默认 warning
	Enabled    bool      `json:"enabled"`
	WindowMin  int       `json:"windowMin"` // 默认 5
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// BizItem 单条规则的扫描结果。
type BizItem struct {
	RuleID     string  `json:"ruleId"`
	RuleName   string  `json:"ruleName"`
	AppID      string  `json:"appId"`
	Metric     string  `json:"metric"`
	Value      float64 `json:"value"`
	Threshold  float64 `json:"threshold"`
	Comparator string  `json:"comparator"`
	Breach     bool    `json:"breach"`
	Severity   string  `json:"severity"`
	Error      string  `json:"error,omitempty"`
}

// BizScanResult 一轮业务接口扫描结果。
type BizScanResult struct {
	At       time.Time `json:"at"`
	Rules    int       `json:"rules"`
	Breaches int       `json:"breaches"`
	Created  int       `json:"created"`
	Items    []BizItem `json:"items"`
}

// BizOverview 业务接口监控总览。
type BizOverview struct {
	Rules     []BizRule      `json:"rules"`
	RuleCount int            `json:"ruleCount"`
	LastScan  *BizScanResult `json:"lastScan"`
	Firing    []AlarmEvent   `json:"firing"`
}

// StartBiz 启动后台业务接口监控循环。阻塞直到 ctx cancel。
func (e *Engine) StartBiz(ctx context.Context) {
	if !e.cfg.AlarmEnabled || !e.cfg.BizMonEnabled {
		return
	}
	interval := e.cfg.BizMonInterval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = e.ScanBiz(ctx)
		}
	}
}

// ListBizRules 列出全部业务监控规则。
func (e *Engine) ListBizRules() []BizRule {
	rules := store.ListInto[BizRule](e.st, bizRuleKind)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules
}

// GetBizRule 按 ID 取规则。
func (e *Engine) GetBizRule(id string) (*BizRule, bool) {
	var r BizRule
	if !store.GetInto(e.st, bizRuleKind, id, &r) {
		return nil, false
	}
	return &r, true
}

// SaveBizRule 新建 / 更新规则，并补齐默认值。
func (e *Engine) SaveBizRule(r *BizRule) error {
	if r == nil {
		return fmt.Errorf("规则不能为空")
	}
	now := time.Now()
	if strings.TrimSpace(r.ID) == "" {
		r.ID = uuid.NewString()
	}
	r.Metric = strings.TrimSpace(r.Metric)
	if r.Metric == "" {
		return fmt.Errorf("metric 不能为空")
	}
	if r.Comparator != "lt" {
		r.Comparator = "gt"
	}
	if r.Severity != "critical" {
		r.Severity = "warning"
	}
	if r.WindowMin <= 0 {
		r.WindowMin = 5
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	return e.st.Put(bizRuleKind, r.ID, *r)
}

// DeleteBizRule 删除规则。
func (e *Engine) DeleteBizRule(id string) error {
	return e.st.Delete(bizRuleKind, id)
}

// LastBizScan 最近一轮扫描结果，nil 表示尚未扫描。
func (e *Engine) LastBizScan() *BizScanResult {
	b, ok := e.st.Get(bizScanKind, bizScanKey)
	if !ok {
		return nil
	}
	var res BizScanResult
	if err := json.Unmarshal(b, &res); err != nil {
		return nil
	}
	return &res
}

// BizOverview 总览：规则 + 最近扫描 + 当前 firing 告警；规则为空时初始化演示规则。
func (e *Engine) BizOverview() BizOverview {
	rules := e.ListBizRules()
	if len(rules) == 0 {
		e.ensureDemoBizRules()
		rules = e.ListBizRules()
	}
	firing := make([]AlarmEvent, 0)
	for _, ev := range e.List("firing") {
		if ev.Source == bizSource {
			firing = append(firing, ev)
		}
	}
	return BizOverview{
		Rules:     rules,
		RuleCount: len(rules),
		LastScan:  e.LastBizScan(),
		Firing:    firing,
	}
}

// ScanBiz 逐规则查询 Prometheus 判定越界，落库并维护 biz 告警生命周期。
func (e *Engine) ScanBiz(ctx context.Context) (*BizScanResult, error) {
	res := &BizScanResult{At: time.Now(), Items: make([]BizItem, 0)}
	if e.gw == nil || !e.gw.Enabled() {
		e.saveBizScan(res)
		return res, fmt.Errorf("统一接入网关未启用")
	}

	rules := e.ListBizRules()
	res.Rules = len(rules)
	keep := make(map[string]bool)
	freshItems := make([]ExternalItem, 0)

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		item := BizItem{
			RuleID:     rule.ID,
			RuleName:   rule.Name,
			AppID:      rule.AppID,
			Metric:     rule.Metric,
			Threshold:  rule.Threshold,
			Comparator: rule.Comparator,
			Severity:   rule.Severity,
		}
		fp := fmt.Sprintf("biz|%s|%s", rule.ID, rule.AppID)

		expr := bizExpr(rule)
		raw, err := e.gw.CallSilent(ctx, "prometheus", "query", map[string]any{"query": expr})
		if err != nil {
			item.Error = err.Error()
			keep[fp] = true // 查询失败不误判恢复
			res.Items = append(res.Items, item)
			continue
		}
		value, ok := parseVectorValue(raw)
		if !ok {
			item.Error = "查询结果为空"
			keep[fp] = true
			res.Items = append(res.Items, item)
			continue
		}
		item.Value = value
		item.Breach = bizBreach(value, rule.Threshold, rule.Comparator)
		res.Items = append(res.Items, item)

		if item.Breach {
			res.Breaches++
			keep[fp] = true
			freshItems = append(freshItems, ExternalItem{
				Fingerprint: fp,
				AlertName:   "BizMetricBreach",
				Instance:    rule.AppID,
				Host:        rule.AppID,
				Severity:    rule.Severity,
				Summary: fmt.Sprintf("业务接口指标越界：%s %s=%s 当前 %.4f（阈值 %s %.4f）",
					rule.AppID, rule.Metric, rule.Name, value, comparatorText(rule.Comparator), rule.Threshold),
				Labels: map[string]string{
					"appId":          rule.AppID,
					"ruleId":         rule.ID,
					"ruleName":       rule.Name,
					"metric":         rule.Metric,
					"comparator":     rule.Comparator,
					"threshold":      fmt.Sprintf("%.4f", rule.Threshold),
					"source":         bizSource,
					"middlewareType": "application",
				},
			})
		}
	}

	created, _, err := e.IngestExternalEvents(ctx, bizSource, freshItems)
	res.Created = len(created)
	e.ResolveExternal(ctx, bizSource, keep)
	e.saveBizScan(res)
	return res, err
}

func (e *Engine) saveBizScan(res *BizScanResult) {
	if res == nil {
		return
	}
	if res.At.IsZero() {
		res.At = time.Now()
	}
	_ = e.st.Put(bizScanKind, bizScanKey, *res)
}

// ensureDemoBizRules 首次无规则时写入 3 条演示规则。
func (e *Engine) ensureDemoBizRules() {
	if len(e.ListBizRules()) > 0 {
		return
	}
	demos := []BizRule{
		{ID: "biz-demo-error-rate", Name: "错误率过高", AppID: "apollo-ehome-pro", Metric: "error_rate", Comparator: "gt", Threshold: 0.05, Severity: "critical", Enabled: true, WindowMin: 5},
		{ID: "biz-demo-tp99", Name: "TP99 过高", AppID: "apollo-ehome-pro", Metric: "tp99", Comparator: "gt", Threshold: 1.0, Severity: "warning", Enabled: true, WindowMin: 5},
		{ID: "biz-demo-request-total", Name: "请求量跌破底线", AppID: "apollo-ehome-pro", Metric: "request_total", Comparator: "lt", Threshold: 2000, Severity: "warning", Enabled: true, WindowMin: 5},
	}
	for i := range demos {
		_ = e.SaveBizRule(&demos[i])
	}
}

// bizExpr 生成规则对应的 PromQL，自定义 Expr 优先。
func bizExpr(rule BizRule) string {
	if strings.TrimSpace(rule.Expr) != "" {
		return strings.ReplaceAll(rule.Expr, "{app}", rule.AppID)
	}
	return bizTemplate(rule.Metric, rule.AppID, "5m")
}

// bizTemplate 按指标生成模板查询，step 允许 T13 用扫描步长替换窗口。
func bizTemplate(metric, app, window string) string {
	switch metric {
	case "request_total":
		return fmt.Sprintf(`sum(rate(http_requests_total{app_id="%s"}[%s]))`, app, window)
	case "tp99":
		return fmt.Sprintf(`histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{app_id="%s"}[%s])) by (le))`, app, window)
	default: // error_rate
		return fmt.Sprintf(`sum(rate(http_requests_total{app_id="%s"}[%s])) by (app_id)`, app, window)
	}
}

func bizBreach(value, threshold float64, comparator string) bool {
	if comparator == "lt" {
		return value < threshold
	}
	return value > threshold
}

func comparatorText(c string) string {
	if c == "lt" {
		return "lt"
	}
	return "gt"
}

// parseVectorValue 从 query 响应里取第一条向量值（兼容字符串 / 数字）。
func parseVectorValue(raw json.RawMessage) (float64, bool) {
	res, err := mcp.PrometheusQuery(raw)
	if err != nil || len(res.Result) == 0 {
		return 0, false
	}
	var item struct {
		Value []any `json:"value"`
	}
	if err := json.Unmarshal(res.Result[0], &item); err != nil || len(item.Value) < 2 {
		return 0, false
	}
	return floatOf(item.Value[1])
}

func floatOf(v any) (float64, bool) {
	switch n := v.(type) {
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%g", &f); err == nil {
			return f, true
		}
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}
