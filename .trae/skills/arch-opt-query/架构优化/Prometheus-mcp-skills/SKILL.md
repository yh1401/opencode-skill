---
name: Prometheus-mcp助手【天翼视联】
skill_id: tianyi_shilian_prometheus_mcp
version: V1
description: 天翼视联智能体平台 Prometheus MCP 助手技能定义，以 Prometheus MCP Server 的 35 个工具为能力底座，覆盖节点拓扑采集、应用指标查询、故障域分析、主机监控、运行时巡检、文档检索、平台运维与架构优化定时分析编排
author: 架构小组
tags: ["天翼视联","Prometheus","MCP","架构优化","监控采集","风险分析"]
category: 运维工具
---

# SKILL.md

>
> 文档版本：V1
> 对应服务：Prometheus MCP Server（HTTP transport，`/mcp` 端点）
> 用途：天翼视联智能体平台助手技能定义，用于平台导入、工具编排与任务执行
> 调度模式：支持**用户手动触发**、**每日定时调度（cron: `0 3 * * *`）**
> 能力底座：MCP Server 的 35 个工具（Prometheus 后端），全部只读或受控写操作
>
> 状态枚举全局统一：
>
> - `SUCCESS`：执行成功且返回有效数据
> - `NO_DATA`：接口成功但结果集为空
> - `DATA_INSUFFICIENT`：部分采样点失败、指标缺失或标签口径不匹配
> - `PARTIAL_FAIL`：多目标调用中部分失败
> - `FAIL`：调用失败或鉴权/网络异常
> - `NOT_APPLY`：该对象不适用此规则（如无 HPA 配置的应用）
>
> 约束：接口失败、监控缺失不可直接输出"无风险"结论，必须标记 `DATA_INSUFFICIENT` 或 `FAIL`。

## 公共入参（所有技能继承）

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| task_id | string | 任务唯一标识，每次分析自动生成 |
| target_center_nodes | array[string] | 中心节点列表，默认 `["云公司->贵州","云公司->广州4d"]` |
| product_name | string | 产品名称，默认 `天翼看家` |
| analyze_config | object | 分析配置：监控窗口 15min、链路统计窗口 5min、采样步长 60s |
| enable_province_analysis | bool | 是否开启省节点故障域分析 |
| truncation_limit | int | 响应截断行数，默认取服务端全局值，`-1` 表示不截断 |

---

## 一、服务接入配置

### 1.1 MCP 端点

| 配置项 | 值 | 说明 |
| --- | --- | --- |
| 传输协议 | HTTP（Streamable HTTP） | `--mcp.transport=http` |
| 端点地址 | `http://<server>:8090/mcp` | 平台侧填入此 URL |
| 鉴权 | 无需 API Key | 当前版本未启用认证，留空即可 |
| 会话超时 | 10m | `--mcp.session-timeout=10m` |

### 1.2 服务端启动参数

```bash
./prometheus-mcp-server \
  --mcp.transport=http \
  --web.listen-address=:8090 \
  --prometheus.url=http://183.56.210.90:8082 \
  --easyviews.province=大模型 \
  --easyviews.app-id=<APP_ID> \
  --easyviews.app-secret=<APP_SECRET>
```

> 说明：`--easyviews.*` 三项缺任一项时，架构拓扑类 5 个工具返回"easyviews 平台未配置"错误；其余 30 个工具不受影响。

### 1.3 环境变量等价配置

| 命令行标志 | 环境变量 |
| --- | --- |
| `--prometheus.url` | `PROMETHEUS_MCP_SERVER_PROMETHEUS_URL` |
| `--mcp.transport` | `PROMETHEUS_MCP_SERVER_MCP_TRANSPORT` |
| `--web.listen-address` | `PROMETHEUS_MCP_SERVER_WEB_LISTEN_ADDRESS` |
| `--easyviews.province` | `PROMETHEUS_MCP_SERVER_EASYVIEWS_PROVINCE` |
| `--easyviews.app-id` | `PROMETHEUS_MCP_SERVER_EASYVIEWS_APP_ID` |
| `--easyviews.app-secret` | `PROMETHEUS_MCP_SERVER_EASYVIEWS_APP_SECRET` |
| `--easyviews.timeout` | `PROMETHEUS_MCP_SERVER_EASYVIEWS_TIMEOUT` |

---

## 二、工具能力总览

服务端共注册 **35 个工具**（Prometheus 后端）。按能力域分组如下，"属性"列标明是否为写操作。

### 2.1 架构拓扑采集（easyviews，5 个）

| 工具名 | 属性 | 用途 |
| --- | --- | --- |
| `get_node_list` | 只读 | 节点列表与标识映射（node / ctyun_node_code3） |
| `get_module_topology` | 只读 | 节点下模块与应用归属（projectName） |
| `get_chain_topology` | 只读 | 应用调用链路拓扑（nodes / edges / 统计） |
| `get_virtual_structure` | 只读 | 物理宿主机与云主机关系 |
| `get_project_topology` | 只读 | 项目与云主机部署关系 |

### 2.2 指标查询与探索（8 个）

| 工具名 | 属性 | 用途 |
| --- | --- | --- |
| `query` | 只读 | 瞬时查询，支持 `timestamp` 指定历史时间点 |
| `range_query` | 只读 | 范围查询（架构分析 15min 窗口**禁止**使用） |
| `exemplar_query` | 只读 | 示例（trace）查询 |
| `series` | 只读 | 按标签选择器查找序列 |
| `label_names` | 只读 | 查询标签名集合 |
| `label_values` | 只读 | 查询指定标签的值集合 |
| `metric_metadata` | 只读 | 指标元数据 |
| `targets_metadata` | 只读 | 目标抓取元数据 |

### 2.3 主机性能指标（2 个）

| 工具名 | 属性 | 用途 |
| --- | --- | --- |
| `host_metrics_realtime` | 只读 | 主机实时 12 项性能指标 |
| `host_metrics_trend` | 只读 | 主机性能趋势（时间范围 + 步长） |

### 2.4 运行时巡检与告警（12 个）

| 工具名 | 属性 | 用途 |
| --- | --- | --- |
| `healthy` | 只读 | Prometheus 健康检查 |
| `ready` | 只读 | Prometheus 就绪检查 |
| `build_info` | 只读 | 构建信息 |
| `runtime_info` | 只读 | 运行时信息 |
| `flags` | 只读 | 运行时标志 |
| `config` | 只读 | 运行配置 |
| `tsdb_stats` | 只读 | TSDB 使用与基数统计 |
| `wal_replay_status` | 只读 | WAL 重放状态 |
| `alertmanagers` | 只读 | Alertmanager 发现概览 |
| `list_alerts` | 只读 | 活动告警列表 |
| `list_rules` | 只读 | 告警与记录规则 |
| `list_targets` | 只读 | 目标发现概览 |

### 2.5 文档检索（3 个）

| 工具名 | 属性 | 用途 |
| --- | --- | --- |
| `docs_list` | 只读 | 列出官方文档文件 |
| `docs_read` | 只读 | 读取指定文档 |
| `docs_search` | 只读 | 搜索文档内容 |

### 2.6 平台运维管理（5 个，**写操作**）

| 工具名 | 属性 | 风险 | 用途 |
| --- | --- | --- | --- |
| `snapshot` | 写 | 中 | 创建 TSDB 数据快照 |
| `clean_tombstones` | 写 | 中 | 清理墓碑、回收磁盘 |
| `delete_series` | 写 | **高** | 删除指定时间范围的序列数据 |
| `reload` | 写 | 中 | 重载配置与规则 |
| `quit` | 写 | **高** | 优雅关闭 Prometheus |

> 此类工具需在平台侧单独授权、限制调用范围，默认不开放给自动编排流程。
> 附加说明：`list_stores` 仅在 Thanos 后端注册，当前 Prometheus 后端不可用。

---

## 三、技能定义

## 1. arch_topology_collect 架构拓扑与故障域采集

- skill_id: `arch_topology_collect`
- 分类：数据采集
- 描述：调用 easyviews 网关（`getTykjProvince`）完成节点、模块、调用链、物理机与云主机的原始数据采集，建立 **节点‑模块‑应用** 与 **云主机‑物理宿主机** 归属关系。
- 封装工具：`get_node_list`、`get_module_topology`、`get_chain_topology`、`get_virtual_structure`、`get_project_topology`

### 覆盖工具与入参

**`get_node_list`** — 获取节点清单

```json
{ "center": "true", "name_prefix": "云公司" }
```

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| center | 否 | 是否中心节点，默认 `true` |
| name_prefix | 否 | 节点名前缀过滤，为空返回全部 |

返回字段：`node`（节点名）、`ctyun_node_code3`（云平台节点编码）、节点 ID、省份信息。

---

**`get_module_topology`** — 建立节点‑模块‑应用归属

```json
{ "node": "云公司->贵州", "product_name": "天翼看家", "center": "true" }
```

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| node | **是** | 取自 `get_node_list` 的 `node` 字段 |
| product_name | **是** | 产品名称，如 `天翼看家` |
| center | 否 | 默认 `true` |

返回字段：`模块标识`、`中文名称`、`对象类型`、`projectName`（应用项目列表，可能为数组）、`deployNode`。

- 约束：`projectName` 为数组时取首元素作为应用唯一键；同一应用归属多个模块时**保留全部归属关系**（去重仅用于指标查询，归属关系不去重）
- 约束：以 `-dep` 结尾的应用为待监控应用；本阶段未覆盖的应用需单独列出
- 约束：缺少 cluster/namespace 时应用唯一键退化为处理后的 `projectName`，须输出重名风险告警

---

**`get_chain_topology`** — 服务调用链路拓扑

```json
{
  "node": "<deployNode>",
  "projects": ["vnet-vpaas-manage-dep"],
  "callee_range": 1,
  "caller_range": 1,
  "minute": 5
}
```

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| node | **是** | 使用 `get_module_topology` 返回的 `deployNode`，可含多个逗号分隔节点 |
| projects | **是** | 目标模块的应用项目名列表，取自 `projectName` |
| center | 否 | 默认 `true` |
| callee_range / caller_range | 否 | 下游/上游调用深度，默认 `1` |
| filter_node | 否 | 是否过滤节点，默认 `false` |
| minute | 否 | 统计时间窗口（分钟），默认 `5` |
| node_type | 否 | 节点类型过滤，默认空 |

返回字段：`nodes`、`edges`、`call_statistic`。用于识别集中依赖、下游依赖过多、长调用链、异常依赖。链路数据缺失时标记 `DATA_INSUFFICIENT`，**禁止编造依赖关系**。

---

**`get_virtual_structure`** — 物理宿主机与云主机关系

```json
{ "node": "hi-haikou-9", "center": "true" }
```

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| node | **是** | **使用 `get_node_list` 返回的 `ctyun_node_code3`**，例如 `hi-haikou-9` |

返回：物理宿主机 IP 及承载的云主机 IP 列表。用于反向构建 `云主机IP → 物理宿主机IP` 映射。

- 约束：`ctyun_node_code3` 为空的节点**跳过**查询并标记 `DATA_INSUFFICIENT`，不发起无效调用
- 约束：映射覆盖率 < 100% 必须输出警告，未匹配 IP 保留不丢弃

---

**`get_project_topology`** — 项目与云主机部署关系

```json
{ "node": "云公司->海口9", "center": "true" }
```

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| node | **是** | **使用 `get_node_list` 返回的 `node` 字段**（注意与 `get_virtual_structure` 的口径差异） |

返回：项目、项目对应云主机 IP 列表、云主机上的项目。用于分析多实例是否集中于同一宿主机（故障域集中风险）。

### 输出

```json
{
  "status": "SUCCESS|PARTIAL_FAIL|FAIL",
  "err_msg": "",
  "node_items": [
    {
      "node_name": "云公司->贵州",
      "module_count": 6,
      "affiliation_list": [],
      "dep_app_list": [],
      "distinct_dep_app_list": [],
      "not_cover_app_list": []
    }
  ],
  "province_fault_domain": {
    "vm_to_physical_host_map": {},
    "project_vm_deploy": {},
    "mapping_coverage": 0.78,
    "unmapped_vm_ip_list": []
  },
  "chain_topology": { "nodes": [], "edges": [], "call_statistic": [] }
}
```

### 约束

1. 单节点接口失败不阻断其余节点采集，返回 `PARTIAL_FAIL` 并记录失败节点
2. 两个"node"参数口径不同（`ctyun_node_code3` vs `node`），调用前必须先用 `get_node_list` 取值，不可混用
3. 节点名必须与平台完全一致（含 `->` 分隔符）；不确定时先调 `get_node_list` 用 `name_prefix` 检索

### 异常行为

鉴权失败返回 `easyviews 返回业务错误，code=7`；平台未配置返回"easyviews 平台未配置"；网络不通返回"请求 easyviews 网关失败"。三类均记入 `err_msg`，不中断整体任务。

---

## 2. app_metric_collect 应用运行指标采集

- skill_id: `app_metric_collect`
- 分类：数据采集
- 描述：针对拓扑输出的去重 `-dep` 应用，通过 `query` 工具以**多个历史时间戳**模拟 15min 窗口（步长 60s、共 15 个采样点），采集副本、CPU 限流、内存、HPA、Pod 负载、OOM 指标。
- 封装工具：`query`（主）、`series`、`label_values`、`metric_metadata`（标签口径校验）

### 入参

```json
{
  "task_id": "string",
  "distinct_dep_app_list": ["xxx-dep"],
  "time_window": 900,
  "step": 60,
  "promql_config": {
    "replica_spec": "kube_deployment_spec_replicas",
    "replica_available": "kube_deployment_status_replicas_available",
    "cpu_throttled": "container_cpu_cfs_throttled_periods_total",
    "cpu_periods": "container_cpu_cfs_periods_total",
    "mem_working_set": "container_memory_working_set_bytes",
    "mem_limit": "kube_pod_container_resource_limits{resource=\"memory\"}",
    "hpa_current": "kube_horizontalpodautoscaler_status_current_replicas",
    "hpa_max": "kube_horizontalpodautoscaler_spec_max_replicas",
    "pod_cpu": "container_cpu_usage_seconds_total",
    "oom_reason": "kube_pod_container_status_last_terminated_reason{reason=\"OOMKilled\"}",
    "restarts": "kube_pod_container_status_restarts_total"
  }
}
```

### 采样方式（关键约束）

对每个应用、每个指标，循环调用 `query` 15 次，`timestamp` 取 `end_ts - offset`（offset 从 0 递增到 900，步长 60）：

```json
{ "query": "sum(your_metric{app=\"xxx-dep\"})", "timestamp": 1792037172 }
```

### 输出

```json
{
  "total_app": 3,
  "per_app_sample_points": 15,
  "total_request": 45,
  "app_metric_items": [
    {
      "app_unique_key": "xxx-dep",
      "sample_points": [],
      "success_point_count": 12,
      "fail_point_count": 3,
      "error_list": [],
      "raw_metric": {}
    }
  ]
}
```

### 约束

1. **禁止使用 `range_query`**——15min 窗口必须由多次 `query` 历史时间点采样构成
2. 单次瞬时值不能证明"持续 15 分钟异常"，必须多点采样后判定持续性
3. 指标空返回或采样点失败 → 交由规则层标记 `DATA_INSUFFICIENT`，禁止直接判定无风险
4. 指标名与标签（`cmdb_node`、`env`、`deployment` 等）以实际环境为准，先调 `label_names` / `series` / `metric_metadata` 校验存在性再执行大批量采样
5. `container_*` 类 cAdvisor 指标与 `kube_*` 类 kube-state-metrics 指标的标签口径可能不同，跨域关联需经 namespace/pod 补齐标签

### 异常行为

网络断开（如 `10053` 连接中止）、超时、单点失败均不抛异常，写入 `error_list`，任务继续。

---

## 3. host_metric_query 主机性能指标查询

- skill_id: `host_metric_query`
- 分类：主机监控
- 描述：查询指定节点或 IP 的主机性能指标，实时值或趋势，共 12 项（CPU 核数/使用率、内存总量/使用率、磁盘容量/使用率、网络连接数、网络流量、磁盘 IO、系统负载 1/5/15m、TCP 连接溢出、总 TCP 连接数）。
- 封装工具：`host_metrics_realtime`、`host_metrics_trend`

### 入参

**`host_metrics_realtime`**

```json
{ "node": "云公司->贵州", "ip": "192.168.7.66" }
```

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| node | 否 | 节点名称 |
| ip | 否 | 节点 IP（与 node 至少提供一个） |

**`host_metrics_trend`**

```json
{ "ip": "192.168.7.66", "start_time": "1h", "end_time": "", "step": "5m" }
```

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| start_time | **是** | 支持 `1h`/`24h` 相对时间、Unix 时间戳、RFC3339 |
| end_time | 否 | 默认当前时间 |
| step | 否 | Go 时长格式，未指定时自动推导 |

### 输出与取值约定

返回含各指标 `*_latest` 字段（当前/区间末值）与原始数据。提取数值优先用 `*_latest`。

建议步长：1 小时数据用 `5m`；24 小时用 `15m`/`30m`；7 天用 `1h`。

### 约束

1. 必须提供 `node` 或 `ip` 至少其一，否则查询无目标
2. 数据返回 `N/A` 多为该主机未部署 node_exporter 或指标缺失，标记 `NO_DATA`，不代表主机异常
3. 健康阈值参考：CPU > 80%、内存 > 85%、磁盘 > 90% 视为偏高

---

## 4. metric_explore 指标探索与元数据

- skill_id: `metric_explore`
- 分类：指标探索
- 描述：在正式采集前校验指标与标签口径，回答"有哪些指标、标签取值是什么、序列是否存在"。
- 封装工具：`label_names`、`label_values`、`series`、`metric_metadata`、`targets_metadata`

### 入参

| 工具 | 入参 | 示例 |
| --- | --- | --- |
| `label_names` | `matches`（可选）、时间范围 | `{"matches":["up"]}` |
| `label_values` | `label`（**必填**）、`matches` | `{"label":"job"}` |
| `series` | `matches`（**必填**） | `{"matches":["container_cpu_cfs_periods_total"]}` |
| `metric_metadata` | `metric`、`limit` | `{"metric":"up","limit":"5"}` |
| `targets_metadata` | `match_target`、`metric`、`limit` | `{"limit":"5"}` |

### 典型用途

1. 校验 `container_*` / `kube_*` 指标是否带 `cmdb_node`、`env` 标签
2. 校验目标应用是否已有 `-dep` 后缀的序列存在
3. 获取指标含义辅助风险评估解释

### 约束

探索类查询同样受响应截断限制影响，必要时传 `truncation_limit: -1`；大结果集建议先用 `limit` 收敛。

---

## 5. prometheus_runtime_inspect Prometheus 运行时巡检

- skill_id: `prometheus_runtime_inspect`
- 分类：平台巡检
- 描述：对 Prometheus 自身做健康与运行状态巡检，覆盖存活、就绪、构建/运行时信息、配置、规则、目标、告警、TSDB 与 WAL。
- 封装工具：`healthy`、`ready`、`build_info`、`runtime_info`、`flags`、`config`、`tsdb_stats`、`wal_replay_status`、`alertmanagers`、`list_alerts`、`list_rules`、`list_targets`

### 入参

除 `exemplar_query` 外，上述工具均为**无参数**，调用时传空对象 `{}` 即可。

### 巡检顺序建议

```text
healthy → ready → build_info → runtime_info → tsdb_stats → wal_replay_status
        → list_targets（目标抓取异常）→ list_rules → list_alerts（活动告警）
```

### 判定

| 工具 | 判定要点 |
| --- | --- |
| `healthy` | 返回成功即服务存活 |
| `ready` | 失败说明 Prometheus 未就绪，查询结果不可信 |
| `tsdb_stats` | 关注序列基数与内存占用，突增需排查标签爆炸 |
| `wal_replay_status` | 有重放活动说明近期发生过重启 |
| `list_targets` | 关注 `down` 状态目标，直接影响指标可用性 |
| `list_alerts` | 活动告警列表，可作为风险佐证 |

### 约束

`config` 与 `flags` 返回内容可能较大，注意截断设置；巡检发现的指标缺失应回溯到数据源层，不可归因为业务风险。

---

## 6. docs_assistant Prometheus 文档助手

- skill_id: `docs_assistant`
- 分类：知识检索
- 描述：检索官方 Prometheus 文档，用于编写 PromQL、解释指标语义、核对配置项。
- 封装工具：`docs_list`、`docs_read`、`docs_search`

### 入参

| 工具 | 入参 | 示例 |
| --- | --- | --- |
| `docs_list` | 无 | `{}` |
| `docs_read` | `file`（**必填**） | `{"file":"querying/basics.md"}` |
| `docs_search` | `query`（**必填**）、`limit` | `{"query":"histogram_quantile","limit":5}` |

### 使用建议

- 编写复杂 PromQL 前先用 `docs_search` 检索语法要点
- `docs_read` 的 `file` 取值必须先来自 `docs_list` 的返回结果

---

## 7. platform_maintenance 平台运维管理（受控写操作）

- skill_id: `platform_maintenance`
- 分类：运维管理
- 描述：TSDB 数据快照、墓碑清理、序列删除、配置重载、服务关闭。**全部为写操作，默认不纳入自动编排**。
- 封装工具：`snapshot`、`clean_tombstones`、`delete_series`、`reload`、`quit`

### 入参与风险

| 工具 | 入参 | 风险 | 前置条件 |
| --- | --- | --- | --- |
| `snapshot` | `{"skip_head": false}` | 中 | 磁盘空间充足 |
| `clean_tombstones` | 无 | 中 | 存在墓碑且需回收空间 |
| `delete_series` | `{"matches":[...], "start_time":"...", "end_time":"..."}` | **高** | `start_time`/`end_time` **必须同时提供** |
| `reload` | 无 | 中 | 配置文件已就绪且校验通过 |
| `quit` | 无 | **高** | 需人工二次确认 |

### 约束

1. `delete_series` 缺省时间范围会拒绝执行（服务端强制校验），**绝不允许不带时间范围调用**
2. `quit` 会导致 Prometheus 停止服务，所有依赖它的查询技能同时失效，仅限维护窗口执行
3. 此类工具在平台侧需单独授权，且调用前必须记录操作人与原因

---

## 四、task_orchestrator 架构优化定时分析总调度

- skill_id: `task_orchestrator`
- 分类：调度编排（顶层入口）
- 描述：智能体顶层入口，支持手动触发与 cron 定时触发；编排各子技能，串接 **MCP 工具调用 → 对象关联 → 风险规则 → 日志与报告** 全流程。
- 依赖技能：`arch_topology_collect`、`app_metric_collect`、`host_metric_query`、`metric_explore`

### 调度参数

| 参数 | 值 | 说明 |
| --- | --- | --- |
| cron | `0 3 * * *` | 每日 03:00 执行 |
| 触发方式 | 手动 / 定时 | 双模式 |
| target_center_nodes | `["云公司->贵州","云公司->广州4d"]` | 默认中心节点 |
| product_name | `天翼看家` | 默认产品 |
| enable_province_analysis | `true` | 是否开启省节点故障域分析 |

### 执行流程与工具映射

| 序号 | 阶段 | 调用工具 / 动作 | 产出 |
| --- | --- | --- | --- |
| 1 | 初始化 | 生成 `task_id`，校验 `healthy` | 任务上下文 |
| 2 | 节点校验 | `get_node_list`（`name_prefix=云公司`） | 节点清单与 `ctyun_node_code3` 映射 |
| 3 | 拓扑采集 | `get_module_topology`（逐中心节点、`product_name=天翼看家`） | 节点‑模块‑应用归属、`-dep` 去重应用列表 |
| 4 | 口径校验 | `label_names` / `series` | 指标与标签可用性确认 |
| 5 | 指标采集 | `query` × 15 采样点（步长 60s，窗口 900s） | 副本/限流/内存/HPA/Pod 负载/OOM 指标 |
| 6 | 故障域采集 | `get_virtual_structure` + `get_project_topology` | 云主机‑宿主机映射、部署集中度 |
| 7 | 链路采集（可选） | `get_chain_topology` | 依赖拓扑、集中依赖、异常依赖 |
| 8 | 主机巡检（可选） | `host_metrics_realtime` / `host_metrics_trend` | 节点级资源佐证 |
| 9 | 对象关联 | 平台侧聚合上述输出 | `节点‑模块‑应用‑指标‑云主机‑物理宿主机` 关联表 |
| 10 | 风险规则 | 平台侧规则引擎判定 | 每条对象的风险状态、实际值、阈值、证据 |
| 11 | 日志产出 | 平台侧生成定时日志 | `agent_arch_optim_<date>.log` |
| 12 | 报告产出 | 平台侧生成 Markdown 报告 | 按 节点→模块→应用 层级组织 |

> 阶段 1–8 由 MCP 工具承载；阶段 9–12 为平台侧计算与产出（**大模型只做解释，不篡改规则引擎数值**）。

### 风险规则阈值（阶段 10 判定依据）

| 风险类型 | 指标 | 阈值 |
| --- | --- | --- |
| 副本可用性 | `kube_deployment_spec_replicas` / `kube_deployment_status_replicas_available` | 期望副本 ≤ 2 视为冗余不足 |
| CPU 限流 | `container_cpu_cfs_throttled_periods_total` ÷ `container_cpu_cfs_periods_total` | > 20% 持续 15min |
| 内存容量 | `container_memory_working_set_bytes` ÷ `kube_pod_container_resource_limits{resource="memory"}` | > 85% 持续 15min |
| 扩容容量 | `kube_horizontalpodautoscaler_status_current_replicas` ≥ `..._spec_max_replicas` | 达到上限且负载仍高 |
| Pod 负载不均衡 | `container_cpu_usage_seconds_total`（按 deployment 聚合） | max/avg > 2 持续 15min |
| OOM | `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` + `kube_pod_container_status_restarts_total` | OOMKilled 且近 30min 有重启 |
| 故障域集中 | `get_virtual_structure` / `get_project_topology` 映射 | 多实例落同一宿主机 |

### 输出

```json
{
  "task_id": "",
  "task_status": "success|partial_failed|failed",
  "log_file_path": "",
  "report_file_path": "",
  "mid_file_path": "./output_result.json",
  "statistics": {}
}
```

---

## 五、调用依赖关系

```text
task_orchestrator（总控入口）
├─ get_node_list（节点清单，先决条件）
├─ get_module_topology ──→ 输出 distinct_dep_app_list ──┐
├─ app_metric_collect（query 多点采样）  ←──────────────┘
├─ get_virtual_structure + get_project_topology（故障域，依赖 ctyun_node_code3 / node）
├─ get_chain_topology（可选，依赖 deployNode + projectName）
├─ host_metric_query（可选，节点资源佐证）
└─ [平台侧] 对象关联 → 风险规则 → 日志/报告
```

关键数据依赖链：

1. `get_node_list` → 提供 `node` 与 `ctyun_node_code3`
2. `get_module_topology` → 提供 `projectName`（应用列表）与 `deployNode`
3. 上述两者 → 驱动 `app_metric_collect`、`get_virtual_structure`、`get_project_topology`、`get_chain_topology`

---

## 六、平台配置与运维

1. **助手注册**：平台侧以 HTTP 方式接入 MCP 端点 `http://<server>:8090/mcp`，无需 API Key
2. **工具授权**：默认放行 30 个只读工具；`platform_maintenance` 组（5 个写工具）单独授权
3. **定时调度**：绑定 `task_orchestrator`，cron `0 3 * * *`
4. **告警规则**：
   - ERROR：`task_status=failed`（核心接口全部失效）触发告警
   - WARN：`task_status=partial_failed` 仅日志留存，人工巡检
   - WARN：出现 `DATA_INSUFFICIENT` 必须在报告中显式标注数据缺口
5. **产物持久化**：中间 JSON、定时日志、Markdown 报告全部落盘，支持按 `task_id` 回溯
6. **连通性自检**（部署后必做）：
   - `curl -s http://127.0.0.1:8090/metrics | grep easyviews` 观察 `api_calls_failed_total`
   - 调用 `get_node_list` 确认 easyviews 鉴权通路，再调用 `get_module_topology` 确认能取到归属数据
   - 调用 `query`（`{"query":"up"}`）确认 Prometheus 通路

---

## 七、故障排查对照

| 现象 | 可能原因 | 处置 |
| --- | --- | --- |
| `unknown tool "xxx"` | 服务端二进制版本不含该工具 | 重新编译并替换二进制，`--version` 校验 build date |
| `easyviews 平台未配置` | 未提供 app-id / app-secret | 补全启动参数或环境变量 |
| `easyviews 返回业务错误，code=7` | 出口 IP 未加白 或 调用侧编码问题 | 在服务器端直连网关自测；必要时申请 IP 加白 |
| `请求 easyviews 网关失败` | 网络不通 | 检查 DNS、出方向策略、代理 |
| 指标查询超时 / `N/A` | Prometheus 地址错误或未放行 | 校验 `--prometheus.url`（不要带 `/api/v1` 后缀），自测 `/api/v1/query?query=up` |
| 工具返回为空数组 | 平台侧该节点无拓扑数据 | 属正常，标记 `NO_DATA`，非故障 |
