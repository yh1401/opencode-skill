# 业务数据/ 数据文件说明

本目录存放**上游真实数据导出与抓包快照**，用于落库、对照字段口径、排查解析问题。均为静态快照，不参与运行时逻辑。

## 一、跳① `appId ↔ 部署项目`（`app_dp_relation`）

| 文件 | 来源 | 用途 |
|---|---|---|
| `apolloApp_list.json` | 上游 `/thirdApi/apolloApp/list` **真实全量导出**（2026-09-17 抓取，1426 行 / 260 个 appId / 767 个部署项目名） | **当前主数据源**：本地库 `app_dp_relation` 由本文件落库（已逐行校验一致）；`mock_server.py` 的占位接口命中本文件即整表使用 |
| `adr接口数据` | 上述全量的**原始终端转储**（curl 命令 + 同一响应原文，约 683KB） | 追溯抓取命令与分页参数（`paginator=false` / `pageIndex` / `pageSize`）；**含真实 `sessionId`，仅限内网查看，勿外发** |
| `upstream-appId-dp-relation-sample.json` | 早期抓包**单页样本**（2026-09-11，100 行，`pageTotal=1420`） | 字段口径核对（`apolloAppId` / `deployProjectId` / `deployProjectName` / `node` / `env` / `apolloHostId`）；全量文件缺失时的兜底样本 |
| `app_dp_relation.sql` | 建表脚本 + **历史样本初始数据**（100 条，源自上面的样本文件） | 本地建档/导数据参考；生产表由启动时 **AutoMigrate 自动创建**（见 `initialize/gorm.go`），无需执行本脚本。实际全量数据由跳① 定时同步**有变化才整表替换**落库（无变化跳过替换） |
| `../../mock/relation_rows_generated.json` | **Mock 造数**（183 行，2026-09-17 生成，非真实抓包） | 仅当全量导出与样本都缺失时兜底；真实全量到位后可删除（`mock_server.py` 检测到缺失即自动关闭造数补充） |

## 二、跳② `部署项目 ↔ Pinpoint 应用`（`deploy_pinpoint_relation`）

| 文件 | 来源 | 用途 |
|---|---|---|
| `deploy_pinpoint_relation.sql` | 该表建表脚本（含 CSV 导入命令） | 本地建档/导数据参考；生产表**不纳入 AutoMigrate**（`initialize/gorm.go` 只注册 `operation_topology` 与 `app_dp_relation`），需由运维/DB 侧按本脚本自建或直接导入生产导出，表内数据也由运维导入 |

**查询期补数（代码常量，2026-09-30）**：运维补充的 4 条 DPR 归属（3 个 `dep_project`）因表侧暂不可改，改为在查询期由代码常量维护，见 `service/pinpoint_relation.go` 的 `pinpointSupplement`，加载 DPR 内存映射时按 `(dep_project, pp_application)` 去重并入（表内已有同键则保留表内数据），**不写库、不回写本地镜像**。清单之外同期反馈的其余数据均已失效、不再使用。

## 三、返回体转储（排障对照）

| 文件 | 来源 | 用途 |
|---|---|---|
| `graph.md` | 生产环境 `/graph` curl 转储（含请求头原文） | 对照真实返回体；**含真实凭据与个人信息，勿外发** |
| `接口示例数据.md` | 由 `接口示例数据.md` + `接口示例数据-graph真实返回.md` + `sysnlog.md` 三份转储合并（2026-09-10） | 逐接口的原始 JSON 返回与可读说明，排障时按小节对照；与 `graph.md` 内容有重叠 |

## 四、补数交付：`运维关系拓扑图待补充数据说明.xlsx`（唯一交付件）

**本目录补数交付仅此一份 xlsx**（2026-09-30 修订版），供运维按表补录跳② 数据。原比对清单 CSV / 快照等中间产物已删除；字段级待协同结论见《[../运维拓扑.md](../运维拓扑.md)》§5.1，缺口口径与实测见同文档 §4。另有 4 条查询期补数改由代码常量维护（见第二节），不入本 xlsx、不落库。

**缺口规模**：跳② 缺口 **333 个 `dep_project` / 479 需求点 / 91 个 appId**；接口表现 `node` / `engProject` 空置 **90 个节点**，对应 **41 个 appId**。

**补哪张表**：只需运维补 **`deploy_pinpoint_relation`（跳②，DPR）**；跳① `app_dp_relation` **不要手工补**（采集期由关系接口全量拉取后整表替换落库，手工行会被下次同步清掉），主表 `operation_topology` 也不用动。

**Sheet 结构**：

| Sheet | 内容 |
|---|---|
| `说明` | 口径、本期补录范围（补跳②：为 333 个 `dep_project` 补齐 `pp_application`）、字段来源与校验 SQL-A |
| `①跳1-上游补录` | 跳① `app_dp_relation` 附表（`apollo_app_id` / `apollo_app_name` / `apolloAppEnv` / `deployProjectId` / `deployProjectName` / `node`），本期暂缓、仅供对照 |
| `②跳2-DPR补录` | 跳② DPR 补录表（`dep_project` / `pp_application`（填「待补」）/ `eg_project`），**333 行** |

**列口径**（`②跳2-DPR补录` sheet）：

| 列 | 说明 |
|---|---|
| `dep_project` | 必填，与 `app_dp_relation.deploy_project_name` 精确一致（区分大小写、`-dep` / `-depadd-gray` 等后缀） |
| `pp_application` | 必填（与 `dep_project` 组成唯一键），**待补** |
| `eg_project` | **已按 ADR `projectName` 预填**（实测命中率 706/944 ≈ 74.8%，仅供参考，需运维校对） |
| `node` / `node_cn` | **均不再补录**：拓扑 `node` 已改取跳① `app_dp_relation.node`（2026-09-30 定，DPR 的 `node` 与 ADR 口径不同，实测 434 个交集部署项目里同值率仅 21.7%）；`node_cn` 已下线，接口不返回、查询期不加载 |

## 使用提示

- 两跳关联的最终效果：`apolloAppId → deployProjectId → dep_project → pp_application/eg_project/node`，属性补充逻辑（多值聚合为列表、不扩行）与降级路径见《[拓扑查询总体逻辑.md](../拓扑查询总体逻辑.md)》。
- 快照仅用于本地核对与落库，**不要**改名为 `api/tprel/data/*.json`（运行时会作为数据源加载，格式不同）。
