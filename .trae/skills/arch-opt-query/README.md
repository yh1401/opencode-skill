# arch-opt-query — 架构优化巡检 Skill

按《架构优化智能体实现方案-V2》执行架构风险分析的 Trae Skill：中心节点的副本 / 容量 / 负载 / OOM / 链路依赖，省节点的宿主机集中度。

**核心原则**：规则引擎（MCP 工具）负责取数、关联与数值计算；大模型负责解释风险、串联业务影响、组织报告。**缺失指标与未确认阈值必须明确说明信息不足，不得补造数据。**

---

## 目录

- [一、这是什么](#一这是什么)
- [二、分析范围](#二分析范围生产环境固定口径)
- [三、目录结构](#三目录结构)
- [四、工具链](#四工具链)
- [五、中心节点 7 步流程](#五中心节点-7-步流程)
- [六、省节点 5 步流程](#六省节点-5-步流程)
- [七、8 条规则与阈值](#七8-条规则与阈值)
- [八、取数口径的四种模式（防卡死设计）](#八取数口径的四种模式防卡死设计)
- [九、四态口径](#九四态口径)
- [十、报告输出要求](#十报告输出要求)
- [十一、已知口径风险](#十一已知口径风险必须随报告声明)
- [十二、上下游依赖](#十二上下游依赖)
- [十三、当前已知问题](#十三当前已知问题)
- [十四、快速开始](#十四快速开始)
- [十五、常见问题](#十五常见问题)

---

## 一、这是什么

这是一个**编排型 Skill**，本身不包含取数逻辑。它的作用是：

1. 告诉 Agent 该按什么顺序调用哪些 MCP 工具
2. 定义哪些结果算风险、哪些必须写「数据不足」
3. 约束报告的字段、口径和优先级排序方式

它**不是**：
- 不是 Python 脚本（取数和判定在 MCP 服务里）
- 不是固定报告模板（Agent 根据 findings 组织内容）
- 不是阈值配置（阈值在 `pinpoint-mcp/config.py` 里，以代码为准）

真正生效的是 `SKILL/SKILL.md`（Trae 启动 Skill 时只加载这个文件）。本 README 是给人看的全景说明。

---

## 二、分析范围（生产环境固定口径）

### 2.1 分析对象

| 对象 | 节点 | 分析重点 |
|---|---|---|
| 中心节点 | `云公司->贵州`（6 模块）、`云公司->广州4d`（7 模块） | 副本可用性、容量瓶颈、负载分布、OOM、服务依赖 |
| 省节点 | `list_nodes_tool` 返回中「云公司」前缀的节点 | 项目多实例是否集中在同一物理宿主机 |

中心节点业务模块：核心业务 API、设备信令调度、直播服务网关、设备告警、云眼核心业务、设备云存上传（苏州）、消息中心 4.0。

### 2.2 三层过滤（生产巡检硬约束，逐层叠加）

#### 第一层：集群排除

在 `list_nodes_tool` 返回后**立即**过滤，后续所有工具不得对这些集群发起查询：

| 排除类型 | 识别方式 | 示例 |
|---|---|---|
| 小 A 系列集群 | 节点名含 `小A` / `a-series` / `a_series`（大小写不敏感） | `云公司->小A海口`、`云公司->a_series_shenzhen` |
| 国际节点集群 | 节点名含 `海外` / `国际` / `global` / `i18n` | `云公司->海外新加坡`、`云公司->global-us` |

节点名无明确标识时，可调用 `get_module_topology_tool` 或 `prometheus_query_tool` 看 `cmdb_node` / `region` 标签辅助判别。

#### 第二层：命名空间排除

拿到节点的应用/项目列表后，必须排除以下 9 类命名空间，仅保留正常业务命名空间的应用：

| 排除类型 | 识别方式 | 典型 namespace |
|---|---|---|
| 系统组件 | k8s 原生系统 ns | `kube-system`、`kube-public`、`kube-node-lease`、`default` |
| Operator | 以 `-operator` 或 `operator-` 结尾/开头 | `prometheus-operator`、`istio-operator`、`cert-manager-operator` |
| 监控可观测 | 监控、日志、追踪组件 | `monitoring`、`prometheus`、`grafana`、`loki`、`tempo`、`efk`、`elastic-system` |
| Ingress / 网关 | 流量入口组件 | `ingress-nginx`、`istio-system`、`traefik`、`apigw` |
| 存储 | 持久化存储组件 | `rook-ceph`、`openebs`、`storage`、`nfs` |
| 灰度 / canary | 灰度发布专用 ns | `*-gray`、`*-canary`、`gray-*`、`canary-*`、`staging` |
| CI/CD | 流水线组件 | `jenkins`、`gitlab-runner`、`argocd`、`tekton` |
| 安全 | 安全扫描/策略 | `kube-bench`、`trivy`、`opa`、`cert-manager` |
| 其他基础设施 | 非业务平台组件 | `velero`、`harbor`、`registry`、`etcd`、`calico-system`、`cattle-system` |

工具层已原生支持 `exclude_namespace` 参数，默认按服务端 `ARCH_EXCLUDE_NAMESPACES` 配置自动排除（35 项），一般不需要显式传。

**`exclude_namespace` 四种传参方式**：

| 场景 | `exclude_namespace` 传值 | 效果 |
|---|---|---|
| 普通巡检（推荐） | 不传 | 自动排除系统/灰度 ns，只拿业务应用 |
| 需要额外排除 | `"kube-system,monitoring,gray-*"` 或 `["kube-system","monitoring"]` | 在默认清单基础上追加 |
| 只想排除特定项（覆盖默认） | 显式传完整清单字符串或 list | 用指定清单替换默认值 |
| 需要全量（含系统 ns） | `""` 或 `"none"` | 禁用排除，拿全节点所有 ns |

**优先级**：显式指定 `namespace="xxx"` 精确值时，`exclude_namespace` 不生效（精确 ns 比排除清单优先级高）。

**哪些工具支持 `exclude_namespace`**：

| 工具 | 支持方式 |
|---|---|
| `arch_metric_query_tool` | PromQL 自动拼 `namespace!~"(...)"` |
| `arch_rule_check_tool` | 同上（单规则判定） |
| `analyze_node_runtime_tool` | 同上（批量 8 条规则） |
| `prometheus_query_tool` | 自动在第一个 PromQL 选择器里拼排除；已有 `namespace="xxx"` 精确值时不自动加；无选择器时返回 warning |
| `get_module_topology_tool` | **不支持**（EasyViews 接口，需在返回后人工过滤） |
| `get_project_deploy_tool` | **不支持**（EasyViews 接口） |
| `analyze_host_concentration_tool` | **不支持**（EasyViews 接口） |

#### 第三层：应用二次筛选

命名空间过滤后，仍需对应用名按 `-dep` 后缀二次筛选，确保最终纳入分析的同时满足：

1. 在业务命名空间内
2. 应用名以 `-dep` 结尾（Deployment 身份确认）

---

## 三、目录结构

```
arch-opt-query/
├── SKILL/
│   └── SKILL.md                    ← 编排主文件（唯一会被 Trae 加载）
├── docs/README.md
├── references/
│   ├── prometheus_query_rules.md   ← PromQL 口径参考
│   └── prompt_template.md          ← 报告模板
├── result/                         ← 运行产物（Agent 落盘的报告）
├── 报告集/                          ← 归档报告
└── 架构优化/                        ← 方案原始文档 + 可行性分析 + 问题记录
```

> 真正的说明全在 `SKILL/SKILL.md`；本 README 是给人看的全景说明。

---

## 四、工具链

### 4.1 EasyViews 组（拓扑与部署关系）

| 工具 | 入参 | 出参关键字段 | 关键约束 |
|---|---|---|---|
| `get_module_topology_tool(node, product_name, simplify)` | `node` 节点名 | `modules[].projectName[]` / `deployNode` / `moduleCount` | 返回结构可能含 `topologyData.nodes[]`，需解析 |
| `get_project_deploy_tool(node)` | `node` 节点名 | `projectIpData`（项目 → 云主机 IP 列表） | 传节点名，不传 `node_code` |
| `get_host_structure_tool(node_code)` | `node_code` = `ctyun_node_code3` | `ipData`（物理宿主机 IP → 云主机 IP 列表） | **必须传 `ctyun_node_code3`**，不能传 `node` |
| `analyze_host_concentration_tool(node, node_code)` | `node` + `node_code` | 集中清单 + 覆盖率 | 内部关联上面两个工具的结果 |

### 4.2 Prometheus 组（运行指标）

| 工具 | 粒度 | 阈值判定方 | 主要出参 |
|---|---|---|---|
| `arch_metric_query_tool(rule, node, ...)` | 单规则 | **Agent**（只取原始数据） | `samples` / `summary`，不带四态结论 |
| `arch_label_probe_tool(node)` | 口径探测 | — | `verdict.suggestedMode` / `metrics[].status` / `labelsSource` |
| `arch_rule_check_tool(rule, node, deployment, namespace, ...)` | 单规则 | **MCP** | `statusCode` / `status` / `items[]` / `scopeFallback` |
| `analyze_node_runtime_tool(node, deployments, ...)` | **整节点 × 8 规则** | **MCP** | `findings[]` / `ruleReports` / `collectionStatus` |
| `prometheus_query_tool(query, ...)` | 任意 PromQL | Agent | 原始 Prometheus 响应 |

**三个工具的关系**：
- `arch_metric_query_tool` 是低级取数接口，Agent 自己判定阈值——**仅用于非标准分析或手工排查**
- `arch_rule_check_tool` 是单规则服务端判定——用于对 findings 里的风险项做**下钻复核**
- `analyze_node_runtime_tool` 是整节点批量入口——**正常巡检的主力工具**，一次跑完 8 条规则

### 4.3 Pinpoint 组（链路下钻）

| 工具 | 作用 |
|---|---|
| `get_link_topology_tool(node, projects, minute, caller_range, callee_range)` | 应用链路拓扑（调用关系） |
| `get_server_map_tool` | Pinpoint 服务地图 |
| `drag_heatmap_tool` | 指定应用的请求数热力图散点 |
| `get_transaction_trace_tool` | 全链路调用栈明细 |

### 4.4 三个标识不可混用（方案 2.1 硬约束）

| 标识 | 来源 | 只能用于 | 错误示例 |
|---|---|---|---|
| `node`（如 `云公司->海口9`） | `list_nodes_tool.node` | 项目部署、模块拓扑、指标标签 | ❌ 传给 `get_host_structure_tool` |
| `ctyun_node_code3`（如 `hi-haikou-9`） | `list_nodes_tool.ctyun_node_code3` | 宿主机关系、集中度 | ❌ 传给 `get_module_topology_tool` |
| `deployNode`（可能逗号串多别名） | `get_module_topology_tool.deployNode` | 链路拓扑 | ❌ 替换为单一节点名 |

`ctyun_node_code3` 为空 → 记「映射数据不足」；`deployNode` 不得替换为单一节点名称。

---

## 五、中心节点 7 步流程

```
1. get_module_topology_tool(node="云公司->贵州")
      ↓ 提取 modules[].projectName[] / deployNode / namespace
2. 命名空间过滤（按第二节第二层排除规则过滤返回的模块）
      ↓ 排除项记为「非业务命名空间，已排除」
3. 应用筛选（只留 -dep 结尾的应用，其余记「本阶段未覆盖」）
4. 去重关联（按 节点 + 应用 + 命名空间 去重；同一应用多模块保留全部归属）
5. arch_label_probe_tool(node)          ← 口径探测（防卡死关键）
   analyze_node_runtime_tool(node)       ← 一次跑完 8 条规则
   （findings 有风险时）arch_rule_check_tool(…, namespace=…)  ← 必须传 namespace
6. get_link_topology_tool(node=deployNode, projects=[模块 projectName])
7. Pinpoint 工具定位慢/失败事务
```

### 各步详细说明

**第 1 步：模块归属**
- 调用 `get_module_topology_tool(node="云公司->贵州")`
- 从返回中提取每个模块对象的 `projectName`（数组）、`deployNode`、`namespace`
- 注意返回结构可能是 `data[].topologyData.nodes[]`，需要解析

**第 2 步：命名空间过滤**
- 按第二节命名空间排除规则过滤
- 被排除的项在报告中记为「非业务命名空间，已排除」

**第 3 步：应用筛选**
- 仅对名称以 `-dep` 结尾的应用执行容器检查
- 其余记为「本阶段未覆盖」
- `-dep` 后缀是初步筛选，Deployment 身份仍需通过监控或资源信息确认

**第 4 步：去重与关联**
- 按「节点 + 应用 + 命名空间」去重
- 结果关联回所属模块
- 同一应用属于多模块时保留全部归属关系
- 应用名可能跨集群/命名空间重复，需结合监控标签补齐唯一标识

**第 5 步：口径探测与指标检查（核心）**
- 先 `arch_label_probe_tool(node=…)` 确认标签口径
- 标签完整时 `analyze_node_runtime_tool(node=…)` 一次跑完 8 条规则
- 部署级缺 `cmdb_node` 时传 `deployments=[应用清单]` 让工具按应用名兜底
- **单应用复核时 `arch_rule_check_tool` 必须传 `namespace`**，避免全量扫描

**第 6 步：链路分析**
- 调用 `get_link_topology_tool(node=deployNode, projects=[模块 projectName])`
- `node` 参数必须用 `deployNode`（可能含多别名逗号串），不能用 `node`

**第 7 步：异常下钻**
- 对异常调用边用 Pinpoint 工具定位具体慢/失败事务

### 指标标签

- `cmdb_node`（节点名）
- `env="生产"`
- `deployment`（应用名）
- `namespace`（命名空间，用于消歧）

> **注意**：`cmdb_node` 在 KSM 部署级指标上可能缺失（见第十一节），此时以应用名口径为准。

---

## 六、省节点 5 步流程

```
1. list_nodes_tool()
   → 筛选「云公司」前缀节点
   → 按集群排除规则过滤小 A 系列和国际集群
   → 取 node 与 ctyun_node_code3
2. get_host_structure_tool(node_code=…)
   → 取 ipData（物理宿主机 IP → 云主机 IP 列表）
   → 反向索引为「云主机 IP → 物理宿主机 IP」
3. get_project_deploy_tool(node=…)
   → 以 projectIpData（项目 → 云主机 IP 列表）为主关联第 2 步
   → 项目名不含 -dep 且命名空间为系统 ns 的排除
4. analyze_host_concentration_tool(node=…, node_code=…)
   → 直接输出集中清单
5. 建立「项目 → 云主机 → 物理宿主机」映射
   → 输出风险项目、涉及实例、宿主机分布与调整建议
```

### 集中度判定（方案 2.5）

建议记录两个统计值：
- **宿主机数**
- **最大宿主机集中比例**（单一宿主机承载的该项目云主机数最大值 ÷ 该项目云主机总数）

| 场景 | 判定与处理 |
|---|---|
| 仅部署 1 台云主机 | 记录单实例部署；是否构成风险由业务冗余要求决定 |
| 多台全部位于同一宿主机 | 明确标记共同故障点，建议分散到不同物理宿主机 |
| 分布于多宿主机但部分集中 | 输出每台宿主机承载数量与比例，评估单宿主机故障影响 |
| 部分云主机无法映射宿主机 | 标记数据不足，输出已匹配与未匹配清单，避免低估风险 |
| 同一云主机映射到多个宿主机 | 标记映射冲突，核查数据更新时间及迁移记录 |

> **映射不完整时，仅输出已知分布与覆盖率，不据此作完整故障域结论。**

---

## 七、8 条规则与阈值

| 风险类型 | rule | 判定依据 | 证据与注意事项 |
|---|---|---|---|
| 副本不足 | `deployment_replicas` | 期望副本未达冗余要求（**副本要大于 2**） | 期望副本、可用副本、节点、应用。⚠️ 口径待确认：如业务要求「至少 2 个副本」需另行确认 |
| 副本不可用 | `deployment_available` | 可用副本低于期望副本 | 差值、持续时间；关联发布、扩缩容和故障事件 |
| CPU 限流 | `cpu_throttle_ratio` | 限流周期比例 **> 20%，持续 15 分钟** | 同容器同窗口比值；**总周期为零时不计算**；需交叉验证应用侧耗时（P99），仅比例超标而延迟无变化时优先级可降低 |
| 内存容量 | `memory_ratio` | 工作集 / 有效 limit **> 85%，持续 15 分钟** | 明确容器范围与有效 limit；limit 缺失、为零或不适用时单独记录 |
| 扩容容量 | `hpa_replicas` | HPA 达最大副本（比值 ≥ 1），且资源或业务负载仍持续偏高 | HPA 当前/最大副本、目标指标与实际负载；**无 HPA 的应用标记「不适用」** |
| Pod 负载不均衡 | `pod_cpu_imbalance` | 同规格 Pod 最大 / 平均 CPU **> 2，持续 15 分钟** | 统一采样窗口与 CPU 口径，排除不同规格与启动阶段；**平均值接近零时跳过**避免比值失真 |
| OOM | `oom_killed` + `restart_increase` | 近期出现 OOMKilled **且**存在对应重启证据（默认近 30 分钟重启增长 > 0） | 容器终止原因、重启增量、时间窗口；**历史终止原因不能单独证明近期 OOM**。出现 OOM 应回查内存使用率是否长期贴近 limit |
| 链路依赖 | — | 集中依赖、复杂依赖、长链路、异常调用、循环依赖 | 调用边及统计窗口、请求量、错误率与耗时；**链路长度、依赖数等阈值需另行制定** |
| 宿主机集中 | `analyze_host_concentration_tool` | 同一项目多个云主机实例落在同一物理宿主机 | 项目实例列表、宿主机映射、覆盖率及业务角色 |

### 持续时间窗口的取证方式

涉及「持续 15 分钟」的规则，必须取完整时间窗口证据。

- `arch_metric_query_tool` 采用**多点历史时间点即时查询采样**（默认 15 点 × 60 秒）
- **不得用单次即时值证明持续触发**
- 部分场景可用经验证的 PromQL 时间窗口表达式替代

---

## 八、取数口径的四种模式（防卡死设计）

每个节点巡检前**必须**先调用一次 `arch_label_probe_tool(node=…)`，按 `verdict.suggestedMode` 选择取数口径，**不要跳过直接跑规则**。

### 8.1 四种模式

| suggestedMode | 含义 | 后续做法 |
|---|---|---|
| `cmdb_node` | 部署级与容器级指标标签一致 | 正常按 `node` 查询，直接进入批量分析 |
| `deployment_names` | 部署级（KSM）缺 `cmdb_node`、容器级（cAdvisor）有该标签 | 先取应用清单，再调 `analyze_node_runtime_tool(node=…, deployments=[…])`，工具会自动按应用名口径兜底 |
| `cluster` | 两类指标均未命中节点标签，或该节点固定按 cluster 取数 | 用 `prometheus_query_tool` 以 `cluster` 选择器查询，或按应用名直查；若两者都为空 → 直接记「采集链路缺失」，**不进入逐应用试查**。已配置映射的节点（广州4d → `cluster="广州4d生产"`）工具会自动应用 |
| `env_mismatch` | `cmdb_node`/`cluster` 有序列，但 `env` 过滤命中为 0 | 说明 `env` 标签取值已变更：先用 `count by (env) (kube_deployment_spec_replicas{deployment=~".+-dep"})` 确认实际取值，再显式传 `env` 参数；**未确认前不得继续批量** |

### 8.2 节点标签口径

- 默认按 `cmdb_node` 取数
- 广州4d 的业务指标不带该标签，固定改用 `cluster="广州4d生产"`（服务端 `ARCH_NODE_CLUSTER_MAP`）
- 规则查询与探测均自动应用，无需手工传
- 手工写 PromQL 时也不要用 `cmdb_node="云公司->广州4d"`

### 8.3 探测结果解读

| 字段 | 值 | 含义 |
|---|---|---|
| `labelsSource` | `series_sample` | 正常降级（VictoriaMetrics 不支持 `labels?match[]`），已在工具内自动完成，**无需换工具重试** |
| `metrics[].status` | `NO_DATA` | 该指标没有任何序列（采集未接入），不是探测失败，也不是标签口径问题 |
| `status` | `FAIL` | 两条标签路径都不可用，需要在报告写明数据源异常 |
| `verdict.suggestedMode` | `env_mismatch` | `envMatched=false`，先确认 `env` 实际取值并显式传参，**不得跳过继续批量**——否则 8 条规则会全部返回「数据不足」，看起来像无风险 |

### 8.4 工具调用预算（每个节点）

| 工具 | 次数 |
|---|---|
| `list_nodes_tool` | 1 次 |
| `arch_label_probe_tool` | 1 次 |
| `analyze_node_runtime_tool` | 1 次（覆盖全部 8 条规则） |
| 单应用复核（`arch_rule_check_tool`） | 仅在 `findings` 出现风险时按需调用 |

**预算用尽仍无结论时，直接在报告记「数据不足」并写明缺失的指标/标签，不得继续试查。**

### 8.5 三条禁止事项

1. 不得为绕开标签缺口，逐规则、逐应用反复调用 `arch_metric_query_tool` 试查
2. 不得把 MCP 返回落盘后写 Python 脚本解析中间结果；采样已按 series 聚合，直接读返回的 `summary` / `findings` / `verdict`
3. 不得把「标签缺失导致的空结果」写成「未触发规则 / 无风险」

---

## 九、四态口径

所有结果须区分四种状态，**接口失败、监控缺失或关联不完整时不得给出无风险结论**：

| 状态 | 含义 | 典型场景 |
|---|---|---|
| 已发现风险 | 规则触发，证据充分 | 副本 < 2 且证据齐全；CPU 限流 > 20% 持续 15 分钟 |
| 未触发规则 | 规则已执行，未达阈值 | 副本 = 3 且可用 = 3；CPU 限流 5% |
| 数据不足 | 指标缺失、映射不完整、采样窗口不足 | PromQL 返回空且标签口径未确认；宿主机映射覆盖率 < 100% |
| 不适用 | 规则本身不适用于该对象 | 无 HPA 的应用之于扩容容量规则；`spec=0` 的下线应用之于副本规则 |

工具返回的 `status` 字段（`SUCCESS` / `NO_DATA` / `DATA_INSUFFICIENT` / `FAIL`）是判定依据之一。

**四态与工具 status 的对应关系**：

| 工具 status | 对应四态 |
|---|---|
| `SUCCESS` | 已发现风险 或 未触发规则（取决于是否达阈值） |
| `NO_DATA` | 数据不足 |
| `DATA_INSUFFICIENT` | 数据不足 |
| `FAIL` | 数据不足（数据源异常） |

---

## 十、报告输出要求

### 10.1 组织方式

按「节点 → 模块 / 项目 → 应用 / 实例」组织。

### 10.2 每条风险的 7 个必填字段

| 字段 | 内容要求 |
|---|---|
| 分析对象 | 节点、模块、应用或项目及唯一标识 |
| 风险类型与状态 | 风险分类，以及已发现风险、未触发、数据不足或不适用 |
| 判定依据 | 触发规则、阈值、实际观测值、持续时间 |
| 证据 | 指标查询结果、调用拓扑、云主机与宿主机映射及采集时间 |
| 影响范围 | 涉及实例、上下游服务、所属核心业务；**推断需显式标注** |
| 优化建议 | 与风险对应的处理建议、前置条件和建议优先级 |
| 数据限制 | 指标缺失、映射不完整、采样窗口不足或口径待确认等 |

### 10.3 首页

报告首页必须展示：

- 分析范围
- 采集窗口
- 数据覆盖率
- 主要风险

### 10.4 优先级

优先级结合**业务重要性、影响范围和证据充分程度**确定，**避免仅凭单一指标大小排序**。

### 10.5 分析记录

每次分析必须记录：

- 任务标识（`task_id`）
- 采集时间
- 分析窗口
- 目标节点
- 规则版本

### 10.6 报告各部分 ↔ 取数工具映射（中心节点）

按报告集里实际出现的章节逐段对照：每一段由哪个工具产出、传什么参数、缺数据时怎么写。

| 报告部分 | 取数工具 | 关键入参 | 取到什么 | 取不到时怎么写 |
|---|---|---|---|---|
| **报告信息表**（任务标识 / 采集时间 / 分析窗口 / 规则版本） | 无工具，Agent 生成 + `config.ARCH_RULE_VERSION` | — | `task_id`、时间戳、窗口、版本号 | — |
| **一、分析范围**：模块与 -dep 应用数 | `get_module_topology_tool` | `node`、`product_name="天翼看家"` | `modules[].projectName[]`、`moduleCount`、`deployNode`、`namespace` | `NO_DATA` → 「该节点未配置业务模块」 |
| 应用去重与模块归属 | 无工具，Agent 按「节点+应用+命名空间」去重 | — | 去重后应用数、重复归属清单 | 归属缺失 → 「未纳入模块归属」 |
| **K8s 实测部署数 / 覆盖率** | `prometheus_query_tool`：`count(kube_deployment_spec_replicas{cmdb_node="…",env="生产",deployment=~".*-dep"})` | 节点标签口径 | 实际 `-dep` 部署数 | 覆盖率 = EasyViews ÷ 实测，必须写出来 |
| **排除项**（灰度 ns / 系统 ns / 非 -dep 项目） | `prometheus_query_tool`：`count by (namespace) (…)` + `get_module_topology_tool` 返回 | — | 被排除的 ns 与项目清单 | 边界不确定 → 标「过滤边界存疑」 |
| **二、规则判定总览**（8 条规则状态表） | `analyze_node_runtime_tool`（首选，一次跑完） | `node`、可选 `deployments`、`namespace` | `ruleReports[].status`、`findings[]`、`collectionStatus` | 整体 `FAIL` → 逐条写「数据不足」并附失败原因 |
| **三、规则明细**：逐应用观测值与证据 | `arch_rule_check_tool`（单规则复核）或 `arch_metric_query_tool`（仅取数） | `rule`、`node`、`deployment`、**`namespace`（必须传）** | `items[]`、`observedValue`、`threshold`、`scopeFallback` | `NO_DATA` → 「数据不足」；`include_samples=false` 只有摘要 |
| 副本不足清单（含 spec=0 / spec=1 拆分） | `arch_rule_check_tool(rule="deployment_replicas")` 或 `analyze_node_runtime_tool` 的 findings | `node` | deployment / namespace / 期望副本 | spec=0 单列 `offlineCount`，不计入风险数 |
| 副本不可用（缺口与持续时长） | `arch_rule_check_tool(rule="deployment_available")` | `node` | 缺口值、持续时间 | 15 分钟窗口无缺口 → 「未触发」 |
| CPU 限流实测比例 | `arch_rule_check_tool(rule="cpu_throttle_ratio")` | `node`、`namespace` | 每采样点的 throttled/periods 比值 | 无 `ARCH_LATENCY_PROMQL` → 声明「无延迟交叉验证」 |
| 内存水位（working_set / limit） | `arch_rule_check_tool(rule="memory_ratio")`；口径不明时 `prometheus_query_tool` 查 `container_spec_memory_limit_bytes` 是否存在 | `node`、`namespace` | 比值与容器清单 | limit 指标缺失 → 「不适用（limit 指标缺失）」，**不得写无风险** |
| HPA 扩容 | `arch_rule_check_tool(rule="hpa_replicas")` | `node` | 当前/最大副本、目标指标 | 无 HPA 序列 → 「不适用」 |
| Pod 负载不均衡 | `arch_rule_check_tool(rule="pod_cpu_imbalance")` | `node` | 同规格 max/avg 比值 | 无 CPU limit/request 规格 → 「数据不足」（无法分组） |
| OOM 证据（终止原因 + 7d 重启增量） | `arch_rule_check_tool(rule="oom_killed")` | `node`、`deployment`、`namespace` | 每 Pod 的 `reason=OOMKilled` 与重启增量 | 7d 子查询超时/工具失败 → 「数据不足」，历史终止原因不得单独判风险 |
| 重启增长 TopN | `arch_rule_check_tool(rule="restart_increase")` | `node` | 7 天累计重启次数与 Pod 分布 | 同上 |
| **链路拓扑规模**（节点数 / 边数 / 孤立节点） | `get_link_topology_tool` | `node=deployNode`、`projects`、`minute` | 节点、调用边、统计字段 | Pinpoint/EasyViews 失败 → 「数据不足」 |
| 集中依赖（入度）、下游过多（出度） | 同上（对返回的图做入/出度统计） | 同上 | 入度/出度 Top | 阈值未定义 → 标「工具默认值，待确认」 |
| 循环依赖 / 最长调用链 | 同上（图算法） | 同上 | 环清单、路径长度 | 环上节点被排除 → 「不能断言无长链」 |
| 异常调用边（错误率 / 慢调用） | 同上 | 同上 + 错误率/耗时阈值 | `abnormalEdgeCount`、边级别错误率 | 小样本 → 标「置信度低」 |
| 链路下钻（慢事务 / 失败事务 trace） | `get_server_map_tool` / `drag_heatmap_tool` / `get_transaction_trace_tool` | 应用名、时间窗 | 事务明细 | 认证 302 → 「数据不足」 |
| **Pinpoint 交叉验证** | `list_applications_tool` + 应用名映射 | — | 应用映射覆盖率、调用边数 | 覆盖率低必须声明「链路结论以 EasyViews 为准」 |
| **四、数据限制与口径声明** | 汇总各工具的 `status` / `degradedReason` / `collectionStatus` / `labelsSource` | — | 失败原因与口径差异 | 必须逐条写出，不得省略 |
| **五、优化建议汇总** | 无工具，Agent 基于 findings 推断 | — | 建议 + 优先级 + 前置条件 | 影响范围推断必须显式标注 |

### 10.7 报告各部分 ↔ 取数工具映射（省节点）

| 报告部分 | 取数工具 | 关键入参 | 取到什么 |
|---|---|---|---|
| 节点清单与 `ctyun_node_code3` | `list_nodes_tool` | `node_prefix="云公司"` | `node`、`ctyun_node_code3`、`missingNodeCode` |
| 项目 → 云主机 IP | `get_project_deploy_tool` | `node` | `projectIpData` |
| 物理宿主机 → 云主机 IP | `get_host_structure_tool` | **`node_code`（= `ctyun_node_code3`）** | `ipData` |
| 项目总数 / 宿主机总数 / 场景分类 / 集中清单 / 覆盖率 / 冲突 | `analyze_host_concentration_tool` | `node` + `node_code` | 场景分布（ALL_IN_SAME_HOST / SINGLE_INSTANCE / PARTIAL_CONCENTRATION / SPREAD）、`unmapped`、`conflictVms` |
| 单实例项目业务属性判断 | 无工具，Agent 按项目名推断 | — | 类别（运维/监控/测试） | 必须标「待业务确认」 |
| 链路依赖（省节点通常不适用） | `get_link_topology_tool` | `deployNode` | 0 边 → 「不适用 + 数据不足」 |

### 10.8 完整处理过程图（中心节点）

下图把 SKILL.md 的 7 步流程、每一步调用什么工具、输入输出的数据长什么样、Agent 在哪一步做什么、最终报告的哪一部分来自这一步，全部串在一起。

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                     中心节点完整处理流程（以「云公司->贵州」为例）                          │
└─────────────────────────────────────────────────────────────────────────────────────────────┘

 Agent 收到请求：「帮我给出贵州的架构风险巡检报告」
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 0：前置判断（Agent 逻辑，无工具）                                      │
  │                                                                              │
  │   1. 节点类型：云公司->贵州是「中心节点」 → 跑 7 步流程                        │
  │      （判断依据：SKILL 范围章节，中心节点为 2 个固定节点）                     │
  │                                                                              │
  │   2. 集群排除：节点名无「小A」「海外」「国际」 → 保留                           │
  │                                                                              │
  │   3. 初始化 task_id = "gz-inspect-{时间戳}"，记录分析窗口与规则版本            │
  │                                                                              │
  │   ── 这一段的输出 ──                                                         │
  │   ┌──────────────────────────────────────────────────────────────────┐       │
  │   │ task_id: "gz-inspect-20261109-1510"                              │       │
  │   │ node: "云公司->贵州"                                             │       │
  │   │ rule_version: "V2.3"                                             │       │
  │   │ env: "生产"                                                      │       │
  │   └──────────────────────────────────────────────────────────────────┘       │
  │                                                                              │
  │   🔴 LLM 卡点：                                                                │
  │   a) 文档与代码阈值漂移——SKILL.md 写「副本>2」「OOM 30 分钟」，               │
  │      config.py 实际是 2 和 7d。Agent 如果先读 SKILL.md 会理解错口径，          │
  │      跑一遍看结果再调整。**所有 7 份报告里 Agent 都提到了这个漂移**              │
  │   b) 报告模板本身长（SKILL.md 约 12KB），Agent 每轮都要通读                     │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告首页「分析信息表」                                                  │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 1：模块拓扑（EasyViews 组）                                            │
  │                                                                              │
  │   调用：get_module_topology_tool(                                            │
  │       node="云公司->贵州",                                                   │
  │       product_name="天翼看家",                                               │
  │       simplify=true                                                         │
  │   )                                                                          │
  │                                                                              │
  │   ── 工具返回 JSON（关键字段）──                                             │
  │   ┌──────────────────────────────────────────────────────────────────┐       │
  │   │ status: "SUCCESS"                                                │       │
  │   │ deployNode: "云公司->贵州,guizhou,tyyy-guizhou,云公司->广州4d,guangzhou4d-prod" │
  │   │ moduleCount: 6                                                   │       │
  │   │ modules: [                                                       │       │
  │   │   { moduleId: "core-api", moduleNameCn: "核心业务API",          │       │
  │   │     projectName: ["ehome-order-prod", "ehome-public-dep", ...],  │       │
  │   │     namespace: "ehome-order" },                                  │       │
  │   │   { moduleId: "cloud-eye", moduleNameCn: "云眼核心业务",         │       │
  │   │     projectName: ["vcp-baseline-dep", ...],                      │       │
  │   │     namespace: "vcp-vcp-pro" },                                  │       │
  │   │   ...（共 6 个模块）                                              │       │
  │   │ ]                                                                │       │
  │   └──────────────────────────────────────────────────────────────────┘       │
  │                                                                              │
  │   ── Agent 处理 ──                                                          │
  │   1. 提取 deployNode（多别名原样保留，后续链路分析要用）                       │
  │   2. 6 个模块 × 共 233 个 -dep 应用（去重后）                                │
  │   3. EasyViews 未覆盖的应用 → 记「未纳入模块归属」                              │
  │                                                                              │
  │   🔴 LLM 卡点：                                                                │
  │   a) EasyViews 覆盖率低（贵州 43%、广州4d 36%）——剩下 158 个「未纳入模块归属」  │
  │      的应用（mcm-*/etouch-*/sl-center-*/f2e-*）要不要纳入风险分析？Agent 要     │
  │      自己判断，容易漏，也容易把不该管的管了                                       │
  │   b) deployNode 多别名（5 个逗号串）——Agent 要记住不能替换成单一 node，否则      │
  │      后续链路分析会丢数据。**贵州 1030 报告里 Agent 就踩过这个坑**                │
  │   c) module 的 projectName 数组可能几百个字符串，Agent 处理时占 token            │
  │                                                                              │
  │   ── 取不到时怎么写 ──                                                       │
  │   ▶ status="NO_DATA" → 报告写明「该节点在天翼看家产品下未配置业务模块」        │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告「一、分析范围」的模块表                                              │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 2+3：命名空间过滤 + 应用筛选（Agent 逻辑 + 工具）                       │
  │                                                                              │
  │   调用：prometheus_query_tool(                                               │
  │       query='count by (namespace) (kube_deployment_spec_replicas{            │
  │           cmdb_node="云公司->贵州", env="生产",                              │
  │           deployment=~".*-dep"})'                                            │
  │   )                                                                          │
  │   → 拿到所有 -dep 部署的 namespace 分布                                       │
  │                                                                              │
  │   Agent 按两层规则过滤：                                                     │
  │   第一层：namespace 排除（工具层默认排除 35 项，如 monitoring/kube-system/灰度 │
  │          Operator/存储/安全 等）                                              │
  │   第二层：应用名必须以 -dep 结尾                                              │
  │                                                                              │
  │   ── 过滤后的应用清单 ──                                                     │
  │   233 个应用（业务 ns 内、-dep 结尾）                                         │
  │   + 47 个 spec=0 应用（记 offlineCount）                                     │
  │   + 若干灰度 ns 应用（已排除并记录）                                          │
  │                                                                              │
  │   🔴 LLM 卡点：                                                                │
  │   a) 灰度/边界识别模糊——SKILL 里列了 gray/canary/staging/beta/test，但          │
  │      实际可能还有其他命名（如 python-gray、sl-center-message-gray、              │
  │      vcp-xxl-job-admin-gray）。Agent 要自己正则匹配，漏了就把灰度应用算进风险。   │
  │      **广州4d 1030 报告里就标了「过滤边界存疑」**                                │
  │   b) prometheus_query_tool 的默认 namespace 排除正则在 VictoriaMetrics 下       │
  │      400（转义问题）→ Agent 不得不一条一条手工排除                              │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告「一、分析范围」的排除项清单 + K8s 实测覆盖率                          │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 4：去重与归属（Agent 逻辑）                                            │
  │                                                                              │
  │   按「节点 + 应用 + namespace」去重                                            │
  │   同一应用多模块归属 → 全部保留                                                │
  │                                                                              │
  │   例：ehome-router-dep-dep 同时在 「核心业务API」和「设备信令调度」两个模块    │
  │   → 报告里两处都写，不是 bug，是正常设计                                       │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告「一、分析范围」的重复归属说明                                        │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 5a：口径探测（Prometheus 组）—— **所有后续查询的前置条件**               │
  │                                                                              │
  │   调用：arch_label_probe_tool(node="云公司->贵州")                            │
  │                                                                              │
  │   ── 工具内部做了什么（2 条并发查询）──                                        │
  │   1. count(kube_deployment_spec_replicas{cmdb_node="云公司->贵州"})          │
  │      → 验证 KSM 部署级指标是否带 cmdb_node                                    │
  │   2. count(container_cpu_usage_seconds_total{cmdb_node="云公司->贵州"})       │
  │      → 验证 cAdvisor 容器级指标是否带 cmdb_node                               │
  │                                                                              │
  │   ── 工具返回 JSON ──                                                       │
  │   ┌──────────────────────────────────────────────────────────────────┐       │
  │   │ status: "SUCCESS"                                                │       │
  │   │ verdict.suggestedMode: "cmdb_node"                               │       │
  │   │ labelsSource: "labels_api"                                       │       │
  │   │ metrics: [                                                       │       │
  │   │   { rule: "deployment_replicas", status: "OK", matchedCount: 816 }, │    │
  │   │   { rule: "cpu_throttle_ratio",   status: "OK", matchedCount: 340000 }, │
  │   │   ...（8 条规则逐条探测）                                        │       │
  │   │ ]                                                                │       │
  │   └──────────────────────────────────────────────────────────────────┘       │
  │                                                                              │
  │   Agent 解读：suggestedMode="cmdb_node" → 直接进入批量分析                    │
  │   ⚠️ 如果 suggestedMode="env_mismatch" → 先 count by (env) 确认再传 env       │
  │   ⚠️ 如果 FAIL → 报告写「数据源异常」，后续规则按 NO_DATA 处理                │
  │   ⚠️ 如果工具不可用 → Agent 卡住（当前已知问题）                               │
  │                                                                              │
  │   🔴 LLM 卡点：                                                                │
  │   a) suggestedMode 几种分支 Agent 必须选对——deployment_names 要传 deployments  │
  │      列表、cluster 要查 cluster 标签、env_mismatch 要先 count by env          │
  │      **任何一步走错，8 条规则全部「数据不足」，看起来像无风险**                  │
  │   b) VictoriaMetrics labels API 422 → 工具内部降级 labelsSource="series_sample" │
  │      但如果返回的 metrics.status 里有 NO_DATA，Agent 容易把"标签查询降级"      │
  │      和"指标本身缺失"搞混，后续走弯路                                            │
  │   c) 工具调用本身不可用时（如连 mock MCP 返回「mock 数据缺失」），Agent        │
  │      拿不到 suggestedMode 又被「禁止逐应用试查」约束住，**直接卡死**              │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告「数据限制」标签口径部分                                              │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 5b：批量规则判定（Prometheus 组）—— **整节点 8 条规则一次跑完**          │
  │                                                                              │
  │   调用：analyze_node_runtime_tool(                                           │
  │       node="云公司->贵州",                                                   │
  │       env="生产",                                                            │
  │       exclude_namespace=默认35项                                             │
  │   )                                                                          │
  │                                                                              │
  │   ── 工具内部数据流（关键）──                                                 │
  │                                                                              │
  │   PromQL 生成                      规则判定（arch_rules.evaluate）             │
  │   ──────────                      ─────────────────────────────               │
  │   deployment_replicas:            期望副本 < 2 → RISK / OK                   │
  │   `kube_deployment_spec_replicas  │                                          │
  │    {cmdb_node,env,depl=~.-dep}`   ├─ cpu_throttle_ratio:                     │
  │   deployment_available:           │  min_over_time(ratio[15m]) > 0.2         │
  │   `kube_deployment_status_replicas│  → RISK / OK                            │
  │    _available{...}`               │                                          │
  │   cpu_throttle_ratio:             ├─ memory_ratio:                            │
  │   `rate(cfs_throttled[5m]) /      │  min_over_time((working/limit)[15m])     │
  │    rate(cfs_periods[5m])`         │  > 0.85 → RISK / OK / NA                │
  │                                    │                                          │
  │   memory_ratio:                   ├─ hpa_replicas:                           │
  │   `container_memory_working_set   │  cur == max && load still high → RISK     │
  │    _bytes / spec_memory_limit`    │  no HPA → NA                              │
  │                                    │                                          │
  │   oom_killed:                     ├─ pod_cpu_imbalance:                      │
  │   `kube_pod_container_status_last │  max/avg CPU per RS > 2 → RISK / OK       │
  │    _terminated_reason{reason=     │  avg ≈ 0 → SKIP (NA)                     │
  │    "OOMKilled"}` + 7d 重启增量     │                                          │
  │                                    ├─ oom_killed + restart_increase:          │
  │                                    │  OOMKilled AND restart[7d]>0 → RISK      │
  │                                    │  OOM only → NO_DATA                      │
  │                                    │  restart only → OK                        │
  │                                    └──────────────────────────────────────────┘
  │                                                                              │
  │   ── 工具返回 JSON ──                                                       │
  │   ┌──────────────────────────────────────────────────────────────────┐       │
  │   │ collectionStatus: "SUCCESS"                                      │       │
  │   │ appsWithMetricData: 816                                          │       │
  │   │ offlineCount: 47                                                 │       │
  │   │ ruleReports: [                                                   │       │
  │   │   { rule: "deployment_replicas", status: "RISK",                 │       │
  │   │     riskCount: 55, offlineCount: 47, okCount: 714 },             │       │
  │   │   { rule: "cpu_throttle_ratio", status: "RISK",                  │       │
  │   │     riskCount: 1, okCount: 3360 },                               │       │
  │   │   { rule: "hpa_replicas", status: "NA" },                        │       │
  │   │   ...（共 8 条）                                                 │       │
  │   │ ]                                                                │       │
  │   │ findings: [                                                      │       │
  │   │   { rule: "cpu_throttle_ratio", target: "prod-165411-dep",       │       │
  │   │     status: "RISK", observedValue: 0.965, threshold: 0.2,         │       │
  │   │     duration: "持续15分钟3采样点全部超标",                        │       │
  │   │     evidence: ["15:16:48=96.5%","15:21:48=95.4%","15:31:48=97.9%"]│       │
  │   │   },                                                             │       │
  │   │   ...（最多 ARCH_BATCH_TOP_N=200 条，超过静默截断 ⚠️）              │       │
  │   │ ]                                                                │       │
  │   └──────────────────────────────────────────────────────────────────┘       │
  │                                                                              │
  │   ── Agent 处理 ──                                                          │
  │   1. 用 ruleReports 生成「规则判定总览表」（8 条规则 × 4 态）                │
  │   2. 从 findings 里按规则分组，生成「风险明细」章节                          │
  │   3. 对 RISK 项：按业务重要性 × 影响范围 × 证据充分程度排序                  │
  │   4. 对 offlineCount：单列，不计入风险数                                     │
  │                                                                              │
  │   ⚠️ 当前实测问题：工具链经常 FAIL，Agent 被迫用 prometheus_query_tool 手工    │
  │      写 PromQL 绕过（见报告集多份报告的「工具链故障」章节）                   │
  │                                                                              │
  │   🔴 🔴 LLM 最大卡点（4 份报告确认）：                                          │
  │                                                                              │
  │   a) **工具 FAIL 后手工写 PromQL 绕过**——这是当前最严重的问题。                 │
  │      Agent 要为 8 条规则各写一条等价 PromQL，每条要拼：                          │
  │        cmdb_node / cluster 口径 + env + 节点排除 + namespace 排除 +           │
  │        [15m:1m] 子查询 + min_over_time/max_over_time + 阈值比较                  │
  │      **贵州 1030 报告里 Agent 写了 20+ 条 PromQL 才跑完 8 条规则**，             │
  │      每条要反复试跑修正，占 token、耗时间、且很容易和规则引擎口径漂移。            │
  │      SKILL.md 明确禁止这种绕过方式（8.5），但当前工具链不可用时这是唯一选择。      │
  │                                                                              │
  │   b) **findings 大量返回时的排序**——200 条 findings（默认上限），Agent 要        │
  │      按「规则分组 × 业务重要性 × 影响范围 × 证据充分程度」排序，再挑 P0/P1 写进   │
  │      报告。**贵州 1030 报告里 Agent 花了大量篇幅把 55 个副本不足应用按模块          │
  │      拆开写清单**，实际 P0 只有 CPU 限流 1 项，其他都是 P1。                    │
  │      静默截断（超过 200 条时）→ Agent 不知道自己漏了。                            │
  │                                                                              │
  │   c) **OOM 7d 查询返回体巨大**——67 个应用 86 个 Pod 的历史 OOM 记录，加上       │
  │      restart 和 oom_events 两条 7 天窗口子查询，**单应用单规则的 JSON 可能          │
  │      到 50KB+**（之前实测 1105 条 Prometheus 序列 / 1.26MB）。Agent 解析慢且     │
  │      容易把历史 OOM（早于 7 天）误判为近期风险。                                  │
  │                                                                              │
  │   d) **内存比值的 limit 指标探测**——主集群 limit 指标缺失时 Agent 要先跑          │
  │      `count(container_spec_memory_limit_bytes{...})` 确认是不是该节点完全没有，    │
  │      再决定写「不适用」还是「数据不足」。容易写成「无风险」。                      │
  │                                                                              │
  │   e) **OOM 历史终止原因 vs 近期重启窗口**——Agent 必须理解：                      │
  │      last_terminated_reason 是状态指标（历史 OOM 会一直存在），restart 才是时间锚。│
  │      看到 49 个 OOMKilled Pod 不等于「近期 OOM」。**贵州 1027 报告里 Agent        │
  │      一开始差点把历史 OOM 判成风险**。                                           │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告「二、规则判定总览」                                                 │
  │   ▶ 报告「三、规则明细」的逐应用观测值与证据                                  │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 5c（可选）：单规则复核—— **仅在 findings 有风险/数据不足时触发**         │
  │                                                                              │
  │   调用：arch_rule_check_tool(                                                │
  │       rule="oom_killed",                                                     │
  │       node="云公司->贵州",                                                   │
  │       deployment="vcp-tree-service-201722-dep",                             │
  │       namespace="vcp-vcp-pro"  ← **必须传，否则会全量扫描**                  │
  │   )                                                                          │
  │                                                                              │
  │   ── 工具做了什么 ──                                                         │
  │   1. _resolve_pod_selector：kube_pod_owner 解析该 deployment 的真实 Pod       │
  │      → 返回 30 个 Pod 的正则                                                │
  │   2. _rule_promqls(rule="oom_killed") 生成 3 条 PromQL：                     │
  │      main:  reason="OOMKilled" 的 Pod                                       │
  │      restart: increase(restarts[7d])                                         │
  │      oom_events: increase(container_oom_events_total[7d])                   │
  │   3. _sample_promql（prometheus_client.query_range × 15 点）                 │
  │   4. scope fallback：main 无数据时去掉 cmdb_node 重查（⚠️ 当前已知问题）      │
  │   5. arch_rules.evaluate → 四态结论                                          │
  │                                                                              │
  │   ── 工具返回 ──                                                             │
  │   status="RISK" / items[] / observedValue / threshold / scopeFallback=...    │
  │                                                                              │
  │   🔴 LLM 卡点（单应用复核才会触发）：                                            │
  │   a) **scopeFallback 误判后的跨区数据**——如果带 deployment 但不带 namespace，      │
  │      Pod 正则可能匹配到跨集群同名 Pod（之前实测 67 个跨 57 集群），返回体 50KB+ │
  │      甚至带出跨区误报（如把阿曼的 Pod 当成贵州风险）。**必须传 namespace**。       │
  │   b) **restart_series + oom_events_series 各 120 条 × 15 点**——即使工具已经做     │
  │      sum by 聚合，Agent 读 items[] 时还是要过滤出真正该看的那几个。                │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告风险明细里的复核证据段                                               │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 6：链路拓扑分析（EasyViews / Pinpoint 组）                              │
  │                                                                              │
  │   调用：get_link_topology_tool(                                              │
  │       node=deployNode,   ← 用 Step 1 返回的多别名逗号串，不是 node          │
  │       projects=[modules.projectName],                                       │
  │       minute=15, caller_range=1, callee_range=1                             │
  │   )                                                                          │
  │                                                                              │
  │   ── 工具返回（EasyViews 侧）──                                              │
  │   695 节点 / 637 调用边 / 255 孤立节点                                       │
  │   inDegree Top: vnet-vpaas-media-dep(32)                                    │
  │   outDegree Top: access-prod-002254-dep(73)                                  │
  │   循环依赖: 20 个环（含 28 节点 ehome 订单环、31 节点 vnet 环 等）            │
  │   abnormalEdges: 3 条（错误率>5%）                                           │
  │                                                                              │
  │   调用：list_applications_tool（Pinpoint 侧交叉验证）                         │
  │   → 121 个应用仅 5 个完成映射，0 条调用边（Pinpoint 覆盖严重不足）             │
  │                                                                              │
  │   ── Agent 处理 ──                                                          │
  │   1. 入度>5 / 出度>5 → 集中依赖/下游过多风险                                  │
  │   2. 循环依赖 → 架构耦合风险                                                 │
  │   3. 异常调用边 → 用 Pinpoint 下钻 trace                                      │
  │   4. Pinpoint 覆盖率低 → 报告写明「链路结论以 EasyViews 为准」                │
  │                                                                              │
  │   ⚠️ 阈值 inDegree>5 / outDegree>5 等是工具默认值，方案未正式定义             │
  │                                                                              │
  │   🔴 LLM 卡点：                                                                │
  │   a) **循环依赖环的叙述整理**——贵州 1030 报告 20 个环里有 28 节点 ehome 订单  │
  │      环（ehome 订单/账号/公共/安全链路 + Kafka 互调）、31 节点 vnet 环等。       │
  │      Agent 要把 20-30 个节点名串成可读的中文句子，token 消耗极大且容易写错。        │
  │      还要区分：自环（X↔X_SPRING_BOOT 是同应用的二进制产物，不算风险） vs 真实环。 │
  │      **贵州 1030 报告里 Agent 专门写了 14 条真实跨应用环清单**。                    │
  │                                                                              │
  │   b) **Pinpoint 覆盖率极低时的交叉验证**——广州4d 121 个应用仅 5 个完成映射、      │
  │      0 条调用边。Agent 要写清楚「EasyViews 有 637 条调用边 vs Pinpoint 0 条」   │
  │      为什么差异这么大（探针覆盖率）、结论以谁为准，这个交叉验证本身就占篇幅。       │
  │      **广州4d 1102 报告里 Agent 花了一整段解释这个差异**。                        │
  │                                                                              │
  │   c) **阈值未定义时的"判据"写法**——链路入度>5、错误率>5% 等是工具默认值，       │
  │      方案没正式定义。Agent 不能直接说「入度>5 是风险」，必须先声明这个阈值      │
  │      是工具默认建议值、待业务确认，再给风险结论。**兰州4 报告里 Agent 花了 70    │
  │      行解释"为什么不适用"，就是因为阈值和应用清单都空**。                         │
  │                                                                              │
  │   d) **deployNode 多别名传错**——把单节点名代替多别名逗号串，EasyViews 返回少      │
  │      数据，但 Agent 不会察觉，因为调用是成功的。链路分析就静默不全了。             │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告「链路风险」章节                                                     │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  Step 7：异常下钻（Pinpoint 组，按需）                                        │
  │                                                                              │
  │   调用：get_server_map_tool / drag_heatmap_tool / get_transaction_trace_tool │
  │   针对：异常调用边、慢调用、Pinpoint 覆盖到的应用                              │
  │                                                                              │
  │   ── 写入报告 ──                                                             │
  │   ▶ 报告风险明细里的「慢事务 trace 片段」段                                   │
  └─────────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────────┐
  │  报告组装（Agent 逻辑，无工具）                                               │
  │                                                                              │
  │   把 Step 0-7 的产物按 SKILL.md 的 7 字段模板组织：                           │
  │                                                                              │
  │   报告首页：分析范围 / 采集窗口 / 数据覆盖率 / 主要风险                        │
  │   一、分析范围                                                               │
  │   二、八条规则判定总览                                                       │
  │   三、已发现风险明细（逐条：分析对象/状态/依据/证据/影响/建议/限制）            │
  │   四、链路依赖风险                                                           │
  │   五、数据限制与口径声明                                                     │
  │   六、优化建议汇总（按 P0-P3 排序）                                           │
  │                                                                              │
  │   ── 保存 ──                                                                 │
  │   result/贵州节点架构风险巡检报告-{时间戳}.md                                │
  └─────────────────────────────────────────────────────────────────────────────┘
```

**关键数据流转节点说明（图例）**：

| 标注 | 含义 |
|---|---|
| `⚠️` | 当前已知问题或风险点 |
| 🔴 | **LLM 处理卡点**——Agent 在这里容易出错、耗时占 token、或被迫做不该做的事 |
| 🔴 🔴 | **最大卡点**——4+ 份报告都遇到、占报告大量篇幅、甚至让 Agent 卡死的 |
| 方括号里的中文 | 工具返回的 JSON 关键字段名 |
| 斜体 | Agent 的判断逻辑，不来自工具 |
| `→` | 数据流向（不是同步时序） |

### LLM 卡点汇总排名（按严重程度）

| 排名 | 卡点 | 出现频率 | 影响 | 报告证据 |
|---|---|---|---|---|
| 🔴🔴 1 | 工具 FAIL 后手工写 PromQL 绕过 | **4/7 份报告** | Agent 要写 20+ 条 PromQL，每条反复试跑，且容易和规则引擎口径漂移 | 贵州 1027「8 条规则全部 400，改用 Prometheus MCP 手工执行」；贵州 1030「规则引擎通道故障」 |
| 🔴🔴 2 | OOM 7d 查询返回体巨大 + 历史/近期混淆 | **3/7 份报告** | 1.26MB Prometheus 响应；Agent 容易把历史 OOM 判成近期风险 | 贵州 1027「49 个 OOMKilled Pod 但 7 天内无重启 → 不能判风险」 |
| 🔴 3 | 循环依赖环的叙述整理 | **2/7 份中心节点报告** | 28 节点环 / 31 节点环，Agent 要把 20-30 个节点名串成中文 | 贵州 1030「14 条真实跨应用环（含跨节点）」 |
| 🔴 4 | EasyViews 覆盖率低 → 未归属应用要不要管 | **3/7 份报告** | 贵州 43%、广州4d 36%，158 个应用 Agent 自己判断 | 贵州 1027「158 个（mcm-*/etouch-*/sl-center-*）未纳入模块归属」 |
| 🔴 5 | 工具不可用时的卡死 | **已知问题，1 份报告实证** | Agent 拿不到 suggestedMode 又不能逐应用试查 → 卡住 | Mock MCP 场景「arch_label_probe_tool 返回 mock 数据缺失」 |
| 🔴 6 | Pinpoint 覆盖率极低时的交叉验证 | **2/7 份报告** | 广州4d 121 应用仅 5 映射 → Agent 要写清楚为什么这么低 | 广州4d 1102「Pinpoint 覆盖严重不足，链路结论以 EasyViews 为准」 |
| 🔴 7 | 灰度/边界识别模糊 | **4/7 份报告标了「过滤边界存疑」** | 多种命名模式，Agent 正则匹配容易漏 | 广州4d 1030「python-gray、sl-center-message-gray、staging/canary/beta/test」 |
| 🔴 8 | 空结果（兰州4 无 -dep 应用）的解释 | **1 份报告** | 70 行写「为什么不适用」，全是解释不是数据 | 兰州4 链路报告「0 个 -dep 应用、6 个 Pinpoint 应用全 0 事务」 |
| 🔴 9 | 阈值文档与代码漂移 | **所有 7 份报告 Agent 都提到** | SKILL 写副本>2、config 写 2；SKILL 写 OOM 30min、config 写 7d | SKILL.md vs config.py 对比 |
| 🔴 10 | offlineCount vs 风险数的边界 | **多份报告** | Agent 要明确「不计入风险数」，容易算错 | 贵州 1030「55 个范围内应用 spec<2：47 spec=0、8 spec=1」 |

**规律总结**：
- 🔴🔴 前两名都和 **工具链故障** 有关——修好 MCP 能解决一半以上
- 🔴 3/4/6 和 **数据源覆盖率** 有关——EasyViews/Pinpoint 补覆盖能缓解
- 🔴 7/9/10 和 **文档规范** 有关——SKILL.md 对齐 config.py 能减少 Agent 判断成本
- 🔴 5 是 **架构性问题**——要改 SKILL.md 的硬约束或让工具内建降级

### 10.9 完整处理过程图（省节点，以兰州4 为例）

省节点不跑运行指标 8 条规则，只做宿主机集中度：

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     省节点完整处理流程（以「云公司->兰州4」为例）            │
└─────────────────────────────────────────────────────────────────────────────┘

 Agent：判断「兰州4」是省节点 → 跑 5 步集中度流程
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  Step 1：节点清单（list_nodes_tool）                                     │
  │  node_prefix="云公司" → 拿到所有云公司节点                               │
  │  过滤：小A/国际 → 兰州4 保留                                             │
  │  关键：同时拿到 node="云公司->兰州4" 和 ctyun_node_code3="gs-lanzhou-4" │
  │  ⚠️ ctyun_node_code3 为空 → 报告「映射数据不足」                        │
  └─────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  Step 2：物理宿主机 ↔ 云主机 IP（get_host_structure_tool）               │
  │  **必须传 node_code（= ctyun_node_code3），不能传 node**                │
  │  返回 ipData = {物理宿主机IP: [云主机IP1, 云主机IP2, ...]}              │
  │  Agent 反向索引：云主机IP → 物理宿主机IP                                │
  │  ⚠️ 同一云主机映射到多宿主机 → conflictVms（迁移/重建记录未更新）         │
  └─────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  Step 3：项目 → 云主机 IP（get_project_deploy_tool）                     │
  │  传 node（不是 node_code）                                               │
  │  返回 projectIpData = {项目名: [云主机IP列表]}                          │
  │  ⚠️ EasyViews 不走 exclude_namespace，Agent 要手动过滤非业务 ns          │
  └─────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  Step 4：集中度分析（analyze_host_concentration_tool）                  │
  │  传 node + node_code，内部自动关联 Step 2/3 的数据                      │
  │  返回：                                                                  │
  │    场景分布：{ALL_IN_SAME_HOST: 5, SINGLE_INSTANCE: 12, ...}            │
  │    集中清单：[{项目名, 实例数, 集中宿主机, 宿主机分布}]                  │
  │    unmapped: [...]     ← 无法映射的云主机                                │
  │    conflictVms: [...]  ← 同一云主机多宿主机                              │
  │    coverage: 0.97      ← 映射覆盖率                                     │
  │  ⚠️ Agent 不得忽略 unmapped，必须在报告写明「可能低估风险」              │
  │                                                                              │
  │  🔴 LLM 卡点：                                                                │
  │  a) **单实例项目的业务属性判断**——114 个项目里可能大量是运维/监控/测试类，    │
  │     Agent 要靠项目名推断（如 kafka-node / nginx-ingress / prometheus-*      │
  │     → 运维，不在风险范围）。推断错了就把基础设施算成了业务风险。               │
  │     **兰州4 报告里 Agent 要对每类项目做分类说明**。                             │
  │  b) **unmapped 云主机的覆盖率描述**——如果 5% 的云主机映射不到宿主机，          │
  │     Agent 要说明"可能低估风险"，但不能算出真实覆盖度。                           │
  │  c) **ctyun_node_code3 为空时的回退**——工具会 FAIL，Agent 要么放弃，          │
  │     要么自己从 node 名或 EasyViews 其他接口推断，后者容易错。                   │
  │                                                                              │
  └─────────────────────────────────────────────────────────────────────────┘
         │
         ▼
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  Step 5：报告组装（Agent 逻辑）                                         │
  │  报告结构：                                                              │
  │    项目总数 / 宿主机总数 / 场景分类统计                                  │
  │    集中项目清单（含每台宿主机承载数量与比例）                             │
  │    单实例项目的业务属性判断（Agent 推断 → 标「待业务确认」）              │
  │    映射覆盖率 + unmapped/conflictVms 说明                                │
  │    建议：分散到不同物理宿主机 / 提升冗余                                  │
  └─────────────────────────────────────────────────────────────────────────┘
```

### 10.8 工具与报告章节的边界约定

1. **报告信息表、优化建议、影响范围推断** 不来自任何工具，是 Agent 的叙述产物——这部分的准确性靠 SKILL 的「推断需显式标注」约束，不靠工具。
2. **规则状态（四态）只能来自 MCP 判定**（`analyze_node_runtime_tool` / `arch_rule_check_tool`）；`arch_metric_query_tool` 只给原始数据，Agent 自己算的结论必须在报告中说明「非规则引擎判定」。
3. **每一个「数据不足」都必须能追溯到具体工具的 status**（`NO_DATA` / `DATA_INSUFFICIENT` / `FAIL` / `degradedReason`）——不能只写「数据不足」而不写哪一步失败。
4. **覆盖率类数字**（EasyViews 覆盖率、Pinpoint 映射率、宿主机映射率）必须同时给出分子分母，否则视为不可复核。

> ⚠️ 口径冲突提示：报告集里的《兰州4 宿主机集中度分析》在页脚写了「落盘数据：`.tool_result_spill/…json`；解析摘要：`lz4_host_conc_summary.json`」，这与 8.5 的「不得把 MCP 返回落盘后写脚本解析」相冲突。该报告是早期产物，后续应按 8.5 执行。

---

## 十一、已知口径风险（必须随报告声明）

1. **副本阈值**：方案原文「是否大于 2 个副本」对应至少 3 个副本，需与业务确认。⚠️ **注意**：SKILL.md 写的是「副本要大于 2」，但 `pinpoint-mcp/config.py` 中实际阈值是 `ARCH_RULE_MIN_REPLICAS=2`（即 < 2 算风险），文档与代码存在漂移，**以代码为准**。

2. **`spec=0` 应用**：已下线/暂停的 Deployment（副本归零）由工具记为「不适用」并单列 `offlineCount`，不计入副本风险；若出现异常缩容需人工确认，报告中**不得把 `offlineCount` 写成风险数**。

3. **指标标签（2026-10 实测）**：生产环境某节点 `kube_deployment_*` / HPA（KSM）中**仅 16 个系统组件带 `cmdb_node`**，121 个业务 `-dep` 应用无该标签（业务部署跨 2 个 K8s 集群）；而 `container_*`（cAdvisor）带 `cmdb_node`（该节点约 7.9k Pod）。因此：部署级规则须支持应用名口径，容器级规则可按节点标签查询。出现「8 条规则集体数据不足」时，先跑 `arch_label_probe_tool` 定位，再决定口径，**不要逐条试查**。

4. **deployment 归属**：`container_*` 指标天然以 pod 为维度，deployment 归属需通过 `kube_pod_owner` 关联或依赖名称前缀约定。

5. **链路阈值**：链路长度、依赖数等阈值方案未定义，需另行制定后方可判定。

6. **接口语义**：链路拓扑的 `minute`、`callerRange`、`calleeRange` 等参数语义与响应字段以联调结果为准。

7. **过滤规则执行层级**：
   - 命名空间排除（`exclude_namespace`）工具层已原生支持且默认生效（`ARCH_EXCLUDE_NAMESPACES` 35 项）
   - 集群排除（小 A 系列 / 国际节点）需智能体在 `list_nodes_tool` 返回后手动过滤，工具层暂无对应参数
   - EasyViews 类工具（`get_module_topology_tool` / `get_project_deploy_tool` / `analyze_host_concentration_tool`）不走 Prometheus 指标，排除逻辑需智能体在返回结果上执行

8. **灰度/小A 识别模糊**：节点名和 namespace 名可能缺乏统一 pattern（如灰度可能用 `staging`、`gray`、`canary`、`beta`、`test` 等多种命名），遇到边界 case 需结合 `env` 标签、发布流水线配置或业务归属交叉验证，不确定时保守排除并在报告中标记"过滤边界存疑"。

---

## 十二、上下游依赖

```
上游（数据源）
├── pinpoint-mcp (Python, 默认端口 8888)  ← 全部 arch_* 工具 + EasyViews 工具
│   ├── Prometheus                       ← KSM + cAdvisor 指标
│   ├── EasyViews                        ← 拓扑 / 部署 / 宿主机（需 APP_ID / APP_SECRET）
│   └── Pinpoint                         ← 链路与事务
└── promthus-mcp (Go, 默认端口 8080)     ← 部分 Prometheus 查询

下游（消费方）
├── Trae Agent（按 SKILL.md 编排）
├── result/*.md（本地落盘报告）
└── 报告集/、飞书等（归档 / 分发）
```

### 数据流

```
EasyViews → 拓扑/部署/宿主机数据 → arch_tools（EasyViews 工具）
                                          ↓
Prometheus → KSM/cAdvisor 指标 → arch_tools（Prometheus 工具）→ arch_rules.evaluate → 四态结论
                                          ↓
Pinpoint → 链路与事务数据 → arch_tools（链路工具）
                                          ↓
                              MCP JSON 返回 → Agent → 巡检报告
```

---

## 十三、当前已知问题

1. **前置探测是硬依赖**：`arch_label_probe_tool` 排在 `analyze_node_runtime_tool` 之前。一旦探测工具不可用（如连到 mock MCP，`arch_label_probe_tool` 返回「mock 数据缺失」），Agent 既拿不到 `suggestedMode`，又被「禁止逐应用试查」约束住，**会卡住**。修复方向：让 `analyze_node_runtime_tool` 内部自动探测口径，不依赖前置调用。

2. **文档与代码漂移**：SKILL 写「副本要大于 2」「OOM 默认近 30 分钟」，而 `pinpoint-mcp/config.py` 实际是 `ARCH_RULE_MIN_REPLICAS=2`、`ARCH_RULE_OOM_RESTART_WINDOW=7d`。以代码为准，文档需对齐。

3. **oom_killed 单规则卡慢 + 跨区误报**：`_resolve_pod_selector` 在部分节点的 `kube_pod_owner` 不带 `cmdb_node` 时，会解析出全集群同名 Pod（实测 67 个跨 57 集群），加上 scope fallback 把「应用本身无 OOM」误判为「口径不对」，导致跨集群拉数据、返回体 50KB+。修复详见 `pinpoint-mcp/性能优化建议.md` 第七章。

4. **findings 静默截断**：`analyze_node_runtime_tool` 的 findings 超过 `ARCH_BATCH_TOP_N`（默认 200）时静默截断，没有 `truncated` 标志，Agent 无法感知数据不全。

详见 `pinpoint-mcp/性能优化建议.md`。

---

## 十四、快速开始

### 14.1 环境要求

- Trae 客户端（支持 Skill）
- MCP 服务：`pinpoint-mcp`（Python）和 `promthus-mcp`（Go）正常运行
- 环境变量：
  - `EASYVIEWS_APP_ID` / `EASYVIEWS_APP_SECRET`（EasyViews 凭据）
  - `PROMETHEUS_BASE_URL`（Prometheus 地址）
  - `PINPOINT_COOKIE` 或等价认证（Pinpoint 凭据）

### 14.2 启动 MCP 服务

```bash
# pinpoint-mcp
cd pinpoint-mcp
export EASYVIEWS_APP_ID=xxx
export EASYVIEWS_APP_SECRET=xxx
export PROMETHEUS_BASE_URL=http://xxx:8082
.venv/bin/python3.13 server.py
```

### 14.3 在 Trae 中使用

1. 确认 Trae 已连接到 MCP 服务
2. 在对话中输入：「帮我给出贵州的架构风险巡检报告」
3. Agent 会自动加载本 Skill 并按 7 步流程执行

### 14.4 验证 MCP 连通性

```bash
# 测试 arch_label_probe_tool
curl -s -X POST http://127.0.0.1:8888/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"arch_label_probe_tool","arguments":{"node":"云公司->贵州"}}}'
```

---

## 十五、常见问题

**Q1：为什么调用工具后 Agent 卡住了？**

A：最常见的原因是 `arch_label_probe_tool` 不可用（连到 mock MCP 或返回错误），Agent 拿不到 `suggestedMode` 又不能逐应用试查，就卡住了。检查 MCP 服务是否正常、凭据是否配置。

**Q2：8 条规则全部返回「数据不足」怎么办？**

A：先跑 `arch_label_probe_tool` 看 `verdict.suggestedMode`：
- 如果是 `env_mismatch`，先用 `count by (env)` 确认实际取值再显式传 `env`
- 如果是 `cluster`，说明该节点指标不带 `cmdb_node`，需要用 `cluster` 口径
- 如果是 `deployment_names`，传 `deployments=[应用清单]` 让工具按应用名兜底

**Q3：报告里的阈值和 SKILL.md 写的不一样？**

A：阈值以 `pinpoint-mcp/config.py` 为准。SKILL.md 和 config.py 存在漂移（如副本阈值、OOM 窗口），已在已知问题里标注。

**Q4：同一应用出现在多个模块里怎么办？**

A：保留全部归属关系。按「节点 + 应用 + 命名空间」去重后，关联回所有所属模块。

**Q5：`deployNode` 可以用 `node` 替换吗？**

A：不可以。`deployNode` 可能含多个节点别名（逗号分隔），用于链路拓扑；`node` 是单一节点名，用于拓扑和部署。混用会导致链路查询数据不全。

**Q6：省节点巡检需要跑 8 条规则吗？**

A：不需要。省节点只做宿主机集中度分析（`analyze_host_concentration_tool`），不跑运行指标 8 条规则。

---

## 版本与维护

- Skill 规范版本：见 `SKILL.md` frontmatter
- 规则版本：`ARCH_RULE_VERSION`（`pinpoint-mcp/config.py`）
- 方案文档：`架构优化/架构优化智能体实现方案-V2.md`
- 问题记录：`架构优化/问题记录.md`
- 性能优化建议：`pinpoint-mcp/性能优化建议.md`
