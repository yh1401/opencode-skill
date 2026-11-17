# 架构优化智能体实施方案

> 目标：将《架构优化智能体实现方案-V2.md》落地为一个 **Trae Code Skill + MCP Server** 架构优化智能体：自动从**业务拓扑接口 / Prometheus / Pinpoint / 云主机宿主机关系接口**采集数据，由**规则引擎**做确定性判定、**大模型**做风险解释与建议，生成架构优化报告。
>
> 配套文件：
> - 需求/分析方案：`架构优化智能体实现方案-V2.md`（本文对齐该版）
> - 报告输出模板：`架构优化报告模板.md`

---

## 一、目标与范围

### 1.1 分析对象

| 分析对象 | 节点标识（node） | 分析重点 | 数据来源 |
|----------|------------------|----------|----------|
| 中心节点：贵州 | `云公司->贵州` | 副本可用性、容量瓶颈、负载分布、OOM、服务依赖 | 拓扑接口、Prometheus、Pinpoint 拓扑接口 |
| 中心节点：广州 | `云公司->广州4d` | 同上 | 同上 |
| 省市节点 | 名称以 `云公司` 开头的节点 | 项目多实例是否集中于同一物理宿主机 | 节点列表、物理机/云主机关系、项目部署拓扑 |

### 1.2 实施流程（对齐 V2）

```
确定分析范围 → 采集数据 → 统一对象映射 → 执行规则 → 汇总证据 → 生成报告
```

### 1.3 结果四态（贯穿始终）

每个分析项必须输出四种状态之一，**接口失败、监控缺失、关联不完整时不得下"无风险"结论**：

| 状态 | 含义 |
|------|------|
| 🟥 已发现风险 | 规则命中 |
| 🟩 未触发规则 | 已检查且未命中 |
| 🟨 数据不足 | 缺指标 / 缺映射 / 缺窗口，无法判定 |
| ⚪ 不适用 | 无 HPA、非 `-dep` 应用未覆盖等 |

### 1.4 规则引擎 vs 大模型分工

| 职责 | 规则引擎 | 大模型 |
|------|----------|--------|
| 数据关联、数值计算、阈值判定 | ✅ | ❌ |
| 保证同一输入一致结果 | ✅ | ❌ |
| 解释风险、串联业务影响 | ❌ | ✅ |
| 组织报告、提出优化建议 | ❌ | ✅ |
| 缺失数据判定"信息不足" | ❌ | ✅（但不得补造数据） |

---

## 二、整体架构

沿用项目现有「**薄指令层 + MCP Server**」分离架构。

```
┌─────────────────────────────────────────────────────────────┐
│                        Trae / Agent 平台                      │
├─────────────────────────────────────────────────────────────┤
│  arch-opt-query (主技能 SKILL.md)                              │
│   ├── 触发条件 / 参数采集（节点、模块、时间窗口）               │
│   ├── 编排：拓扑 → 各维度规则 → 宿主机映射                      │
│   ├── 汇总四态结果 + 证据 → 组装 Prompt                        │
│   └── 调 LLM 套《架构优化报告模板.md》生成报告                  │
└─────────────────────────────────────────────────────────────┘
                          │  MCP 工具调用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│          arch-opt-mcp-server (Python / FastAPI)               │
│  位置: arch-opt-mcp-server/scripts/mcp_server.py               │
│  —— 中心节点 ——                                               │
│  ├── mcp_module_topology   获取模块与应用归属(拓扑接口)        │
│  ├── mcp_replica           副本数量与可用性(规则)              │
│  ├── mcp_capacity          容量瓶颈 CPU/内存/HPA(规则)         │
│  ├── mcp_load_balance      Pod 负载不均衡(规则)               │
│  ├── mcp_oom               OOM 风险(规则)                     │
│  ├── mcp_link_dependency   链路依赖(Pinpoint 拓扑接口)         │
│  —— 省市节点 ——                                              │
│  ├── mcp_node_list         节点列表(筛"云公司")               │
│  ├── mcp_virtual_structure 物理机↔云主机关系                  │
│  ├── mcp_topology_info     项目↔云主机部署关系                │
│  └── mcp_fault_domain      宿主机集中判定(汇总映射)            │
└─────────────────────────────────────────────────────────────┘
          │
          ├── Prometheus  http://183.56.210.90:8082/api/v1/query
          ├── 统一拓扑代理  https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince
          │     └─ getTopologyByProductName / getTopologyByNodeProject / getNode / getVirtualStructure / getTopologyInfo
          └── Pinpoint 拓扑（经上述拓扑代理）
```

### 职责划分

| 职责 | Skill 指令层 | MCP Server | 大模型 |
|------|--------------|------------|--------|
| 触发条件识别、参数映射 | ✅ | ❌ | ❌ |
| 编排调用顺序、Prompt 组装 | ✅ | ❌ | ❌ |
| 数据采集、对象映射 | ❌ | ✅ | ❌ |
| 数值计算、阈值判定（确定性） | ❌ | ✅ | ❌ |
| 风险解释、影响串联、建议生成 | ❌ | ❌ | ✅ |

---

## 三、数据采集层设计（核心）

> **数据源与维度查询对照总表**（落地实现口径，完整 PromQL / 接口入参 / 取值路径见 `arch-opt-mcp-server/数据查询对照表.md`）：

| 维度 | 工具 | 数据源 | 查询依据（指标 / 接口） | 判定阈值 |
|------|------|--------|------------------------|----------|
| 入口 | `mcp_module_topology` | 拓扑代理 | `getTopologyByProductName` | — |
| D1 副本数量与可用性 | `mcp_replica` | Prometheus | `kube_deployment_spec_replicas`、`kube_deployment_status_replicas_available` | `min_replicas` = 3 |
| D2 容量瓶颈 | `mcp_capacity` | Prometheus | CPU 限流 `rate(container_cpu_cfs_throttled_seconds_total[15m])/rate(container_cpu_cfs_periods_total[15m])`；内存 `container_memory_working_set_bytes/kube_pod_container_resource_limits_memory_bytes`；HPA `kube_horizontalpodautoscaler_status_current_replicas == …_spec_max_replicas` | CPU>20%、内存>85%、15min |
| D3 负载不均衡 | `mcp_load_balance` | Prometheus | `max(rate(container_cpu_usage_seconds_total[15m])) by (deployment) / avg(…)` | > 2.0、15min |
| D4 OOM | `mcp_oom` | Prometheus | `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}`、`kube_pod_container_status_restarts_total` | 近 7 天 |
| D5 链路依赖 | `mcp_link_dependency` | 拓扑代理 | `getTopologyByNodeProject`（`data.nodes` / `data.edges`） | 待制定 |
| D6 故障域（辅助） | `mcp_node_list` | 拓扑代理 | `getNode`（`ctyun_node_code3`） | — |
| D6 故障域（辅助） | `mcp_virtual_structure` | 拓扑代理 | `getVirtualStructure`（`ipData`） | — |
| D6 故障域（辅助） | `mcp_topology_info` | 拓扑代理 | `getTopologyInfo`（`projectIpData`） | — |
| D6 故障域 | `mcp_fault_domain` | 拓扑代理 | 串联上述 3 个接口 | 同宿主机/集中 > 50% |

> 数据源说明：**拓扑代理**（统一网关，透传 5 个业务接口）与 **Prometheus**（直连容器指标）两个来源；D1~D4 全部走 Prometheus 的 PromQL（持续类规则用 `[15m]` 窗口），D5/D6 及入口走拓扑代理业务接口。

### 3.0 统一拓扑代理接口

所有业务接口经同一代理转发，鉴权参数固定：

```json
{
  "url": "<实际业务接口地址>",
  "types": "GET|POST",
  "provinceDetail": {
    "province": "大模型",
    "appId": "YOUR_EASYVIEWS_APP_ID",
    "appSecret": "YOUR_EASYVIEWS_APP_SECRET"
  },
  "reqParam": { "...": "..." }
}
```

代理地址：`https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince`（POST）

---

### 3.1 获取模块与应用归属 `mcp_module_topology`

- **业务接口**：`getTopologyByProductName`
- **入参**：`center=true`，`node`（`云公司->贵州` / `云公司->广州4d`），`productName="天翼看家"`
- **解析**：遍历 `data[].topologyData.nodes[]`，保留 `name`、`nameCn`、`objectType`、`projectName`
- **建立映射**：节点 → 模块（nameCn）→ 应用（projectName）
- **筛选**：第一阶段仅对 `projectName` 以 `-dep` 结尾的应用执行容器检查，其余记「⚪ 不适用／本阶段未覆盖」
- **注意**：应用名可能跨集群/命名空间重复，需结合监控标签补齐唯一标识；同一应用属多模块时保留全部归属

---

### 3.2 D1 副本数量与可用性 `mcp_replica`

- **数据源**：Prometheus（`http://183.56.210.90:8082/api/v1/query`）
- **指标**：
  - 期望副本：`kube_deployment_spec_replicas{cmdb_node="云公司->贵州", deployment="...", endpoint="https-main", env="生产"}`
  - 可用副本：`kube_deployment_status_replicas_available{...}`
- **判定**（副本不足 与 单点部署 分开描述）：
  - 副本不足：期望副本 < 冗余要求（口径待确认：原文"大于 2 副本"= 至少 3 副本）
  - 副本不可用：可用副本 < 期望副本
- **输出**：每应用 `{deployment, spec, available, 状态四态, 结论}`

### 3.3 D2 容量瓶颈 `mcp_capacity`

| 子项 | 指标 / 判定 |
|------|-------------|
| CPU 限流 | 同一容器窗口内 `rate(container_cpu_cfs_throttled_seconds_total[15m]) / rate(container_cpu_cfs_periods_total[15m]) > 0.20`，持续 15min；总周期为 0 不计，关联调用延迟 |
| 内存容量 | `container_memory_working_set_bytes / kube_pod_container_resource_limits_memory_bytes > 0.85`，持续 15min；limit 缺失/为 0 记 🟨 或 ⚪ |
| 扩容容量 | `kube_horizontalpodautoscaler_status_current_replicas == kube_horizontalpodautoscaler_spec_max_replicas` 且负载偏高；无 HPA 记 ⚪ |

> ⚠️ "持续 15 分钟"不能仅靠单次即时值，需 **时间窗口 PromQL 表达式**或 **range query** 证明持续触发。

### 3.4 D3 负载不均衡 `mcp_load_balance`

- **指标**：同 deployment 每个 Pod 的 CPU 负载 `rate(container_cpu_usage_seconds_total{...}[15m]) by (pod)`
- **判定**：`max(pod 负载) / avg(pod 负载) > 2`，持续 15min；统一采样窗口与 CPU 口径，排除不同规格与启动阶段，均值接近零时避免比值失真

### 3.5 D4 OOM `mcp_oom`

- **数据源**：K8s 事件（优先）或 Prometheus `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` + `kube_pod_container_status_restarts_total`
- **判定**：近期出现 OOMKilled 且有重启证据；**"近期"窗口需配置**，历史终止原因不能单独证明近期 OOM

### 3.6 D5 链路依赖 `mcp_link_dependency`

- **业务接口**：`getTopologyByNodeProject`
- **入参**：`reqParam.node` 用 3.1 返回的 `deployNode`（可能含多节点/别名，勿替换为单一节点）；`projects` 用模块的 `projectName` 列表；`minute/callerRange/calleeRange/filterNode/nodeType` 联调时确认
- **输出**：依赖图（节点 + 边 + 可用统计字段：调用量、错误率、耗时），供分析 8 类依赖问题
- ⚠️ 链路长度、依赖数、错误率等阈值**当前未定，联调时制定**；未定项标 🟨

### 3.7 D6 省节点故障域 `mcp_fault_domain`（三步）

第一步 `mcp_node_list` — 获取节点列表（业务接口 `getNode`，`center=true`），筛选名称以 `云公司` 开头的节点（注意：省市节点 `ctyun_node_code3` 多数为空）。

第二步 `mcp_virtual_structure` — 获取物理机↔云主机关系（业务接口 `getVirtualStructure`，传 `ctyun_node_code3`）。`ipData` 键为物理机 IP、值为该机上的云主机 IP 列表，反向索引为「云主机 IP → 物理机 IP」。

第三步 `mcp_topology_info` — 获取项目↔云主机部署关系（业务接口 `getTopologyInfo`，传 `node`）。用 `projectIpData`（项目→云主机列表）为主，关联第二步结果。

**汇总判定**：
- 建立「项目 → 云主机 → 物理宿主机」三层映射
- 统计：**宿主机数**、**最大宿主机集中比例**（单一宿主机承载该项目云主机数最大值 ÷ 该项目云主机总数）
- 五类场景判定（单实例/全集中/部分集中/映射不全/映射冲突），对应报告模板 3.6

---

## 四、阈值配置（`config/thresholds.json`）

```json
{
  "version": "2.0.0",
  "replica": {
    "min_replicas": 3,
    "min_replicas_note": "原文'大于2副本'=至少3副本，业务确认后定稿",
    "check_available_equal": true
  },
  "capacity": {
    "cpu_throttle_ratio": 0.20,
    "memory_usage_ratio": 0.85,
    "hpa_max_reached_check": true,
    "duration_minutes": 15
  },
  "load_balance": {
    "max_avg_ratio": 2.0,
    "duration_minutes": 15
  },
  "oom": {
    "lookback_days": 7,
    "lookback_note": "近期窗口需业务确认"
  },
  "link_dependency": {
    "error_rate_threshold": null,
    "slow_call_threshold_ms": null,
    "note": "链路长度/依赖数/错误率阈值待联调制定"
  },
  "fault_domain": {
    "min_instance_to_check": 1,
    "same_host_all_risk": true,
    "partial_concentration_report": true
  }
}
```

---

## 五、MCP 工具注册（FastAPI）

| 工具名 | 入参（核心） | 输出 |
|--------|-------------|------|
| `mcp_module_topology` | `node` / `productName` | 模块与应用归属 + `deployNode` |
| `mcp_replica` | `node` / `deployments[]` | 副本四态结果 |
| `mcp_capacity` | `node` / `deployments[]` / `duration` | 容量四态结果（CPU/内存/HPA） |
| `mcp_load_balance` | `node` / `deployments[]` | 负载四态结果 |
| `mcp_oom` | `node` / `deployments[]` / `days` | OOM 四态结果 |
| `mcp_link_dependency` | `deployNode` / `projects[]` | 依赖拓扑数据 |
| `mcp_node_list` | — | 云公司节点列表 |
| `mcp_virtual_structure` | `ctyun_node_code3` | 物理机↔云主机映射 |
| `mcp_topology_info` | `node` | 项目↔云主机映射 |
| `mcp_fault_domain` | `node` | 宿主机集中四态结果 |

**工程要点**（对齐项目约定）：
- 统一 `_send_request`（超时 + 重试，`max_retries`/`retry_delay` 可配）封装拓扑代理、Prometheus、Pinpoint 请求。
- 记录每次分析的任务标识、采集时间、分析窗口、目标节点、映射依据、规则版本。
- 每个结果带四态 `status` 字段 + 证据（指标值、阈值、持续时长、覆盖率），供 LLM 引用。
- 阈值从 `config/thresholds.json` 动态加载；接口端点从 `config/datasources.json` 读取。
- 端口建议 `8062`（避免与 cmdb 8061 冲突）。

---

## 六、Skill 薄指令层设计

### 6.1 主技能 `SKILL.md`（`.trae/skills/arch-opt-query/`）

frontmatter：
```
---
name: arch-opt-query
description: 架构优化智能体。采集副本可用性、容量瓶颈、负载分布、OOM、服务依赖、宿主机故障域集中等维度数据，由规则引擎判定、大模型生成架构优化报告。
version: 2.0.0
---
```

### 6.2 编排流程

1. 识别分析对象（贵州 / 广州4d / 云公司相关省市节点），缺失则引导选择。
2. 中心节点：`mcp_module_topology` 拿模块归属 → 过滤 `-dep` 应用 → 并行调 `mcp_replica`、`mcp_capacity`、`mcp_load_balance`、`mcp_oom`、`mcp_link_dependency`。
3. 省市节点：`mcp_node_list` → `mcp_virtual_structure` → `mcp_topology_info` → `mcp_fault_domain`。
4. 汇总四态结果 + 原始证据 + 覆盖率，组装 Prompt。
5. 调 LLM 套《架构优化报告模板.md》生成报告，缺失数据处标「信息不足」，不补造。

---

## 七、AI 分析层设计

### 7.1 Prompt 结构

```
系统指令：
你是云原生架构优化专家。规则引擎已完成数值计算与阈值判定，
你只负责：解释风险、串联业务影响、组织报告、提出优化建议。

要求：
1. 只依据给定证据下结论，不补造数据；缺失处明确标注"信息不足"。
2. 每个已发现风险，按报告模板的 7 字段问题卡输出。
3. 结果区分四态：已发现/未触发/数据不足/不适用。
4. 优化建议含：立即缓解、根治方案、预期收益、前置条件、优先级(P0~P3)。
5. 优先级的判定综合考虑业务重要性、影响范围、证据充分程度，不只看指标大小。

规则判定结果(JSON，含四态 status 与证据)：
{ 各维度 MCP 工具返回的结构化结果 }

阈值配置(thresholds.json)：
{ ... }

请输出报告正文。
```

### 7.2 关键点

- **确定性归规则引擎、解释性归大模型**：LLM 不碰数值计算，避免同一输入产生不同判定。
- **四态 + 覆盖率**随结果一并传入，让 LLM 能如实说明"数据不足"，而非强行下结论。
- **阈值与口径缺口**（如"副本>2 待确认""链路阈值待定""OOM 近期窗口"）显式传给 LLM，正文中如实标注。

---

## 八、目录结构

```
opencode_skill/
├── ops-optimize/
│   ├── 架构优化智能体实现方案-V2.md   # 需求/分析方案（最新）
│   ├── 架构优化报告模板.md            # 报告模板（已对齐 V2）
│   └── 架构优化Skill实施方案.md       # 本文件
├── .trae/skills/arch-opt-query/      # Skill 薄指令层
│   ├── SKILL.md
│   ├── docs/README.md                 # Skill 介绍文档
│   └── references/                    # Prompt 模板、输出规范、字段映射
└── arch-opt-mcp-server/              # MCP Server
    ├── scripts/mcp_server.py          # 工具实现 + FastAPI
    ├── config/thresholds.json         # 判定阈值（含规则版本）
    ├── config/datasources.json        # 拓扑代理/Prometheus/Pinpoint 端点
    ├── docs/README.md                 # MCP 服务介绍文档
    ├── 数据查询对照表.md               # 各维度查询语句与判定口径
    ├── .env.example
    ├── requirements.txt
    ├── start.sh
    ├── Dockerfile
    └── docker-compose.yml
```

---

## 九、实施顺序与验收（对齐 V2 四阶段）

| 阶段 | 工作内容 | 验收重点 |
|------|----------|----------|
| 1. 数据联通 | 联调模块/节点/项目/宿主机接口，核实监控标签与 `-dep` 命名规则 | 目标对象可定位、字段含义明确、缺失与冲突（如 `ctyun_node_code3` 为空）可识别 |
| 2. 基础规则 | 完成副本、容量（CPU/内存/HPA）、负载、OOM 及宿主机集中分析 | 规则可配置、持续时间可验证（时间窗口证据）、结果四态可追溯 |
| 3. 链路分析 | 接入 Pinpoint 拓扑，关联模块归属与上下游影响 | 拓扑覆盖范围明确、统计窗口明确、链路阈值制定、风险判定有依据 |
| 4. 报告生成 | 汇聚规则结果 + 大模型生成建议，套报告模板 | 每结论有证据、数据不足可见（覆盖率）、建议与风险一一匹配 |

---

## 十、验收标准

1. 输入"分析贵州、广州4d 及云公司省市的架构风险"能一键产出报告。
2. 每条结论带四态（已发现/未触发/数据不足/不适用）与可溯源证据（附录 6.1）。
3. 阈值/口径仅改 `thresholds.json` 即可调整，无需改代码、改模板。
4. 持续 15 分钟的规则有完整时间窗口证据，不靠单次即时值。
5. 数据不足、接口失败、`ctyun_node_code3` 为空等场景，报告明确标注"信息不足"，不误报"无风险"。
6. 报告严重程度分级与人工巡检一致，建议与风险匹配、优先级考虑业务影响而非仅指标大小。

---

## 十一、风险与依赖

| 风险 | 影响 | 应对 |
|------|------|------|
| 副本口径未定（>2 还是 ≥3） | 单点判定存疑 | 阈值 `min_replicas` + note 标注，业务确认后定稿 |
| 链路依赖阈值未定 | D5 无法定量判定 | 联调阶段制定，未定前标 🟨 数据不足 |
| 省市节点 `ctyun_node_code3` 多为空 | 宿主机映射中断 | 记录数据不足，探明替代查询方式 |
| 应用名跨集群/命名空间重复 | 指标关联错位 | 结合监控标签补齐唯一标识 |
| "持续 15 分钟"难证明 | 误判 | 用时间窗口 PromQL / range query，不用即时值 |
| Pinpoint 字段语义未确认（minute/callerRange 等） | 链路数据解析偏差 | 联调确认字段后再固化解析 |
| 多数据源 token 压力 | 上下文过大 | 规则结果只回传四态 + 命中项 + 关键指标，全量明细放附录 |