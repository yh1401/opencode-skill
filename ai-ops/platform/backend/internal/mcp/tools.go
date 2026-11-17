package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Pinpoint
// ---------------------------------------------------------------------------

// PinpointApplication Pinpoint 应用条目。字段名在不同版本间不一致，统一归一。
type PinpointApplication struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ServiceTypeCode int    `json:"serviceTypeCode,omitempty"`
	ServiceTypeName string `json:"serviceTypeName,omitempty"`
}

type pinpointAppRaw struct {
	ApplicationID   string `json:"applicationId"`
	AppID           string `json:"appId"`
	ID              string `json:"id"`
	ApplicationName string `json:"applicationName"`
	AppName         string `json:"appName"`
	Name            string `json:"name"`
	ServiceTypeCode int    `json:"serviceTypeCode"`
	ServiceTypeName string `json:"serviceTypeName"`
}

func (r pinpointAppRaw) normalize() PinpointApplication {
	return PinpointApplication{
		ID:              firstNonEmptyStr(r.ApplicationID, r.AppID, r.ID),
		Name:            firstNonEmptyStr(r.ApplicationName, r.AppName, r.Name),
		ServiceTypeCode: r.ServiceTypeCode,
		ServiceTypeName: r.ServiceTypeName,
	}
}

// PinpointApplications 解析 list_applications_tool 返回。
// 兼容 {"applications":[...]} / {"data":[...]} / {"data":{"applications":[...]}} / 直接数组。
func PinpointApplications(raw json.RawMessage) ([]PinpointApplication, error) {
	res := ResultOf(raw)
	if len(res.Payload) == 0 {
		return nil, fmt.Errorf("Pinpoint 应用清单为空")
	}

	var arr []pinpointAppRaw
	if json.Unmarshal(res.Payload, &arr) == nil && arr != nil {
		return normalizePinpointApps(arr), nil
	}

	var body struct {
		Applications []pinpointAppRaw `json:"applications"`
		Data         json.RawMessage  `json:"data"`
	}
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		return nil, fmt.Errorf("Pinpoint 应用清单解析失败: %w", err)
	}
	if len(body.Applications) > 0 {
		return normalizePinpointApps(body.Applications), nil
	}
	data := body.Data
	if len(data) == 0 {
		return nil, fmt.Errorf("Pinpoint 应用清单结构未知")
	}
	if json.Unmarshal(data, &arr) == nil && arr != nil {
		return normalizePinpointApps(arr), nil
	}
	var nested struct {
		Applications []pinpointAppRaw `json:"applications"`
	}
	if json.Unmarshal(data, &nested) == nil && len(nested.Applications) > 0 {
		return normalizePinpointApps(nested.Applications), nil
	}
	return nil, fmt.Errorf("Pinpoint 应用清单结构未知")
}

func normalizePinpointApps(in []pinpointAppRaw) []PinpointApplication {
	out := make([]PinpointApplication, 0, len(in))
	for _, a := range in {
		app := a.normalize()
		if app.ID == "" && app.Name == "" {
			continue
		}
		out = append(out, app)
	}
	return out
}

// FindPinpointApplication 按 ID 精确 → 名称精确 → 名称包含 的顺序匹配。
func FindPinpointApplication(apps []PinpointApplication, ids, names []string) (PinpointApplication, bool) {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		for _, a := range apps {
			if strings.EqualFold(a.ID, id) {
				return a, true
			}
		}
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, a := range apps {
			if strings.EqualFold(a.Name, name) {
				return a, true
			}
		}
	}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		for _, a := range apps {
			if strings.Contains(strings.ToLower(a.Name), name) || strings.Contains(strings.ToLower(a.ID), name) {
				return a, true
			}
		}
	}
	return PinpointApplication{}, false
}

// PinpointNode 服务地图节点。
type PinpointNode struct {
	ApplicationName string         `json:"applicationName,omitempty"`
	ServiceType     string         `json:"serviceType,omitempty"`
	InstanceCount   int            `json:"instanceCount,omitempty"`
	Raw             map[string]any `json:"raw,omitempty"`
}

// PinpointMiddleware 服务地图中间件。
type PinpointMiddleware struct {
	Name        string         `json:"name,omitempty"`
	Type        string         `json:"type,omitempty"`
	ServiceType string         `json:"serviceType,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

// PinpointLink 服务地图调用链（from → to）。
type PinpointLink struct {
	Source       string         `json:"source,omitempty"`
	Target       string         `json:"target,omitempty"`
	Count        int            `json:"count,omitempty"`
	ErrorCount   int            `json:"errorCount,omitempty"`
	ResponseTime int            `json:"responseTime,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
}

// PinpointServerMap get_server_map_tool 的结构化视图。
type PinpointServerMap struct {
	Nodes       []PinpointNode       `json:"nodes"`
	Middlewares []PinpointMiddleware `json:"middlewares"`
	Links       []PinpointLink       `json:"links"`
	Raw         map[string]any       `json:"-"`
}

// PinpointServerMapOf 解析服务地图；结构未知时仍保留 Raw，不阻断排障。
func PinpointServerMapOf(raw json.RawMessage) (*PinpointServerMap, error) {
	res := ResultOf(raw)
	if len(res.Payload) == 0 {
		return nil, fmt.Errorf("Pinpoint 服务地图为空")
	}
	var m map[string]any
	if err := json.Unmarshal(res.Payload, &m); err != nil {
		return nil, fmt.Errorf("Pinpoint 服务地图解析失败: %w", err)
	}
	out := &PinpointServerMap{Raw: m}
	for _, key := range []string{"nodeData", "node_data", "nodes", "applicationData"} {
		if list := mapSlice(m[key]); len(list) > 0 {
			for _, item := range list {
				out.Nodes = append(out.Nodes, PinpointNode{
					ApplicationName: strOf(item, "applicationName", "application_name", "appName", "name"),
					ServiceType:     strOf(item, "serviceType", "service_type", "type"),
					InstanceCount:   intOf(item, "instanceCount", "instance_count", "count"),
					Raw:             item,
				})
			}
			break
		}
	}
	for _, key := range []string{"middleware", "middlewares", "middlewareData"} {
		if list := mapSlice(m[key]); len(list) > 0 {
			for _, item := range list {
				out.Middlewares = append(out.Middlewares, PinpointMiddleware{
					Name:        strOf(item, "name", "applicationName", "application_name"),
					Type:        strOf(item, "type", "serviceType", "service_type"),
					ServiceType: strOf(item, "serviceType", "service_type", "type"),
					Raw:         item,
				})
			}
			break
		}
	}
	for _, key := range []string{"linkData", "link_data", "links"} {
		if list := mapSlice(m[key]); len(list) > 0 {
			for _, item := range list {
				out.Links = append(out.Links, PinpointLink{
					Source:       strOf(item, "from", "source", "caller", "fromApplication", "fromApplicationName"),
					Target:       strOf(item, "to", "target", "callee", "toApplication", "toApplicationName"),
					Count:        intOf(item, "count", "callCount", "totalCount", "sampleCount"),
					ErrorCount:   intOf(item, "errorCount", "error_count", "errCount", "error"),
					ResponseTime: intOf(item, "responseTime", "response_time", "avgResponseTime", "elapsed"),
					Raw:          item,
				})
			}
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Prometheus
// ---------------------------------------------------------------------------

// PromAlert 统一告警条目（list_alerts / Alertmanager / api/v2/alerts）。
type PromAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state,omitempty"`
	ActiveAt    string            `json:"activeAt,omitempty"`
	StartsAt    string            `json:"startsAt,omitempty"`
	Value       string            `json:"value,omitempty"`
}

// PrometheusAlerts 解析告警列表，兼容 data.alerts / alerts / 直接数组。
func PrometheusAlerts(raw json.RawMessage) ([]PromAlert, error) {
	res := ResultOf(raw)
	if len(res.Payload) == 0 {
		return nil, fmt.Errorf("Prometheus 告警返回为空")
	}
	var arr []PromAlert
	if json.Unmarshal(res.Payload, &arr) == nil && arr != nil {
		return arr, nil
	}
	var body struct {
		Data struct {
			Alerts []PromAlert `json:"alerts"`
		} `json:"data"`
		Alerts []PromAlert `json:"alerts"`
	}
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		return nil, fmt.Errorf("Prometheus 告警解析失败: %w", err)
	}
	if len(body.Data.Alerts) > 0 {
		return body.Data.Alerts, nil
	}
	return body.Alerts, nil
}

// PromQueryResult 即时查询结果（query）。
type PromQueryResult struct {
	Status     string            `json:"status"`
	ResultType string            `json:"resultType"`
	Result     []json.RawMessage `json:"result"`
}

// PrometheusQuery 解析 query 返回，兼容 data.result / result / 直接数组。
func PrometheusQuery(raw json.RawMessage) (PromQueryResult, error) {
	res := ResultOf(raw)
	if len(res.Payload) == 0 {
		return PromQueryResult{}, fmt.Errorf("Prometheus 查询返回为空")
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string            `json:"resultType"`
			Result     []json.RawMessage `json:"result"`
		} `json:"data"`
		ResultType string            `json:"resultType"`
		Result     []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		return PromQueryResult{}, fmt.Errorf("Prometheus 查询结果解析失败: %w", err)
	}
	out := PromQueryResult{Status: body.Status}
	if len(body.Data.Result) > 0 || body.Data.ResultType != "" {
		out.ResultType = body.Data.ResultType
		out.Result = body.Data.Result
	} else {
		out.ResultType = body.ResultType
		out.Result = body.Result
	}
	return out, nil
}

// PromSample 即时查询的单条样本（一个时间序列的当前值）。
type PromSample struct {
	Metric map[string]string `json:"metric"`
	Value  float64           `json:"value"`
}

// PrometheusSamples 把 query 结果展开成逐条样本，便于按 instance 取值（如 up）。
func PrometheusSamples(raw json.RawMessage) ([]PromSample, error) {
	res := ResultOf(raw)
	if len(res.Payload) == 0 {
		return nil, fmt.Errorf("Prometheus 查询返回为空")
	}
	type series struct {
		Metric map[string]string `json:"metric"`
		Value  []any             `json:"value"`
	}
	var body struct {
		Data struct {
			Result []series `json:"result"`
		} `json:"data"`
		Result []series `json:"result"`
	}
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		return nil, fmt.Errorf("Prometheus 样本解析失败: %w", err)
	}
	rows := body.Data.Result
	if len(rows) == 0 {
		rows = body.Result
	}
	out := make([]PromSample, 0, len(rows))
	for _, r := range rows {
		s := PromSample{Metric: r.Metric}
		if len(r.Value) >= 2 {
			if f, ok := floatOf(r.Value[1]); ok {
				s.Value = f
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// PromHostMetrics host_metrics_realtime 的结构化视图（cpuUsage/memUsage/diskUsage/netConnections 等）。
type PromHostMetrics struct {
	Node    string             `json:"node,omitempty"`
	Metrics map[string]float64 `json:"metrics"`
	Raw     map[string]any     `json:"-"`
}

// PromHostMetricsOf 解析 host_metrics_realtime 返回，兼容 metrics / data.metrics / data 三种嵌套。
func PromHostMetricsOf(raw json.RawMessage) (PromHostMetrics, error) {
	res := ResultOf(raw)
	if res.IsError {
		msg := res.Text
		if msg == "" {
			msg = "Prometheus 主机指标查询失败"
		}
		return PromHostMetrics{}, fmt.Errorf("%s", msg)
	}
	if len(res.Payload) == 0 {
		return PromHostMetrics{}, fmt.Errorf("Prometheus 主机指标返回为空")
	}
	var body map[string]any
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		return PromHostMetrics{}, fmt.Errorf("Prometheus 主机指标解析失败: %w", err)
	}
	out := PromHostMetrics{Node: strOf(body, "node", "host", "instance", "ip"), Raw: body}
	if data, ok := body["data"].(map[string]any); ok {
		if out.Node == "" {
			out.Node = strOf(data, "node", "host", "instance", "ip")
		}
		if m, ok := data["metrics"].(map[string]any); ok {
			out.Metrics = numericMap(m)
			return out, nil
		}
	}
	if m, ok := body["metrics"].(map[string]any); ok {
		out.Metrics = numericMap(m)
		return out, nil
	}
	if data, ok := body["data"].(map[string]any); ok {
		out.Metrics = numericMap(data)
	}
	if out.Metrics == nil {
		out.Metrics = numericMap(body)
	}
	return out, nil
}

func numericMap(m map[string]any) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		if f, ok := floatOf(v); ok {
			out[k] = f
		}
	}
	return out
}

// PromMatrix 矩阵结果（range_query）的第一条时间序列。
type PromMatrix struct {
	Status string
	Metric map[string]string
	Values []float64
	Series []json.RawMessage
}

// PrometheusMatrix 解析 range_query 返回。
func PrometheusMatrix(raw json.RawMessage) (PromMatrix, error) {
	res := ResultOf(raw)
	if len(res.Payload) == 0 {
		return PromMatrix{}, fmt.Errorf("Prometheus 区间查询返回为空")
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][]any           `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		return PromMatrix{}, fmt.Errorf("Prometheus 区间结果解析失败: %w", err)
	}
	out := PromMatrix{Status: body.Status}
	if len(body.Data.Result) == 0 {
		return out, nil
	}
	first := body.Data.Result[0]
	out.Metric = first.Metric
	for _, pair := range first.Values {
		if len(pair) < 2 {
			continue
		}
		if f, ok := floatOf(pair[1]); ok {
			out.Values = append(out.Values, f)
		}
	}
	for _, s := range body.Data.Result {
		b, _ := json.Marshal(s)
		out.Series = append(out.Series, json.RawMessage(b))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// CMDB
// ---------------------------------------------------------------------------

// CMDBPage CMDB 分页结果。
type CMDBPage struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Records []map[string]any `json:"records"`
	Total   int              `json:"total"`
}

// CMDBRecords 解析 cmdb_server_query 返回（data.records / records / 直接数组）。
func CMDBRecords(raw json.RawMessage) (CMDBPage, error) {
	res := ResultOf(raw)
	if len(res.Payload) == 0 {
		return CMDBPage{}, fmt.Errorf("CMDB 返回为空")
	}
	var arr []map[string]any
	if json.Unmarshal(res.Payload, &arr) == nil && arr != nil {
		return CMDBPage{Records: arr, Total: len(arr)}, nil
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Records []map[string]any `json:"records"`
			Total   int              `json:"total"`
		} `json:"data"`
		Records []map[string]any `json:"records"`
		Total   int              `json:"total"`
	}
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		return CMDBPage{}, fmt.Errorf("CMDB 返回解析失败: %w", err)
	}
	page := CMDBPage{Code: body.Code, Message: body.Message}
	if len(body.Data.Records) > 0 || body.Data.Total > 0 {
		page.Records = body.Data.Records
		page.Total = body.Data.Total
	} else {
		page.Records = body.Records
		page.Total = body.Total
	}
	if page.Code != 0 && page.Code != 200 {
		msg := page.Message
		if msg == "" {
			msg = "未知错误"
		}
		return page, fmt.Errorf("CMDB 业务错误 code=%d: %s", page.Code, msg)
	}
	return page, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func mapSlice(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func strOf(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func intOf(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				return int(n)
			case int:
				return n
			case string:
				if i, err := strconv.Atoi(n); err == nil {
					return i
				}
			}
		}
	}
	return 0
}

func floatOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
