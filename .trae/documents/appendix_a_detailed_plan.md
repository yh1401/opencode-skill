# 附录 A 实现蓝图 · 补充细化方案

## Repository Research

### 当前状态

- 附录 A 已有 15 项骨架级蓝图：后端接口表（路径 + 说明）、前端步骤（功能点枚举）、Go 骨架（关键算法）、预估代码量
- 实际落地 4 页：`/op-topology`（✅ done）、`/diagnosis`（✅ done）、`/topology`（partial）、`/topo-sync`（partial）、`/inspection`（partial）、`/mcp`（partial）
- 20 个菜单仍是 Placeholder，nav.tsx 全部指向同一个 Placeholder 组件
- 后端 7 个已注册路由组：`/health` `/meta` `/topo` `/topo/op` `/inspect` `/diagnose` `/mcp`，总路由数约 30 个
- 前端已定义 TypeScript 类型约 50 个（types.ts）、API 函数约 25 个（api.ts）
- 统一存储抽象 `store.Store`（文件为主 + PG 备份，当前降级为文件）
- 统一 MCP 调用入口 `mcp.Gateway.Call(server, tool, args)`，前端也有 `/api/mcp/call` 代理
- 统一拓扑 Provider 抽象 `topo.Provider`（file 本地计算 / http 透传 topo 服务）
- 统一 SSE 协议 `event: step | session | done | timeout | error`，前端 `streamDiagnosis` 已实现

### 当前蓝图的粒度缺口

| 维度 | 当前 | 目标 |
|---|---|---|
| 接口契约 | 路径 + 说明 | 完整 OpenAPI 风格（参数 schema + 返回 JSON 示例 + 错误码） |
| 前后端映射 | 前端步骤枚举 | 每一步 UI ↔ 精确接口调用（含 query/body 参数） |
| 文件级改动 | "internal/alertcenter/handler.go"（示意） | router.go 加哪行、types.ts 加哪个 interface、api.ts 加哪个函数、nav.tsx 加什么 import |
| 数据模型 | JSON Schema（2-3 个） | 每个功能点的域模型 Go struct + TypeScript interface + 存储 key 规划 |
| 依赖顺序 | 无 | DAG 依赖排序（上游先实现 → 下游可搭） |
| 验收标准 | 无 | 每个 P0/P1 功能点的冒烟测试命令 |

## Files and Modules（本文档将要修改的）

- `ai-ops/doc/需求功能点与菜单底座对照表.md`
  - 在附录 A 每项末尾追加：**完整接口契约表**（请求 + 返回 + 错误码）、**前端步骤 × 接口映射矩阵**、**后端文件级改动清单**、**域模型定义**、**冒烟测试**
  - 在附录 A 末尾追加：**实现顺序依赖图** + **验收检查点**

## Implementation Steps

### Step 1：给每个 P0 功能点补完整接口契约（请求参数 + 返回 JSON + 错误码）

范围：A.7 变更方案 + A.8 风险评估 + A.4 隐患与工单 + A.6 巡检规则配置化

每个接口补充：
```
| 方法 | 路径 | Query/Body | 返回 JSON 示例 | 错误码 |
```

对齐现有模式：
- HTTP 统一 200，业务错误看 body（当前项目 body 直接返回对象，无 `code/msg` 双信封）
- 列表分页统一 `pageNum/pageSize/pageTotal/list`
- SSE 统一 `event: xxx` 帧格式
- 存储 key 命名：`{module}_{entity}` 如 `change_template` `ticket`

### Step 2：给每个功能点补前端步骤 × 接口映射矩阵

示例格式：
```
| 前端步骤 | UI 组件 | 接口调用 | 参数来源 | 状态处理 |
| 1. 列表页加载 | AntD Table | GET /api/tickets | 无 | loading → 渲染 → 空态 |
| 2. 详情 Modal | Descriptions | GET /api/tickets/:id | row.id | 点击行触发 |
| 3. 状态变更 | Select + Modal | PATCH /api/tickets/:id/status | body={status} | 乐观锁，失败回滚 |
```

### Step 3：给每个功能点补后端文件级改动清单

示例格式：
```
新增：
- internal/changes/handler.go      // ValidateRisk + 方案生成
- internal/changes/templates.go     // 5 个内置模板定义
- internal/domain/changes.go       // ChangeTemplate / ChangePlan / RiskScore struct

修改：
- internal/api/router.go           // 加 api.Group("/changes") 路由
- internal/api/router.go           // Server struct 加 changes 字段
- cmd/server/main.go               // New 时初始化 changes 模块
- internal/domain/models.go        // 复用现有 InspectionTask Scope
```

### Step 4：补域模型定义（Go struct + TypeScript interface）

每个功能点至少给出 2-3 个核心域模型的完整字段定义：
- Go struct（含 `json` tag）
- TypeScript interface（含可选字段 `?`）
- 存储 key 规划（store.Put 的主键）

### Step 5：补实现顺序依赖图

用 DAG 把 15 项功能点串起来：
```
P0 独立项（无上游）：A.6 / A.7+A.8 / A.4
P1 依赖 prometheus-mcp：A.1 → A.2 → A.15
P1 依赖排障引擎：A.1 → A.4 → A.15
P2 依赖 topo sync diff：A.11 / A.10
...
```

### Step 6：补验收检查点（冒烟测试命令）

每个功能点给出 1-2 条 curl 冒烟命令 + 预期返回片段。

### Step 7：执行编辑

按 Step 1-6 的顺序，把每个功能点的补充内容追加到文档。

## Dependencies and Considerations

### 现有代码模式复用约束
- **存储统一用 `store.Store`**：文件为主 + PG 备份，key 命名 `{module}_{entity}_{id}`（如 `change_template_tpl-001`）
- **Server struct 注入**：`api.Server` 持有 `topo/mcp/inspect/diag/store/cfg` 六个字段，新模块也按这个模式加字段，在 `cmd/server/main.go` 初始化
- **路由注册**：在 `router.go` 的 `Router()` 里加 `api.Group("/xxx")` 子组，每个 handler 是 `func (s *Server) xxxHandler(c *gin.Context)` 方法
- **错误返回**：统一用 `fail(c, code, err)`，HTTP 状态码 + body `{"error": "..."}`
- **前端 API**：`api.ts` 的 `api.xxx(params)` 函数 + `types.ts` 的 interface，对齐后端 struct JSON tag
- **前端路由**：nav.tsx 的 `NAV` 数组加 `items`，每个 item 是 `{key, label, status, element: <Component />}`

### 与现有底座的复用点（关键发现，决定实现路径）
- **排障引擎 `diagnose.Engine`**：`Start(req) / List(limit) / Get(id)` 三个方法，被 A.1/A.2/A.4/A.12/A.15 五处复用
- **拓扑 Provider `topo.Provider`**：`ImpactByApp / ImpactByMiddleware / Diff / Apps / Rows`，被 A.2/A.7/A.10/A.11 四处复用
- **MCP Gateway `mcp.Gateway`**：`Call(server, tool, args)` 代理，被 A.1/A.2/A.3/A.9/A.13 五处复用
- **巡检引擎 `inspect.Engine`**：`Start(task, trigger)` 硬编码 T1-T6，A.6 需要把它配置化
- **Store `store.Store`**：`Put/Get/Delete/ListInto` 通用 CRUD，新模块全部复用

### 存储降级行为
当前 Docker Desktop 不可用 → PG 未启动 → Store 降级为文件存储。所有蓝图设计统一按文件存储设计，PG 是可选备份层，不影响功能正确性。

## Validation

### 文档层面
- 每个补充项前后交叉引用一致（接口路径 ↔ 路由注册 ↔ 前端 API 函数 ↔ TypeScript 类型 ↔ Go struct json tag）
- 实现顺序图的依赖关系正确（无环）
- 冒烟测试命令可直接在 localhost:8080 执行

### 代码层面（后续真正实现时）
- `go build ./...` 无编译错误
- `npm run build` 无 TS 错误
- 所有 curl 冒烟测试返回预期 JSON 结构

## Risks

- **范围膨胀风险**：15 项全补会让文档增加 ~1500 行。处理：P0 四项先补完整（Step 1-6 全覆盖），P1/P2/P3 补接口契约 + 文件清单即可，实现细节放在真正开发时再写。
- **用户其实只想要某几项的详细实现**：如果用户只想先做 P0 的 A.7+A.8 变更方案，那全补文档是过度。处理：方案里先标注"先补 P0 四项完整，其余按需"，用户批准后再按优先级推进。
- **Go struct 定义需要与现有代码一致**：已读 router.go 确认了 Server struct 和 handler 签名模式，对齐无误。
