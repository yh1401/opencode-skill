# Pinpoint MCP Server 使用文档

将 Pinpoint Web 的 REST API 封装为 MCP（Model Context Protocol）工具，通过 Streamable HTTP 传输，供 AI 客户端查询监控数据。

- **传输方式**：Streamable HTTP，端点 `/mcp`，默认监听 `0.0.0.0:8888`
- **实现语言**：纯 Python 3.10+，**不依赖 JDK**，任何 JDK 版本（含 1.8）均可运行
- **被监控对象**：Pinpoint Web（需 Java 17+，可为另一台机器）

***

## 1. 已封装工具

### 1.1 Pinpoint 监控查询

| 工具名                          | 对应接口                           | 说明                    |
| ---------------------------- | ------------------------------ | --------------------- |
| `list_applications_tool`     | `GET /api/applications`        | 列出全部被监控应用             |
| `get_server_map_tool`        | `GET /api/servermap/serverMap` | 查询应用服务拓扑图（ServerMap）  |
| `drag_heatmap_tool`          | `GET /api/heatmap/drag`        | 按散点图拖拽区域查询事务列表        |
| `get_transaction_trace_tool` | `GET /api/transaction/trace`   | 查询单事务完整调用树（CallStack） |

### 1.2 架构优化分析（对应《架构优化智能体实现方案-V2》）

| 工具名                               | 方案章节      | 数据源                    | 说明                                                                          |
| --------------------------------- | --------- | ---------------------- | --------------------------------------------------------------------------- |
| `list_arch_rules_tool`            | 4.1.4     | 本地                     | 规则清单、当前阈值与规则版本                                                              |
| `get_module_topology_tool`        | 1.1       | EasyViews              | 模块与应用归属（含 `projectName`、`deployNode`）                                       |
| `filter_dep_projects_tool`        | 1.2       | 本地                     | 按 `-dep` 后缀筛选分析范围（selected / excluded）                                      |
| `arch_rule_check_tool`            | 1.2 / 4.2 | Prometheus             | **取数 + 阈值判定**，四态结论；`include_samples` 回传原始采样                                 |
| `arch_metric_query_tool`          | 1.2       | Prometheus             | 按规则名生成 PromQL 并多点采样（不含判定）                                                   |
| `arch_label_probe_tool`           | 1.2 前置    | Prometheus             | **巡检前口径探测**：各指标族是否带 `cmdb_node`，给出 `suggestedMode`；广州4d 等映射节点按 `cluster` 探测 |
| `analyze_node_runtime_tool`       | 1.2       | Prometheus + EasyViews | 一次跑完 8 条规则；缺节点标签时自动改应用名口径并返回 `scopeFallback`                                |
| `analyze_nodes_runtime_tool`      | 1.2       | Prometheus + EasyViews | **多节点巡检**：服务端并发处理节点，每节点只回摘要（计数 + top3 风险），1 次调用覆盖 23 节点                     |
| `arch_task_open_tool`             | 任务        | 本地                     | 注册 `task_id` 节点白名单，子任务 Worker 继承同一作用域                                       |
| `arch_task_status_tool`           | 任务        | 本地                     | 查询任务作用域状态（已用/未用节点、过期时间）                                                     |
| `prometheus_query_tool`           | 1.2       | Prometheus             | 任意 PromQL 即时/范围查询，返回数值（`limit` 默认 50 条并标 `truncated`）                       |
| `build_app_mapping_tool`          | 1.3 前置    | EasyViews + Pinpoint   | projectName ↔ Pinpoint 应用名映射                                                |
| `get_link_topology_tool`          | 1.3       | EasyViews              | 应用链路拓扑（调用边与统计字段）                                                            |
| `analyze_link_topology_tool`      | 1.3       | EasyViews              | 链路图计算（7 类结论：含影响范围、关键依赖）                                                     |
| `list_nodes_tool`                 | 2.2       | EasyViews              | 节点列表（默认"云公司"前缀，含 `ctyun_node_code3`）                                        |
| `get_host_structure_tool`         | 2.3       | EasyViews              | 物理机 ↔ 云主机映射（含反向索引、冲突检测）                                                     |
| `get_project_deploy_tool`         | 2.4       | EasyViews              | 项目 ↔ 云主机部署关系                                                                |
| `analyze_host_concentration_tool` | 2.5       | EasyViews              | 宿主机集中度（实例数/宿主机数/最大集中比例/覆盖率）                                                 |

**判定四态**：`RISK`（已发现风险）/ `OK`（未触发规则）/ `INSUFFICIENT`（数据不足）/ `NOT_APPLICABLE`（不适用）。
取数状态另由 `collectionStatus` 表示：`SUCCESS` / `NO_DATA` / `DATA_INSUFFICIENT` / `FAIL`。

***

## 2. 工具详解

### 2.1 list\_applications\_tool — 列出应用列表

```json
{ "service_name": "DEFAULT", "clear_cache": false }
```

| 参数             | 类型      | 必填 | 默认值       | 说明              |
| -------------- | ------- | -- | --------- | --------------- |
| `service_name` | string  | 否  | `DEFAULT` | 服务名（租户维度）       |
| `clear_cache`  | boolean | 否  | `false`   | 是否清除应用列表缓存后重新查询 |

通常作为第一步使用：确认应用名拼写，并获取各应用的 `serviceTypeCode`（后两个工具未指定服务类型时会自动解析）。

### 2.2 get\_server\_map\_tool — 服务拓扑图

```json
{
  "application_name": "order-service",
  "caller_range": 1,
  "callee_range": 1,
  "bidirectional": true,
  "was_only": false
}
```

| 参数                  | 类型      | 必填    | 默认值     | 说明             |
| ------------------- | ------- | ----- | ------- | -------------- |
| `application_name`  | string  | **是** | —       | 应用名            |
| `from_ts`           | integer | 否     | 最近 5 分钟 | 开始时间（毫秒时间戳）    |
| `to_ts`             | integer | 否     | 当前时间    | 结束时间（毫秒时间戳）    |
| `service_type_code` | integer | 否     | 自动解析    | 服务类型编码，通常省略    |
| `service_type_name` | string  | 否     | —       | 服务类型名，通常省略     |
| `caller_range`      | integer | 否     | `1`     | 上游调用方搜索深度（1-4） |
| `callee_range`      | integer | 否     | `1`     | 下游被调方搜索深度（1-4） |
| `bidirectional`     | boolean | 否     | `true`  | 是否双向搜索         |
| `was_only`          | boolean | 否     | `false` | 是否仅显示 WAS 节点   |

> 未指定 `service_type_code/name` 时自动从 `/api/applications` 解析；若应用名不存在则报错，请先用 `list_applications_tool` 核对应用名。

### 2.3 drag\_heatmap\_tool — 散点拖拽查询事务

按散点图拖拽区域查询事务列表：**x 轴为时间（毫秒时间戳），y 轴为响应时间（毫秒）**。

```json
{
  "application": "order-service",
  "x1": 1720000000000,
  "x2": 1720003600000,
  "y1": 0,
  "y2": 5000,
  "limit": 50
}
```

| 参数                  | 类型      | 必填    | 默认值  | 说明                              |
| ------------------- | ------- | ----- | ---- | ------------------------------- |
| `application`       | string  | **是** | —    | 应用名                             |
| `x1`, `x2`          | integer | **是** | —    | 时间范围 `[x1, x2]`（毫秒时间戳）          |
| `y1`, `y2`          | integer | **是** | —    | 响应时间范围 `[y1, y2]`（毫秒）           |
| `agent_id`          | string  | 否     | —    | 指定 Agent，可选                     |
| `dot_status`        | boolean | 否     | —    | 过滤状态：`true`=成功，`false`=失败，缺省不过滤 |
| `limit`             | integer | 否     | `50` | 最大返回条数                          |
| `service_type_code` | integer | 否     | 自动解析 | 服务类型编码                          |
| `service_type_name` | string  | 否     | —    | 服务类型名                           |

> 该接口有 `@PreAuthorize` 权限校验，调用账号需具备 **inspector** 权限，否则返回 403。

### 2.4 get\_transaction\_trace\_tool — 单事务调用树

```json
{ "trace_id": "agent01^1720000000000^1^123" }
```

| 参数                | 类型      | 必填    | 默认值  | 说明                                       |
| ----------------- | ------- | ----- | ---- | ---------------------------------------- |
| `trace_id`        | string  | **是** | —    | 事务 ID，形如 `agentId^startTime^seq^elapsed` |
| `focus_timestamp` | integer | 否     | `0`  | 聚焦时间点（毫秒）                                |
| `agent_id`        | string  | 否     | —    | 指定 Agent，可选                              |
| `span_id`         | integer | 否     | `-1` | Span ID                                  |
| `link_trace_id`   | string  | 否     | —    | 关联事务 ID，可选                               |
| `link_span_id`    | integer | 否     | `-1` | 关联 Span ID                               |

> `trace_id` 可从 `drag_heatmap_tool` 返回结果中获取，用于进一步下钻单事务的完整调用栈。

***

## 3. 快速开始

### 环境要求

| 依赖           | 版本          | 说明                            |
| ------------ | ----------- | ----------------------------- |
| Python       | 3.10+       | 本服务运行环境                       |
| Pinpoint Web | 可达的 HTTP 服务 | 被调用的 Web，需 Java 17+，可与本机不是同一台 |

### 3.1 Windows

```powershell
cd pinpoint-mcp-server
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt

# 配置环境变量（示例：Basic 认证，默认模式）
$env:WEB_BASE_URL = "http://127.0.0.1:8080"     # Pinpoint Web 地址（必配，指向 Web 而非本服务）
$env:PINPOINT_USER = "admin"
$env:PINPOINT_PASS = "admin"
# 认证：Basic 默认开启（固定凭据，无需配置），Cookie / X-Auth-Session 可选，见第 4、5 节
# $env:PINPOINT_BASIC_AUTH = "YOUR_PINPOINT_BASIC_AUTH"  # 可选：显式覆盖 Basic 凭据
# $env:PINPOINT_COOKIE = "pinpointJwt=xxx; JSESSIONID=xxx"
# $env:PINPOINT_X_AUTH_SESSION = "xxxx"

python server.py
```

### 3.2 Linux

```bash
cd /opt/pinpoint-mcp-server
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

export WEB_BASE_URL="http://<Web服务器IP>:8080"
export PINPOINT_USER="admin"
export PINPOINT_PASS="admin"
python server.py
```

### 3.3 systemd 守护（推荐生产）

创建 `/etc/systemd/system/pinpoint-mcp.service`：

```ini
[Unit]
Description=Pinpoint MCP Server
After=network.target

[Service]
Type=simple
User=mcp
WorkingDirectory=/opt/pinpoint-mcp-server
ExecStart=/opt/pinpoint-mcp-server/.venv/bin/python server.py
Environment=WEB_BASE_URL=http://127.0.0.1:8080
Environment=PINPOINT_USER=admin
Environment=PINPOINT_PASS=admin
# 若 Web 为 https 自签证书，必须加：
# Environment=PINPOINT_SSL_VERIFY=false
Environment=MCP_HOST=0.0.0.0
Environment=MCP_PORT=8888
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo cp pinpoint-mcp.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now pinpoint-mcp

# 查看状态与日志
sudo systemctl status pinpoint-mcp
sudo journalctl -u pinpoint-mcp -f
```

***

## 4. 环境变量

### 4.1 核心配置

| 变量                                | 默认值                     | 说明                                                                                         |
| --------------------------------- | ----------------------- | ------------------------------------------------------------------------------------------ |
| `WEB_BASE_URL`                    | `http://127.0.0.1:8080` | **Pinpoint Web 地址**（必配）。注意是 Web 的地址，不是本 MCP 服务端口；https 自签证书需配合 `PINPOINT_SSL_VERIFY=false` |
| `PINPOINT_USER` / `PINPOINT_PASS` | `pinpoint-web` / 内置默认密码 | formLogin 账号（`POST /login`）                                                                |
| `PINPOINT_SSL_VERIFY`             | `true`                  | `false` 关闭 SSL 证书校验（自签证书的 https Web 必须设 false，否则握手失败）                                      |
| `MCP_HOST` / `MCP_PORT`           | `0.0.0.0` / `8888`      | MCP 服务监听地址与端口                                                                              |
| `MCP_NAME`                        | `pinpoint`              | MCP Server 名称                                                                              |
| `DEFAULT_RANGE_MINUTES`           | `5`                     | `get_server_map_tool` 未指定时间时的默认查询范围（分钟）                                                    |

### 4.2 认证配置

支持多种认证模式，**可组合使用**。认证机制详见第 5 节。

| 变量                              | 默认值                                        | 说明                                                                                                                                              |
| ------------------------------- | ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `PINPOINT_BASIC_AUTH`           | `YOUR_PINPOINT_BASIC_AUTH` | HTTP Basic 认证的 Base64 凭据（`base64(user:pass)`），**默认开启**：默认凭据固定为 `pinpoint-web:YOUR_PINPOINT_PASSWORD` 的 Base64，随所有请求携带 `Authorization: Basic` 头；显式配置可覆盖 |
| `PINPOINT_COOKIE`               | 空                                          | 浏览器登录后的完整 Cookie 串（如 `pinpointJwt=xxx; JSESSIONID=xxx`），用于企业 SSO / 定制网关环境。**配置后跳过 formLogin**                                                   |
| `PINPOINT_X_AUTH_SESSION`       | 空                                          | 自定义网关的会话令牌（如 nginx 返回的 `X-Auth-Session` 响应头值），随所有请求携带                                                                                           |
| `LOGIN_URL`                     | `/login`                                   | 登录端点（Spring Security formLogin / 自定义网关登录页）                                                                                                      |
| `PINPOINT_AUTO_LOGIN`           | `false`                                    | 请求遇 302/401/403 时自动重新登录并重试一次                                                                                                                    |
| `PINPOINT_LOGIN_USERNAME_FIELD` | `username`                                 | 登录表单用户名字段名                                                                                                                                      |
| `PINPOINT_LOGIN_PASSWORD_FIELD` | `password`                                 | 登录表单密码字段名                                                                                                                                       |

### 4.3 架构优化数据源

| 变量                                                                 | 默认值                | 说明                                                                            |
| ------------------------------------------------------------------ | ------------------ | ----------------------------------------------------------------------------- |
| `EASYVIEWS_BASE_URL`                                               | 内置生产地址             | EasyViews 代理入口                                                                |
| `EASYVIEWS_PROVINCE` / `EASYVIEWS_APP_ID` / `EASYVIEWS_APP_SECRET` | 内置默认值              | 代理入口鉴权参数                                                                      |
| `EASYVIEWS_RETRY` / `EASYVIEWS_RETRY_BACKOFF`                      | `2` / `1.0`        | 网络异常或 5xx 重试次数与退避基数（秒），4xx 与业务错误不重试                                           |
| `EASYVIEWS_RETRY_TIMEOUT`                                          | `false`            | 请求超时是否重试；默认不重试，避免 60s×3 的等待叠加                                                   |
| `PROMETHEUS_BASE_URL`                                              | 内置默认值              | Prometheus 地址                                                                 |
| `PROMETHEUS_SSL_VERIFY`                                            | `true`             | `false` 关闭 Prometheus SSL 校验                                                  |
| `PROMETHEUS_TIMEOUT`                                               | `30`               | Prometheus 请求超时（秒）                                                            |
| `PROMETHEUS_RETRY` / `PROMETHEUS_RETRY_BACKOFF`                    | `2` / `1.0`        | 网络异常或 5xx 重试次数与退避基数；4xx 与查询错误不重试                                              |
| `ARCH_ENV`                                                         | `生产`               | 指标 `env` 标签默认值                                                                |
| `ARCH_PRODUCT_NAME`                                                | `天翼看家`             | 模块拓扑查询的产品名                                                                    |
| `ARCH_NODE_CLUSTER_MAP`                                            | `云公司->广州4d=广州4d生产` | 节点级标签口径覆盖，格式 `节点=cluster值;…`；命中节点改用 `cluster` 取数，不再附加 `cmdb_node`             |
| `ARCH_LATENCY_PROMQL`                                              | 空（不启用）             | P99 延迟模板，支持 `$NODE`/`$ENV`/`$DEPLOYMENT`/`$WINDOW`，需返回毫秒                      |
| `ARCH_LATENCY_P99_MS`                                              | `1000`             | P99 延迟阈值（毫秒），用于限流影响判定                                                         |
| `ARCH_POD_OWNER_LOOKUP`                                            | `true`             | 用 `kube_pod_owner` 解析真实 Pod，失败回退名称前缀                                          |
| `ARCH_POD_OWNER_MAX_PODS`                                          | `200`              | 单次解析 Pod 数上限，超过回退前缀匹配                                                         |
| `ARCH_POD_OWNER_CACHE_TTL`                                         | `300`              | Pod 归属解析结果缓存秒数（键为 节点+env+deployment+namespace）；`0` 关闭缓存，指定 `end_ts` 的历史查询不走缓存 |

### 4.4 规则阈值（可配置，配合 `ARCH_RULE_VERSION` 追溯）

| 变量                                                     | 默认值             | 说明                                                                                               |
| ------------------------------------------------------ | --------------- | ------------------------------------------------------------------------------------------------ |
| `ARCH_RULE_VERSION`                                    | `V2.1`          | 规则版本，写入每份分析报告                                                                                    |
| `ARCH_RULE_MIN_REPLICAS`                               | `3`             | 副本冗余要求（方案"副本大于 2"即至少 3，待业务确认）                                                                    |
| `ARCH_RULE_CPU_THROTTLE_RATIO`                         | `0.20`          | CPU 限流周期比例上限（> 20%）                                                                              |
| `ARCH_RULE_MEMORY_RATIO`                               | `0.85`          | 内存工作集 / limit 上限（> 85%）                                                                          |
| `ARCH_RULE_POD_CPU_IMBALANCE`                          | `2.0`           | Pod CPU 最大/平均比值上限（> 2）                                                                           |
| `ARCH_RULE_DURATION_MINUTES`                           | `15`            | 持续型规则观察窗口（分钟）                                                                                    |
| `ARCH_RULE_OOM_RESTART_WINDOW`                         | `30m`           | OOM 重启证据窗口                                                                                       |
| `ARCH_RULE_OOM_AUX_INSTANT`                            | `true`          | OOM 辅助查询（restart / oom\_events 的 `increase([窗口])`）取单点即时值；设为 `false` 回滚为逐点采样（判定只取 `any(>0)`，结论等价） |
| `ARCH_RULE_TIMEOUT`                                    | `45`            | 单次 `arch_rule_check_tool` 复核总时限（秒），超时中止取数并返回 `INSUFFICIENT` + `degradedReason=TIMEOUT`；`0` 不限制   |
| `ARCH_RULE_MIN_COVERAGE`                               | `0.8`           | 持续型规则最低采样覆盖率                                                                                     |
| `ARCH_RULE_IMBALANCE_MIN_AVG_CORES`                    | `0.01`          | 负载不均衡均值下限（低于此值跳过判定）                                                                              |
| `ARCH_DEP_SUFFIX_ONLY` / `ARCH_DEP_SUFFIX`             | `true` / `-dep` | 第一阶段仅检查 `-dep` 结尾应用                                                                              |
| `ARCH_MAPPING_MIN_LEN` / `ARCH_MAPPING_MIN_SCORE`      | `6` / `0.7`     | 应用名映射最短长度与最低置信分                                                                                  |
| `ARCH_LINK_HIGH_INDEGREE` / `ARCH_LINK_HIGH_OUTDEGREE` | `5` / `5`       | 链路集中依赖判定：入度/出度上限（超过即标记）                                                                          |
| `ARCH_LINK_LONG_PATH`                                  | `8`             | 长链路阈值：DAG 最长路径节点数上限（超过即标记）                                                                       |
| `ARCH_LINK_ERROR_RATE` / `ARCH_LINK_SLOW_MS`           | `0.05` / `1000` | 异常边判定：错误率与平均耗时上限                                                                                 |
| `ARCH_LINK_IMPACT_MIN_NODES`                           | `5`             | 影响范围：反向可达上游节点数达到即标记（0 关闭）                                                                        |
| `ARCH_LINK_CRITICAL_PATHS`                             | `3`             | 关键依赖：源→汇路径数达到即标记（0 关闭）                                                                           |
| `ARCH_LINK_MAX_NODES` / `ARCH_LINK_TOP_N`              | `800` / `20`    | 图规模上限与排行榜条数                                                                                      |
| `ARCH_LINK_TIMEOUT`                                    | `45`            | 链路取数总时限（秒），覆盖 `get_link_topology_tool` 与 `analyze_link_topology_tool`；超时中止取数并返回 `DATA_INSUFFICIENT` + `degradedReason=TIMEOUT`；`0` 不限制        |
| `ARCH_LINK_BATCH_SIZE`                                 | `30`            | 链路取数单批应用数上限；`projects` 超过该值自动分批并发取数并按节点/边去重合并；`0` 关闭分批（大清单易触发网关空闲超时）        |
| `ARCH_LINK_BATCH_CONCURRENCY`                          | `4`             | 链路分批取数的并发批数；批数越多总耗时越低，但加大对上游监控平台的压力        |
| `ARCH_HOST_CONCENTRATION_RATIO`                        | `0.5`           | 部分集中判定：单宿主机承载占比阈值                                                                                |
| `ARCH_BATCH_TOP_N`                                     | `10`            | `analyze_node_runtime_tool` 单规则返回明细上限（仅风险/数据不足项）                                                 |
| `ARCH_FINDINGS_MAX`                                    | `30`            | `analyze_node_runtime_tool` 风险清单 `findings` 上限                                                   |
| `ARCH_OUTPUT_DETAIL`                                   | `summary`       | 宿主机集中度默认输出档位；`full` 回全量明细（兼容旧行为）                                                                 |
| `ARCH_PARTIAL_LIMIT`                                   | `5`             | summary 下部分集中清单保留条数                                                                              |
| `ARCH_PROJECT_LIMIT`                                   | `10`            | summary 下 projects 兜底条数                                                                          |
| `ARCH_NODE_CONCURRENCY`                                | `6`             | `analyze_nodes_runtime_tool` 节点并发数（可调 8\~12）                                                     |
| `ARCH_PATROL_TOP_N`                                    | `3`             | 多节点巡检每节点回传风险条数                                                                                   |
| `ARCH_PATROL_RISK_MAX`                                 | `30`            | 多节点巡检全局风险清单上限                                                                                    |
| `ARCH_PATROL_NODE_TIMEOUT`                             | `180`           | 单节点处理超时（秒）                                                                                       |
| `ARCH_TASK_TTL_MINUTES`                                | `30`            | 任务作用域有效期                                                                                         |
| `ARCH_API_TOKEN`                                       | 空               | REST `/api/arch/*` 令牌；为空时端点返回 403                                                                |

补充口径：

- `oom_killed` 使用 `kube_pod_container_status_last_terminated_reason` 与 `container_oom_events_total` 增量两类证据，任一成立即为 OOM 证据，且仍须有重启增长
- `pod_cpu_imbalance` 先按 Pod CPU limit 分组（缺失回退 CPU request），取不到规格的目标记为「数据不足」，不做不同规格混算
- `arch_rule_check_tool`、`analyze_link_topology_tool`、`analyze_host_concentration_tool` 支持 `task_id` 透传，返回 `taskId`、`collectTime`（毫秒）与 `collectTimeIso`
- EasyViews 类工具返回 `dataUpdateTime`（源数据更新时间，无法识别时为 `null`，不做推测）
- `-dep` 后缀筛选由 `ARCH_DEP_SUFFIX_ONLY` / `ARCH_DEP_SUFFIX` 控制，可用 `filter_dep_projects_tool` 或 `analyze_link_topology_tool` 的 `dep_suffix_only` / `dep_suffix` 参数覆盖；返回 `selected` / `excluded` 清单，不做静默丢弃
- 比值类规则分母为零时返回 `NOT_APPLICABLE` 并在 `reason` 注明（如 limit 为 0、CPU 总周期为 0）；指标无数据或数值不可解析返回 `INSUFFICIENT`
- `arch_rule_check_tool` 传 `include_samples=true` 时额外返回 `evidence`（`main` 原始采样、`aux` 辅助序列与失败点；`pod_cpu_imbalance` 另含 `derived` 分组比值），用于报告逐条回溯
- Prometheus 查询对网络异常与 5xx 按 `PROMETHEUS_RETRY` 指数退避重试；重试仍失败时该采样点计入 `errorList`，不静默忽略
- Prometheus 查询**超时不重试**（超时说明查询本身过重，重试会把等待叠加成倍）；`query_range` 超时也不再降级为即时查询，`degradedReason` 注明原因
- `arch_rule_check_tool` 受 `ARCH_RULE_TIMEOUT`（默认 45s）总时限约束，超时返回 `INSUFFICIENT` + `degradedReason=TIMEOUT`，保证在网关空闲超时前一定返回响应
- `analyze_link_topology_tool` 输出 7 类 findings：`HIGH_IN_DEGREE`、`HIGH_OUT_DEGREE`、`CIRCULAR_DEPENDENCY`、`LONG_CALL_CHAIN`、`ABNORMAL_EDGE`、`WIDE_IMPACT_SCOPE`、`CRITICAL_DEPENDENCY`
- `analyze_link_topology_tool` 默认不返回原始拓扑图（`rawData`），需要核对解析口径时传 `include_raw=true`；风险项按类型各保留前 `ARCH_LINK_TOP_N`（默认 20）条，同类总数见 `findingCounts`，避免 121 应用规模下结果膨胀到数百 KB 导致客户端消费时卡住
- 影响范围按反向可达节点数计算（谁调用它）；关键依赖按 DAG 源→汇路径数计算，仅剔除真实环上节点，环的下游节点重新作为源点
- `cpu_throttle_ratio` 附带 `latencyCrossCheck`：配置 `ARCH_LATENCY_PROMQL` 后按 P99 是否超阈值给出 `CONFIRMED_IMPACT` / `NO_LATENCY_IMPACT` 与 `priorityHint`（HIGH / LOWER）；未配置返回 `NOT_CONFIGURED`，不得据此判断业务影响；限流未触发时 `applied=false`
- 指定 deployment 的 Pod 类规则优先用 `kube_pod_owner` 解析真实 Pod，返回 `podSelector.mode`（`owner` / `prefix` / `prefix-fallback`），回退时 `reason` 说明原因，不静默改变统计口径；`podSelector.scope` 记录实际查询口径（广州4d 等 cluster 节点先带 `cluster` 收口，未命中再回退全局并标 `scopedFallback`），命中缓存时带 `cached: true`
- `analyze_host_concentration_tool` 默认 `detail=summary`：保留全部计数，集中/单实例/部分集中只回精简项与 `partialSummary` 分布，`truncated` 标注省略量；`detail=full` 恢复逐宿主机分布与 IP 明细
- `analyze_nodes_runtime_tool` 自动注册 `task_id` 节点白名单，子任务 Worker 用同一 `task_id` 即可继承工具权限；节点越界返回 `SCOPE_DENIED`
- REST 批量入口（供无 MCP 连接的 Worker）：`POST /api/arch/batch`（body: `task_id`/`nodes`/`rules`/`concurrency`/`cursor`）、`GET /api/arch/task/{task_id}`、`GET /api/arch/health`；需 `X-Arch-Token` 与 `ARCH_API_TOKEN` 一致

***

## 5. 认证机制

认证按以下模式（可组合）工作：

| 模式                 | 配置方式                              | 说明                                                                                                                       |
| ------------------ | --------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| **Cookie 认证**      | `PINPOINT_COOKIE`                 | 全局携带 Cookie 头，跳过 formLogin。适用于企业 SSO / nginx 校验会话 Cookie 的环境                                                             |
| **Basic 认证**       | `PINPOINT_BASIC_AUTH`             | **默认开启**：默认凭据固定为 `pinpoint-web:YOUR_PINPOINT_PASSWORD` 的 Base64，全局携带 `Authorization: Basic xxx` 头。适用于通过 nginx/网关的 Basic Auth 校验 |
| **X-Auth-Session** | `PINPOINT_X_AUTH_SESSION`         | 全局携带 `X-Auth-Session` 头。适用于自定义网关下发会话令牌的环境                                                                                |
| **自动登录**           | `PINPOINT_AUTO_LOGIN=true`        | 请求遇 302/401/403 时，自动访问 `LOGIN_URL` 解析表单并提交账号密码续期会话；`X-Auth-Session` 自动从响应头同步                                             |
| **formLogin**      | `PINPOINT_USER` / `PINPOINT_PASS` | 补充认证：启动预登录 + 失效重试，`POST /login` 获取应用层 Cookie；失败时自动降级为仅 Basic                                                             |

常见组合：

- **企业 SSO / 定制网关（推荐）**：`PINPOINT_COOKIE` + `PINPOINT_X_AUTH_SESSION`（见项目实际部署配置）
- **网关 Basic + 应用层 Cookie**：`PINPOINT_BASIC_AUTH` + formLogin（`PINPOINT_USER`/`PINPOINT_PASS`），两者缺一不可
- **纯内网开发环境**：仅配置 `PINPOINT_USER`/`PINPOINT_PASS` 即可，默认即为 Basic 认证

> 注意：开启 `PINPOINT_AUTO_LOGIN` 时建议不要同时配置静态 `PINPOINT_COOKIE`，否则静态 Cookie 头会覆盖自动登录更新的会话。登录页含验证码/动态 token 时无法自动登录，需退回手动配置 `PINPOINT_COOKIE`。

**会话失效重试**：请求返回 302/401/403 时，自动重新认证（formLogin 或自动登录）后重试一次；重试仍失败则抛出明确异常，便于定位。

***

## 6. 启动与验证

启动时会对 Web 预登录一次（仅告警不阻塞）；工具调用时仍会按需登录重试。

```bash
# 启动（前台）
python server.py

# 验证服务可访问（HTTP 端点探测，返回 404/非 JSON 属正常，说明服务已监听）
curl http://127.0.0.1:8888/mcp
```

### 网关路径前缀

服务除标准 `/mcp` 路径外，还兼容网关前缀，以下路径均等价于 `/mcp`：

- `/mcp`
- `/gateway/pinpoint`
- `/gateway/pinpoint/`

其他路径一律返回 `404 {"error": "not found"}`。

### 连通性测试脚本

仓库内置 [mcp\_test\_client.py](mcp_test_client.py)：完成握手 → 列出工具 → 调用 `list_applications_tool` 验证数据链路。

```bash
# 在 venv 中执行（Windows）
.venv\Scripts\python mcp_test_client.py
.venv\Scripts\python mcp_test_client.py http://127.0.0.1:8888/mcp   # 自定义地址
```

### 接入 AI 客户端

任意支持 **MCP Streamable HTTP** 的客户端，配置服务地址为：

```
http://<MCP服务器IP>:8888/mcp
```

***

## 7. 故障排查

| 现象                           | 可能原因                     | 处理                                        |
| ---------------------------- | ------------------------ | ----------------------------------------- |
| 日志 `connect error` / SSL 错误  | Web 为 https 自签证书         | 设置 `PINPOINT_SSL_VERIFY=false`            |
| 请求返回 302/401/403 后重试仍失败      | 账号密码错误 / Cookie 过期 / 缺权限 | 检查认证配置；`drag_heatmap_tool` 需 inspector 权限 |
| 非 JSON 响应错误（body 为 HTML）     | 网关返回登录页/拦截页              | 检查 Cookie / X-Auth-Session 是否仍有效，重新获取     |
| 自动登录报错「登录页含验证码」              | 登录页有验证码/动态 token         | 改用手动 `PINPOINT_COOKIE`                    |
| `get_server_map_tool` 报未找到应用 | 应用名拼写错误                  | 先用 `list_applications_tool` 核对            |
| 404 not found（访问 `/mcp` 时）   | 请求路径不被网关重写               | 确认使用 `/mcp` 或网关前缀路径                       |
| 启动日志 `pre-login failed`      | 启动时 Web 不可达              | 不影响启动，首次工具调用会重试；持续失败则检查 `WEB_BASE_URL`    |

***

## 8. 安全注意事项

- 认证凭据（账号密码、Cookie、Basic Base64）属于敏感信息，生产环境应通过 systemd `EnvironmentFile` 或密钥管理注入，**勿写入代码/提交到仓库**。
- 服务默认监听 `0.0.0.0:8888`，如仅内网使用，建议设置 `MCP_HOST=127.0.0.1` 或由网关统一暴露 `/gateway/pinpoint` 前缀。

