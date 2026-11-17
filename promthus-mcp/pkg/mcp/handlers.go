package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/tjhop/prometheus-mcp-server/internal/metrics"
	mcpProm "github.com/tjhop/prometheus-mcp-server/pkg/prometheus"
)

// 处理程序的常量和共享类型
var (
	// DefaultLookbackDelta 是查询的默认时间范围
	DefaultLookbackDelta = -5 * time.Minute

	// Prometheus API调用指标监控
	metricAPICallsFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(metrics.MetricNamespace, "api", "calls_failed_total"),
			Help: "Prometheus API 失败总数，按端点统计",
		},
		[]string{"target_path"},
	)

	metricAPICallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:                        prometheus.BuildFQName(metrics.MetricNamespace, "api", "call_duration_seconds"),
			Help:                        "Prometheus API 调用持续时间，按端点统计（秒）",
			Buckets:                     prometheus.ExponentialBuckets(0.25, 2, 10),
			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"target_path"},
	)

	errTSDBAdminToolsNotEnabled = errors.New("TSDB 管理工具必须使用 `--dangerous.enable-tsdb-admin-tools` 标志启用")
)

// 管理 API 端点常量
const (
	mgmtAPIEndpointPrefix  = "/-/"
	mgmtAPIHealthyEndpoint = mgmtAPIEndpointPrefix + "healthy"
	mgmtAPIReadyEndpoint   = mgmtAPIEndpointPrefix + "ready"
	mgmtAPIReloadEndpoint  = mgmtAPIEndpointPrefix + "reload"
	mgmtAPIQuitEndpoint    = mgmtAPIEndpointPrefix + "quit"

	// defaultRangeQueryDataPoints 是范围查询的目标数据点数量
	// 当未明确提供步长时，步长会自动计算以在此查询范围内产生大约这么多数据点
	defaultRangeQueryDataPoints = 250
)

func init() {
	metrics.Registry.MustRegister(
		metricAPICallsFailed,
		metricAPICallDuration,
	)
}

// queryAPIResponse 是查询 API 调用的响应结构
type queryAPIResponse struct {
	Result   string          `json:"result"`
	Warnings promv1.Warnings `json:"warnings"`
}

// truncateStringByLines 将字符串截断到指定的行数
// 返回截断后的字符串和一个布尔值，表示是否发生了截断
func truncateStringByLines(s string, limit int) (string, bool) {
	if limit <= 0 {
		// 禁用截断
		return s, false
	}

	endMarker := 0
	for range limit {
		// 从上次的 endMarker 开始查找下一个换行符
		x := strings.Index(s[endMarker:], "\n")
		if x == -1 {
			// 没有找到更多换行符，说明行数少于限制，返回完整字符串
			return s, false
		}

		// 总是跳过换行符，使每次迭代从下一行开始，
		// 并且截断结果包含最后一行的尾随换行符
		endMarker += x + 1
	}

	// 根据限制中的最后一行末尾进行字符串截断
	return s[:endMarker], true
}

const (
	truncationWarningTemplate = "\n\n" +
		"警告：结果已被截断，因为 Prometheus MCP 服务器使用 '--prometheus.truncation-limit=%d' 标志启动。\n" +
		"您可能需要尝试通过优化标签过滤器或使用聚合函数来分组结果来优化查询。\n" +
		"如果需要，多个工具支持 'truncation_limit'/'limit' 参数，可以在每次工具调用的基础上覆盖全局截断限制。\n" +
		"这包括通过将截断限制设置为 -1 来在工具调用上禁用截断的能力。"
)

// displayTruncationWarning 返回截断结果的警告消息
func displayTruncationWarning(limit int) string {
	return fmt.Sprintf(truncationWarningTemplate, limit)
}

// parseTimeWithDefault 使用 ParseTimestampOrDuration 解析时间字符串
// 如果输入为空，则返回 defaultVal。这整合了多个处理程序中重复的可选时间解析模式
func parseTimeWithDefault(s string, defaultVal time.Time) (time.Time, error) {
	if s == "" {
		return defaultVal, nil
	}
	return mcpProm.ParseTimestampOrDuration(s)
}

// parseTimeRangeInputWithDefaults 解析 TimeRangeInput 中的开始和结束时间字符串，
// 当任一值为空时应用给定的默认值。这整合了具有独立开始/结束默认值的处理程序使用的重复模式
func parseTimeRangeInputWithDefaults(tr TimeRangeInput, startDefault, endDefault time.Time) (start, end time.Time, err error) {
	start, err = parseTimeWithDefault(tr.StartTime, startDefault)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("解析 start_time 失败: %w", err)
	}
	end, err = parseTimeWithDefault(tr.EndTime, endDefault)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("解析 end_time 失败: %w", err)
	}
	return start, end, nil
}

// ==================== ServerContainer 的工具处理程序方法 ====================

// QueryHandler 处理瞬时查询工具
func (s *ServerContainer) QueryHandler(ctx context.Context, req *mcp.CallToolRequest, input QueryInput) (*mcp.CallToolResult, any, error) {
	if input.Query == "" {
		return newToolErrorResult("query 参数是必需的"), nil, nil
	}

	ts, err := parseTimeWithDefault(input.Timestamp, time.Now())
	if err != nil {
		return newToolErrorResult(fmt.Sprintf("解析时间戳失败: %v", err)), nil, nil
	}

	truncationLimit := s.GetEffectiveTruncationLimit(input.TruncationLimit)
	result, err := s.queryAPICall(ctx, input.Query, ts, truncationLimit)
	if err != nil {
		return newToolErrorResult("查询 API 调用失败: " + err.Error()), nil, nil
	}

	return newToolTextResult(result), nil, nil
}

// RangeQueryHandler 处理范围查询工具
func (s *ServerContainer) RangeQueryHandler(ctx context.Context, req *mcp.CallToolRequest, input RangeQueryInput) (*mcp.CallToolResult, any, error) {
	if input.Query == "" {
		return newToolErrorResult("query 参数是必需的"), nil, nil
	}

	endTs, err := parseTimeWithDefault(input.EndTime, time.Now())
	if err != nil {
		return newToolErrorResult(fmt.Sprintf("解析 end_time 失败: %v", err)), nil, nil
	}

	startTs, err := parseTimeWithDefault(input.StartTime, endTs.Add(DefaultLookbackDelta))
	if err != nil {
		return newToolErrorResult(fmt.Sprintf("解析 start_time 失败: %v", err)), nil, nil
	}

	// 根据实际时间范围计算步长（解析用户输入后）
	var step time.Duration
	if input.Step != "" {
		parsedModelStep, err := model.ParseDuration(input.Step)
		if err != nil {
			return newToolErrorResult(fmt.Sprintf("解析步长失败: %v", err)), nil, nil
		}
		parsedStep := time.Duration(parsedModelStep)
		if parsedStep <= 0 {
			return newToolErrorResult("步长必须是正的持续时间（例如 '30s', '5m', '1h', '1d'）"), nil, nil
		}
		step = parsedStep
	} else {
		// 自动计算步长以产生大约 defaultRangeQueryDataPoints 个数据点
		resolution := math.Max(math.Floor(endTs.Sub(startTs).Seconds()/defaultRangeQueryDataPoints), 1)
		step = time.Duration(resolution) * time.Second
	}

	truncationLimit := s.GetEffectiveTruncationLimit(input.TruncationLimit)
	result, err := s.rangeQueryAPICall(ctx, input.Query, startTs, endTs, step, truncationLimit)
	if err != nil {
		return newToolErrorResult("范围查询 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// ExemplarQueryHandler 处理示例查询工具
func (s *ServerContainer) ExemplarQueryHandler(ctx context.Context, req *mcp.CallToolRequest, input ExemplarQueryInput) (*mcp.CallToolResult, any, error) {
	if input.Query == "" {
		return newToolErrorResult("query 参数是必需的"), nil, nil
	}

	endTs, err := parseTimeWithDefault(input.EndTime, time.Now())
	if err != nil {
		return newToolErrorResult(fmt.Sprintf("解析 end_time 失败: %v", err)), nil, nil
	}

	startTs, err := parseTimeWithDefault(input.StartTime, endTs.Add(DefaultLookbackDelta))
	if err != nil {
		return newToolErrorResult(fmt.Sprintf("解析 start_time 失败: %v", err)), nil, nil
	}

	truncationLimit := s.GetEffectiveTruncationLimit(input.TruncationLimit)
	result, err := s.exemplarQueryAPICall(ctx, input.Query, startTs, endTs, truncationLimit)
	if err != nil {
		return newToolErrorResult("示例查询 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// SeriesHandler 处理序列查询工具
func (s *ServerContainer) SeriesHandler(ctx context.Context, req *mcp.CallToolRequest, input SeriesInput) (*mcp.CallToolResult, any, error) {
	if len(input.Matches) == 0 {
		return newToolErrorResult("至少需要一个 matches 参数"), nil, nil
	}

	startTs, endTs, err := parseTimeRangeInputWithDefaults(input.TimeRangeInput, time.Time{}, time.Time{})
	if err != nil {
		return newToolErrorResult(err.Error()), nil, nil
	}

	truncationLimit := s.GetEffectiveTruncationLimit(input.TruncationLimit)
	result, err := s.seriesAPICall(ctx, input.Matches, startTs, endTs, truncationLimit)
	if err != nil {
		return newToolErrorResult("序列 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// LabelNamesHandler 处理标签名称查询工具
func (s *ServerContainer) LabelNamesHandler(ctx context.Context, req *mcp.CallToolRequest, input LabelNamesInput) (*mcp.CallToolResult, any, error) {
	startTs, endTs, err := parseTimeRangeInputWithDefaults(input.TimeRangeInput, time.Time{}, time.Time{})
	if err != nil {
		return newToolErrorResult(err.Error()), nil, nil
	}

	truncationLimit := s.GetEffectiveTruncationLimit(input.TruncationLimit)
	result, err := s.labelNamesAPICall(ctx, input.Matches, startTs, endTs, truncationLimit)
	if err != nil {
		return newToolErrorResult("标签名称 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// LabelValuesHandler 处理标签值查询工具
func (s *ServerContainer) LabelValuesHandler(ctx context.Context, req *mcp.CallToolRequest, input LabelValuesInput) (*mcp.CallToolResult, any, error) {
	if input.Label == "" {
		return newToolErrorResult("label 参数是必需的"), nil, nil
	}

	startTs, endTs, err := parseTimeRangeInputWithDefaults(input.TimeRangeInput, time.Time{}, time.Time{})
	if err != nil {
		return newToolErrorResult(err.Error()), nil, nil
	}

	truncationLimit := s.GetEffectiveTruncationLimit(input.TruncationLimit)
	result, err := s.labelValuesAPICall(ctx, input.Label, input.Matches, startTs, endTs, truncationLimit)
	if err != nil {
		return newToolErrorResult("标签值 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// MetricMetadataHandler 处理指标元数据工具
func (s *ServerContainer) MetricMetadataHandler(ctx context.Context, req *mcp.CallToolRequest, input MetricMetadataInput) (*mcp.CallToolResult, any, error) {
	result, err := s.metricMetadataAPICall(ctx, input.Metric, input.Limit)
	if err != nil {
		return newToolErrorResult("指标元数据 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// TargetsMetadataHandler 处理目标元数据工具
func (s *ServerContainer) TargetsMetadataHandler(ctx context.Context, req *mcp.CallToolRequest, input TargetsMetadataInput) (*mcp.CallToolResult, any, error) {
	result, err := s.targetsMetadataAPICall(ctx, input.MatchTarget, input.Metric, input.Limit)
	if err != nil {
		return newToolErrorResult("目标元数据 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// callAPIAndReturnToolResult 封装了简单工具处理程序的通用模式，
// 这些处理程序调用单个 API 方法并将结果作为工具文本响应返回
func callAPIAndReturnToolResult(ctx context.Context, call func(context.Context) (string, error), errPrefix string) (*mcp.CallToolResult, any, error) {
	result, err := call(ctx)
	if err != nil {
		return newToolErrorResult(errPrefix + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// AlertmanagersHandler 处理 alertmanagers 工具
func (s *ServerContainer) AlertmanagersHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.alertmanagersAPICall, "alertmanagers API 调用失败: ")
}

// FlagsHandler 处理 flags 工具
func (s *ServerContainer) FlagsHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.flagsAPICall, "flags API 调用失败: ")
}

// ListAlertsHandler 处理 list_alerts 工具
func (s *ServerContainer) ListAlertsHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.listAlertsAPICall, "告警列表 API 调用失败: ")
}

// TsdbStatsHandler 处理 tsdb_stats 工具
func (s *ServerContainer) TsdbStatsHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.tsdbStatsAPICall, "TSDB 统计 API 调用失败: ")
}

// BuildInfoHandler 处理 build_info 工具
func (s *ServerContainer) BuildInfoHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.buildinfoAPICall, "构建信息 API 调用失败: ")
}

// ConfigHandler 处理 config 工具
func (s *ServerContainer) ConfigHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.configAPICall, "配置 API 调用失败: ")
}

// RuntimeInfoHandler 处理 runtime_info 工具
func (s *ServerContainer) RuntimeInfoHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.runtimeinfoAPICall, "运行时信息 API 调用失败: ")
}

// ListRulesHandler 处理 list_rules 工具
func (s *ServerContainer) ListRulesHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.rulesAPICall, "规则列表 API 调用失败: ")
}

// ListTargetsHandler 处理 list_targets 工具
func (s *ServerContainer) ListTargetsHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.targetsAPICall, "目标列表 API 调用失败: ")
}

// WALReplayHandler 处理 WAL 重放状态工具
func (s *ServerContainer) WALReplayHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	return callAPIAndReturnToolResult(ctx, s.walReplayAPICall, "WAL 重放 API 调用失败: ")
}

// ==================== Prometheus TSDB 管理工具处理程序 ====================

// CleanTombstonesHandler 处理 clean_tombstones 管理工具
func (s *ServerContainer) CleanTombstonesHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	if !s.tsdbAdminToolsEnabled {
		return newToolErrorResult("清理墓碑 API 调用失败: " + errTSDBAdminToolsNotEnabled.Error()), nil, nil
	}

	logger := s.GetToolLogger(req, nil)

	logger.Warn("正在执行 TSDB 管理操作: 清理墓碑")

	result, err := s.cleanTombstonesAPICall(ctx)
	if err != nil {
		return newToolErrorResult("清理墓碑 API 调用失败: " + err.Error()), nil, nil
	}

	logger.Warn("清理墓碑完成")
	return newToolTextResult(result), nil, nil
}

// DeleteSeriesHandler 处理 delete_series 管理工具
func (s *ServerContainer) DeleteSeriesHandler(ctx context.Context, req *mcp.CallToolRequest, input DeleteSeriesInput) (*mcp.CallToolResult, any, error) {
	if !s.tsdbAdminToolsEnabled {
		return newToolErrorResult("删除序列 API 调用失败: " + errTSDBAdminToolsNotEnabled.Error()), nil, nil
	}

	if len(input.Matches) == 0 {
		return newToolErrorResult("至少需要一个 matches 参数"), nil, nil
	}

	// 破坏性操作需要明确的时间边界。如果没有时间边界，
	// Prometheus 默认会删除整个保留窗口的数据。
	// 这几乎可以肯定不是最终用户的预期行为，但如果是，让他们明确请求。
	if input.StartTime == "" || input.EndTime == "" {
		return newToolErrorResult("delete_series 必须同时提供 start_time 和 end_time 以防止意外删除所有数据；请指定明确的时间边界"), nil, nil
	}

	logger := s.GetToolLogger(req, input)

	startTs, endTs, err := parseTimeRangeInputWithDefaults(input.TimeRangeInput, time.Time{}, time.Time{})
	if err != nil {
		return newToolErrorResult(err.Error()), nil, nil
	}

	logger.Warn("正在执行 TSDB 管理操作: 删除序列")

	result, err := s.deleteSeriesAPICall(ctx, input.Matches, startTs, endTs)
	if err != nil {
		return newToolErrorResult("删除序列 API 调用失败: " + err.Error()), nil, nil
	}

	logger.Warn("删除序列完成")
	return newToolTextResult(result), nil, nil
}

// SnapshotHandler 处理 snapshot 管理工具
func (s *ServerContainer) SnapshotHandler(ctx context.Context, req *mcp.CallToolRequest, input SnapshotInput) (*mcp.CallToolResult, any, error) {
	if !s.tsdbAdminToolsEnabled {
		return newToolErrorResult("快照 API 调用失败: " + errTSDBAdminToolsNotEnabled.Error()), nil, nil
	}

	logger := s.GetToolLogger(req, input)

	logger.Warn("正在执行 TSDB 管理操作: 快照")

	result, err := s.snapshotAPICall(ctx, input.SkipHead)
	if err != nil {
		return newToolErrorResult("快照 API 调用失败: " + err.Error()), nil, nil
	}

	logger.Warn("快照完成")
	return newToolTextResult(result), nil, nil
}

// ==================== 管理 API 处理程序 ====================

// HealthyHandler 处理健康检查工具
func (s *ServerContainer) HealthyHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	result, err := s.doManagementAPICall(ctx, http.MethodGet, mgmtAPIHealthyEndpoint)
	if err != nil {
		return newToolErrorResult("健康检查 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// ReadyHandler 处理就绪检查工具
func (s *ServerContainer) ReadyHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	result, err := s.doManagementAPICall(ctx, http.MethodGet, mgmtAPIReadyEndpoint)
	if err != nil {
		return newToolErrorResult("就绪检查 API 调用失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// ReloadHandler 处理重新加载配置工具
func (s *ServerContainer) ReloadHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	logger := s.GetToolLogger(req, nil)

	logger.Warn("正在触发 Prometheus 配置重新加载")

	result, err := s.doManagementAPICall(ctx, http.MethodPost, mgmtAPIReloadEndpoint)
	if err != nil {
		return newToolErrorResult("重新加载 API 调用失败: " + err.Error()), nil, nil
	}

	logger.Warn("重新加载完成")
	return newToolTextResult(result), nil, nil
}

// QuitHandler 处理退出/关闭工具
func (s *ServerContainer) QuitHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	logger := s.GetToolLogger(req, nil)

	logger.Warn("正在触发 Prometheus 关闭")

	result, err := s.doManagementAPICall(ctx, http.MethodPost, mgmtAPIQuitEndpoint)
	if err != nil {
		return newToolErrorResult("退出 API 调用失败: " + err.Error()), nil, nil
	}

	logger.Warn("退出信号已发送 - Prometheus 正在关闭")
	return newToolTextResult(result), nil, nil
}

// ==================== 文档工具处理程序 ====================

// DocsListHandler 处理 docs_list 工具
func (s *ServerContainer) DocsListHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	resourceReq := &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{
			URI: resourcePrefix + "docs",
		},
	}

	resourceResult, err := s.DocsListResourceHandler(ctx, resourceReq)
	if err != nil {
		return newToolErrorResult("列出文档失败: " + err.Error()), nil, nil
	}

	return embedResourceContentsInToolResult(resourceResult, &mcp.CallToolResult{}), nil, nil
}

// DocsReadHandler 处理 docs_read 工具
func (s *ServerContainer) DocsReadHandler(ctx context.Context, req *mcp.CallToolRequest, input DocsReadInput) (*mcp.CallToolResult, any, error) {
	if input.File == "" {
		return newToolErrorResult("file 参数是必需的"), nil, nil
	}

	// 使用正确的 URL 编码构造资源 URI
	uri := resourcePrefix + "docs/" + url.PathEscape(input.File)
	resourceReq := &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{
			URI: uri,
		},
	}

	resourceResult, err := s.DocsReadResourceHandler(ctx, resourceReq)
	if err != nil {
		return newToolErrorResult("读取文档文件失败: " + err.Error()), nil, nil
	}

	return embedResourceContentsInToolResult(resourceResult, &mcp.CallToolResult{}), nil, nil
}

// DocsSearchHandler 处理 docs_search 工具
func (s *ServerContainer) DocsSearchHandler(ctx context.Context, req *mcp.CallToolRequest, input DocsSearchInput) (*mcp.CallToolResult, any, error) {
	logger := s.GetToolLogger(req, input)

	if input.Query == "" {
		return newToolErrorResult("query 参数是必需的"), nil, nil
	}

	matchingChunkIDs, err := s.SearchDocs(input.Query, input.Limit)
	if err != nil {
		return newToolErrorResult("搜索文档失败: " + err.Error()), nil, nil
	}

	if len(matchingChunkIDs) == 0 {
		return newToolTextResult("未找到匹配查询的文档: " + input.Query), nil, nil
	}

	// 从块 ID 中提取唯一的文件名
	matchingDocsFiles := []string{}
	docsFilesSeen := make(map[string]struct{})
	for _, chunkID := range matchingChunkIDs {
		parts := strings.Split(chunkID, "#")
		if len(parts) != 2 {
			// 有效的块格式是 `filename#chunkID`
			logger.Warn("跳过格式错误的块 ID", "chunk_id", chunkID)
			continue
		}
		name := parts[0]
		if _, seen := docsFilesSeen[name]; !seen {
			docsFilesSeen[name] = struct{}{}
			matchingDocsFiles = append(matchingDocsFiles, name)
		}
	}

	logger.Debug("文档搜索完成", "matching_files", len(matchingDocsFiles))

	// 通过资源处理程序读取所有匹配的文件并合并结果
	resourceResults := make([]*mcp.ReadResourceResult, 0, len(matchingDocsFiles))
	for _, file := range matchingDocsFiles {
		uri := resourcePrefix + "docs/" + url.PathEscape(file)
		// 每次迭代分配一个新的请求以避免在对 DocsReadResourceHandler 的并发或顺序调用中共享结构体突变
		resourceReq := &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: uri}}

		resourceResult, err := s.DocsReadResourceHandler(ctx, resourceReq)
		if err != nil {
			return newToolErrorResult("读取文档文件失败: " + err.Error()), nil, nil
		}
		resourceResults = append(resourceResults, resourceResult)
	}

	searchSummary := fmt.Sprintf("找到 %d 个匹配查询 %q 的文档文件: %q", len(matchingDocsFiles), input.Query, matchingDocsFiles)

	combinedSearchContents := concatResourceContents(resourceResults...)
	content := make([]mcp.Content, 0, len(combinedSearchContents)+1)
	content = append(content, &mcp.TextContent{Text: searchSummary})
	for _, rc := range combinedSearchContents {
		content = append(content, &mcp.EmbeddedResource{Resource: rc})
	}

	return &mcp.CallToolResult{Content: content}, nil, nil
}

// ==================== Thanos 专用处理程序 ====================

// ThanosStoresHandler 处理 Thanos list_stores 工具
func (s *ServerContainer) ThanosStoresHandler(ctx context.Context, req *mcp.CallToolRequest, input EmptyInput) (*mcp.CallToolResult, any, error) {
	_, rt := s.GetAPIClient(ctx)

	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/stores"
	result, err := s.doHTTPRequest(ctx, http.MethodGet, rt, path, true)
	if err != nil {
		return newToolErrorResult("从 Thanos 获取存储失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(result), nil, nil
}

// ==================== ServerContainer 的 Prometheus API 调用方法 ====================

// formatTruncatedQueryAPIResponse 对结果字符串应用基于行的截断，
// 如果被截断则添加警告，用可选的警告将其包装在 queryAPIResponse 中，并格式化输出
func (s *ServerContainer) formatTruncatedQueryAPIResponse(resultString string, warnings promv1.Warnings, truncationLimit int) (string, error) {
	truncatedResult, truncated := truncateStringByLines(resultString, truncationLimit)
	if truncated {
		resultString = truncatedResult + displayTruncationWarning(truncationLimit)
	}
	return s.FormatOutput(queryAPIResponse{
		Result:   resultString,
		Warnings: warnings,
	})
}

func (s *ServerContainer) queryAPICall(ctx context.Context, query string, ts time.Time, truncationLimit int) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/query"
	startTs := time.Now()
	result, warnings, err := client.Query(ctx, query, ts)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("执行瞬时查询失败: %w", wrapErrorIfNotFound(err, path))
	}

	return s.formatTruncatedQueryAPIResponse(result.String(), warnings, truncationLimit)
}

func (s *ServerContainer) rangeQueryAPICall(ctx context.Context, query string, start, end time.Time, step time.Duration, truncationLimit int) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/query_range"
	startTs := time.Now()
	result, warnings, err := client.QueryRange(ctx, query, promv1.Range{Start: start, End: end, Step: step})
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("执行范围查询失败: %w", wrapErrorIfNotFound(err, path))
	}

	return s.formatTruncatedQueryAPIResponse(result.String(), warnings, truncationLimit)
}

func (s *ServerContainer) exemplarQueryAPICall(ctx context.Context, query string, start, end time.Time, truncationLimit int) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/query_exemplars"
	startTs := time.Now()
	res, err := client.QueryExemplars(ctx, query, start, end)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("执行示例查询失败: %w", wrapErrorIfNotFound(err, path))
	}

	var resultSB strings.Builder
	for _, r := range res {
		b, err := json.Marshal(r)
		if err != nil {
			return "", fmt.Errorf("序列化示例失败: %w", err)
		}
		resultSB.Write(b)
		resultSB.WriteString("\n")
	}
	return s.formatTruncatedQueryAPIResponse(resultSB.String(), nil, truncationLimit)
}

func (s *ServerContainer) seriesAPICall(ctx context.Context, matches []string, start, end time.Time, truncationLimit int) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/series"
	startTs := time.Now()
	result, warnings, err := client.Series(ctx, matches, start, end)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("获取序列失败: %w", wrapErrorIfNotFound(err, path))
	}

	lsets := make([]string, len(result))
	for i, lset := range result {
		lsets[i] = lset.String()
	}

	return s.formatTruncatedQueryAPIResponse(strings.Join(lsets, "\n"), warnings, truncationLimit)
}

func (s *ServerContainer) labelNamesAPICall(ctx context.Context, matches []string, start, end time.Time, truncationLimit int) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/labels"
	startTs := time.Now()
	result, warnings, err := client.LabelNames(ctx, matches, start, end)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("获取标签名称失败: %w", wrapErrorIfNotFound(err, path))
	}

	return s.formatTruncatedQueryAPIResponse(strings.Join(result, "\n"), warnings, truncationLimit)
}

func (s *ServerContainer) labelValuesAPICall(ctx context.Context, label string, matches []string, start, end time.Time, truncationLimit int) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/label/:name/values"
	startTs := time.Now()
	result, warnings, err := client.LabelValues(ctx, label, matches, start, end)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("获取标签值失败: %w", wrapErrorIfNotFound(err, path))
	}

	lvals := make([]string, len(result))
	for i, lval := range result {
		lvals[i] = string(lval)
	}

	return s.formatTruncatedQueryAPIResponse(strings.Join(lvals, "\n"), warnings, truncationLimit)
}

func (s *ServerContainer) metricMetadataAPICall(ctx context.Context, metric, limit string) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/metadata"
	startTs := time.Now()

	truncationLimit := s.truncationLimit
	if limit == "" && truncationLimit != 0 {
		limit = strconv.Itoa(truncationLimit)
	}

	limitInt := 0
	if limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			return "", fmt.Errorf("转换限制为整数失败: %w", err)
		}
		if n < 0 {
			// 在其他工具的 `truncation_limit` 字段中，`-1` 表示无限制。
			// 如果这里给了负数，将其规范化为 0 用于 prom API 调用以将其视为无限制。
			n = 0
		}
		limitInt = n
	}

	if limitInt == 0 {
		limit = ""
	}

	mm, err := client.Metadata(ctx, metric, limit)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("从 Prometheus 获取指标元数据失败: %w", wrapErrorIfNotFound(err, path))
	}

	encodedData, err := s.FormatOutput(mm)
	if err != nil {
		return "", fmt.Errorf("编码指标元数据失败: %w", err)
	}

	if limitInt != 0 {
		encodedData += displayTruncationWarning(limitInt)
	}

	return encodedData, nil
}

func (s *ServerContainer) targetsMetadataAPICall(ctx context.Context, matchTarget, metric, limit string) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/targets/metadata"
	startTs := time.Now()

	truncationLimit := s.truncationLimit
	if limit == "" && truncationLimit != 0 {
		limit = strconv.Itoa(truncationLimit)
	}

	limitInt := 0
	if limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil {
			return "", fmt.Errorf("转换限制为整数失败: %w", err)
		}
		if n < 0 {
			// 在其他工具的 `truncation_limit` 字段中，`-1` 表示无限制。
			// 如果这里给了负数，将其规范化为 0 用于 prom API 调用以将其视为无限制。
			n = 0
		}
		limitInt = n
	}

	if limitInt == 0 {
		limit = ""
	}

	tm, err := client.TargetsMetadata(ctx, matchTarget, metric, limit)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("从 Prometheus 获取目标元数据失败: %w", wrapErrorIfNotFound(err, path))
	}

	encodedData, err := s.FormatOutput(tm)
	if err != nil {
		return "", fmt.Errorf("编码目标元数据失败: %w", err)
	}

	if limitInt != 0 {
		encodedData += displayTruncationWarning(limitInt)
	}

	return encodedData, nil
}

// doSimpleAPICall 封装了 Prometheus API 调用的通用模式，
// 除了上下文之外不需要参数：获取客户端，设置超时，记录指标，
// 调用 API，并格式化结果
func (s *ServerContainer) doSimpleAPICall(ctx context.Context, path, errMsg string, call func(context.Context, promv1.API) (any, error)) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	startTs := time.Now()
	result, err := call(ctx, client)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("%s: %w", errMsg, wrapErrorIfNotFound(err, path))
	}

	return s.FormatOutput(result)
}

func (s *ServerContainer) alertmanagersAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/alertmanagers", "从 Prometheus 获取 alertmanager 状态失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.AlertManagers(ctx)
		})
}

func (s *ServerContainer) flagsAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/status/flags", "从 Prometheus 获取运行时标志失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.Flags(ctx)
		})
}

func (s *ServerContainer) listAlertsAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/alerts", "从 Prometheus 获取告警失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.Alerts(ctx)
		})
}

func (s *ServerContainer) tsdbStatsAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/status/tsdb", "从 Prometheus 获取 tsdb 统计失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.TSDB(ctx)
		})
}

func (s *ServerContainer) buildinfoAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/status/buildinfo", "从 Prometheus 获取构建信息失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.Buildinfo(ctx)
		})
}

func (s *ServerContainer) configAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/status/config", "从 Prometheus 获取配置失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.Config(ctx)
		})
}

func (s *ServerContainer) runtimeinfoAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/status/runtimeinfo", "从 Prometheus 获取运行时信息失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.Runtimeinfo(ctx)
		})
}

func (s *ServerContainer) rulesAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/rules", "从 Prometheus 获取规则失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.Rules(ctx)
		})
}

func (s *ServerContainer) targetsAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/targets", "从 Prometheus 获取目标失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.Targets(ctx)
		})
}

func (s *ServerContainer) walReplayAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/status/walreplay", "从 Prometheus 获取 WAL 重放状态失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return client.WalReplay(ctx)
		})
}

func (s *ServerContainer) cleanTombstonesAPICall(ctx context.Context) (string, error) {
	return s.doSimpleAPICall(ctx, "/api/v1/admin/tsdb/clean_tombstones", "从 Prometheus 清理墓碑失败",
		func(ctx context.Context, client promv1.API) (any, error) {
			return "success", client.CleanTombstones(ctx)
		})
}

func (s *ServerContainer) deleteSeriesAPICall(ctx context.Context, matches []string, start, end time.Time) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/admin/tsdb/delete_series"
	startTs := time.Now()
	err := client.DeleteSeries(ctx, matches, start, end)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("从 Prometheus 删除序列失败: %w", wrapErrorIfNotFound(err, path))
	}

	return "success", nil
}

func (s *ServerContainer) snapshotAPICall(ctx context.Context, skipHead bool) (string, error) {
	client, _ := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/admin/tsdb/snapshot"
	startTs := time.Now()
	ss, err := client.Snapshot(ctx, skipHead)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return "", fmt.Errorf("创建 Prometheus 快照失败: %w", wrapErrorIfNotFound(err, path))
	}

	return s.FormatOutput(ss)
}

func (s *ServerContainer) doManagementAPICall(ctx context.Context, method, path string) (string, error) {
	_, rt := s.GetAPIClient(ctx)
	ctx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	data, err := s.doHTTPRequest(ctx, method, rt, path, false)
	if err != nil {
		return "", fmt.Errorf("向 %s 发起 Prometheus 管理 API 调用失败: %w", path, err)
	}

	return strings.Trim(data, "\n\""), nil
}

// doHTTPRequest 使用提供的 round tripper 发起 HTTP 请求
func (s *ServerContainer) doHTTPRequest(ctx context.Context, method string, rt http.RoundTripper, requestPath string, expectJSON bool) (string, error) {
	fullPath, err := url.JoinPath(s.prometheusURL, requestPath)
	if err != nil {
		return "", fmt.Errorf("构造请求 URL 失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullPath, nil)
	if err != nil {
		return "", fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	// 重用默认传输的缓存客户端以共享其空闲连接池
	// 对于覆盖认证的传输，创建一次性客户端
	var httpClient *http.Client
	if s.defaultHTTPClient.Transport != nil && rt == s.defaultHTTPClient.Transport {
		httpClient = &s.defaultHTTPClient
	} else {
		httpClient = &http.Client{Transport: rt}
	}

	startTs := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("发起 HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()
	metricAPICallDuration.With(prometheus.Labels{"target_path": requestPath}).Observe(time.Since(startTs).Seconds())

	if resp.StatusCode != http.StatusOK {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": requestPath}).Inc()
		if resp.StatusCode == http.StatusNotFound {
			return "", &ErrEndpointNotSupported{
				Endpoint:   requestPath,
				StatusCode: resp.StatusCode,
			}
		}
		return "", fmt.Errorf("收到非 OK HTTP 状态码: %d", resp.StatusCode)
	}

	// TODO(@tjhop): 添加 io.LimitReader 并强制执行最大响应体大小？
	// 它应该是用户可配置的（标志）吗？
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应体失败: %w", err)
	}

	var data any
	if expectJSON {
		err = json.Unmarshal(body, &data)
		if err != nil {
			return "", fmt.Errorf("反序列化 JSON 响应失败: %w", err)
		}
	} else {
		data = string(body)
	}

	return s.FormatOutput(data)
}

// ==================== 主机指标辅助函数 ====================

// buildInstanceMatcher 根据节点名称或 IP 地址构建 Prometheus 标签匹配器字符串
func buildInstanceMatcher(node, ip string) string {
	if ip != "" {
		return fmt.Sprintf(`instance=~"%s:.*"`, ip)
	} else if node != "" {
		return fmt.Sprintf(`instance=~"%s:.*"`, node)
	}
	return ""
}

// queryPrometheusValue 执行 Prometheus 瞬时查询并返回第一个数值结果。
// 使用类型化的 Prometheus 客户端 API，避免字符串序列化导致的精度丢失和解析错误。
func (s *ServerContainer) queryPrometheusValue(ctx context.Context, query string) (float64, error) {
	client, _ := s.GetAPIClient(ctx)
	queryCtx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/query"
	startTs := time.Now()
	result, _, err := client.Query(queryCtx, query, time.Now())
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return 0, fmt.Errorf("执行瞬时查询失败: %w", wrapErrorIfNotFound(err, path))
	}

	switch v := result.(type) {
	case model.Vector:
		if len(v) > 0 {
			return float64(v[0].Value), nil
		}
	case *model.Scalar:
		return float64(v.Value), nil
	}

	return 0, fmt.Errorf("查询结果为空或类型不匹配")
}

// queryPrometheusRangeLatest 执行 Prometheus 范围查询并返回最新的数值结果。
func (s *ServerContainer) queryPrometheusRangeLatest(ctx context.Context, query string, start, end time.Time, step time.Duration) (float64, error) {
	client, _ := s.GetAPIClient(ctx)
	queryCtx, cancel := context.WithTimeout(ctx, s.apiTimeout)
	defer cancel()

	path := "/api/v1/query_range"
	startTs := time.Now()
	result, _, err := client.QueryRange(queryCtx, query, promv1.Range{Start: start, End: end, Step: step})
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return 0, fmt.Errorf("执行范围查询失败: %w", wrapErrorIfNotFound(err, path))
	}

	switch v := result.(type) {
	case model.Matrix:
		if len(v) > 0 && len(v[0].Values) > 0 {
			return float64(v[0].Values[len(v[0].Values)-1].Value), nil
		}
	}

	return 0, fmt.Errorf("查询结果为空或类型不匹配")
}

// ==================== 主机指标处理程序 ====================

// HostMetricsRealtimeHandler 处理主机指标实时查询工具
func (s *ServerContainer) HostMetricsRealtimeHandler(ctx context.Context, req *mcp.CallToolRequest, input HostMetricsRealtimeInput) (*mcp.CallToolResult, any, error) {
	if input.Node == "" && input.IP == "" {
		return newToolErrorResult("必须提供 node 或 ip 参数"), nil, nil
	}

	if input.Node != "" && input.IP != "" && input.Node != input.IP {
		return newToolErrorResult(fmt.Sprintf("node 与 ip 参数冲突: node=%q, ip=%q，两者值不同时请只提供一个", input.Node, input.IP)), nil, nil
	}

	matcher := buildInstanceMatcher(input.Node, input.IP)
	if matcher == "" {
		return newToolErrorResult("构建实例匹配器失败"), nil, nil
	}

	results := make(map[string]interface{})
	var errors []string

	// ========== 12 项主机性能指标实时查询（并发执行）==========

	var mu sync.Mutex
	var wg sync.WaitGroup

	setResult := func(key string, val interface{}, errMsg string) {
		mu.Lock()
		defer mu.Unlock()
		if errMsg != "" {
			errors = append(errors, errMsg)
			results[key] = nil
		} else {
			results[key] = val
		}
	}

	// [1/12] CPU 核数
	wg.Add(1)
	go func() {
		defer wg.Done()
		val, err := s.queryPrometheusValue(ctx, fmt.Sprintf(`count by(instance) (node_cpu_seconds_total{mode="idle",%s})`, matcher))
		if err != nil {
			setResult("cpu_cores", nil, "cpu_cores: "+err.Error())
		} else {
			setResult("cpu_cores", val, "")
		}
	}()

	// [2/12] CPU 使用率
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`100 - (avg by(instance) (irate(node_cpu_seconds_total{mode="idle",%s}[1m])) * 100)`, matcher)
		val, err := s.queryPrometheusValue(ctx, query)
		if err != nil {
			setResult("cpu_usage_percent", nil, "cpu: "+err.Error())
		} else {
			setResult("cpu_usage_percent", val, "")
		}
	}()

	// [3/12] 内存总量
	wg.Add(1)
	go func() {
		defer wg.Done()
		val, err := s.queryPrometheusValue(ctx, fmt.Sprintf(`node_memory_MemTotal_bytes{%s}`, matcher))
		if err != nil {
			setResult("memory_total_bytes", nil, "memory_total: "+err.Error())
		} else {
			setResult("memory_total_bytes", val, "")
		}
	}()

	// [4/12] 内存使用率
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`(1 - node_memory_MemAvailable_bytes{%s} / node_memory_MemTotal_bytes{%s}) * 100`, matcher, matcher)
		val, err := s.queryPrometheusValue(ctx, query)
		if err != nil {
			setResult("memory_usage_percent", nil, "memory: "+err.Error())
		} else {
			setResult("memory_usage_percent", val, "")
		}
	}()

	// [5/12] 磁盘容量
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`sum by(instance) (node_filesystem_size_bytes{fstype!~"tmpfs|devtmpfs",%s})`, matcher)
		val, err := s.queryPrometheusValue(ctx, query)
		if err != nil {
			setResult("disk_total_bytes", nil, "disk_total: "+err.Error())
		} else {
			setResult("disk_total_bytes", val, "")
		}
	}()

	// [6/12] 磁盘使用率
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`100 - (sum by(instance) (node_filesystem_avail_bytes{fstype!~"tmpfs|devtmpfs",%s}) / sum by(instance) (node_filesystem_size_bytes{fstype!~"tmpfs|devtmpfs",%s})) * 100`, matcher, matcher)
		val, err := s.queryPrometheusValue(ctx, query)
		if err != nil {
			setResult("disk_usage_percent", nil, "disk_usage: "+err.Error())
		} else {
			setResult("disk_usage_percent", val, "")
		}
	}()

	// [7/12] 网络连接数
	wg.Add(1)
	go func() {
		defer wg.Done()
		val, err := s.queryPrometheusValue(ctx, fmt.Sprintf(`node_netstat_Tcp_CurrEstab{%s}`, matcher))
		if err != nil {
			setResult("network_connections", nil, "network_connections: "+err.Error())
		} else {
			setResult("network_connections", val, "")
		}
	}()

	// [8/12] 网络流量
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`sum by(instance) (irate(node_network_receive_bytes_total{device!~"lo|virbr.*",%s}[1m]) + irate(node_network_transmit_bytes_total{device!~"lo|virbr.*",%s}[1m]))`, matcher, matcher)
		val, err := s.queryPrometheusValue(ctx, query)
		if err != nil {
			setResult("network_traffic_bytes_per_second", nil, "network_traffic: "+err.Error())
		} else {
			setResult("network_traffic_bytes_per_second", val, "")
		}
	}()

	// [9/12] 磁盘 IO
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`sum by(instance) (irate(node_disk_read_bytes_total{%s}[1m]) + irate(node_disk_written_bytes_total{%s}[1m]))`, matcher, matcher)
		val, err := s.queryPrometheusValue(ctx, query)
		if err != nil {
			setResult("disk_io_bytes_per_second", nil, "disk_io: "+err.Error())
		} else {
			setResult("disk_io_bytes_per_second", val, "")
		}
	}()

	// [10/12] 系统负载（1/5/15 分钟）
	wg.Add(1)
	go func() {
		defer wg.Done()
		val1, err := s.queryPrometheusValue(ctx, fmt.Sprintf(`node_load1{%s}`, matcher))
		mu.Lock()
		if err != nil {
			errors = append(errors, "load: "+err.Error())
			results["load_1m"] = nil
			results["load_5m"] = nil
			results["load_15m"] = nil
			mu.Unlock()
			return
		}
		results["load_1m"] = val1
		mu.Unlock()

		val5, err5 := s.queryPrometheusValue(ctx, fmt.Sprintf(`node_load5{%s}`, matcher))
		mu.Lock()
		if err5 != nil {
			results["load_5m"] = nil
		} else {
			results["load_5m"] = val5
		}
		mu.Unlock()

		val15, err15 := s.queryPrometheusValue(ctx, fmt.Sprintf(`node_load15{%s}`, matcher))
		mu.Lock()
		if err15 != nil {
			results["load_15m"] = nil
		} else {
			results["load_15m"] = val15
		}
		mu.Unlock()
	}()

	// [11/12] TCP 连接溢出
	wg.Add(1)
	go func() {
		defer wg.Done()
		val, err := s.queryPrometheusValue(ctx, fmt.Sprintf(`node_netstat_Tcp_ListenOverflow{%s}`, matcher))
		if err != nil {
			setResult("tcp_listen_overflow", nil, "tcp_overflow: "+err.Error())
		} else {
			setResult("tcp_listen_overflow", val, "")
		}
	}()

	// [12/12] 总 TCP 连接数
	wg.Add(1)
	go func() {
		defer wg.Done()
		val, err := s.queryPrometheusValue(ctx, fmt.Sprintf(`node_sockstat_TCP_inuse{%s}`, matcher))
		if err != nil {
			setResult("total_tcp_connections", nil, "total_tcp: "+err.Error())
		} else {
			setResult("total_tcp_connections", val, "")
		}
	}()

	// 等待所有并发查询完成
	wg.Wait()

	if input.Node != "" {
		results["node"] = input.Node
	}
	if input.IP != "" {
		results["ip"] = input.IP
	}
	results["timestamp"] = time.Now().Format(time.RFC3339)

	if len(errors) > 0 {
		results["warnings"] = errors
	}

	// 直接使用 json.Marshal，与其他工具格式保持一致
	output, err := json.Marshal(results)
	if err != nil {
		return newToolErrorResult("格式化输出失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(string(output)), nil, nil
}

// HostMetricsTrendHandler 处理主机指标趋势查询工具
func (s *ServerContainer) HostMetricsTrendHandler(ctx context.Context, req *mcp.CallToolRequest, input HostMetricsTrendInput) (*mcp.CallToolResult, any, error) {
	if input.Node == "" && input.IP == "" {
		return newToolErrorResult("必须提供 node 或 ip 参数"), nil, nil
	}

	if input.Node != "" && input.IP != "" && input.Node != input.IP {
		return newToolErrorResult(fmt.Sprintf("node 与 ip 参数冲突: node=%q, ip=%q，两者值不同时请只提供一个", input.Node, input.IP)), nil, nil
	}

	endTs, err := parseTimeWithDefault(input.EndTime, time.Now())
	if err != nil {
		return newToolErrorResult(fmt.Sprintf("解析 end_time 失败: %v", err)), nil, nil
	}

	defaultStart := endTs.Add(-1 * time.Hour)
	startTs, err := parseTimeWithDefault(input.StartTime, defaultStart)
	if err != nil {
		return newToolErrorResult(fmt.Sprintf("解析 start_time 失败: %v", err)), nil, nil
	}

	var step time.Duration
	if input.Step != "" {
		parsedModelStep, err := model.ParseDuration(input.Step)
		if err != nil {
			return newToolErrorResult(fmt.Sprintf("解析步长失败: %v", err)), nil, nil
		}
		parsedStep := time.Duration(parsedModelStep)
		if parsedStep <= 0 {
			return newToolErrorResult("步长必须是正的持续时间（例如 '30s', '5m', '1h', '1d'）"), nil, nil
		}
		step = parsedStep
	} else {
		resolution := math.Max(math.Floor(endTs.Sub(startTs).Seconds()/defaultRangeQueryDataPoints), 1)
		step = time.Duration(resolution) * time.Second
	}

	matcher := buildInstanceMatcher(input.Node, input.IP)
	if matcher == "" {
		return newToolErrorResult("构建实例匹配器失败"), nil, nil
	}

	results := make(map[string]interface{})
	var warnings []string

	// ========== 12 项主机性能指标趋势查询（并发执行）==========

	var mu sync.Mutex
	var wg sync.WaitGroup

	setLatest := func(latestKey string, val interface{}) {
		mu.Lock()
		results[latestKey] = val
		mu.Unlock()
	}

	// [1/12] CPU 核数
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`count by(instance) (node_cpu_seconds_total{mode="idle",%s})`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "cpu_cores: "+err.Error())
			results["cpu_cores"] = nil
			results["cpu_cores_latest"] = nil
			mu.Unlock()
			return
		}
		results["cpu_cores"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("cpu_cores_latest", latest)
	}()

	// [2/12] CPU 使用率
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`100 - (avg by(instance) (irate(node_cpu_seconds_total{mode="idle",%s}[1m])) * 100)`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "cpu: "+err.Error())
			results["cpu_usage_percent"] = nil
			results["cpu_usage_percent_latest"] = nil
			mu.Unlock()
			return
		}
		results["cpu_usage_percent"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("cpu_usage_percent_latest", latest)
	}()

	// [3/12] 内存总量
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`node_memory_MemTotal_bytes{%s}`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "memory_total: "+err.Error())
			results["memory_total_bytes"] = nil
			results["memory_total_bytes_latest"] = nil
			mu.Unlock()
			return
		}
		results["memory_total_bytes"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("memory_total_bytes_latest", latest)
	}()

	// [4/12] 内存使用率
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`(1 - node_memory_MemAvailable_bytes{%s} / node_memory_MemTotal_bytes{%s}) * 100`, matcher, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "memory: "+err.Error())
			results["memory_usage_percent"] = nil
			results["memory_usage_percent_latest"] = nil
			mu.Unlock()
			return
		}
		results["memory_usage_percent"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("memory_usage_percent_latest", latest)
	}()

	// [5/12] 磁盘容量
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`sum by(instance) (node_filesystem_size_bytes{fstype!~"tmpfs|devtmpfs",%s})`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "disk_total: "+err.Error())
			results["disk_total_bytes"] = nil
			results["disk_total_bytes_latest"] = nil
			mu.Unlock()
			return
		}
		results["disk_total_bytes"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("disk_total_bytes_latest", latest)
	}()

	// [6/12] 磁盘使用率
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`100 - (sum by(instance) (node_filesystem_avail_bytes{fstype!~"tmpfs|devtmpfs",%s}) / sum by(instance) (node_filesystem_size_bytes{fstype!~"tmpfs|devtmpfs",%s})) * 100`, matcher, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "disk_usage: "+err.Error())
			results["disk_usage_percent"] = nil
			results["disk_usage_percent_latest"] = nil
			mu.Unlock()
			return
		}
		results["disk_usage_percent"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("disk_usage_percent_latest", latest)
	}()

	// [7/12] 网络连接数
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`node_netstat_Tcp_CurrEstab{%s}`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "network_connections: "+err.Error())
			results["network_connections"] = nil
			results["network_connections_latest"] = nil
			mu.Unlock()
			return
		}
		results["network_connections"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("network_connections_latest", latest)
	}()

	// [8/12] 网络流量
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`sum by(instance) (irate(node_network_receive_bytes_total{device!~"lo|virbr.*",%s}[1m]) + irate(node_network_transmit_bytes_total{device!~"lo|virbr.*",%s}[1m]))`, matcher, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "network_traffic: "+err.Error())
			results["network_traffic_bytes_per_second"] = nil
			results["network_traffic_bytes_per_second_latest"] = nil
			mu.Unlock()
			return
		}
		results["network_traffic_bytes_per_second"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("network_traffic_bytes_per_second_latest", latest)
	}()

	// [9/12] 磁盘 IO（读取+写入，字节/秒）
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`sum by(instance) (irate(node_disk_read_bytes_total{%s}[1m]) + irate(node_disk_written_bytes_total{%s}[1m]))`, matcher, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "disk_io: "+err.Error())
			results["disk_io_bytes_per_second"] = nil
			results["disk_io_bytes_per_second_latest"] = nil
			mu.Unlock()
			return
		}
		results["disk_io_bytes_per_second"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("disk_io_bytes_per_second_latest", latest)
	}()

	// [10/12] 系统负载 1 分钟
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`node_load1{%s}`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "load_1m: "+err.Error())
			results["load_1m"] = nil
			results["load_1m_latest"] = nil
			mu.Unlock()
			return
		}
		results["load_1m"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("load_1m_latest", latest)
	}()

	// 系统负载 5 分钟
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`node_load5{%s}`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "load_5m: "+err.Error())
			results["load_5m"] = nil
			results["load_5m_latest"] = nil
			mu.Unlock()
			return
		}
		results["load_5m"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("load_5m_latest", latest)
	}()

	// 系统负载 15 分钟
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`node_load15{%s}`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "load_15m: "+err.Error())
			results["load_15m"] = nil
			results["load_15m_latest"] = nil
			mu.Unlock()
			return
		}
		results["load_15m"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("load_15m_latest", latest)
	}()

	// [11/12] TCP 连接溢出
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`node_netstat_Tcp_ListenOverflow{%s}`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "tcp_overflow: "+err.Error())
			results["tcp_listen_overflow"] = nil
			results["tcp_listen_overflow_latest"] = nil
			mu.Unlock()
			return
		}
		results["tcp_listen_overflow"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("tcp_listen_overflow_latest", latest)
	}()

	// [12/12] 总 TCP 连接数
	wg.Add(1)
	go func() {
		defer wg.Done()
		query := fmt.Sprintf(`node_sockstat_TCP_inuse{%s}`, matcher)
		result, err := s.rangeQueryAPICall(ctx, query, startTs, endTs, step, s.truncationLimit)
		mu.Lock()
		if err != nil {
			warnings = append(warnings, "total_tcp: "+err.Error())
			results["total_tcp_connections"] = nil
			results["total_tcp_connections_latest"] = nil
			mu.Unlock()
			return
		}
		results["total_tcp_connections"] = result
		mu.Unlock()
		latest, _ := s.queryPrometheusRangeLatest(ctx, query, startTs, endTs, step)
		setLatest("total_tcp_connections_latest", latest)
	}()

	// 等待所有并发查询完成
	wg.Wait()

	if input.Node != "" {
		results["node"] = input.Node
	}
	if input.IP != "" {
		results["ip"] = input.IP
	}
	results["start_time"] = startTs.Format(time.RFC3339)
	results["end_time"] = endTs.Format(time.RFC3339)
	results["step"] = step.String()

	if len(warnings) > 0 {
		results["warnings"] = warnings
	}

	// 直接使用 json.Marshal，与其他工具格式保持一致
	output, err := json.Marshal(results)
	if err != nil {
		return newToolErrorResult("格式化输出失败: " + err.Error()), nil, nil
	}
	return newToolTextResult(string(output)), nil, nil
}
