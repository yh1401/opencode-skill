# 架构优化维度判定规则（arch-opt-mcp-server 主源 + pinpoint 补充）

> 双源规则文件：
> - **主源 `arch-opt-mcp-server`**：`mcp_replica` / `mcp_capacity` / `mcp_load_balance` / `mcp_oom` 覆盖 D1–D4，`mcp_module_topology` 覆盖范围派生，`mcp_node_list` → `mcp_virtual_structure` → `mcp_topology_info` → `mcp_fault_domain` 覆盖 D6。
> - **补充源 `pinpoint-mcp-server`**：仅 `get_server_maps_batch_tool` 等提供 D5 应用级 ServerMap。
> - 本文件同时保留 D1–D4 的底层 PromQL 口径，用于服务不可达时的人工核对与阈值溯源（**不作为技能默认调用路径**）。
>
> - 阈值来源：`arch-opt-mcp-server/config/thresholds.json` v2.2.0（两处保持一致）。
> - 判定逻辑来源：`arch-opt-mcp-server/scripts/mcp_server.py` 中 `tool_replica` / `tool_capacity` / `tool_load_balance` / `tool_oom` / `tool_fault_domain`；D5 为本文件 §6A。
> - **大模型不得自行改判**；命中/不命中一律以工具返回与本文件规则为准，如需调整口径，改本文件与 `thresholds.json`。

---

## §0 前置条件

### 0.1 服务与连接

| 项 | 值 |
|----|----|
| 主源 MCP Server | `arch-opt-mcp-server`（Python FastAPI，`GET /sse` + `POST /messages`，兼容 `POST /mcp`） |
| 主源启动 | `MCP_PORT=8062 python scripts/mcp_server.py`（凭据 `ARCH_APP_ID` / `ARCH_APP_SECRET` / `PROMETHEUS_URL` 环境变量注入） |
| 补充源 MCP Server | `pinpoint-mcp-server`（Python，Streamable HTTP `/mcp`，默认 `8888`） |
| Prometheus 实例 | `http://183.56.210.90:8082`（供 arch-opt-mcp-server 内部调用，技能侧一般不直连） |
| 默认端口 | `8062`（arch-opt）、`8888`（pinpoint）；（cmdb-mcp-server 8061；独立 prometheus-mcp-server 8063 仅调试用） |

### 0.2 用到的工具

| 工具 | 入参要点 | 用途 |
|------|----------|------|
| `mcp_module_topology` | `node`、`product_name`、`only_dep` | 范围派生：模块归属与 `-dep` 应用列表 |
| `mcp_replica` | `node`、可选 `deployments` | D1 主查询 |
| `mcp_capacity` | `node`、可选 `deployments` | D2 主查询 |
| `mcp_load_balance` | `node`、可选 `deployments` | D3 主查询 |
| `mcp_oom` | `node`、可选 `deployments`、`days` | D4 主查询 |
| `mcp_node_list` | `filter_prefix` | D6 第一步：节点 `ctyun_node_code3` |
| `mcp_virtual_structure` | `ctyun_node_code3` | D6 第二步：宿主机 ↔ 云主机 |
| `mcp_topology_info` | `node` | D6 第三步：项目 ↔ 云主机 |
| `mcp_fault_domain` | `node`、可选 `min_instances` | D6 汇总判定 |
| `query` / `range_query` / `series` / `label_values` / `metric_metadata` | 见 prometheus-mcp-server | **仅调试/核对**：底层 PromQL 复核，不参与默认编排 |

> 独立 `prometheus-mcp-server` 额外提供 `host_metrics_realtime` / `host_metrics_trend`，与本技能无关，不启用。

### 0.3 连通性 / 标签口径校验（**必须先跑**）

直查依赖 `cmdb_node`（节点级）与 `deployment`（应用级）两个标签，但**这两个标签在不同指标族上并不一致**（见 0.4 实测），因此先探测再取数，等价于 MCP 工具 `arch_label_probe_tool(node=…)`：

```promql
# 1) deployment 级指标：该节点下是否有业务 -dep 序列
count(kube_deployment_spec_replicas{cmdb_node="<NODE>",deployment=~".+-dep"})

# 2) container 级指标：该节点下是否有容器序列
count(count by (namespace,pod)(container_memory_working_set_bytes{cmdb_node="<NODE>"}))

# 3) 应用名口径：不依赖节点标签，指定应用是否有序列
count(kube_deployment_spec_replicas{deployment="<DEP>"})
```

判定：

- 1) > 0 → 部署级规则可按 `cmdb_node` 查询
- 1) = 0 且 3) > 0 → 部署级必须改用应用名口径（见 0.5），并在报告中标注口径差异；**不得判为「无风险」**
- 1) 与 2) 均为 0 → 节点标签整体缺失或采集未接入，先查指标采集链路，相关维度统一降级为 🟨

### 0.4 实测口径（2026-10-19 / 2026-10-30 两次校验，PromQL 以此为准）

| 项 | 实测结论 | 影响 |
|----|----------|------|
| `cmdb_node` | ⚠️ **分指标族存在差异**：`container_*`（cAdvisor）有；`kube_deployment_*`（KSM）在业务应用上**没有** | 部署级查询（D1/D2-HPA）不能默认用节点标签，须先探测 |
| KSM 节点标签覆盖（2026-10-30） | 某节点仅 16 个系统组件 Deployment（calico/coredns/harbor/monitoring）带 `cmdb_node`；121 个业务 `-dep` **不带**；业务部署分布在 2 个 K8s 集群 | 出现「8 条规则集体数据不足」时，是标签口径断点，不是应用无数据 |
| `deployment` | ✅ 存在于 `kube_*` 指标；❌ **不存在于 `container_*` 指标** | D2/D3 必须用 `pod=~"<DEP>-.*"` + `label_replace` 派生 deployment |
| 同节点重名 deployment | ⚠️ 同一 `cmdb_node` 下同名 deployment 可存在于多个 namespace（如 `ai-platform-dev` / `ai-platform-test`） | 唯一键为 `(namespace, deployment)`，聚合时必须带 `namespace` |
| `container` 标签 | ⚠️ 同时存在 `container=""`（Pod 级 cgroup）与 `container="POD"`（pause）序列 | 容器级查询必须加 `container!=""`、`container!="POD"` |
| HPA 指标 | ❌ 无任何序列（`kube_horizontalpodautoscaler_*` 为空） | D2 HPA 统一 ⚪ `not_applicable`，扩容能力改用「副本 1h 无变化 + CPU/内存持续偏高」替代判定 |
| `-dep` 应用规模 | 贵州约 1180 个 deployment 名；按 `(namespace,deployment)` 约 1500+ | 全量巡检须分批/聚合查询，避免报告爆炸 |
| `-dep` 节点分布 | 默认中心节点 `广州4c` 有 570 条 `-dep` 序列；广州4d 仅 16 个系统组件（harbor/calico/monitoring），业务 `-dep` 分布在其他广州集群 | 对 0 个 `-dep` 的节点，D1–D4 输出 🟨「该节点无 -dep 业务应用」，**不得判为 🟩** |
| 单次序列数上限 | 无选择器的 `container_cpu_cfs_*` / `kube_pod_container_status_last_terminated_reason` 会触发 30000 序列上限 | 所有查询必须带 `cmdb_node` 或 `pod=~...` 收窄 |

> 节点有效性校验（每次巡检先跑，结果写入报告「巡检范围」）：
>
> ```promql
> count(count by (namespace,deployment)(
>   kube_deployment_spec_replicas{cmdb_node="<NODE>",deployment=~".+-dep"}
> ))
> ```
>
> 返回 0 时**不能直接收工**：再执行
>
> ```promql
> count(kube_deployment_spec_replicas{deployment=~".+-dep",namespace!~"<EXCLUDE>"})
> ```
>
> 后者 > 0 说明数据在、只是缺节点标签 → 走 0.5 的应用名口径；两者都为 0 → 该节点确无 `-dep` 业务应用，D1–D4 全部 🟨。

**节点标签口径映射**（服务端 `ARCH_NODE_CLUSTER_MAP`）：广州4d 的业务指标不带 `cmdb_node`，固定改用 `cluster="广州4d生产"`。命中映射的节点，规则查询与 `arch_label_probe_tool` 探测都会自动替换为 `cluster`，**不要再用 `cmdb_node="云公司->广州4d"`**（该条件在本节点恒为 0）。

### 0.5 兜底口径：应用名直查（`cmdb_node` 缺失时）

应用清单取自 EasyViews 模块拓扑（`projectName` / `-dep` 名称），也可由 `get_project_deploy_tool` 返回：

```promql
# D1 副本不足（不依赖节点标签）
kube_deployment_spec_replicas{deployment=~"^(<APP1>|<APP2>)$",namespace!~"<EXCLUDE>"} < 2

# D2/D3/D4 容器级：pod 前缀即应用名（deployment 由 pod 名派生）
container_memory_working_set_bytes{pod=~"^(<APP1>|<APP2>)-.*",container!="",container!="POD"}
```

编排时不要手工拼这些 PromQL，直接：

```text
analyze_node_runtime_tool(node="<NODE>", deployments=["<APP1>", "<APP2>", …])
```

工具会先按 `cmdb_node/env` 查询，无数据时自动切换到应用名口径并返回 `scopeFallback`；应用清单超过 100 个时按 30~50 个分批传入，避免选择器过长与单次序列超限。

### 0.6 PromQL 字符串转义（手写查询必读）

PromQL 的双引号字符串沿用 Go 转义规则，合法转义只有 `\" \\ \n \t \r \a \b \f \v \xNN \uNNNN \UNNNNNNNN`。

- **禁止用 `re.escape()` 把值拼进 PromQL 字符串**：Python 3.7+ 会把 `-` 转义成 `\-`，生成 `deployment=~".+\-dep"` → 后端返回 400 `invalid escape sequence`。
- 正则里的 `.`、`+`、`*`、`(` 等**本身就是正则语法，不需要也不应该转义**；字符串层只处理 `"` 和 `\`。
- 正确写法：`deployment=~".+-dep"`、`pod=~"^(app1|app2)-.*"`、`namespace!~"kube-system|monitoring"`。
- 若应用名/环境值本身含 `"` 或 `\`，按 `value.replace("\\","\\\\").replace('"','\\"')` 处理后再拼接。

工具侧已内置自检：`prometheus_query_tool` 与批量规则入口会先扫描双引号字符串内的非法转义，命中直接返回 `status=FAIL` / `INVALID_PROMQL` 并给出问题片段，**不会发请求**；4xx 响应也不再触发降级重发（旧口径下会把 400 掩盖成「数据不足」）。看到 `errorKinds` 含 `INVALID_PROMQL` 或 `HTTP_4XX` 时，改查询本身，不要换节点或换参数重试。

---

## §1 适用范围

| 维度 | 支持 | 说明 |
|------|------|------|
| D1 副本数量与可用性 | ✅ | `mcp_replica`（底层 PromQL 见 §3） |
| D2 容量瓶颈 | ✅ | `mcp_capacity`（底层 PromQL 见 §4） |
| D3 负载不均衡 | ✅ | `mcp_load_balance`（底层 PromQL 见 §5） |
| D4 OOM | ✅ | `mcp_oom`（底层 PromQL 见 §6） |
| D5 链路依赖 | ✅ | pinpoint-mcp-server 的 ServerMap（见 §6A） |
| D6 省节点故障域 | ✅ | `mcp_node_list` → `mcp_virtual_structure` → `mcp_topology_info` → `mcp_fault_domain`（见 §6B） |

**仅对 `-dep` 结尾的应用执行容器检查**（`container_check.dep_suffix_filter=true`）；非 `-dep` 应用记 ⚪ `not_applicable`。

---

## §2 确定分析范围

**默认路径**：调用 `mcp_module_topology(node, product_name, only_dep=true)`，直接得到模块归属、`deploy_node` 与 `-dep` 应用列表。
**兜底路径**：服务不可用或需更细粒度核对时，才用 Prometheus 反查（本节 2.1–2.2）。

### 2.1 反查 deployment 列表

```promql
# 方式 A：直接反查序列，从返回的标签里提取 deployment 名
series(matches=["kube_deployment_spec_replicas{cmdb_node=\"云公司->贵州\"}"])

# 方式 B：按 selector 取 deployment 标签值
label_values(label="deployment", matches=["kube_deployment_spec_replicas{cmdb_node=\"云公司->贵州\"}"])
```

- 从结果标签集中提取 `(namespace, deployment)`，**过滤 `deployment.endswith('-dep')`**，得到待检查应用列表。
- **唯一键是 `(namespace, deployment)`**：同一节点下同名 deployment 可能同时存在于测试/灰度/生产 namespace，不能只按 deployment 名去重或合并。
- 若用户已显式给定 `deployments`，跳过本步。

### 2.2 范围规模校验

```promql
# 待检查应用数（唯一键口径）
count(count by (namespace,deployment)(
  kube_deployment_spec_replicas{cmdb_node="<NODE>",deployment=~".+-dep"}
))
```

- 返回 0 → 该节点无 `-dep` 业务应用，D1–D4 全部 🟨（见 §0.4）。
- 返回较大（如 >500）→ `arch-opt-mcp-server` 已用**全节点一次聚合**查询，无需技能侧逐应用循环；报告只展开 🟥 应用。

### 2.3 局限（须在报告「数据限制」中说明）

- ✅ 产品维度 / 模块归属：由 `mcp_module_topology` 提供；该接口不可用时才退化为无产品维度。
- ⚠️ Prometheus 中的 deployment 名可能与拓扑接口口径不完全一致（依赖标注规范）。
- ⚠️ 拓扑接口与 Prometheus 的应用集不一致时，以 Prometheus `-dep` 序列为准执行 D1–D4，并披露差异。

---

## §3 D1 副本数量与可用性

### PromQL

```promql
# 单应用
kube_deployment_spec_replicas{cmdb_node="<NODE>",namespace="<NS>",deployment="<DEP>"}
kube_deployment_status_replicas_available{cmdb_node="<NODE>",namespace="<NS>",deployment="<DEP>"}

# 全量风险项（一次取回，效率优先）
kube_deployment_spec_replicas{cmdb_node="<NODE>",deployment=~".+-dep"} < 2

kube_deployment_status_replicas_available{cmdb_node="<NODE>",deployment=~".+-dep"}
  < on(cmdb_node,namespace,deployment,env,cluster)
kube_deployment_spec_replicas{cmdb_node="<NODE>",deployment=~".+-dep"}
```

> 返回为 vector，按返回标签中的 `(namespace, deployment)` 分组；**不要只按 deployment 分组**。
> 名称含 `gray` / `canary` / `shadow` 的灰度副本默认剔除（`ARCH_GRAY_PATTERNS`），不计入冗余与可用性判定。

### 判定（逐 deployment）

| 条件 | 四态 | conclusion |
|------|------|-----------|
| 非 `-dep` 结尾 | ⚪ | 非 -dep 应用，本阶段未覆盖 |
| 未查到 `spec_replicas` 记录 | 🟨 | 未查询到期望副本指标 |
| `spec = 0` | ⚪ | 已下线/暂停（副本归零），记不适用并单列，不计入风险 |
| `spec < min_replicas`(2) | 🟥 | 副本不足：期望 spec < 冗余要求 2（单副本） |
| `check_available_equal=true` 且 `available < spec` | 🟥 | 副本不可用：可用 avail < 期望 spec |
| 以上均不命中 | 🟩 | 副本数量与可用性正常 |

- 汇总态：只要有一个 🟥 → 整体 🟥，否则 🟩。

### 证据字段

`deployment`、`spec_replicas`、`available_replicas`、`evidence={spec, available, min_replicas}`。

---

## §4 D2 容量瓶颈

窗口 `window = "<duration_minutes>m"`，默认 `15m`。

### 4.0 应用级聚合前置说明

`container_*` 指标**没有 `deployment` 标签**，只有 `pod` / `namespace` / `container`。因此 D2/D3 必须：

1. 用 `pod=~"<DEP>-.*"` 圈定该应用的 Pod；
2. 用 `label_replace` 从 Pod 名派生 `deployment`：
   `label_replace(<metric>, "deployment", "$1", "pod", "(.+)-[a-z0-9]+-[a-z0-9]+")`
3. 排除 Pod 级 cgroup 与 pause 容器：`container!=""`、`container!="POD"`；
4. 聚合键统一为 `(namespace, deployment)`。

> ⚠️ 该正则匹配 ReplicaSet 生成的 Pod 名（`<deployment>-<rs-hash>-<pod-suffix>`）。若应用使用 StatefulSet/自定义命名，Pod 不会被派生，须在报告中记 🟨。

### 4.1 CPU 限流（限流周期比例）

```promql
sum by (namespace,deployment)(
  label_replace(
    rate(container_cpu_cfs_throttled_seconds_total{cmdb_node="<NODE>",pod=~"<DEP>-.*",container!=""}[15m]),
    "deployment","$1","pod","(.+)-[a-z0-9]+-[a-z0-9]+"
  )
)
/
sum by (namespace,deployment)(
  label_replace(
    rate(container_cpu_cfs_periods_total{cmdb_node="<NODE>",pod=~"<DEP>-.*",container!=""}[15m]),
    "deployment","$1","pod","(.+)-[a-z0-9]+-[a-z0-9]+"
  )
)
```

- 全量查询可用 `pod=~".+-dep-.*"` 一次取回，再按应用筛 `ratio > 0.20`。
- `ratio > cpu_throttle_ratio`(0.20) → 🟥；否则 🟩。
- 无序列 → 🟨「未查询到 CPU 限流指标（需时间窗口证据）」。

### 4.2 内存容量（工作集 / limit）

```promql
sum by (namespace,deployment)(
  label_replace(
    container_memory_working_set_bytes{cmdb_node="<NODE>",pod=~"<DEP>-.*",container!="",container!="POD"},
    "deployment","$1","pod","(.+)-[a-z0-9]+-[a-z0-9]+"
  )
)
/
sum by (namespace,deployment)(
  label_replace(
    kube_pod_container_resource_limits_memory_bytes{cmdb_node="<NODE>",pod=~"<DEP>-.*"},
    "deployment","$1","pod","(.+)-[a-z0-9]+-[a-z0-9]+"
  )
)
```

- `ratio > memory_usage_ratio`(0.85) → 🟥；`0 < ratio <= 0.85` → 🟩。
- **`ratio == 0` → 🟨**「内存 limit 可能缺失或为 0，口径待确认」。
- 无序列 → 🟨「未查询到内存容量指标或 limit 缺失」。
- ⚠️ 分子含未设 limit 的容器、分母不含，比例可能偏高；报告中注明该口径。

### 4.3 HPA 扩容

```promql
kube_horizontalpodautoscaler_status_current_replicas{namespace="<NS>"}
== on(namespace,horizontalpodautoscaler)
kube_horizontalpodautoscaler_spec_max_replicas{namespace="<NS>"}
```

- 有结果 → 🟥「HPA 已达最大副本 <cur>，需结合负载判断是否持续偏高」。
- **无结果 → ⚪**「无 HPA 或未达到最大副本」。
- 当前环境实测无任何 HPA 序列（见 §0.4），统一 ⚪；HPA→deployment 映射需 `scaleTargetRef`，暂不展开。

### 汇总

整体态 = 任一项 🟥 → 🟥，否则 🟩（🟨 / ⚪ 不计入风险计数，但须如实列出）。

---

## §5 D3 负载不均衡

### PromQL

```promql
# 单应用：先按 Pod 聚合，再取 max/avg
max by (namespace,deployment)(
  sum by (namespace,deployment,pod)(
    label_replace(
      rate(container_cpu_usage_seconds_total{cmdb_node="<NODE>",pod=~"<DEP>-.*",container!=""}[15m]),
      "deployment","$1","pod","(.+)-[a-z0-9]+-[a-z0-9]+"
    )
  )
)
/
avg by (namespace,deployment)(
  sum by (namespace,deployment,pod)(
    label_replace(
      rate(container_cpu_usage_seconds_total{cmdb_node="<NODE>",pod=~"<DEP>-.*",container!=""}[15m]),
      "deployment","$1","pod","(.+)-[a-z0-9]+-[a-z0-9]+"
    )
  )
)
```

- 全量查询用 `pod=~".+-dep-.*"` 一次取回，再按应用筛 `ratio > 2.0`。
- Pod 名派生规则与限制同 §4.0。

### 判定

- `ratio > max_avg_ratio`(2.0) → 🟥「最大/平均负载 <ratio> > 2.0（持续 15min）」。
- 否则 🟩。
- 无序列 → 🟨「未查询到 Pod 负载指标（需时间窗口证据）」。
- ⚠️ 单副本应用 `max == avg`，ratio 恒为 1，应记 ⚪「仅 1 个 Pod，负载不均衡不适用」，不参与 🟩 统计。

---

## §6 D4 OOM

近期窗口 `lookback_days`，默认 **7 天**。

### PromQL

```promql
# 即时：该 gauge 持久保存「最近一次终止原因」，即时值即覆盖历史 OOM
kube_pod_container_status_last_terminated_reason{
  cmdb_node="<NODE>",pod=~"<DEP>-.*",reason="OOMKilled"
} == 1

# OOM 事件计数（cAdvisor，7d 增量）：补证「OOM 后又因其他原因重启」的情况
increase(container_oom_events_total{cmdb_node="<NODE>",pod=~"<DEP>-.*"}[7d])

# 重启次数（辅助证据）
increase(kube_pod_container_status_restarts_total{cmdb_node="<NODE>",pod=~"<DEP>-.*"}[7d])
```

- **必须带 `== 1`**：裸 selector 会把 `value=0` 的序列也返回，导致误判 🟥。
- **不要用 `max_over_time(...[7d:15m])` 子查询**：7d×15m 每个采样点要评估 672 步，实测是巡检耗时主因（单节点 20 分钟级）；改用即时值 + `increase` 增量补证。
- `kube_pod_container_status_last_terminated_reason` 只记录**最近一次**终止原因；若 Pod 之后因其他原因终止，历史 OOM 会丢失，由 `container_oom_events_total` 的 7d 增量兜住，报告中注明该局限。

### 判定

| 条件 | 四态 | conclusion |
|------|------|-----------|
| OOM 序列非空（值=1）或 7d OOM 事件计数增长，且重启同时增长 | 🟥 | 近 7 天出现 OOMKilled [，累计重启 N 次] |
| OOM 空、restarts 非空 | 🟩 | 未发现近期 OOM |
| 两条均为空 | 🟨 | 未查询到 OOM/重启指标，无法判定近期 OOM |

---

## §6A D5 链路依赖（pinpoint-mcp-server）

> 与 D1–D4 不同，D5 不查 Prometheus，改调 pinpoint-mcp-server 的 ServerMap。
> **本段阈值均为 provisional（建议值），待与业务/真实数据联调后确认。**

### 6A.1 采集步骤

1. `list_applications_tool(service_name="DEFAULT")` → 取 Pinpoint 应用清单。
2. **应用名对齐**：Prometheus 的 deployment 名去掉 `-dep` 后缀后，与 `applicationName` 匹配：
   - 优先精确匹配；其次忽略大小写、`_`/`-` 归一化后匹配。
   - 无法匹配的应用，D5 记 🟨（不得判 🟩）。
3. **风险优先采样**（避免上千次调用）：
   - 取 D1–D4 判为 🟥 的应用；
   - 补齐各节点入度最高的核心应用（每节点 Top 20）；
   - 合并去重后作为 D5 采样集，报告中记录 `sampled/total` 覆盖率。
4. `get_server_maps_batch_tool(application_names=[…], caller_range=1, callee_range=1, bidirectional=true, concurrency=3)`。
   - 时间范围默认最近 5 分钟；需更长窗口时显式传 `from_ts` / `to_ts`（毫秒）。ServerMap 聚合窗口过长会降精度，建议 ≤ 1 小时。
   - 单应用 `ok=false` 只影响该应用，记 🟨 并保留 `error`。
5. 需要定位具体异常调用时，`drag_heatmap_tool` 取 `traceId` → `get_transaction_trace_tool` 下钻；权限不足（403）时记 🟨。

### 6A.2 返回结构与指标计算

返回 `applicationMapData.{nodeDataArray, linkDataArray}`：

- `nodeDataArray[]`：`key`、`applicationName`、`serviceType`、`nodeCategory`（SERVER / DATABASE / MESSAGE_BROKER / CACHE / USER / UNKNOWN）、`totalCount`、`errorCount`、`slowCount`、`responseStatistics{Tot,Sum,Avg,Max}`、`instanceCount`。
- `linkDataArray[]`：`from`、`to`（**均为 node 的 `key`，不是应用名**）、`totalCount`、`errorCount`、`slowCount`、`responseStatistics`。

技能侧计算（**大模型不得自行计算**）：

| 指标 | 计算方式 |
|------|----------|
| `in_degree(app)` | `linkDataArray` 中 `to == node.key` 的条数（去重上游） |
| `out_degree(app)` | `linkDataArray` 中 `from == node.key` 的条数（去重下游） |
| `error_rate` | `errorCount / totalCount`（`totalCount=0` 时不可算） |
| `slow_rate` | `slowCount / totalCount` |
| `avg_ms` | `responseStatistics.Avg`（缺失时 `Tot / totalCount`） |
| `cycle` | 存在 A→B 且 B→A 的有向边对 |
| `chain_depth` | 从该节点沿 `from→to` BFS 的最大层数（受 `callee_range` 限制，最大 4） |

> 先建 `key → applicationName` 映射，再输出结论；否则会把内部 key 当应用名写进报告。

### 6A.3 判定规则（阈值 provisional）

| 规则 | 条件 | 四态 | conclusion |
|------|------|------|-----------|
| 集中依赖 | `in_degree ≥ min_in_degree(2)` | 🟥 | 被 N 个上游依赖，单点故障影响面大 |
| 下游过宽 | `out_degree > max_out_degree(10)` | 🟥 | 依赖 N 个下游，任一异常易拖垮自身 |
| 错误率偏高 | `totalCount ≥ min_sample_count(20)` 且 `error_rate > 0.05` | 🟥 | 调用错误率 X%（错误 N/M） |
| 慢调用偏高 | `totalCount ≥ min_sample_count(20)` 且 `slow_rate > 0.10` | 🟥 | 慢调用占比 X%（慢 N/M） |
| 循环依赖 | 存在 A→B 且 B→A | 🟥 | 与 X 互为上下游，存在循环调用 |
| 长链路 | `chain_depth > max_chain_depth(4)` | 🟨 | 受 4 跳上限限制，实际链路可能更长，无法确认 |
| 样本不足 | `totalCount < min_sample_count(20)` | 🟨 | 调用样本过少，比例类规则不可判 |
| 数据缺失 | 应用未在 Pinpoint / `nodeDataArray` 为空 / 全部 `linkDataArray` 为空 | 🟨 | 未获取到该应用的拓扑数据 |
| 未命中 | 以上均不满足 | 🟩 | 未触发 D5 规则（采样口径） |

- 同一应用命中多条规则时**合并为一条问题卡**，证据里列出全部命中项。
- `slowCount` 的「慢」由 Pinpoint 服务端阈值决定（常见 3s，未确认），报告中须注明该口径未知。
- `nodeCategory` 只作证据（识别 DB / CACHE / MQ 依赖），不单独判风险。

### 6A.4 口径限制（必须写入报告「数据限制」）

- ❌ 无 `cmdb_node` / namespace / 产品 / 模块维度，无法按节点或产品范围查询，只能按应用输出。
- ❌ 单次 ServerMap 最多 4 跳，长调用链只能标记为「可能更长」。
- ⚠️ D5 是**采样**而非全量，覆盖率必须披露。
- ⚠️ 应用名依赖命名规范，未对齐的应用一律 🟨。

---

## §6B D6 省节点故障域（arch-opt-mcp-server）

> 数据源为统一拓扑代理（easyviews 网关，凭据 `ARCH_APP_ID` / `ARCH_APP_SECRET`）。
> D6 必须按以下四步顺序执行，任一步失败即在该步断点记 🟨。

### 6B.1 四步链路

| 步骤 | 工具 | 关键字段 | 失败处理 |
|------|------|----------|----------|
| 1 | `mcp_node_list(filter_prefix="云公司")` | 节点名、`ctyun_node_code3` | 目标节点缺失或编码为空 → 🟨「节点编码缺失」 |
| 2 | `mcp_virtual_structure(ctyun_node_code3)` | `ipData`：宿主机 ↔ 云主机 | 映射为空 → 🟨「宿主机映射为空」 |
| 3 | `mcp_topology_info(node)` | `projectIpData`：项目 ↔ 云主机 | 部署关系为空 → 🟨「项目部署关系为空」 |
| 4 | `mcp_fault_domain(node, min_instances?)` | 实例数、宿主机数、`max_host_concentration` | 实例数 < `min_instance_to_check` → 🟨 |

> 服务端在步骤 2 建立「云主机 IP → 宿主机 IP」反向索引，步骤 4 据此统计每个项目实例落在哪些宿主机上。

### 6B.2 判定规则

| 条件 | 四态 | conclusion |
|------|------|-----------|
| 项目实例数 < `min_instance_to_check`(1) | 🟨 | 实例数不足，无法评估故障域集中度 |
| 存在宿主机的实例集中比例 > `max_host_concentration`(0.5) | 🟥 | 单宿主机承载 N/M 实例（X%），故障域过于集中 |
| 最大集中比例 ≤ 0.5 且映射完整 | 🟩 | 实例在宿主机间分布正常 |
| 任一步接口失败/映射缺失 | 🟨 | 断点步骤 + 缺口原因 |

### 6B.3 口径限制（写入报告「数据限制」）

- D6 只能给出**物理宿主机**维度的集中度，不覆盖机架/交换机/可用区。
- 云主机 IP 与宿主机 IP 的映射以拓扑代理返回为准；未纳管的云主机会被排除，可能低估集中度。
- `get_agent_statistics_tool` 返回的是 Agent 主机名/IP，**不是**物理宿主机，不得替代 D6。
- 拓扑代理不可达时 D6 一律 🟨，不得用 K8s node 分布代替风险判定（K8s node ≠ 物理宿主机）。

---

## §7 四态汇总规则

| 状态 | 标识 | 含义 | 报告处理 |
|------|------|------|----------|
| `risk` | 🟥 | 阈值命中，含证据 | 输出问题卡 + 优化建议 |
| `ok` | 🟩 | 未触发规则 | 计入正常项，简述 |
| `insufficient` | 🟨 | 指标缺失 / 标签口径不符 | 标注「信息不足」并说明缺口 |
| `not_applicable` | ⚪ | 非 -dep、无 HPA 等 | 单独归类，不计风险/正常 |

> 汇总口径：某维度**只要有一条 🟥 → 该维度 🟥**；🟨/⚪ 不改变维度主态，但必须逐条列出。

---

## §8 输出结构（每条结果）

技能侧判定后，每条结果统一为：

```json
{
  "status": "risk | ok | insufficient | not_applicable",
  "dimension": "D1 | D2 | D3 | D4 | D5 | D6",
  "node": "<cmdb_node>",
  "namespace": "<namespace>",
  "deployment": "<部署名>",
  "conclusion": "<中文结论>",
  "collection": "<采集工具与参数，如 mcp_replica(node=…, deployments=…) / get_server_maps_batch_tool(concurrency=3)>",
  "evidence": {
    "source": "arch-opt-mcp-server | pinpoint-mcp-server",
    "query": "<工具名+参数 或 PromQL>",
    "observed": "<观测值>",
    "threshold": "<阈值>"
  },
  "collected_at": "YYYY-MM-DD HH:MM:SS",
  "rule_version": "2.2.0"
}
```

维度汇总结果额外含：`node`、`window`、`counts={risk, ok, insufficient, not_applicable}`、`items[]`。

- `collected_at`：本次查询时间（arch-opt 工具直接返回）。
- `rule_version`：`2.2.0`（与 `thresholds.json` 的 `version` 一致）。

---

## §9 阈值表（与 thresholds.json v2.2.0 对齐）

| 键 | 值 | 用途 |
|----|----|------|
| `replica.min_replicas` | 2 | D1 副本冗余下限（业务确认：至少 2 副本；`spec < 2` 即单副本判风险） |
| `replica.check_available_equal` | true | D1 是否校验 available == spec |
| `capacity.cpu_throttle_ratio` | 0.20 | D2 CPU 限流比阈值 |
| `capacity.memory_usage_ratio` | 0.85 | D2 内存使用率阈值 |
| `capacity.hpa_max_reached_check` | true | D2 是否检查 HPA 达最大副本 |
| `capacity.duration_minutes` | 15 | D2 持续窗口 |
| `load_balance.max_avg_ratio` | 2.0 | D3 最大/平均负载比阈值 |
| `load_balance.duration_minutes` | 15 | D3 持续窗口 |
| `oom.lookback_days` | 7 | D4 近期窗口 |
| `link_dependency.min_in_degree` | 2 | D5 集中依赖判定（**provisional**） |
| `link_dependency.max_out_degree` | 10 | D5 下游过宽判定（**provisional**） |
| `link_dependency.error_rate_threshold` | 0.05 | D5 错误率阈值（**provisional**） |
| `link_dependency.slow_call_ratio_threshold` | 0.10 | D5 慢调用占比阈值（**provisional**） |
| `link_dependency.min_sample_count` | 20 | D5 比例类规则最小样本量（**provisional**） |
| `link_dependency.max_chain_depth` | 4 | D5 链路深度上限（受 Pinpoint 4 跳限制） |
| `link_dependency.cycle_check` | true | D5 是否检查 A↔B 循环依赖 |
| `link_dependency.slow_call_threshold_ms` | null | Pinpoint 服务端慢调用阈值，未知，待确认 |
| `fault_domain.min_instance_to_check` | 1 | D6 最小实例数，低于则 🟨 |
| `fault_domain.same_host_all_risk` | true | D6 同宿主机承载全部实例直接判风险 |
| `fault_domain.partial_concentration_report` | true | D6 是否输出部分集中度明细 |
| `fault_domain.max_host_concentration` | 0.5 | D6 单宿主机最大集中比例阈值，超过判 🟥 |
| `container_check.dep_suffix_filter` | true | 仅对 `-dep` 结尾应用做容器检查 |

> 修改本表时，同步修改 `arch-opt-mcp-server/config/thresholds.json`，保持两处版本号一致。
> D5 全部阈值为 provisional，联调后需回写本表与 `thresholds.json`，并升级 `rule_version`。

### 9.1 实测基线（贵州，2026-10-19，即时查询）

| 维度 | 命中规模 | 备注 |
|------|----------|------|
| D1 `spec < 2` | 约 1363 条序列（`-dep` 共 1802 条，约 76%） | `min_replicas` 已由 3 调为 2；单副本应用仍普遍，报告须按产品/namespace 分组呈现 |
| D1 `available < spec` | 15 个 | 真实可用性缺口 |
| 广州4c（默认节点）`-dep` 规模 | 570 条序列，其中 `spec < 2` 约 503 条 | 广州业务主力节点 |
| D2 CPU 限流 > 0.20 | 0 个（972 个应用有指标） | — |
| D2 内存 > 0.85 | 46 个（1391 个应用有 limit 指标） | Top 值接近 0.99 |
| D3 max/avg > 2.0 | 10 个 | — |
| D4 OOM（7 天窗口） | 30 个 | 即时查询为 23 个，说明窗口回看更完整 |
| D2 HPA | 0 个 | 无 HPA 指标，统一 ⚪ |
