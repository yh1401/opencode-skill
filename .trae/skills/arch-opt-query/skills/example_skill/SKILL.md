---
name: "架构优化【运维】"
description: "按《架构优化智能体实现方案-V2》执行架构风险分析：中心节点副本/容量/负载/OOM/链路依赖，省节点宿主机集中度。生产环境巡检自动排除小A系列/国际集群及系统组件/operator/监控/灰度命名空间，仅分析正常业务命名空间的 -dep 应用。当用户要求架构优化分析、风险巡检、故障域排查时调用。"
category: 开发工具
---

# 架构优化分析技能

面向中心节点与省节点，结合业务拓扑、运行指标、调用链及宿主机关系，识别可用性、容量、稳定性和故障域集中风险，输出有证据支撑的优化建议。

**核心原则**：规则引擎（MCP 工具）负责取数、关联与数值计算；大模型负责解释风险、串联业务影响、组织报告。**缺失指标与未确认阈值必须明确说明信息不足，不得补造数据。** **生产巡检默认只分析正常业务命名空间的业务应用**，系统组件、灰度、海外集群一律排除（详见第一节过滤口径）。

## 一、分析范围（固定口径 · 生产环境）

### 生产节点

| 分析对象 | 节点标识                                             | 分析重点                     |
| ---- | ------------------------------------------------ | ------------------------ |
| 中心节点 | `云公司->贵州`（6 模块）、`云公司->广州4d`（7 模块）                | 副本可用性、容量瓶颈、负载分布、OOM、服务依赖 |
| 省节点  | `list_nodes_tool` 中前缀为「云公司」的节点（**需按下方集群排除规则过滤**） | 项目多实例是否集中于同一物理宿主机        |

中心节点业务模块：核心业务 API、设备信令调度、直播服务网关、设备告警、云眼核心业务、设备云存上传（苏州）、消息中心 4.0。

### 集群排除规则（生产巡检硬约束）

以下两类集群**不纳入巡检范围**，在 `list_nodes_tool` 返回后立即过滤，后续所有工具均不得对其发起查询：

| 排除类型     | 识别方式                                                             | 示例                                   |
| -------- | ---------------------------------------------------------------- | ------------------------------------ |
| 小 A 系列集群 | 节点名含 `小A` 或 `a-series` / `a_series`（大小写不敏感）                      | `云公司->小A海口`、`云公司->a_series_shenzhen` |
| 国际节点集群   | 节点名含 `海外` / `国际` / `global` / `i18n` 或 `cmdb_node` 标签指向境外 region | `云公司->海外新加坡`、`云公司->global-us`        |

**判断优先级**：先按节点名 pattern 过滤；节点名无明确标识时，可调用 `get_module_topology_tool` 或 `prometheus_query_tool` 看 `cmdb_node` / `region` 标签辅助判别。

### 命名空间排除规则（生产巡检硬约束）

拿到节点的应用/项目列表后，**必须排除以下命名空间**，仅保留正常业务命名空间的应用：

| 排除类型         | 识别方式                              | 典型 namespace                                                              |
| ------------ | --------------------------------- | ------------------------------------------------------------------------- |
| 系统组件         | k8s 原生系统 ns                       | `kube-system`、`kube-public`、`kube-node-lease`、`default`                   |
| Operator     | 以 `-operator` 或 `operator-` 结尾/开头 | `prometheus-operator`、`istio-operator`、`cert-manager-operator`            |
| 监控可观测        | 监控、日志、追踪组件                        | `monitoring`、`prometheus`、`grafana`、`loki`、`tempo`、`efk`、`elastic-system` |
| Ingress / 网关 | 流量入口组件                            | `ingress-nginx`、`istio-system`、`traefik`、`apigw`                          |
| 存储           | 持久化存储组件                           | `rook-ceph`、`openebs`、`storage`、`nfs`                                     |
| 灰度 / canary  | 灰度发布专用 ns                         | `*-gray`、`*-canary`、`gray-*`、`canary-*`、`staging`                         |
| CI/CD        | 流水线组件                             | `jenkins`、`gitlab-runner`、`argocd`、`tekton`                               |
| 安全           | 安全扫描/策略                           | `kube-bench`、`trivy`、`opa`、`cert-manager`                                 |
| 其他基础设施       | 非业务平台组件                           | `velero`、`harbor`、`registry`、`etcd`、`calico-system`、`cattle-system`       |

### 工具调用约束（工具层已原生支持命名空间排除）

工具层已提供 `exclude_namespace` 参数，**默认按服务端 ARCH\_EXCLUDE\_NAMESPACES 配置自动排除非业务命名空间**（35 项，含系统组件 / operator / 监控 / ingress / 存储 / 灰度等），一般不需要显式传。四种传参方式：

| 场景            | exclude\_namespace 传值                                              | 效果                  |
| ------------- | ------------------------------------------------------------------ | ------------------- |
| 普通巡检（推荐）      | 不传（默认）                                                             | 自动排除系统/灰度 ns，只拿业务应用 |
| 需要额外排除        | `"kube-system,monitoring,gray-*"` 或 `["kube-system","monitoring"]` | 在默认清单基础上追加          |
| 只想排除特定项（覆盖默认） | 显式传完整清单字符串或 list                                                   | 用指定清单替换默认值          |
| 需要全量（含系统 ns）  | `""` 或 `"none"`                                                    | 禁用排除，拿全节点所有 ns      |

**优先级**：显式指定 `namespace="xxx"` 精确值时，`exclude_namespace` 不生效（精确 ns 比排除清单优先级高）。

**工具适配清单**（已原生支持 exclude\_namespace）：

| 工具                                | exclude\_namespace 生效方式                                                     |
| --------------------------------- | --------------------------------------------------------------------------- |
| `arch_metric_query_tool`          | PromQL 自动拼 `namespace!~"(...)"`                                             |
| `arch_rule_check_tool`            | 同上（单规则判定）                                                                   |
| `analyze_node_runtime_tool`       | 同上（批量 8 条规则）                                                                |
| `prometheus_query_tool`           | 自动在第一个 PromQL 选择器里拼排除；已有 `namespace="xxx"` 精确值时不自动加；无任何选择器时返回 warning 提示手动加 |
| `get_module_topology_tool`        | 不支持（EasyViews 接口，需在返回后人工过滤）                                                 |
| `get_project_deploy_tool`         | 不支持（EasyViews 接口）                                                           |
| `analyze_host_concentration_tool` | 不支持（EasyViews 接口）                                                           |

### 应用二次筛选（与命名空间排除叠加）

命名空间过滤后，**仍需对应用名按** **`-dep`** **后缀二次筛选**（SKILL 原有规则），确保最终纳入分析的同时满足：

1. 在业务命名空间内
2. 应用名以 `-dep` 结尾（Deployment 身份确认）

### 取数口径探测与工具调用预算（防卡死硬约束）

**每个节点巡检前先调用一次** **`arch_label_probe_tool(node=…)`**，按 `verdict.suggestedMode` 选择取数口径，不要跳过直接跑规则：

| suggestedMode      | 含义与实测场景                                   | 后续做法                                                                                                                                   |
| ------------------ | ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `cmdb_node`        | 部署级与容器级指标标签一致                             | 正常按 `node` 查询，直接进入批量分析                                                                                                                 |
| `deployment_names` | 部署级（KSM）缺 `cmdb_node`、容器级（cAdvisor）有该标签   | 先取应用清单（`get_module_topology_tool` / `get_project_deploy_tool`），再调 `analyze_node_runtime_tool(node=…, deployments=[…])`，工具会自动按应用名口径兜底   |
| `cluster`          | 两类指标均未命中节点标签，或该节点固定按 cluster 取数           | 用 `prometheus_query_tool` 以 `cluster` 选择器查询，或按应用名直查；若两者都为空 → 直接记「采集链路缺失」，**不进入逐应用试查**。已配置映射的节点（广州4d → `cluster="广州4d生产"`）工具会自动应用，无需手工传 |
| `env_mismatch`     | `cmdb_node`/`cluster` 有序列，但 `env` 过滤命中为 0 | 说明 `env` 标签取值已变更：先用 `count by (env) (kube_deployment_spec_replicas{deployment=~".+-dep"})` 确认实际取值，再显式传 `env` 参数；**未确认前不得继续批量**         |

**单轮取数（硬约束）**：探测完成后只允许调用 **一次** `analyze_node_runtime_tool`：

| 探测结论 | 唯一允许的取数序列 |
| ---- | ------------------------------------ |
| `cmdb_node` / `cluster` | `arch_label_probe_tool` → `analyze_node_runtime_tool(node=…)` |
| `deployment_names` | `arch_label_probe_tool` → 取应用清单 → `analyze_node_runtime_tool(node=…, deployments=[…])` |
| `env_mismatch` | 先确认 `env` 实际取值 → 再按上两行之一执行 |

**禁止**先用节点口径跑一次「试跑批量」再按应用清单跑第二次。探测试跑得到的计数与 risk 若已进入上下文，只能作为最终结论引用，不得重复判定或重跑。同一次巡检内 `arch_label_probe_tool` 结果复用，同一节点不重复探测。

**节点标签口径**：默认按 `cmdb_node` 取数；广州4d 的业务指标不带该标签，固定改用 `cluster="广州4d生产"`（服务端 `ARCH_NODE_CLUSTER_MAP`），规则查询与探测均自动应用。手工写 PromQL 时也不要用 `cmdb_node="云公司->广州4d"`。

**工具调用预算（每个节点）**：`list_nodes_tool` 1 次；`arch_label_probe_tool` 1 次；`analyze_node_runtime_tool` 1 次（覆盖全部 8 条规则，禁止试跑）；单应用复核仅在 `findings` 出现风险时按需调用，且同一应用最多 1 次。预算用尽仍无结论时，直接在报告记「数据不足」并写明缺失的指标/标签，**不得继续试查**。

**多节点巡检（优先路径）**：节点数 ≥2 时先 `arch_task_open_tool(task_id, nodes=[…])` 注册白名单，再用 `analyze_nodes_runtime_tool(task_id, nodes=[…])` **一次调用**完成全部节点（服务端并发，默认每轮 6 节点）；返回只含每节点计数与 top3 风险，需要明细时再对单个节点 `analyze_node_runtime_tool(detail=…)` 或 `arch_rule_check_tool` 下钻。禁止按节点拆成多次 `analyze_node_runtime_tool` 调用。节点数 ≥6、或单轮上下文过大 / 模型思考过慢时，改走第十节「多节点分批巡检（子 Agent 并行）」，分批仍必须走 `analyze_nodes_runtime_tool` 或按节内固定分工执行。

**探测结果解读**：`labelsSource=series_sample` 是正常降级（VictoriaMetrics 不支持 `labels?match[]`），已在工具内自动完成，无需换工具重试；`metrics[].status=NO_DATA` 表示该指标没有任何序列（采集未接入），不是探测失败，也不是标签口径问题；只有 `status=FAIL`（两条标签路径都不可用）才需要在报告写明数据源异常。`verdict.suggestedMode=env_mismatch`（`envMatched=false`）时，先确认 `env` 实际取值并显式传参，**不得跳过继续批量**——否则 8 条规则会全部返回「数据不足」，看起来像无风险。

**禁止事项**：

- 不得为绕开标签缺口，逐规则、逐应用反复调用 `arch_metric_query_tool` 试查
- 不得把 MCP 返回落盘后写 Python 脚本解析中间结果；采样已按 series 聚合，直接读返回的 `summary` / `findings` / `verification` / `verdict`（例外：第八节副本类规则对账，平台修复验证前强制）
- 不得用 Python「同规格重算」工具已经给出的阈值判定（`riskCount` / `observedValue` / `validPoints` 都是最终值）
- 不得把「标签缺失导致的空结果」写成「未触发规则 / 无风险」
- 不得为「保险」而对全量 121 应用重新取一遍数做复核；复核只允许定向验证报告将引用的结论
- 不得每次巡检重写报告生成脚本；已有渲染脚本只替换数据源

### 执行效率约束（巡检硬性要求）

| 约束 | 具体要求 |
| ---- | ---- |
| 单轮取数 | 探测 → 取清单（如需）→ 一次批量判定；禁止试跑批量后重跑 |
| 直读结果 | 工具返回的 `summary` / `ruleCount` / `findings` / `verification` 即判定结论，直接引用；不得落盘后二次计算指标、阈值或持续时间（例外：第八节副本类对账） |
| 脚本用途 | Python 只允许做一件事：把工具 JSON 渲染成报告；不得解析 MCP 返回、二次判定、拼接证据（例外：第八节副本类对账） |
| 定向复核 | 复核优先用返回体已有字段：`verification.offlineApps`（spec=0 清单）、`verification.minValidPointRatio`（采样覆盖率）、`rules[].seriesCount`（序列/Pod 量）、`scopeFallback`（取数口径）。缺口字段才补查，且每个缺口最多 1 次 |
| 报告装配 | 复用 `references/gen_report.py` 的渲染结构（表头字段、四态统计、优先级分组），只替换数据源；禁止重新设计报告结构 |
| 超时熔断 | 同一工具同一目标连续 2 次超时后停止重试；最多允许 1 次降级（缩范围或换工具），仍失败记「数据不足」并进入下一环节 |
| 上下文控制 | 工具已支持 `detail=summary` / `top_n` / `findings` 截断，优先用摘要模式；需要明细时按应用单查，不拉全量 |

## 二、工具映射

| 方案章节        | 工具                                                                         | 说明                                                                   |
| ----------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| 1.1 模块与应用归属 | `get_module_topology_tool(node=…)`                                         | 返回模块、`projectName`、`deployNode`                                      |
| 1.2 运行指标    | `arch_metric_query_tool(rule=…, node=…)`                                   | 按规则名自动生成 PromQL 并多点采样                                                |
| 1.2 口径探测    | `arch_label_probe_tool(node=…)`                                            | 巡检前置：确认各指标族是否带 `cmdb_node`                                           |
| 1.2 批量判定    | `analyze_node_runtime_tool(node=…, deployments=[…])`                       | 一次跑完 8 条规则；标签缺失时自动按应用名兜底                                             |
| 1.2 多节点批量   | `analyze_nodes_runtime_tool(task_id, nodes=[…])`                           | **优先**：服务端并发处理多节点，每节点只回摘要，1 轮对话完成 23 节点                              |
| 任务作用域       | `arch_task_open_tool(task_id, nodes=[…])` / `arch_task_status_tool`        | 父会话注册节点白名单，子任务 Worker 用同一 `task_id` 继承工具权限 |
| 分批巡检       | `delegate_tasks` / `spawn_task_worker` + `analyze_nodes_runtime_tool` | 子 Agent 并行分批，每 Worker 只回压缩摘要，见第十节                           |
| 1.2 自定义指标   | `prometheus_query_tool(query=…)`                                           | 任意 PromQL，即时查询支持历史时间点                                                |
| 1.3 服务链路    | `get_link_topology_tool(node=deployNode, projects=[…])`                    | 保留原始节点、调用边与统计字段                                                      |
| 1.3 链路下钻    | `get_server_map_tool` / `drag_heatmap_tool` / `get_transaction_trace_tool` | Pinpoint：拓扑、失败事务、调用树                                                 |
| 2.2 节点列表    | `list_nodes_tool()`                                                        | 取 `node` 与 `ctyun_node_code3`                                        |
| 2.3 物理机-云主机 | `get_host_structure_tool(node_code=…)`                                     | 传 `ctyun_node_code3`，返回反向索引 `vmToHost`                               |
| 2.4 项目部署    | `get_project_deploy_tool(node=…)`                                          | 传节点名称，取 `projectIpData`                                              |
| 2.5 宿主机集中度  | `analyze_host_concentration_tool(node=…, node_code=…)`                     | 关联 2.3+2.4，输出集中清单；默认 `detail=summary`（约 8\~12KB），逐项证据传 `detail=full` |

### 标识不可混用（方案 2.1 硬约束）

| 标识                                  | 来源                                    | 用于             |
| ----------------------------------- | ------------------------------------- | -------------- |
| `node`（如 `云公司->海口9`）                | `list_nodes_tool.node`                | 项目部署、模块拓扑、指标标签 |
| `ctyun_node_code3`（如 `hi-haikou-9`） | `list_nodes_tool.ctyun_node_code3`    | 宿主机关系、集中度分析    |
| `deployNode`（可能含多别名逗号串）             | `get_module_topology_tool.deployNode` | 链路拓扑           |

`ctyun_node_code3` 为空 → 记「映射数据不足」；`deployNode` **不得**替换为单一节点名称。

## 三、中心节点流程

1. **模块归属**：`get_module_topology_tool(node="云公司->贵州")` → 提取各对象 `projectName`、`deployNode`、`namespace`
2. **命名空间过滤**：按第一节命名空间排除规则过滤返回的模块，排除项记为「非业务命名空间，已排除」
3. **应用筛选**：**仅对名称以** **`-dep`** **结尾的应用**执行容器检查，其余记为「本阶段未覆盖」。该命名规则为初步筛选，Deployment 身份仍需通过监控或资源信息确认
4. **去重与关联**：按节点 + 应用 + 命名空间去重查询，结果关联回所属模块；同一应用属于多模块时保留全部归属关系。应用名可能跨集群/命名空间重复，需结合监控标签补齐唯一标识
5. **口径探测与指标检查**：先 `arch_label_probe_tool(node=…)` 确认标签口径；多节点巡检用 `analyze_nodes_runtime_tool(task_id, nodes=[…])` 一次跑完所有节点的 8 条规则；**单节点只跑一次** `analyze_node_runtime_tool`，`suggestedMode=deployment_names` 时直接带 `deployments=[应用清单]`，禁止先用 node 口径试跑；**单应用复核时** **`arch_rule_check_tool`** **必须传** **`namespace`**，避免全量扫描，同一应用最多复核 1 次
6. **链路分析**：`get_link_topology_tool(node=deployNode, projects=[模块 projectName])`；超时最多重试 1 次并缩到核心业务模块（≤30 应用），连续 2 次超时按「数据不足」记录，**不得改用 PromQL 逐应用重算**替代链路拓扑
7. **异常下钻**：对异常调用边用 Pinpoint 工具定位具体慢/失败事务

指标标签：`cmdb_node`（节点名）、`env="生产"`、`deployment`（应用名）、`namespace`（命名空间，用于消歧）。**注意** **`cmdb_node`** **在 KSM 部署级指标上可能缺失（见第八节实测），此时以应用名口径为准。**

## 四、规则与阈值

| 风险类型      | rule                              | 判定依据                                            | 证据与注意事项                                                                    |
| --------- | --------------------------------- | ----------------------------------------------- | -------------------------------------------------------------------------- |
| 副本不足      | `deployment_replicas`             | 期望副本未达冗余要求（**副本要大于 2**）                         | 期望副本、可用副本、节点、应用。⚠️ 口径待确认：如业务要求「至少 2 个副本」需另行确认                              |
| 副本不可用     | `deployment_available`            | 可用副本低于期望副本                                      | 差值、持续时间；关联发布、扩缩容和故障事件                                                      |
| CPU 限流    | `cpu_throttle_ratio`              | 限流周期比例 **> 20%，持续 15 分钟**                       | 同容器同窗口比值；**总周期为零时不计算**；需交叉验证应用侧耗时（P99），仅比例超标而延迟无变化时优先级可降低                  |
| 内存容量      | `memory_ratio`                    | 工作集 / 有效 limit **> 85%，持续 15 分钟**               | 明确容器范围与有效 limit；limit 缺失、为零或不适用时单独记录                                       |
| 扩容容量      | `hpa_replicas`                    | HPA 达最大副本（比值 ≥ 1），且资源或业务负载仍持续偏高                 | HPA 当前/最大副本、目标指标与实际负载；**无 HPA 的应用标记「不适用」**                                 |
| Pod 负载不均衡 | `pod_cpu_imbalance`               | 同规格 Pod 最大 / 平均 CPU **> 2，持续 15 分钟**            | 统一采样窗口与 CPU 口径，排除不同规格与启动阶段；**平均值接近零时跳过**避免比值失真                             |
| OOM       | `oom_killed` + `restart_increase` | 近期出现 OOMKilled **且**存在对应重启证据（默认近 30 分钟重启增长 > 0） | 容器终止原因、重启增量、时间窗口；**历史终止原因不能单独证明近期 OOM**。出现 OOM 应回查 1.2.2 内存使用率是否长期贴近 limit |
| 链路依赖      | —                                 | 集中依赖、复杂依赖、长链路、异常调用、循环依赖                         | 调用边及统计窗口、请求量、错误率与耗时；**链路长度、依赖数等阈值需另行制定**                                   |
| 宿主机集中     | `analyze_host_concentration_tool` | 同一项目多个云主机实例落在同一物理宿主机                            | 项目实例列表、宿主机映射、覆盖率及业务角色                                                      |

### 持续时间窗口的取证方式

涉及「持续 15 分钟」的规则，必须取完整时间窗口证据。工具内部用**单次 `query_range` 取回全部采样点**（默认 15 点 × 60 秒，服务端按 step 切片），**不得用单次即时值证明持续触发**，也不需要模型侧再做多点即时查询；部分场景可用经验证的 PromQL 时间窗口表达式替代。

## 五、省节点流程

1. `list_nodes_tool()` → 筛选「云公司」前缀节点，**再按集群排除规则过滤小 A 系列和国际集群**，取 `node` 与 `ctyun_node_code3`
2. `get_host_structure_tool(node_code=…)` → 取 `ipData`（键=物理宿主机 IP，值=其上云主机 IP 列表），反向索引为「云主机 IP → 物理宿主机 IP」
3. `get_project_deploy_tool(node=…)` → 以 `projectIpData`（项目 → 云主机 IP 列表）为主关联第 2 步；**项目名不含** **`-dep`** **且命名空间为系统 ns 的排除**
4. `analyze_host_concentration_tool(node=…, node_code=…)` → 直接输出集中清单
5. 建立「项目 → 云主机 → 物理宿主机」映射，输出风险项目、涉及实例、宿主机分布与调整建议

### 集中度判定（方案 2.5）

建议记录两个统计值：**宿主机数**、**最大宿主机集中比例**（单一宿主机承载的该项目云主机数最大值 ÷ 该项目云主机总数）。

| 场景            | 判定与处理                     |
| ------------- | ------------------------- |
| 仅部署 1 台云主机    | 记录单实例部署；是否构成风险由业务冗余要求决定   |
| 多台全部位于同一宿主机   | 明确标记共同故障点，建议分散到不同物理宿主机    |
| 分布于多宿主机但部分集中  | 输出每台宿主机承载数量与比例，评估单宿主机故障影响 |
| 部分云主机无法映射宿主机  | 标记数据不足，输出已匹配与未匹配清单，避免低估风险 |
| 同一云主机映射到多个宿主机 | 标记映射冲突，核查数据更新时间及迁移记录      |

**映射不完整时，仅输出已知分布与覆盖率，不据此作完整故障域结论。**

## 六、状态口径

所有结果须区分四种状态，**接口失败、监控缺失或关联不完整时不得给出无风险结论**：

| 状态    | 含义                 |
| ----- | ------------------ |
| 已发现风险 | 规则触发，证据充分          |
| 未触发规则 | 规则已执行，未达阈值         |
| 数据不足  | 指标缺失、映射不完整、采样窗口不足  |
| 不适用   | 如无 HPA 的应用之于扩容容量规则 |

工具返回的 `status` 字段（`SUCCESS` / `NO_DATA` / `DATA_INSUFFICIENT` / `FAIL`）是判定依据之一。

## 七、报告输出

按「节点 → 模块 / 项目 → 应用 / 实例」组织，每条风险至少包含：

| 字段      | 内容要求                          |
| ------- | ----------------------------- |
| 分析对象    | 节点、模块、应用或项目及唯一标识              |
| 风险类型与状态 | 风险分类，以及已发现风险、未触发、数据不足或不适用     |
| 判定依据    | 触发规则、阈值、实际观测值、持续时间            |
| 证据      | 指标查询结果、调用拓扑、云主机与宿主机映射及采集时间    |
| 影响范围    | 涉及实例、上下游服务、所属核心业务；**推断需显式标注** |
| 优化建议    | 与风险对应的处理建议、前置条件和建议优先级         |
| 数据限制    | 指标缺失、映射不完整、采样窗口不足或口径待确认等      |

报告首页展示分析范围、采集窗口、数据覆盖率与主要风险。**优先级结合业务重要性、影响范围和证据充分程度确定，避免仅凭单一指标大小排序。**

### 报告装配流程（固定，不得每次重设计）

1. 数据源只用一次批量判定的返回值：`scope` / `rules[]` / `findings` / `moduleSummary` / `verification` / `dataLimitations`（副本类规则以第八节对账输出为准）
2. 渲染复用 `references/gen_report.py` 的结构（首页信息表、模块汇总、风险清单、数据限制、附录），只替换数据源文件与少量标题字段
3. 覆盖率、离线应用数、序列数直接取 `verification` 字段，不再重新查询
4. 落盘后只对「修正过的行」做 diff 校验，不整篇读回重写

每次分析记录：任务标识、采集时间、分析窗口、目标节点、规则版本。

## 八、批量数据对账（平台修复验证前强制）

**背景（2026-11-06 贵州节点事故）**：`analyze_node_runtime_tool` 批量路径存在序列串配缺陷——同命名空间两个 Deployment 除 `deployment` 标签外完全同标签（同集群、同 KSM 实例）时，副本类规则输出发生串配（初版报告 40 项副本风险全部误报，全标签直查证实 0 缺口）；内存规则疑似同族缺陷（报 2 项 / 实测 6 应用）。单应用 `arch_rule_check_tool` 使用精确 `deployment="X"` 过滤，不受影响。**平台侧修复通过回归验证前，批量工具副本类规则输出不得直接作为风险结论依据，必须执行以下对账。**

### 副本类规则（deployment_replicas / deployment_available）：强制全量对账

1. 执行两条节点级精确查询，`by` 子句**必须保留 `deployment` / `cluster` / `env`**（序列唯一身份，禁止子集键）：

   ```promql
   sum by (namespace, deployment, cluster, env) (kube_deployment_spec_replicas{<节点选择器>, env="生产"})
   sum by (namespace, deployment, cluster, env) (kube_deployment_status_replicas_available{<节点选择器>, env="生产"})
   ```

   节点选择器：`cmdb_node=~"云公司->贵州|云公司->广州4d"`；已配置集群映射的节点（如广州4d）改用 `cluster` 标签（`cluster="广州4d生产"`）。
2. 运行 `references/replica_reconcile.py <spec_spill文件> <avail_spill文件> <scope.json>`（scope = 第一节定出的 -dep 应用清单；spill 文件为工具超长返回的落盘文件）：
   - `collisions > 0` → 聚合口径错误，本轮结果作废重查；
   - `only_spec / only_avail > 0` → 记入报告「数据限制」清单，不得静默丢弃；
   - 对账输出（真实缺口 / 单副本 / 下线 / 单侧序列）是副本类规则的**权威口径**；批量工具副本 findings 仅作交叉参考。
3. **例外条款**：本对账是第一节「禁止事项」中「MCP 返回落盘 + Python 解析 / 二次判定」禁止项的唯一例外，仅适用于副本类规则，不得推广到其他规则或其他用途。

### 其他规则（memory / OOM / restart / CPU / HPA / imbalance）：定向复核

- 批量 findings 中的每个 RISK 项，用 `arch_rule_check_tool`（精确 deployment + namespace）或 PromQL 直查逐条复核；复核不复现的项标记「待复核」，不得写入报告作为风险。
- RISK 项超过 30 个时，改用全量直查对账（PromQL 直查 + 本地 join），join 口径写入报告数据质量声明。

### 数据质量声明（报告首页必备）

1. 规则版本与阈值（`list_arch_rules_tool`）、采集窗口、节点 / 集群口径；
2. 对账情况：哪些规则经何种方式复核（全量直查 / 单应用接口），与批量输出的差异（如「初版批量副本风险 40 项经全键对账全部证实误报」）；
3. 数据限制：无序列应用、规格指标缺失、链路映射覆盖率等；
4. 未经对账 / 复核的批量工具输出，不得写入报告作为风险结论。

### 退出条件（满足后可降级）

① 回归夹具通过：同命名空间两部署副本数 80 / 2，乱序输入，无串配；② 连续两轮巡检批量结果与全键对账直查一致。满足后副本类规则可降级为「10% 抽样对账」，本节保留为附录。

## 九、已知口径风险（必须随报告声明）

- **批量工具副本串配（2026-11-06 坐实）**：`analyze_node_runtime_tool` 批量路径同命名空间序列串配（两部署除 `deployment` 外标签全同），副本类规则输出不可信（贵州初版 40 项误报全部证实）；内存规则疑似同族（报 2 项 / 实测 6 应用）。强制对账路径见第八节；`arch_rule_check_tool`（精确 deployment 过滤）不受影响
- **副本阈值**：方案原文「是否大于 2 个副本」对应至少 3 个副本，需与业务确认
- **`spec=0`** **应用**：已下线/暂停的 Deployment（副本归零）由工具记为「不适用」并单列 `offlineCount`，不计入副本风险；清单直接取 `verification.offlineApps`，不要为"确认 0 副本应用"再发一次查询。若出现异常缩容需人工确认，报告中不得把 `offlineCount` 写成风险数
- **指标标签（2026-10 实测）**：生产环境某节点 `kube_deployment_*` / HPA（KSM）中**仅 16 个系统组件带** **`cmdb_node`**，121 个业务 `-dep` 应用无该标签（业务部署跨 2 个 K8s 集群）；而 `container_*`（cAdvisor）带 `cmdb_node`（该节点约 7.9k Pod）。因此：部署级规则须支持应用名口径，容器级规则可按节点标签查询。出现「8 条规则集体数据不足」时，先跑 `arch_label_probe_tool` 定位，再决定口径，**不要逐条试查**
- **deployment 归属**：`container_*` 指标天然以 pod 为维度，deployment 归属需通过 `kube_pod_owner` 关联或依赖名称前缀约定
- **链路阈值**：链路长度、依赖数等阈值方案未定义，需另行制定后方可判定
- **接口语义**：链路拓扑的 `minute`、`callerRange`、`calleeRange` 等参数语义与响应字段以联调结果为准
- **过滤规则执行层级**：命名空间排除（`exclude_namespace`）工具层已原生支持且默认生效（`ARCH_EXCLUDE_NAMESPACES` 35 项）；集群排除（小 A 系列 / 国际节点）需智能体在 `list_nodes_tool` 返回后手动过滤，工具层暂无对应参数；EasyViews 类工具（`get_module_topology_tool` / `get_project_deploy_tool` / `analyze_host_concentration_tool`）不走 Prometheus 指标，排除逻辑需智能体在返回结果上执行
- **灰度/小A识别模糊**：节点名和 namespace 名可能缺乏统一 pattern（如灰度可能用 `staging`、`gray`、`canary`、`beta`、`test` 等多种命名），遇到边界 case 需结合 `env` 标签、发布流水线配置或业务归属交叉验证，不确定时保守排除并在报告中标记"过滤边界存疑"

## 十、多节点分批巡检（子 Agent 并行）

**触发条件**：本次巡检节点总数 ≥6，或单轮巡检上下文过大 / 模型思考过慢。2~5 个节点直接用一次 `analyze_nodes_runtime_tool` 即可，不必分批。

**原则**：每个子 Agent Worker 独立上下文、只装自己那批的数据；父会话只收压缩摘要；最终报告由父会话统一装配，Worker 不出最终报告。

### 10.1 固定流程

1. **父会话注册作用域**（仅一次）：
   - `arch_task_open_tool(task_id="arch-patrol-<YYYYMMDD>-<序号>", nodes=[本次全部节点], ttl_minutes=120)`
   - `ttl_minutes` 须覆盖整个巡检周期（默认 30 分钟不够）；`arch_task_status_tool` 查剩余有效期，临期用同一 task_id 重新注册续期
2. **父会话并行创建 Worker**：`delegate_tasks`，分批粒度固定如下（省节点清单由父会话先 `list_nodes_tool` 并按第一节集群排除规则过滤后传入，含 `node` 与 `ctyun_node_code3` 对）：

   | Worker | 对象 | 取数路径 | 输出文件（当前工作目录下） |
   | ---- | ---- | -------- | ------------------------ |
   | 1 | 中心节点 `云公司->贵州` | 探测 → `analyze_node_runtime_tool`（一次跑 8 规则）→ 第八节副本对账 | `reports/center-guizhou.md` |
   | 2 | 中心节点 `云公司->广州4d` | 同上 | `reports/center-guangzhou4d.md` |
   | 3 | 省节点前半（≤12 个） | 每节点 `analyze_host_concentration_tool(node=…, node_code=…, detail=summary)` | `reports/province-1.md` |
   | 4 | 省节点后半（≤12 个） | 同上 | `reports/province-2.md` |

   省节点过滤后按清单顺序切两批、每批 ≤12；节点少则相应减少批数。若巡检要求明确包含对省节点跑 8 条运行规则，对应 Worker 改为一次 `analyze_nodes_runtime_tool(task_id, nodes=该批)`（禁止拆成逐节点调用），再补宿主机集中度。
3. **Worker 任务模板**（原样下发，仅替换目标节点与文件名）：
   > 你是巡检任务 task_id=… 的分批 Worker，目标节点：[…]。
   > ① 若本批含 8 条规则运行判定：每节点先 `arch_label_probe_tool` 一次、按口径取数，禁止逐应用试查；宿主机集中度路径无需探测；
   > ② 批量判定严格按上述取数路径执行；RISK 项仅允许单应用定向复核（`arch_rule_check_tool` 必须带 `namespace`，同一应用 ≤1 次）；
   > ③ 把本批结论写入 `reports/<文件名>.md`：分析范围、四态统计、风险清单（含判定依据与证据）、数据限制，并声明 task_id / 采集时间 / 分析窗口 / 规则版本；
   > ④ 完成后只回 3~5 行摘要（风险数、top 风险、数据限制）。
   > 禁止拉全量明细，禁止读其他批次的文件，禁止装配最终报告。
4. **父会话装配**：收齐各 Worker 摘要 → 只读 `reports/` 各文件的摘要节 → 统一装配最终报告（复用 `references/gen_report.py` 结构）→ 按第八节写数据质量声明，对账结论以中心节点文件为准。

### 10.2 关键约束

| 约束 | 要求 |
| ---- | ---- |
| 分批路径 | 多节点批量判定必须走 `analyze_nodes_runtime_tool`；禁止按节点拆成多次 `analyze_node_runtime_tool` |
| 上下文隔离 | Worker 只装本批数据；父会话不读原始数据，只读 Worker 摘要与各文件摘要节 |
| 报告装配 | 最终报告仅父会话产出；Worker 只写批次数据文件 |
| 再分批 / 续跑 | 单批仍过重或超时：把批再切小，或用 `cursor`（已完成节点数）断点续跑 |
| 超时熔断 | 同一工具同一目标连续 2 次超时停止重试；最多 1 次降级；仍失败记「数据不足」进入下一环节（与第一节一致） |
| 对账责任 | 第八节副本对账由对应中心节点 Worker 执行；未经对账 / 复核的批量输出不得写入报告 |

### 10.3 效果说明

- 每个大模型只装单批数据，单轮思考时间随批大小下降；
- Worker 并行执行，巡检墙钟时间取决于最慢一批；
- 最重的第八节全量对账（全量 PromQL + Python 对账）被中心节点 Worker 吸收，父会话上下文不被污染。


[READ_HINT] 该文件位于**其他会话**目录 ``files/sessions/b0bd8fba-b91d-425b-9d08-5a77876fe90a/…``，不是本会话产出。若用户要求从零创建系统，请**不要**沿用该文件路径；应 ``remote_write_file`` 写入本会话相对路径（如 ``index.html``）。