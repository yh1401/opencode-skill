# 日志目录说明

本目录两类日志：

| 文件 | 内容 |
| --- | --- |
| `YYYY-MM-DD.log` | 模块应用日志（`api/tprel/tplog`）：按天滚动，只保留最近 5 份（`TPREL_LOG_DIR` 改目录、`TPREL_LOG_KEEP` 改份数），并同步打印到 stdout |
| `e2e-run-YYYYMMDD.log` | 端到端验证归档：进程 stdout 全量原文（含上面的应用日志 + gin/gorm 输出），一次连续运行记录全部场景，跑完手工保留 |

环境：`config_local.yaml`（服务 `:8867`）、MySQL（docker `devops_mysql`，库 `devops_automation_test`）、
Mock 服务 `:18080`（承接 Apollo 三类接口与关系接口 `/thirdApi/appIdDeployProjectRelations`）。

## 归档 run（2026-09-17，真实全量关系数据口径）

> `e2e-run-20260917.log`：清空旧日志后**一次连续跑完 4 个场景**（主流程 → 降级一 → 降级二 → 恢复），
> 全场景区间 **L5141–L16520**（15:47:19–15:51:36），其后为服务常驻日志与抽查请求。下表行号均指该文件。
>
> **关系数据为真实全量**：`app_dp_relation` 1426 行 / 260 appId / 1085 条 PRO，来源上游
> `/thirdApi/apolloApp/list` 导出（`docs/业务数据/apolloApp_list.json`），Mock 已改为直接回放该导出，
> 本地 `/sync` 与每日 01:30 cron 不会再用样本/造数覆盖。**归档 run 内的 Mock 造数口径已作废。**

### 归档清单

| 场景 | 决定性日志行 |
| --- | --- |
| 启动 | L4371 本地快照已加载 `totalRows=1418`；L4515 定时同步已注册 `cron=0 30 1 * * * ?` |
| ① 主流程·采集期 | L5141 同步开始（`envs=[PRO] baseUrl=127.0.0.1:18080`）；L5142 关系接口拉取开始；L5145–L5173 分页 15 页 `accRows=1426`；L5174 拉取完成 `rows=1426 pages=15 costMs=63`；**L5317 跳① 关系同步完成 `rows=1426 dbRows=1426 wrote=true err=false msg=整表替换落库 1426 行`**；L8212 快照直写 `rows=1418`；L8281 镜像落库 `rows=1418`；L8283 主表同步完成 `insert=1418 totalMs=1761`；L8284 结果总览 / L8293 `关系同步: 整表替换落库 1426 行` |
| ① 主流程·查询期 | L8674 pinpoint 部署项目映射 `deps=1501`；**L8675 关系索引已加载 `source=db rows=1426 apps=538`**；请求 L8676 `/graph`、L8816 `/list`、L8960 `/detail`、L9008 `/middlewareTypes` |
| ② 降级一（库无关系表） | L9368 同步开始；L9369–L9401 关系接口仍拉取 15 页 1426 行（`costMs=53`）；**L9476 跳① `dbRows=0 wrote=false err=true`（关系表不存在未落库）**；L12441 主表仍 `insert=1418 totalMs=1697`；L12451 结果总览 `关系同步: 【异常】…`；**L12660 关系索引已加载 `source=file rows=1426 apps=538`**（L12661 `/graph`） |
| ③ 降级二（库与镜像均无） | 移走镜像并等索引 TTL（60s）过期后查询：**L13006 关系索引已加载 `source=api rows=1426 apps=538`**（L13007 `/graph`、L13147 `/list`） |
| ④ 恢复 | L13163 同步开始；**L13338 跳① `rows=1426 dbRows=1426 wrote=true err=false`**；L16303 同步完成 `totalMs=635`；L16304 结果总览；**L16519 关系索引已加载 `source=db`**（L16520 `/graph`） |

### 接口实测结果（真实数据，Mock 承接全部外部调用）

| 接口 | 结果 |
| --- | --- |
| `/graph?env=PRO` | `code=0` `dataSource=db` `syncDate=2026-09-17` `partial=false` `counts={"apolloHosts":5,"application":108,"middleware":341,"edges":756,"nodes":449}`（四场景一致） |
| `/list?page=1&pageSize=5&env=PRO` | `code=0`（**数字**，信封为 `{code,list,pageTotal}`，无 `data`）`pageTotal=13322`（**展开后行数**，非快照行数）；行内 `node/engProject/depProject/ppApplication` 均有值 |
| `/detail?id=app:apollo-ehome-pro&env=PRO` | `name=ehome-pro-server`、`node=GuiZhou3-Product`、`engProject=ehome-pro`、`depProject=proservice-prod-113804-dep`（跳①）、`ppApp=GZ3-ehome-pro`、`apolloHosts=2`、`middlewares=2`（mysql） |
| `/middlewareTypes` | `code=0`，7 类（elasticsearch/kafka/mysql/nacos/redis/rocketmq/zookeeper） |
| `POST /sync` → `/sync/status` | `insertCount=1418` `hostCount=5` `appCount=234` `namespaceCount=702` `partial=false` `relationRows=1426` `relationWrote=true` `relationErr=false` `relationMsg=整表替换落库 1426 行`；`costMs=1761`（主流程）/`1697`（降级一）/`635`（恢复） |
| `/sync/logs` | `code=0`，`date=2026-09-17` `file=2026-09-17.log` `truncated=true` |
| 无 token 请求 | HTTP 200 + body `code=401` `msg=未登录或非法访问` |

> 节点 id 口径（2026-09-17 起以 appId 为主）：应用节点合并键为 `apollo_app_id`，节点 id 为 `app:{apollo_app_id}`
> （如 `app:apollo-ehome-pro`）。除 `pp_application` 外，`node/eng_project/dep_project` 均为 appId 的属性。

### 两跳覆盖率（真实数据，按 appId 维度）

| 指标 | 实测 | 说明 |
| --- | --- | --- |
| OT 快照行 / appId（env=PRO） | 1418 / 108 | `operation_topology` 当日快照 |
| 跳① 命中 appId | **83 / 108** | 其余 25 个在 `app_dp_relation` 无 PRO 关系行 |
| 跳①+跳② 四列齐全 appId | **69 / 108** | 83 − 69 = 14 个命中跳①但部署项目在 DPR 无对应 |
| 行级：命中跳① | 1305 / 1418（92.0%） | 行展开后 `dep_project` 有值 |
| 行级：四列齐全 | **1199 / 1418（84.6%）** | 四列同源，均来自两跳关联 |
| 关系索引 | `rows=1426 apps=538` | 键为 `apollo_app_id|env`，故 apps 数大于 PRO appId 数 |

落库核对：`app_dp_relation=1426` 行（与 `apolloApp_list.json` 逐行一致）、`deploy_pinpoint_relation=4264` 行 / 1501 个部署项目、
`operation_topology` 当日快照 1418 行；镜像 `data/app_dp_relation.json` 1426 行 / 245 KB。
ADR 的 685 个 PRO 部署项目中有 259 个在 DPR 无对应（待运维补 DPR 侧数据）。

缺口清单（25 个跳①未命中 appId、14 个跳②未命中 appId）见
[../docs/待运维补充/运维补充说明.md](../docs/待运维补充/运维补充说明.md)。

## 关系接口拉取日志（`[dp_relation]` 模块）

跳① 关系数据通过上游接口 `/thirdApi/appIdDeployProjectRelations` 分页拉取，全过程可观测，
日志模块前缀 `[dp_relation]`（主表拉取 Apollo 数据为 `[operation_topology]`，两者可分别 grep）。

| 日志 | 时机 | 关键字段 |
| --- | --- | --- |
| `请求完成` / `请求失败` | 每一页 HTTP 请求（`doGet`） | `tag=appId部署项目关系`、`url`、`costMs`、`bytes` |
| `关系接口拉取开始` | `fetchRelationDTOs` 进入分页循环前 | `path`、`pageSize`、`maxPages` |
| `关系接口分页完成` | 每页解析成功 | `page`、`pageTotal`、`rows`、`accRows` |
| `关系接口拉取完成` | 全部页拉取结束 | `rows`、`pages`、`costMs` |
| `关系接口拉取失败` | 任一页失败（含首页） | `page`、`error` |
| `关系接口总页数超上限，仅拉取前若干页` | `pageTotal > maxPages` | `pageTotal`、`maxPages` |
| `关系接口兜底拉取失败` | 查询期 api 兜底分支失败 | `error` |

采集期一次 `/sync` 的完整片段（`e2e-run-20260917.log`）：

```
L5141  [operation_topology] 同步入口/同步开始     envs=[PRO] namespaces=3 workers=8 baseUrl=http://127.0.0.1:18080
L5142  [dp_relation] 关系接口拉取开始             path=/thirdApi/appIdDeployProjectRelations pageSize=100 maxPages=200
L5145  [dp_relation] 关系接口分页完成             page=1  pageTotal=15 rows=100
L5147  [dp_relation] 关系接口分页完成             page=2  pageTotal=15 rows=100 accRows=200
  …（page 3–14 每页 100 行）…
L5173  [dp_relation] 关系接口分页完成             page=15 pageTotal=15 rows=26  accRows=1426
L5174  [dp_relation] 关系接口拉取完成             rows=1426 pages=15 costMs=63
L5317  [dp_relation] 跳① 关系同步完成             rows=1426 dbRows=1426 wrote=true err=false
```

## 全流程链路（对应 `e2e-run-20260917.log` 场景①）

```
启动
  L4371  [datastore] 本地快照已加载               totalRows=1418
  L4515  定时同步已注册（运维关系拓扑）            cron=0 30 1 * * * ?  syncEnable=true
采集期（手动 /sync，与 cron 每日 01:30 同一内核）
  L5141  [operation_topology] 同步入口/同步开始
  L5142-L5174 [dp_relation] 关系接口拉取          1426 行 / 15 页 / 63ms
  L5317  [dp_relation] 跳① 关系同步完成           rows=1426 dbRows=1426 wrote=true
  L8212  [datastore]  本地快照已直写               syncDate=2026-09-17 rows=1418
  L8281  [datastore]  快照镜像落库完成             rows=1418
  L8283  [operation_topology] 同步完成             insert=1418 partial=false totalMs=1761
  L8284  · 结果总览                                执行结果: 成功 / 关系同步: 整表替换落库 1426 行
查询期（首查懒加载关系索引）
  L8674  [pinpoint]  部署项目映射已加载            deps=1501
  L8675  [dp_relation] 关系索引已加载              source=db rows=1426 apps=538
  查询请求：L8676 /graph、L8816 /list、L8960 /detail、L9008 /middlewareTypes
```

结论：采集期把跳① 关系全量拉取并**整表替换落库** `app_dp_relation` + 回写镜像，同时失效内存索引；
查询期只读「库 / 镜像 / 接口兜底」，正常情况下不再依赖上游关系接口。

## 查询期三级降级

`dpTargets()` 依次尝试：**库（db）→ 本地镜像（file）→ 关系接口兜底（api）**，三者皆无则置空并记 Warn，读接口按原行返回。

| 级别 | 构造方式 | 实测（`e2e-run-20260917.log`） |
| --- | --- | --- |
| 一：库无表 | `RENAME TABLE app_dp_relation TO app_dp_relation_bak` 后 `/sync` + 查询 | L12660 `source=file rows=1426 apps=538` |
| 二：库与镜像均无 | 再移走 `data/app_dp_relation.json`，**等索引 TTL（60s）过期**后查询 | L13006 `source=api rows=1426 apps=538`（兜底经 Mock 拉取 15 页 1426 行） |
| 恢复 | 表与镜像改回，再 `/sync` | L13338 `wrote=true` + L16519 `source=db` |

三条路径下 `/graph?env=PRO` 均返回 `nodes=449` / `edges=756` / 应用节点 108，`/detail` 的
`engProject` / `depProject` / `node` / `ppApp` / `middlewares` 与 `/list` 的四列完全一致。

> 说明：`/graph` 响应里的 `dataSource` 指**拓扑主表快照**来源（db/file），不是关系索引的级别；
> 关系索引降级级别看 `[dp_relation] appId↔部署项目 关系索引已加载` 的 `source` 字段。
> 启动后首次加载即发现库不可用时不再额外打「关系表读取失败」告警（避免每次刷新刷屏）。接口兜底结果按 `relation_api_ttl`（60s）缓存。

## 报错升级验证（降级一段）

`/sync` 结果含 `relationErr`：关系表缺失 / 写库失败 / 上游拉取失败时置 `true`，并在 `relationMsg` 说明原因。

| 场景 | `/sync` 返回 | 结果总览 |
| --- | --- | --- |
| 正常（L5317） | `relationWrote=true` `relationErr=false` `relationMsg=整表替换落库 1426 行` | `执行结果: 成功` / `关系同步: 整表替换落库 1426 行`（L8293） |
| 表缺失（L9476） | `relationWrote=false` `relationErr=true` `relationMsg=关系表 app_dp_relation 不存在，未落库（请先执行 docs/业务数据/app_dp_relation.sql）；本次仅写本地镜像` | `执行结果: 成功（关系同步异常）` / `关系同步: 【异常】…`（L12451） |
| 恢复后正常（L13338） | `relationWrote=true` `relationErr=false` | `执行结果: 成功` / `关系同步: 整表替换落库 1426 行` |

表缺失期间主表同步与查询均不受影响：本次 `/sync` 仍 `insert=1418`（L12441），主表查询 `dataSource=db`，
关系索引降级为 `source=file`（L12660），`/list` 正常返回四列。注意表缺失时关系接口仍会正常拉取，仅落库步骤失败，因此拉取日志与落库报错可分别定位。

## 复现方式

```bash
# 1. 依赖（Mock 会直接回放 docs/业务数据/apolloApp_list.json 的 1426 行）
docker start devops_mysql mock_apollo
# 2. 建表（首次）
docker exec -i devops_mysql mysql -udevops_automation_test -p'<pwd>' devops_automation_test < api/tprel/docs/业务数据/app_dp_relation.sql
# 3. 先清旧日志，再启动（stdout 全量归档，单一文件记录全部场景）
rm -f api/tprel/logs/e2e-run-*.log api/tprel/logs/$(date +%F).log
CGO_ENABLED=0 go build -o /tmp/tprel_server . \
  && nohup /tmp/tprel_server -c config_local.yaml > api/tprel/logs/e2e-run-$(date +%Y%m%d).log 2>&1 &
# 4. 采集期：触发同步（异步提交后轮询状态）
curl -X POST "http://127.0.0.1:8867/api/external/operationTopology/sync" -H "token: $TOKEN"
curl "http://127.0.0.1:8867/api/external/operationTopology/sync/status" -H "token: $TOKEN"
# 5. 查询期：首查懒加载关系索引，日志出现 [dp_relation] 关系索引已加载 source=db
curl "http://127.0.0.1:8867/api/external/operationTopology/graph?env=PRO" -H "token: $TOKEN"
# 6. 构造降级一（库无表）：改名后 /sync（应 relationErr=true）再查询，索引应 source=file
docker exec devops_mysql mysql -udevops_automation_test -p'<pwd>' devops_automation_test \
  -e "RENAME TABLE app_dp_relation TO app_dp_relation_bak;"
# 7. 构造降级二（库与镜像均无）：移走镜像，等 60s 索引 TTL 过期后再查询，索引应 source=api
mv api/tprel/data/app_dp_relation.json /tmp/
# 8. 恢复：镜像与表改回，再 /sync，应 relationWrote=true / relationErr=false，索引回到 source=db
docker exec devops_mysql mysql -udevops_automation_test -p'<pwd>' devops_automation_test \
  -e "RENAME TABLE app_dp_relation_bak TO app_dp_relation;"
```

> 说明：接口契约见 [接口逻辑说明.md](../docs/接口逻辑说明.md)，链路设计见 [拓扑查询总体逻辑.md](../docs/拓扑查询总体逻辑.md)。
