package mcp

import "log/slog"

// 工具输入结构体，用于 modelcontextprotocol/go-sdk 库。
// 这些结构体使用 jsonschema 标签进行自动 schema 生成。
// json 标签中没有 omitempty 的字段被视为必填项。

// 通用嵌入结构体，用于多个工具之间复用

// EmptyInput 用于没有输入参数的工具
type EmptyInput struct{}

// TimeRangeInput 为时间范围查询提供可选的开始/结束时间参数
type TimeRangeInput struct {
	StartTime string `json:"start_time,omitempty" jsonschema:"查询的开始时间戳。支持：Unix 时间戳（秒）、RFC3339 格式、或相对于现在的持续时间字符串（如 5m, 1h30m 等）。默认为 5 分钟前。"`
	EndTime   string `json:"end_time,omitempty" jsonschema:"查询的结束时间戳。支持：Unix 时间戳（秒）、RFC3339 格式、或相对于现在的持续时间字符串（如 5m, 1h30m 等）。默认为当前时间。"`
}

// TruncatableInput 为查询响应提供可选的截断限制
type TruncatableInput struct {
	TruncationLimit int `json:"truncation_limit,omitempty" jsonschema:"查询响应的截断限制（行数/条目数），设置为 -1 可禁用截断"`
}

// 工具定义结构体

// QueryInput 是瞬时查询工具的输入参数
type QueryInput struct {
	Query     string `json:"query" jsonschema:"要执行的 PromQL 查询语句"`
	Timestamp string `json:"timestamp,omitempty" jsonschema:"瞬时查询的评估时间戳。支持：Unix 时间戳（秒）、RFC3339 格式、或相对于现在的持续时间字符串（如 5m, 1h30m 等）。默认为当前时间。"`
	TruncatableInput
}

// LogValue 实现 slog.LogValuer 接口，用于结构化日志记录
func (qi QueryInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("query", qi.Query),
		slog.String("timestamp", qi.Timestamp),
	)
}

// RangeQueryInput 是范围查询工具的输入参数
type RangeQueryInput struct {
	Query string `json:"query" jsonschema:"要执行的 PromQL 查询语句"`
	Step  string `json:"step,omitempty" jsonschema:"查询分辨率步长，使用 Go 持续时间格式（如 '30s', '5m', '1h'），未指定时自动设置"`
	TimeRangeInput
	TruncatableInput
}

// LogValue 实现 slog.LogValuer 接口
func (rqi RangeQueryInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("query", rqi.Query),
		slog.String("step", rqi.Step),
		slog.String("start_time", rqi.StartTime),
		slog.String("end_time", rqi.EndTime),
	)
}

// ExemplarQueryInput 是示例查询工具的输入参数
type ExemplarQueryInput struct {
	Query string `json:"query" jsonschema:"要执行的 PromQL 查询语句"`
	TimeRangeInput
	TruncatableInput
}

// LogValue 实现 slog.LogValuer 接口
func (eqi ExemplarQueryInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("query", eqi.Query),
		slog.String("start_time", eqi.StartTime),
		slog.String("end_time", eqi.EndTime),
	)
}

// SeriesInput 是序列查询工具的输入参数
type SeriesInput struct {
	Matches []string `json:"matches" jsonschema:"选择要返回的序列的选择器参数，必填"`
	TimeRangeInput
	TruncatableInput
}

// LogValue 实现 slog.LogValuer 接口
func (si SeriesInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("matches", si.Matches),
		slog.String("start_time", si.StartTime),
		slog.String("end_time", si.EndTime),
	)
}

// LabelNamesInput 是标签名称查询工具的输入参数
type LabelNamesInput struct {
	Matches []string `json:"matches,omitempty" jsonschema:"用于过滤标签名称的序列选择器参数"`
	TimeRangeInput
	TruncatableInput
}

// LogValue 实现 slog.LogValuer 接口
func (lni LabelNamesInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("matches", lni.Matches),
		slog.String("start_time", lni.StartTime),
		slog.String("end_time", lni.EndTime),
	)
}

// LabelValuesInput 是标签值查询工具的输入参数
type LabelValuesInput struct {
	Label   string   `json:"label" jsonschema:"要查询值的标签名称，必填"`
	Matches []string `json:"matches,omitempty" jsonschema:"用于过滤标签值的序列选择器参数"`
	TimeRangeInput
	TruncatableInput
}

// LogValue 实现 slog.LogValuer 接口
func (lvi LabelValuesInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("label", lvi.Label),
		slog.Any("matches", lvi.Matches),
		slog.String("start_time", lvi.StartTime),
		slog.String("end_time", lvi.EndTime),
	)
}

// MetricMetadataInput 是指标元数据工具的输入参数
type MetricMetadataInput struct {
	Metric string `json:"metric,omitempty" jsonschema:"要获取元数据的指标名称，为空时获取所有指标"`
	Limit  string `json:"limit,omitempty" jsonschema:"返回的最大指标数量"`
}

// LogValue 实现 slog.LogValuer 接口
func (mmi MetricMetadataInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("metric", mmi.Metric),
		slog.String("limit", mmi.Limit),
	)
}

// TargetsMetadataInput 是目标元数据工具的输入参数
type TargetsMetadataInput struct {
	MatchTarget string `json:"match_target,omitempty" jsonschema:"用于匹配目标的标签选择器，为空时匹配所有目标"`
	Metric      string `json:"metric,omitempty" jsonschema:"要获取元数据的指标名称，为空时获取所有指标"`
	Limit       string `json:"limit,omitempty" jsonschema:"要匹配的最大目标数量"`
}

// LogValue 实现 slog.LogValuer 接口
func (tmi TargetsMetadataInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("match_target", tmi.MatchTarget),
		slog.String("metric", tmi.Metric),
		slog.String("limit", tmi.Limit),
	)
}

// DeleteSeriesInput 是删除序列管理工具的输入参数
type DeleteSeriesInput struct {
	Matches []string `json:"matches" jsonschema:"要删除的序列的选择器参数，必填"`
	TimeRangeInput
}

// LogValue 实现 slog.LogValuer 接口
func (dsi DeleteSeriesInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("matches", dsi.Matches),
		slog.String("start_time", dsi.StartTime),
		slog.String("end_time", dsi.EndTime),
	)
}

// SnapshotInput 是快照管理工具的输入参数
type SnapshotInput struct {
	SkipHead bool `json:"skip_head,omitempty" jsonschema:"跳过头部块中的数据"`
}

// LogValue 实现 slog.LogValuer 接口
func (si SnapshotInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("skip_head", si.SkipHead),
	)
}

// DocsReadInput 是文档读取工具的输入参数
type DocsReadInput struct {
	File string `json:"file" jsonschema:"要读取的文档文件名"`
}

// LogValue 实现 slog.LogValuer 接口
func (dri DocsReadInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("file", dri.File),
	)
}

// DocsSearchInput 是文档搜索工具的输入参数
type DocsSearchInput struct {
	Query string `json:"query" jsonschema:"要在文档中搜索的查询词"`
	Limit int    `json:"limit,omitempty" jsonschema:"返回的最大搜索结果数量"`
}

// LogValue 实现 slog.LogValuer 接口
func (dsi DocsSearchInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("query", dsi.Query),
		slog.Int("limit", dsi.Limit),
	)
}

// HostMetricsRealtimeInput 是主机指标实时查询工具的输入参数
type HostMetricsRealtimeInput struct {
	Node string `json:"node,omitempty" jsonschema:"要查询指标的节点名称"`
	IP   string `json:"ip,omitempty" jsonschema:"要查询指标的节点 IP 地址"`
}

// LogValue 实现 slog.LogValuer 接口
func (hmi HostMetricsRealtimeInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("node", hmi.Node),
		slog.String("ip", hmi.IP),
	)
}

// HostMetricsTrendInput 是主机指标趋势查询工具的输入参数
type HostMetricsTrendInput struct {
	Node      string `json:"node,omitempty" jsonschema:"要查询指标的节点名称"`
	IP        string `json:"ip,omitempty" jsonschema:"要查询指标的节点 IP 地址"`
	StartTime string `json:"start_time" jsonschema:"查询的开始时间戳。支持：Unix 时间戳（秒）、RFC3339 格式、或相对于现在的持续时间字符串（如 5m, 1h30m 等）。默认为 1 小时前。"`
	EndTime   string `json:"end_time,omitempty" jsonschema:"查询的结束时间戳。支持：Unix 时间戳（秒）、RFC3339 格式、或相对于现在的持续时间字符串（如 5m, 1h30m 等）。默认为当前时间。"`
	Step      string `json:"step,omitempty" jsonschema:"查询分辨率步长，使用 Go 持续时间格式（如 '30s', '5m', '1h'），未指定时自动设置"`
}

// LogValue 实现 slog.LogValuer 接口
func (hti HostMetricsTrendInput) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("node", hti.Node),
		slog.String("ip", hti.IP),
		slog.String("start_time", hti.StartTime),
		slog.String("end_time", hti.EndTime),
		slog.String("step", hti.Step),
	)
}

// ModuleTopologyInput 是模块拓扑工具的输入参数
type ModuleTopologyInput struct {
	Node        string `json:"node" jsonschema:"目标节点，取值来自节点列表接口的 node 字段，例如：云公司->贵州"`
	ProductName string `json:"product_name" jsonschema:"产品名称，例如：天翼看家"`
	Center      string `json:"center,omitempty" jsonschema:"是否为中心节点，取值 true 或 false，默认为 true"`
}

// ChainTopologyInput 是服务调用链路拓扑工具的输入参数
type ChainTopologyInput struct {
	Node        string   `json:"node" jsonschema:"节点标识，使用模块拓扑接口返回的 deployNode 值，可能包含多个以逗号分隔的节点或别名"`
	Projects    []string `json:"projects" jsonschema:"目标模块的应用项目名称列表，来自模块拓扑接口返回的 projectName"`
	Center      string   `json:"center,omitempty" jsonschema:"是否为中心节点，取值 true 或 false，默认为 true"`
	CalleeRange int      `json:"callee_range,omitempty" jsonschema:"下游调用深度，默认为 1"`
	CallerRange int      `json:"caller_range,omitempty" jsonschema:"上游调用深度，默认为 1"`
	FilterNode  bool     `json:"filter_node,omitempty" jsonschema:"是否过滤节点，默认为 false"`
	Minute      int      `json:"minute,omitempty" jsonschema:"统计时间窗口（分钟），默认为 5"`
	NodeType    string   `json:"node_type,omitempty" jsonschema:"节点类型过滤，默认为空"`
}

// NodeListInput 是节点列表工具的输入参数
type NodeListInput struct {
	Center     string `json:"center,omitempty" jsonschema:"是否为中心节点，取值 true 或 false，默认为 true"`
	NamePrefix string `json:"name_prefix,omitempty" jsonschema:"按节点名称前缀过滤，例如：云公司，为空时返回全部节点"`
}

// VirtualStructureInput 是物理机与云主机关系工具的输入参数
type VirtualStructureInput struct {
	Node   string `json:"node" jsonschema:"节点标识，使用节点列表接口返回的 ctyun_node_code3 字段，例如：hi-haikou-9"`
	Center string `json:"center,omitempty" jsonschema:"是否为中心节点，取值 true 或 false，默认为 true"`
}

// ProjectTopologyInput 是项目及云主机部署关系工具的输入参数
type ProjectTopologyInput struct {
	Node   string `json:"node" jsonschema:"节点名称，使用节点列表接口返回的 node 字段，例如：云公司->海口9"`
	Center string `json:"center,omitempty" jsonschema:"是否为中心节点，取值 true 或 false，默认为 true"`
}
