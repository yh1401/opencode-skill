# 视联 AI 运维平台 — 总体架构与实现方案（框架先行版）

> 版本：v0.8（2026-11-13 按当前实现回填第 0/2.2/2.4/3.3/4/9/12/14 章：部署形态、技术栈、页面路由、编排层现状、API 清单、存储 docType、工具数量口径、菜单地图）
> 历史：v0.7（2026-11-13 回填第 0/2.4/4/9/12 章基础事实）；v0.6（2026-11-13 新增第 14 章「平台如何运转：已落地能力与菜单地图」）；v0.5（2026-11-10 同步第一批跨页联动断点 ①②③ 与拓扑排障/工单状态着色落地）；v0.4（2026-11-06 同步 M2–M5 落地：数据源验真、问题单、三路取证、证据驱动定因）；v0.3（2026-11-05 合并《排障×数据治理·两场景一体贯通方案》，新增第 13 章）
> 范围：前端框架 / 后端框架 / 拓扑能力融入智能巡检与智能排障 / pinpoint-mcp 与 promthus-mcp 接入细节 / 开源复用映射 / 分期落地 / 两场景一体贯通 / 平台运转形态与菜单地图
> 上位依据：《附件4.1 立项申报 计划任务书——网络运营与信息安全部——视联网端到端统一运维AI 自智能力研发项目》

---

## 0. 结论先行

1. **不造轮子，先搭"骨架 + 网关 + 两个闭环"**。可复用的开源件已经覆盖 60%~70% 的通用能力（Agent 编排、告警收敛、处置执行、图数据库、向量检索、实验追踪），自研集中在三件事：**拓扑与业务语义的对齐**、**巡检/排障两条业务闭环**、**统一接入网关与审计**。
2. **拓扑（topo）以「上下文服务」为主，另配只读浏览页**。核心职责仍是回答「这个应用依赖谁 / 谁依赖这个中间件」，供巡检做**基线 diff 与爆炸半径**、排障做**影响面收敛**；前端在此之上提供 `资源拓扑与影响面`、`运维关系拓扑`、`拓扑数据同步` 三个只读/半只读入口（均标「部分」），不在页面里产出结论。
3. **上游服务不直连 Agent，统一走统一接入网关**（初版为 pinpoint / prometheus 两个；2026-10-28 增 cmdb，2026-11-06 增 es，现为四个，接入方式一致）。原因很具体：pinpoint-mcp 只有 streamable-http、无健康检查、无工具开关；promthus-mcp 有 `/metrics`、有工具白名单但会被 `--prometheus.backend` 覆盖。两者语义、错误模型、鉴权方式都不同，必须由网关抹平。
4. **对齐键是最大风险**。topo 的 `appKey = appId|depProject|node`（折叠/展开两种口径），Pinpoint 的 application name 是另一套命名，中间只能靠 pinpoint-mcp 自带的 `build_app_mapping_tool` / `filter_dep_projects_tool` 做桥接。这一层必须单独建表固化，不能每次实时算。

---

## 1. 现状盘点（可复用的既有资产）

### 1.1 拓扑服务 topo

| 维度 | 事实 |
| --- | --- |
| 技术栈 | Go + Gin + GORM v2 + zap，母工程 module `devops-automation`，目录内无 `go.mod`（需寄宿母工程） |
| 对外接口 | 7 条，全部挂在 `/api/external/operationTopology/*`：`/list`、`/graph`、`/detail`、`POST /sync`、`/sync/status`、`/middlewareTypes`、`/sync/logs` |
| 数据模型 | 单事实表 `operation_topology`（26 列），唯一键 `apollo_host_id + apollo_app_id + middleware_type + connect_addr + env + sync_date`；每日快照 |
| 关系链 | 跳①`app_dp_relation`（apollo_app_id → deploy_project_name → node）；跳②`deploy_pinpoint_relation`（dep_project → pp_application）；**node 实际取跳①**（`dp_relation.go:598-599`），跳② 的 node 字段已废弃 |
| 数据源 | `file` / `db` / `auto` 三态；`InitDatastore` 当前**固定 file 模式**；file 模式按 `sync_date` 分桶保留 3 份，tmp+rename 原子写 |
| 采集 | Apollo 配置中心 14 个 namespace，`envs=["PRO"]`，`workers=8`，`scanBudget=60s`；cron `0 30 1 * * ?`（每日 01:30） |
| 图模型 | `GetGraph`：节点 `app:{appKey}` / `mw:{type}:{addr}`，边 `Source=应用 → Target=中间件`，`Kind="depend"` |
| 真实规模 | 折叠口径：应用 108 / 中间件 341 / 边 756；展开口径：应用 240 / 中间件 341 / 边 2943；`/list` 分页总数 1418（折叠） vs 5266（展开） |
| 已知缺口 | 333 个 `dep_project` 缺失、479 个需求点未覆盖、91 个 appId 无数据；ADR/DPR 的 node 同值率仅 21.7%；`pinpoint_relation.go` 有 4 条硬编码补数（只作用于内存） |
| 平台化缺失 | 无租户维度、无 `task_id`（只有进程级 `syncMu` 全局锁）、`/list` 响应信封与其余接口不一致、图 id 跨口径不可复用 |

### 1.2 pinpoint-mcp

| 维度 | 事实 |
| --- | --- |
| 语言/SDK | Python + FastMCP，`mcp>=1.10.0,<2.0.0`、`httpx>=0.27.0`（未显式声明 uvicorn/starlette） |
| 传输 | **仅 streamable-http**，路径 `/mcp`，兼容 `/gateway/pinpoint(/)`，其余 404 |
| 端口 | `MCP_HOST=0.0.0.0`、`MCP_PORT=8888` |
| 工具数 | 25 个（基础 4 + 架构分析 21） |
| 上游 | Pinpoint Web（`/api/applications`、`/api/servermap/serverMap`、`/api/heatmap/drag`、`/api/transaction/trace`）+ EasyViews 网关 5 个内层接口 + Prometheus `/api/v1/query` |
| 认证 | 表单登录 `/login` + Basic + Cookie + `X-Auth-Session`；302/401/403 重登一次 |
| 部署 | 仅 systemd（`User=mcp`、`Restart=always`、`EnvironmentFile=/fortress/pinpoint-mcp/mcp.env`） |
| 限制 | 无分页、无限流、无服务端缓存、无工具级开关、无 Dockerfile/k8s、无健康检查端点；`httpx.AsyncClient(timeout=30.0)` 硬编码；错误多为抛异常（上游 `isError`） |

**25 个工具清单**：`list_applications_tool`、`get_server_map_tool`、`drag_heatmap_tool`、`get_transaction_trace_tool`、`list_nodes_tool`、`get_module_topology_tool`、`resolve_analysis_scope_tool`、`analyze_link_topology_tool`、`get_link_topology_tool`、`get_host_structure_tool`、`get_project_deploy_tool`、`analyze_host_concentration_tool`、`prometheus_query_tool`、`arch_metric_query_tool`、`arch_rule_check_tool`、`list_arch_rules_tool`、`arch_label_probe_tool`、`analyze_node_runtime_tool`、`analyze_nodes_runtime_tool`、`filter_dep_projects_tool`、`build_app_mapping_tool`、`analyze_pinpoint_dependencies_tool`、`summarize_architecture_findings_tool`、`arch_task_open_tool`、`arch_task_status_tool`

**实现形态（2026-11-13 源码核对）**

- 框架与入口：Python + FastMCP，`server.py` 用 `@mcp.tool()` 逐函数注册，函数名即工具名（统一 `*_tool` 后缀）；`_startup()` 预登录 Pinpoint，失败只告警不阻断。
- 传输：仅 Streamable HTTP（`mcp.streamable_http_app()` + uvicorn），端点 `/mcp`，默认 `0.0.0.0:8888`；不支持 stdio。另有路径重写兼容网关前缀 `/gateway/pinpoint`。
- 工具规模：25 个 = 基础 4（`list_applications_tool`、`get_server_map_tool`、`drag_heatmap_tool`、`get_transaction_trace_tool`）+ 架构分析 21。
- 一个进程内挂三个上游客户端：`pinpoint_client.py`（Pinpoint Web，Cookie/Basic/账号密码 formLogin 三种认证，302·401·403 自动重登一次）、`prometheus_client.py`（`PROMETHEUS_BASE_URL`，无认证，5xx 指数退避重试 2 次）、`easyviews_client.py`（统一代理 `getTykjProvince`，凭据放请求体 `provinceDetail`）。因此它实际是「Pinpoint 调用链 + Prometheus 指标 + EasyViews 拓扑」三合一分析网关，`arch_*` 系列工具会跨这三个源取数。
- 旁路 REST：`GET /api/arch/health`、`POST /api/arch/batch`、`GET /api/arch/task/{id}`，供无法继承 MCP 会话的子 Worker 使用，需请求头 `X-Arch-Token`；未配置 `ARCH_API_TOKEN` 时全部返回 403。
- 时间单位：Pinpoint 类工具用**毫秒**；内部 Prometheus 取数工具 `ts/start_ts/end_ts` 用**秒**；EasyViews 链路用 `minute` 分钟。
- 全部只读，无写操作；风险在结果体量（`return_raw_samples=true`、`include_raw=true`、`include_samples=true` 会返回超大数据）与任意 PromQL 可执行。

### 1.3 promthus-mcp

| 维度 | 事实 |
| --- | --- |
| 语言/SDK | Go 1.26 + 官方 MCP go-sdk v1.6.0 |
| 传输 | 默认 stdio；`--mcp.transport=http` 走 `/mcp` |
| 端口 | 代码默认 `:8080`，deploy env 为 `:8090` |
| 工具数 | Prometheus 后端 35 个（Thanos 后端 31 个；并集 36，`list_stores` 仅 Thanos 注册） |
| 配置 | 环境变量前缀 `PROMETHEUS_MCP_SERVER_`（flag → 大写，`.`/`-` → `_`） |
| 关键 flag | `--mcp.tools=all\|core\|<allowlist>`、`--prometheus.url`、`--prometheus.timeout=1m`、`--web.max-requests=40`、`--dangerous.enable-tsdb-admin-tools=false`、`--mcp.session-timeout=10m`、`--mcp.stateless=false` |
| 认证 | Prometheus 默认无认证；支持 `--http.config` 与请求级 `Authorization` 透传；EasyViews 用 `appId/appSecret/province` 放 POST body |
| 错误语义 | 统一 `newToolErrorResult`（`IsError=true` 业务错误体）；404 包装为 `ErrEndpointNotSupported` |
| 部署 | Dockerfile（多阶段、`EXPOSE 8080`、`HEALTHCHECK /metrics`）+ systemd（`DynamicUser=true`）；无 k8s/Helm |
| 限制 | 无重试、无分页、无查询缓存、无工具级限流；EasyViews 内层域名硬编码生产；`--prometheus.backend` 会**整体覆盖** `--mcp.tools` 白名单；`handlers.go` 响应体未加 `io.LimitReader` |

**核心 9 个（`--mcp.tools=core`）**：`docs_list`、`docs_read`、`docs_search`、`query`、`range_query`、`metric_metadata`、`label_names`、`label_values`、`series`

**业务扩展 5 个（视联定制）**：`host_metrics_realtime`、`host_metrics_trend`、`get_module_topology`、`get_chain_topology`、`get_node_list`、`get_virtual_structure`、`get_project_topology`

**实现形态（2026-11-13 源码核对）**

- 框架与入口：Go + 官方 client，`cmd/prometheus-mcp-server/main.go`，kingpin 参数；`pkg/mcp/{tools.go,registration.go,handlers.go,easyviews.go}` 分层。
- 传输：`--mcp.transport=stdio`（默认）或 `http`，HTTP 模式挂 Streamable HTTP 于 `/mcp`，默认 `:8080`；另有 `/metrics`、`/docs/`。
- 工具规模：36 个（并集口径）。`all` + prometheus 后端为 35；`list_stores` 仅 `--prometheus.backend=thanos` 时注册，同时移除 `alertmanagers/config/wal_replay_status/reload/quit`。核心集 `core` 仅 9 个。
- 上游与认证：`--prometheus.url`（默认 `http://127.0.0.1:9090`），`--http.config` 提供 basic_auth / bearer_token / TLS；HTTP 传输时请求头 `Authorization` 可覆盖默认凭据。单实例单基址，**不支持多集群**，多集群需多实例。
- 写操作：`delete_series`、`clean_tombstones`、`snapshot` 需 `--dangerous.enable-tsdb-admin-tools`；`reload`、`quit` 需显式开启生命周期开关。生产接入必须保持默认关闭并纳入网关黑名单。
- 返回形态：`query`/`range_query`/`exemplar_query` 返回 `{"result":"<字符串>","warnings":[...]}`，`result` 是 `model.Value.String()` 的文本行，不是 JSON，需按文本解析。
- EasyViews 同源：`easyviews.go` 的 5 个拓扑工具（`get_node_list`、`get_module_topology`、`get_chain_topology`、`get_virtual_structure`、`get_project_topology`）与 `pinpoint-mcp` 的 `easyviews_client.py` 封装同一个上游网关、同一信封、同一组内层 API。两侧互为镜像，必须指定唯一权威源。

---

## 2. 总体架构

### 2.1 分层

```
┌──────────────────────────────────────────────────────────────┐
│ 接入层  Web 控制台（React） / OpenAPI / 机器人（IM） / 定时器     │
├──────────────────────────────────────────────────────────────┤
│ 应用层  智能巡检 · 智能排障 · 智能数据治理 · 智能割接维护          │
│         （每个能力 = 任务编排 + 规则/模型 + 报告）                │
├──────────────────────────────────────────────────────────────┤
│ 编排层  Agent Runtime（kagent/HolmesGPT 或自研）· 工作流引擎       │
│         工具调度 · 证据收集 · 结论生成 · 人工确认卡点             │
├──────────────────────────────────────────────────────────────┤
│ 工具层  统一接入网关（统一鉴权/限流/审计/探针/结果归一）           │
│         ├─ pinpoint-mcp  25 tools  :8888/mcp                    │
│         ├─ promthus-mcp  35 tools  :8090/mcp                    │
│         ├─ topo-service   7 APIs   /api/external/operationTopology│
│         ├─ apollo-mcp-server                                    │
│         └─ cmdb-mcp-server / arch-opt-mcp-server                │
├──────────────────────────────────────────────────────────────┤
│ 数据层  PostgreSQL(+pgvector) · Redis · Neo4j(二期) · MinIO ·     │
│         ClickHouse(可选，指标/日志长时序)                         │
├──────────────────────────────────────────────────────────────┤
│ 执行层  StackStorm / AWX（变更与处置）· 审批流 · 回滚             │
└──────────────────────────────────────────────────────────────┘
```

### 2.2 已锁定的三项技术决策

| 决策点 | 结论 | 影响 |
| --- | --- | --- |
| 后端控制面语言 | **Go + Gin** | topo 以 Go module 内嵌（补 `go.mod`），不跨语言；前端只对接一套 BFF |
| 编排层 | 规划为 **kagent + HolmesGPT**；当前实现为 **自研规则流水线** | 当前排障/巡检均由 Go 侧引擎按固定步骤执行（拓扑影响面 → 证据采集 → 规则打分 → 结论），证据链与 SSE 已打通；kagent/HolmesGPT 保留为后续可选项，接口契约不变 |
| 一期拓扑维度 | **仅 应用 / 中间件 两级** | 不使用 node、机房维度出结论；报告中显式声明口径 |

### 2.3 关键设计取舍

| 取舍点 | 选择 | 理由 |
| --- | --- | --- |
| 是否另起 Python 服务 | **一期不引入**；仅在需要 MLflow/Evidently 评测时作为离线作业存在 | 决策已定 Go 主栈，避免第二套运行时进入主链路 |
| 图存储 | **一期 PostgreSQL 邻接表 + 物化视图**，二期按需上 Neo4j | 规模仅 341 中间件 / 2943 边，PG 足够；Neo4j 的价值在多跳与路径查询，二期再说 |
| 前端拓扑渲染 | **AntV G6**（与 `prototype/topology_g6.html` 一致） | 已有三视图（two/inst/force）实现，直接复用交互与布局经验，降低重写成本 |
| 上游调用方式 | **网关托管连接 + 服务端会话** | pinpoint-mcp 仅 streamable-http 且会话依赖 Cookie，必须由网关维持长连接 |

### 2.4 部署形态

- **当前实现**：`platform/docker-compose.yml` 一体化编排 4 个常驻容器 — `mock-mcp`（单进程四端口 8888 pinpoint / 8090 prometheus / 8061 cmdb / 9201 es）、`backend`（8080，内嵌 MCP 网关）、`frontend`（5173 → nginx 80，静态站 + `/api` 反代）、`postgres`（55432，文件存储的镜像备份）。
- **对真实上游**：`--profile real-mcp` 起 `pinpoint-mcp` / `prometheus-mcp`（与 mock 端口互斥），后端通过 `PINPOINT_MCP_URL` / `PROMETHEUS_MCP_URL` / `CMDB_MCP_URL` / `ES_MCP_URL` 切换；**上游不对外暴露**。
- **尚未落地**：无 TLS/443、无 K8s 清单、无独立网关进程 —— 网关能力就是后端 `/api/mcp/*`，单实例无 HA。生产化部署需另行补齐。

---

## 3. 后端框架设计

### 3.1 目录骨架

```
backend/
├── cmd/server/main.go
├── internal/
│   ├── api/                 # HTTP 路由、DTO、参数校验
│   │   ├── v1/inspection.go
│   │   ├── v1/diagnosis.go
│   │   ├── v1/topology.go
│   │   └── v1/report.go
│   ├── domain/              # 领域模型与仓储接口（无框架依赖）
│   │   ├── inspection/
│   │   ├── diagnosis/
│   │   └── topology/
│   ├── app/                 # 用例编排（事务边界）
│   │   ├── inspection_run.go
│   │   ├── diagnosis_session.go
│   │   └── impact_analysis.go
│   ├── infra/
│   │   ├── toposvc/         # 内嵌 topo（router+service+model）
│   │   ├── mcpclient/       # 统一接入网关 客户端
│   │   ├── repo/            # GORM 实现
│   │   └── notify/          # IM / 工单
│   ├── gateway/             # 统一接入网关 实现（独立进程亦可）
│   └── pkg/                 # 日志、错误码、雪花 ID、配置
├── configs/
└── migrations/
```

### 3.2 领域模型（DDL 草案）

```sql
-- 巡检任务（周期/一次性）
CREATE TABLE inspection_task (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     VARCHAR(64) NOT NULL DEFAULT 'default',
  name          VARCHAR(128) NOT NULL,
  scene         VARCHAR(32) NOT NULL,          -- middleware_dependency / capacity / ha / change
  cron_expr     VARCHAR(64),
  scope_json    JSONB NOT NULL,                -- {appIds:[], mwTypes:[], envs:["PRO"], nodes:[]}
  rule_set_ver  VARCHAR(32) NOT NULL,
  enabled       BOOLEAN NOT NULL DEFAULT TRUE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 巡检批次（每次调度一个 run，绑定一个 topo 快照）
CREATE TABLE inspection_run (
  id            BIGSERIAL PRIMARY KEY,
  task_id       BIGINT NOT NULL REFERENCES inspection_task(id),
  trigger_type  VARCHAR(16) NOT NULL,          -- cron / manual / api
  topo_sync_date DATE,                         -- 关键：锁定快照，保证可复现
  status        VARCHAR(16) NOT NULL,          -- pending/running/succeeded/failed/partial
  stats_json    JSONB,                         -- {items:120, risk:8, nodata:5, error:1}
  started_at    TIMESTAMPTZ,
  finished_at   TIMESTAMPTZ
);

-- 巡检发现（四态结论，与 arch-opt 口径一致）
CREATE TABLE inspection_finding (
  id            BIGSERIAL PRIMARY KEY,
  run_id        BIGINT NOT NULL REFERENCES inspection_run(id),
  rule_code     VARCHAR(64) NOT NULL,          -- ARCH_D1..D6 / TOPO_T1..
  severity      VARCHAR(8)  NOT NULL,          -- P0..P3
  verdict       VARCHAR(16) NOT NULL,          -- RISK / NOT_TRIGGERED / NO_DATA / NOT_APPLICABLE
  object_type   VARCHAR(16) NOT NULL,          -- app / middleware / host / cluster
  object_key    VARCHAR(256) NOT NULL,         -- app:{appKey} | mw:{type}:{addr}
  evidence_json JSONB NOT NULL,                -- 指标值 / 依赖列表 / 快照 diff
  impact_json   JSONB,                         -- 影响应用数、受影响 appId 列表
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_finding_run ON inspection_finding(run_id, verdict);

-- 排障会话
CREATE TABLE diagnosis_session (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     VARCHAR(64) NOT NULL DEFAULT 'default',
  alert_key     VARCHAR(256),                  -- 指纹：alertname+labels hash
  symptom       TEXT NOT NULL,
  status        VARCHAR(16) NOT NULL,          -- open/investigating/root_caused/closed
  root_cause    TEXT,
  confidence    NUMERIC(4,3),
  impact_json   JSONB,                         -- topo 产出的影响面
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  closed_at     TIMESTAMPTZ
);

-- 证据链（每个工具调用一条，可回放）
CREATE TABLE diagnosis_evidence (
  id            BIGSERIAL PRIMARY KEY,
  session_id    BIGINT NOT NULL REFERENCES diagnosis_session(id),
  step_no       INT NOT NULL,
  source        VARCHAR(32) NOT NULL,          -- pinpoint / promthus / topo / cmdb / apollo
  tool_name     VARCHAR(96) NOT NULL,
  args_json     JSONB,
  result_digest JSONB,                         -- 归一化后的关键字段，不存全量
  latency_ms    INT,
  is_error      BOOLEAN NOT NULL DEFAULT FALSE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 应用命名对齐（跨源桥接表，最关键的"胶水"）
CREATE TABLE app_identity (
  id              BIGSERIAL PRIMARY KEY,
  apollo_app_id   VARCHAR(128) NOT NULL,
  dep_project     VARCHAR(128),
  node            VARCHAR(128),
  pinpoint_app    VARCHAR(256),               -- Pinpoint application name
  eng_project     VARCHAR(128),               -- EasyViews 工程项目
  pp_application  VARCHAR(256),
  confidence      NUMERIC(4,3),
  source          VARCHAR(32),                -- mapping_tool / manual / inferred
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (apollo_app_id, dep_project, node, pinpoint_app)
);
```

### 3.3 API 契约（v1）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/topology/graph?appKey=&syncDate=&expand=` | 代理 topo `/graph`，补齐统一信封与 `syncDate` 参数 |
| GET | `/api/v1/topology/impact?mwType=&addr=&syncDate=` | **新增聚合接口**：某中间件地址 → 依赖它的应用全集（爆炸半径） |
| GET | `/api/v1/topology/impact?appKey=&direction=up\|down` | 某应用 → 上下游中间件全集 |
| GET | `/api/v1/topology/diff?from=&to=` | **新增**：两个 `sync_date` 快照的依赖 diff（新增/消失） |
| POST | `/api/v1/inspection/tasks` | 建任务 |
| POST | `/api/v1/inspection/tasks/{id}:run` | 手动触发，返回 `runId` |
| GET | `/api/v1/inspection/runs/{id}` | 批次详情 + findings 分页 |
| GET | `/api/v1/inspection/runs/{id}/report` | 报告（md/json） |
| POST | `/api/v1/diagnosis/sessions` | 建会话（入参：告警或自然语言症状） |
| POST | `/api/v1/diagnosis/sessions/{id}:diagnose` | 触发诊断，SSE 流式返回步骤与证据 |
| GET | `/api/v1/diagnosis/sessions/{id}/evidence` | 证据链 |
| POST | `/api/v1/diagnosis/sessions/{id}:confirm` | 人工确认根因/执行处置 |
| GET | `/api/v1/reports/{id}` | 统一报告出口 |

> 实现说明：上表是 v1 设计契约。当前落地版本已收敛为 `/api/topo/*`、`/api/inspect/*`、`/api/diagnose/sessions*` 三组路径，未使用 `/api/v1` 前缀，也没有 `:run` / `:diagnose` / `:confirm` 这类动作式 URI；以第 12.5 节的实际 API 清单为准。

**统一响应信封**（对齐 topo `/list` 与其余接口不一致的问题，平台侧一律用这一层）：

```json
{ "code": 0, "message": "ok", "requestId": "req_...", "data": {}, "traceId": "..." }
```

### 3.4 任务模型

- 所有长任务（巡检、排障、topo 同步）统一抽象为 `job`：`jobId + type + status + progress + resultRef`。
- **不用进程内锁**：topo 的 `syncMu` 是进程级全局锁，平台侧必须改为**分布式锁（Redis）+ 幂等键**，否则多副本部署会串。
- 状态机：`pending → running → (succeeded | failed | partial)`；`partial` 用于部分数据源不可用（如 Pinpoint 超时），报告必须显式标注。

---

## 4. 前端框架设计

### 4.1 技术选型

| 项 | 选择 | 说明 |
| --- | --- | --- |
| 框架 | React 18 + TypeScript + Vite 5 | 与 topo 原型（纯前端 HTML/JS）解耦；构建产物由 nginx 托管 |
| UI | Ant Design 5.21 | 表格、抽屉、时间轴、Popover 齐全，设计 token 见《前台设计系统规范》 |
| 拓扑渲染 | AntV G6 v5（`@antv/g6 ^5.0.45`） | 运维关系拓扑与资源拓扑使用；旧 tprel 原型页仍用内置 `g6.min.js` |
| 图表 | 原生 SVG + CSS 绘制 | **未引入 ECharts**；趋势/条形/漏斗均为轻量自绘，减少包体 |
| 数据层 | TanStack Query 5 | 缓存 + 轮询任务状态 + 变更后失效 |
| 路由 | React Router 6 | 菜单项与路由 key 同源（`nav.tsx` → `ALL_ITEMS`），未知路径回落总览 |
| 流式 | SSE（`EventSource`） | 排障过程逐步呈现，不用 WebSocket 降低运维复杂度 |
| 状态 | useState + Context + localStorage | **无 Zustand**：跨页只保留主题（`aiops-theme`）与菜单偏好（`aiops-menu-prefs`） |

### 4.2 页面结构（与当前路由一致）

```
/                         → 重定向到 /dashboard
/dashboard                总览（告警态势 + 巡检健康度 + 最近排障）
/alert-center             告警中心（列表/详情、自动排障、静默、通知测试）
/diagnosis                排障工作台（发起、SSE 证据链、历史会话、反馈）
/log-analysis             日志分析（嵌入 log_analyzer，健康探测失败时降级提示）
/biz-monitor              外部业务接口监控（概览 / 规则 / 扫描）
/tickets                  隐患与工单（状态流转、排障回填）
/diag-flows               排障流程管理（占位，规划）
/inspection               巡检任务与报告（任务、批次、四态看板、findings）
/inspect-plans            巡检方案与脚本（规则集配置）
/change/plans             变更方案（模板生成、影响面）
/change/risk              风险评估（Diff + 历史故障）
/change/execution         执行与验证（占位，规划）
/op-topology              运维关系拓扑（file 本地计算 / http 透传）
/audit                    资源稽核（拓扑 ↔ CMDB 对账、Prometheus 佐证）
/relations                关系修正（建议 → 审批 → 执行留痕）
/topology                 资源拓扑与影响面（只读浏览）
/topo-sync                拓扑数据同步（file 模式只读）
/resources                运维对象（部分占位）
/quality                  质差分析（扫描、报告）
/degradation              指标劣化（劣化扫描与概览）
/metrics                  考核指标看板（时延 P50/P90、达标率、自动化率）
/mcp                      数据与能力接口（工具清单、试跑、黑名单、审计）
/knowledge                历史故障库与知识库（检索、相似案例）
/platform                 平台服务（部分占位）
/samples /features /preprocess /algorithms /training /models
                          模型与算法 6 项（占位，整组可隐藏）
*                         未知路径 → 回落 /dashboard
```

菜单侧栏：8 个分组 29 项 + 总览；底部「菜单显示」按规划中功能 / 模型与算法 / 智能割接维护 / 运营指标与评价体系四组显隐（localStorage 持久化），另有「展开全部 / 合并全部」一键切换。详见第 14.4、14.6 节。

### 4.3 三个核心组件

1. **四态结论看板**：🟥 已发现风险 / 🟩 未触发规则 / 🟨 数据不足 / ⚪ 不适用。与 `arch-opt-query` skill 的结论口径保持一致，避免"巡检报告和架构报告两套说法"。
2. **影响面拓扑**：以告警对象为中心，一层层外扩（应用 → 中间件 → 依赖该中间件的其它应用），节点大小 = 依赖方数量，颜色 = 健康度。数据来自 `/api/topo/impact`（`appId` 或 `mwType+addr` 入参）。
3. **证据时间轴**：排障每一步的工具名、入参摘要、关键返回、耗时、是否报错。**必须可回放**，这是"定位准确率 >50%"能自证的前提。

### 4.4 前端必须处理的三个"脏点"

- `appKey` 折叠/展开两套口径 → 前端用统一 `viewMode` 全局开关，切换时重取而不是本地重算。
- 图 id 跨口径不可复用 → 节点选中态按 `appId`（不是 `appKey`）持久化。
- topo `/list` 信封与 `/graph` 不同 → 一律经 BFF 归一，前端不接触原始响应。

---

## 5. 拓扑能力融入「智能巡检」

### 5.1 挂载点总览

| 巡检阶段 | 调用 topo | 用途 |
| --- | --- | --- |
| 开检前（基线固化） | `GET /list?sync_date=` + 平台 `/topology/diff` | 生成本次基线；与上一快照比对出"依赖新增/消失" |
| 巡检中（对象展开） | `GET /graph?appKey=` + 平台 `/topology/impact` | 把"单应用巡检"扩成"依赖链巡检"；计算爆炸半径 |
| 判定时（规则入参） | 依赖清单注入规则上下文 | 让 D1–D6 规则拿到"上下游是谁" |
| 出报告（归因与排序） | 影响面数据 | 按"影响应用数 × 严重度"排序，而不是按规则码排序 |

### 5.2 新增巡检场景（T1–T6 为 topo 独有，T7–T11 为上游证据扩展，非 arch-opt 覆盖）

| 编码 | 场景 | 判定逻辑 | 数据来源 | 四态处理 |
| --- | --- | --- | --- | --- |
| TOPO_T1 | **依赖漂移** | 今日快照 vs 昨日：新增依赖边 / 消失依赖边 | `/list` 两日快照 | 两天都有数据才判定；缺一天 → 🟨 |
| TOPO_T2 | **中间件爆炸半径** | 单个 `mw:{type}:{addr}` 的被依赖应用数 ≥ 阈值（默认 10） | `/graph` 全量聚合 | 无应用数据 → 🟨 |
| TOPO_T3 | **单点中间件** | 某中间件实例数 = 1 且被依赖应用数 ≥ 3 | `/graph` + 实例地址去重 | 地址缺失 → 🟨 |
| TOPO_T4 | **依赖环 / 自依赖** | 图内出现环，或应用依赖自身 | `/graph` DFS | 图不完整 → 🟨 |
| TOPO_T5 | **孤儿应用** | 应用无任何中间件依赖（可能采集失败，非真无依赖） | `/list` | **必须标 🟨 而非 🟩**，避免误判"健康" |
| TOPO_T6 | **跨环境错配** | 同一 `appId` 在 PRO 依赖的中间件地址出现在非 PRO 清单 | `/list` 按 env 分组 | env 不全 → ⚪ |
| TOPO_T7 | **主机存活** | 巡检范围内中间件主机查 Prometheus `up`；`up=0` 判失联 | Prometheus `query` | 无序列 → 数据不足 |
| TOPO_T8 | **调用链异常** | Pinpoint 服务地图上下游链路错误数 / 平均耗时超阈值 | Pinpoint `get_server_map_tool` | 无应用 / 无调用链 → 数据不足 |
| TOPO_T9 | **CMDB 资产一致性** | 拓扑引用主机与 CMDB 对账：查无资产 / 非在线 / 缺负责人 | CMDB `cmdb_server_query` | 查无记录 → 数据不足 |
| TOPO_T10 | **日志错误** | 按主机统计 ES 近 N 分钟 ERROR/FATAL 日志量超阈值 | ES `search` | 索引缺失 / 查询失败 → 数据不足 |
| TOPO_T11 | **主机指标阈值** | Prometheus 主机实时指标（CPU / 内存 / 磁盘 / TCP 连接数）超阈值 | Prometheus `query` | 无指标 → 数据不足 |

> T1–T6 为拓扑基础规则（2026-10）；T7–T11 于 2026-11-05 扩展，规则注册表见 `internal/inspect/rules.go`（共 11 条）。
>
> T5 是刻意设计的：topo 有 91 个 appId 无数据、333 个 `dep_project` 缺失，"查不到"绝不能等同于"没问题"。

### 5.3 数据结构：规则上下文

巡检执行时，为每个对象构造统一上下文，规则只读上下文，不直接调上游服务：

```json
{
  "object": { "type": "app", "key": "app:10086|dp-order|node-3", "appId": "10086" },
  "topo": {
    "syncDate": "2026-10-27",
    "upstream": [{"type":"mysql","addr":"10.0.0.1:3306","depApps":12}],
    "downstream": [],
    "depApps": 12,
    "prevSyncDate": "2026-10-26",
    "diff": {"added": 1, "removed": 0}
  },
  "metrics": { "cpuMax7d": 0.42, "memMax7d": 0.88 },
  "meta": { "env": "PRO", "engProject": "...", "pinpointApp": "..." }
}
```

好处：规则可单测（喂 JSON 即可），数据获取与判定解耦，四态结论天然可表达。

### 5.4 需要的 topo 侧改造（最小集）

| 改造 | 现状 | 目标 | 优先级 |
| --- | --- | --- | --- |
| 支持 `sync_date` 查询参数 | `/list` 隐含当前快照 | `/list`、`/graph`、`/detail` 显式入参 | P0 |
| 新增批量/反查能力 | `/graph` 按单 appKey | 平台侧 `/topology/impact` 聚合，或 topo 提供 `byMiddleware` | P0 |
| 快照 diff | 无 | 平台侧基于两日 `/list` 计算（不改 topo） | P0 |
| 统一响应信封 | `/list` 与其余不一致 | BFF 归一（不改 topo） | P0 |
| 分布式锁 | 进程级 `syncMu` | Redis 锁（平台侧托管同步任务） | P1 |
| 补数去硬编码 | 4 条硬编码 | 落库为 `app_identity` 的 `manual` 记录 | P1 |
| 补 333 个 `dep_project` | 缺口 | 运营补录 + 自动推断，写入 `confidence` | P2 |

---

## 6. 拓扑能力融入「智能排障」

### 6.1 排障主流程（含 topo 的位置）

```
告警/症状
  │
  ├─① 归一化：解析 labels → appId / mwType+addr / hostIp
  │
  ├─② 身份对齐：app_identity 表 → pinpointApp / engProject
  │
  ├─③ 影响面（topo）：/topology/impact → 候选主机 + 被影响应用
  │
  ├─④ 发现：promthus-mcp list_alerts + query(up)
  │
  ├─⑤ 数据源验真（M2）：healthy → targets_metadata → series → get_shards
  │      └─ 结果写 sourceHealth；unavailable/degraded 时结论置信度封顶 0.5
  │
  ├─⑥ 确认与定界：cmdb_server_query 归属 → 生成问题单（DiagnosisTicket）
  │      └─ confidence = confirmed / degraded / no_data
  │
  ├─⑦ 定位：pinpoint-mcp list_applications_tool + get_server_map_tool
  │
  ├─⑧ 取证：range_query 基线 → drag_heatmap_tool → get_transaction_trace_tool
  │        → ES search 日志（失败回退本地 log-analyzer）
  │
  ├─⑨ 定因：rank 由证据集推导置信度
  │      └─ 子 span 占根 elapsed ≥50% 判调用链瓶颈；无证据强制「疑似」
  │
  └─⑩ 人工确认 → 处置建议（StackStorm/AWX 剧本）→ 复盘入库
```

> 前端排障工作台（`/diagnosis/:id`）在步骤流之外新增两张卡：**问题单**（实体/级别/信号/时间窗/影响面/可信度）与**数据源验真**（逐源状态/检查项/说明）。接口字段见 `internal/domain/models.go` 的 `DiagnosisTicket` / `SourceHealth`。

#### 置信度计算（M5，`internal/diagnose/engine.go`）

置信度是**规则式推导**，不是 LLM 打分：先按证据集算基础置信度，再乘以各根因规则的强度系数。

```text
base = min(0.9, 0.25 + 0.10 × 证据来源数 + 0.02 × min(证据条数, 10))
候选置信度 = round2(base × 规则强度系数)
无任何有效证据时 base = 0.2
```

证据来源共六类：`topo` / `pinpoint` / `promthus`（告警 + 指标）/ `log` / `cmdb` / `rule`，同一来源的多条证据累加为条数，条数最多计 10 条。

| 根因候选规则 | 强度系数 |
|---|---|
| 共享中间件爆炸半径 ≥ 阈值 | 1.0 |
| 调用链单 span 占比 ≥50% | 1.0 |
| CMDB 已非在线且无心跳 | 1.0 |
| 活跃告警命中 | 0.95 |
| 日志错误激增 | 0.85 |
| CMDB 状态过期但仍有心跳 | 0.85 |
| 主机负载持续劣化（node_load1 上升 >30%） | 0.8 |
| 应用仅依赖单一中间件实例 | 0.7 |
| CMDB 查无资产（账实不符） | 0.7 |
| 影响面收敛、更像应用侧问题 | 0.6 |
| Pinpoint 未覆盖 | 0.45 |
| 兜底「证据不足」 | 0.35 |

三道强制降权：零有效证据 → 全部候选压到 0.2 并标记「疑似」；弱证据（拓扑为空且 Pinpoint 未匹配）→ 最高候选压到 0.5；数据源验真 `degraded` / `unavailable` → 全部候选封顶 0.5。

会话级 `confidence` 取第一候选，状态机为 `sourceCount==0 → no_data`、`≥0.6 → root_caused`、其余 `investigating`；工单准入 ≥0.3，优先级 `≥0.8→P0 / ≥0.6→P1 / ≥0.4→P2 / 其余→P3`。

### 6.2 三种典型链路与调用的具体接口

**A. 应用变慢（有明确 appId）**
1. topo `/graph?appKey=` → 拿到依赖的中间件清单（如 3 个 MySQL + 1 个 Redis）。
2. pinpoint-mcp `get_transaction_trace_tool` → 找出耗时最长的下游调用。
3. 用 trace 中的下游地址与 topo 的 `connect_addr` 做**地址级对齐**（这是最可靠的对齐方式，比应用名对齐稳）。
4. promthus-mcp `host_metrics_realtime` / `range_query` 验证该中间件宿主机资源。
5. 若多个不相关应用同时报障且都依赖同一 `mw:{type}:{addr}` → 直接收敛到中间件，跳过应用侧排查。

**B. 中间件故障（告警只给地址）**
1. topo `/topology/impact?mwType=mysql&addr=10.0.0.1:3306` → 直接给出**受影响应用全集**（这是 topo 最不可替代的价值）。
2. 按受影响应用的业务等级排序，输出"影响面清单"进工单。
3. 用 pinpoint-mcp `get_server_map_tool` 交叉验证调用关系。

**C. 大面积抖动（无明确对象）**
1. promthus-mcp `list_alerts` 拉全量告警 → 聚类。
2. 对每个簇取代表对象 → 走 A/B。
3. 用 topo 求多个簇的依赖交集，交集节点即共同上游。

### 6.3 与 pinpoint-mcp 的"对齐三原则"

1. **地址优先**：能对齐 IP:Port 就不对齐应用名（topo 有 `connect_addr`，Pinpoint trace 有下游地址）。
2. **映射落库**：`build_app_mapping_tool` 的结果必须写 `app_identity`，不每次实时调（该工具无缓存）。
3. **低置信度显式标注**：`confidence < 0.7` 的对齐结果在证据链标黄，禁止作为根因唯一依据。

---

## 7. 上游服务接入细节（以 pinpoint / prometheus 为例）

> 2026-11-06 口径：平台已接入**四个**上游服务 —— 除本节两个外，还有 `cmdb-mcp-server`（`cmdb_server_query`）与 `elasticsearch-mcp`（`get_shards` + `search`）。四服务的工具清单、调用矩阵与字段契约见《上游服务数据契约与工具对照》第二部分 §8 / §9.10（条件矩阵见第一部分 §9）。

### 7.1 对照矩阵

| 维度 | pinpoint-mcp | promthus-mcp |
| --- | --- | --- |
| 语言 / SDK | Python / FastMCP | Go / 官方 mcp-go-sdk v1.6.0 |
| 传输 | **仅 streamable-http** | stdio（默认）/ http（`--mcp.transport=http`） |
| 端点 | `http://<host>:8888/mcp`（兼容 `/gateway/pinpoint(/)`） | `http://<host>:8090/mcp`（代码默认 8080，deploy 为 8090） |
| 工具数 | 17 | 35（Prometheus）/ 31（Thanos） |
| 工具开关 | **无** | `--mcp.tools=all\|core\|<allowlist>` |
| 鉴权（对上游） | 表单登录 + Basic + Cookie + `X-Auth-Session` | Prometheus 无认证；可选 `--http.config`；请求级 `Authorization` 透传 |
| 鉴权（对调用方） | **无** | **无**（依赖网关） |
| 错误语义 | 抛异常 → 上游 `isError` | `newToolErrorResult`，`IsError=true`；404 → `ErrEndpointNotSupported` |
| 健康检查 | **无端点** | `/metrics`（Dockerfile HEALTHCHECK） |
| 超时 | `httpx` 30s 硬编码 | `--prometheus.timeout=1m` 可配 |
| 限流 | 无 | `--web.max-requests=40` |
| 部署 | 仅 systemd | Dockerfile + systemd |
| 凭据位置 | `config.py` 明文（3 处）+ `.service` 明文 | `deploy/prometheus-mcp-server.env` 明文 appId/appSecret |

### 7.2 网关规范（统一抹平）

```
POST /mcp-gateway/{server}/{tool}
Header: X-Tenant-Id, X-Request-Id, X-Trace-Id
Body:   { "arguments": {...}, "timeoutMs": 15000 }
Resp:   { "ok": bool, "data": {}, "error": {"code","message","retryable"}, "meta": {"latencyMs","server","tool"} }
```

网关必做 6 件事：

1. **探针**：pinpoint-mcp 用 `initialize` + `tools/list` 做应用层探针（无 HTTP 健康端点）；promthus-mcp 用 `/metrics` + `healthy` 工具。
2. **工具白名单**：按场景下发。巡检只开只读工具；`delete_series`、`snapshot`、`reload`、`quit`、`clean_tombstones` 默认**全禁**（`--dangerous.enable-tsdb-admin-tools=false` 且网关二次拦截）。
3. **超时与重试**：默认 15s；只对幂等只读工具重试 1 次；`analyze_*` 类长工具不重试，改异步 job。
4. **结果归一**：把 pinpoint 的 raw dict 与 promthus 的 TSDB matrix 都归一为 `{series|records, unit, window}`，Agent 侧不感知差异。
5. **审计**：每次调用落 `diagnosis_evidence`（或独立 `tool_call_log`），含 tenant、session、入参摘要、耗时、是否错误。
6. **凭据托管**：各上游服务的明文凭据全部改环境变量 / K8s Secret，网关不接触上游密码，只做路由。

### 7.3 工具分工（按场景，不按服务）

| 场景 | pinpoint-mcp | promthus-mcp | topo |
| --- | --- | --- | --- |
| 应用清单 | `list_applications_tool`、`list_nodes_tool` | — | `/list` |
| 调用链 | `get_transaction_trace_tool`、`analyze_link_topology_tool`、`get_link_topology_tool` | — | `/graph` |
| 服务地图 | `get_server_map_tool`、`get_module_topology_tool` | — | `/graph` |
| 资源指标 | `prometheus_query_tool`（兜底） | `query`/`range_query`/`host_metrics_*` | — |
| 容量集中度 | `analyze_host_concentration_tool` | `range_query` + 聚合 | `/topology/impact` |
| 架构规则 | `list_arch_rules_tool`、`arch_rule_check_tool` | — | 依赖数据注入 |
| 身份对齐 | `build_app_mapping_tool`、`filter_dep_projects_tool` | — | `app_identity` |
| 部署结构 | `get_project_deploy_tool`、`get_host_structure_tool` | `get_project_topology`、`get_virtual_structure` | 跳①/跳② 关系 |

> 存在**功能重叠**（拓扑类工具在三个源都有）。规则：**拓扑类一律以 topo 为准**，Pinpoint/promthus 的拓扑工具只用于"交叉验证"，不用于结论。原因：topo 是唯一带 Apollo 配置语义与快照能力的源，另两个是运行时视图，口径不可比。

### 7.4 上线前必须修的 6 个坑

1. `--prometheus.backend` 会**整体覆盖** `--mcp.tools` 白名单 → 部署脚本禁止同时使用，改用显式 allowlist。
2. pinpoint-mcp 的 `WEB_BASE_URL` 含 `/serverMap` 后缀且有绝对路径拼接 → 需实测确认，否则 trace 类工具全 404。
3. pinpoint-mcp 无 `uvicorn/starlette` 显式依赖 → `requirements.txt` 补齐并锁定版本。
4. promthus-mcp `handlers.go` 响应体未加 `io.LimitReader` → 大范围 `range_query` 可能打爆内存，网关侧限制 `step` 与时间窗。
5. EasyViews 内层域名硬编码生产 → 非生产环境无法联调，需参数化。
6. 上游服务都**无调用方鉴权** → 必须只在集群内网暴露，禁止直接挂公网 Ingress。

---

## 8. 开源项目映射（哪些已经有了）

| 能力域 | 候选开源 | 成熟度 | 结论 | 说明 |
| --- | --- | --- | --- | --- |
| Agent 运行时 / 工具编排 | **kagent**（CNCF sandbox） | 中 | **直接用 + 轻定制** | K8s 原生 Agent CRD，支持上游接口接入；适合作为"编排层" |
| 开箱 RCA / 排障推理 | **HolmesGPT**（CNCF sandbox） | 中 | **直接用 + 换数据源** | 已有告警→证据→根因范式与 Prompt 资产；把数据源换成我们的上游服务 |
| 告警接入 / 去重 / 收敛 | **Keep** | 中 | **直接用** | 多源告警归一、去重、工作流；替代自研告警总线 |
| 处置编排 / 变更执行 | **StackStorm**、**AWX**、**Rundeck** | 高 | **直接用（选一）** | StackStorm 事件驱动强，AWX 适合 Ansible 批量；割接自动化优先 AWX |
| CMDB / 资产模型 | **NetBox**、**Nautobot** | 高 | **二期评估** | 若现有 CMDB 可用则不自建；Nautobot 的 GraphQL 更利于拓扑融合 |
| 图存储 / 依赖查询 | **Neo4j**（社区版） | 高 | **二期** | 一期 PG 足够；多跳路径查询与影响面扩散时再上 |
| 向量检索 / 案例库 | **pgvector** / **Milvus** | 高 | **pgvector 直接用** | 与 PG 同库，运维成本最低；规模上来再换 Milvus |
| 实验追踪 / 模型管理 | **MLflow** | 高 | **用（限 AI 能力）** | 规则阈值寻优、模型版本管理 |
| 超参 / 阈值寻优 | **Optuna** | 高 | **用** | 巡检阈值（如爆炸半径=10）自动寻优 |
| 标注 / 评测集 | **Label Studio**、**Evidently** | 高 | **用** | 构建"隐患/故障"标注集与准确率评测，支撑 >80% 验收 |
| AIOps 评测基准 | **AIOpsLab**（MIT） | 中 | **参考** | 借鉴其故障注入 + 评测方法论，不直接生产部署 |
| 混沌 / 故障注入 | **LitmusChaos** | 高 | **用（验证阶段）** | 验证排障准确率与恢复时长，支撑 1-5-10 指标 |
| 可视化 / 大盘 | **Grafana**（AGPLv3） | 高 | **用（注意 License）** | 内嵌大盘可以；对外商业化分发需评估 AGPL |
| 拓扑渲染 | **AntV G6** | 高 | **直接用** | 已有三视图原型，直接复用 |
| 工作流 / DAG | **Temporal** 或 **Argo Workflows** | 高 | **二选一** | 长任务、可重试、可回放；巡检与排障都用它替代手写状态机 |
| 统一接入网关 | **自研（薄）** | — | **必须自研** | 现成网关（如 mcp-proxy）不满足租户/审计/工具策略/结果归一 |
| 身份对齐（appId ↔ Pinpoint ↔ EasyViews） | 无 | — | **必须自研** | 这是本项目的核心业务资产，无开源可替 |
| 巡检规则库（D1–D6 + TOPO_T*） | 部分（arch-opt-query skill） | 中 | **自研为主** | 已有 v3.1.0 规则与阈值体系，直接演进 |
| 视联业务语义（产品/机房/工程项目） | 无 | — | **必须自研** | CMDB/Apollo 的业务含义无法外部化 |

**复用率估算**：通用基础设施（告警、编排、执行、存储、检索、评测、可视化）约 60%~70% 可直接复用；业务语义与闭环逻辑（约 30%~40%）必须自研。

---

## 9. 分期落地

| 阶段 | 目标 | 交付物 | 验收口径 | 截至 v0.7 状态 |
| --- | --- | --- | --- | --- |
| **M0 骨架** | 前后端框架跑通，上游服务能调通 | Go 控制面 + React 控制台 + 统一接入网关 + **4 个上游探针**；`/api/topo/impact` 可用 | 能手动发起一次"输入 appId → 输出依赖清单 + 影响面" | ✅ 已落地（compose 4 容器，43 个 GET 接口全通） |
| **M1 智能巡检 MVP** | 巡检闭环 | 任务/批次/发现三张表；**T1–T11** 规则接入；四态报告 | 巡检自动化率 >95%（定时任务无人工介入）；报告可复现（锁定 `sync_date`） | ✅ 已落地（调度 20s tick，含拓扑/指标/MCP/日志/trace 五类规则） |
| **M2 智能排障 MVP** | 排障闭环 | 会话/证据链；三条典型链路（A/B/C）；SSE 流式 | 1 分钟发现、5 分钟定位有数据支撑；定位准确率基线可测 | ✅ 已落地（SSE、证据链、工单回填、反馈沉淀；13 处工具调用点） |
| **M3 准确率与自动处置** | 指标达标 | 案例库；StackStorm/AWX 剧本；人工确认卡点 | 隐患与故障发现准确率 >80%、定位准确率 >50%、自动处置率 >80%、10 分钟恢复 | ◐ 部分：案例库已上线（关键词重合度检索，**无 pgvector**）；自动处置未做 |
| **M4 数据治理与割接** | 另两条能力线 | 数据质量规则、割接编排与回滚 | 割接自动化 >75% | ◐ 部分：稽核 C1/C2/C4、关系修正、质差报告已上线；割接执行与回滚未做 |

> M0 的关键不是功能，而是**把"对齐"这件事先做完**：`app_identity` 表 + 地址级对齐 + 快照锁定。这三件事不做，后面所有准确率都无法归因。

---

## 10. 风险与待确认

| # | 风险 | 影响 | 处置 |
| --- | --- | --- | --- |
| R1 | topo 无 `go.mod`，不能独立构建 | 无法独立部署与单测 | 补 `go.mod`，或明确寄宿母工程并只做冒烟测试 |
| R2 | 333 个 `dep_project` / 91 个 appId 无数据 | 影响面算不全，误判"无依赖" | 全部结论落 🟨；运营补录进 `app_identity.confidence` |
| R3 | ADR/DPR 的 node 同值率 21.7% | 节点维度归因不可信 | **决策：一期不出 node 维度结论**，仅 app / 中间件两级 |
| R4 | 折叠/展开两套口径 | 同一对象两种影响面 | 平台强制单一 `viewMode`，报告标注口径 |
| R5 | pinpoint-mcp 无工具开关、无健康检查 | 无法做精细权限与 SLA | 网关白名单 + 应用层探针 |
| R6 | 上游服务明文凭据 | 安全合规 | 全部改 Secret，作为 M0 前置项 |
| R7 | `运维拓扑.md` §4.4/§4.5 写"node 取自跳②"，与代码相反 | 后续开发踩坑 | 文档更正，以代码为准（跳① ADR） |

---

## 11. 下一步（v0.7 之后的待办）

M0 清单已全部交付（控制面、六张表、网关最小版、聚合接口、前端骨架）。当前编排仍是**规则流水线**，未引入 kagent + HolmesGPT；以下是 v0.7 之后仍未闭环的待办：

1. **接真实上游**：`--profile real-mcp` 替换 mock，校准 pinpoint 17 / prometheus 36 / cmdb 5 / es 5 的真实返回与超时；把明文凭据迁到 Secret。
2. **鉴权与多租户**：当前 API 无鉴权、无租户隔离，需接入 SSO + RBAC 后才可对外。
3. **存储主备切换**：PG 目前只是文件存储的镜像，需把 `store` 主实现切到 PG（含迁移脚本与回滚）。
4. **准确率闭环**：知识库改向量检索（pgvector），用 `diagnosis_feedback` 统计定位准确率基线；补齐自动处置剧本（StackStorm/AWX）与人工确认卡点。
5. **割接执行**：`/change/execution` 从占位变为可执行的编排与回滚，配审批流。
6. **可选**：若引入 kagent/HolmesGPT 做多智能体编排，保持 `/api/diagnose/sessions` 契约不变，仅替换内部执行器。

验收：真实上游下「输入 appId → 依赖清单 + 影响面 + 证据链」可复现，且报告锁定 `sync_date`。

---

## 12. 附录：可运行骨架（已落地）

本章记录 M0 骨架的实际落地形态，与第 3/4 章的规划差异以本章为准。

### 12.1 目录

```
ai-ops/platform/
├── backend/                      # Go + Gin 控制面
│   ├── cmd/server/main.go        # 装配 + 优雅关闭
│   ├── internal/config/          # 环境变量
│   ├── internal/domain/          # 领域模型（前后端 JSON 契约）
│   ├── internal/topo/            # 双数据源：file / http
│   ├── internal/store/           # 文件为主 + PG 备份镜像
│   ├── internal/mcp/             # streamable-http 客户端 + 网关
│   ├── internal/inspect/         # TOPO_T1–T11 规则引擎
│   ├── internal/diagnose/        # 排障会话 + 证据链
│   ├── internal/alarm/           # 告警采集 + 指纹幂等 + 自动转排障（2026-11-03 新增）
│   ├── internal/tickets/         # 隐患/工单引擎
│   └── internal/api/             # 路由 + SSE
├── frontend/                     # React 18 + Vite + AntD 5 + G6 v5
│   └── src/pages/                # Dashboard/Topology/OpTopology/TopoSync/Inspection/InspectPlans/Diagnosis/AlarmCenter/BizMonitor/Quality/Degradation/Metrics/ChangePlans/Tickets/Audit/Relations/Knowledge/LogAnalysis/McpConsole
└── docker-compose.yml            # mock 上游 + 后端 + 前端 + Postgres（real-mcp profile 接真实上游）
```

### 12.2 启动

**方式一：Docker 一键全链路（含 mock 上游）**

```bash
cd ai-ops/platform
./scripts/docker_up.sh   # 造数 + 交叉编译 + 构建前端 + docker compose up
```

`docker_up.sh` 会先跑 `seed_demo.py`、把后端交叉编译成 linux 二进制、`npm run build` 出 `dist`，再 `docker compose up -d --build`，因此镜像只用本机已有的 alpine / nginx:alpine，离线也能构建。启动后 mock-mcp(8888/8090/8061/9201) + backend(8080) + frontend(5173) + postgres(55432) 全部就绪。

mock 上游直接读仓库内 `backend/mockdata`（bind mount，改完重启 `mock-mcp` 生效），后端状态落 `backend/data`，拓扑读 `topo/tprel/data`。要对真实上游时改用 `--profile real-mcp`（与 mock 端口冲突，不可同时启用）。

**方式二：本地文件模式**

```bash
cd ai-ops/platform
./scripts/run_demo.sh              # 造数 + mockmcp + 后端
./scripts/run_demo.sh --frontend   # 额外起 Vite dev server
```

或分终端手动起：

```bash
cd ai-ops/platform/backend && go run ./cmd/mockmcp   # 终端 A：四上游
cd ai-ops/platform/backend && go run ./cmd/server     # 终端 B：http://127.0.0.1:8080
cd ai-ops/platform/frontend && npm install && npm run dev   # http://localhost:5173
```

### 12.3 环境变量（后端）

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `TOPO_MODE` | `file` | `file` 直读快照 / `http` 调 topo 服务 |
| `TOPO_DATA_DIR` | 自动定位 `topo/tprel/data` | `file` 模式数据目录 |
| `TOPO_BASE_URL` | `http://127.0.0.1:9000` | `http` 模式基地址 |
| `TOPO_TOKEN` | 空 | topo 鉴权 |
| `STORE_DIR` | `data/platform` | 本地文件存储根目录 |
| `POSTGRES_DSN` | 空 | 非空时开启 PG 备份镜像；不可用不阻塞启动 |
| `MCP_ENABLED` | `true` | 关闭后排障退化为拓扑 + 规则 |
| `PINPOINT_MCP_URL` | `http://127.0.0.1:8888/mcp` | Pinpoint 上游服务 |
| `PROMETHEUS_MCP_URL` | `http://127.0.0.1:8090/mcp` | Prometheus 上游服务 |
| `CMDB_MCP_URL` | `http://127.0.0.1:8061/mcp` | CMDB 上游服务（2026-10-28 接入） |
| `ES_MCP_URL` | 空（关闭） | Elasticsearch 上游服务（2026-11-06 接入，需显式配置） |
| `MCP_TIMEOUT_MS` | `8000` | 单次工具调用超时 |
| `MCP_ALLOW_TOOLS` | 空 | 白名单优先于黑名单；为空表示不限 |
| `MCP_DENY_TOOLS` | 写类工具 | 逗号分隔的黑名单 |
| `EXPLOSION_THRESHOLD` | `10` | 爆炸半径 P 级判定阈值 |
| `DEFAULT_ENV` / `DEFAULT_VIEW_MODE` | `PRO` / `folded` | 默认口径 |
| `ALARM_ENABLED` | `true` | 关闭后不启动告警采集循环 |
| `ALARM_POLL_INTERVAL_SEC` | `30` | 告警采集间隔 |
| `ALARM_COOLDOWN_MIN` | `10` | 同指纹自动排障冷却 |
| `ALARM_AUTO_DIAG` | `true` | 是否自动转排障 |
| `ALARM_MAX_AUTO_DIAG` | `5` | 单轮自动排障上限 |
| `ALARM_MISS_TOLERANCE` | `2` | 连续缺失次数达到后判 resolved |
| `ALERTMANAGER_ENABLED` | `true` | 是否从 Alertmanager 拉取活跃告警 |
| `ALERTMANAGER_URL` | `http://127.0.0.1:9093` | Alertmanager 地址（`/api/v2/alerts`） |
| `ALERTMANAGER_TIMEOUT_SEC` | `3` | 拉取超时 |
| `ALERTMANAGER_GROUP_LABEL` | `group` | 分组标签名 |
| `ALARM_NOTIFY_URL` / `_TIMEOUT_SEC` / `_TOKEN` | 空 / `5` / 空 | 出站通知（Webhook），空则不投递 |
| `INSPECT_MCP_ENABLED` | `true` | 巡检是否调用 MCP 类规则 |
| `INSPECT_MCP_MAX_HOSTS` / `_MAX_APPS` | `20` / `10` | 单轮 MCP 巡检对象上限 |
| `INSPECT_MCP_TIMEOUT_MS` | `5000` | 巡检 MCP 调用超时 |
| `ES_LOG_INDEX` / `_TIME_FIELD` / `_LEVEL_FIELD` / `_HOST_FIELD` | `logs-*` / `@timestamp` / `level` / `host` | 日志类巡检规则字段映射 |
| `INSPECT_ES_LOOKBACK_MIN` / `_ERROR_THRESHOLD` | `15` / `20` | 日志回看窗口与错误数阈值 |
| `IDENTITY_ENABLED` / `_REBUILD_ON_BOOT` | `true` / `true` | 身份映射开关与启动重建 |
| `IDENTITY_CMDB_PAGES` / `_PAGE_SIZE` / `_DNS_RESOLVE` | `5` / `200` / `false` | CMDB 拉取分页与 DNS 反查 |
| `INSPECT_CRON_ENABLED` | `true` | 关闭后不启动巡检 Cron 调度器 |
| `INSPECT_CRON_TICK_SEC` | `20` | 调度器 tick 间隔（秒），建议 ≤ 60 |
| `DEGRADATION_ENABLED` | `true` | 关闭后不启动指标劣化扫描 |
| `DEGRADATION_INTERVAL_SEC` | `300` | 劣化扫描周期 |
| `DEGRADATION_WINDOW_MIN` | `60` | 取序列时间窗（分钟） |
| `DEGRADATION_STEP_SEC` | `300` | `range_query` 采样步长 |
| `DEGRADATION_THRESHOLD_PCT` | `30` | 劣化判定涨幅阈值（%） |
| `DEGRADATION_MAX_TARGETS` | `10` | 单轮最多扫描目标数（按 host 去重后限量） |
| `DEGRADATION_QUERY` | `node_load1{instance=~"{host}.*"}` | 劣化扫描 PromQL 模板，`{host}` 会被替换 |
| `LOG_ANALYZER_ENABLED` | `true` | 关闭后排障跳过日志证据 |
| `LOG_ANALYZER_URL` | `http://127.0.0.1:8000` | 外部日志分析服务地址（**后端排障证据链专用**；compose 内默认 `http://host.docker.internal:8020`，因本机 8000 被 prometheus-mcp-server 占用。前端 `/log-analysis` iframe 默认 `:8000`，可用 `VITE_LOG_ANALYZER_URL` 覆盖） |
| `LOG_PATH_TEMPLATE` | `/tmp/aiops-logs/{host}.log` | 日志路径模板，支持 `{host}` / `{app}`；请求可用 `logPath` 覆盖 |
| `LOG_ANALYZE_TIMEOUT_SEC` | `30` | 日志分析任务最长等待 |
| `LOG_ANALYZE_POLL_MS` | `800` | 日志分析任务轮询间隔 |

### 12.4 存储策略

- **主**：`<STORE_DIR>/<docType>/<docID>.json`，`.tmp` + `rename` 原子写。当前共 22 类 docType：
  - 巡检：`inspection_task` / `inspection_run` / `finding` / `inspection_report`
  - 排障与工单：`diagnosis_session` / `ticket` / `tool_call_log`
  - 告警与劣化：`alarm_event` / `alarm_notification` / `degradation_scan`
  - 割接：`change_plan`
  - 数据治理：`audit_run` / `audit_finding` / `relation_proposal` / `relation_override` / `identity_app` / `identity_host`
  - 质量与业务监控：`quality_report` / `quality_scan` / `biz_rule` / `biz_scan`
  - 知识库：`knowledge_case`
- **备份**：配置 `POSTGRES_DSN` 后，每次写入同时 best-effort 镜像到 `platform_docs(doc_type, doc_id, payload jsonb, updated_at)`；PG 故障只记日志，不影响主链路。
- **回退**：读文件缺失时回退 PG，并回填本地文件；PG 惰性连接，15s 重试间隔。

### 12.5 API 清单（骨架版，前缀 `/api`）

| 组 | 端点 |
| --- | --- |
| 健康 | `GET /health`、`GET /meta` |
| 拓扑 | `GET /topo/{dates,apps,graph,impact,diff,mwtypes,rows}` |
| 运维关系拓扑 | `GET /topo/op/{graph,detail,list,middlewareTypes,mwTargets}`、`POST /topo/op/sync`、`GET /topo/op/sync/{status,logs}` |
| 巡检 | `GET /inspect/rules`（T1–T11 规则元数据）、`GET/POST /inspect/tasks`、`PUT /inspect/tasks/:id`、`DELETE /inspect/tasks/:id`、`POST /inspect/tasks/:id/run`、`POST /inspect/run`、`GET /inspect/runs[/:id]`、`GET /inspect/runs/:id/findings`、`GET /inspect/runs/:id/report` |
| 排障 | `POST/GET /diagnose/sessions`、`GET /diagnose/sessions/:id`、`GET /diagnose/sessions/:id/stream`（SSE）、`POST /diagnose/sessions/:id/feedback`（根因准确率人工回填）；请求支持可选 `logPath` |
| 变更 | `GET /changes/templates`、`POST /changes/generate`、`GET /changes[/:id]` |
| 工单 | `GET /tickets`、`GET /tickets/:id`、`PATCH /tickets/:id/status`、`POST /tickets/:id/resolve` |
| 稽核 | `POST /audit/run`、`GET /audit/runs[/:id]`、`GET /audit/runs/:id/findings` |
| 告警 | `GET /alarms`（state/severity/q/page/pageSize）、`GET /alarms/stats`、`GET /alarms/trend?hours=`、`GET /alarms/degradation`（劣化概况 + 最近扫描）、`GET /alarms/notifications?limit=`（出站投递记录）、`GET /alarms/:fp`、`POST /alarms/poll`、`POST /alarms/webhook`（Alertmanager v4 入站）、`POST /alarms/degradation/scan`、`POST /alarms/notifications/test`、`POST /alarms/:fp/diagnose`、`POST /alarms/:fp/silence`、`POST /alarms/:fp/unsilence` |
| 业务接口监控 | `GET /biz-monitor/{overview,rules}`、`POST /biz-monitor/rules`、`DELETE /biz-monitor/rules/:id`、`POST /biz-monitor/scan` |
| 质差分析 | `GET /quality/overview`、`POST /quality/scan`、`GET /quality/reports[/:id]` |
| 考核指标 | `GET /metrics/kpi?days=7`（时延 P50/P90/达标率、自动化率、根因准确率、每日趋势） |
| 关系修正 | `POST /relations/scan`、`GET /relations`、`GET /relations/{stats,overrides}`、`PATCH /relations/:id`、`POST /relations/:id/apply` |
| 知识库 | `GET /knowledge`、`GET /knowledge/{stats,similar}`、`GET /knowledge/:id`、`POST /knowledge/reindex` |
| 日志服务 | `GET /logs/health`（log_analyzer 探活，供 `/log-analysis` 决定嵌入或降级） |
| 身份映射 | `GET /identity/{stats,apps,hosts,resolve}`、`POST /identity/rebuild` |
| 上游接口 | `GET /mcp/probe`、`GET /mcp/tools`、`POST /mcp/call`、`GET /mcp/logs` |

### 12.6 冒烟结论（本地实测）

- `TOPO_MODE=file` 直读快照：3 个日期（2026-09-29 / 09-30 / 10-08），108 个应用，rocketmq 191 实例 / mysql 62 / redis 40。
- 全量巡检约 3s 完成：`items=12, risk=7, noData=2, notApplicable=1, notTriggered=2`；报告锁定 `sync_date` 可复现。
- 排障（`appId=area`）：拓扑影响面 9 应用；M2–M5 后为 10–11 步证据链、13 个工具调用点 + `sourceHealth` 验真记录，mock 下 `root_caused` / 置信度 0.74 / 问题单 `confirmed`；Prometheus 指向死端口时 `sourceHealth=unavailable`、置信度封顶 0.50、结论 `investigating`；SSE `step/session/done` 正常。
- 前端 `npm run build` 通过，核心页面经 Vite 代理访问后端正常。

### 12.7 与规划的偏差（有意为之）

1. **topo 未内嵌**：topo 无 `go.mod`，改为 `file`/`http` 双 Provider；后续补 `go.mod` 后可替换为进程内调用。
2. **编排层用规则引擎而非 kagent/HolmesGPT**：M2–M5 已交付固定四步流水线（拓扑影响面 → 证据采集 → 规则打分 → 结论/工单），证据链与 SSE 流式已打通；kagent/HolmesGPT 降级为后续可选项，接口契约保持不变。
3. **巡检规则版本**：骨架实现 TOPO_T1–T6，2026-11-05 扩至 TOPO_T1–T11（新增主机存活 / Pinpoint 调用链 / CMDB 一致性 / ES 日志 / 主机指标）；`T4` 因两级图无环恒为 `NOT_APPLICABLE`，`T5` 依赖孤儿数据恒为 `NO_DATA`，均如实出四态结论而非隐藏。

---

## 13. 智能监控排障 × 云网数据治理：两场景一体贯通

> 口径：**只用当前真有、真可用的数据源**（不引入新底座）。
> 本节原为独立文档《排障×数据治理·两场景一体贯通方案》（2026-10-30），2026-11-05 合并进本文，作为总体架构的「动线贯通」章节。

### 13.1 两边各自有什么

#### 13.1.1 数据源总表（当前真实可用）

| 数据源 | 接入方式 | 已在哪个引擎用 |
|---|---|---|
| topo 运维关系快照（file 本地） | Provider（Dates / Apps / Graph / Impact / Diff / Rows / MwTypes / ImpactByApp / ImpactByMiddleware） | 排障（步骤 1）、稽核（C1/C3/C4）、变更方案、tprel 原型 |
| pinpoint-mcp | 统一接入网关 → `list_applications_tool` `get_server_map_tool` `drag_heatmap_tool` `get_transaction_trace_tool` | 排障（第 6–7 步定位、第 9–10 步取证、第 12 步定因） |
| prometheus-mcp | 统一接入网关 → `list_alerts` `query` `healthy` `targets_metadata` `series` `range_query` `host_metrics_realtime` | 排障（第 2–3 步发现、第 4 步验真、第 8 步基线）、巡检、告警、稽核（C4） |
| cmdb-mcp | 统一接入网关 → `cmdb_server_query` | 排障（第 5 步归属 / 问题单）、稽核（C1/C2） |
| elasticsearch-mcp | 统一接入网关 → `get_shards` `list_indices` `search`（可选，`ES_MCP_URL` 为空则不注册；本地演示需显式设为 `http://127.0.0.1:9201/mcp`） | 排障（第 4 步验真、第 11 步日志取证）、巡检 T10 |
| log_analyzer | 本机 FastAPI `:8020`（源码 `~/Documents/trae_projects/log/log_analyzer`；`GET /api/logs/health` 后端探测就绪后 iframe 嵌入，未就绪时页面降级为启动指引） | 独立页 + 排障第 11 步日志证据（ES 不可用时回退） |
| 工单 store | 文件/PG | 排障（root_caused 自动生成）、人工闭环 |
| 稽核 store | 文件/PG | 稽核四态结论 |

#### 13.1.2 后端引擎调用关系（已串的线）

```
diagnose.Engine ── step1 ──▶ topo.Provider (ImpactByApp / ImpactByMiddleware)
       │                                    ▲
       │ step2-11 (pinpoint / prometheus / cmdb / es)
       │                                    │
       │ tickets auto-gen            audit.Engine ── C1/C3 ──▶ topo.Provider
       │                                    │  C4 ───────────────▶ prometheus-mcp
       │                                    C1/C2 ──────────────▶ cmdb-mcp
       ▲
changes.Engine ──── step1 ──▶ topo Impact + Diff + diagnose 历史故障
```

**结论：后端引擎之间其实已经串了三条线（排障 ↔ 拓扑、稽核 ↔ 拓扑+上游服务、变更 ↔ 拓扑+排障），但前端页面之间没有动线。**

#### 13.1.3 跨源数据贯通：让故障信息在数据源之间流起来（2026-11-04 落地）

这里说的「数据连通」不是把几个上游服务的数据字段对齐就结束，而是**以一次故障为线索，把上一跳的产出作为下一跳的输入，一路传导出可执行的运维判断**：

```
Prometheus 告警（instance=10.214.100.2:3306, alertname=MySQLDown）
  → 归一：这台机器 / 这个应用在别的系统里叫什么
  → CMDB：这是谁的资产、在哪个机房、是否还在线、负责人是谁
  → 拓扑：它挂了会波及哪些应用、上下游还有哪些中间件
  → Pinpoint：调用链上哪一跳开始变慢 / 报错
  → 日志：对应时间窗内的错误类型与堆栈
  → 规则排序：把告警、资产、影响面、调用链交叉验证，给出根因候选
  → 工单 / 知识库 / 告警事件：把结论写回去，下一次同类故障可直接命中
```

**传导链落点（当前实现）**

| 跳 | 输入 | 动作 | 输出 | 代码位置 |
|---|---|---|---|---|
| ① 告警归一 | Prometheus `list_alerts` 的 `labels.instance` / `labels.appId` | `identity.ResolveHost` / `ResolveApp` 把 `instance`、Pinpoint 名、Apollo 名收敛为同一主机 / 应用 | 规范主机 Key、Apollo AppID | `alarm/loop.go`、`identity` |
| ② 主机 → 资产 | ① 的主机（IP 优先，域名经身份映射补 IP） | `cmdb_server_query{ip=host}` → 按 `ip/vip/publicIp/hostName` 别名命中资产 | 机房、在线状态、负责人、规格、环境 | `diagnose/engine.go` `collectCMDB` |
| ③ 资产 ↔ 心跳 | ② 的在线状态 + `up{instance=~"..."}` | `up` 按 `instance` 展开成 `host → 0/1`，与 CMDB 状态交叉验证 | 「台账非在线但仍有心跳」= 资产状态过期；「非在线且无心跳」= 实例已下线仍被依赖 | `collectPrometheus` + `rank` |
| ④ 影响面 | 应用 / 中间件 | `topo.ImpactByApp` / `ImpactByMiddleware` | 受影响应用数、上下游中间件实例 | `collectImpact` |
| ⑤ 调用链 | 应用（② 归一后的 Pinpoint 名） | `list_applications_tool` 精确匹配 → `get_server_map_tool`（时间窗与告警对齐） | 上下游节点、中间件依赖 | `collectPinpoint` |
| ⑥ 日志 | 主机 / 应用 | `search`（ES，按主机+级别+时间窗）优先；不可用时回退 `log_analyzer` 按 `LOG_PATH_TEMPLATE` 推导路径 | 错误 TopN、异常模式、关键问题 | `collectLogEvidence` |
| ⑦ 结论回流 | ③④⑤⑥ 全部证据 | 规则排序（拓扑 / 调用链 / 告警 / 资产四类） | 根因候选 + 置信度 → 工单、知识库、告警事件的 `DiagSessionID` | `rank`、`tickets`、`knowledge`、`alarm` |

**已打通 / 仍缺**

- ✅ 排障证据链已含 CMDB 一跳：Prometheus / 拓扑拿到的 host 会反查资产，输出机房、在线状态、负责人，并参与根因排序（此前只有拓扑 / Pinpoint / Prometheus 三类证据）。
- ✅ 告警触发排障时，`labels.appId` 与 `labels.instance` 已经过身份映射归一，不会因为叫法不同而排错对象。
- ⚠️ 域名形态的实例（如 `ehome-pro.mysql.ehomeinner.cn`）若在身份映射里没有 IP 记录，CMDB 这一跳会落到「未纳管」结论，需要补 DNS 或补 CMDB 别名；`IDENTITY_DNS_RESOLVE` 默认关闭。
- ⚠️ 告警事件目前只回写 `DiagSessionID`，尚未把「影响几个应用、资产负责人、机房」富化到告警列表；这部分留到下一阶段。

**这条链的底座（本次一并补齐，不新增表 / 底座）**

| 层 | 位置 | 作用 |
|---|---|---|
| 统一上游返回解析 | `internal/mcp/result.go`、`internal/mcp/tools.go` | 兼容标准 `content[].text` envelope / 直接业务 JSON / 双重编码字符串，并按工具提供类型化视图：`PinpointApplications`、`PinpointServerMapOf`、`PrometheusAlerts`、`PrometheusQuery`、`PrometheusSamples`、`PrometheusMatrix`、`CMDBRecords` |
| 身份映射（跨源归一） | `internal/identity` | 从 topo 快照聚合应用与主机，回填 Pinpoint 应用清单、CMDB 资产、可选 DNS，生成应用 / 主机别名索引；提供 `ResolveApp`、`ResolveHost`、`HostAliases`、`MatchRecord`、`NormalizeHost`、`UpQuery` |

**存储口径**：复用现有 `store.Store`（文件主 + PG 镜像，路径 `<dir>/<docType>/<docID>.json`，原子写），只新增两个 docType，**零 DDL**：

- `identity_app`：docID = 规范应用 Key（Apollo AppID），payload 含 Apollo/Pinpoint/Prom 多套标识与节点、依赖项目、别名
- `identity_host`：docID = 归一主机 Key，payload 含 addr / IP / hostName / CMDB ID / 所属节点 / 中间件类型 / 关联应用

**相关接口**：`GET /api/identity/stats|apps|hosts|resolve`、`POST /api/identity/rebuild`（启动时异步重建，失败不阻断服务）。

**调用方改造**：排障（新增 CMDB 资产证据 + 资产维度排序）、稽核（CMDB 记录按别名对账 + `up` 转义）、告警（`labels.appId` 归一 + 主机反查中间件/应用 + 统一告警解析）、质差 / 劣化 / 业务监控（统一 `query`/`range_query` 解析，`range_query` 参数修正为 `start_time`/`end_time`/`step`）。

### 13.2 断在哪里（前端页面动线缺口）

| 断点 | 现状 | 补了之后 |
|---|---|---|
| ① 稽核发现风险 → 开排障 | ~~Audit 页只展示 C1~C4 结论，风险 host / 孤儿 host 没有"去排障"按钮~~ | **✅ 2026-11-09 已落地**：风险项卡片增加「开排障」，按 `evidence.middlewareType + objectKey` 跳转 `/diagnosis` 并预填目标与症状；稽核 C1/C2/C4 证据已补 `middlewareType`，C3 孤儿因未在拓扑中而按钮禁用并提示手工指定 |
| ② 运维关系拓扑节点 → 开排障 | ~~tprel 原型节点详情卡只展示信息，无联动入口~~ | **✅ 2026-11-09 已落地**：应用 / 中间件实例 / 中间件类型聚合节点的详情卡均加「从这里开排障」，按 `appId` 或 `mwType + addr` 经 `postMessage` 回到 SPA 跳转 `/diagnosis` 并预填目标、环境、症状（聚合节点按实例逐个入口） |
| ③ 运维关系拓扑节点 → 跑稽核 | ~~同上，原型只展示~~ | **✅ 2026-11-09 已落地**：`POST /api/audit/run` 加 `host` 多值白名单参数（可选），命中时 `maxHosts` 强制等于 host 数，跳过 C3 反向孤儿全量抽样（返回 `NOT_APPLICABLE`）；拓扑详情卡在中间件实例、host 节点、聚合节点实例三项均挂「对这个实例跑稽核」按钮，`postMessage` 回 SPA 后 toast 展示风险数 + navigate 到 `/audit` |
| ④ 日志分析 → 并入排障证据链 | ~~iframe 独立页，排障流水线没有日志~~ | **✅ 2026-11-03 已落地，2026-11-06 升级为第 10 步**：优先 ES `search`（按候选主机拼 `query_string` 取计数/样本），ES 不可用时回退 log_analyzer（`POST /api/process-from-path` + 轮询任务），产出错误 TopN / 关键问题 / 异常模式；前端渲染日志证据卡。两者皆不可用或路径未解析时该步 `skipped`，不阻断排障 |
| ⑤ Prometheus 告警 → 自动排障 | prometheus `list_alerts` 已接进排障发现阶段（step2），但没人定时拉 | 定时 30s 拉一次 active alerts → 匹配已在排障或工单中的目标 → 新告警自动创建排障会话并生成工单（**告警中心 MVP**，全基于现有底座）<br>**✅ 2026-11-03 已落地**：`internal/alarm` 采集循环 + 指纹幂等 + 四规则准入 + 自动建单 + 恢复检测，前端 `/alert-center` 已上线并端到端验证 |

### 13.3 串起来的用户动线（一张图）

```
          ┌─────────────────────────────────────────────────────┐
          │           触发（任一入口）                           │
          ├───────────────┬───────────────┬────────────────────┤
          │ A. 运维同学   │ B. 稽核发现    │ C. Prometheus 告警 │
          │   手动发起    │   风险 host     │   自动触发          │
          └───────┬───────┴───────┬───────┴────────┬───────────┘
                  │               │                │
   ┌──────────────▼───────────────▼────────────────▼──────────┐
   │            创建排障会话（diagnose.Start）                  │
   │            Request: { appId | mwType+addr, symptom }     │
   └──────────────────────────┬───────────────────────────────┘
                              │ 异步 10–11 步 SSE
        ┌─────────────────────┼───────────────────────┐
        ▼                     ▼                       ▼
  step1 topo impact   step3~7 验真/CMDB/Pinpoint  step2/8 prometheus
  (爆炸半径 / 影响应用)  (资产·问题单·调用链)          (告警 / up / 基线)
        ▼                     ▼                       ▼
  step9 调用链定因   step10 日志取证(ES→log_analyzer)   step11 证据定因
        │                     │                       │
        └─────────────────────┼───────────────────────┘
                              ▼
            ┌──────────────┼──────────────┐
            ▼              ▼              ▼
   confidence≥0.6   有候选但<0.6     无证据
   root_caused     investigating    no_data（疑似）
            │              │              │
            ▼              ▼              ▼
    自动生成工单    等待人工补证据    工单生成(优先级低)
    (tickets 引擎)  (日志/Pinpoint)   (NO_DATA 口径)
            │
            ▼
    运维处理关闭工单
    → 会话结束（可回放复盘）
```

**同时，运维关系拓扑与资源稽核作为"横向分析面"始终存在**：排障会话里看到某个可疑 host，可以一键跳到稽核页查它是不是孤儿；在拓扑图里点某个节点，可以直接把它变成排障会话起点。两个面不孤立。

### 13.4 每个断点的最小实现（按投入排序，全部不新增底座）

| 断点 | 后端改动 | 前端改动 | 估时 |
|---|---|---|---|
| ① 稽核 → 排障 | 无（`POST /api/diagnose/sessions` 已存在） | ✅ 已落地：Audit 风险卡片「开排障」，按 `mwType + addr` 预填 `/diagnosis`；症状自动带稽核结论 | 已完成 |
| ② 拓扑节点 → 排障 | 无 | ✅ 已落地：详情卡「从这里开排障」，应用传 `appId`、中间件传 `mwType + addr`（聚合节点按实例），`postMessage` 由 `OpTopology` 接管 SPA 内跳转 | 已完成 |
| ③ 拓扑节点 → 稽核 | ✅ 已落地：`Run` 扩展 variadic `hostFilter`，handler 读 `c.QueryArray("host")`，命中时强制 `maxHosts=len(hosts)`、跳过 C3 | ✅ 已落地：详情卡「对这个实例跑稽核」→ fetch `POST /api/audit/run?host=` → `postMessage`（`aiops-audit-run`）→ SPA toast + navigate | 已完成 |
| ④ 日志并入排障 step5 | diagnose.Engine.execute 末尾加一步：对会话 Request 的 host，调 log_analyzer `/api/analyze?host=`（已暴露），结果塞 Step；log_analyzer 挂掉就 skipped 不阻塞 | Diagnosis 页第五个 step panel，内联 iframe 或 iframe 嵌入 Step.Result 里已有的 URL（log_analyzer 有历史报告页） | ✅ 已落地（2026-11-03；2026-11-06 升级为第 10 步，优先 ES `search`、回退 log_analyzer） |
| ⑤ 告警自动排障（MVP） | 新增 `internal/alarm`（`parser.go` 两层解码+指纹、`loop.go` 采集/准入/同步工单）；`mcp.CallSilent` 免审计轮询；6 个 `ALARM_*` 配置；`/api/alarms` 13 个接口 | 新增 `pages/AlarmCenter.tsx`（统计卡 + 列表 + 筛选 + 强制排障 + 详情抽屉），`nav.tsx` 占位换真实页 | ✅ 已落地（2026-11-03，端到端验证） |

**五条断点已全部落地（①–③ 2026-11-09、④ 2026-11-03/11-06、⑤ 2026-11-03），全部基于当前底座，无新依赖。**

### 13.5 要做"整体"就还要补什么

上面 5 个断点补完之后，还有一个重要的"视觉/交互层"没说：**拓扑视图应该能"着色"显示排障会话的影响范围和工单状态**。这不是断点，而是"整体感"的关键——运维一眼能看到"我点进去排障的那个区域，现在工单是什么状态"。

这个也不需要新底座，**✅ 2026-11-10 已落地**：

- 后端：`GET /api/topo/op/graph` 在 `TOPO_MODE=file` 下调用 `enrichOpGraphRefs`，给节点附带 `diagRefs: [{sessionId,status,confidence}]` 与 `ticketRefs: [{id,status}]`——应用按 `appId` 建索引，中间件按 `mwType|host` 归一匹配（地址去端口）；
- 前端：`topology_dagre.html` 的 `nodeOpsLevel` 按 `排障中(红虚) > 工单待处理(橙) > 已定位根因(绿) > 工单已解决(青) > 排障失败(灰虚)` 取最高优先级着色，`toG6Nodes` 与归聚弹窗节点统一应用；统计栏按实际命中数动态渲染图例，详情卡顶部展示状态横幅；
- 兼容性：`convertApiGraph` 在字段白名单中保留 `diagRefs` / `ticketRefs`，否则图例与描边会静默失效。

### 13.6 总结

| 层 | 补之前 | 当前进度 |
|---|---|---|
| **跨源故障传导** | 各引擎各自解上游返回、靠字符串包含做弱匹配；排障证据链缺 CMDB 一跳 | **✅ 2026-11-04 已落地**：统一解析层 `internal/mcp` + 身份映射 `internal/identity`（`identity_app` 108 条 / `identity_host` 330 条，Pinpoint 匹配 108/108）；排障新增 CMDB 资产步骤（机房 / 在线状态 / 负责人），并按「资产状态 × Prometheus 心跳」生成根因候选 |
| **数据源** | 6 路：topo 快照 / pinpoint / prometheus / cmdb / log_analyzer / store | **7 路**：新增 elasticsearch（`ES_MCP_URL` 非空才注册），排障第 4 步验真与第 11 步日志取证、巡检 T10 复用 |
| **后端引擎协作** | 已有 3 条（排障↔拓扑、稽核↔拓扑+上游服务、变更↔拓扑+排障） | 新增第 4 条 **告警↔排障↔工单**（⑤ 已落地）、第 5 条 **排障↔CMDB 资产**（Prometheus 主机 → 资产 → 排序） |
| **前端动线** | 两场景页面孤立，各自为政 | ⑤ 告警中心已打通、④ 日志并入排障、① 稽核 → 排障 **✅ 2026-11-09**、② 拓扑 → 排障 **✅ 2026-11-09**、③ 拓扑 → 稽核 **✅ 2026-11-09**（第一档 3 条动线全部落地） |
| **整体感** | 没有"一张图上看到排障状态" | 拓扑节点告警着色 **✅ 2026-11-03**；**拓扑节点排障 / 工单状态着色 ✅ 2026-11-10 已落地**：`opGraph` 在 file 模式下 enrich 每个节点的 `diagRefs` / `ticketRefs`（应用按 appId、中间件按 mwType+host 归一匹配），前端按 `排障中(红虚) > 工单待处理(橙) > 已定位根因(绿) > 工单已解决(青) > 排障失败(灰虚)` 优先级描边，统计栏附动态图例，详情卡顶部加状态横幅 |

**原估约 16 人日**（5 个断点 + 视觉增强），已分两期推进：

- **第二期（4 + 5）**：⑤ 告警自动排障 **✅ 2026-11-03 已完成**；④ 日志并入排障 **✅ 2026-11-03 已完成**（2026-11-06 起 ES `search` 优先、`log_analyzer` 回退，现为第 10 步；服务不可用/路径缺失时该步 `skipped`，不阻断排障）
- **第一期（1 + 2 + 3 + 视觉增强）**：①稽核→排障 **✅ 2026-11-09**、②拓扑节点→排障 **✅ 2026-11-09**、③拓扑节点→稽核 **✅ 2026-11-09**；拓扑节点排障/工单状态着色 **✅ 2026-11-10**（视觉增强落地，第一档动线 + 整体感闭环）

两个场景真串起来的秘密**不是造新底座，是把已经跑着的引擎和页面用几段 fetch 按钮连起来**。

---

## 14. 平台如何运转：已落地能力与菜单地图

> 第 2–13 章回答「为什么这样设计、怎么一步步建」；本章回答三个使用视角的问题：
> 这台平台**靠什么在转**、**哪些能力已经真的可用**、**菜单怎么组织、怎么用**。
> 两者若有冲突，以本章为当前可用形态的口径（第 4.2 节的页面结构是早期规划稿，实际路由见 14.4）。

### 14.1 运转总览：三条常驻循环 + 一个手动控制台

平台不是"点一下查一下"的查询工具，后端常驻三条自动循环，另外四个场景由人在页面上操作。

| 循环 | 默认周期 | 做什么 | 产出 |
| --- | --- | --- | --- |
| 告警采集 | 30s（`ALARM_POLL_INTERVAL_SEC`） | 拉取 / 接收 Alertmanager v4 与巡检、劣化、质差、业务接口事件，按指纹幂等入库 | 告警事件；命中规则时自动转排障并开工单 |
| 巡检调度 | 20s tick（`INSPECT_CRON_TICK_SEC`） | 扫描巡检任务的 cron，到点执行 TOPO_T1–T11 | 巡检批次 → findings → 巡检报告 |
| 指标劣化 | 300s（`DEGRADATION_INTERVAL_SEC`） | 按 PromQL 模板取主机序列，涨幅超阈值判为劣化 | 劣化告警 + 工单 |

自动闭环有四条，最终都落到同一套存储与工单模型上，所以在不同页面看到的是同一条记录：

- **告警 → 排障 → 工单 → 知识库**（核心动线，见 14.5）
- **巡检 → findings → 工单**
- **指标劣化 → 告警 / 工单 / 排障**
- **资源稽核 → 关系修正建议 → 审批回写**

另一个方向是**收尾闭环**：Prometheus / 指标劣化 / 外部业务接口 / 巡检四类告警恢复时，`tickets.AutoResolve` 把关联工单自动从 open / assigned / in_progress 转到 resolved 并追加恢复说明；closed 仍由人工确认，避免"指标回摆"被当成"已处置"。工单的 `detectedAt / resolvedAt / closedAt` 三个时间点因此可用于计算发现时延与处置时长（MTTR）。

### 14.2 一次请求怎么走

```
浏览器（React 18 + AntD 5 + AntV G6 + TanStack Query）
   │  /api/*
   ▼
Nginx（aiops-frontend，5173 → 容器 80，托管 dist + 反代）
   ▼
Go BFF（aiops-backend :8080；全部路由集中在 internal/api/router.go）
   ├─ alarm     告警采集 / 指纹幂等 / 自动转排障 / 通知
   ├─ inspect   T1–T11 规则引擎 + Cron 调度
   ├─ diagnose  排障会话、证据链、SSE、置信度
   ├─ changes   变更方案与风险评估
   ├─ tickets   工单引擎（状态流转 / 解决）
   ├─ audit     资源稽核（topo ↔ CMDB + 指标佐证）
   ├─ relations 关系修正（扫描 → 建议 → 审批 → 覆盖）
   ├─ knowledge 历史故障归档 / 检索 / 相似案例
   ├─ identity  拓扑 / Pinpoint / Prometheus / CMDB 身份对齐
   ├─ mcp       统一接入网关（探针 / 白黑名单 / 结果归一 / 调用审计）
   └─ topo      file 快照 / http 双 Provider
   ▼
上游与存储
   ├─ topo 快照（file 模式直读；可切 `TOPO_MODE=http` 调 topo 服务）
   ├─ pinpoint-mcp :8888（25 工具）· prometheus-mcp :8090（36 工具）
   ├─ cmdb-mcp :8061（5 工具）· es-mcp :9201（5 工具）——合计 71 个 MCP 工具
   ├─ log_analyzer :8020（外部日志服务，`LOG_ANALYZER_URL`）
   └─ 本地 JSON 文件（主存储）+ PostgreSQL :55432（镜像备份，`platform_docs`）
```

四条运行时约定：

1. **前端不直连上游。** 所有 MCP 调用经 `internal/mcp` 网关：先探针验真，再按 `MCP_DENY_TOOLS` 黑名单拦截写类工具，最后结果归一 + 审计落盘（`tool_call_log`）。
2. **排障是固定流水线，不是自由对话。** 拓扑影响面 → 多路证据（Pinpoint 调用链、Prometheus 指标、CMDB 资产、ES / log_analyzer 日志）→ 规则打分 → 根因候选 → 置信度 → 自动开工单；过程走 SSE 增量推送，会话落盘可回放。
3. **数据源不健康时不伪造结论。** `sourceHealth=unavailable` 时置信度封顶 0.50，结论降级为 `investigating`，对应步骤标记 `skipped` 而不是报错中断。
4. **跨源靠身份映射对齐。** `internal/identity` 把拓扑 `appId`、Pinpoint application、Prometheus 实例、CMDB 资产串成一张表（当前 108 应用 / 330 主机），这是稽核、排障、指标能互相对上的前提。

### 14.3 已落地能力与边界

按业务域汇总"现在真能做什么"，以及容易误解的口径。

| 域 | 已落地（可直接使用） | 边界 / 口径 |
| --- | --- | --- |
| 智能监控排障 | 告警中心：统计 / 趋势 / 筛选 / 详情 / 静默 / 强制排障 / 出站通知记录 / 劣化概况；排障工作台：10–11 步证据链、SSE 过程、影响面、CMDB 资产、日志取证、置信度、根因反馈、会话回放；日志分析：嵌入外部 log_analyzer，健康探测失败自动降级提示；外部业务接口监控：规则 CRUD + 扫描；隐患与工单：自动生成、状态流转、解决归档 | 排障流程为固定流水线（四步骨架），不支持自定义编排；告警采集为单实例轮询，无 HA |
| 智能巡检 | TOPO_T1–T11 规则；巡检任务 CRUD / 启停 / 定时 / 手动触发；四态结论（发现风险 / 未触发 / 数据不足 / 不适用）；批次 findings 明细；巡检报告；巡检方案与脚本 | T4 因两级图无环恒为「不适用」、T5 依赖孤儿数据恒为「数据不足」，如实出四态而非隐藏；判定为规则口径，尚未接模型 |
| 智能割接维护 | 变更方案：按模板 + 拓扑 diff + 历史故障生成；风险评估：影响面、爆炸半径、风险项 | 执行与验证未落地（规划），执行侧无自动化与拨测 |
| 云网数据治理 | 运维关系拓扑：G6 三视图、节点按排障 / 工单状态着色、节点详情可跳排障或稽核；资源稽核：topo ↔ CMDB 对账 + 指标佐证，支持按 host 定向；关系修正：扫描 → 建议 → 审批 → 覆盖回写；资源拓扑与影响面；拓扑数据同步；身份映射底座 | 运维对象只覆盖业务应用与中间件（MySQL/TiDB/Redis 等）；`TOPO_MODE=file` 下同步为只读提示 |
| 网络及服务质量优化 | 质差分析：按 `appId` 扫描并出报告，数据不足时降级为 `degraded`；指标劣化：PromQL 模板 + 定时扫描，可转告警 / 工单 | 依赖 Prometheus 序列质量；阈值为固定配置，非自适应基线 |
| 运营指标 | 考核指标看板：时延 P50 / P90、达标率、自动化执行率、根因准确率、每日趋势（默认 7 天） | 指标来自已产生的会话与工单，无数据时为空而不是补零 |
| 通用能力 | 数据与能力接口：4 个上游的清单 / 文档 / 试跑 / 黑名单拦截 / 调用审计（本地 mock 63 个工具：pinpoint 17 / prometheus 36 / cmdb 5 / es 5；接真实上游后按各自实际工具数动态呈现，代码口径 71）；历史故障库与知识库：归档、关键词检索、相似案例、重建索引；平台服务：REST + SSE 能力面 | 「平台服务」为部分落地：缺鉴权、租户、OpenAPI、调用配额与正式交付物 |
| 模型与算法 | 6 个入口均以占位页明确能力项与依赖 | 全部规划中：样本 / 特征 / 预处理 / 算法 / 训练 / 模型库均未建设 |

### 14.4 菜单地图

菜单共 8 个分组、29 个条目，加「总览」自成一级。状态语义：✅ 已落地、◐ 部分落地、○ 规划中（当前 15 / 6 / 8）。

| 分组 | 菜单项（状态） | 用它做什么 |
| --- | --- | --- |
| 总览 | 总览 ✅ | 一屏汇总告警态势、巡检健康度、最近排障与考核指标，作为当班入口 |
| 智能监控排障 | 告警中心 ✅ · 排障工作台 ✅ · 日志分析 ✅ · 外部业务接口监控 ✅ · 隐患与工单 ✅ · 排障流程管理 ○ | 故障的发现 → 定位 → 取证 → 闭环主链路；排障流程管理沉底为规划项 |
| 智能巡检 | 巡检任务与报告 ◐ · 巡检方案与脚本 ✅ | 主动发现风险；任务侧报告能力部分落地，方案与脚本已可用 |
| 智能割接维护（可整组隐藏） | 变更方案 ✅ · 风险评估 ✅ · 执行与验证 ○ | 变更前评估影响面与风险；执行与验证为规划项 |
| 云网数据治理 | 运维关系拓扑 ✅ · 资源稽核 ✅ · 关系修正 ✅ · 资源拓扑与影响面 ◐ · 拓扑数据同步 ◐ · 运维对象 ◐ | 把拓扑数据变成可信资产：看关系 → 对账 → 修关系 → 同步；也可作为排障横向分析面 |
| 网络及服务质量优化 | 质差分析 ✅ · 指标劣化 ✅ | 从业务质差与主机指标两侧发现劣化 |
| 运营指标 | 考核指标看板 ✅ | 时延、达标率、自动化率、根因准确率的量化呈现 |
| 通用能力 | 数据与能力接口 ◐ · 历史故障库与知识库 ✅ · 平台服务 ◐ | 工具与数据源统一入口、经验沉淀、对外能力开放 |
| 模型与算法（可整组隐藏，默认隐藏） | 样本库管理 ○ · 特征库管理 ○ · 数据预处理与预检验 ○ · 算法管理 ○ · 模型训练 ○ · 模型库管理 ○ | 规划中的模型工程链路，当前仅保留入口与能力项 |

排布原则：场景组内按处置动线排序（发现 → 定位 → 观测 → 闭环），已落地项靠上、部分 / 规划项沉到组尾；通用能力只保留当下可用的接口、知识库与平台服务；模型与算法整组外置，避免干扰日常菜单。

### 14.5 四条使用动线

**① 故障处置（最常用）**

```
告警中心（告警/劣化/质差事件，按指纹去重）
  → 排障工作台（自动或手动发起：拓扑影响面 → 证据链 → 根因候选 → 置信度）
  → 隐患与工单（root_caused 自动建单，人工处理并解决）
  → 历史故障库与知识库（会话归档、相似案例检索）
  → 考核指标看板（时延、自动化率、根因准确率回看）
```

**② 主动发现**

```
巡检任务与报告（定时 T1–T11）或 指标劣化扫描
  → findings / 劣化告警
  → 隐患与工单（或直接转排障）
```

**③ 数据治理**

```
运维关系拓扑（看应用—中间件关系）
  → 资源稽核（与 CMDB 对账、指标佐证）
  → 关系修正（建议 → 审批 → 覆盖回写）
  → 拓扑数据同步 / 资源拓扑与影响面
```

横向联动（已落地）：稽核风险卡「开排障」、拓扑节点「从这里开排障」「对这个实例跑稽核」，跳转时自动预填对象与症状。

**④ 割接变更**

```
变更方案（模板 + 拓扑 diff + 历史故障生成）
  → 风险评估（影响面 + 爆炸半径 + 风险项）
  → 执行与验证（规划中）
```

### 14.6 菜单本身的交互

- **菜单显示**：侧栏底部「菜单显示」面板提供四个开关——规划中功能（默认关）、模型与算法（默认关）、智能割接维护（默认开）、运营指标与评价体系（默认关）。默认隐藏 9 项，可见 20 项；按钮右侧角标显示当前隐藏数量，面板内「全部显示」可一键恢复。偏好写入 `localStorage` 的 `aiops-menu-prefs`，仅对当前浏览器生效；被隐藏的页面仍可通过 URL 直达。
- **一键展开 / 合并**：侧栏底部同一区域提供「展开全部 / 合并全部」切换，作用于当前可见分组；合并后只保留一级分组，选中页仍可凭页头标题识别。
- **状态表达**：已落地项正常显示；部分落地项默认置灰、悬停恢复为正文色；规划项常灰；选中态优先于置灰，保证当前位置有反馈。
- 侧栏采用 `height: 100vh` + 菜单区内部滚动，底部三个动作（展开 / 菜单显示 / 设置）常驻可见。

### 14.7 运行形态与已知边界

- **部署**：`docker compose` 四容器——`mock-mcp`（8888 / 8090 / 8061 / 9201）、`backend`（8080）、`frontend`（5173 → 80）、`postgres`（55432）；接真实上游改 `--profile real-mcp`（与 mock 端口互斥）。也可 `run_demo.sh` 走本地文件模式。
- **存储**：JSON 文件为主（`<STORE_DIR>/<docType>/<docID>.json`，tmp + rename 原子写），PG 镜像备份并在文件缺失时回填。文档类型覆盖 `alarm_event` / `ticket` / `diagnosis_session` / `inspection_task|run|report` / `finding` / `tool_call_log` / `change_plan` / `audit_run|finding` / `relation_proposal|override` / `knowledge_case` / `quality_report|scan` / `biz_rule|scan` / `degradation_scan` 等。
- **已知边界**：无鉴权与租户模型、无 OpenAPI 与调用配额；后端单实例无 HA；CMDB / ES 未配置时不注册对应工具；拓扑 `file` 模式只读；模型与算法整链未建设。这些对应各菜单项的「部分 / 规划」状态，不在页面上伪装成已完成。

