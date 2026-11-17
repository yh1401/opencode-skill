# 架构优化 MCP 改造清单（D1–D6）

> v3.1　|　2026-10-23　|　对照：pinpoint-mcp `main@fd124f9`、promthus-mcp `main@cdf3327`

判定原则：**阈值判定必须由 MCP 给出四态结论**（已发现风险 / 未触发规则 / 数据不足 / 不适用），大模型只做解释。
角色：**pinpoint-mcp 是判定源**；promthus-mcp 只取数，不参与判定。

图例：✅ 已实现并可判定　⚠️ 部分实现　➖ 仅取数、不参与判定

## 一、D1–D6 参与情况

| 维度 | 需求 | pinpoint-mcp | promthus-mcp | 需补充 |
|---|---|---|---|---|
| D1 副本 | 副本数 ≥ 3、可用副本 = 期望，持续 15m | ✅ `arch_rule_check_tool`（`deployment_replicas` / `deployment_available`） | ➖ `query` 兜底取数 | 唯一键补 namespace；阈值 3 基准待确认 |
| D2 容量 | CPU 限流 > 20%、内存 > 85%、HPA 达上限，持续 15m | ✅ `arch_rule_check_tool`（`cpu_throttle_ratio` / `memory_ratio` / `hpa_replicas`） | ➖ `query` / `host_metrics_*` 佐证 | 唯一键补 namespace |
| D3 负载 | 同规格 Pod CPU 最大/平均 > 2，持续 15m | ✅ `arch_rule_check_tool`（`pod_cpu_imbalance`） | ➖ `query` 兜底取数 | 唯一键补 namespace |
| D4 OOM | 窗口内 OOMKilled 且重启同时增长 | ⚠️ `arch_rule_check_tool`（`oom_killed` / `restart_increase`），窗口固定 30m | ➖ `query` 兜底取数 | 增加 `window`，支持 7d（`max_over_time([7d:15m])`） |
| D5 链路 | 入度 > 5、出度 > 5、最长链 > 8、异常边、影响面、关键路径 | ⚠️ `analyze_link_topology_tool`（项目级图计算）；`get_server_map_tool` 仅单应用明细 | ⚠️ `get_chain_topology` 仅原始调用图，无图计算 | 补批量 ServerMap 工具；EasyViews 工具重复需收敛 |
| D6 故障域 | 最大宿主机集中比例 > 50%、映射覆盖率 ≥ 80% | ⚠️ `analyze_host_concentration_tool`（六类场景），无 `min_instances` | ⚠️ `get_node_list` / `get_virtual_structure` / `get_project_topology` 仅原始关系 | 补 `min_instances`；EasyViews 工具重复需收敛 |

## 二、需补充（只列 P0 / P1）

| 优先级 | 所属 MCP | 维度 | 缺口 | 期望改动 |
|---|---|---|---|---|
| P0 | pinpoint-mcp | D5 | `get_server_maps_batch_tool` 不存在 | 恢复批量工具，单应用失败隔离 |
| P0 | pinpoint-mcp | D6 | `analyze_host_concentration_tool` 无 `min_instances` | 增加参数（默认 1），实例不足判“不适用” |
| P0 | pinpoint-mcp | D4 | OOM 窗口固定 30m | 增加 `window`，用 `max_over_time([7d:15m])` |
| P1 | pinpoint-mcp | D1–D4 | 唯一键缺 namespace | 标签与分组补 namespace |
| P1 | pinpoint-mcp | D1–D4 | 仅支持单个 deployment，8×N 次调用 | 支持 `deployments=[...]` 批量 |
| P1 | pinpoint-mcp | 范围派生 | 项目名映射不到 deployment | 补 `list_deployments_tool(node, env)` |
| P1 | promthus-mcp | D5 / D6 | 5 个 EasyViews 工具与 pinpoint 重复且命名不一致（`get_chain_topology` vs `analyze_link_topology_tool`） | 只保留一套（建议 pinpoint 的 `*_tool`），另一侧删除或标 deprecated |
| P1 | 两个 MCP | 全部 | 阈值基准 2/3 不一致 | 确认基准，固定输出 `ruleVersion` |

## 三、验收

1. `arch_rule_check(rule="oom_killed", window="7d")` 返回 `threshold.window="7d"`；
2. `analyze_host_concentration(..., min_instances=1)` 不报 unexpected argument；
3. `get_server_maps_batch_tool` 返回 results 数组，单个失败不影响其他；
4. `list_arch_rules` 输出 `ruleVersion` 与全部阈值。

## 四、Skill 侧待补

| 优先级 | 维度 | 现状 | 待补 |
|---|---|---|---|
| P1 | 全部 | `task_id` 未接入；多个工具支持该参数，但报告未透传 | 每次采集生成 `task_id` 并写入报告，可回溯到采集批次 |
| P1 | D2 | 未使用 `latencyCrossCheck`（需配置 `ARCH_LATENCY_PROMQL`），限流结论缺延迟侧交叉验证 | 配置后读取 `latencyCrossCheck`，出现 HIGH 时在证据栏提示 |
| P2 | D1–D4 | 只说明 `include_samples`，未说明 `sample_points` / `step_seconds` / `end_ts` | 补长窗口与降采样口径，避免与 15 点采样冲突 |
| P2 | D5 | `get_link_topology_tool` 列在工具清单，但编排流程无使用场景 | 明确仅用于原始拓扑核对，或从清单移除 |
| P1 | D5 | 明细取证只能串行调 `get_server_map_tool`，Top 20 即 20 次调用，无失败隔离 | 暂以串行 + 单次失败记 🟨 兜底；待 P0 批量工具落地后改一次调用 |

上表均为 Skill 自身可改项，不依赖 MCP 改造；最后一条在 P0 批量工具落地后自动消解。
