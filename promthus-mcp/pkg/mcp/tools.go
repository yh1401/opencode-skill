package mcp

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// emptyInputSchema 是一个显式的 JSON Schema，用于没有参数的工具。
// MCP SDK 为 EmptyInput 自动生成的 schema 会省略 "properties" 键，
// 这会导致 OpenAI 的严格验证出现问题。包含一个空的 "properties" 对象在语义上是相同的。
//
// 参考: https://github.com/tjhop/prometheus-mcp-server/issues/119
var emptyInputSchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

// ptr 返回给定值的指针。
//
// 主要用于 `true` 是常量的情况，我们无法直接获取它的地址
// 用于需要 bool 指针的地方。
func ptr[T any](v T) *T {
	return &v
}

// MCP 服务器的工具定义
var (
	// queryToolDef - 瞬时查询工具：执行 Prometheus 瞬时查询
	queryToolDef = &mcp.Tool{
		Name:        "query",
		Description: "对 Prometheus 数据源执行瞬时查询",
		Annotations: &mcp.ToolAnnotations{
			Title:        "瞬时查询",
			ReadOnlyHint: true,
		},
	}

	// rangeQueryToolDef - 范围查询工具：执行 Prometheus 范围查询
	rangeQueryToolDef = &mcp.Tool{
		Name:        "range_query",
		Description: "对 Prometheus 数据源执行范围查询",
		Annotations: &mcp.ToolAnnotations{
			Title:        "范围查询",
			ReadOnlyHint: true,
		},
	}

	// exemplarQueryToolDef - 示例查询工具：查询与指标样本关联的跟踪示例
	exemplarQueryToolDef = &mcp.Tool{
		Name:        "exemplar_query",
		Description: "对 Prometheus 数据源执行示例查询，查找与指标样本关联的跟踪示例",
		Annotations: &mcp.ToolAnnotations{
			Title:        "示例查询",
			ReadOnlyHint: true,
		},
	}

	// seriesToolDef - 序列查询工具：通过标签匹配查找序列
	seriesToolDef = &mcp.Tool{
		Name:        "series",
		Description: "通过标签匹配查找序列",
		Annotations: &mcp.ToolAnnotations{
			Title:        "查找序列",
			ReadOnlyHint: true,
		},
	}

	// labelNamesToolDef - 标签名称工具：返回指定时间范围内存在的唯一标签名称
	labelNamesToolDef = &mcp.Tool{
		Name:        "label_names",
		Description: "根据给定的时间范围和匹配条件，按排序顺序返回块中存在的唯一标签名称",
		Annotations: &mcp.ToolAnnotations{
			Title:        "标签名称",
			ReadOnlyHint: true,
		},
	}

	// labelValuesToolDef - 标签值工具：查询给定标签的值
	labelValuesToolDef = &mcp.Tool{
		Name:        "label_values",
		Description: "根据给定的标签、时间范围和匹配条件查询标签值",
		Annotations: &mcp.ToolAnnotations{
			Title:        "标签值",
			ReadOnlyHint: true,
		},
	}

	// metricMetadataToolDef - 指标元数据工具：返回指标的元数据
	metricMetadataToolDef = &mcp.Tool{
		Name:        "metric_metadata",
		Description: "返回当前按指标名称抓取的指标的元数据",
		Annotations: &mcp.ToolAnnotations{
			Title:        "指标元数据",
			ReadOnlyHint: true,
		},
	}

	// targetsMetadataToolDef - 目标元数据工具：返回目标的元数据
	targetsMetadataToolDef = &mcp.Tool{
		Name:        "targets_metadata",
		Description: "返回当前目标抓取的指标的元数据",
		Annotations: &mcp.ToolAnnotations{
			Title:        "目标元数据",
			ReadOnlyHint: true,
		},
	}

	// alertmanagersToolDef - Alertmanager 工具：获取 Alertmanager 发现概览
	alertmanagersToolDef = &mcp.Tool{
		Name:        "alertmanagers",
		Description: "获取 Prometheus Alertmanager 发现概览",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Alertmanager",
			ReadOnlyHint: true,
		},
	}

	// flagsToolDef - 运行时标志工具：获取运行时标志
	flagsToolDef = &mcp.Tool{
		Name:        "flags",
		Description: "获取运行时标志",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "运行时标志",
			ReadOnlyHint: true,
		},
	}

	// listAlertsToolDef - 告警列表工具：列出所有活动告警
	listAlertsToolDef = &mcp.Tool{
		Name:        "list_alerts",
		Description: "列出所有活动告警",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "告警列表",
			ReadOnlyHint: true,
		},
	}

	// tsdbStatsToolDef - TSDB 统计工具：获取 TSDB 使用情况和基数统计
	tsdbStatsToolDef = &mcp.Tool{
		Name:        "tsdb_stats",
		Description: "从 TSDB 获取使用情况和基数统计信息",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "TSDB 统计",
			ReadOnlyHint: true,
		},
	}

	// buildInfoToolDef - 构建信息工具：获取 Prometheus 构建信息
	buildInfoToolDef = &mcp.Tool{
		Name:        "build_info",
		Description: "获取 Prometheus 构建信息",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "构建信息",
			ReadOnlyHint: true,
		},
	}

	// configToolDef - 配置工具：获取 Prometheus 配置
	configToolDef = &mcp.Tool{
		Name:        "config",
		Description: "获取 Prometheus 配置",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "配置信息",
			ReadOnlyHint: true,
		},
	}

	// runtimeInfoToolDef - 运行时信息工具：获取 Prometheus 运行时信息
	runtimeInfoToolDef = &mcp.Tool{
		Name:        "runtime_info",
		Description: "获取 Prometheus 运行时信息",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "运行时信息",
			ReadOnlyHint: true,
		},
	}

	// listRulesToolDef - 规则列表工具：列出所有告警和记录规则
	listRulesToolDef = &mcp.Tool{
		Name:        "list_rules",
		Description: "列出所有已加载的告警规则和记录规则",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "规则列表",
			ReadOnlyHint: true,
		},
	}

	// listTargetsToolDef - 目标列表工具：获取 Prometheus 目标发现概览
	listTargetsToolDef = &mcp.Tool{
		Name:        "list_targets",
		Description: "获取 Prometheus 目标发现概览",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "目标列表",
			ReadOnlyHint: true,
		},
	}

	// walReplayToolDef - WAL 重放状态工具：获取当前 WAL 重放状态
	walReplayToolDef = &mcp.Tool{
		Name:        "wal_replay_status",
		Description: "获取当前 WAL 重放状态",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "WAL 重放状态",
			ReadOnlyHint: true,
		},
	}

	// ========== TSDB 管理工具 ==========

	// cleanTombstonesToolDef - 清理墓碑工具：从磁盘删除已删除的数据并清理墓碑
	cleanTombstonesToolDef = &mcp.Tool{
		Name:        "clean_tombstones",
		Description: "从磁盘删除已删除的数据并清理现有的墓碑",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           "清理墓碑",
			DestructiveHint: ptr(true),
		},
	}

	// deleteSeriesToolDef - 删除序列工具：删除指定时间范围内的序列数据
	deleteSeriesToolDef = &mcp.Tool{
		Name:        "delete_series",
		Description: "删除指定时间范围内选择的序列数据。必须同时提供 start_time 和 end_time 以防止意外删除所有数据。删除操作会创建墓碑；运行 clean_tombstones 或等待压缩以回收磁盘空间。",
		Annotations: &mcp.ToolAnnotations{
			Title:           "删除序列",
			DestructiveHint: ptr(true),
		},
	}

	// snapshotToolDef - 快照工具：创建当前所有数据的快照
	snapshotToolDef = &mcp.Tool{
		Name:        "snapshot",
		Description: "将所有当前数据创建快照到 TSDB 数据目录下的 snapshots/<datetime>-<rand> 目录，并返回该目录作为响应",
		Annotations: &mcp.ToolAnnotations{
			Title:           "创建快照",
			DestructiveHint: ptr(true),
		},
	}

	// ========== 管理 API 工具 ==========

	// healthyToolDef - 健康检查工具：检查 Prometheus 健康状态
	healthyToolDef = &mcp.Tool{
		Name:        "healthy",
		Description: "管理 API 端点，用于检查 Prometheus 健康状态",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "健康检查",
			ReadOnlyHint: true,
		},
	}

	// readyToolDef - 就绪检查工具：检查 Prometheus 是否准备好处理流量
	readyToolDef = &mcp.Tool{
		Name:        "ready",
		Description: "管理 API 端点，用于检查 Prometheus 是否已准备好服务流量（即响应查询）",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "就绪检查",
			ReadOnlyHint: true,
		},
	}

	// reloadToolDef - 重新加载工具：触发 Prometheus 配置和规则文件重新加载
	reloadToolDef = &mcp.Tool{
		Name:        "reload",
		Description: "管理 API 端点，用于触发 Prometheus 配置和规则文件的重新加载",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           "重新加载配置",
			DestructiveHint: ptr(true),
		},
	}

	// quitToolDef - 退出工具：触发 Prometheus 优雅关闭
	quitToolDef = &mcp.Tool{
		Name:        "quit",
		Description: "管理 API 端点，用于触发 Prometheus 的优雅关闭",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           "关闭服务",
			DestructiveHint: ptr(true),
		},
	}

	// ========== 文档工具 ==========

	// docsListToolDef - 文档列表工具：列出官方 Prometheus 文档文件
	docsListToolDef = &mcp.Tool{
		Name:        "docs_list",
		Description: "列出官方 Prometheus 文档文件",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "文档列表",
			ReadOnlyHint: true,
		},
	}

	// docsReadToolDef - 文档读取工具：读取指定的文档文件
	docsReadToolDef = &mcp.Tool{
		Name:        "docs_read",
		Description: "从 prometheus/docs 仓库读取包含官方 Prometheus 文档的指定 markdown 文件",
		Annotations: &mcp.ToolAnnotations{
			Title:        "读取文档",
			ReadOnlyHint: true,
		},
	}

	// docsSearchToolDef - 文档搜索工具：搜索文档内容
	docsSearchToolDef = &mcp.Tool{
		Name:        "docs_search",
		Description: "搜索 prometheus/docs 仓库中包含官方 Prometheus 文档的 markdown 文件",
		Annotations: &mcp.ToolAnnotations{
			Title:        "搜索文档",
			ReadOnlyHint: true,
		},
	}

	// ========== Thanos 专用工具 ==========

	// thanosStoresToolDef - Thanos 存储工具：列出所有存储 API 服务器
	thanosStoresToolDef = &mcp.Tool{
		Name:        "list_stores",
		Description: "列出所有存储 API 服务器",
		InputSchema: emptyInputSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:        "存储列表",
			ReadOnlyHint: true,
		},
	}

	// ========== 主机指标工具 ==========

	// hostMetricsRealtimeToolDef - 主机实时指标工具：获取指定节点的实时性能指标
	hostMetricsRealtimeToolDef = &mcp.Tool{
		Name:        "host_metrics_realtime",
		Description: "获取指定节点或 IP 地址的实时主机性能指标（CPU核数、CPU使用率、内存总量、内存使用率、磁盘容量、磁盘使用率、网络连接数、网络流量、磁盘IO、系统负载、TCP连接溢出、总TCP连接数，共12项指标）",
		Annotations: &mcp.ToolAnnotations{
			Title:        "主机实时指标",
			ReadOnlyHint: true,
		},
	}

	// hostMetricsTrendToolDef - 主机趋势指标工具：获取指定时间范围内的主机性能趋势数据
	hostMetricsTrendToolDef = &mcp.Tool{
		Name:        "host_metrics_trend",
		Description: "获取指定节点或 IP 地址在时间范围内的主机性能指标历史趋势数据（CPU核数、CPU使用率、内存总量、内存使用率、磁盘容量、磁盘使用率、网络连接数、网络流量、磁盘IO、系统负载、TCP连接溢出、总TCP连接数，共12项指标）",
		Annotations: &mcp.ToolAnnotations{
			Title:        "主机趋势指标",
			ReadOnlyHint: true,
		},
	}

	// ========== easyviews 架构分析工具 ==========

	// moduleTopologyToolDef - 模块拓扑工具：获取节点下的业务模块与应用归属
	moduleTopologyToolDef = &mcp.Tool{
		Name:        "get_module_topology",
		Description: "获取指定节点的业务模块拓扑，返回模块标识、中文名称、对象类型及关联的应用项目名称列表（projectName），用于建立节点、模块与应用归属关系",
		Annotations: &mcp.ToolAnnotations{
			Title:        "模块拓扑",
			ReadOnlyHint: true,
		},
	}

	// chainTopologyToolDef - 服务调用链路拓扑工具：获取模块的应用调用关系
	chainTopologyToolDef = &mcp.Tool{
		Name:        "get_chain_topology",
		Description: "获取指定节点与项目列表的应用调用链路拓扑，返回调用节点、调用边及统计字段，用于分析服务依赖、集中依赖、长调用链与异常依赖",
		Annotations: &mcp.ToolAnnotations{
			Title:        "服务调用链路拓扑",
			ReadOnlyHint: true,
		},
	}

	// nodeListToolDef - 节点列表工具：获取节点列表及节点标识映射
	nodeListToolDef = &mcp.Tool{
		Name:        "get_node_list",
		Description: "获取节点列表及其标识映射，返回节点名称（node）、云平台节点编码（ctyun_node_code3）、节点 ID 与省份信息，支持按节点名称前缀过滤",
		Annotations: &mcp.ToolAnnotations{
			Title:        "节点列表",
			ReadOnlyHint: true,
		},
	}

	// virtualStructureToolDef - 物理机与云主机关系工具
	virtualStructureToolDef = &mcp.Tool{
		Name:        "get_virtual_structure",
		Description: "获取指定节点下物理宿主机与云主机的关系，返回物理宿主机 IP 与其承载的云主机 IP 列表，用于识别故障域集中部署",
		Annotations: &mcp.ToolAnnotations{
			Title:        "物理机与云主机关系",
			ReadOnlyHint: true,
		},
	}

	// projectTopologyToolDef - 项目及云主机部署关系工具
	projectTopologyToolDef = &mcp.Tool{
		Name:        "get_project_topology",
		Description: "获取指定节点下的项目列表与云主机部署关系，返回项目、项目对应的云主机 IP 列表及云主机上的项目，用于分析多实例是否集中于同一宿主机",
		Annotations: &mcp.ToolAnnotations{
			Title:        "项目部署拓扑",
			ReadOnlyHint: true,
		},
	}
)
