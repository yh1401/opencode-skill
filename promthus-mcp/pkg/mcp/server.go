package mcp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alpkeskin/gotoon"
	"github.com/blevesearch/bleve/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/promslog"
	"github.com/tmc/langchaingo/textsplitter"

	"github.com/tjhop/prometheus-mcp-server/internal/metrics"
	"github.com/tjhop/prometheus-mcp-server/internal/version"
	mcpProm "github.com/tjhop/prometheus-mcp-server/pkg/prometheus"
)

// MCP 服务器的嵌入式资源
//
//go:embed assets/*
var assets embed.FS

// bleveSetLogOnce 确保 Bleve 日志记录器只配置一次，
// 防止多个 goroutine 调用 buildDocsState 时出现竞态条件
var bleveSetLogOnce sync.Once

// MCP 服务器监控的 Prometheus 指标
var (
	metricServerReady = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(metrics.MetricNamespace, "server", "ready"),
			Help: "MCP 服务器就绪状态指标，就绪时为 '1'，否则为 '0'",
		},
	)

	metricToolCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:                        prometheus.BuildFQName(metrics.MetricNamespace, "tool", "call_duration_seconds"),
			Help:                        "工具调用持续时间（秒），按工具统计",
			Buckets:                     prometheus.ExponentialBuckets(0.25, 2, 10),
			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"tool_name"},
	)

	metricToolCallsFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(metrics.MetricNamespace, "tool", "calls_failed_total"),
			Help: "每个工具的失败调用总数",
		},
		[]string{"tool_name"},
	)

	metricResourceCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:                        prometheus.BuildFQName(metrics.MetricNamespace, "resource", "call_duration_seconds"),
			Help:                        "资源调用持续时间（秒），按资源统计",
			Buckets:                     prometheus.ExponentialBuckets(0.25, 2, 10),
			NativeHistogramBucketFactor: 1.1,
		},
		[]string{"resource_uri"},
	)

	metricResourceCallsFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(metrics.MetricNamespace, "resource", "calls_failed_total"),
			Help: "每个资源的失败调用总数",
		},
		[]string{"resource_uri"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		metricServerReady,
		metricToolCallDuration,
		metricToolCallsFailed,
		metricResourceCallDuration,
		metricResourceCallsFailed,
	)
}

// ServerConfig 保存创建新 MCP 服务器的配置
type ServerConfig struct {
	Logger                *slog.Logger
	PrometheusURL         string
	PrometheusBackend     string
	PrometheusTimeout     time.Duration
	TruncationLimit       int
	RoundTripper          http.RoundTripper
	TSDBAdminToolsEnabled bool
	EnabledTools          []string
	DocsFS                fs.FS
	ToonOutputEnabled     bool
	ClientLoggingEnabled  bool
	KeepAlive             time.Duration
	McpStateless          bool

	// easyviews 平台集成配置
	EasyviewsURL       string
	EasyviewsProvince  string
	EasyviewsAppID     string
	EasyviewsAppSecret string
	EasyviewsTimeout   time.Duration
}

// NewServer 使用官方 Go SDK 创建新的 MCP 服务器
func NewServer(ctx context.Context, cfg ServerConfig) (*mcp.Server, *ServerContainer, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = promslog.NewNopLogger()
	}

	// 从嵌入式资源加载说明文档
	coreInstructions, err := assets.ReadFile("assets/instructions.md")
	if err != nil {
		logger.Error("从嵌入式资源读取说明文档失败", "err", err)
		coreInstructions = []byte("Prometheus MCP Server")
	}
	instrx := string(coreInstructions)

	container, err := newServerContainer(cfg)
	if err != nil {
		return nil, nil, err
	}

	serverOpts := &mcp.ServerOptions{
		Instructions: instrx,
		Logger:       logger.WithGroup("go_sdk_logger"),
		KeepAlive:    cfg.KeepAlive,
	}
	if cfg.McpStateless {
		// 无状态模式：GetSessionID 返回空字符串，
		// 使 SDK 不下发 Mcp-Session-Id 响应头（仅设置 StreamableHTTPOptions.Stateless 不足以抑制该头部）。
		serverOpts.GetSessionID = func() string { return "" }
	}

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "prometheus-mcp-server",
			Title:   "Prometheus MCP Server",
			Version: version.Info(),
		},
		serverOpts,
	)

	// 根据配置选择合适的工具集
	toolsetMap := getToolset(toolsetConfig{
		enabledTools:      cfg.EnabledTools,
		prometheusBackend: cfg.PrometheusBackend,
		logger:            logger,
	})
	toolset := toolsetToToolRegistrationSlice(toolsetMap)

	// 注册工具
	registerTools(server, container, toolset)

	// 注册资源
	registerResources(server, container)

	// 添加用于指标和日志的遥测中间件
	server.AddReceivingMiddleware(telemetryMiddleware(logger))

	logger.Info("MCP 服务器创建成功",
		"prometheus_url", cfg.PrometheusURL,
		"tool_count", len(toolset),
	)

	return server, container, nil
}

// NewStreamableHTTPHandler 为 MCP 服务器创建 HTTP 处理器
// 它包装处理器并添加认证上下文中间件以转发 Authorization 头部
// 当 stateless 为 true 时启用无状态模式：不再校验 Mcp-Session-Id，并禁用 GET(SSE)。
// 注意：要真正不再下发 Mcp-Session-Id 响应头，还需将 ServerConfig.McpStateless 置为 true。
func NewStreamableHTTPHandler(server *mcp.Server, logger *slog.Logger, sessionTimeout time.Duration, stateless bool) http.Handler {
	if sessionTimeout == 0 {
		// sessionTimeout 为 0 表示会话永不关闭
		// 如果未设置，则设置默认值
		sessionTimeout = 1 * time.Hour
	}

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			return server
		},
		&mcp.StreamableHTTPOptions{
			SessionTimeout: sessionTimeout,
			Stateless:      stateless,
			Logger:         logger,
		},
	)

	// 使用认证上下文中间件包装
	return authContextMiddleware(handler)
}

// authHeaderKey 是存储 Authorization 头部的上下文键
type authHeaderKey struct{}

// addAuthToContext 将 Authorization 头部值添加到上下文中
func addAuthToContext(ctx context.Context, auth string) context.Context {
	return context.WithValue(ctx, authHeaderKey{}, auth)
}

// getAuthFromContext 从上下文中检索 Authorization 头部
func getAuthFromContext(ctx context.Context) string {
	if auth, ok := ctx.Value(authHeaderKey{}).(string); ok {
		return auth
	}
	return ""
}

// authContextMiddleware 创建一个 HTTP 中间件，从请求中提取 Authorization
// 头部并将其添加到请求上下文中
func authContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if auth := r.Header.Get("Authorization"); auth != "" {
			ctx = addAuthToContext(ctx, auth)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// docsState 保存文档文件系统和搜索索引
// 设计用于原子替换以支持文档的实时更新
type docsState struct {
	fs          fs.FS
	searchIndex bleve.Index
}

// ServerContainer 保存工具和资源处理程序所需的所有依赖项
// 它用显式依赖注入替代了 mcp-go 库所需的基于中间件的上下文传递模式
type ServerContainer struct {
	// 核心依赖
	logger            *slog.Logger
	defaultAPIClient  promv1.API
	prometheusURL     string
	defaultRT         http.RoundTripper
	defaultHTTPClient http.Client

	// MCP 服务器需要使用/关心的配置值
	truncationLimit       int
	toonOutputEnabled     bool
	tsdbAdminToolsEnabled bool
	apiTimeout            time.Duration
	clientLoggingEnabled  bool

	// 文档状态管理
	docsMu sync.RWMutex
	docs   *docsState

	// easyviews 平台集成依赖
	easyviewsBaseURL    string
	easyviewsProvince   string
	easyviewsAppID      string
	easyviewsAppSecret  string
	easyviewsTimeout    time.Duration
	easyviewsHTTPClient *http.Client
}

// newServerContainer 使用给定配置创建新的 ServerContainer
func newServerContainer(cfg ServerConfig) (*ServerContainer, error) {
	client, err := mcpProm.NewAPIClient(cfg.PrometheusURL, cfg.RoundTripper)
	if err != nil {
		return nil, fmt.Errorf("创建默认 API 客户端失败: %w", err)
	}

	container := &ServerContainer{
		logger:                cfg.Logger,
		defaultAPIClient:      client,
		prometheusURL:         cfg.PrometheusURL,
		defaultRT:             cfg.RoundTripper,
		defaultHTTPClient:     http.Client{Transport: cfg.RoundTripper},
		truncationLimit:       cfg.TruncationLimit,
		toonOutputEnabled:     cfg.ToonOutputEnabled,
		tsdbAdminToolsEnabled: cfg.TSDBAdminToolsEnabled,
		apiTimeout:            cfg.PrometheusTimeout,
		clientLoggingEnabled:  cfg.ClientLoggingEnabled,

		easyviewsBaseURL:    cfg.EasyviewsURL,
		easyviewsProvince:   cfg.EasyviewsProvince,
		easyviewsAppID:      cfg.EasyviewsAppID,
		easyviewsAppSecret:  cfg.EasyviewsAppSecret,
		easyviewsTimeout:    cfg.EasyviewsTimeout,
		easyviewsHTTPClient: &http.Client{},
	}

	// 如果提供了文件系统，则初始化文档搜索
	if cfg.DocsFS != nil {
		state, err := buildDocsState(cfg.Logger, cfg.DocsFS)
		if err != nil {
			cfg.Logger.Error("初始化文档搜索失败", "err", err)
			// 非致命错误 - 继续运行但不启用文档搜索
		}

		container.swapDocsState(state)
	}

	return container, nil
}

// GetAPIClient 返回 Prometheus API 客户端，可选地从上下文获取认证信息
// 如果上下文中存在 Authorization 头部，则创建带有这些凭据的新客户端
// 否则返回默认客户端
func (s *ServerContainer) GetAPIClient(ctx context.Context) (promv1.API, http.RoundTripper) {
	auth := getAuthFromContext(ctx)
	if auth != "" {
		client, rt := s.createClientWithAuth(auth)
		if client != nil {
			return client, rt
		}
		s.logger.Warn("使用提供的认证信息创建客户端失败，回退到默认客户端")
	}

	return s.defaultAPIClient, s.defaultRT
}

// createClientWithAuth 使用给定的 Authorization 头部创建新的 API 客户端
func (s *ServerContainer) createClientWithAuth(authorization string) (promv1.API, http.RoundTripper) {
	var authType, secret string
	if strings.Contains(authorization, " ") {
		parts := strings.SplitN(authorization, " ", 2)
		authType = parts[0]
		secret = parts[1]
	} else {
		s.logger.Debug("假设 Authorization 头部没有指定类型，使用 Bearer 认证")
		authType = "Bearer"
		secret = authorization
	}

	rt := config.NewAuthorizationCredentialsRoundTripper(authType, config.NewInlineSecret(secret), s.defaultRT)
	client, err := mcpProm.NewAPIClient(s.prometheusURL, rt)
	if err != nil {
		s.logger.Error("使用凭据创建 API 客户端失败", "err", err)
		return nil, nil
	}
	return client, rt
}

// FormatOutput 根据配置将数据编码为 JSON 或 TOON 格式
func (s *ServerContainer) FormatOutput(data any) (string, error) {
	if s.toonOutputEnabled {
		toonEncoded, err := gotoon.Encode(data)
		if err != nil {
			return "", fmt.Errorf("TOON 编码数据失败: %w", err)
		}

		return toonEncoded, nil
	}

	jsonEncoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("JSON 序列化数据失败: %w", err)
	}
	return string(jsonEncoded), nil
}

// GetEffectiveTruncationLimit 返回每次调用的限制（如果设置），否则返回全局限制
func (s *ServerContainer) GetEffectiveTruncationLimit(perCallLimit int) int {
	// 负数表示工具想要覆盖并禁用截断
	if perCallLimit < 0 {
		return 0
	}

	// 设置了 perCallLimit 意味着我们请求特定的截断限制
	if perCallLimit > 0 {
		return perCallLimit
	}

	// 否则返回全局截断限制
	return s.truncationLimit
}

// ==================== 文档搜索方法 ====================

// errDocsNotProvided 当文档文件系统未配置时返回
var errDocsNotProvided = errors.New("未提供文档文件系统")

// swapDocsState 用新状态替换当前文档状态
// 它获取写锁，成功交换新文档后才尝试关闭旧文档搜索索引
func (s *ServerContainer) swapDocsState(state *docsState) {
	s.docsMu.Lock()
	oldState := s.docs
	s.docs = state
	s.docsMu.Unlock()

	if oldState != nil && oldState.searchIndex != nil {
		if err := oldState.searchIndex.Close(); err != nil {
			s.logger.Error("关闭旧搜索索引失败", "err", err)
		}
	}
}

// buildDocsState 从给定文件系统创建新的 docsState
// 它将 markdown 文件分块并构建搜索索引
func buildDocsState(logger *slog.Logger, docsFS fs.FS) (*docsState, error) {
	if docsFS == nil {
		return nil, errDocsNotProvided
	}

	splitter := textsplitter.NewMarkdownTextSplitter(
		textsplitter.WithAllowedSpecial([]string{"all"}),
		textsplitter.WithChunkOverlap(docChunkOverlap),
		textsplitter.WithChunkSize(docChunkSize),
		textsplitter.WithCodeBlocks(true),
		textsplitter.WithHeadingHierarchy(true),
		textsplitter.WithJoinTableRows(true),
		textsplitter.WithKeepSeparator(true),
		textsplitter.WithReferenceLinks(true),
	)

	docFiles, err := getDocFileNames(docsFS)
	if err != nil {
		return nil, fmt.Errorf("列出文档文件失败: %w", err)
	}

	// 只配置一次 bleve 日志记录器，避免并发构建多个文档状态时出现竞态条件
	bleveSetLogOnce.Do(func() {
		bleve.SetLog(slog.NewLogLogger(logger.Handler(), slog.LevelDebug))
	})
	mapping := bleve.NewIndexMapping()
	searchIndex, err := bleve.NewMemOnly(mapping)
	if err != nil {
		return nil, fmt.Errorf("创建内存搜索索引失败: %w", err)
	}

	for _, fn := range docFiles {
		content, err := getDocFileContent(docsFS, fn)
		if err != nil {
			logger.Error("读取文档文件失败", "file", fn, "err", err)
			continue
		}

		textChunks, err := splitter.SplitText(content)
		if err != nil {
			logger.Error("将文档分块失败", "file", fn, "err", err)
			continue
		}

		for i, c := range textChunks {
			newChunk := chunk{
				ID:      i + 1,
				Name:    fn,
				Content: c,
			}
			if err := searchIndex.Index(newChunk.String(), newChunk); err != nil {
				logger.Error("索引分块失败", "chunk_id", newChunk.String(), "err", err)
				continue
			}
		}
	}

	return &docsState{
		fs:          docsFS,
		searchIndex: searchIndex,
	}, nil
}

// SearchDocs 搜索文档索引并返回匹配的分块 ID
func (s *ServerContainer) SearchDocs(q string, limit int) ([]string, error) {
	s.docsMu.RLock()
	defer s.docsMu.RUnlock()

	ds := s.docs
	if ds == nil || ds.searchIndex == nil {
		return nil, errors.New("文档搜索索引未初始化")
	}

	if limit < 1 {
		limit = defaultDocsSearchLimit
	}

	query := bleve.NewMatchQuery(q)
	query.Fuzziness = 1

	req := bleve.NewSearchRequest(query)
	req.Size = limit
	req.Highlight = bleve.NewHighlight()
	req.Fields = []string{"*"}

	searchRes, err := ds.searchIndex.Search(req)
	if err != nil {
		return nil, fmt.Errorf("搜索文档索引失败: %w", err)
	}

	var result []string
	for _, hit := range searchRes.Hits {
		result = append(result, hit.ID)
	}
	return result, nil
}

// GetDocFileNames 返回所有文档文件名列表
func (s *ServerContainer) GetDocFileNames() ([]string, error) {
	s.docsMu.RLock()
	defer s.docsMu.RUnlock()

	ds := s.docs
	if ds == nil || ds.fs == nil {
		return nil, errDocsNotProvided
	}
	return getDocFileNames(ds.fs)
}

// GetDocFileContent 返回文档文件的内容
func (s *ServerContainer) GetDocFileContent(path string) (string, error) {
	s.docsMu.RLock()
	defer s.docsMu.RUnlock()

	ds := s.docs
	if ds == nil || ds.fs == nil {
		return "", errDocsNotProvided
	}
	return getDocFileContent(ds.fs, path)
}

// ==================== 日志辅助方法 ====================

// GetToolLogger 返回工具处理程序中使用的 slog.Logger
// 如果启用了客户端日志记录，返回一个链式日志记录器，同时记录到
// 应用程序的日志输出并向 MCP 客户端发送通知
// 否则返回服务器的本地日志记录器
//
// 工具名称从请求中提取并用作 MCP 客户端通知中的日志记录器名称
// 如果提供了输入（非 nil），它将被添加到日志记录器上下文中
// 输入类型应该实现 slog.LogValuer 以支持结构化日志记录
//
// 这是为了补充每个工具通过遥测中间件获得的标准日志记录
// 由于它还可以通过协议通知向 MCP 客户端发送日志，因此它旨在用于
// 应该通知用户并进行适当日志记录的重要上下文消息
// 目前主要由 TSDB 管理工具用于在管理工具调用周围进行额外日志记录
func (s *ServerContainer) GetToolLogger(req *mcp.CallToolRequest, input any) *slog.Logger {
	logger := s.logger
	if s.clientLoggingEnabled {
		logger = getChainedLogger(logger, req, req.Params.Name)
	}

	if input != nil {
		logger = logger.With(slog.Any("input", input))
	}

	return logger
}

// ==================== MCP 结果辅助方法 ====================

// newToolTextResult 创建带有文本内容的新 CallToolResult
func newToolTextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// newToolErrorResult 创建表示错误的新 CallToolResult
// 确保设置 IsError 以使客户端知道这是错误
//
// 因为遥测中间件会自动检查 IsError 字段，
// 所以所有对 newToolErrorResult 的调用都会导致工具调用失败指标增加，
// 并且会自动为工具调用失败生成错误日志
func newToolErrorResult(err string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err}},
		IsError: true,
	}
}

// embedResourceContentsInToolResult 获取 ReadResourceResult 并将其嵌入到
// CallToolResult 中，通过将新的嵌入资源追加到 CallToolResult 的内容上
// 返回修改后的 CallToolResult
// 这允许工具重用资源处理程序并将其结果作为嵌入资源返回
func embedResourceContentsInToolResult(resourceResult *mcp.ReadResourceResult, toolResult *mcp.CallToolResult) *mcp.CallToolResult {
	embeddedRes := make([]mcp.Content, len(resourceResult.Contents))
	for i, r := range resourceResult.Contents {
		embeddedRes[i] = &mcp.EmbeddedResource{Resource: r}
	}

	toolResult.Content = append(toolResult.Content, embeddedRes...)
	return toolResult
}

// concatResourceContents 将多个 ReadResourceResults 的内容连接成一个切片
// 这允许调用者根据需要使用组合内容（例如，作为工具结果中的嵌入资源）
func concatResourceContents(results ...*mcp.ReadResourceResult) []*mcp.ResourceContents {
	var allContents []*mcp.ResourceContents
	for _, result := range results {
		allContents = append(allContents, result.Contents...)
	}
	return allContents
}
