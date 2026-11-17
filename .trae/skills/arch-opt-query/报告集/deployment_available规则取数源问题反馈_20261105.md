# 反馈单：`deployment_available` 规则"期望副本"取数源疑似错误

- 反馈日期：2026-11-05
- 反馈人：【运维】架构优化助手（AI1470）
- 涉及工具：`analyze_node_runtime_tool` / `arch_rule_check_tool`，规则 `deployment_available`
- 关联巡检：任务 `arch-gz4d-20261105-1439`（节点 云公司->广州4d）
- 严重度：**高**（导致可用性规则系统性误报，误触发 P0 级误判）

---

## 一、问题一句话

`deployment_available` 规则判定的"期望副本"与实时 K8s `kube_deployment_spec_replicas` **对不上**，疑似取的是 **CMDB/EasyViews 声明副本数**而非实时 spec，导致"声明值 > 实际规模"的应用全部被误报为"副本不可用"。

## 二、现象

对 云公司->广州4d 跑 `analyze_node_runtime_tool`，`deployment_available` 报 **20 个应用"高缺口"**，典型：
- `vcp-saas-cmpt-workflow-dep`：80→0（报"完全不可用"）
- `vcp-saas-cmpt-dataflow-dep`：80→24
- `vnet-vpaas-sentinel-dashboard-dep`：80→2
- `seelink-device-center-control-dep`：32→8
- …（共 20 应用，缺口普遍 70%–100%）

据此曾判定"节点级资源耗尽 / 系统性副本不可用（P0）"。

## 三、复核证据（直接查 Prometheus 原始指标，绕过规则工具）

取数口径：`cmdb_node=云公司->广州4d`，`env=生产`。

1. **6 个高缺口应用逐一复核，实时 spec == 实时 available，零真实缺口：**

   | 应用 | 规则报告 | 实时 spec | 实时 available | 真实缺口 |
   | --- | --- | --- | --- | --- |
   | vcp-saas-cmpt-workflow-dep | 80→0 | 0 | 0 | **0** |
   | vcp-saas-cmpt-dataflow-dep | 80→24 | 24 | 24 | **0** |
   | vnet-vpaas-sentinel-dashboard-dep | 80→2 | 2 | 2 | **0** |
   | ehome-order-prod-022629-dep | 80→30 | 30 | 30 | **0** |
   | seelink-device-center-control-dep | 32→8 | 8 | 8 | **0** |
   | vnet-vpaas-message-dep | 80→4 | 4 | 4 | **0** |

2. **"80"这个值在全集群任何位置都不存在**：不带节点过滤查 `kube_deployment_spec_replicas{deployment="vcp-saas-cmpt-workflow-dep"}`，5 条序列值为 6 / 1 / 0 / 1 / 1（贵州 6、广州4d 0、测试 1×3），**无 80**。
3. **workflow 的 spec 近 3.5 小时（42 采样点）恒为 0**，并非"80 副本突然挂 0"。
4. **全节点总量**：`sum(spec) − sum(available)` 近 3.5h 恒为 **−51**（可用反超期望 51）。若真存在 20 个各缺 50~80 的缺口，总量应为巨大正数，**逻辑上不可能为 −51**。

## 四、根因分析（疑似）

规则"期望副本"取数源不是实时 `kube_deployment_spec_replicas`，而是 **CMDB/EasyViews 登记的声明副本数**（"应有/最大"值）。当应用实际部署规模低于声明值（缩容后基线未更新）时，规则把"声明 − 实际"当成"不可用缺口"报出。

> 佐证：规则工具自身 `dataLimitations` 提到"3 个应用期望副本为 0（已下线/暂停）"，说明其"期望"口径与实时 spec 存在系统性偏差。

## 五、影响

- 本节点单次巡检产生 **21 个虚假"可用性"风险项**，误触发 P0 级"系统性不可用"判定，可能浪费事故响应、误导排障方向。
- 该问题在**任何"实际规模 < CMDB 声明值"的节点都会复现**，属系统性误报，非个例。

## 六、修复建议（按优先级）

1. **（首选）"期望副本"改用实时 `kube_deployment_spec_replicas`**，与 `kube_deployment_status_replicas_available` 同源对比，保证"缺口"语义 = 真实未就绪副本。
2. **若产品意图就是对比 CMDB 声明基线**：请把该规则**改名/改分类**为"CMDB 声明 vs 实际不一致（基线漂移）"，不要归入"副本不可用/可用性"，避免与真实宕机混淆。
3. **报告增强**：输出缺口时**同时回显实时 spec 值**（而非仅"期望/可用"两个数），让使用者能一眼区分"不可用"与"已缩容"。
4. **加一道交叉校验**：当 `实时 spec == 实时 available` 时，不应判为"不可用"。

## 七、验证方法（修复后回归）

对 云公司->广州4d 重跑 `deployment_available`：
- 上述 20 个应用**不应再被判为"副本不可用"**；
- 若仍报，应归类为"基线漂移"且回显实时 spec；
- 全节点不应再出现"期望≫可用"的聚合缺口（实测总量缺口为 −51）。

## 八、环境信息

- 节点：云公司->广州4d（中心节点，`cmdb_node` 标签可用，`cluster=广州4d生产`）
- 指标源：kube-state-metrics（`job=kube-state-metrics`，`type=prod-k8s`）
- 规则版本：V2.3
- 关联报告：`广州4d架构风险巡检报告_20261105.md`（同目录，含完整复核过程与改判）
