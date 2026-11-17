package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // 引入 pprof 用于性能分析
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"

	"github.com/tjhop/prometheus-mcp-server/internal/metrics"
	"github.com/tjhop/prometheus-mcp-server/internal/version"
	"github.com/tjhop/prometheus-mcp-server/pkg/mcp"
)

const (
	programName = "prometheus-mcp-server" // 程序名称
	defaultPort = 8080                    // 默认 HTTP 端口
)

var (
	//go:embed all:external/docs
	assetsDocs embed.FS // 嵌入的文档资源
	docsFs     fs.FS    // 文档文件系统

	// 配置标志定义

	// flagMcpTools 指定要加载的 MCP 工具列表
	// - "all": 加载所有工具
	// - "core": 只加载核心工具
	// - 其他: 加载指定的工具（同时包含核心工具）
	flagMcpTools = kingpin.Flag(
		"mcp.tools",
		"要加载的 MCP 工具列表。"+
			" 使用 `all` 加载所有工具。"+
			" 使用 `core` 只加载核心工具: "+strings.Join(mcp.CoreTools, ",")+
			" 否则，它将被视为要加载的工具的允许列表，同时包含核心工具。"+
			" 请参阅项目 README 了解更多信息和完整工具列表。",
	).Default("all").Strings()

	// flagMcpToonOutputEnabled 启用 TOON 格式输出（替代 JSON）
	flagMcpToonOutputEnabled = kingpin.Flag(
		"mcp.enable-toon-output",
		"启用 Token-Oriented Object Notation (TOON) 格式输出，替代 JSON",
	).Default("false").Bool()

	// flagMcpClientLogging 启用向 MCP 客户端发送日志消息
	flagMcpClientLogging = kingpin.Flag(
		"mcp.enable-client-logging",
		"启用将日志消息作为协议通知发送给连接的 MCP 客户端。"+
			" 启用后，工具执行日志将同时发送到服务器的主日志输出和 MCP 客户端，允许 LLM 观察服务器活动。",
	).Default("false").Bool()

	// flagMcpTransport 指定 MCP 服务器使用的传输类型
	flagMcpTransport = kingpin.Flag(
		"mcp.transport",
		"MCP 服务器使用的传输类型 [`stdio`, `http`]。",
	).Default("stdio").String()

	// flagPrometheusBackend 指定 Prometheus API 兼容后端
	flagPrometheusBackend = kingpin.Flag(
		"prometheus.backend",
		"为特定的 Prometheus API 兼容后端自定义工具集。"+
			" 支持的后端包括: "+strings.Join(mcp.PrometheusBackends, ","),
	).String()

	// flagPrometheusURL 指定要连接的 Prometheus 实例的 URL
	flagPrometheusURL = kingpin.Flag(
		"prometheus.url",
		"要连接的 Prometheus 实例的 URL",
	).Default("http://127.0.0.1:9090").String()

	// flagPrometheusTimeout 指定 Prometheus API 调用的超时时间
	flagPrometheusTimeout = kingpin.Flag(
		"prometheus.timeout",
		"对 Prometheus 后端的 API 调用超时时间",
	).Default("1m").Duration()

	// flagPrometheusTruncationLimit 指定查询响应的截断限制
	flagPrometheusTruncationLimit = kingpin.Flag(
		"prometheus.truncation-limit",
		"如果启用，此选项控制从 API 响应提供给 LLM 的最大查询响应大小（行数/条目数）。"+
			" LLM 可以根据需要通过支持工具的工具请求参数在每次工具调用的基础上覆盖截断限制。"+
			" 设置为 0 可禁用截断限制。",
	).Default("0").Int()

	// flagHTTPConfig 指定 HTTP 客户端配置文件路径
	flagHTTPConfig = kingpin.Flag(
		"http.config",
		"设置 Prometheus HTTP 客户端选项的配置文件路径",
	).String()

	// flagWebTelemetryPath 指定暴露指标的路径
	flagWebTelemetryPath = kingpin.Flag(
		"web.telemetry-path",
		"暴露指标的路径。",
	).Default("/metrics").String()

	// flagWebMaxRequests 指定最大并发请求数
	flagWebMaxRequests = kingpin.Flag(
		"web.max-requests",
		"最大并行抓取请求数。设置为 0 可禁用限制。",
	).Default("40").Int()

	// flagEnableTsdbAdminTools 启用 TSDB 管理工具（危险操作）
	flagEnableTsdbAdminTools = kingpin.Flag(
		"dangerous.enable-tsdb-admin-tools",
		"启用并允许使用访问 Prometheus TSDB Admin API 端点的工具"+
			"（`snapshot`、`delete_series` 和 `clean_tombstones` 工具）。"+
			" 这是危险的，允许进行删除数据等破坏性操作。"+
			" 如果您连接的 LLM 删除了所有数据，这不关此 MCP 服务器的责任。"+
			" 文档: https://prometheus.io/docs/prometheus/latest/querying/api/#tsdb-admin-apis",
	).Default("false").Bool()

	// flagMcpKeepaliveInterval 指定保持连接的 ping 间隔
	flagMcpKeepaliveInterval = kingpin.Flag(
		"mcp.keepalive-interval",
		"向连接的 MCP 会话发送保持连接 ping 的间隔。"+
			" 如果对等方未能响应，会话将关闭。"+
			" 对于 HTTP 传输最有用，可以防止空闲连接断开。",
	).Default("30s").Duration()

	// flagMcpSessionTimeout 指定 HTTP 传输会话的空闲超时时间
	flagMcpSessionTimeout = kingpin.Flag(
		"mcp.session-timeout",
		"HTTP 传输 MCP 会话的空闲超时时间。",
	).Default("10m").Duration()

	// flagMcpStateless 启用无状态 HTTP 传输（取消 SessionID）
	flagMcpStateless = kingpin.Flag(
		"mcp.stateless",
		"启用无状态（stateless）HTTP 传输：不再下发或校验 Mcp-Session-Id。"+
			" 适用于无需服务端反向调用客户端的纯工具服务。",
	).Default("false").Bool()

	// flagDocsAutoUpdate 启用文档自动更新
	flagDocsAutoUpdate = kingpin.Flag(
		"docs.auto-update",
		"启用从官方 prometheus/docs 仓库自动更新文档。"+
			" 每隔 "+mcp.DocsUpdateInterval.String()+" 检查一次。",
	).Default("false").Bool()

	// easyviews 平台集成标志（用于架构优化分析）

	// flagEasyviewsURL 指定 easyviews 平台统一网关地址
	flagEasyviewsURL = kingpin.Flag(
		"easyviews.url",
		"easyviews 平台统一网关地址（getTykjProvince 接口）。"+
			" 当 app-id 与 app-secret 均未配置时，相关工具将返回未配置错误。",
	).Default("https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince").String()

	// flagEasyviewsProvince 指定 easyviews 平台省份标识
	flagEasyviewsProvince = kingpin.Flag(
		"easyviews.province",
		"easyviews 平台省份标识（provinceDetail.province）。",
	).Default("大模型").String()

	// flagEasyviewsAppID 指定 easyviews 平台应用 ID
	flagEasyviewsAppID = kingpin.Flag(
		"easyviews.app-id",
		"easyviews 平台应用 ID（provinceDetail.appId）。",
	).String()

	// flagEasyviewsAppSecret 指定 easyviews 平台应用密钥
	flagEasyviewsAppSecret = kingpin.Flag(
		"easyviews.app-secret",
		"easyviews 平台应用密钥（provinceDetail.appSecret）。",
	).String()

	// flagEasyviewsTimeout 指定 easyviews API 调用超时时间
	flagEasyviewsTimeout = kingpin.Flag(
		"easyviews.timeout",
		"easyviews 平台 API 调用的超时时间。",
	).Default("30s").Duration()

	// flagLogToFile 指定日志文件路径
	flagLogToFile = kingpin.Flag(
		"log.file",
		"要写入日志的文件名称（文件轮转策略应使用 logrotate 等外部工具配置）",
	).String()

	// toolkitFlags Web 服务器配置标志
	toolkitFlags = kingpinflag.AddFlags(kingpin.CommandLine, fmt.Sprintf(":%d", defaultPort))
)

func main() {
	// 从嵌入文件中设置文档提交哈希（在构建时生成）。
	// 这会填充 version.DocsCommit，用于版本信息和文档更新器。
	// 该文件由 `make submodules` 或 goreleaser 的 before hook 生成。
	if data, err := assetsDocs.ReadFile("external/docs/COMMIT_HASH"); err == nil {
		version.DocsCommit = strings.TrimSpace(string(data))
	}

	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print(programName))
	kingpin.CommandLine.UsageWriter(os.Stdout)
	kingpin.HelpFlag.Short('h')
	kingpin.CommandLine.DefaultEnvars()
	kingpin.Parse()

	// 如果指定了日志文件，则设置日志输出到文件
	if *flagLogToFile != "" {
		f, err := os.OpenFile(*flagLogToFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			slog.Error("无法打开日志文件进行写入", "file", *flagLogToFile, "err", err)
			os.Exit(1)
		}
		defer f.Close()

		promslogConfig.Writer = f
	}

	logger := promslog.New(promslogConfig)
	slog.SetDefault(logger)
	logger.Info("启动 "+programName, "version", version.Version, "build_date", version.BuildDate, "commit", version.Commit, "docs_commit", version.DocsCommit, "go_version", runtime.Version())

	// 可选地加载 HTTP 配置文件以配置 Prometheus API 的 HTTP 客户端
	rt, err := getRoundTripperFromConfig(*flagHTTPConfig)
	if err != nil {
		logger.Error("加载 HTTP 配置文件失败，使用默认的 HTTP round tripper", "err", err)
	}

	ctx, rootCtxCancel := context.WithCancel(context.Background())
	defer rootCtxCancel()

	// 设置嵌入的 Prometheus 文档的静态文件服务器
	docs, err := fs.Sub(assetsDocs, "external/docs/docs")
	if err != nil {
		logger.Error("创建嵌入文档的子文件系统失败", "err", err)
	} else {
		docsFs = docs
	}

	// 创建 MCP 服务器
	mcpServer, mcpContainer, err := mcp.NewServer(ctx, mcp.ServerConfig{
		Logger:                logger,
		PrometheusURL:         *flagPrometheusURL,
		PrometheusBackend:     *flagPrometheusBackend,
		PrometheusTimeout:     *flagPrometheusTimeout,
		TruncationLimit:       *flagPrometheusTruncationLimit,
		RoundTripper:          rt,
		TSDBAdminToolsEnabled: *flagEnableTsdbAdminTools,
		EnabledTools:          *flagMcpTools,
		DocsFS:                docsFs,
		ToonOutputEnabled:     *flagMcpToonOutputEnabled,
		ClientLoggingEnabled:  *flagMcpClientLogging,
		KeepAlive:             *flagMcpKeepaliveInterval,
		McpStateless:          *flagMcpStateless,

		EasyviewsURL:       *flagEasyviewsURL,
		EasyviewsProvince:  *flagEasyviewsProvince,
		EasyviewsAppID:     *flagEasyviewsAppID,
		EasyviewsAppSecret: *flagEasyviewsAppSecret,
		EasyviewsTimeout:   *flagEasyviewsTimeout,
	})
	if err != nil {
		logger.Error("创建 MCP 服务器失败", "err", err)

		// 在退出前显式取消上下文
		rootCtxCancel()
		os.Exit(1) //nolint:gocritic
	}
	srv := initHTTPServer(logger)

	// 使用 oklog/run 管理多个并发 goroutine
	var g run.Group
	{
		// 信号处理和清理
		term := make(chan os.Signal, 1)
		signal.Notify(term, os.Interrupt, syscall.SIGTERM)
		cancel := make(chan struct{})
		g.Add(
			func() error {
				select {
				case sig := <-term:
					logger.Warn("收到信号，正在优雅退出", "signal", sig.String())
				case <-cancel:
				}

				return nil
			},
			func(err error) {
				close(cancel)
				rootCtxCancel()
			},
		)
	}
	{
		// Web 服务器（用于指标、pprof 和 HTTP 传输）
		cancel := make(chan struct{})

		g.Add(
			func() error {
				err := web.ListenAndServe(srv, toolkitFlags, logger)
				if !errors.Is(err, http.ErrServerClosed) {
					logger.Error("Web 服务器失败", "err", err)
					return err
				}

				<-cancel

				return nil
			},
			func(error) {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer shutdownCancel()
				if err := srv.Shutdown(shutdownCtx); err != nil {
					logger.Error("无法优雅关闭 Web 服务器", "err", err)
				}
				close(cancel)
				rootCtxCancel()
			},
		)
	}
	{
		// MCP 传输设置和服务器
		cancel := make(chan struct{})

		g.Add(
			func() error {
				switch *flagMcpTransport {
				case "stdio":
					logger.Debug("启动 MCP 服务器", "transport", "stdio")

					if err := mcpServer.Run(ctx, &sdkmcp.StdioTransport{}); err != nil {
						return fmt.Errorf("MCP 服务器失败: %w", err)
					}

				case "http":
					logger.Debug("启动 MCP 服务器", "transport", "http")

					httpMcpHandler := mcp.NewStreamableHTTPHandler(mcpServer, logger, *flagMcpSessionTimeout, *flagMcpStateless)
					http.Handle("/mcp", httpMcpHandler)
					<-cancel
				default:
					return fmt.Errorf("不支持的传输类型: %s", *flagMcpTransport)
				}

				return nil
			},
			func(err error) {
				close(cancel)
				rootCtxCancel()
			},
		)
	}

	// 如果启用了文档自动更新，则启动更新器
	if *flagDocsAutoUpdate {
		updater := mcp.NewDocsUpdater(logger, mcpContainer, version.DocsCommit)
		cancel := make(chan struct{})
		g.Add(
			func() error {
				logger.Info("文档自动更新器已启动", "embedded_commit", version.DocsCommit)
				ticker := time.NewTicker(mcp.DocsUpdateInterval)
				defer ticker.Stop()

				for {
					if err := updater.Update(ctx); err != nil {
						logger.Warn("自动更新 Prometheus 文档失败", "err", err)
					}

					select {
					case <-ticker.C:
						continue
					case <-ctx.Done():
						return nil
					case <-cancel:
						return nil
					}
				}
			},
			func(error) {
				close(cancel)
			},
		)
	}

	// 运行所有 goroutine
	if err := g.Run(); err != nil {
		logger.Error("运行守护进程 goroutine 失败", "err", err)

		// 在退出前显式取消上下文
		rootCtxCancel()
		os.Exit(1)
	}
	logger.Info("下次再见！")
}

// initHTTPServer 初始化 HTTP 服务器
func initHTTPServer(logger *slog.Logger) *http.Server {
	server := &http.Server{
		// 这些是单个 HTTP 请求/响应周期的 TCP 级超时，不是 MCP 会话超时。
		// MCP 会话是长生命周期的，通过会话 ID 跟踪，并通过 go-sdk 与这些 HTTP 服务器值分开管理。
		//
		// 重要提示：因为 SSE/HTTP 传输是流式的，所以必须禁用 WriteTimeout，
		// 因为响应"永远不会完成"。
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  30 * time.Second,
	}

	// 创建指标处理程序
	metricsHandler := promhttp.HandlerFor(
		prometheus.Gatherers{metrics.Registry},
		promhttp.HandlerOpts{
			ErrorLog:            slog.NewLogLogger(logger.Handler(), slog.LevelError),
			ErrorHandling:       promhttp.ContinueOnError,
			MaxRequestsInFlight: *flagWebMaxRequests,
			Registry:            metrics.Registry,
		},
	)
	metricsHandler = promhttp.InstrumentMetricHandler(
		metrics.Registry, metricsHandler,
	)
	http.Handle("/metrics", metricsHandler)

	// 构建首页链接列表
	landingPageLinks := []web.LandingLinks{
		{
			Address: *flagWebTelemetryPath,
			Text:    "Metrics",
		},
	}

	// 如果使用 HTTP 传输，则添加 MCP 服务器链接
	if *flagMcpTransport == "http" {
		landingPageLinks = append(landingPageLinks,
			web.LandingLinks{
				Address: "/mcp",
				Text:    "Prometheus MCP Server",
			},
		)
	}

	// 如果文档文件系统存在，则添加文档链接
	if docsFs != nil {
		http.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.FS(docsFs))))
		landingPageLinks = append(landingPageLinks,
			web.LandingLinks{
				Address: "/docs/",
				Text:    "Prometheus Documentation",
			},
		)
	}

	// 如果遥测路径不是根路径，则创建首页
	if *flagWebTelemetryPath != "/" {
		landingConfig := web.LandingConfig{
			Name:        "Prometheus MCP Server",
			Description: "MCP Server to interact with Prometheus",
			Version:     version.Info(),
			Links:       landingPageLinks,
		}
		landingPage, err := web.NewLandingPage(landingConfig)
		if err != nil {
			logger.Error("创建首页失败", "err", err)
			os.Exit(1)
		}
		http.Handle("/", landingPage)
	}

	return server
}

// getRoundTripperFromConfig 从配置文件获取 HTTP RoundTripper
func getRoundTripperFromConfig(httpConfig string) (http.RoundTripper, error) {
	httpClient := http.DefaultClient
	if httpConfig != "" {
		httpCfg, _, err := config_util.LoadHTTPConfigFile(httpConfig)
		if err != nil {
			return nil, fmt.Errorf("加载 HTTP 配置文件 %s 失败: %w", httpConfig, err)
		}

		if err = httpCfg.Validate(); err != nil {
			return nil, fmt.Errorf("验证 HTTP 配置文件 %s 失败: %w", httpConfig, err)
		}

		httpClient, err = config_util.NewClientFromConfig(*httpCfg, programName)
		if err != nil {
			return nil, fmt.Errorf("从配置文件 %s 创建 HTTP 客户端失败: %w", httpConfig, err)
		}
	}

	rt := http.DefaultTransport
	if httpClient.Transport != nil {
		rt = httpClient.Transport
	}

	return rt, nil
}
