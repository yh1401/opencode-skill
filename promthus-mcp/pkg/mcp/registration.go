package mcp

import (
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/common/promslog"
)

// toolRegistration 保存工具定义及其注册函数
// 使用注册函数是因为处理程序有不同的泛型类型
type toolRegistration struct {
	tool     *mcp.Tool
	register func(server *mcp.Server, container *ServerContainer)
}

// 工具分组
var (
	// CoreTools 是始终加载的核心工具列表
	CoreTools = []string{
		"docs_list",
		"docs_read",
		"docs_search",
		"query",
		"range_query",
		"metric_metadata",
		"label_names",
		"label_values",
		"series",
	}

	// PrometheusTsdbAdminTools 是需要显式启用的危险管理工具
	PrometheusTsdbAdminTools = []string{
		"clean_tombstones",
		"delete_series",
		"snapshot",
	}

	// PrometheusBackends 是直接支持的 Prometheus API
	// 兼容后端列表。除 prometheus 本身外的后端可能
	// 暴露一套更适合后端的工具集和/或改变现有工具的功能
	PrometheusBackends = []string{
		"prometheus",
		"thanos",
	}

	// prometheusToolset 包含通过 HTTP API 与标准 prometheus 交互的所有工具
	prometheusToolset map[string]toolRegistration

	// thanosToolset 包含与 thanos（作为 prometheus HTTP API 兼容后端）交互的所有工具
	thanosToolset map[string]toolRegistration
)

// initPrometheusToolset 初始化 prometheus 工具集映射
// 在 init 期间调用以避免初始化循环并控制初始化顺序
func initPrometheusToolset() {
	prometheusToolset = map[string]toolRegistration{
		"query": {
			tool: queryToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, queryToolDef, c.QueryHandler)
			},
		},
		"range_query": {
			tool: rangeQueryToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, rangeQueryToolDef, c.RangeQueryHandler)
			},
		},
		"exemplar_query": {
			tool: exemplarQueryToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, exemplarQueryToolDef, c.ExemplarQueryHandler)
			},
		},
		"series": {
			tool: seriesToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, seriesToolDef, c.SeriesHandler)
			},
		},
		"label_names": {
			tool: labelNamesToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, labelNamesToolDef, c.LabelNamesHandler)
			},
		},
		"label_values": {
			tool: labelValuesToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, labelValuesToolDef, c.LabelValuesHandler)
			},
		},
		"metric_metadata": {
			tool: metricMetadataToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, metricMetadataToolDef, c.MetricMetadataHandler)
			},
		},
		"targets_metadata": {
			tool: targetsMetadataToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, targetsMetadataToolDef, c.TargetsMetadataHandler)
			},
		},
		"alertmanagers": {
			tool: alertmanagersToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, alertmanagersToolDef, c.AlertmanagersHandler)
			},
		},
		"flags": {
			tool: flagsToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, flagsToolDef, c.FlagsHandler)
			},
		},
		"list_alerts": {
			tool: listAlertsToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, listAlertsToolDef, c.ListAlertsHandler)
			},
		},
		"tsdb_stats": {
			tool: tsdbStatsToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, tsdbStatsToolDef, c.TsdbStatsHandler)
			},
		},
		"build_info": {
			tool: buildInfoToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, buildInfoToolDef, c.BuildInfoHandler)
			},
		},
		"config": {
			tool: configToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, configToolDef, c.ConfigHandler)
			},
		},
		"runtime_info": {
			tool: runtimeInfoToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, runtimeInfoToolDef, c.RuntimeInfoHandler)
			},
		},
		"list_rules": {
			tool: listRulesToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, listRulesToolDef, c.ListRulesHandler)
			},
		},
		"list_targets": {
			tool: listTargetsToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, listTargetsToolDef, c.ListTargetsHandler)
			},
		},
		"wal_replay_status": {
			tool: walReplayToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, walReplayToolDef, c.WALReplayHandler)
			},
		},
		// TSDB 管理工具
		"clean_tombstones": {
			tool: cleanTombstonesToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, cleanTombstonesToolDef, c.CleanTombstonesHandler)
			},
		},
		"delete_series": {
			tool: deleteSeriesToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, deleteSeriesToolDef, c.DeleteSeriesHandler)
			},
		},
		"snapshot": {
			tool: snapshotToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, snapshotToolDef, c.SnapshotHandler)
			},
		},
		// 管理 API 工具
		"healthy": {
			tool: healthyToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, healthyToolDef, c.HealthyHandler)
			},
		},
		"ready": {
			tool: readyToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, readyToolDef, c.ReadyHandler)
			},
		},
		"reload": {
			tool: reloadToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, reloadToolDef, c.ReloadHandler)
			},
		},
		"quit": {
			tool: quitToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, quitToolDef, c.QuitHandler)
			},
		},
		// 文档工具
		"docs_list": {
			tool: docsListToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, docsListToolDef, c.DocsListHandler)
			},
		},
		"docs_read": {
			tool: docsReadToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, docsReadToolDef, c.DocsReadHandler)
			},
		},
		"docs_search": {
			tool: docsSearchToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, docsSearchToolDef, c.DocsSearchHandler)
			},
		},
		// 主机指标工具
		"host_metrics_realtime": {
			tool: hostMetricsRealtimeToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, hostMetricsRealtimeToolDef, c.HostMetricsRealtimeHandler)
			},
		},
		"host_metrics_trend": {
			tool: hostMetricsTrendToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, hostMetricsTrendToolDef, c.HostMetricsTrendHandler)
			},
		},
		// easyviews 架构分析工具
		"get_module_topology": {
			tool: moduleTopologyToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, moduleTopologyToolDef, c.GetModuleTopologyHandler)
			},
		},
		"get_chain_topology": {
			tool: chainTopologyToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, chainTopologyToolDef, c.GetChainTopologyHandler)
			},
		},
		"get_node_list": {
			tool: nodeListToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, nodeListToolDef, c.GetNodeListHandler)
			},
		},
		"get_virtual_structure": {
			tool: virtualStructureToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, virtualStructureToolDef, c.GetVirtualStructureHandler)
			},
		},
		"get_project_topology": {
			tool: projectTopologyToolDef,
			register: func(s *mcp.Server, c *ServerContainer) {
				mcp.AddTool(s, projectTopologyToolDef, c.GetProjectTopologyHandler)
			},
		},
	}
}

// thanosRemovedTools 列出 prometheusToolset 中 Thanos 不支持的工具
var thanosRemovedTools = []string{
	"alertmanagers",
	"config",
	"wal_replay_status",
	"reload",
	"quit",
}

// initThanosToolset 初始化 thanos 工具集映射
// 在 init 期间调用以避免初始化循环并控制初始化顺序
//
// 它从 prometheusToolset 开始，移除不支持的工具，并添加
// Thanos 特定的工具（list_stores）
func initThanosToolset() {
	thanosToolset = make(map[string]toolRegistration)
	for name, tool := range prometheusToolset {
		if !slices.Contains(thanosRemovedTools, name) {
			thanosToolset[name] = tool
		}
	}

	// 添加 Thanos 特定的工具
	thanosToolset["list_stores"] = toolRegistration{
		tool: thanosStoresToolDef,
		register: func(s *mcp.Server, c *ServerContainer) {
			mcp.AddTool(s, thanosStoresToolDef, c.ThanosStoresHandler)
		},
	}
}

func init() {
	initPrometheusToolset()
	initThanosToolset()
}

// registerTools 将给定的工具集注册到 MCP 服务器
func registerTools(server *mcp.Server, container *ServerContainer, toolset []toolRegistration) {
	for _, tool := range toolset {
		tool.register(server, container)
	}
}

type toolsetConfig struct {
	enabledTools      []string
	prometheusBackend string
	logger            *slog.Logger
}

func getToolset(cfg toolsetConfig) map[string]toolRegistration {
	logger := cfg.logger
	if logger == nil {
		logger = promslog.NewNopLogger()
	}

	toolset := make(map[string]toolRegistration)

	switch {
	case len(cfg.enabledTools) == 1 && cfg.enabledTools[0] == "all":
		logger.Info("根据提供的工具集设置工具", "toolset", "all")
		for name, tool := range prometheusToolset {
			toolset[name] = tool
		}
	case len(cfg.enabledTools) == 1 && cfg.enabledTools[0] == "core":
		logger.Info("根据提供的工具集设置工具", "toolset", "core")
		for _, toolName := range CoreTools {
			toolset[toolName] = prometheusToolset[toolName]
		}
	default:
		// 始终包含核心工具，加上任何额外指定的工具
		enabledTools := slices.Concat(cfg.enabledTools, CoreTools)
		slices.Sort(enabledTools)
		enabledTools = slices.Compact(enabledTools)
		logger.Info("根据提供的工具集设置工具", "toolset", enabledTools)

		for _, toolName := range enabledTools {
			val, ok := prometheusToolset[toolName]
			if !ok {
				logger.Warn("找不到要注册的工具", "tool_name", toolName)
				continue
			}

			logger.Debug("添加工具到工具集以进行注册", "tool_name", toolName)
			toolset[toolName] = val
		}
	}

	backend := strings.ToLower(cfg.prometheusBackend)
	switch backend {
	case "": // 保持已加载的工具集
	case "prometheus":
		logger.Info("根据提供的 prometheus 后端设置工具", "backend", backend)
		backendToolset := make(map[string]toolRegistration)
		for name, tool := range prometheusToolset {
			backendToolset[name] = tool
		}
		toolset = backendToolset
	case "thanos":
		logger.Info("根据提供的 prometheus 后端设置工具", "backend", backend)
		backendToolset := make(map[string]toolRegistration)
		for name, tool := range thanosToolset {
			backendToolset[name] = tool
		}
		toolset = backendToolset
	default:
		logger.Warn("Prometheus 后端没有自定义工具支持，保持现有工具集",
			"backend", backend, "toolset", cfg.enabledTools)
	}

	// 生命周期工具（reload/quit）默认不注册：误触发会导致 Prometheus 重载或退出。
	// 仅当显式设置 MCP_ENABLE_LIFECYCLE_TOOLS=true 时注册；
	// 即使注册，Prometheus 侧仍需 --enable-lifecycle 支持才会生效。
	if !strings.EqualFold(os.Getenv("MCP_ENABLE_LIFECYCLE_TOOLS"), "true") {
		delete(toolset, "reload")
		delete(toolset, "quit")
		logger.Info("生命周期工具未启用，已跳过注册 reload/quit（设置 MCP_ENABLE_LIFECYCLE_TOOLS=true 开启）")
	}

	return toolset
}

// toolsetToToolRegistrationSlice 将工具集映射转换为 toolRegistration 切片
// 这对于传递给期望切片的 RegisterTools 很有用
func toolsetToToolRegistrationSlice(toolset map[string]toolRegistration) []toolRegistration {
	result := make([]toolRegistration, 0, len(toolset))
	for _, tool := range toolset {
		result = append(result, tool)
	}
	return result
}
