# Mock 数据演示说明

> 面向：演示讲解 / 领导汇报 / 交接
> 日期：2026-11-02（更新 2026-11-03 增补告警中心演示，2026-11-04 增补阶段四 T8–T11、阶段五 T12–T14 与 S5 日志证据，2026-11-06 增补 M2–M5 验真/基线/热力图/调用链/ES 日志 mock，2026-11-10 增补全栈复测基线、浏览器端逐页验收与平台使用动线，2026-11-11 按实测校正 C3/稽核/关系修正/知识库与干净种子态基线）　｜　数据版本：`mockdata/_meta.json`（生成于 2026-10-30 17:37）
> 目标：不依赖任何外部 Pinpoint / Prometheus / CMDB / Elasticsearch 服务，本地即可完整跑通「智能监控排障（含告警中心闭环、业务接口监控、质差分析、告警↔拓扑联动）」与「云网数据治理稽核」两条主链路；日志证据优先取 ES mock，ES 不可用时回退 log_analyzer（未启动时该步自动 `skipped`）。

---

## 一、一句话结论

四个上游服务（pinpoint / prometheus / cmdb / es）的 **63 个工具全部有本地文件数据**，配一个单进程 Mock 上游服务占用与生产一致的 4 个端口；平台**零改造**（不加任何 mock 分支代码）即可完成探活、稽核出账、诊断出结论、告警入库并自动排障出单。

---

## 二、交付清单

| 类别 | 位置 | 内容 |
|---|---|---|
| 数据文件 | [backend/mockdata/](file:///Users/a666/Documents/trae_projects/opencode_skill/ai-ops/platform/backend/mockdata) | 68 个 JSON，按 server 分 4 个目录 |
| Mock 服务 | [cmd/mockmcp/main.go](file:///Users/a666/Documents/trae_projects/opencode_skill/ai-ops/platform/backend/cmd/mockmcp/main.go) | 单进程 4 端口，JSON-RPC 2.0（MCP streamable-http） |
| 数据生成脚本 | `/tmp/gen_mockdata.py` | 从拓扑快照一次性生成，可重跑 |

**工具覆盖**

| Server | 端口 | 工具数 | 数据文件 | 说明 |
|---|---|---|---|---|
| pinpoint | 8888 | 17 | 20 | 含 `_apps.json`（108 应用 + 调用关系）、`_arch.json`（架构规则） |
| prometheus | 8090 | 36 | 38 | 含 `_up_table.json`（330 台主机存活表） |
| cmdb | 8061 | 5 | 7 | 含 `_db_servers.json`（45 条资产记录） |
| es | 9201 | 5 | 3 | `_indices.json`（索引清单）、`_log_counts.json`（按主机/级别的日志计数） |
| **合计** | — | **63** | **68** | 与平台 `config.go` 默认地址完全一致；另在 `mockdata/` 根目录有 1 个 `_meta.json` 生成清单（108 应用 / 330 主机 / 45 条 CMDB 记录），不计入工具响应文件 |

> 其中 prometheus 的 `delete_series`、`clean_tombstones`、`snapshot`、`reload`、`quit` 5 个管理类工具数据也已备好，但仍被平台网关默认拉黑（`MCP_DENY_TOOLS`），演示时不会误触。

**M2–M5 新增 mock 覆盖**

| 工具 | mock 行为 | 支撑场景 |
|---|---|---|
| `healthy` | `data.health=ok`、递增 uptime | 排障验真 `sourceHealth[promthus]=ok` |
| `targets_metadata` | 元数据含 `up` 及各主机指标 | 区分「未纳管」与「抓取失败」 |
| `series` | 返回全部 330 台主机的 `up` 实例 | 证明序列存在，空即 `NO_DATA` |
| `get_shards` | 全部 `STARTED`，无未分配分片 | ES 日志可信性验真 |
| `range_query` | 按主机生成 61 个采样点，部分主机近 20 分钟上行 1.8 倍 | 劣化判定（前后 1/3 均值比 ≥1.3） |
| `drag_heatmap_tool` | 返回 `traceIds=["mock-trace-<fnv32(app)>"]` | 从热力图下钻到单笔 trace |
| `get_transaction_trace_tool` | 扁平 `callStack`（`parentId` 关联），首个下游 span 在 `h%3==0` 时放大为根 `elapsed` 的 6/10 | 调用链瓶颈占比 ≥50% 判据 |
| `search`（ES） | 按 `host` 过滤返回计数，未知应用无候选主机时不返回 | 日志取证；无候选主机直接跳过，避免假证据 |

---

## 三、数据不是假编的：来自真实拓扑

全部数据以 `topo/tprel/data/operation_topology.json` 的 **2026-10-08 / PRO** 快照（1418 行）为源生成，保证与平台拓扑页看到的内容对齐：

- **108 个应用**（含 Apollo 归属、部门、负责人、中间件依赖清单、上下游调用关系）
- **330 台中间件主机**：mysql 54、redis 38、rocketmq 191、kafka 26、elasticsearch 12、nacos 6、zookeeper 3
- **45 条 CMDB 资产**：33 条对应拓扑真实主机（含 `usingBy` 反查应用名）+ 12 条刻意的「孤儿资产」

因此演示时：拓扑页点某个应用 → 诊断页能看到同一应用的 Pinpoint 服务地图 → 稽核页能对该主机的 CMDB 对账出结论，**三页数据自洽**。

---

## 四、埋点剧本（每类结论都有真实样本）

| 稽核项 | 含义 | 埋点主机 | 预期结论 |
|---|---|---|---|
| C1 存在性 | 拓扑引用了，但 CMDB 查无此机 | `public-push1.rocketmq.ehome.dlifeinner.cn`<br>`common1.redis-cluster.ehome.dlifeinner.cn` | **P1 风险 ×2**（账实不符） |
| C2 状态一致性 | CMDB 非在线，但仍有应用依赖 | `public-push3.rocketmq.ehome.dlifeinner.cn`（state=4 已报废）<br>`sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn`（state=6 待清退） | **P1 风险 ×2** + 一致 ×16 |
| C4 指标佐证 | CMDB 说下线，Prometheus 还有没有心跳 | 报废机 `public-push3…`（up=1）<br>待清退机 `sdk3a4…`（up=0） | **双向各 1 条**：一条「CMDB 状态疑似过期」，一条「与 CMDB 一致」 |
| C3 反向孤儿 | CMDB 在线，但拓扑没引用 | `10.214.100.2` ~ `10.214.105.7` 等 12 台 | **P3 ×6**（state=0 抽样 20 条中命中 6 条） |

**告警侧**另有 6 条 firing 告警（TargetDown ×2、HostHighCPU、HighErrorRate、MySQL 主从延迟、RocketMQ 消费积压），其中 `TargetDown` 指向的正是 C2 的两台主机——**稽核与告警能互相印证**，这是演示的关键说服点。

---

## 五、平台怎么用、怎么串起来（先看这一节）

### 5.1 一句话理解

> 四个上游 MCP（pinpoint / prometheus / cmdb / es）提供数据 → 三类检测（告警 / 巡检 / 稽核）产出风险 → 自动排障定位根因 → 自动建单 → 结果回写拓扑与知识库 → 考核指标看板统计闭环效率。

页面只是这四条主线的入口和结果展示，数据流转在后台由平台后端串起来，不需要人工搬运。

### 5.2 四条主线

```
                 ┌──────────── mock 上游（4 个 MCP，63 个工具）────────────┐
                 │  pinpoint 8888   prometheus 8090   cmdb 8061   es 9201  │
                 └───────┬───────────────┬──────────────┬────────┬─────────┘
                         │               │              │        │
              ┌──────────▼───────────────▼──────────────▼────────▼─────────┐
              │                    MCP 网关（探活 / 工具清单 / 调用审计）  │
              └──────────────────────────────┬─────────────────────────────┘
                                             │
        ┌────────────────────┬───────────────┴───────────────┬─────────────────────┐
        ▼                    ▼                               ▼                     ▼
   ① 告警中心           ② 主动巡检                     ③ 资源稽核            ④ 拓扑与影响面
   （含业务监控扫描）    （T1–T11 规则）                （C1–C4 对账）        （/topology、/op-topology）
        │                    │                               │                     │
        └──────────┬─────────┴───────────────┬───────────────┘                     │
                   ▼                         ▼                                     │
             自动排障会话 ──定位根因──▶ 工单 ──引用回写──▶ 拓扑节点着色 ◀───────────┘
                   │                         │
                   └──────────▶ 历史故障知识库 ◀──────────┘
                                     │
                                     ▼
                            考核指标看板（1/5/10 分钟达标率、自动化率）
```

| 主线 | 入口 | 后台做了什么 | 结果去哪看 |
|---|---|---|---|
| **① 告警闭环** | `/alert-center` 点「采集」；或 `/biz-monitor` 点「立即扫描」 | 拉取告警 → 指纹幂等入库 → 四道准入 → 自动开排障 → 逐项取证（topo / prometheus / cmdb / pinpoint / es 日志 / rule）→ 定位根因 → 自动建单 | 告警抽屉时间线、`/diagnosis` 证据链、`/tickets` 工单、`/op-topology` 红虚线/橙描边、`/knowledge` 归档 |
| **② 主动巡检** | `/inspect-plans` 看规则 → `/inspection` 运行批次 | 按 T1–T11 规则扫拓扑与上游，产出风险项与报告 | 批次详情、报告（TOPO_T7/T9/T10/T11），风险项回流告警中心 |
| **③ 资源稽核** | `/audit` 点「执行稽核」 | C1 存在性 / C2 状态一致性 / C3 反向孤儿 / C4 Prometheus 指标佐证，账实对账 | 批次 `8 项 / 风险 4 / 正常 3 / 不适用 1`（种子场景），风险项可开排障/建单 |
| **④ 拓扑与影响面** | `/topology` 选应用；`/op-topology` 看全局 | `/api/topo/op/graph` 计算 `diagRefs` 与 `ticketRefs` 并回写节点状态 | 影响面爆炸半径、节点五级描边、详情面板引用项 |

> 补充两条横向能力：`/degradation`（指标劣化扫描 → 隐患告警）与 `/quality`（质差分析 → 报告 + 根因候选），结论同样回流告警中心与知识库；`/metrics` 汇总上述全流程的时间戳，输出达标率与自动化率。

### 5.3 页面地图（30 个路由）

| 分组 | 页面 |
|---|---|
| 总览 | `/dashboard` |
| 智能监控排障 | `/diagnosis` 排障工作台、`/log-analysis` 日志分析、`/biz-monitor` 外部业务接口监控、`/alert-center` 告警中心、`/tickets` 隐患与工单 |
| 巡检与治理 | `/inspection` 巡检任务与报告、`/inspect-plans` 巡检方案与脚本、`/audit` 资源稽核、`/relations` 关系修正、`/quality` 质差分析、`/degradation` 指标劣化 |
| 拓扑与架构 | `/op-topology` 运维关系拓扑、`/topology` 资源拓扑与影响面、`/topo-sync` 拓扑数据同步 |
| 变更与度量 | `/change/plans` 变更方案、`/change/risk` 风险评估、`/metrics` 考核指标看板 |
| 通用能力 | `/mcp` 数据与能力接口、`/knowledge` 历史故障库 |
| 规划中 / 部分实现 | `/diag-flows`、`/change/execution`、`/samples`、`/features`、`/preprocess`、`/algorithms`、`/training`、`/models`、`/resources`、`/platform` |

### 5.4 5 条示例场景分别去哪看

| 场景 | 主看点 | 联动看点 |
|---|---|---|
| D1 应用错误率飙升（`apollo-ehome-pro`） | 告警中心 → 应用视角排障 11 步 → 工单 | 业务监控扫描、知识库、指标看板 |
| D2 MySQL 主从延迟（`ehome.mysql`） | 中间件视角排障 9 步 | 资源稽核 C1/C4 |
| D3 待下线主机失联（`sdk3a4…`） | 巡检 T7 down、`TargetDown` 告警、host up=0 | 排障 CMDB 交叉验证 |
| D4 报废主机仍上报指标（`public-push3…`） | 资源稽核 C2、巡检 T11 CPU 91% | 主机排障、工单 |
| D5 配置引用主机缺 CMDB（`public-push1…`） | 资源稽核 C1、拓扑影响 11 个应用 | 中间件排障、关系修正 |

### 5.5 推荐演示动线（按顺序点，可直接对照下文步骤）

| 序 | 页面 | 操作 | 对应章节 |
|---|---|---|---|
| 1 | `/dashboard` | 打开，看 KPI 与上游探针 4/4 | — |
| 2 | `/mcp` | 切换上游下拉，看 17/36/5/5 共 63 个工具 | 第 1 步 |
| 3 | `/alert-center` | 点「采集」，点开种子告警看时间线 | 第 4 步 |
| 4 | `/diagnosis` | 打开 HighErrorRate 会话看 11 步证据链 | 第 3 步 |
| 5 | `/tickets` | 看自动生成工单 | 第 4 步 |
| 6 | `/op-topology` | 看图例「排障中 / 工单待处理」与节点描边 | — |
| 7 | `/inspection` | 跑一次批次，看风险项与报告 | 第 4.9 步 |
| 8 | `/audit` | 点「执行稽核」→「详情」看 C1/C2/C4 | 第 2 步 |
| 9 | `/biz-monitor` | 点「立即扫描」，3 条规则越界回流告警 | 第 4.10 步 |
| 10 | `/degradation` | 点「立即扫描」，看劣化项与处置状态 | 第 4.5 步 |
| 11 | `/knowledge` | 点「详情」看归档案例与证据链 | 第 4.11 步 |
| 12 | `/metrics` | 看 1/5/10 分钟达标率与每日趋势 | 第 4.7 步 |

### 5.6 常用命令

```bash
# 健康检查（storage / topo / mcp / now）
curl -s http://127.0.0.1:8080/api/health

# 外部日志分析服务是否就绪（/log-analysis 页据此决定嵌入还是降级提示）
curl -s http://127.0.0.1:8080/api/logs/health

# 手动触发三条主线
curl -X POST http://127.0.0.1:8080/api/alarms/poll
curl -X POST http://127.0.0.1:8080/api/audit/run
curl -X POST http://127.0.0.1:8080/api/inspect/run

# 容器日志
docker compose -f ai-ops/platform/docker-compose.yml logs -f backend mock-mcp frontend

# 重置演示数据（回到 5 条种子场景）
python3 ai-ops/platform/backend/scripts/seed_demo.py
docker compose -f ai-ops/platform/docker-compose.yml restart backend mock-mcp
```

> 默认关闭定时巡检（`INSPECT_CRON_ENABLED=false`），告警轮询 60 秒（`ALARM_POLL_INTERVAL_SEC=60`）；演示时「采集 / 扫描 / 执行稽核」都是手动按钮，节奏可控。业务数据落在 `platform/backend/data` 的 JSON store，重启不丢。

---

## 六、演示流程（约 20 分钟，可按需裁剪 4.5–4.11）

### 第 0 步｜启动（终端 2 个）

```bash
# 一键：Docker 全链路（造数 + 编译 + mock 上游 + 后端 + 前端）
cd ai-ops/platform && ./scripts/docker_up.sh

# 或本地文件模式：造数 + mockmcp + 后端（加 --frontend 连前端一起起）
cd ai-ops/platform && ./scripts/run_demo.sh
```

手动分终端等价于：

```bash
# 终端 A：Mock 上游服务（4 端口，数据读 backend/mockdata）
cd ai-ops/platform/backend
go run ./cmd/mockmcp

# 终端 B：平台后端
cd ai-ops/platform/backend
go run ./cmd/server
```

前端：`cd ai-ops/platform/frontend && npm run dev` → 打开 `http://localhost:5173`。

> Vite 已配置 `server.host = true`，同时监听 IPv4/IPv6；`http://localhost:5173` 与 `http://127.0.0.1:5173` 均可访问（此前仅监听 IPv6，`127.0.0.1:5173` 会被拒）。

> 若 8888/8090/8061/9201 被占用，可用 `PINPOINT_PORT` / `PROMETHEUS_PORT` / `CMDB_PORT` / `ES_PORT` 调端口，并在平台侧同步改 `PINPOINT_MCP_URL` 等环境变量（ES 对应 `ES_MCP_URL`）。

### 第 1 步｜证明「四个上游服务都活着」（1 分钟）

打开平台「上游接口」页，或直接看接口：

```bash
curl -s http://127.0.0.1:8080/api/mcp/probe
```

预期：4 个 server 全部 `reachable: true`，工具数 **17 / 36 / 5 / 5**（pinpoint / prometheus / cmdb / es）。
ES mock 位于 `:9201`，数据来自 `mockdata/es/_indices.json` 与 `_log_counts.json`；本地演示 T10 日志巡检需启动后端时带 `ES_MCP_URL=http://127.0.0.1:9201/mcp`。

**讲解词**：「这四个数就是生产环境四个上游服务的真实接口量，现在全部指向本地文件，断网也能跑。」

### 第 2 步｜跑一次资源稽核（3 分钟）

「资源稽核」页点「开始稽核」，或：

```bash
curl -s -X POST http://127.0.0.1:8080/api/audit/run \
  -H 'Content-Type: application/json' -d '{"maxHosts":20}'
```

预期（实测值）：**28 项发现，11 项风险**

| 检查项 | 风险 | 一致/未触发 |
|---|---|---|
| C1 存在性 | 2 (P1) | — |
| C2 状态一致性 | 2 (P1) | 16 |
| C3 反向孤儿 | 6 (P3) | — |
| C4 指标佐证 | 1 (P1) | 1 |

**讲解词**：「这 11 条风险不是随机造的。C1 的两台是 RocketMQ、Redis 集群域名级组件，CMDB 只登记了 IP 没登记域名，这是真实系统里最常见的账实不符形态；C4 那条更有意思——CMDB 标了已报废，Prometheus 还在采心跳，说明 CMDB 状态过期了。」

### 第 3 步｜从拓扑进排障（3 分钟）

在拓扑页选中 `ehome-pro-server` 的 MySQL 依赖，点「智能排障」，症状填「接口超时」，或：

```bash
curl -s -X POST http://127.0.0.1:8080/api/diagnose/sessions \
  -H 'Content-Type: application/json' \
  -d '{"appId":"apollo-ehome-pro","middlewareType":"mysql","addr":"ehome-pro.mysql.ehomeinner.cn:13303","symptom":"接口超时","env":"PRO","date":"2026-10-08","viewMode":"folded"}'
```

预期（实测）：会话状态 `root_caused`，置信度 **0.75**，问题单 `confirmed / warning`；11 条证据链步骤全部 `ok`，另有 4 条 `sourceHealth` 验真记录（promthus ×3 + es ×1）：

| 步 | 数据源 | 工具 | 结果 |
|---|---|---|---|
| 1 | topo | 影响面分析 | ok（本地拓扑，不调上游服务） |
| 2 | promthus | `list_alerts` | ok（命中症状关键词） |
| 3 | promthus | `query`（`up`） | ok（主机存活证据） |
| 4 | cmdb | `cmdb_server_query` | ok（资产归属 / 在线状态） |
| 5 | pinpoint | `list_applications_tool` | **matched=true**（应用被真实命中） |
| 6 | pinpoint | `get_server_map_tool` | ok（上下游边有真实应用名） |
| 7 | promthus | `range_query` | ok（`node_load1` 前后基线采样 ≥2 点） |
| 8 | pinpoint | `drag_heatmap_tool` | ok（返回 `traceIds`） |
| 9 | pinpoint | `get_transaction_trace_tool` | ok（子 span 占比判瓶颈） |
| 10 | log | `search`（ES） | ok；ES 不可用时回退本地 `log_analyzer`，两者都失败才 `skipped` |
| 11 | rule | 排序定级 | ok（证据集驱动置信度） |

> 演示提醒：请求中的 `appId` 必须是快照中真实存在的应用（如 `apollo-ehome-pro`），否则拓扑影响面为空、Pinpoint 不匹配，平台会判定为弱证据（`weakEvidence=true`）：置信度封顶 0.5、会话转 `investigating`、工单降为 P2，不再误开 P1。可用它现场演示「证据不足不硬下结论」。
>
> 需要日志证据时，在请求体加 `"logPath":"/Users/a666/aiops-logs/172.17.228.214.log"`（或在部署环境配置 `LOG_PATH_TEMPLATE`，默认 `/tmp/aiops-logs/{host}.log`），并保证 `LOG_ANALYZER_URL` 指向的日志分析服务可用；第 10 步成功时前端会渲染「日志证据」卡（命中文件/总行数/错误数/警告数 + 错误类型 TopN + 关键问题）。

**讲解词**：「第 2 步的 matched 是真实字符串匹配，不是写死的 true——说明 Pinpoint 应用清单里确实有 `apollo-ehome-pro`；第 2 步的 3 个命中来自那 6 条告警。」

### 第 4 步｜告警中心闭环（2 分钟）

打开「告警中心」菜单（`/alert-center`），或直接：

```bash
curl -s -X POST http://127.0.0.1:8080/api/alarms/poll     # 手动拉一轮
curl -s http://127.0.0.1:8080/api/alarms/stats            # 分级统计
curl -s http://127.0.0.1:8080/api/alarms                  # 列表（含排障/工单关联字段）
```

预期：6 条 mock 告警入库（2 critical TargetDown + 4 warning），已过准入的自动开出排障会话并回填 `diagSessionId` / `ticketId`；页面顶部 4 张统计卡、表格 6 行、分级药丸红/橙。

**mock 生命周期（按 mockmcp 进程启动时间分桶）**：

| 窗口 | `list_alerts` 返回 | 演示效果 |
|---|---|---|
| 0–90s | 全 6 条 firing | 首次 `poll` → `upserted=6`，触发自动排障 |
| 90–150s | 4 条（去掉 2 条 TargetDown） | 连续 2 轮 poll（`ALARM_MISS_TOLERANCE=2`）后 TargetDown → `resolved`，工单追加"告警已恢复" |
| 150s+ | 恢复全 6 条 | 再次 firing（`fireCount` 累加，代表"死灰复燃"） |

**讲解词**：「这不是静态列表——它按进程运行时间模拟了'告警消失又复发'，所以恢复检测和工单回写是真跑出来的。」

### 第 4.5 步｜指标劣化 → 隐患告警（S1，1 分钟）

页面点「扫描劣化」，或：

```bash
curl -s -X POST http://127.0.0.1:8080/api/alarms/degradation/scan
```

预期：`scanned=10`、`degraded≈9`，告警中心出现若干「指标劣化」标签行。mockmcp 的 `range_query` 按 host 哈希让**一半目标呈上升趋势、一半平稳**，因此这一结果每轮稳定复现。

**讲解词**：「传统阈值告警只在'已经超线'时响；这里是在'还没超线但持续爬升'时就给 warning，并且和 Prometheus 告警走同一套去重、恢复、自动排障。」

### 第 4.6 步｜巡检定时调度（S2，1 分钟）

在「巡检中心 → 新建任务」填 Cron `* * * * *`（每分钟），保存后等一个分钟边界，批次列表会出现 `Trigger=cron` 的批次。

```bash
curl -s -X POST http://127.0.0.1:8080/api/inspect/tasks \
  -H 'Content-Type: application/json' \
  -d '{"name":"演示-每分钟","scene":"topo","cron":"* * * * *","scope":{"env":"PRO"}}'
```

预期：调度器每 20s tick 一次，同一分钟只触发一次；手上有 `running` 批次时跳过，不会与手动批次撞车。非法表达式（如 `0 2 * *`）创建时直接 400。

### 第 4.7 步｜考核指标看板（S3，1 分钟）

左侧「运营指标 → 考核指标看板」，切 7 / 30 / 90 天窗口。

```bash
curl -s 'http://127.0.0.1:8080/api/metrics/kpi?days=7' | python3 -m json.tool
```

预期：4 张卡（1 分钟发现 / 5 分钟定位 / 10 分钟闭环 / 巡检自动化）、时延分布表（P50 / P90 / 达标率）、自动化率进度条、每日趋势柱图。数据来自前几步产生的告警 / 排障 / 工单 / 巡检批次，**不需要额外造数**。

**讲解词**：「这些数字不是手填的，是整条链路跑出来的。发现慢在哪一段、自动化的比例多少，看板直接对得上任务书的 1/5/10 分钟与自动化率考核项。」

### 第 4.8 步｜Alertmanager 抑制联动（S4，1 分钟）

演示点：上游静默 / 抑制的告警**不会**触发自动排障与工单，解除后自动恢复采集。

1）启动一个本地 Alertmanager 模拟服务（仅演示用，返回 `/api/v2/alerts`）：

```bash
cat > /tmp/am_alerts.json <<'EOF'
[{"labels":{"alertname":"DemoSilenced","instance":"10.9.9.9:9100","severity":"critical","group":"demo"},
  "annotations":{"summary":"上游静默演示"},"startsAt":"2026-11-04T09:00:00+08:00",
  "status":{"state":"suppressed","silencedBy":["silence-1"],"inhibitedBy":[]}}]
EOF
python3 - <<'EOF' &
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        body = open('/tmp/am_alerts.json', 'rb').read()
        self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a): pass

HTTPServer(('127.0.0.1', 9093), H).serve_forever()
EOF
```

2）触发一次采集，然后看告警中心：

```bash
curl -s -X POST http://127.0.0.1:8080/api/alarms/poll
curl -s 'http://127.0.0.1:8080/api/alarms?q=DemoSilenced' | python3 -m json.tool
```

预期：`suppressedBy="silenced"`，`diagSessionId` / `ticketId` 为空；前端状态列显示紫色「上游静默」标签。

3）webhook 入站与恢复（不依赖 Alertmanager 服务）：

```bash
curl -s -X POST http://127.0.0.1:8080/api/alarms/webhook \
  -H 'Content-Type: application/json' \
  -d '{"version":"4","status":"firing","groupLabels":{"alertname":"DemoWebhook","instance":"10.8.8.8:9100"},
       "alerts":[{"status":"firing","labels":{"alertname":"DemoWebhook","instance":"10.8.8.8:9100","severity":"warning"},
       "annotations":{"summary":"webhook 入站演示"}}]}'
```

预期：返回 `{"ingested":1,...}`；再发同 fingerprint 且 `"status":"resolved"` 的载荷，返回 `{"resolved":1}`，告警状态转为「已恢复」。

**讲解词**：「上游已经静默的告警，平台不会重复打扰；静默解除后又能自动接上排障，这就是告警运营里最重要的“防噪声”能力。」

### 第 4.9 步｜运营闭环补强（阶段四 T8–T11，3 分钟）

**T8 指标劣化独立页**：打开「指标劣化」菜单（`/degradation`），点击「立即扫描」。

```bash
curl -s -X POST http://127.0.0.1:8080/api/alarms/degradation/scan
curl -s http://127.0.0.1:8080/api/alarms/degradation | python3 -m json.tool
```

预期：页面四张卡显示扫描目标 / 劣化指标 / 受影响主机 / 自动排障；`lastScan` 含 `windowMin`、`thresholdPct`、`at`，刷新与重启后仍在。

**T9 巡检 RISK → 告警**：用全规则任务跑一次巡检，再按来源筛选告警。

```bash
curl -s -X POST http://127.0.0.1:8080/api/inspect/run -H 'Content-Type: application/json' \
  -d '{"scene":"topo","scope":{"env":"PRO"}}'
curl -s 'http://127.0.0.1:8080/api/alarms?pageSize=200' | python3 -c "
import json,sys
for a in json.load(sys.stdin)['items']:
    if a.get('source')=='inspection': print(a['state'], a['alertName'], a['labels'].get('taskId'))"
```

预期：出现 `source=inspection` 的告警，fingerprint 以 `inspection|taskId|ruleCode|objectKey` 开头；符合准入的高风险项自动开排障。下一轮未再命中的旧风险自动转「已恢复」。

**T10 根因准确率回填**：在「排障工作台」打开任一会话，找到「根因准确率回填」卡片，选择「结论准确 / 不准确」并提交；随后看「考核指标」。

```bash
curl -s -X POST http://127.0.0.1:8080/api/diagnose/sessions/<sessionId>/feedback \
  -H 'Content-Type: application/json' -d '{"accurate":true,"note":"演示标注","operator":"demo"}'
curl -s 'http://127.0.0.1:8080/api/metrics/kpi?days=7' | python3 -c "import json,sys;print(json.load(sys.stdin)['accuracy'])"
```

预期：`accuracy={labeled,accurate,rate,coverage}`；`labeled=0` 时看板显示 0% 并在提示中说明「未标注」。

**T11 通知出站**：起一个本地接收端并带 `ALARM_NOTIFY_URL` 启动后端，然后点「通知记录 → 发送测试」。

```bash
# 终端 A：接收端
python3 - <<'EOF'
from http.server import BaseHTTPRequestHandler, HTTPServer
class H(BaseHTTPRequestHandler):
    def do_POST(self):
        n=int(self.headers.get('Content-Length',0)); print(self.rfile.read(n).decode())
        self.send_response(200); self.end_headers(); self.wfile.write(b'{"ok":true}')
    def log_message(self,*a): pass
HTTPServer(('127.0.0.1',9199),H).serve_forever()
EOF

# 终端 B：带配置启动
ALARM_NOTIFY_URL=http://127.0.0.1:9199/notify ALARM_NOTIFY_TOKEN=demo ./server_linux

# 测试投递
curl -s -X POST http://127.0.0.1:8080/api/alarms/notifications/test
curl -s 'http://127.0.0.1:8080/api/alarms/notifications?limit=5' | python3 -m json.tool
```

预期：接收端收到 `{"source":"aiops-platform","type":"test","count":1,...}` 且带 `Authorization: Bearer demo`；告警中心「通知记录」抽屉显示 `sent / HTTP 200`。不配置 `ALARM_NOTIFY_URL` 时链路零网络调用，抽屉提示「未启用」。

**讲解词**：「阶段四补的是运营闭环的四块短板：劣化有独立视角、巡检发现能进同一告警闭环、根因质量可量化、告警能按需出站，并且全部复用现有告警表与准入规则，没有新增中间件。」

### 第 4.10 步｜阶段五感知扩面（T12–T14 + S5，3 分钟）

**T12 外部业务接口监控**：打开「外部业务接口监控」（`/biz-monitor`），点「立即扫描」。

```bash
curl -s http://127.0.0.1:8080/api/biz-monitor/rules | python3 -m json.tool
curl -s -X POST http://127.0.0.1:8080/api/biz-monitor/scan | python3 -m json.tool
```

预期：默认 3 条规则（错误率 / TP99 / 请求量），首轮 `breaches=3, created=3`，告警中心出现 `source=biz` 条目；立刻再扫一次 `created=0`（同 fingerprint 幂等）。页面「一键排障」会带 appId 跳排障工作台。

**T13 质差分析**：打开「质差分析」（`/quality`），点「开始分析」。

```bash
curl -s -X POST http://127.0.0.1:8080/api/quality/scan \
  -H 'Content-Type: application/json' -d '{"appId":"apollo-ehome-pro","windowMin":60}' | python3 -m json.tool
curl -s http://127.0.0.1:8080/api/quality/reports | python3 -m json.tool
```

预期（实测）：`degraded=true`、`score≈1.0`、`rootCause=database / ehome-pro.mysql.ehomeinner.cn:13303`，报告落库；详情页可直接跳排障。

**T14 告警 ↔ 拓扑双向钻取**：告警中心任一告警行点「拓扑定位」，拓扑视图自动聚焦并高亮该节点；反向在拓扑节点详情点「查看告警」，回到告警中心并带上该节点过滤。

**S5 日志证据**：在第 3 步请求体加 `logPath`（见第 3 步说明），或先在「日志分析」页跑一次真实文件。需要本地日志服务时启动 `log_analyzer`（默认 `http://127.0.0.1:8000`，`LOG_ANALYZER_URL` 可改）。

预期（实测）：第 10 步 `ok`，前端「日志证据」卡显示总行数 / 错误数 / 警告数、错误类型 TopN 与关键问题（真实样例 10 行 / 0 错误，约 1.4s）；服务不可用或路径不存在时该步 `skipped`，会话结论不受影响。

**讲解词**：「阶段五把'发现'从只有 Prometheus 告警，扩展到业务接口指标与端到端质差；日志证据补上最后一类现场证据——找不到日志时宁可标 skipped，也不编结论。」

### 第 4.11 步｜阶段六数据治理与知识沉淀（T16–T17，3 分钟）

**T16 关系修正**：打开「关系修正」（`/relations`），点「扫描差异」。

```bash
# 双来源扫描：拓扑 Diff + 最近一次稽核 C3 孤儿
curl -s -X POST http://127.0.0.1:8080/api/relations/scan \
  -H 'Content-Type: application/json' -d '{}' | python3 -m json.tool
curl -s http://127.0.0.1:8080/api/relations/stats | python3 -m json.tool
curl -s 'http://127.0.0.1:8080/api/relations?status=pending' | python3 -m json.tool
```

预期（实测）：`added=0, removed=0, orphans=6, created=6`；立刻再扫一次 `created=0, skipped=6`（稳定指纹去重，人工审批不被覆盖）。

页面上挑一条建议审批（干净种子态下只产出 6 条孤儿建议，没有「缺失依赖」项）→「审批」→「通过」→ 行内「执行修正」，然后查看下方「修正留痕」表：

```bash
curl -s -X PATCH http://127.0.0.1:8080/api/relations/<id> \
  -H 'Content-Type: application/json' -d '{"status":"approved","note":"确认漏采"}' | python3 -m json.tool
curl -s -X POST http://127.0.0.1:8080/api/relations/<id>/apply | python3 -m json.tool
curl -s http://127.0.0.1:8080/api/relations/overrides | python3 -m json.tool
```

预期：孤儿建议审批后状态变 `approved`，执行返回 400 并提示「无法自动修正，请人工确认纳管或清退」，`relation_override` 不新增；若存在 `action=add` 缺失依赖建议，则执行后状态 `approved → applied`、`relation_override` 多一条 `action=add`，重复执行返回 200 且不重复留痕；已 `applied` 不可再审批。

**T17 历史故障知识库**：打开「历史故障知识库」（`/knowledge`），搜索「告警」。

```bash
curl -s http://127.0.0.1:8080/api/knowledge/stats | python3 -m json.tool
curl -s 'http://127.0.0.1:8080/api/knowledge?q=告警&limit=5' | python3 -m json.tool
curl -s 'http://127.0.0.1:8080/api/knowledge/similar?appId=apollo-ehome-pro&symptom=告警' | python3 -m json.tool
```

预期（实测，干净种子态）：`total=10, apps=1, reviewed=1, accuracy=100.0`；`q=告警` 在 `limit=5` 下命中 5 条并带 `score`；相似案例按分数排序；点开任一案例，抽屉展示根因 / 置信度 / 证据链（样例 11 条）与「以该对象重新排障」入口。

**讲解词**：「阶段六没有新增任何采集：关系修正吃的是已有的拓扑 Diff 和稽核孤儿，知识库吃的是已有的排障会话与工单。修正走'建议—审批—执行—留痕'，不直接改底座；知识只做归档与检索，不做自动改写。这两件事把'数据'变成可审计的'治理动作'和可复用的'经验资产'。」

### 第 5 步｜收口（1 分钟）

一句话：**「拓扑 → 诊断 → 稽核 → 告警 → 业务监控/质差 → 关系修正/知识库」主链路的数据源全部打通，且都指向本地文件或确定性模拟，演示不依赖现场网络；外部日志服务不可用时自动降级不影响结论。**

---

## 七、数据是怎么组织的

```
backend/mockdata/
├── _meta.json                  # 规模、埋点清单（核对用）
├── pinpoint/
│   ├── _tools.json             # tools/list 的 17 个工具定义
│   ├── _apps.json              # 108 应用（含中间件依赖/上下游）
│   ├── _arch.json              # 架构规则版本/阈值/违规清单
│   └── <tool>.json  ×17        # 每个工具一份形态数据
├── prometheus/
│   ├── _tools.json             # 36 个工具定义
│   ├── _up_table.json          # 330 主机 up 表（2 台为 0）
│   └── <tool>.json  ×36
└── cmdb/
    ├── _tools.json             # 5 个工具定义
    ├── _db_servers.json        # 45 条资产（真实 CMDB 字段结构）
    └── <tool>.json  ×5
```

- **静态工具**：`tools/call` 直接返回 `<tool>.json` 文件内容，改数据只需改文件、重启服务。
- **参数相关工具**（7 个）：由服务端按参数动态生成，保证同一工具不同入参返回不同结果——
  - `cmdb_server_query`：按 `ip` / `state` / 分页过滤 45 条资产
  - `query` / `range_query`：解析 `up{instance=~"host.*"}` 查 330 台主机存活表
  - `label_values`：按 `label` 返回 instance 全集或 job 名
  - `host_metrics_realtime` / `host_metrics_trend`：按主机名哈希生成 12 项指标（同一主机多次调用值稳定）
  - `get_server_map_tool` / `drag_heatmap_tool` / `get_transaction_trace_tool`：按应用名 / traceId 生成
  - `list_alerts`：**不读 `prometheus/list_alerts.json`**，由 `promListAlerts(startTime)` 按进程运行时间分桶返回 6 条模板（0–90s 全量 / 90–150s 去掉 TargetDown / 150s+ 恢复），用于演示 alert 生命周期

---

## 八、边界说明（演示时如实交代）

1. **不是真实业务数据**：全部由 2026-10-08 生产拓扑快照派生，指标数值为确定性模拟（哈希生成，可复现但非实测）。
2. **只有 1 天快照**：跨日期对比（如变更前后 diff）会因只有 `2026-09-29 / 09-30 / 10-08` 三天而同源，演示时间对比建议固定 10-08。
3. **CMDB 抽样是 45 条**：真实环境是万级，故 C3 孤儿率看着偏高（12/45≈27%），汇报时应说明「已按比例压缩，真实环境需全量扫描」。
4. **切真实环境零改动**：去掉 Mock 服务、把四个 `*_MCP_URL` 指向真实地址即可，平台代码无 mock 分支。
5. **告警生命周期只在进程启动后 2.5 分钟内切换**：长演示建议在讲解告警中心前重启 mockmcp（或后续加 `ALARM_MOCK_SCENARIO` 环境变量固定阶段）。
6. **日志证据依赖外部服务**：`log_analyzer` 不在本 Mock 四端口范围内，ES mock 不可用且未启动 log_analyzer 或路径不在其白名单时第 10 步为 `skipped`；外部服务已知遗留为 LLM 接口 404 与 `report/generator.py` 的 `ProcessingResult.get`，不影响规则报告生成与平台解析。`/log-analysis` 页已改为后端探测 + 友好降级（`GET /api/logs/health`），服务未就绪时展示启动指引而非错误页面。

### 8.1 2026-11-10 全栈复测基线（可直接对照）

| 指标 | 实测值 |
|---|---|
| 上游 probe | pinpoint 17 / prometheus 36 / cmdb 5 / es 5，全部 reachable |
| 运维关系拓扑 | 1580 节点 / 23399 边，57 个节点带排障或工单状态引用 |
| 全量资源稽核 | 21 项 / 14 风险 |
| 单 host 定向稽核 | 3 项 / 2 风险（C3 返回 `NOT_APPLICABLE`） |
| 巡检 | 102 项 / 39 风险 |
| 告警中心 | 127 条（inspection 20 条，其中 4 条已自动排障并建单） |
| 工单 | 105 张（全 open） |
| 身份映射 | 108 应用 / 330 主机 |
| 应用维度排障 | 11 步 `root_caused`，置信度 0.64（覆盖 topo / prometheus / cmdb / pinpoint / log / rule） |
| 中间件维度排障 | 9 步 `root_caused`，置信度 0.79 |
| 前端页面 | 30 个路由全站巡检：0 白屏 / 0 报错页；20 个已实现页面全部 PASS，8 个规划中页面 + 2 个部分实现页面正常展示占位内容（详见 8.2） |

### 8.2 浏览器端验收（2026-11-10，Docker 模式逐页实测）

`http://127.0.0.1:5173` 全站 30 路由逐页打开验证，结论如下：

> 数字口径说明：8.1 为多次运行累积后的库状态（告警 127 / 工单 105 / 巡检风险 39），8.2 为 `seed_demo.py` 重置并按第六章流程跑完后的干净种子态（告警 59 / 自动排障 18 / 工单 18 / 巡检 102 项 41 风险）。两组都是真实实测，演示前建议先重置，即可稳定复现 8.2 这一组。

| 分组 | 页面 | 结论 | 说明 |
|---|---|---|---|
| 总览 | `/dashboard` | PASS | KPI 卡 + 上游探针 4/4 |
| 智能监控排障 | `/alert-center` | PASS | 59 条告警，采集/抽屉时间线正常 |
| | `/diagnosis` | PASS | 18 个自动排障会话，证据链完整 |
| | `/tickets` | PASS | 18 张工单，详情抽屉正常 |
| | `/biz-monitor` | PASS | 3 条规则扫描，越界 3 条回流告警 |
| | `/log-analysis` | PASS | 已接入 log_analyzer（宿主机 `:8020`），iframe 正常加载；服务未起时自动降级为「启动指引」，不再渲染错误页面 |
| 巡检与治理 | `/inspection` | PASS | 102 项 / 41 风险，批次与报告正常 |
| | `/inspect-plans` | PASS | 规则库 11 条，参数区间正常（已修复） |
| | `/audit` | PASS | 8 项 / 风险 4 / 正常 3 / 不适用 1 |
| | `/relations` | PASS | 关系修正列表与操作正常 |
| | `/quality` | PASS | 质差分析报告与根因候选正常 |
| | `/degradation` | PASS | 指标劣化扫描与处置状态正常 |
| 拓扑与架构 | `/op-topology` | PASS | 1580 节点 / 23399 边，红虚线/橙描边图例正常 |
| | `/topology` | PASS | 应用影响面与引用项正常 |
| | `/topo-sync` | PASS | 快照列表与同步状态正常 |
| 变更与度量 | `/change/plans` | PASS | 生成方案 + 详情抽屉正常（已修复白屏） |
| | `/change/risk` | PASS | 与 `/change/plans` 共用同一组件，风险评估列表正常 |
| | `/metrics` | PASS | 时延分布样本列 19 正常（已修复） |
| 通用能力 | `/mcp` | PASS | 上游切换，工具数 17/36/5/5 |
| | `/knowledge` | PASS | 4 个归档案例，列宽与横向滚动正常（已修复） |
| 规划中 / 部分实现 | `/diag-flows`、`/change/execution`、`/samples`、`/features`、`/preprocess`、`/algorithms`、`/training`、`/models`、`/resources`、`/platform` | PASS（占位） | 共 10 个（8 个规划中 + 2 个部分实现），均正常展示占位内容，无白屏无报错 |

**本轮发现并已修复的 4 处前端缺陷**

| 页面 | 现象 | 原因 | 修复 |
|---|---|---|---|
| `/change/plans`、`/change/risk` | 生成方案或点「详情」白屏，控制台 `TypeError: Cannot read properties of null (reading 'length')` | 后端 `risk.blockers` 为 nil，JSON 序列化为 `null`，前端直接 `.length` | `(viewing.risk.blockers ?? [])`、`(viewing.risk.blockers ?? []).map(...)`、`dataSource={viewing.steps ?? []}` 三处兜底 |
| `/metrics` | 时延分布表「样本」列恒为 0，而 KPI 卡显示 19 个样本 | 字段层级取错，`samples` 实际在 `r.source` 内 | 改为 `r.source.samples` 渲染 |
| `/knowledge` | 案例表「案例」「根因」列被压成 0 宽，文字重叠 | 未设列宽、无横向滚动 | 「案例」`width: 280`、「根因」`width: 240`、`scroll={{ x: 1430 }}`、副文本 `maxWidth: 260` |
| `/inspect-plans` | 标题写死「内置规则库（6 条）」实际 11 条；参数区间显示 `(1-undefined)` | 计数硬编码；`min`/`max` 缺省未判断 | 改为动态计数 `${rules.data?.rules?.length ?? 0} 条`；区间按 `min/max` 是否存在输出 `(a-b)` / `(≥a)` / `(≤b)` |

修复后均已重建前端镜像（`docker compose build frontend`）并回归，全部 PASS；当前线上 bundle 为 `index-DD4WmWc6.js`。

**环境依赖（演示时如实说明）**

- `/log-analysis` 接入的是独立的 `log_analyzer`（FastAPI，源码在本机 `~/Documents/trae_projects/log/log_analyzer`），不属于平台四路上游 MCP。页面为**后端探测 + 友好降级**：`GET /api/logs/health` 由后端访问 `LOG_ANALYZER_URL/api/health`，探明「可用 / 连不上 / 端口被其他服务占用」三种情况；服务在才嵌入 iframe，不在则显示探测详情、启动命令与配置说明。
- 本机 8000 已被 `prometheus-mcp-server` 容器占用，日志服务统一跑在 **8020**（启动命令：`cd ~/Documents/trae_projects/log && PYTHONPATH=$PWD .venv/bin/python -m uvicorn log_analyzer.web.app:app --host 0.0.0.0 --port 8020`，首次导入约 4 分钟才监听）。前端构建时设 `VITE_LOG_ANALYZER_URL=http://127.0.0.1:8020`，后端 `LOG_ANALYZER_URL` 由 compose 默认为 `http://host.docker.internal:8020`（健康探测与排障取证共用）。当前实测：探测 `reachable=true`、`/log-analysis` iframe 正常加载。
- 平台其他能力不依赖该服务：排障日志证据优先走 ES mock（`:9201`），ES 不可用且 `log_analyzer` 不可用时该步自动 `skipped`。
- 前端改动后必须 `docker compose build frontend` 才会生效（容器是 `COPY dist` 静态文件），仅 `up -d` 不会更新；浏览器若命中旧 bundle 缓存，带 `?v=` 参数或强制刷新即可。

---

## 九、后续可做（如需要）

| 优先级 | 事项 | 说明 |
|---|---|---|
| P0 | 演示前健康自检脚本 | 一键校验 3 端口 + probe + 一次稽核出预期数字，避免现场掉链子 |
| P1 | 数据可配置化 | 把埋点清单抽成 `scenario.json`，支持「故障剧本」切换（如全下线 / 大面积孤儿） |
| P2 | 快照补全 | 补齐更多日期，支持变更前后对比演示 |
