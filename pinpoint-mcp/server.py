"""Pinpoint MCP Server 入口

启动方式：
    python server.py
    # 或指定环境变量：
    #   WEB_BASE_URL=http://127.0.0.1:8080
    #   PINPOINT_USER=admin PINPOINT_PASS=admin
    #   MCP_HOST=0.0.0.0 MCP_PORT=8888
"""
import asyncio
import json
import logging

import uvicorn
from mcp.server.fastmcp import FastMCP
from starlette.responses import JSONResponse

import arch_tools
import config
import task_scope
from pinpoint_client import get_client
from tools import (
    drag_heatmap,
    get_server_map,
    get_transaction_trace,
    list_applications,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
logger = logging.getLogger(__name__)

mcp = FastMCP(config.MCP_NAME, host=config.MCP_HOST, port=config.MCP_PORT)


@mcp.tool()
async def list_applications_tool(service_name: str = "DEFAULT", clear_cache: bool = False) -> dict:
    """列出 Pinpoint 监控的全部应用列表。

    参数：
        service_name: 服务名（租户维度），默认 DEFAULT
        clear_cache: 是否清除应用列表缓存后重新查询
    """
    return await list_applications(service_name=service_name, clear_cache=clear_cache)


@mcp.tool()
async def get_server_map_tool(
    application_name: str,
    from_ts: int | None = None,
    to_ts: int | None = None,
    service_type_code: int | None = None,
    service_type_name: str | None = None,
    caller_range: int = 1,
    callee_range: int = 1,
    bidirectional: bool = True,
    was_only: bool = False,
) -> dict:
    """查询指定应用的 ServerMap 服务拓扑图。

    参数：
        application_name: 应用名（必填）
        from_ts: 开始时间（毫秒时间戳），默认最近 5 分钟
        to_ts: 结束时间（毫秒时间戳），默认当前时间
        service_type_code: 服务类型编码，通常可省略
        service_type_name: 服务类型名，通常可省略
        caller_range: 上游调用方搜索深度 1-4，默认 1
        callee_range: 下游被调方搜索深度 1-4，默认 1
        bidirectional: 是否双向搜索，默认 true
        was_only: 仅显示 WAS 节点，默认 false
    """
    return await get_server_map(
        application_name=application_name,
        from_ts=from_ts,
        to_ts=to_ts,
        service_type_code=service_type_code,
        service_type_name=service_type_name,
        caller_range=caller_range,
        callee_range=callee_range,
        bidirectional=bidirectional,
        was_only=was_only,
    )


@mcp.tool()
async def drag_heatmap_tool(
    application: str,
    x1: int,
    x2: int,
    y1: int,
    y2: int,
    agent_id: str | None = None,
    dot_status: bool | None = None,
    limit: int = 50,
    service_type_code: int | None = None,
    service_type_name: str | None = None,
) -> dict:
    """按散点图拖拽区域查询事务列表。

    x 轴为时间（毫秒时间戳），y 轴为响应时间（毫秒）。

    参数：
        application: 应用名（必填）
        x1, x2: 时间范围 [x1, x2]（毫秒时间戳）
        y1, y2: 响应时间范围 [y1, y2]（毫秒）
        agent_id: 指定 Agent，可选
        dot_status: 过滤状态：true=成功，false=失败，缺省不过滤
        limit: 最大返回条数，默认 50
    """
    return await drag_heatmap(
        application=application,
        x1=x1,
        x2=x2,
        y1=y1,
        y2=y2,
        agent_id=agent_id,
        dot_status=dot_status,
        limit=limit,
        service_type_code=service_type_code,
        service_type_name=service_type_name,
    )


@mcp.tool()
async def get_transaction_trace_tool(
    trace_id: str,
    focus_timestamp: int = 0,
    agent_id: str | None = None,
    span_id: int = -1,
    link_trace_id: str | None = None,
    link_span_id: int = -1,
) -> dict:
    """查询单个事务的完整调用树（CallStack）。

    参数：
        trace_id: 事务 ID（必填），形如 'agentId^startTime^seq^elapsed'
        focus_timestamp: 聚焦时间点（毫秒），默认 0
        agent_id: 指定 Agent，可选
        span_id: Span ID，默认 -1
        link_trace_id: 关联事务 ID，可选
        link_span_id: 关联 Span ID，默认 -1
    """
    return await get_transaction_trace(
        trace_id=trace_id,
        focus_timestamp=focus_timestamp,
        agent_id=agent_id,
        span_id=span_id,
        link_trace_id=link_trace_id,
        link_span_id=link_span_id,
    )


# ---------------- 架构优化分析工具 ----------------

@mcp.tool()
async def list_nodes_tool(node_prefix: str = "云公司") -> dict:
    """获取节点列表（架构优化 2.2）。

    参数：
        node_prefix: 节点名前缀筛选，默认"云公司"（方案要求），传空串返回全部
    返回节点的 node（节点名称）与 ctyun_node_code3（宿主机查询标识），两者不可混用。
    """
    return await arch_tools.list_nodes(node_prefix=node_prefix)


@mcp.tool()
async def get_module_topology_tool(
    node: str,
    product_name: str | None = None,
    simplify: bool = True,
) -> dict:
    """获取节点的模块与应用归属（架构优化 1.1）。

    参数：
        node: 节点名，如 '云公司->贵州'、'云公司->广州4d'
        product_name: 产品名，默认取 ARCH_PRODUCT_NAME（天翼看家）
        simplify: 是否返回精简结构（模块标识/中文名/对象类型/projectName）
    返回的 deployNode 作为 get_link_topology_tool 的 node 入参。
    """
    return await arch_tools.get_module_topology(
        node=node, product_name=product_name, simplify=simplify
    )


@mcp.tool()
async def analyze_link_topology_tool(
    node: str,
    projects: list[str],
    minute: int | None = None,
    caller_range: int = 1,
    callee_range: int = 1,
    filter_node: bool = False,
    node_type: str = "",
    task_id: str = "",
    dep_suffix_only: bool | None = None,
    dep_suffix: str | None = None,
    include_raw: bool = False,
) -> dict:
    """链路拓扑 + 基础图计算（架构优化 1.3，分析链路风险时优先使用）。

    在原始拓扑之上完成确定性图计算，直接输出风险项：
    - 集中依赖（入度超阈值）
    - 下游依赖过多（出度超阈值）
    - 循环依赖（强连通分量）
    - 长调用链（DAG 最长路径）
    - 异常调用边（错误率 / 耗时超阈值）
    - 上下游影响范围（反向可达节点数，即异常波及的上游服务）
    - 关键依赖（经过节点的源→汇路径数，被多条核心链路共享者优先保护）

    参数：
        node: get_module_topology_tool 返回的 deployNode（多节点别名按原样传入）
        projects: 目标模块的 projectName 列表
        minute: 统计窗口（分钟），默认 ARCH_LINK_WINDOW_MINUTES（15）
        caller_range / callee_range: 上下游搜索深度，默认 1
        filter_node: 是否过滤节点，默认 false
        node_type: 节点类型，默认空
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
        dep_suffix_only: 是否只分析 -dep 结尾项目，默认取 ARCH_DEP_SUFFIX_ONLY
        dep_suffix: 后缀，默认取 ARCH_DEP_SUFFIX（-dep）
        include_raw: 是否附带原始拓扑图（121 应用 × 15 分钟约 500KB，易导致
                      IDE 工具结果卡住），默认 false；仅在核对解析口径时置 true
    阈值由 ARCH_LINK_* 配置（方案尚未定阈值，当前为建议值）。
    未入选项目在 depFilter.excluded 中列明，不会被静默丢弃。
    """
    return await arch_tools.analyze_link_topology(
        node=node,
        projects=projects,
        minute=minute,
        caller_range=caller_range,
        callee_range=callee_range,
        filter_node=filter_node,
        node_type=node_type,
        task_id=task_id or None,
        dep_suffix_only=dep_suffix_only,
        dep_suffix=dep_suffix,
        include_raw=include_raw,
    )


@mcp.tool()
async def get_link_topology_tool(
    node: str,
    projects: list[str],
    minute: int | None = None,
    caller_range: int = 1,
    callee_range: int = 1,
    filter_node: bool = False,
    node_type: str = "",
) -> dict:
    """获取应用链路拓扑（架构优化 1.3）。

    参数：
        node: 使用 get_module_topology_tool 返回的 deployNode（多节点别名按原样传入）
        projects: 目标模块的 projectName 列表
        minute: 统计窗口（分钟），默认 ARCH_LINK_WINDOW_MINUTES（15）
        caller_range / callee_range: 上下游搜索深度，默认 1
        filter_node: 是否过滤节点，默认 false
        node_type: 节点类型，默认空

    上游耗时随 minute 与 projects 规模增长：应用数超过 ARCH_LINK_BATCH_SIZE（默认 30）
    时自动按批并发取数再合并（节点按 id、边按 source+target 去重）；
    整次调用受 ARCH_LINK_TIMEOUT（默认 45s）总时限约束，超时返回 DATA_INSUFFICIENT
    + degradedReason=TIMEOUT，避免长时间无响应被网关空闲超时切断后表现为“一直卡住”。
    """
    return await arch_tools.get_link_topology(
        node=node,
        projects=projects,
        minute=minute,
        caller_range=caller_range,
        callee_range=callee_range,
        filter_node=filter_node,
        node_type=node_type,
    )


@mcp.tool()
async def get_host_structure_tool(node_code: str, reverse: bool = True) -> dict:
    """获取物理机与云主机关系（架构优化 2.3）。

    参数：
        node_code: 节点的 ctyun_node_code3（如 'hi-haikou-9'），不是节点名称
        reverse: 是否返回"云主机 IP → 物理宿主机 IP"反向索引，默认 true
    """
    return await arch_tools.get_host_structure(node_code=node_code, reverse=reverse)


@mcp.tool()
async def get_project_deploy_tool(node: str) -> dict:
    """获取项目与云主机部署关系（架构优化 2.4）。

    参数：
        node: 节点名称（如 '云公司->海口9'），不是 ctyun_node_code3
    """
    return await arch_tools.get_project_deploy(node=node)


@mcp.tool()
async def analyze_host_concentration_tool(
    node: str,
    node_code: str,
    only_risk: bool = False,
    task_id: str = "",
    detail: str = "summary",
    partial_limit: int = 5,
    project_limit: int = 10,
    include_ips: bool = False,
) -> dict:
    """宿主机集中度分析（架构优化 2.5）。

    自动关联项目部署关系与宿主机映射，输出每个项目的实例数、宿主机数、
    最大宿主机集中比例、逐宿主机比例与映射覆盖率，并按方案 2.5 五类场景分类：
    SINGLE_INSTANCE（单实例）/ ALL_IN_SAME_HOST（全部同一宿主机）/
    PARTIAL_CONCENTRATION（部分集中）/ PARTIAL_MAPPING（映射不全）/
    MAPPING_CONFLICT（一云主机映射多宿主机）/ SPREAD（分布均衡）。

    参数：
        node: 节点名称（如 '云公司->海口9'）
        node_code: 节点的 ctyun_node_code3（如 'hi-haikou-9'）
        only_risk: 仅返回集中风险与单实例项目，默认 false（返回全量，避免漏报单实例部署）
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
        detail: summary（默认）只回计数、精简关键项与分布聚合；full 回全量明细
        partial_limit: summary 下部分集中清单保留条数，默认 5
        project_limit: summary 下各类清单与 projects 兜底条数，默认 10
        include_ips: summary 下是否保留映射缺失/冲突的 IP 明细，默认 false
    """
    return await arch_tools.analyze_host_concentration(
        node=node,
        node_code=node_code,
        only_risk=only_risk,
        task_id=task_id or None,
        detail=detail,
        partial_limit=partial_limit,
        project_limit=project_limit,
        include_ips=include_ips,
    )


@mcp.tool()
async def prometheus_query_tool(
    query: str,
    ts: float | None = None,
    start_ts: float | None = None,
    end_ts: float | None = None,
    step: str = "1m",
    exclude_namespace: str | list[str] | None = None,
    limit: int = 50,
) -> dict:
    """执行 PromQL 查询（架构优化 1.2）。

    参数：
        query: PromQL 表达式
        ts: 即时查询的时间点（Unix 秒），缺省为当前时刻
        start_ts / end_ts: 同时提供则执行范围查询（"持续 N 分钟"类规则必须使用）
        step: 范围查询步长，默认 1m
        exclude_namespace: 排除非业务命名空间（str 支持逗号分隔 / list[str] / None），
            默认用服务端 ARCH_EXCLUDE_NAMESPACES 配置；
            显式传空字符串 / "none" 禁用；
            query 里已有 namespace="xxx" 精确值时不自动生效（精确优先）；
            query 无 PromQL 选择器时不自动拼，返回 warning 提示手动加
        limit: 最多返回的序列条数，默认 50（超出部分截断，返回 truncated=true）；
            结果已压缩为 {metric, value}（vector）或 {metric, values}（matrix），
            只回传数值，不回传原始时间戳数组
    """
    return await arch_tools.prometheus_query(
        query=query, ts=ts, start_ts=start_ts, end_ts=end_ts, step=step,
        exclude_namespace=exclude_namespace, limit=limit,
    )


@mcp.tool()
async def arch_metric_query_tool(
    rule: str,
    node: str,
    env: str | None = None,
    deployment: str | None = None,
    window: str = "15m",
    end_ts: float | None = None,
    sample_points: int = 15,
    step_seconds: int = 60,
    namespace: str | None = None,
    dep_only: bool = True,
    task_id: str = "",
    return_raw_samples: bool = False,
    exclude_namespace: str | list[str] | None = None,
) -> dict:
    """按规则名取运行指标（架构优化 1.2，自动生成 PromQL 并多点采样）。

    ⚠️ **工具优先级**：架构风险分析请优先使用 analyze_node_runtime_tool（一次调用
    完成全节点 8 条规则判定，仅返回 findings 风险清单）或 arch_rule_check_tool
    （单规则服务端阈值判定，返回四态结论），本工具为低级取数接口——
    需要自行做非标准分析时才调用。

    参数：
        rule: deployment_replicas / deployment_available / cpu_throttle_ratio /
              memory_ratio / hpa_replicas / pod_cpu_imbalance / oom_killed /
              restart_increase
        node: 节点标识，如 '云公司->贵州'；广州4d 等已配置 ARCH_NODE_CLUSTER_MAP 的节点
              自动改用 cluster 标签（广州4d → cluster="广州4d生产"）
        env: env 标签值，默认 ARCH_ENV（生产）
        deployment: deployment 名称，用于缩小范围（可选）
        window: PromQL 内部窗口，默认 15m
        end_ts: 采样结束时间（Unix 秒），默认当前时刻
        sample_points: 采样点数，默认 15
        step_seconds: 采样步长（秒），默认 60
        namespace: 命名空间，同名 deployment 跨命名空间时用于消歧（可选）
        dep_only: 是否强制只分析名称以 -dep 结尾的应用，默认 true
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
        return_raw_samples: 是否返回完整原始时间序列，默认 false
            false（默认）samples 为压缩摘要 {seriesCount, coverage, stat, riskHints}
                        → 适合 agent 多轮调用，7 条规则累计约 2k tokens
            true           samples 为完整按 series 聚合格式
                        → 389 deployment × 15 点 ≈ 90k tokens/条，慎用
        exclude_namespace: 排除非业务命名空间（str 逗号分隔 / list[str] / None），
            默认服务端 ARCH_EXCLUDE_NAMESPACES；显式传 "" / "none" 禁用；
            仅在 namespace 精确值未指定时生效（精确值优先级更高）
    标签兜底：指定 deployment/namespace 时，若 cmdb_node/env 口径无数据，
        会自动去掉节点标签重查一次，实际口径见 scopeFallback。
    默认 15 点 × 60 秒 = 15 分钟窗口，用于"持续 15 分钟"类规则判定。
    指定 deployment 时优先用 kube_pod_owner 解析真实 Pod，失败回退名称前缀，模式见 podSelector。
    """
    return await arch_tools.arch_metric_query(
        rule=rule,
        node=node,
        env=env,
        deployment=deployment,
        window=window,
        end_ts=end_ts,
        sample_points=sample_points,
        step_seconds=step_seconds,
        namespace=namespace,
        dep_only=dep_only,
        task_id=task_id or None,
        return_raw_samples=return_raw_samples,
        exclude_namespace=exclude_namespace,
    )


@mcp.tool()
async def list_arch_rules_tool() -> dict:
    """列出架构优化规则清单、当前阈值与规则版本（方案 4.1.4）。

    用于报告声明"本次分析使用的规则版本与阈值"。
    """
    return await arch_tools.list_arch_rules()


@mcp.tool()
async def arch_label_probe_tool(
    node: str,
    env: str | None = None,
    deployment: str | None = None,
    task_id: str = "",
) -> dict:
    """探测指标族的标签口径，定位"数据管道断点"（架构优化巡检前置检查）。

    当规则结果集中出现"数据不足"时，先用本工具判断是"应用真的没有指标"，
    还是"节点标签 cmdb_node 在该指标族上不存在"——后者属于采集口径问题，
    不应按无风险处理，也不该反复试查。

    检查内容：
        - deployment 级指标（kube_deployment_* / HPA）与
          容器级指标（cAdvisor / kube_pod_container_*）各带哪些标签
        - 用 cmdb_node、cluster、env 过滤时是否有序列（count 计数）
        - cluster 标签的可选值（低基数，最多 50 个）

    参数：
        node: 节点标识，如 '云公司->广州4d'（该节点按 cluster="广州4d生产" 取数，不用 cmdb_node）
        env: env 标签值，默认 ARCH_ENV（生产）
        deployment: 可选，额外验证单个应用是否有独立序列
        task_id: 任务标识；已注册作用域时校验节点是否在白名单内

    返回 verdict.suggestedMode：
        cmdb_node         各指标族标签一致，按 node 查询即可
        deployment_names  deployment 级缺 cmdb_node，需传应用清单
                          （analyze_node_runtime_tool 会自动按应用名兜底）
        cluster           两类指标均未命中节点标签，改用 cluster 选择器或应用名直查
    """
    return await arch_tools.arch_label_probe(
        node=node, env=env, deployment=deployment, task_id=task_id or None
    )


@mcp.tool()
async def arch_rule_check_tool(
    rule: str,
    node: str,
    env: str | None = None,
    deployment: str | None = None,
    end_ts: float | None = None,
    sample_points: int | None = None,
    step_seconds: int = 60,
    task_id: str = "",
    include_samples: bool = False,
    namespace: str | None = None,
    dep_only: bool = True,
    cross_check_latency: bool = True,
    exclude_namespace: str | list[str] | None = None,
) -> dict:
    """执行架构规则判定（架构优化，含阈值判定，方案 4.2）。

    与 arch_metric_query_tool 的区别：本工具**内置阈值判定**，直接给出
    「已发现风险 / 未触发规则 / 数据不足 / 不适用」四态结论，
    并携带规则版本、阈值、实测值、持续时间与采集时间，可直接写入报告。

    参数：
        rule: deployment_replicas / deployment_available / cpu_throttle_ratio /
              memory_ratio / hpa_replicas / pod_cpu_imbalance / oom_killed /
              restart_increase
        node: 节点标识，如 '云公司->贵州'；广州4d 等已配置 ARCH_NODE_CLUSTER_MAP 的节点
              自动改用 cluster 标签（广州4d → cluster="广州4d生产"）
        env: env 标签值，默认 ARCH_ENV（生产）
        deployment: deployment 名称，用于缩小范围（可选）
        end_ts: 采样结束时间（Unix 秒），默认当前时刻
        sample_points: 采样点数，默认等于 ARCH_RULE_DURATION_MINUTES（15）
        step_seconds: 采样步长（秒），默认 60
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
        include_samples: 是否返回 evidence（原始采样序列与失败点），默认 false
        namespace: 命名空间，同名 deployment 跨命名空间时用于消歧（可选）
        dep_only: 是否强制只分析名称以 -dep 结尾的应用，默认 true
        cross_check_latency: CPU 限流风险是否做调用延迟交叉验证，默认 true
        exclude_namespace: 排除非业务命名空间（str 逗号分隔 / list[str] / None），
            默认服务端 ARCH_EXCLUDE_NAMESPACES；显式传 "" / "none" 禁用；
            仅在 namespace 精确值未指定时生效
    默认 15 点 × 60 秒 = 15 分钟窗口；持续型规则要求有效点覆盖率达 80% 且全部超阈值。
    pod_cpu_imbalance 会按 Pod CPU 规格分组，取不到规格时记为数据不足。
    oom_killed 同时使用终止原因与 container_oom_events_total 两类证据。
    指定 deployment 时优先用 kube_pod_owner 解析真实 Pod，模式见 podSelector。
    cpu_throttle_ratio 在配置 ARCH_LATENCY_PROMQL 后返回 latencyCrossCheck，
    按延迟是否超过阈值给出 HIGH / LOWER 优先级提示。
    标签兜底：指定 deployment/namespace 时，若 cmdb_node/env 口径无数据，
    会自动去掉节点标签重查一次，实际口径见 scopeFallback。
    """
    return await arch_tools.arch_rule_check(
        rule=rule,
        node=node,
        env=env,
        deployment=deployment,
        end_ts=end_ts,
        sample_points=sample_points,
        step_seconds=step_seconds,
        task_id=task_id or None,
        include_samples=include_samples,
        namespace=namespace,
        dep_only=dep_only,
        cross_check_latency=cross_check_latency,
        exclude_namespace=exclude_namespace,
    )


@mcp.tool()
async def filter_dep_projects_tool(
    projects: list[str],
    suffix_only: bool | None = None,
    suffix: str | None = None,
) -> dict:
    """按 -dep 命名规则筛选第一阶段分析范围（架构优化 1.2）。

    第一阶段仅对名称以 -dep 结尾的应用执行容器检查，其余应用记录为本阶段未覆盖。
    返回 selected / excluded 两个清单，不做静默丢弃。

    参数：
        projects: 待筛选的应用/项目名列表
        suffix_only: 是否只保留后缀匹配项，默认取 ARCH_DEP_SUFFIX_ONLY（true）
        suffix: 后缀，默认取 ARCH_DEP_SUFFIX（-dep），支持重复后缀
    """
    return arch_tools.filter_dep_projects(
        projects, suffix_only=suffix_only, suffix=suffix
    )


@mcp.tool()
async def build_app_mapping_tool(
    node: str,
    product_name: str | None = None,
    projects: list[str] | None = None,
    min_score: float | None = None,
) -> dict:
    """构建 EasyViews projectName ↔ Pinpoint applicationName 映射（方案 1.3 前置）。

    链路分析需把 Pinpoint 结果关联回模块归属，但两平台应用命名体系不同，
    本工具按归一化名称匹配并输出置信分、匹配方式、未匹配与冲突清单。

    参数：
        node: 节点名，如 '云公司->贵州'
        product_name: 产品名，默认 ARCH_PRODUCT_NAME
        projects: 项目名列表，留空则自动从模块拓扑提取该节点全部 projectName
        min_score: 最低置信分，默认 ARCH_MAPPING_MIN_SCORE（0.7）
    """
    return await arch_tools.build_app_mapping(
        node=node,
        product_name=product_name,
        projects=projects,
        min_score=min_score,
    )


@mcp.tool()
async def resolve_analysis_scope_tool(
    node: str,
    product_name: str | None = None,
    dep_suffix_only: bool | None = None,
    dep_suffix: str | None = None,
    task_id: str = "",
) -> dict:
    """确定本次分析范围：节点 → 模块 → -dep 应用清单（方案 1.2 / 4.1.1）。

    只做确定性整理、不做风险判定，输出模块清单、每模块纳入/排除应用数、
    应用的多模块归属与排除预览，可直接作为报告首页"分析范围"。
    未入选应用必须显式记录，不得静默丢弃。

    参数：
        node: 节点名，如 '云公司->贵州'
        product_name: 产品名，默认 ARCH_PRODUCT_NAME
        dep_suffix_only: 是否只保留 -dep 应用，默认 ARCH_DEP_SUFFIX_ONLY
        dep_suffix: 后缀，默认 ARCH_DEP_SUFFIX（-dep）
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
    """
    return await arch_tools.resolve_analysis_scope(
        node=node,
        product_name=product_name,
        dep_suffix_only=dep_suffix_only,
        dep_suffix=dep_suffix,
        task_id=task_id or None,
    )


@mcp.tool()
async def analyze_node_runtime_tool(
    node: str,
    module: str | None = None,
    deployments: list[str] | None = None,
    rules: list[str] | None = None,
    env: str | None = None,
    end_ts: float | None = None,
    sample_points: int | None = None,
    step_seconds: int = 60,
    dep_only: bool = True,
    top_n: int | None = None,
    task_id: str = "",
    exclude_namespace: str | list[str] | None = None,
) -> dict:
    """节点级运行架构批量分析：一次调用完成"定范围 → 8 条规则判定 → 汇总"（方案 1.2）。

    替代按应用逐个调用 arch_rule_check 的 N×M 次查询：
    每条规则仅发起 sample_points 次节点级查询，规则间并发受 ARCH_BATCH_CONCURRENCY 限制。

    范围优先级：deployments > module > 节点全部 -dep 应用。
    返回 scope / window / rules / summary / findings / moduleSummary / dataLimitations。

    取数口径兜底：当某条规则按 cmdb_node/env 查询无数据、且已知应用清单时，
    会自动改用"应用名选择器"重查一次（部分集群 KSM 部署级指标不带节点标签），
    实际生效口径见返回的 scopeFallback，dataLimitations 中也会标注受影响规则。

    参数：
        node: 节点名，如 '云公司->贵州'；广州4d 等已配置 ARCH_NODE_CLUSTER_MAP 的节点
            自动改用 cluster="广州4d生产"，不再附加 cmdb_node
        module: 只分析指定模块（模块中文名/英文名/ID 均可）
        deployments: 只分析指定应用清单（优先级最高；标签缺失环境下建议显式传入，
            应用清单可用 EasyViews 拓扑或 get_project_deploy_tool 获取）
        rules: 只跑指定规则，默认全部 8 条
        env: env 标签值，默认 ARCH_ENV（生产）
        end_ts: 采样结束时间（Unix 秒），默认当前时刻
        sample_points: 采样点数，默认 ARCH_RULE_DURATION_MINUTES（15）
        step_seconds: 采样步长（秒），默认 60
        dep_only: 是否强制只分析 -dep 应用，默认 true
        top_n: 每条规则最多返回的明细条数（仅 RISK / INSUFFICIENT），默认 ARCH_BATCH_TOP_N(10)
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
        exclude_namespace: 排除非业务命名空间（str 逗号分隔 / list[str] / None），
            默认服务端 ARCH_EXCLUDE_NAMESPACES；显式传 "" / "none" 禁用
    批量模式按 deployment 聚合；findings 上限 ARCH_FINDINGS_MAX(30)，超出时
    findingsTruncated=true，风险总数仍保留在 summary.riskItemCount。
    OK / NOT_APPLICABLE 明细不回传（计数见 okCount / naCount），
    风险项建议用 arch_rule_check 单应用复核。
    """
    return await arch_tools.analyze_node_runtime(
        node=node,
        module=module,
        deployments=deployments,
        rules=rules,
        env=env,
        end_ts=end_ts,
        sample_points=sample_points,
        step_seconds=step_seconds,
        dep_only=dep_only,
        top_n=top_n,
        task_id=task_id or None,
        exclude_namespace=exclude_namespace,
    )


@mcp.tool()
async def analyze_pinpoint_dependencies_tool(
    node: str,
    projects: list[str] | None = None,
    product_name: str | None = None,
    min_score: float | None = None,
    caller_range: int = 2,
    callee_range: int = 2,
    top_n: int | None = None,
    task_id: str = "",
) -> dict:
    """基于 Pinpoint 构建服务依赖图并做链路风险判定（方案 1.3）。

    流程：EasyViews↔Pinpoint 应用映射 → 批量 serverMap → 合并调用边 →
    入/出度、循环依赖、最长链路、影响范围、关键依赖、异常调用边。
    单个应用 serverMap 失败不阻断整体，失败清单进入 dataLimitations。

    参数：
        node: 节点名，如 '云公司->贵州'
        projects: 限定 EasyViews 项目，留空则用节点全部 projectName
        product_name: 产品名，默认 ARCH_PRODUCT_NAME
        min_score: 映射最低置信分，默认 ARCH_MAPPING_MIN_SCORE
        caller_range: Pinpoint serverMap 上游范围，默认 2
        callee_range: Pinpoint serverMap 下游范围，默认 2
        top_n: 每类结论条数上限，默认 ARCH_LINK_TOP_N
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
    """
    return await arch_tools.analyze_pinpoint_dependencies(
        node=node,
        projects=projects,
        product_name=product_name,
        min_score=min_score,
        caller_range=caller_range,
        callee_range=callee_range,
        top_n=top_n,
        task_id=task_id or None,
    )


@mcp.tool()
async def summarize_architecture_findings_tool(
    center: dict | None = None,
    province: dict | None = None,
    task_id: str = "",
) -> dict:
    """汇总中心节点与省节点分析结果，生成方案第五章报告结构（不做新的取数）。

    center 传 analyze_node_runtime 的返回，province 传 analyze_host_concentration
    的返回；本工具只做确定性归一化、优先级建议与数据限制汇总，
    输出 scope / overview / entries / dataLimitations。

    参数：
        center: 中心节点分析结果（analyze_node_runtime_tool 返回值）
        province: 省节点分析结果（analyze_host_concentration_tool 返回值）
        task_id: 任务标识，透传到结果用于报告追溯（留空自动生成）
    """
    return await arch_tools.summarize_architecture_findings(
        center=center,
        province=province,
        task_id=task_id or None,
    )


@mcp.tool()
async def arch_task_open_tool(
    task_id: str,
    nodes: list[str],
    ttl_minutes: int = 30,
) -> dict:
    """注册跨会话任务作用域（父会话调用一次，Worker 继承同一 task_id）。

    注册后，任意会话（包括无法继承 MCP 配置的子任务 Worker）只要携带该
    task_id 调用 arch 类工具，即被视为同一巡检任务；节点不在白名单内会被
    拒绝（SCOPE_DENIED）。未注册的 task_id 保持原行为，不影响旧调用。

    参数：
        task_id: 任务标识，如 arch-patrol-20261103-0937
        nodes: 本次巡检的节点白名单，如 ['云公司->贵州', '云公司->广州4d']
        ttl_minutes: 作用域有效期（分钟），默认 30
    """
    if not task_id:
        raise ValueError("task_id 不能为空")
    scope = task_scope.get_registry().register(task_id, nodes, ttl_minutes, "arch_task_open")
    return {
        "status": "SUCCESS",
        **scope.to_dict(),
        "note": "Worker 使用同一 task_id 即可调用 arch 工具；无 MCP 连接时走 REST /api/arch/batch",
    }


@mcp.tool()
async def arch_task_status_tool(task_id: str) -> dict:
    """查询任务作用域状态（已注册节点、已使用节点、剩余有效期）。"""
    info = task_scope.get_registry().status(task_id, config.ARCH_TASK_TTL_MINUTES)
    if not info:
        return {"status": "NOT_FOUND", "taskId": task_id}
    return {"status": "SUCCESS", **info}


@mcp.tool()
async def analyze_nodes_runtime_tool(
    task_id: str,
    nodes: list[str],
    rules: list[str] | None = None,
    detail: str = "summary",
    concurrency: int = 0,
    cursor: str = "",
) -> dict:
    """多节点运行架构巡检（一次调用覆盖全部节点，23 节点 1 轮完成）。

    服务端按 ARCH_NODE_CONCURRENCY 并发处理节点，单节点内 8 条规则并发受
    ARCH_BATCH_CONCURRENCY 限制；每节点只回摘要（计数 + topN 风险），
    不回 items / moduleSummary，单节点明细用 analyze_node_runtime_tool 下钻。

    参数：
        task_id: 任务标识（必填），自动注册节点白名单供 Worker 继承
        nodes: 节点清单，如 ['云公司->贵州', '云公司->广州4d']，上限 ARCH_PATROL_MAX_NODES
        rules: 只跑指定规则，留空跑全部 8 条
        detail: 目前固定为 summary（保留参数用于后续 full 明细）
        concurrency: 节点并发数，0 表示用 ARCH_NODE_CONCURRENCY(6)
        cursor: 已完成节点数（断点续跑），留空从头开始
    """
    return await arch_tools.arch_patrol_run(
        task_id=task_id,
        nodes=nodes,
        rules=rules,
        detail=detail,
        concurrency=concurrency or None,
        cursor=cursor or None,
    )


async def _startup() -> None:
    """启动时预登录一次，验证账号可用；失败仅告警，不阻塞服务启动。

    工具调用时仍会按需登录（见 pinpoint_client.get_json 的 401 重试逻辑）。
    """
    client = get_client()
    try:
        await client.login()
        logger.info("pre-login done, ready to serve tools")
    except Exception as e:
        logger.warning("pre-login failed (will retry on first tool call): %s", e)


# 网关可能携带的路径前缀（如 Unla 的 routers.prefix），需重写到 /mcp
_GATEWAY_PATHS = {"/mcp", "/gateway/pinpoint", "/gateway/pinpoint/"}


def _plain_headers(scope) -> dict:
    return {
        k.decode().lower(): v.decode()
        for k, v in (scope.get("headers") or [])
    }


async def _read_json(receive):
    msg = await receive()
    body = msg.get("body") or b""
    try:
        return json.loads(body) if body else {}
    except Exception:
        return None


async def _send_json(scope, receive, send, payload: dict, status_code: int = 200) -> None:
    response = JSONResponse(payload, status_code=status_code)
    await response(scope, receive, send)


async def _handle_arch_api(scope, receive, send) -> bool:
    """REST 批量入口：给无法继承 MCP 工具的子任务 Worker 使用。

    返回 True 表示已处理（调用方不再走 MCP 路由）。
    """
    path = scope.get("path", "").rstrip("/")
    method = scope.get("method", "GET").upper()
    headers = _plain_headers(scope)

    if path == "/api/arch/health":
        await _send_json(
            scope, receive, send,
            {
                "status": "ok",
                "tasks": task_scope.get_registry().count(),
                "nodeConcurrency": config.ARCH_NODE_CONCURRENCY,
                "ruleVersion": config.ARCH_RULE_VERSION,
            },
        )
        return True

    token = headers.get("x-arch-token", "")
    if not config.ARCH_API_TOKEN or token != config.ARCH_API_TOKEN:
        await _send_json(
            scope, receive, send,
            {"status": "FORBIDDEN", "errMsg": "缺少或错误的 X-Arch-Token（服务端需配置 ARCH_API_TOKEN）"},
            403,
        )
        return True

    if path == "/api/arch/batch" and method == "POST":
        body = await _read_json(receive)
        if not isinstance(body, dict):
            await _send_json(scope, receive, send, {"status": "FAIL", "errMsg": "请求体不是合法 JSON"}, 400)
            return True
        task_id = str(body.get("task_id") or "").strip()
        nodes = body.get("nodes") or []
        if not task_id or not isinstance(nodes, list) or not nodes:
            await _send_json(
                scope, receive, send,
                {"status": "FAIL", "errMsg": "task_id 与 nodes 均为必填，nodes 需为非空数组"},
                400,
            )
            return True
        try:
            result = await arch_tools.arch_patrol_run(
                task_id=task_id,
                nodes=[str(n) for n in nodes],
                rules=body.get("rules"),
                concurrency=int(body.get("concurrency") or 0) or None,
                cursor=str(body.get("cursor") or "") or None,
            )
            await _send_json(scope, receive, send, result)
        except Exception as e:
            await _send_json(
                scope, receive, send,
                {"status": "FAIL", "taskId": task_id, "errMsg": str(e)[:300]},
                500,
            )
        return True

    if path.startswith("/api/arch/task/") and method == "GET":
        task_id = path.rsplit("/", 1)[-1]
        info = task_scope.get_registry().status(task_id, config.ARCH_TASK_TTL_MINUTES)
        payload = {"status": "SUCCESS", **info} if info else {"status": "NOT_FOUND", "taskId": task_id}
        await _send_json(scope, receive, send, payload, 200 if info else 404)
        return True

    await _send_json(scope, receive, send, {"error": "not found"}, 404)
    return True


def main() -> None:
    async def _run():
        await _startup()
        logger.info("starting MCP server %s on %s:%s", config.MCP_NAME, config.MCP_HOST, config.MCP_PORT)

        # FastMCP 内部 ASGI 应用，路由注册在 /mcp
        mcp_app = mcp.streamable_http_app()

        # 路径重写中间件：兼容网关前缀（/gateway/pinpoint）与标准 /mcp
        async def routed_app(scope, receive, send):
            if scope["type"] == "http":
                path = scope.get("path", "")
                if path.startswith("/api/arch"):
                    await _handle_arch_api(scope, receive, send)
                    return
                if path in _GATEWAY_PATHS:
                    scope = dict(scope)
                    scope["path"] = "/mcp"
                    scope["raw_path"] = b"/mcp"
                    await mcp_app(scope, receive, send)
                    return
                response = JSONResponse({"error": "not found"}, status_code=404)
                await response(scope, receive, send)
                return
            await mcp_app(scope, receive, send)

        server = uvicorn.Server(
            uvicorn.Config(routed_app, host=config.MCP_HOST, port=config.MCP_PORT, log_level="info")
        )
        await server.serve()

    try:
        asyncio.run(_run())
    except KeyboardInterrupt:
        logger.info("shutdown")


if __name__ == "__main__":
    main()
