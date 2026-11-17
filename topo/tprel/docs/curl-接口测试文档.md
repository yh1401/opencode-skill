# 运维关系拓扑图 · 接口测试文档

> 环境：本地独立测试服务 `http://127.0.0.1:8098`（`go run ./api/tprel/test/server`）
> 基础路径：`/api/external/operationTopology`
> 统一响应：`{"code":0,"data":...,"msg":"操作成功"}`（`code!=0` 为失败）
>
> ⚠️ **HTTP 状态码恒为 200**（含鉴权失败、业务失败），成败一律判断 body 的 `code`；`code:401` 表示未登录/非法访问（见 §7）。
>
> ⚠️ 本文所有 curl 基于**本地测试服务**（无鉴权）。在**服务器（生产/联调）**上执行请见文末 **[§8 服务器环境测试](#8-服务器环境测试生产--联调)**——地址不同且**全部接口需带 JWT**。

---

## 1. 健康检查

```bash
curl -s http://127.0.0.1:8098/healthz
```

**返回：** `{"code":0,"msg":"ok"}`

---

## 2. 拓扑图（GET /graph）

### 2.1 请求参数

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `env` | string | 否 | 环境（PRO/SIT），不传默认最新 |
| `node` | string | 否 | 节点（机房），如 `GuiZhou3-Product` |
| `middlewareType` | string | 否 | 中间件类型过滤 |
| `syncDate` | string | 否 | 快照日期（YYYY-MM-DD），默认最新 |
| `expandDepNode` | bool | 否 | 部署点展开开关：**不传即展开**（默认，应用节点粒度 = 部署点，id 形如 `app:{appId}\|{部署项目}\|{节点}`）；显式 `0`/`false` 折叠为 appId 粒度（id 形如 `app:{appId}`） |
| `source` | string | 否 | 数据源覆盖 `db\|file`（默认 `db` 优先，不可用回退 `file`） |

> **口径提醒**：本文 §2.3 的示例为 **`expandDepNode=0` 折叠口径**；**默认展开**时 `env=PRO` 实测 581 节点 / 2943 边（2026-09-20 快照）。两种口径完整对照见《[运维拓扑.md](运维拓扑.md)》§4.5。
>
> **量级说明**：`nodes` / `edges` / `pageTotal` 随每日快照变化，本文数字仅为**某次快照的参考值**，实际以接口返回的 `counts` / `pageTotal` 为准。
>
> **⚠️ `node` 过滤注意**：应用节点的 `node` 属性由查询期两跳关联（`app_dp_relation` → `deploy_pinpoint_relation`）回填，**当前快照回填率极低**（快照部署项目与 DPR 的 108 个部署项目几乎无交集），故 `node=xxx` 过滤通常返回空（`counts` 五项全 0）。按机房过滤前请先确认 `/graph` 返回的节点上 `node` 字段确有值（当前仅 `app:ehome-account-new-service|ehome-account-new-service|ct-yk` 等极少数有值）。

### 2.2 curl 命令

```bash
# ① 全量拓扑（默认最新快照）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO"

# ② 按机房过滤
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO&node=GuiZhou3-Product"

# ③ 按中间件类型过滤
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO&middlewareType=mysql"

# ④ 指定历史快照（日期须为库中已有快照，否则返回 msg「暂无数据」）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO&syncDate=2026-09-20"

# ⑤ 组合过滤（机房 + 类型）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO&node=GuiZhou3-Product&middlewareType=mysql"

# ⑥ 格式化查看完整返回
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO" | python3 -m json.tool

# ⑦ 折叠口径（一个 appId 一个节点，属性多值聚合为列表；nodes/edges/counts 随之变小）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO&expandDepNode=0"
```

### 2.3 返回结构

```json
{
  "code": 0,
  "data": {
    "nodes": [
      {
        "id": "app:apollo-ehome-pro",
        "category": "application",
        "name": "ehome-pro-server",
        "label": "ehome-pro-server",
        "node": "GuiZhou3-Product",
        "engProject": "ehome-pro",
        "ppApp": "GZ3-ehome-pro",
        "apolloAppId": "apollo-ehome-pro",
        "appIds": null,
        "mwType": "",
        "addr": "",
        "mwProject": "",
        "depCount": 0
      },
      {
        "id": "mw:mysql:ehome-pro.mysql.ehomeinner.cn:13302",
        "category": "middleware",
        "name": "mysql:ehome-pro.mysql.ehomeinner.cn:13302",
        "label": "ehome-pro.mysql.ehomeinner.cn:13302",
        "node": "",
        "engProject": "",
        "ppApp": "",
        "apolloAppId": "",
        "appIds": ["app:apollo-ehome-pro", "..."],
        "mwType": "mysql",
        "addr": "ehome-pro.mysql.ehomeinner.cn:13302",
        "mwProject": "mysql",
        "depCount": 1
      }
    ],
    "edges": [
      {"source": "app:apollo-ehome-pro", "target": "mw:mysql:ehome-pro.mysql.ehomeinner.cn:13302", "kind": "depend"}
    ],
    "syncDate": "2026-09-20",
    "env": "PRO",
    "partial": false,
    "counts": { "apolloHosts": 5, "application": 108, "middleware": 341, "edges": 756, "nodes": 449 }
  },
  "msg": "操作成功"
}
```

> 上例为 `env=PRO` 全量**折叠口径（`expandDepNode=0`）**的**节选**（`nodes` 实际 449 个），`counts` 始终是**全量**口径，与筛选条件一致；**默认展开**时同条件实测 `nodes` 581 / `edges` 2943（应用节点 id 形如 `app:{appId}|{部署项目}|{节点}`）。两种口径对照见《[运维拓扑.md](运维拓扑.md)》§4.5。
>
> 示例中的字段**值**（`node` / `engProject` / `ppApp` / `syncDate` 等）取自某次快照，会随快照与两跳回填结果变化（`node` / `engProject` 当前多为空串），**字段集合与类型固定**。

**字段说明：**
- `id` 规范：应用 **默认展开** `app:{apollo_app_id}|{部署项目}|{节点}`、**折叠**（`expandDepNode=0`）`app:{apollo_app_id}`（缺失时回退 `dep_project`）；中间件 `mw:{type}:{addr}`
- 中间件节点 `appIds` = 依赖它的应用 id 清单，`depCount` = 依赖数（前端角标）
- `partial=true` 表示同步预算超时部分完成（如从未同步则 nodes 为空，msg 提示"暂无数据"）
- `counts`：与**当前过滤条件一致**的列表规模汇总——`apolloHosts`=返回 `apolloHosts` 数组长度；`application`/`middleware`=返回 `nodes` 中对应 `category` 的节点数量；`edges`=返回 `edges` 条数；`nodes`=节点总数（=application+middleware）。空图（无数据）时五项全为 0。**仅 `graph` 返回该字段**，`list`/`detail`/`middlewareTypes`/`sync`/`sync/status`/`sync/logs` 均无。

### 2.4 实测结果

| 用例 | 口径 | nodes | edges |
|---|---|---|---|
| graph 全量 PRO | 展开（默认，不传） | 581 | 2943 |
| graph 全量 PRO | 折叠（`expandDepNode=0`） | 449 | 756 |
| graph + middlewareType=mysql | 展开（默认，不传） | 234 | 302 |
| graph + middlewareType=mysql | 折叠（`expandDepNode=0`） | 147 | 142 |

> 上表为 **2026-09-20 快照**实测值，两列为**同一份快照、不同 `expandDepNode` 口径**；默认不传即展开，故「默认口径」看展开行。量级随每日快照变化，以接口返回的 `counts` 为准。
> `node=xxx` 机房过滤未列入上表：当前快照下应用节点 `node` 属性回填率极低，过滤结果恒为 0（见 §2.1 的 ⚠️ 说明）。

---

## 3. 关系列表（GET /list）

### 3.1 请求参数

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `page` | int | 否 | 页码（默认 1） |
| `pageSize` | int | 否 | 每页条数（默认 20） |
| `node` | string | 否 | 节点（机房） |
| `env` | string | 否 | 环境 |
| `middlewareType` | string | 否 | 中间件类型 |
| `depProject` | string | 否 | 部署项目名 |
| `apolloAppId` | string | 否 | Apollo appid |
| `connectAddr` | string | 否 | 连接地址 |
| `syncDate` | string | 否 | 快照日期（默认最新） |
| `expandDepNode` | bool | 否 | 部署点展开开关：**不传即展开**（默认，行按部署点扩行）；显式 `0`/`false` 折叠为 appId 粒度（属性补充不扩行）。**影响 `pageTotal`（展开 5266 / 折叠 1418，2026-09-20 快照）** |
| `source` | string | 否 | 数据源覆盖 `db\|file`（默认 `db` 优先、不可用回退 `file`） |

### 3.2 curl 命令

```bash
# ① 分页查询
curl -s "http://127.0.0.1:8098/api/external/operationTopology/list?page=1&pageSize=5&env=PRO"

# ② 按部署项目过滤
curl -s "http://127.0.0.1:8098/api/external/operationTopology/list?page=1&pageSize=10&depProject=area-prod-224326-dep"

# ③ 按中间件类型过滤
curl -s "http://127.0.0.1:8098/api/external/operationTopology/list?page=1&pageSize=10&middlewareType=mysql"

# ④ 按连接地址过滤
curl -s "http://127.0.0.1:8098/api/external/operationTopology/list?page=1&pageSize=10&connectAddr=areacode.mysql.vcpinner.cn:13302"

# ⑤ 组合过滤
curl -s "http://127.0.0.1:8098/api/external/operationTopology/list?page=1&pageSize=10&env=PRO&middlewareType=redis"
```

### 3.3 返回结构

```json
{
  "code": 0,
  "pageTotal": 1418,
  "list": [
    {
      "id": 2837,
      "apolloHostId": 14,
      "apolloHostName": "天翼云眼广州4多AZ生产Apollo",
      "apolloHostAddr": "https://tyyy-guangzhou4d-apollo-prod.ctseelink.cn:8158",
      "secondProductId": "",
      "appCount": 0,
      "apolloAppId": "area",
      "apolloAppName": "area",
      "appDept": "部门2",
      "appOwner": "gaoshj",
      "node": "",
      "engProject": "",
      "depProject": "area-prod-224326-dep",
      "ppApplication": "area",
      "depProjects": ["area-prod-224326-dep"],
      "namespace": "application",
      "configKey": "spring.datasource.url",
      "middlewareType": "mysql",
      "connectAddr": "areacode.mysql.vcpinner.cn:13302",
      "middlewareDb": "",
      "middlewareProject": "mysql",
      "env": "PRO",
      "syncDate": "2026-09-20",
      "source": "apollo",
      "remark": "",
      "createTime": "2026-09-20T07:57:14+08:00",
      "updateTime": "2026-09-20T07:57:14+08:00"
    }
  ]
}
```

> 上例为 `GET /list?env=PRO&expandDepNode=0&apolloAppId=area&page=1&pageSize=1` 的**真实返回**（2026-09-20 快照，`pageTotal` 为全量口径故此处示意为 1418）。`pageTotal` 是**候选关系行数**（一行 = 一条「应用 → 中间件」关系），折叠口径下 = 快照行数（OT 1418 行）；**默认展开时 `pageTotal` = 5266**（按部署点扩行后的行数）；
> `depProject`/`engProject`/`node`/`ppApplication` 四列由查询期两跳关联作为 appId 属性补充（单值取属性列表首值，多值走 `depProjects`/`engProjects`/`nodes`/`ppApplications` 列表），**不扩行**；**多值列表字段为空时整体不输出**（`omitempty`），故上例只有单值列。
> 上例 `id`（2837）/`createTime`/`updateTime` 来自 DB；**file 模式下 `id` 恒为 `0`、时间列为零值**，此时「倒序/升序」只体现为行序，不可用 `id` 判读（见《[运维关系拓扑图方案.md](运维关系拓扑图方案.md)》§3.3）。
> 示例字段**值**随快照与两跳回填变化，**字段集合与类型固定**。

---

## 4. 手动触发同步（POST /sync，异步）＋ 状态轮询（GET /sync/status）＋ 日志读取（GET /sync/logs）

> 全量同步是**分钟级长任务**（服务器真实环境几百应用 × 多 namespace，耗时几十秒~数分钟），走 HTTP 网关/负载均衡（LVS/Tengine 默认几十秒超时）会 504。
> 因此 `POST /sync` 改为**异步提交**：提交后立即返回（不阻塞、不超时），由调用方**轮询** `GET /sync/status` 获取进度与最终结果。

### 4.1 提交任务（立即返回）

```bash
curl -s -X POST "http://127.0.0.1:8098/api/external/operationTopology/sync"
```

**返回（提交成功）：**

```json
{
  "code": 0,
  "data": {
    "started": true,
    "message": "同步任务已提交，请轮询 /operationTopology/sync/status 获取结果",
    "state": {
      "running": true,
      "triggerBy": "手动同步(页面/sync接口)",
      "startTime": "2026-09-20 16:00:00",
      "stageText": "任务已提交，等待执行",
      "percent": 0,
      "costMs": 5
    }
  },
  "msg": "同步任务已提交"
}
```

- `started=true`：任务已提交后台执行（与每天 01:30 定时任务互斥排队，不会双跑）。
- 已有任务在执行/排队时再提交返回 `started=false`、`state.running=true`，直接轮询即可，无需重复提交。

### 4.2 轮询同步状态

```bash
curl -s "http://127.0.0.1:8098/api/external/operationTopology/sync/status"
```

**返回（执行中）：**

```json
{
  "code": 0,
  "data": {
    "running": true,
    "triggerBy": "手动同步(页面/sync接口)",
    "startTime": "2026-09-20 16:00:00",
    "endTime": "",
    "costMs": 214000,
    "error": "",
    "stageText": "正在扫描配置 456/702",
    "percent": 42,
    "stats": { "hosts": 5, "apps": 234, "middleware": 341, "relations": 1418 },
    "result": null
  },
  "msg": "操作成功"
}
```

**返回（完成后）：** `running=false`、`stageText`/`percent` 清空，`stats` 保留最终规模统计，`result` 为本次完整 `SyncResult`（结构见 4.3）。

- `running=true`：仍在执行，继续轮询（建议间隔 3~5 秒；原型实现按 1 秒轮询以获得平滑进度条）。
- **进度字段**（供前端做进度条提示）：
  - `stageText`：当前阶段文案（如 "正在扫描配置 456/1077"），随执行阶段推进自动变化；
  - `percent`：当前阶段完成度 **0~100**；`-1` 表示该阶段暂无定量进度（如探活/拉应用列表），前端此时展示"不确定"滑动动画即可，阶段文案见 `stageText`。
- **规模统计 `stats`**（运行中实时更新、完成后即最终值，供弹窗内展示总体数据）：
  - `hosts`：扫描的 Apollo 套总数；`apps`：应用总数（任务组装后即确定）；
  - `middleware`：已识别中间件节点数（按 类型+地址 去重，随扫描实时增长）；
  - `relations`：已解析连接关系条数（随扫描实时增长）。
- `running=false` 且 `error=""`：**成功**，`result` 为本次完整同步结果（`SyncResult`），`stats` 为最终统计（与 result 的 hostCount/appCount/insertCount 等一致）。
- `error` 非空：**失败**，`error` 为失败原因，`result` 为空。

> 阶段推进顺序：`任务已提交，等待执行` → `正在探测 Apollo 套可用性`(percent=-1) → `正在拉取应用列表`(-1) → `扫描任务就绪，共 N 项` → `正在扫描配置 d/N`(0~100，且 stats.middleware/relations 实时增长) → `正在对比并生成快照`(100)。

### 4.3 result（SyncResult）关键字段

| 字段 | 含义 |
|---|---|
| `hostCount` / `availableHosts` | 扫描套数 / 探活可用套数 |
| `appCount` / `namespaceCount` / `requestCount` | 应用总数 / 请求数 / namespace 扫描数 |
| `insertCount` | 本次落库关系数 |
| `partial` / `unfinishedApps` | 是否预算超时部分完成 / 未完成应用列表 |
| `newRelations` / `lostRelations` / `prevDate` | 与上次快照（`prevDate` 标注日期）相比的新增/消失关系 |
| `costMs` / `triggerBy` | 总耗时 / 触发方式 |
| `nodeStats` / `typeStats` | 各中间件类型节点数（**按“类型+地址”去重**，与图节点口径一致） / 关系数（**含实例重复**），两者并存 |
| `noMiddlewareCount` / `noMiddlewareApps` | 无中间件应用数量 / 明细 `[{appId, appName}]`（按 appId 升序），位于 result JSON 末尾 |

> **无中间件口径（重要）**：`noMiddlewareCount`/`noMiddlewareApps` 统计本次**已实际执行过 namespace 拉取**（任意 ns 有返回即算，404 也算）但**未解析出任何中间件关系行**的应用；**排除**「未完成应用」（拉取失败）与「未执行应用」（预算截断未轮到，无法判定）——**没扫到的应用不算“无中间件”**。日志体现：同步结束数量 >0 时打印整块「本次同步 · 无中间件应用清单（已扫描未发现中间件关系）共 N 个（不含未完成/未执行应用）」，“结果总览”块另加一行汇总（最多示例 5 个）。

> 重复触发不会写坏数据：服务端互斥锁与定时任务排队；同日重复同步为幂等整桶覆盖（先删当日快照再全量插入）。
> **同步期间查询数据稳定**：采集/同步过程中查询始终读**旧版**数据（内存快照加载后不可变，只在结束时刻一次落盘切换），整批成功才**原子替换**本地快照；`partial=true`（部分完成）且本地已有可用旧快照 → **不落盘、保留旧快照**（提示"已保留现有快照 {date} N 条，本次未覆盖"），仅从未有过可用快照时才先落部分数据。

### 4.4 同步日志读取 sync/logs（GET）

> 读取同步日志（由 `tplog` 按天落盘为 `YYYY-MM-DD.log`，默认目录 `api/tprel/logs`，可用环境变量 `TPREL_LOG_DIR` 覆盖；一天内多次同步累积在同一文件）。排障看同步过程日志不必上服务器翻盘。

**请求参数：**

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `date` | string | 否 | 日志日期 YYYY-MM-DD；缺省自动选最近一天有日志的文件（已做严格正则 + 目录穿越双重防护） |
| `download` | string | 否 | `"1"`/`"true"` 时以 `text/plain` **附件下载**该日志完整内容，不返回 JSON |

**curl 命令：**

```bash
# ① 默认：最近一天有日志的同步日志（展示模式只取最近 256KB，超长 truncated=true）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/sync/logs"

# ② 指定日期（日期须为 `files` 清单中存在的一天，否则 `code=7`「该日期无同步日志」）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/sync/logs?date=2026-09-20"

# ③ 下载完整日志（text/plain 附件，不受 256KB 限制；curl -OJ 自动落盘为 operation-topology_{日期}.log）
curl -OJ "http://127.0.0.1:8098/api/external/operationTopology/sync/logs?download=1"
```

**返回（展示模式，节选）：**

```json
{
  "code": 0,
  "data": {
    "date": "2026-09-20",
    "file": "2026-09-20.log",
    "sizeBytes": 10621225,
    "content": "...（日志文本）...",
    "truncated": true,
    "files": [
      { "date": "2026-09-20", "file": "2026-09-20.log", "sizeBytes": 10621225 },
      { "date": "2026-09-19", "file": "2026-09-19.log", "sizeBytes": 1330729 },
      { "date": "2026-09-18", "file": "2026-09-18.log", "sizeBytes": 6650566 },
      { "date": "2026-09-17", "file": "2026-09-17.log", "sizeBytes": 1990023 }
    ]
  },
  "msg": "操作成功"
}
```

- `data.date` 命中日期；`file` 文件名；`sizeBytes` 文件字节数；`content` 日志内容（**展示模式只取最近 256KB**，超长时 `truncated=true`）；`files` 可用日志文件清单 `[{date,file,sizeBytes}]`（日期倒序，最多 30 条）。
- 完整内容请用 `download=1`（附件下载，不返回 JSON）；日志目录为空（从未同步）时默认请求返回 `code=7`，msg `暂无同步日志（日志目录为空）`。

**错误用例（均 `code=7`）：**

```bash
# ④ date 格式非法 → msg=date 参数格式非法（应为 YYYY-MM-DD）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/sync/logs?date=2026-9-9"

# ⑤ 该日期无同步日志 → msg=该日期无同步日志: 2026-09-01
curl -s "http://127.0.0.1:8098/api/external/operationTopology/sync/logs?date=2026-09-01"
```

---

## 5. 中间件类型枚举（GET /middlewareTypes）

```bash
curl -s "http://127.0.0.1:8098/api/external/operationTopology/middlewareTypes"
```

**返回：**

```json
{
  "code": 0,
  "data": ["elasticsearch", "kafka", "mysql", "nacos", "redis", "rocketmq", "zookeeper"],
  "msg": "操作成功"
}
```

---

## 6. 一键测试脚本

复制以下内容保存为 `test_api.sh` 后执行：

```bash
#!/bin/bash
# 运维关系拓扑图 接口一键测试
BASE="http://127.0.0.1:8098/api/external/operationTopology"
echo "== ① 健康检查 =="
curl -s http://127.0.0.1:8098/healthz; echo
echo "== ② graph 全量 =="
curl -s "$BASE/graph?env=PRO" | python3 -c "import sys,json;g=json.load(sys.stdin)['data'];print('nodes',len(g['nodes']),'edges',len(g['edges']),'sync',g['syncDate'])"
echo "== ③ graph 机房过滤 =="
curl -s "$BASE/graph?env=PRO&node=GuiZhou3-Product" | python3 -c "import sys,json;g=json.load(sys.stdin)['data'];print('nodes',len(g['nodes']),'edges',len(g['edges']))"
echo "== ④ graph 类型过滤 =="
curl -s "$BASE/graph?env=PRO&middlewareType=mysql" | python3 -c "import sys,json;g=json.load(sys.stdin)['data'];print('nodes',len(g['nodes']),'edges',len(g['edges']))"
echo "== ⑤ list 分页 =="
curl -s "$BASE/list?page=1&pageSize=5" | python3 -c "import sys,json;d=json.load(sys.stdin);print('total',d['pageTotal'])"
echo "== ⑥ middlewareTypes =="
curl -s "$BASE/middlewareTypes"; echo
echo "== ⑦ sync（异步提交，轮询至完成，展示进度） =="
curl -s -X POST "$BASE/sync" >/dev/null
for i in $(seq 1 120); do
  curl -s "$BASE/sync/status" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];r=d['result'] or {};print('running',d['running'],'|',d.get('stageText'),'| pct',d.get('percent'),'| costMs',d['costMs'],'| insert',r.get('insertCount'),'| err',(d['error'] or '')[:60])"
  curl -s "$BASE/sync/status" | grep -q '"running": false' && break
  sleep 3
done
echo "== ⑧ sync/logs 同步日志读取（展示模式，验证 date/content/files/truncated 字段存在） =="
curl -s "$BASE/sync/logs" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];print('date',d.get('date'),'| file',d.get('file'),'| sizeBytes',d.get('sizeBytes'),'| truncated',d.get('truncated'),'| content_len',len(d.get('content') or ''),'| files',len(d.get('files') or []))"
```

---

## 7. 常见错误

| HTTP/响应 | 含义 |
|---|---|
| **HTTP 200** + body `code:401` + `{"data":{"reload":true}}` | **仅服务器环境**：缺少/无效 JWT（需加 `-H "token: <JWT>"`，无 `Bearer` 前缀）。**注意：后端统一返回 HTTP 200，鉴权失败体现在 body 的 `code`，请勿按 HTTP 状态码判断** |
| `code: 404` | 路径错误或服务未启动 |
| `code: 500` / 连接拒绝 | 独立服务未运行（先 `go run ./api/tprel/test/server`） |
| `data.nodes` 为空 | 今日未同步（先 POST /sync）或过滤无匹配 |
| graph 中文乱码 | 需 `curl -G --data-urlencode` 处理中文参数（本接口均为 ASCII 参数，一般无需） |

---

## 8. 服务器环境测试（生产 / 联调）

> 服务器上跑的是**主程序**（非 8098 测试服务）：路径与接口一致，但需 **JWT 鉴权**、且 `sync` 会**真实抓取 EasyOps/Apollo** 数据。

### 8.1 前置准备

| 事项 | 说明 |
|---|---|
| 服务已启动 | 主程序 + 已配置 `apollo.base_url` / `apollo.session_id`（或用同名环境变量覆盖），`sync_enable` 开启 |
| 拿到 BASE | `http://{服务器IP或网关}:{端口}`，端口以部署 `system.addr` / 网关转发为准（**不是** 8098） |
| 拿到 JWT | 通过项目登录接口签发，或向后端索取；请求头字段名固定为 `token`（JWT 原文，**不带 `Bearer` 前缀**） |

### 8.2 七个接口命令（可直接复制执行）

```bash
BASE="http://{服务器IP:端口}"        # 按实际替换
TOKEN="<你的JWT>"
G="$BASE/api/external/operationTopology"

# ① 拓扑图全量（默认展开口径：本地真实全量实测 581 节点 / 2943 边；折叠 expandDepNode=0 为 449 / 756；服务器量级参考：约 391 节点 / 3816 边，2026-09-07 实测）
curl -s -H "token: $TOKEN" "$G/graph?env=PRO"

# ② 关系列表分页（pageTotal：默认展开 5266；折叠 expandDepNode=0 为 1418 = 快照关系行数；服务器参考：约 3816）
curl -s -H "token: $TOKEN" "$G/list?page=1&pageSize=5&env=PRO"

# ③ 节点详情（id 必须与 /graph 同口径：折叠态 id 形如 app:{appId}，展开态形如 app:{appId}|{部署项目}|{节点}）
curl -s -H "token: $TOKEN" "$G/detail?id=app:apollo-ehome-pro&env=PRO&expandDepNode=0"

# ④ 中间件类型枚举
curl -s -H "token: $TOKEN" "$G/middlewareTypes"

# ⑤ 手动同步（异步提交，立即返回；随后轮询 sync/status 拿结果）
curl -s -X POST -H "token: $TOKEN" "$G/sync"
curl -s -H "token: $TOKEN" "$G/sync/status"   # 轮询：running=false 且有 result 即完成

# ⑥ 同步日志读取（默认最近一天；指定日期 / 下载完整日志见 §4.4）
curl -s -H "token: $TOKEN" "$G/sync/logs"
curl -s -H "token: $TOKEN" "$G/sync/logs?date=2026-09-20"
curl -OJ -H "token: $TOKEN" "$G/sync/logs?download=1"   # text/plain 附件下载完整日志
```

### 8.3 与本地测试文档的差异（服务器上必看）

| 项目 | 本地 8098（本文 §1~§6） | 服务器（本节） |
|---|---|---|
| 地址 | `http://127.0.0.1:8098` | `http://{服务器IP:端口}`（按部署确认） |
| 鉴权 | 无 | 全部接口 `-H "token: <JWT>"`；无效/缺失返回 **HTTP 200** + body `{"code":401,"data":{"reload":true}}` |
| `/healthz` | test server 有 | **主程序无此路由**；连通性直接用上面 ① 验证 |
| 数据 | 本地快照（OT 1418 行 + 真实关系数据 1426 行；`/list` 行数 = 快照关系行数 1418，不放大） | 真实 Apollo 采集（3816 条关系、391 节点，2026-09-07 实测） |
| `/sync` | 打本地 mock + 真实关系接口，秒级（实测 `costMs≈1761`） | 真实打 EasyOps 代理全量采集，**几十秒~数分钟**；且每日 01:30 cron 已自动同步，一般无需手动触发 |
| 数据源标记 | 快照文件（`data.dataSource: "file"`，test server 固定 file 模式） | `db`（库可用）/ `file`（库不可用降级），见 `data.dataSource` |

### 8.4 返回量级对照（用于判断"像不像真的"）

| 指标 | 本地（2026-09-20 实测，真实全量数据） | 服务器真实（2026-09-07 实测，仅供参考） |
|---|---|---|
| graph nodes / edges（展开，默认） | 581 / 2943 | — |
| graph nodes / edges（折叠 `expandDepNode=0`） | 449 / 756 | 391 / 3816 |
| graph application 节点（展开 / 折叠） | 240 / 108 | — |
| list pageTotal（展开 / 折叠） | 5266 / 1418 | 3816 |
| sync appCount / insertCount | 234 / 1418 | 359 / 3816 |
| sync 关系同步 relationRows | 1426 | — |
| middlewareTypes | 7 类 | 7 类 |

> 两列**数据快照不同**（本地为 2026-09-20 快照 + 已落库的真实关系数据 `app_dp_relation` 1426 行），量级仅用于判断「像不像真的」，不作为对账依据。**本地量级随每日快照与两跳回填结果波动**，判断真实性以「数量级 + 结构」为准。
> 注意 `list pageTotal` 是**候选关系行数**，且**随 `expandDepNode` 口径变化**：折叠（显式 `0`）= 1418（属性补充不扩行，与快照行数同口径）；**默认展开** = 5266（按部署点扩行后行数）。同理 `graph` 的 `nodes` / `edges` 展开后为 581 / 2943。其余示例除特别标注外均为折叠口径。
> **⚠️ 数据侧待办（非接口问题）**：当前快照的应用节点 `node` / `engProject` 属性回填率极低——`node` 只取自跳② DPR（见 [dp_relation.go](file:///Users/a666/Documents/trae_projects/devops-automation/api/tprel/service/dp_relation.go) 的 `joinDpTargets`，DPR 未命中该部署项目时只填 `DepProject`），而快照部署项目与 DPR 的 108 个部署项目几乎无交集，导致 `node` 过滤与按机房展示不可用；`operation_topology` 快照表本身的 `node` / `dep_project` 两列亦全为空。
> 本地完整跑批与行号见 [../logs/README.md](../logs/README.md)。

---

## 9. 七个接口 curl 速查（复制即用）

> 覆盖全部接口（graph / list / detail / middlewareTypes / sync / sync/status / sync/logs）。复制整块到终端，改 `BASE`（本地测试 `http://127.0.0.1:8098`，服务器 `http://{服务器IP:端口}`）与 `TOKEN`（服务器必填，本地留空）即可执行。

```bash
# ====== 环境变量（按需修改）======
BASE="http://127.0.0.1:8098"      # 本地测试；服务器改为 http://{服务器IP:端口}
TOKEN=""                          # 本地测试留空；服务器填 JWT（无 Bearer）
G="$BASE/api/external/operationTopology"
H=()
[ -n "$TOKEN" ] && H=(-H "token: $TOKEN")

echo "== ① graph 拓扑图全量 =="
curl -s "${H[@]}" "$G/graph?env=PRO"

echo; echo "== ② list 关系列表（分页） =="
curl -s "${H[@]}" "$G/list?page=1&pageSize=5&env=PRO"

echo; echo "== ③ detail 节点详情 =="
curl -s "${H[@]}" "$G/detail?id=app:apollo-ehome-pro&env=PRO&expandDepNode=0"

echo; echo "== ④ middlewareTypes 中间件类型枚举 =="
curl -s "${H[@]}" "$G/middlewareTypes"

echo; echo "== ⑤ sync 手动同步 · 异步提交（服务器会真实采集，慎用） =="
curl -s -X POST "${H[@]}" "$G/sync"
echo

echo; echo "== ⑥ sync/status 轮询结果（running=false 且有 result 即完成；执行中可见 stageText/percent/stats） =="
for i in $(seq 1 40); do
  curl -s "${H[@]}" "$G/sync/status" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];print('running',d['running'],'|',d.get('stageText'),'| pct',d.get('percent'),'| err',(d['error'] or '')[:60])"
  curl -s "${H[@]}" "$G/sync/status" | grep -q '"running": false' && break
  sleep 3
done
echo

echo; echo "== ⑦ sync/logs 同步日志读取（默认最近一天；指定日期加 ?date=YYYY-MM-DD，下载完整日志加 &download=1） =="
curl -s "${H[@]}" "$G/sync/logs"
echo
```

### 9.1 detail 返回结构（其余六接口返回结构见 §2.3/§3.3/§4（4.1~4.4）/§5）

`GET /detail?id=app:apollo-ehome-pro&env=PRO&expandDepNode=0`（本地 :8867 真实全量实测，syncDate 随最新快照变化）：

> **入参口径**：上例 `app:apollo-ehome-pro` 为**折叠态** id，**必须同传 `expandDepNode=0`**——默认展开态下该 id 不存在，会返回 `code:7 未找到应用节点 apollo-ehome-pro`；**默认展开**时应用 id 形如 `app:{appId}|{部署项目}|{节点}`（id 自带 2 个 `|`，后端一律按部署点定位），`engProject`/`depProject`/`node` 为单值、多值列表清空。`mw:` / `host:` 节点的「引用应用列表」粒度由入参 `expandDepNode` 决定，应与 `/graph` 同值传入（见《[运维拓扑.md](运维拓扑.md)》§4.5）。

```json
{
  "code": 0,
  "data": {
    "id": "app:apollo-ehome-pro",
    "category": "application",
    "name": "ehome-pro-server",
    "syncDate": "2026-09-20",
    "env": "PRO",
    "apolloAppId": "apollo-ehome-pro",
    "apolloAppName": "ehome-pro-server",
    "appDept": "部门2",
    "appOwner": "zn_huangt",
    "engProject": "",
    "depProject": "proservice-prod-113804-dep",
    "node": "",
    "ppApp": "ehome-pro-server",
    "depProjects": ["proservice-prod-113804-dep"],
    "mwType": "",
    "addr": "",
    "mwDb": "",
    "mwProject": "",
    "depCount": 0,
    "hostId": 0,
    "hostName": "",
    "hostAddr": "",
    "hostAppCount": 0,
    "apolloHosts": [
      { "id": 14, "name": "天翼云眼广州4多AZ生产Apollo",
        "host": "https://tyyy-guangzhou4d-apollo-prod.ctseelink.cn:8158", "appCount": 0 },
      { "id": 3, "name": "天翼云眼Apollo",
        "host": "https://apollo-vcp.21cn.com", "appCount": 0 }
    ],
    "middlewares": [
      { "mwType": "mysql", "addr": "ehome-pro.mysql.ehomeinner.cn:13302", "mwDb": "", "mwProject": "mysql", "depCount": 1 },
      { "mwType": "mysql", "addr": "ehome-pro.mysql.ehomeinner.cn:13303", "mwDb": "", "mwProject": "mysql", "depCount": 2 }
    ],
    "apps": null
  },
  "msg": "操作成功"
}
```

- `id` 必填，取值 = graph 节点的 id（应用 `app:{appid}` / 中间件 `mw:{类型}:{地址}` / 实例 `host:{host_id}`）
- 返回按节点类别聚合：应用 → 依赖的中间件清单 `middlewares[]`；中间件/实例 → 依赖它的应用清单 `apps[]`；不适用分类为 null
- 应用节点直接带出四列属性（`engProject`/`depProject`/`node`/`ppApp`，取属性列表首值）与多值列表（`engProjects`/`depProjects`/`nodes`/`ppApps`），取值来自查询期两跳关系（`app_dp_relation` → `deploy_pinpoint_relation`）作为 appId 属性补充，**不扩行/不分裂节点**
- 想看更精简的返回可加管道：`| python3 -m json.tool`

---

## 10. 单条 curl 命令（独立可复制，共 7 个接口）

> 每条命令**独立成行**、无变量依赖，复制到终端即可执行。默认连本地 8098（无鉴权）；要连**服务器**就把地址换成 `http://{服务器IP:端口}` 并加 `-H "token: <JWT>"`（每个接口下本地/服务器两条，二选一复制）。
> 本模块共 **7 个接口**：查询类 4 个（graph / list / detail / middlewareTypes）＋ 同步类 3 个（sync 异步提交 / sync/status 轮询 / sync/logs 日志读取）。
>
> 另：**本地测试服务**额外提供 1 个非业务接口 `GET /doc`（读取 `docs/前端对接说明.md`，`?download=1` 下载为 text/markdown 附件；目录可用环境变量 `TPREL_DOC_DIR` 覆盖）。**仅本地提供，生产服务不注册该路由**；原型页工具栏的「API 对接文档」按钮即用该接口在线查看/下载文档，并内置 8 个接口的在线调试。

### ① 拓扑图 graph（GET）

```bash
# 本地（复制即跑）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/graph?env=PRO"
# 服务器
curl -s -H "token: <JWT>" "http://{服务器IP:端口}/api/external/operationTopology/graph?env=PRO"
```

### ② 关系列表 list（GET，分页 + env 过滤）

```bash
# 本地
curl -s "http://127.0.0.1:8098/api/external/operationTopology/list?page=1&pageSize=5&env=PRO"
# 服务器
curl -s -H "token: <JWT>" "http://{服务器IP:端口}/api/external/operationTopology/list?page=1&pageSize=5&env=PRO"
```

### ③ 节点详情 detail（GET）

```bash
# 本地（折叠态 id：一个 appId 一个节点；必须同传 expandDepNode=0，否则默认展开态下找不到该 id）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/detail?id=app:apollo-ehome-pro&env=PRO&expandDepNode=0"

# 展开态应用节点（id 形如 app:{appId}|{部署项目}|{节点}，须 URL 编码 %7C；建议直接复制 /graph 返回的 id，避免示例过期）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/detail?id=app:cloud-ehome%7Ccloud-ehome-prod-114804-dep%7C&env=PRO"

# 服务器
curl -s -H "token: <JWT>" "http://{服务器IP:端口}/api/external/operationTopology/detail?id=app:apollo-ehome-pro&env=PRO&expandDepNode=0"
```

### ④ 中间件类型枚举 middlewareTypes（GET）

```bash
# 本地
curl -s "http://127.0.0.1:8098/api/external/operationTopology/middlewareTypes"
# 服务器
curl -s -H "token: <JWT>" "http://{服务器IP:端口}/api/external/operationTopology/middlewareTypes"
```

### ⑤ 手动同步 sync（POST · 异步提交，立即返回）

```bash
# 本地（提交任务，立即返回，不阻塞不超时）
curl -s -X POST "http://127.0.0.1:8098/api/external/operationTopology/sync"
# 服务器
curl -s -X POST -H "token: <JWT>" "http://{服务器IP:端口}/api/external/operationTopology/sync"
```

> `data.started=true`：任务已提交后台执行（与每天 01:30 定时任务互斥排队，不会双跑），返回体见 §4.1；
> `data.started=false`：已有任务在执行/排队，无需重复提交，直接轮询 ⑥ 即可。

### ⑥ 同步状态 sync/status（GET · 轮询直到 running=false）

```bash
# 本地（轮询：running=false 且有 result 即完成）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/sync/status"
# 服务器
curl -s -H "token: <JWT>" "http://{服务器IP:端口}/api/external/operationTopology/sync/status"
```

> 执行中：`running=true`，`stageText`/`percent`/`stats` 实时推进（示例见 §4.2）；
> 完成：`running=false` 且 `error=""`，`result` 为本次同步结果、`stats` 为最终规模统计；
> 失败：`error` 非空即为失败原因，`result` 为空。建议每 3~5 秒轮询一次。

### ⑦ 同步日志读取 sync/logs（GET）

```bash
# 本地（默认最近一天；指定日期加 ?date=YYYY-MM-DD；下载完整日志加 &download=1）
curl -s "http://127.0.0.1:8098/api/external/operationTopology/sync/logs"
# 服务器
curl -s -H "token: <JWT>" "http://{服务器IP:端口}/api/external/operationTopology/sync/logs"
```

> 展示模式 `data` 返回 `date`/`file`/`sizeBytes`/`content`/`truncated`/`files`（`content` 只取最近 256KB，超长 `truncated=true`）；完整内容请用 `download=1` 附件下载。返回与错误结构见 §4.4。

> 想格式化输出（更易读），任一条命令末尾追加 `| python3 -m json.tool` 即可。

---

## 11. EasyViews 网关前缀联调 curl（复制即用）

> 场景：EasyViews 页面环境下联调。前缀用 **EasyViews 网关** `https://test-easyviews.tech.ctseelink.cn` + 本服务路径，鉴权头为 **EasyViews 会话凭证**（`token`/`x-token` 同值、`sessionId`、`sign`、`Authorization`），从 EasyViews 页面 DevTools → Network 抓取。下列命令已内联当前会话示例值。
>
> **提醒**：
> 1. `sign` 与**具体 URL 绑定**（网关校验场景下按 URL 不同需各自抓取；网关若只做统一放行则无需更新）
> 2. 若返回 **HTTP 200** + body `{"code":401,"data":{"reload":true}}`：网关已放行，但透传的 **EasyViews token 过不了本后端验签**（signing-key 不同）→ 需网关 **SSO 换签**（见《前端对接说明》§1.4），非 curl 可绕过
> 3. `token` 约 7 天有效，过期后从浏览器重新抓取替换

### ① 拓扑图 graph

```bash
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/graph?env=PRO' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'
```

### ② 关系列表 list

```bash
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/list?page=1&pageSize=5&env=PRO' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'
```

### ③ 节点详情 detail

```bash
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/detail?id=app:ehome-order&env=PRO' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'
```

### ④ 中间件类型枚举 middlewareTypes

```bash
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/middlewareTypes' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'
```

### ⑤ 手动同步 sync（POST · 异步提交，立即返回）

> 提交后立即返回（不再 504）：`data.started=true` 且 `state.running=true`，后台执行中（与定时任务互斥排队）；若返回 `started=false` 说明已有任务在执行/排队，无需重复提交。提交返回体见 §4.1。

```bash
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/sync' -X POST \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'
```

### ⑥ 同步状态 sync/status（GET · 轮询直到 running=false）

> 提交后**另起一个终端 / 复用同一会话**轮询：`data.running=false` 且 `data.error=""`、`data.result` 非空即完成（执行中 `stageText`/`percent`/`stats` 实时推进；完成后 `stats` 保留最终规模统计）。状态返回体见 §4.2。
> **提醒**：`sign` 与具体 URL 绑定（网关校验场景下），`/sync` 与 `/sync/status` 的 `sign` 需分别从浏览器 DevTools 抓取替换；网关若只做统一放行则无需更新。

```bash
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/sync/status' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'
```

### ⑦ 同步日志读取 sync/logs（GET）

> 展示模式 `data` 返回 `date`/`file`/`sizeBytes`/`content`/`truncated`/`files`（`content` 只取最近 256KB，超长 `truncated=true`）；完整内容用 `download=1` 附件下载。返回与错误结构见 §4.4。
> **提醒**：`sign` 与**具体 URL 绑定**，下面三条命令的 URL 各不相同（是否带 `date` / `download`），网关校验场景下需分别从浏览器 DevTools 抓取替换；网关若只做统一放行则无需更新。

```bash
# ① 默认：最近一天有日志的同步日志
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/sync/logs' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'

# ② 指定日期（date=YYYY-MM-DD；格式非法或无日志会返回对应 msg，见 §4.4 ④⑤）
curl --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/sync/logs?date=2026-09-20' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'

# ③ 下载完整日志（text/plain 附件，不受 256KB 限制；-OJ 自动落盘为 operation-topology_{日期}.log）
curl -OJ --url 'https://test-easyviews.tech.ctseelink.cn/api/external/operationTopology/sync/logs?download=1' \
  -H 'token: YOUR_EASYVIEWS_TOKEN' \
  -H 'x-token: YOUR_EASYVIEWS_TOKEN' \
  -H 'sessionId: YOUR_SESSION_ID' \
  -H 'sign: YOUR_SIGN' \
  -H 'Authorization: Bearer REDACTED_GRAFANA_API_KEY' \
  -H 'X-Forwarded-Origin: https://test-easyviews.tech.ctseelink.cn'
```
