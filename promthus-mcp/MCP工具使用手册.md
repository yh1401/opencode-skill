# Prometheus MCP Server 工具使用手册

本文档整理本 MCP 服务器对外暴露的**全部工具**的用途、参数、调用示例与注意事项。参数定义与代码严格一致，可直接作为 MCP 客户端接入与提示词编写的参考。

- 平台：Prometheus MCP Server（`github.com/tjhop/prometheus-mcp-server`）
- 传输方式：`stdio`（默认）、`http`（Streamable HTTP / SSE）
- 工具数量：`prometheus` 后端 **35 个**；`thanos` 后端 **31 个**
- 所有工具均以 JSON 返回结果（可选 TOON 编码）

---

## 一、快速开始

### 1.1 启动服务

**stdio 模式**（本地进程，由客户端拉起）：

```bash
prometheus-mcp-server --prometheus.url=http://127.0.0.1:9090
```

**HTTP 模式**（远程服务）：

```bash
prometheus-mcp-server \
  --mcp.transport=http \
  --web.listen-address=:8080 \
  --prometheus.url=http://127.0.0.1:9090 \
  --easyviews.app-id=<appId> \
  --easyviews.app-secret=<appSecret>
```

### 1.2 MCP 客户端配置

stdio：

```json
{
  "mcpServers": {
    "prometheus": {
      "command": "/usr/local/bin/prometheus-mcp-server",
      "args": ["--prometheus.url=http://127.0.0.1:9090"]
    }
  }
}
```

HTTP：

```json
{
  "mcpServers": {
    "prometheus": {
      "type": "http",
      "url": "http://<server>:8080/mcp"
    }
  }
}
```

> HTTP 模式下，客户端请求头中的 `Authorization` 会自动转发给 Prometheus，可用于多租户场景（不同用户携带不同凭据）。

---

## 二、通用调用约定

### 2.1 时间参数格式

凡 `*_time` 参数（`timestamp`、`start_time`、`end_time`）均支持三种格式：

| 格式 | 示例 | 说明 |
|---|---|---|
| Unix 时间戳（秒） | `1782115200` | 整数秒 |
| RFC3339 | `2026-06-23T10:00:00Z` | 标准时间字符串 |
| 相对时长 | `5m`、`1h30m`、`1d` | 相对于"现在"向前推算 |

各工具的时间默认值见对应章节。

### 2.2 截断参数

`query` / `range_query` / `exemplar_query` / `series` / `label_names` / `label_values` 支持：

| 参数 | 类型 | 说明 |
|---|---|---|
| `truncation_limit` | int | 响应最大行数/条目数。`-1` 表示本次调用禁用截断；`>0` 覆盖全局限制；缺省时沿用服务端 `--prometheus.truncation-limit` |

`metric_metadata` / `targets_metadata` 使用 `limit`（string 类型）控制返回数量；`docs_search` 使用 `limit`（int）。

结果被截断时，返回文本末尾会附加截断警告，提示当前生效的限制值。

### 2.3 工具集选择（`--mcp.tools`）

| 取值 | 效果 |
|---|---|
| `all`（默认） | 加载全部 35 个工具 |
| `core` | 仅加载核心工具：`docs_list`、`docs_read`、`docs_search`、`query`、`range_query`、`metric_metadata`、`label_names`、`label_values`、`series` |
| 工具名列表 | 指定工具 + 核心工具（始终包含核心工具） |

> 注意：一旦显式设置 `--prometheus.backend`（`prometheus` 或 `thanos`），后端工具集会**整体替换**已选工具集，此时 `--mcp.tools` 选择失效。

### 2.4 后端与工具差异（`--prometheus.backend`）

| 差异 | 说明 |
|---|---|
| `prometheus` | 全量 35 个工具 |
| `thanos` | 移除 `alertmanagers`、`config`、`wal_replay_status`、`reload`、`quit`；新增 `list_stores`，共 31 个 |
| 未设置 | 保留按 `--mcp.tools` 计算的工具集 |

### 2.5 错误返回

工具执行失败时返回 `isError: true` 与中文错误文本，常见形态：

- 参数校验错误：`node 参数是必需的`
- 上游调用错误：`范围查询 API 调用失败: ...`
- 未启用能力：`清理墓碑 API 调用失败: ... TSDB 管理工具未启用`
- 未配置集成：`easyviews 平台未配置，请通过 --easyviews.app-id 与 --easyviews.app-secret 提供鉴权信息`

---

## 三、工具总览

### 3.1 查询类（7 个）

| 工具 | 用途 | 必填参数 |
|---|---|---|
| `query` | 瞬时查询 | `query` |
| `range_query` | 范围查询 | `query` |
| `exemplar_query` | 示例（trace）查询 | `query` |
| `series` | 按标签匹配查找序列 | `matches` |
| `label_names` | 标签名称列表 | 无 |
| `label_values` | 标签值列表 | `label` |
| `metric_metadata` | 指标元数据 | 无 |

### 3.2 状态与信息类（11 个，均无参数）

`targets_metadata`、`alertmanagers`、`flags`、`list_alerts`、`tsdb_stats`、`build_info`、`config`、`runtime_info`、`list_rules`、`list_targets`、`wal_replay_status`

> `targets_metadata` 例外：参数可选，见 4.8。

### 3.3 管理类（6 个）

| 工具 | 用途 | 必填参数 | 前置条件 |
|---|---|---|---|
| `healthy` | 健康检查 | 无 | — |
| `ready` | 就绪检查 | 无 | — |
| `reload` | 重载配置 | 无 | Prometheus `--web.enable-lifecycle` |
| `quit` | 优雅关闭 | 无 | Prometheus `--web.enable-lifecycle` |
| `clean_tombstones` | 清理墓碑 | 无 | 见 §7.3 |
| `delete_series` | 删除序列数据 | `matches`、`start_time`、`end_time` | 见 §7.3 |
| `snapshot` | 创建 TSDB 快照 | 无 | 见 §7.3 |

### 3.4 文档类（3 个）

`docs_list`、`docs_read`、`docs_search`

### 3.5 主机指标类（2 个）

`host_metrics_realtime`、`host_metrics_trend`

### 3.6 easyviews 架构分析类（5 个）

| 工具 | 用途 | 必填参数 |
|---|---|---|
| `get_node_list` | 节点列表与标识映射 | 无 |
| `get_module_topology` | 模块拓扑与项目归属 | `node`、`product_name` |
| `get_chain_topology` | 服务调用链路拓扑 | `node`、`projects` |
| `get_virtual_structure` | 物理宿主机与云主机关系 | `node` |
| `get_project_topology` | 项目与云主机部署关系 | `node` |

### 3.7 Thanos 专用（1 个，仅 `thanos` 后端）

`list_stores`

---

## 四、查询类工具

### 4.1 `query` — 瞬时查询

在单个时间点评估 PromQL 表达式。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `query` | string | 是 | PromQL 查询语句 |
| `timestamp` | string | 否 | 评估时间点，默认当前时间 |
| `truncation_limit` | int | 否 | 见 §2.2 |

```json
{
  "query": "sum by (job) (rate(http_requests_total[5m]))",
  "timestamp": "2026-06-23T10:00:00Z",
  "truncation_limit": 200
}
```

典型用途：获取当前值、判断是否存在、验证指标名与标签。

### 4.2 `range_query` — 范围查询

在一段时间区间内按步长评估 PromQL，用于趋势与绘图数据。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `query` | string | 是 | PromQL 查询语句 |
| `start_time` | string | 否 | 默认 `end_time - 5m` |
| `end_time` | string | 否 | 默认当前时间 |
| `step` | string | 否 | 步长（如 `30s`、`5m`、`1h`）。未指定时自动按约 250 个数据点计算 |
| `truncation_limit` | int | 否 | 见 §2.2 |

```json
{
  "query": "node_load1{instance=~\"192.168.7.66:.*\"}",
  "start_time": "2026-06-23T09:00:00Z",
  "end_time": "2026-06-23T10:00:00Z",
  "step": "1m"
}
```

> 步长必须为正持续时间，否则报错。大时间范围请显式放大 `step`，避免返回数据点过多。

### 4.3 `exemplar_query` — 示例查询

查询与指标样本关联的 trace 示例（需后端支持 exemplars）。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `query` | string | 是 | PromQL 查询语句 |
| `start_time` | string | 否 | 默认 `end_time - 5m` |
| `end_time` | string | 否 | 默认当前时间 |
| `truncation_limit` | int | 否 | 见 §2.2 |

### 4.4 `series` — 查找序列

按标签选择器返回匹配的序列集合。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `matches` | string[] | 是 | 序列选择器数组，如 `["up", "{job=\"node\"}"]` |
| `start_time` | string | 否 | 默认 `end_time - 5m` |
| `end_time` | string | 否 | 默认当前时间 |
| `truncation_limit` | int | 否 | 见 §2.2 |

```json
{
  "matches": ["node_cpu_seconds_total"],
  "truncation_limit": 50
}
```

典型用途：发现某指标存在哪些标签组合。

### 4.5 `label_names` — 标签名称

返回指定时间范围内存在的唯一标签名（按字典序）。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `matches` | string[] | 否 | 过滤用的序列选择器 |
| `start_time` | string | 否 | 默认 `end_time - 5m` |
| `end_time` | string | 否 | 默认当前时间 |
| `truncation_limit` | int | 否 | 见 §2.2 |

### 4.6 `label_values` — 标签值

查询指定标签的所有取值。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `label` | string | 是 | 标签名 |
| `matches` | string[] | 否 | 过滤用的序列选择器 |
| `start_time` | string | 否 | 默认 `end_time - 5m` |
| `end_time` | string | 否 | 默认当前时间 |
| `truncation_limit` | int | 否 | 见 §2.2 |

```json
{
  "label": "__name__",
  "matches": ["{job=\"node\"}"],
  "truncation_limit": 100
}
```

> 常用技巧：`label="__name__"` 列出全部指标名。

### 4.7 `metric_metadata` — 指标元数据

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `metric` | string | 否 | 指标名，为空返回全部指标元数据 |
| `limit` | string | 否 | 返回的最大指标数量 |

### 4.8 `targets_metadata` — 目标元数据

返回目标抓取到的指标元数据。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `match_target` | string | 否 | 目标标签选择器，为空匹配所有目标 |
| `metric` | string | 否 | 指标名，为空返回全部 |
| `limit` | string | 否 | 最大目标数量 |

---

## 五、状态与信息类工具

### 5.1 无参数工具速查

以下工具不接受任何参数，直接调用即可：

| 工具 | 返回内容 |
|---|---|
| `alertmanagers` | Alertmanager 发现概览 |
| `flags` | Prometheus 运行时命令行标志 |
| `list_alerts` | 所有活动告警 |
| `tsdb_stats` | TSDB 使用情况与基数统计（`seriesCountByMetricName`、`labelValueCountByLabelName` 等） |
| `build_info` | Prometheus 版本、GOOS/GOARCH 等构建信息 |
| `config` | 当前配置（抓取配置、外部标签等） |
| `runtime_info` | 运行时信息（存储保留、GOGC、GOMAXPROCS 等） |
| `list_rules` | 已加载的告警规则与记录规则 |
| `list_targets` | 目标发现概览与健康状态 |
| `wal_replay_status` | WAL 重放状态 |

> `alertmanagers`、`config`、`wal_replay_status` 在 `thanos` 后端不可用。

---

## 六、管理类工具

### 6.1 `healthy` / `ready`

无参数，分别用于检查 Prometheus 是否健康、是否已就绪处理查询。适合作为服务连通性冒烟测试。

### 6.2 `reload`

无参数。触发 Prometheus 重新加载配置与规则文件，无停机。需 Prometheus 启动时启用 `--web.enable-lifecycle`。

### 6.3 `quit`

无参数。触发 Prometheus 优雅关闭。**危险操作**，仅在用户明确要求时使用。需 `--web.enable-lifecycle`。

---

## 七、TSDB 管理工具（危险）

> **双重前置条件**：MCP 服务端必须启用 `--dangerous.enable-tsdb-admin-tools`；Prometheus 必须启用 `--web.enable-admin-api`。任一未开启，调用即返回错误。
>
> 这 3 个工具仍会出现在 `tools/list` 中，但未启用时**调用会失败**（而非不注册）。调用前必须与用户确认。

### 7.1 `clean_tombstones`

无参数。从磁盘删除已标记删除的数据并清理墓碑，消耗 I/O 与 CPU，建议低峰期执行。

### 7.2 `delete_series`

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `matches` | string[] | 是 | 要删除的序列选择器 |
| `start_time` | string | 是 | **必须显式提供** |
| `end_time` | string | 是 | **必须显式提供** |

```json
{
  "matches": ["{job=\"test\",instance=\"127.0.0.1:9100\"}"],
  "start_time": "2026-06-23T00:00:00Z",
  "end_time": "2026-06-23T01:00:00Z"
}
```

> `start_time` 与 `end_time` 为强制项，用于防止误删整个保留窗口的数据。删除仅写入墓碑，磁盘空间需通过 `clean_tombstones` 或等待压缩回收。

### 7.3 `snapshot`

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `skip_head` | bool | 否 | 跳过 head block 中的数据 |

数据快照写入 TSDB 数据目录下的 `snapshots/<datetime>-<rand>`，返回该目录路径。

---

## 八、文档类工具

官方 Prometheus 文档以嵌入式资源提供，**无需网络**即可检索。启用 `--docs.auto-update` 后每 24 小时从官方仓库刷新，嵌入文档始终作为回退。

> 若构建时未拉取 `prometheus/docs` 子模块，嵌入文档为空，`docs_read` / `docs_search` 将返回空结果；此状态不影响其他工具。

### 8.1 `docs_list`

无参数。列出可用的官方文档文件清单。

### 8.2 `docs_read`

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `file` | string | 是 | 文档文件名（取自 `docs_list` 的路径） |

```json
{ "file": "querying/basics.md" }
```

### 8.3 `docs_search`

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `query` | string | 是 | 搜索关键词 |
| `limit` | int | 否 | 最大结果数量 |

```json
{ "query": "recording rules naming", "limit": 5 }
```

---

## 九、主机指标工具

从 Prometheus 中的 node_exporter 指标（`node_*`）聚合出 12 项主机性能指标。查询前需确保目标主机已部署 node_exporter 并被采集。

### 9.1 节点/实例匹配规则

- `node` 与 `ip` **至少提供一个**；
- 两者都提供且值不同时**报错**；
- 匹配方式为 `instance=~"<值>:.*"`，因此传入 IP 时会匹配该 IP 上任意端口暴露的导出器。

### 9.2 `host_metrics_realtime` — 实时指标

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `node` | string | 二选一 | 节点名/IP |
| `ip` | string | 二选一 | 节点 IP |

```json
{ "ip": "192.168.7.66" }
```

返回 JSON 对象，字段如下（查询失败项为 `null`，并在 `warnings` 数组中给出原因）：

| 字段 | 含义 | 数据来源示例 |
|---|---|---|
| `cpu_cores` | CPU 核数 | `node_cpu_seconds_total` 计数 |
| `cpu_usage_percent` | CPU 使用率（%） | `irate(node_cpu_seconds_total{mode="idle"}[1m])` |
| `memory_total_bytes` | 内存总量（字节） | `node_memory_MemTotal_bytes` |
| `memory_usage_percent` | 内存使用率（%） | `MemAvailable / MemTotal` |
| `disk_total_bytes` | 磁盘容量（字节，排除 tmpfs/devtmpfs） | `node_filesystem_size_bytes` |
| `disk_usage_percent` | 磁盘使用率（%） | `1 - avail/size` |
| `network_connections` | 网络连接数 | `node_netstat_Tcp_CurrEstab` |
| `network_traffic_bytes_per_second` | 网络流量（B/s，收发之和） | `node_network_*_bytes_total` |
| `disk_io_bytes_per_second` | 磁盘 IO（B/s，读写之和） | `node_disk_*_bytes_total` |
| `load_1m` / `load_5m` / `load_15m` | 系统负载 | `node_load1/5/15` |
| `tcp_listen_overflow` | TCP 监听队列溢出 | `node_netstat_Tcp_ListenOverflow` |
| `total_tcp_connections` | 总 TCP 连接数 | `node_sockstat_TCP_inuse` |
| `node` / `ip` | 回显查询目标 | — |
| `timestamp` | 采集时间（RFC3339） | — |
| `warnings` | 失败项错误汇总 | 仅在有错误时出现 |

### 9.3 `host_metrics_trend` — 趋势指标

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `node` | string | 二选一 | 节点名/IP |
| `ip` | string | 二选一 | 节点 IP |
| `start_time` | string | 建议填写 | 默认为 `end_time - 1h` |
| `end_time` | string | 否 | 默认为当前时间 |
| `step` | string | 否 | 步长；未指定时按约 250 个数据点自动计算 |

```json
{
  "ip": "192.168.7.66",
  "start_time": "1h",
  "end_time": "now",
  "step": "5m"
}
```

返回 JSON 对象，包含与实时工具相同的指标名，另为每项附加 `*_latest` 字段（该区间内最后一个采样值），例如：

```json
{
  "cpu_cores": "{...区间序列...}",
  "cpu_cores_latest": 32,
  "cpu_usage_percent": "{...}",
  "cpu_usage_percent_latest": 27.5,
  "memory_total_bytes_latest": 68719476736,
  "warnings": []
}
```

---

## 十、easyviews 架构分析工具

easyviews 平台提供基础设施拓扑与部署关系数据，用于架构优化分析。所有调用经统一网关 `getTykjProvince` 转发，需配置鉴权信息（见 §10.6）。未配置时所有工具返回明确的"未配置"错误。

返回内容为网关响应体中 `data` 字段的**原始数据**，字段解读与规则判定交由 LLM 处理。

### 10.1 `get_node_list` — 节点列表

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `center` | string | 否 | 是否为中心节点，`true`/`false`，默认 `true` |
| `name_prefix` | string | 否 | 按节点名称前缀过滤，如 `云公司`；为空返回全部 |

返回节点名称（`node`）、云平台节点编码（`ctyun_node_code3`）、节点 ID 与省份信息。

```json
{ "center": "true", "name_prefix": "云公司" }
```

> 该工具是其他 easyviews 工具的**入口**：`node` 用于 `get_module_topology` / `get_project_topology`，`ctyun_node_code3` 用于 `get_virtual_structure`。

### 10.2 `get_module_topology` — 模块拓扑

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `node` | string | 是 | 节点，取自 `get_node_list` 的 `node`，如 `云公司->贵州` |
| `product_name` | string | 是 | 产品名称，如 `天翼看家` |
| `center` | string | 否 | 默认 `true` |

返回模块标识、中文名称、对象类型，以及关联的应用项目名称列表（`projectName`）。输出的 `projectName` 是 `get_chain_topology` 的输入。

### 10.3 `get_chain_topology` — 服务调用链路拓扑

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `node` | string | 是 | 节点标识，使用模块拓扑返回的 `deployNode` 值（可能为逗号分隔的多节点或别名） |
| `projects` | string[] | 是 | 目标模块的应用项目名列表，来自 `projectName`，至少一项 |
| `center` | string | 否 | 默认 `true` |
| `callee_range` | int | 否 | 下游调用深度，默认 `1` |
| `caller_range` | int | 否 | 上游调用深度，默认 `1` |
| `filter_node` | bool | 否 | 是否过滤节点，默认 `false` |
| `minute` | int | 否 | 统计时间窗口（分钟），默认 `5` |
| `node_type` | string | 否 | 节点类型过滤，默认为空 |

```json
{
  "node": "hi-haikou-9",
  "projects": ["order-service", "pay-service"],
  "callee_range": 2,
  "minute": 10
}
```

返回调用节点、调用边与统计字段，用于分析服务依赖、集中依赖、长调用链与异常依赖。

### 10.4 `get_virtual_structure` — 物理机与云主机关系

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `node` | string | 是 | 节点标识，取自 `get_node_list` 的 `ctyun_node_code3`，如 `hi-haikou-9` |
| `center` | string | 否 | 默认 `true` |

返回物理宿主机 IP 与其承载的云主机 IP 列表，用于识别**故障域集中部署**。

### 10.5 `get_project_topology` — 项目部署拓扑

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `node` | string | 是 | 节点名称，取自 `get_node_list` 的 `node`，如 `云公司->海口9` |
| `center` | string | 否 | 默认 `true` |

返回项目、项目对应的云主机 IP 列表及云主机上的项目，用于分析多实例是否集中于同一宿主机。

### 10.6 鉴权与配置

| 参数 | 说明 | 默认值 |
|---|---|---|
| `--easyviews.url` | 统一网关地址 | `https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince` |
| `--easyviews.province` | 省份标识（`provinceDetail.province`） | `大模型` |
| `--easyviews.app-id` | 应用 ID | 空（未配置时工具不可用） |
| `--easyviews.app-secret` | 应用密钥 | 空（未配置时工具不可用） |
| `--easyviews.timeout` | API 调用超时 | `30s` |

```bash
prometheus-mcp-server --easyviews.app-id="<appId>" --easyviews.app-secret="<appSecret>"
```

**推荐调用顺序**：

```
get_node_list  →  get_module_topology  →  get_chain_topology
       ↓
  get_project_topology / get_virtual_structure
```

### 10.7 常见错误

| 错误信息 | 原因 | 处理 |
|---|---|---|
| `easyviews 平台未配置...` | 未提供 `app-id` / `app-secret` | 补齐启动参数并重启 |
| `easyviews 返回业务错误，code=..., msg=...` | 鉴权信息错误或网关侧业务失败 | 核对 `appId`/`appSecret`，查看 `msg` |
| `请求 easyviews 网关失败: ...` | 网络不通或超时 | 检查网关地址与网络，必要时调大 `--easyviews.timeout` |
| `easyviews 网关返回非 200 状态码: ...` | 网关异常 | 联系平台侧确认网关状态 |

---

## 十一、Thanos 专用工具

### 11.1 `list_stores`

无参数，**仅在 `--prometheus.backend=thanos` 时可用**。列出所有 Store API 服务器。

---

## 十二、附录

### 12.1 主要启动参数

| 参数 | 说明 | 默认值 |
|---|---|---|
| `--mcp.transport` | 传输方式：`stdio` / `http` | `stdio` |
| `--mcp.tools` | 工具集：`all` / `core` / 工具名列表 | `all` |
| `--mcp.enable-toon-output` | 启用 TOON 编码输出 | `false` |
| `--mcp.enable-client-logging` | 向 MCP 客户端发送日志通知 | `false` |
| `--mcp.keepalive-interval` | 会话保活 ping 间隔 | `30s` |
| `--mcp.session-timeout` | HTTP 会话空闲超时 | `10m` |
| `--prometheus.url` | 后端地址 | `http://127.0.0.1:9090` |
| `--prometheus.backend` | 后端类型：`prometheus` / `thanos` | 空 |
| `--prometheus.timeout` | 后端 API 调用超时 | `1m` |
| `--prometheus.truncation-limit` | 全局截断限制，`0` 禁用 | `0` |
| `--dangerous.enable-tsdb-admin-tools` | 启用 TSDB 管理工具 | `false` |
| `--docs.auto-update` | 每 24 小时自动更新文档 | `false` |
| `--web.listen-address` | HTTP 监听地址 | `:8080` |

### 12.2 推荐工作流

**探索未知指标**：`label_values`（`__name__`）→ `metric_metadata` → `series` → `query`

**排查数据缺失**：`list_targets` → `config` → `metric_metadata` → `range_query`

**分析高基数**：`tsdb_stats` → `series` → `count by(<label>)` → `label_values`

**检查系统健康**：`healthy` → `ready` → `list_alerts` → `tsdb_stats` → `runtime_info`

**主机性能诊断**：`host_metrics_realtime` → `host_metrics_trend`（异常时用 §9.2 的 `warnings` 定位缺失指标）

### 12.3 PromQL 最佳实践提示

- 计数器使用 `rate()`，`irate()` 仅用于绘图表征瞬时尖峰
- 先 `rate` 再聚合，禁止先 `sum` 后 `rate`
- 高基数指标必须先聚合，或使用 `topk()` / 缩小截断限制
- 大时间范围请显式增大 `step`
