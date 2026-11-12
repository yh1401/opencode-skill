# Prometheus 告警原理与告警中心 MVP 方案

> 日期：2026-11-02（方案）｜ 更新：2026-11-10（P0–P3 全部落地并验证）
> 口径：**只基于当前真实底座**（prometheus-mcp / 统一接入网关 / diagnose 引擎 / tickets 引擎 / store 文件表），不新增外部依赖
> 面向：后端实现 / 架构评审 / 演示准备
>
> **当前状态**：P0（入库+幂等）、P1（自动排障+自动建单）、P2.1（恢复检测）、P2.2（告警中心页）、P2.3（Dashboard 卡片 / 拓扑状态着色）、P3（webhook 入站 / 静默）**已全部实现并通过端到端验证**；2026-11-10 补齐「稽核→排障 / 拓扑→排障 / 拓扑→稽核」三条跨页联动断点，告警入口与拓扑、工单形成闭环。

---

## 一、一句话结论

`list_alerts` 这条链路原本**只做"被排障拉一把"的被动证据**（引擎 step3，关键字子串匹配，零时序语义），**没有主动轮询、没有去重、没有恢复检测、没有自动建工单**。补一个 `internal/alarm` 包（采集循环 + 指纹去重 + 冷却限流 + 自动转排障）就能把告警变成"第一类入口"——**该方案已落地，见第七章落地记录。**

---

## 二、现在怎么实现的：逐层拆解

### 2.1 链路图（5 跳）

```
Prometheus          prometheus-mcp           平台统一接入网关        diagnose 引擎          前端
───────────        ──────────────          ──────────────        ──────────────          ────
GET /api/v1/alerts → list_alerts()        → Gateway.Call()      → collectPrometheus()   → SSE step
state=firing       原样 JSON 字符串        白名单·8s·审计        关键字子串匹配          → root_caused 候选
labels{...}        塞进 content[0].text    JSON-RPC 2.0         keywords 包含即命中
annotations        protocolVersion         Mcp-Session-Id       matchedKeywords 计数
activeAt           2024-11-05                                   不读 activeAt
```

### 2.2 每一跳的代码锚点

#### ① Prometheus

`GET /api/v1/alerts`。返回的是 **Prometheus 自己按 `alerting rules` 算出来的告警列表**，不是事件流。典型响应形态（见 `mockdata/prometheus/list_alerts.json`）：

```json
{
  "status": "success",
  "data": {
    "alerts": [{
      "labels":      {"alertname":"TargetDown","instance":"host:9100","severity":"critical",...},
      "annotations": {"summary":"主机失联","description":"目标已超过 5 分钟未上报指标"},
      "state":       "firing",
      "activeAt":    "2026-10-30T06:12:00+08:00",
      "value":       "0"
    }]
  }
}
```

**三个关键语义边界**（最容易被误解）：

| 边界 | 后果 |
|---|---|
| 没有分组 | 100 台机器 TargetDown = 100 条独立告警 |
| 没有静默/抑制 | 无 Alertmanager 时，`silenced` / `inhibited` 字段根本不存在 |
| 没有历史 | 只能看到当前态，看不到"刚才响过又恢复了" |

→ 去重、抑制、冷却**必须平台自己建**。

#### ② prometheus-mcp

位置：`promthus-mcp/pkg/mcp/handlers.go#L873-L878`

```go
func (s *ServerContainer) listAlertsAPICall(ctx context.Context) (string, error) {
    return s.doSimpleAPICall(ctx, "/api/v1/alerts", "从 Prometheus 获取告警失败",
        func(ctx context.Context, client promv1.API) (any, error) {
            return client.Alerts(ctx)
        })
}
```

**零加工透传**。`promv1.API.Alerts(ctx)` 的响应直接 `FormatOutput` 成字符串，作为 `content[0].text` 返回。平台拿到的是 **JSON 字符串套在 JSON 里**——这是之前 `promUp` bug（见 `audit/engine.go#L256-L283`）的根因，也是告警方案必须写结构化解码器的直接原因。

工具定义在 `promthus-mcp/pkg/mcp/tools.go#L130`，36 个只读工具之一，已在 上游白名单。

#### ③ 接入协议

位置：`platform/backend/internal/mcp/client.go`

极简 streamable-http 客户端，三条硬约束：

- `initialize` 必须先调一次，带 `protocolVersion: "2024-11-05"`，回 `Mcp-Session-Id`
- 后续请求必须带着这个 `Session-Id` 头
- 响应可能是 `application/json` 也可能是 `text/event-stream`，`parseSSE()`（L115）兼容两者

`tools/call` 请求体：`{"jsonrpc":"2.0","id":N,"method":"tools/call","params":{"name":"list_alerts","arguments":{}}}`

#### ④ 平台统一接入网关

位置：`platform/backend/internal/mcp/gateway.go#L144-L165`

三件事，**没有参数感知也没有结果语义**：

```go
func (g *Gateway) Call(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, error) {
    if !g.Allowed(server, tool) { return nil, fmt.Errorf("未放行") } // 黑名单优先白名单
    cctx, cancel := context.WithTimeout(ctx, g.timeout); defer cancel()  // 默认 8s
    res, err := c.CallTool(cctx, tool, args)
    g.audit(CallLog{...})   // 每次调用写一条 tool_call_log
    return res, err
}
```

⚠️ **30s 轮询的话一天 = 2880 条审计**，方案里会处理这个膨胀点。

#### ⑤ diagnose 引擎 step3

位置：`platform/backend/internal/diagnose/engine.go#L270-L321`

**第一步**：无参调 `list_alerts`（L280）。

**第二步**：关键字子串计数（L289-L299）。

```go
text := strings.ToLower(string(raw))
hits := 0
for _, kw := range keywords(req) {   // appId / mwType / addr / host
    if strings.Contains(text, strings.ToLower(kw)) { hits++ }
}
```

`keywords()`（L441-L449）的产物就是这四个字段里 trim 后非空的那些。

**第三步**：附带着还查一次 `up{instance=~"<host>.*"}`（L301-L320），但 `host=="" || impact==nil` 直接 return——**应用视角（只给 appId）根本不会查 up**。

**定级**：rank()（L362-L373）里 `matchedKeywords > 0 → candidate(0.6)`；execute() 末尾 `Confidence >= 0.6 → status = root_caused`（L188-L190）。

**工单生成**：`tickets.Engine.SyncFromSessions()`（`tickets/engine.go#L24-L46`），**不是事件驱动**，而是 `List()` 时懒同步，按 `sess.ID` 幂等。

**前端展示**：300ms ticker 轮询内存会话（`router.go#L495`），增量下发 `event: step` / `event: session`，3 分钟超时。

**mock**：`mockdata/prometheus/list_alerts.json` 静态 6 条，`cmd/mockmcp/main.go` 的 `callTool` 未命中动态处理器就回落读文件。

---

## 三、为什么"能用"但"起不来"：五个硬缺陷

| # | 缺陷 | 实例 | 代码锚点 |
|---|---|---|---|
| A | **没有第 0 步：零主动轮询** | 全仓只有 SSE ticker + 启动 goroutine，没有任何定时器去拉告警 | `cmd/server/main.go` 全文只有 ListenAndServe；`diagnose/engine.go#L74` 只有一次性 `go e.execute` |
| B | **没有告警实体：会话主键是 uuid** | 同一台机器 30 秒内响两次，就会开两个会话、两个工单 | `domain/models.go#L208` DiagnosisSession.ID = uuid.NewString()；没有 alarm 关联字段 |
| C | **子串匹配三缺陷** | 假阳性：`host=10.214.100.1` 命中 `10.214.100.10`；无语义：不知道命中的是 `instance` 还是 summary 文案；无时序：不读 `activeAt` | `diagnose/engine.go#L289-L299` |
| D | **应用视角拿不到 up** | `hostOf(addr)` 空 → 直接 return | `diagnose/engine.go#L301-L304` |
| E | **审计膨胀** | 30s × 一天 = 2880 条 `tool_call_log` 文件 | `mcp/gateway.go#L159` 每次都写 |

---

## 四、告警中心 MVP 方案

### 4.1 设计目标（三条，不多做）

1. **告警成为第一类入口**：定时采集 → 结构化 → 去重 → 自动转排障 → 自动建单
2. **不新增底座**：只加 `internal/alarm` 一个包 + 一张表 + 一组 API
3. **可演示**：mock 能模拟"触发 → 自动开单 → 恢复"完整生命周期

### 4.2 数据模型

```
store kind = "alarm_event"    key = fingerprint（幂等键）

  fingerprint    alertname|instance|severity   ← 示例：TargetDown|sdk3a4-detectmessage1:9100|critical
  alertName       TargetDown
  instance        sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9100
  host            sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn
  severity        critical | warning | info
  labels          map[string]string（原样留档，供后续按 type/appId 关联）
  summary         annotations.summary
  state           firing | resolved
  firstSeenAt     time.Time
  lastSeenAt      time.Time
  fireCount       累计次数（resolved 后再次 firing 则 +1）
  missCount       采集窗口未出现次数（恢复防抖）
  lastDiagAt      上次自动排障时间（冷却判断）
  diagSessionID   关联排障会话
  ticketID        关联工单
```

三个设计理由：

- **instance 做幂等键**：同一机器同一告警规则永远是同一个 `alarm_event`，状态机在这一行内部走 firing → resolved → firing
- **labels 原样留档**：后续稽核 C1/C3 扩展、Apollo 配置告警接入，都能复用这张表
- **diagSessionID + ticketID 非强制**：info 级别告警只入库不建工单

### 4.3 采集循环

位置：`platform/backend/internal/alarm/loop.go`

```
每 ALARM_POLL_INTERVAL（默认 30s）：
  ① raw ← gateway.Call(ctx, "prometheus", "list_alerts", {})
     （走一个特殊 CallSilent，不落审计日志，详见 4.7）
  ② alerts ← parseAlerts(raw)
     解析 content[0].text → JSON → data.alerts
     每一条转成内部 alertView{fingerprint, host, ...}
  ③ seen := set()
     for a in alerts:
         fp := a.fingerprint; seen.add(fp)
         ev := store.Get("alarm_event", fp)
         if ev == nil:                        // 新告警
             ev = newAlarmEvent(firing, now, fireCount=1)
             maybeAutoDiagnose(ev)
         elif ev.state == "resolved":          // 死灰复燃
             ev.state = firing; ev.fireCount++
             ev.firstSeenAt = now
             maybeAutoDiagnose(ev)
         else:                                // 持续 firing
             ev.lastSeenAt = now              // fireCount 不变
         store.Put("alarm_event", ev)
  ④ 恢复检测（在 seen 之外）
     for ev in store.List("alarm_event") where ev.state == "firing" and !seen.contains(ev.fp):
         ev.missCount++
         if ev.missCount >= ALARM_MISS_TOLERANCE（默认 2）:
             ev.state = resolved
             ev.resolvedAt = now
             ev.diagSessionID 对应工单追加"告警已恢复"备注
         store.Put("alarm_event", ev)
```

**两个关键点**：

- **恢复必须连续两轮确认**（`missCount >= 2`）：Prometheus 抓取抖动会产生假恢复，两轮确认是行业标准
- **fireCount 在持续 firing 时不累加**：只在"首次出现"或"resolved → firing"时 +1，避免长时间 firing 时无限增长

### 4.4 自动排障准入（防风暴）

| 规则 | 配置名 | 默认值 | 理由 |
|---|---|---|---|
| 只自动处理 severity ∈ {critical, warning} | 硬编码 | — | info 只入库 |
| 同指纹冷却 | `ALARM_COOLDOWN` | 10 min | 抑制同一告警的风暴 |
| 同 host 已有活跃会话 | — | 跳过新建，把新告警作为 Step 追加 | 一次故障一个会话 |
| 单轮自动触发上限 | `ALARM_MAX_AUTO_DIAG` | 5 | 防止批量 TargetDown 瞬间打爆引擎 |
| 全局开关 | `ALARM_AUTO_DIAG` | true | 演示 / 压测可关 |

**告警 → 排障请求的映射**（三层策略）：

```
1. labels.appId 存在        → AppID = labels.appId
                               （应用视角排障，复用 diagnose.Request.AppID）
2. labels.type 存在          → MiddlewareType = labels.type, Addr = instance
   （host 不在 topo 中间件列表时的兜底，直接 type 推断）
3. host 在 topo 中间件列表中 → MiddlewareType = topo 匹配到的类型, Addr = instance
                               （最理想路径，稽核 C1/C3 的孤儿正好从这里进来）

Symptom = alertname + "：" + annotations.summary
```

**第三条**是打通稽核场景的关键：`host` 在 topo 中间件 host 列表中 → 那这个 host 至少不孤独 → 那 `host` 不在 topo 但 CMDB 有 → 稽核 C1/C3 的孤儿 → 两个场景真咬合。

### 4.5 自动建单

不改 `tickets.Engine` 的逻辑：`diagnose.Start` 完成后，会话自然落进 `tickets.List()` 的懒同步，按 `sess.ID` 幂等生成工单。

只需在 `alarm_event` 上回写 `diagSessionID`，并在 API 层做二次关联展示：`GET /api/alarms/:fp` → 查 `alarm_event.diagSessionID` → 查 diagnose store → 再查 ticket store。

### 4.6 恢复闭环

告警恢复时：

- `alarm_event.state = resolved`，记 `resolvedAt`
- 若关联工单 `open` 或 `in_progress` → **不自动关单**，只追加一条 `Findings: 告警已恢复（<time>），建议人工确认处置`（自动关单在运维场景风险高）
- 若工单已 `resolved` → 不动

### 4.7 API

```
GET  /api/alarms                    ?state=firing&severity=critical&host=&appId=&limit=
GET  /api/alarms/stats              按 severity / state 聚合，供 Dashboard
GET  /api/alarms/:fp                单条详情（含关联会话 / 工单）
POST /api/alarms/poll               手动拉一轮（演示与排障都靠它）
POST /api/alarms/:fp/diagnose       人工强制开排障（忽略冷却）
POST /api/alarms/webhook            预留给未来 Alertmanager 推送（路由占位，不实现）
```

### 4.8 配置（`config.go` 追加，走环境变量）

```go
ALARM_ENABLED          true       // 总开关
ALARM_POLL_INTERVAL    30s        // 采集周期
ALARM_COOLDOWN         10m        // 同指纹自动排障冷却
ALARM_MAX_AUTO_DIAG    5          // 单轮自动开单上限
ALARM_AUTO_DIAG        true       // 自动排障开关
ALARM_MISS_TOLERANCE   2          // 恢复防抖：连续几次没在 firing 列表出现才判定 resolved
```

### 4.9 审计膨胀处理

`mcp/gateway.go` 新增一个 `CallSilent(ctx, server, tool, args)` 方法，与 `Call` 同逻辑但**不落 `tool_call_log`**。`alarm` 包用 `CallSilent` 采集，演示时切回 `Call` 看完整审计。

如果想统一落盘，也可以让 `CallLog.At` 精确到分钟，或给 `alarm` 单独一张 `alarm_poll_log`（只存异常）。默认走前者。

### 4.10 前端

| 页 / 位置 | 内容 |
|---|---|
| **新增「告警中心」页** | 列表（指纹 / 主机 / 级别 / 状态 / 首次 / 最近 / 关联会话 / 工单）+ 筛选 + 「开排障」按钮（复用断点①动线） |
| **Dashboard 加两卡** | 当前 firing 数（按 severity 分色）+ 近 24h 自动开单数 |
| **拓扑节点着色** | 有 firing 告警的节点描边（`state=firing` 红色 dashed，`resolved` 灰色），与排障 / 工单状态共用一套视觉 |

### 4.11 mock 演示支持

现在 `mockdata/prometheus/list_alerts.json` 是静态 6 条。加一个**生命周期模拟**（`cmd/mockmcp/main.go` 的 `callTool` 里加 `list_alerts` 动态处理器）：

```
启动后 0–90s   → 6 条全 firing（含 TargetDown ×2）
启动后 90–150s → 只剩 4 条（TargetDown 消失，模拟恢复）
启动后 150s+   → 6 条全 firing（死灰复燃）
```

这样一轮演示能完整走完：**自动建会话 → 自动建单 → 告警恢复标记 → 再次 firing**。

### 4.12 分期与验收

| 期 | 内容 | 验收标准 | 状态 |
|---|---|---|---|
| **P0** | `internal/alarm` 包：解析器 + 指纹 + 采集循环 + `GET /api/alarms` + `POST /poll` + `CallSilent` | 手动 poll 后 6 条 mock 告警入库；重复 poll 不产生新行，`fireCount` 正确累加 | ✅ 2026-11-03 已验证（`upserted=6` 两轮不变） |
| **P1** | 自动开排障 + 冷却 / 去重 / 限流 + 回写 `diagSessionID` + `GET /api/alarms/:fp` | 一轮里只自动开 ≤5 个会话；同 host 10 分钟内不重复；工单自动出现在 Tickets 页 | ✅ 已验证（`autoDiag>0`、`diagSessionId`/`ticketId` 回填、同 fp 二次 ForceDiagnose 幂等） |
| **P2.1** | 恢复检测 + 工单追加恢复备注 | mock 切到"恢复"后连续 2 轮 poll 内 `state=resolved`；工单收到恢复备注 | ✅ 已验证（`missCount≥2 → resolved`，Findings 追加"告警已恢复（时间），建议人工确认处置"） |
| **P2.2** | 告警中心页 | 列表 + 筛选 + `ForceDiagnose` 按钮 + 详情抽屉 | ✅ 已验证（浏览器 6 项全 PASS：4 统计卡 / 6 行表格 / 分级药丸 / 时间列 / 零 console 错误） |
| **P2.3** | Dashboard 两张卡 + 拓扑节点着色 | 当前 firing 数（分级分色）、近 24h 自动开单数；拓扑节点按告警/排障/工单状态描边 | ✅ 已验证（Dashboard「当前告警」「24h 自动建单」两卡上线；拓扑五级着色，firing 经自动排障后落为「排障中」红色虚线描边，见总体架构方案 §13.4） |
| **P3** | webhook 入站 / 静默 / Alertmanager 对接 | 外部告警可推入、静默期内不自动开排障 | ✅ webhook（`POST /api/alarms/webhook`）、手动静默（30m/2h/24h/永久）、`GET /api/v2/alerts` 双通道采集与 `mergeAlerts` 抑制/分组补全均已落地（`ALERTMANAGER_ENABLED` 默认 true，`ALERTMANAGER_URL` 默认 `http://127.0.0.1:9093`，不可达时静默降级） |

**实际落地范围 = P0 + P1 + P2.1 + P2.2 + P2.3 + P3（webhook / 静默 / Alertmanager 双通道）**，与"两场景贯通方案断点⑤"完全对齐；2026-11-10 三条跨页联动断点（①②③）也已闭环。

---

## 五、对两场景贯通断点的影响

告警中心 MVP 落地后，原方案断点⑤（Prometheus 告警 → 自动排障）**从"无底座"变成"底座已齐"**。整个贯通流程变成：

```
                    ┌── 稽核页（C1/C2/C3/C4）──┐
                    │   每行风险 host 可一键开排障 │  ← 断点① ✅ 2026-11-09
                    └──────────────────────────┘
                    ┌── 拓扑节点详情卡 ─────────┐
                    │   开排障 / 跑稽核          │  ← 断点②③ ✅ 2026-11-09
                    └──────────────────────────┘
Prometheus firing ──→ alarm 采集循环 ──→ 自动排障 ──→ 自动建工单  ← 告警中心 P1
（告警入口）                                                        闭环
                                                    恢复 → 工单备注
```

日志并入排障 step5（断点④）和告警中心**没有依赖关系**，可以独立推进；两者已于 2026-11-03 分别随 T3、T1/T2 落地。

---

## 六、两个必须提前认的风险

| 风险 | 对策 |
|---|---|
| **子串匹配必须废掉** | 新解析器严格按 `labels.instance` 精确匹配 + 前缀匹配分开处理，避免 `10.214.100.1` 命中 `10.214.100.10` |
| **审计膨胀** | `CallSilent` 绕开；或换按分钟归并 `tool_call_log`（默认走前者） |

---

## 附录：关键文件清单

| 文件 | 现状 | 方案改动 |
|---|---|---|
| `platform/backend/internal/alarm/loop.go` | 不存在 | 新增 |
| `platform/backend/internal/alarm/parser.go` | 不存在 | 新增：告警 JSON → `alarmView` 结构体 |
| `platform/backend/internal/mcp/gateway.go` | 已有 `Call` | 追加 `CallSilent` |
| `platform/backend/internal/config/config.go` | 已有 Config | 追加 6 个 `ALARM_*` 字段 |
| `platform/backend/cmd/server/main.go` | 启动 goroutine 只有 ListenAndServe | 追加 alarm 循环的 goroutine |
| `platform/backend/internal/api/router.go` | 已有 `/api/mcp/*` | 追加 `/api/alarms/*` 7 个路由 |
| `platform/backend/cmd/mockmcp/main.go` | `list_alerts` 无动态处理器 | 追加生命周期模拟（启动时间分桶） |
| `platform/frontend/src/pages/Dashboard.tsx` | 已有 Dashboard | 追加两个告警统计卡 |
| `platform/frontend/src/nav.tsx` | 顶部菜单 | 追加「告警中心」 |

---

## 七、落地记录（2026-11-03）

### 7.1 代码清单（实际改动）

| 文件 | 类型 | 说明 |
|---|---|---|
| `internal/alarm/parser.go` | 新增 | 两层解码（上游 `content[0].text` → Prometheus JSON → `[]alertView`）；`fingerprint()` = `alertname|instance|severity` 全小写 |
| `internal/alarm/loop.go` | 新增 | `Engine`：`Start` 采集循环、`PollOnce`、`List/Get/Stats`、`ForceDiagnose`、`SyncTickets`；`runAutoDiagnose` 四条准入（severity∈{critical,warning} / 同指纹冷却 / 同 host 无活跃会话 / 无 diagSessionID）；`buildDiagnoseRequest` 三层映射（`labels.appId` → topo 匹配 → `labels.type`） |
| `internal/api/alarm_handler.go` | 新增 | 5 个 handler：list（内嵌完整 `AlarmEvent` + 二次关联）、stats、get、poll、diagnose |
| `internal/api/router.go` | 改动 | `/api/alarms` 路由组（5 条）；`fail()` 改为 nil-safe（空 error 按状态码给默认文案） |
| `internal/mcp/gateway.go` | 改动 | 抽出 `callInner`，新增 `CallSilent`（不写审计，供高频轮询） |
| `internal/config/config.go` | 改动 | 6 个 `ALARM_*`：`ENABLED` / `POLL_INTERVAL_SEC`(30) / `COOLDOWN_MIN`(10) / `MAX_AUTO_DIAG`(5) / `AUTO_DIAG`(true) / `MISS_TOLERANCE`(2) |
| `cmd/server/main.go` | 改动 | 构建 `alarm.Engine`；`AlarmEnabled` 时 `go al.Start(ctx)`；`context.WithCancel` 优雅退出 |
| `cmd/mockmcp/main.go` | 改动 | `promListAlerts(startTime)` 生命周期模拟：0–90s 全 6 条 → 90–150s 去掉 2 条 TargetDown → 150s+ 恢复全 6 条 |
| `frontend/src/types.ts` | 改动 | `AlarmEvent` / `AlarmStats` / `AlarmListResp` / `AlarmPollResp` + severity/state/diagStatus 别名 |
| `frontend/src/api.ts` | 改动 | `alarmList/alarmStats/alarmGet/alarmPoll/alarmDiagnose` |
| `frontend/src/pages/AlarmCenter.tsx` | 新增 | 统计卡×4 + 表格（等级/状态/名称/现象/排障/工单/时间/操作）+ 筛选 + 详情 Drawer + 15s 自动刷新 |
| `frontend/src/nav.tsx` | 改动 | `/alert-center` 由 `Placeholder` 替换为 `<AlarmCenter />`，状态 `planned → done` |

### 7.2 验证结果

| 场景 | 证据 |
|---|---|
| 入库 + 幂等 | 两轮 `POST /poll` 均 `upserted=6`，`fireCount` 不翻倍，`lastSeenAt` 刷新 |
| 自动排障 | poll 返回 `autoDiag>0`；同 host 只建 1 会话；同 fp 二次 `ForceDiagnose` 返回同一 session |
| 自动建单 | `diagSessionId` 回填后 poll 触发 `SyncTickets`，`ticketId` 回填，`/api/tickets` 可见 |
| 恢复检测 | mock 90–150s 窗口去掉 TargetDown → 连续 2 轮 poll 后 `state=resolved`，工单 Findings 追加"告警已恢复（<time>），建议人工确认处置" |
| 重启持久化 | kill server 重启后 `diagSessionId` / `ticketId` / `fireCount` 全部保持 |
| 并发 | 5 个 poll 并发 → 仍 6 行；poll 与 ForceDiagnose 并发不丢锁 |
| 环境变量 | 日志确认 `poll_interval=10m / cooldown=10m / auto_diag=true / max_auto_diag=5` 生效 |
| 前端渲染 | 浏览器 6/6 PASS：4 统计卡、6 行表格、分级药丸、时间列 `2026/11/3 11:07:12`、零 console 错误 |

### 7.3 落地期间修掉的 3 个 Bug

| # | 层 | 现象 | 根因 | 修复 |
|---|---|---|---|---|
| 1 | 后端 | `GET /api/alarms/不存在的fp` 返回 500 | `fail(c,404,nil)` 内 `err.Error()` 空指针 | `fail()` nil-safe |
| 2 | 后端 | 前端时间列 `Invalid Date`、labels 为空 | `alarmList` 手写 `gin.H` 挑字段，漏 `firstSeenAt`/`lastSeenAt`/`labels`/`missCount` | 内嵌 `alarm.AlarmEvent` 全量序列 + merge 二次关联 |
| 3 | 前端 | 页面白屏，`Cannot read properties of undefined (reading 'node')` | `a.labels.node` 未判空 | `a.labels?.node` |

### 7.4 与方案的偏差（需知悉）

- **mock 数据未读文件**：`promListAlerts` 内置 6 条告警模板，而非读 `mockdata/prometheus/list_alerts.json`。好处是生命周期可按启动时间分桶，坏处是改 mock 数据要改代码。
- **未做 SSE 增量推送**：前端用 15s 轮询替代。MVP 够用，告警量大时再换 SSE。
- **未接 Alertmanager**：仍走 prometheus `list_alerts`，拿不到 `silenced`/`inhibited` 状态。

---

## 八、下一阶段规划（阶段二：告警联动深化）

### 8.1 目标

告警中心已完成"采集→入库→自动排障→自动建单→恢复"闭环。下一阶段解决三个"只有数据、没有看见 / 没有联动 / 没有度量"的问题：

1. **看不见**：Dashboard 只有上游探针，没有告警面；拓扑图上分不清哪台机器在告警
2. **不联动**：告警排障证据链缺少日志证据（断点④）；指标劣化类隐患也还没纳入同一条流水线
3. **不度量**：考核指标"1 分钟发现 / 5 分钟定位"没有埋点，现在有告警数据源了可以开始算

### 8.2 任务拆解（按依赖顺序）

| 优先级 | 任务 | 内容 | 验收标准 | 依赖 | 估时 |
|---|---|---|---|---|---|
| **P0** | ✅ T1 告警可视化收尾（P2.3） | Dashboard 加 2 张卡（firing 分级数 / 24h 自动开单数）；拓扑节点 firing 时红色虚线描边 + tooltip 带告警名；排障工作台顶部告警横幅 | 有 firing 告警时 Dashboard 数字非 0；拓扑对应节点描边可见 | 告警 API 已有 | 1–2 天 |
| **P0** | ✅ T2 告警中心增强 | 服务端筛选+分页（`?state=&severity=&q=&page=`）；近 24h 告警趋势柱图（按小时聚合）；告警 timeline（firing→diagnose→ticket→resolved 四节点）；手动静默（30m/2h/永久，`AlarmEvent.SilencedUntil`） | 100 条告警下分页正常；静默期内不自动排障 | 与 T1 无依赖，可并行 | 2–3 天 |
| **P1** | ✅ T3 日志证据并入排障（现第 10 步，断点④） | diagnose 新增日志证据步：优先用告警 `host` 调 ES `search`（计数 + 样本），ES 不可用时回退 log_analyzer（firing 前 10min 窗口），产出"错误日志 TopN + 异常模式"；均不可用时步骤标 `skipped` 不阻断 | 排障 SSE 出现日志证据步；日志命中数与证据卡可见；服务宕机时排障仍完成 | log_analyzer 已跑通（iframe 版）+ 2026-11-06 已接入 ES | 3–4 天 |
| **P1** | T4 指标劣化 → 隐患告警 | 独立 sweep（每 15min）算 1h 斜率，超阈值写 `AlarmEvent{Source:"degradation", Severity:"warning"}`，复用同表/同套自动排障 | `/api/alarms` 出现 `source=degradation`；可一键开排障 | T2 的筛选字段 | 3–4 天 |
| **P2** | T5 考核指标埋点看板（#29/#30） | 汇总 `firstSeenAt → sessionCreatedAt → rootCausedAt` 三段时长，输出 P50/P90 与达标率；`/metrics` 从占位转真实页 | 有数据时看板出数；空数据不报错 | T1–T4 产生的数据 | 2–3 天 |
| **P2** | T6 巡检 Cron 调度 | 复用 `InspectionTask.Cron`，内嵌轻量调度器（每分钟扫到期任务），触发 `/inspect/run` | 配置 `*/30 * * * *` 后自动产生批次 | 独立小项 | 1–2 天 |
| **P3** | ✅ T7 Alertmanager 接入（webhook 已落地） | `POST /api/alarms/webhook` + `GET /api/v2/alerts` 双模式；`silenced`/`inhibited` 不入库；分组标签落 `labels.group` | 静默告警不入库 | T2 | 2 天 |

> 落地记录（2026-11-03）：T1/T2 已完成。后端新增 `GET /api/alarms/trend?hours=`、`POST /api/alarms/:fp/silence`、`POST /api/alarms/:fp/unsilence`，`GET /api/alarms` 支持 `state/severity/q/page/pageSize` 服务端筛选分页并回填 `diagStatus/ticketStatus` 等关联字段；静默在 `runAutoDiagnose` 准入 1.5 生效。前端 Dashboard 新增「当前告警 / 近 24h 自动建单」两卡；告警中心重写为统计卡 + 24h 趋势柱图 + 筛选分页表格 + 详情 timeline 抽屉 + 行内静默；排障页顶部新增活跃告警横幅；运维关系拓扑在 `topology_dagre.html` 内叠加告警描边与悬停提示（30s 轮询，兼容该页 `{code,data}` 信封）。

> 落地记录（2026-11-10）：拓扑着色统一升级为「排障 / 工单状态」五级描边（`opGraph` 为节点附带 `diagRefs` / `ticketRefs`，前端按 `排障中(红虚) > 工单待处理(橙) > 已定位根因(绿) > 工单已解决(青) > 排障失败(灰虚)` 优先级着色，统计栏附动态图例，详情卡顶部加状态横幅），原 firing 告警描边由「firing → 自动排障 → 排障中」链路承接。同时落地 `POST /api/alarms/webhook`（`alarm.IngestWebhook`）与手动静默四档，P3 的入站侧闭环；出站侧 `GET /api/v2/alerts` 双通道采集与 `mergeAlerts`（抑制态/分组补全）也已在 `alarm/alertmanager.go` 实现，`ALERTMANAGER_URL` 不可达时静默降级。

> 落地记录（2026-11-03，T3）：新增 `backend/internal/diagnose/logevidence.go`（日志分析服务客户端 + markdown 报告解析：总行数/错误数/警告数、错误类型 TopN、高频错误类、关键错误分析、错误模式识别）；`engine.execute` 在根因排序后追加 `log` 步骤（`tool=log_analyzer`），`Request` 新增可选 `logPath`，缺省按 `LOG_PATH_TEMPLATE`（默认 `/tmp/aiops-logs/{host}.log`）用告警实例 host 推导。新增环境变量 `LOG_ANALYZER_ENABLED`、`LOG_ANALYZER_URL`、`LOG_PATH_TEMPLATE`、`LOG_ANALYZE_TIMEOUT_SEC`、`LOG_ANALYZE_POLL_MS`。降级：未配置服务 / 路径缺失 / 调用失败或超时 → 步骤 `skipped` + 原因，会话照常结束。前端排障页新增可选「日志路径」输入，并在证据链中渲染日志证据卡（命中文件/总行数/错误数/警告数 + 错误类型 TopN + 关键问题）。说明：日志分析服务接口仅接受文件路径，暂不支持 from/to 时间裁剪，`windowMinutes=10` 仅作证据关联标注；本机联调时该服务因自身 LLM 配置问题（`LLMClient` 收到 dict）无法完成分析，成功分支用本地桩验证，真实服务下步骤按设计降级。

### 8.3 建议节奏

- **第一批（T1 + T2）✅ 2026-11-03 完成**：纯展示与交互，不动排障引擎，风险最低，演示效果提升最明显
- **第二批（T3 ✅ + T4）**：进入引擎层，T3 已打通断点④（两场景贯通的关键一环），T4 把"隐患"也纳入同一条告警流水线
- **第三批（T5 + T6 + T7）**：度量与调度、外部对接，可按演示需要对调

### 8.4 阶段二整体验收口径

1. 一条告警从 `firing` 到 `resolved`，在**告警中心（timeline）→ 排障工作台（5 步证据）→ 工单（处置记录）→ Dashboard（趋势）** 四处都能看到且数据一致
2. 关掉 log_analyzer 服务，排障仍能跑完（step5 优雅降级）
3. 指标劣化与 Prometheus 告警共用同一套去重/冷却/自动排障规则，无两套逻辑
4. 考核指标看板能算出"发现/定位"时长的 P50/P90，且分母为 0 时不崩

### 8.5 风险

| 风险 | 对策 |
|---|---|
| 告警量上来后 15s 轮询 + 前端全量拉取会卡 | T2 一并做服务端分页；后续换 SSE 增量 |
| 日志证据时间窗与告警时间对不上（时区/延迟） | 外部服务当前只接受文件路径、不支持 from/to；T3 先按文件全量分析并把 `windowMinutes=10` 作为关联标注，阶段三 S5 补时间裁剪 |
| 劣化告警与 Prometheus 告警同 host 撞车，重复开排障 | 复用同 host 准入规则；不同 `Source` 的同 host 告警共享同一会话，仅追加证据 |
| mock 生命周期只在 2.5 分钟内切换，长演示不好控 | 增加 `?force=` 调试参数或 `ALARM_MOCK_SCENARIO` 环境变量控制阶段 |

---

## 九、阶段三规划（告警运营化：预防 → 度量 → 接入）

### 9.1 目标与边界

阶段二把告警从"数据"推进到"看得见 + 能联动 + 有日志证据"。阶段三聚焦**告警运营化**，做五件事：

1. **预防**：Prometheus 规则告警之外的"隐患"（指标劣化）也进入同一张告警表
2. **自动化**：巡检任务按 cron 自动执行，产出批次与工单，支撑"巡检自动化执行率 > 95%"
3. **度量**：把"1 分钟发现 / 5 分钟定位"变成可计算的埋点与看板
4. **接入**：对接 Alertmanager，拿到 `silenced` / `inhibited`，避免无效排障与重复工单
5. **加固**：让日志分析服务真实可用，并补上告警时间窗裁剪

边界：不改现有告警引擎主流程，不引入新的外部底座；所有新告警源共用同一套指纹幂等 / 冷却 / 自动排障 / 自动建单。

### 9.2 任务拆解

| 优先级 | 任务 | 内容 | 验收标准 | 依赖 | 估时 |
|---|---|---|---|---|---|
| **P0 ✅** | S1 指标劣化 → 隐患告警（原 T4） | 独立 sweep 每 15min 取指标近 1h 序列算斜率，超阈值写 `AlarmEvent{Source:"degradation", Severity:"warning"}`，标签带指标名/阈值/斜率；复用同表、同准入、同自动排障 | `/api/alarms?source=degradation` 有数据；同 host 不重复开排障；恢复后可 resolved | Prometheus 上游服务需支持 `range_query`（mockmcp 若不支持需先补） | 3–4 天 |
| **P0 ✅** | S2 巡检 Cron 调度（原 T6） | 复用 `InspectionTask.Cron`，内嵌每分钟调度器扫描到期任务并触发 `/inspect/run`；批次幂等、失败重试、下次执行时间顺延 | 配置 `*/30 * * * *` 到点自动产生批次；服务重启不重复执行；手动与自动批次互不覆盖 | 无 | 1–2 天 |
| **P1 ✅** | S3 考核指标埋点与看板（原 T5） | 埋点 `firstSeenAt → sessionCreatedAt → rootCausedAt → ticketCreatedAt`；新增汇总接口返回 P50/P90 与达标率；`/metrics` 从占位转真实页 | 有数据时看板出数；分母为 0 不报错；数值与告警 timeline 一致 | S1、S2 产生的数据 | 2–3 天 |
| **P1 ✅** | S4 Alertmanager 接入与抑制联动（原 T7） | webhook 入站 + `/api/v2/alerts` 主动拉双模式；`silenced`/`inhibited` 不入库；fingerprint 与现有规则对齐；分组标签落 `labels.group` | 静默期间零排障、零工单；解除静默后恢复采集；与 `list_alerts` 不重复 | 告警中心（T2） | 2 天 |
| **P2** | S5 日志证据加固 | 修复外部服务 LLM 配置问题；支持 `from`/`to` 时间窗（服务端参数或平台侧按 host + 时间裁剪临时文件）；证据卡展示真实窗口 | 真实报告可用；10 分钟窗口真实生效；窗口信息在证据卡可见 | 外部 log_analyzer | 1–2 天（外部依赖） |

### 9.3 建议节奏

- **第一批（S1 + S2）**：都在后端定时任务层，产出新告警源与新自动化数据，为度量铺路
- **第二批（S3）**：依赖 S1/S2 的数据沉淀，先埋点后看板
- **第三批（S4 + S5）**：外部系统对接，按 Alertmanager / log_analyzer 的实际可用性排期

### 9.4 阶段三验收口径

1. `degradation` 告警与 Prometheus 告警同表、同准入、同冷却，不存在两套逻辑
2. 巡检任务按 cron 自动执行率 ≥ 95%，失败可见、可重试
3. `/metrics` 能给出"发现/定位"时长的 P50/P90 与达标率，空数据不崩
4. Alertmanager 静默期间不产生排障与工单，解除后自动恢复
5. 日志证据真实可用，且能按告警时间窗裁剪并在证据卡标注窗口

### 9.5 风险与前置校验

| 风险 | 对策 |
|---|---|
| mockmcp 的 Prometheus 工具没有 `range_query` | 先补工具；或平台侧改为多次 instant query 采样，再算斜率 |
| cron 与手动巡检并发重复 | 批次唯一键 `taskId + startedAt`，调度器加运行锁 |
| 埋点缺少历史数据 | 只从上线时刻起算，看板明确展示"统计起始时间" |
| Alertmanager 与轮询双源重复入库 | 统一 fingerprint，webhook 优先、轮询做补偿去重 |
| 日志分析服务配置与代码不一致（本机已复现） | S5 先修服务自身 `LLMClient` / config 映射，再谈时间窗 |

### 9.6 S1 / S2 落地记录（2026-11-03）

#### S1 指标劣化 → 隐患告警

| 项 | 实现 |
|---|---|
| 采集 | `internal/alarm/degradation.go`：`StartDegradation` 定时（默认 5min）+ `SweepDegradation` 单轮扫描 |
| 算法 | 对每个目标调 `range_query` 取近 60min 序列，前 1/3 均值 vs 后 1/3 均值，涨幅超阈值即判定劣化 |
| 告警 | `Source="degradation"`、`AlertName="MetricDegradation"`、`Severity="warning"`，labels 带 `metric` / `middlewareType` / `type` / `source` |
| 幂等 | fingerprint = `degradation\|host\|metricLabel`；持续劣化只刷 `lastSeenAt`，不重复计数 |
| 恢复 | 单独维护生命周期：未命中的 degradation 告警 `MissCount++`，达 `ALARM_MISS_TOLERANCE` 置 `resolved` 并追加工单 Findings |
| 联动 | 结束调用 `autoDiagnoseEvents`，与 Prometheus 告警共用 severity / 冷却 / 同 host / 已关联会话四道准入 |
| 手动入口 | `POST /api/alarms/degradation/scan`；前端「告警中心 → 扫描劣化」按钮，完成提示扫描数/劣化数/新增/恢复/自动排障 |
| 展示 | 列表 `source==='degradation'` 显示「指标劣化」橙色标签；详情抽屉新增「来源」字段 |

#### S2 巡检 Cron 调度

| 项 | 实现 |
|---|---|
| 调度器 | `internal/inspect/scheduler.go`：`Start` 每 `INSPECT_CRON_TICK_SEC`（默认 20s）tick 一次，`Tick` 扫描全部 `inspection_task` |
| 表达式 | 自研 5 段解析：`*`、`*/n`、`a`、`a-b`、`a-b/n`、逗号列表、日/周 `?`；周字段兼容 `0` 与 `7` 表示周日 |
| 幂等 | 同一任务同一分钟只触发一次；已有 `running` 批次时跳过本轮，避免与手动批次互相覆盖 |
| 校验 | `inspect.ValidateCron` 在创建/更新任务时前置拦截非法表达式（400）；运行期非法仅首次打日志 |
| 触发 | 批次 `Trigger="cron"`，与 `manual` / `api` 区隔，前端批次列表可直接分辨 |
| 前端 | 新建任务表单新增 Cron 输入（5 段校验），任务表新增 Cron 列（空值显示「手动」） |

#### 验证结果（Mock 模式）

- `POST /api/alarms/degradation/scan` → `scanned=10`、`degraded=9`、事件写入 `alarm_event` 并出现在告警列表（含自动排障 + 自动建单各 1 例）
- 创建 `cron="* * * * *"` 的任务后，每 20s tick 在分钟边界触发一次，批次 `trigger=cron`、`status=succeeded`
- 非法表达式 `0 2 * *` → `400 {"error":"cron 需要 5 段（分 时 日 月 周），实际 4 段"}`

### 9.7 S3 落地记录（2026-11-03）

| 项 | 实现 |
|---|---|
| 汇总接口 | `GET /api/metrics/kpi?days=7`（`internal/kpi`），按自然日窗口过滤告警 / 会话 / 工单 / 巡检批次 |
| 时延链 | ① 告警 `firstSeenAt` → 会话 `createdAt`（目标 60s）② 会话 `createdAt` → `finishedAt`（300s）③ 会话 `finishedAt` → 工单 `createdAt`（300s）④ `firstSeenAt` → 工单 `createdAt`（600s） |
| 统计量 | 每段输出 P50 / P90 / 均值 / 有效样本数 / 达标率（最近秩法取分位，分母为 0 时返回 0 而非 NaN） |
| 自动化率 | 自动开排障率（有 session / 告警总数）、自动建单率（有工单 / 有 session）、巡检自动化率（`trigger=cron` 批次 / 全部批次）、定位成功率（`root_caused` 或已有根因 / 会话总数） |
| 数据一致性 | 建单关系按 `ticket.sourceID = session.id` 关联，不依赖 `alarm.ticketId` 的回填时机；接口调用前做一次幂等 `SyncFromSessions` |
| 看板 | `/metrics` 从占位替换为 `pages/Metrics.tsx`：4 张考核卡 + 时延分布表 + 自动化率进度条 + 每日趋势柱图 + 口径说明 |
| 准确率口径 | 现场考核的「发现/定位准确率」需人工回填标注，当前以「定位成功率」替代并在看板明示，避免把无标注数据包装成准确率 |

#### 验证结果（Mock 模式，近 7 天）

- 告警 15 条（`prometheus 6 / degradation 9`）、排障会话 24 个、工单 26 张、巡检批次 3 个
- `alertToSession` P90 ≈ 2397s / 达标率 16.7%（历史数据早于自动排障上线，属预期）
- `sessionToRootCause` P90 ≈ 0.4s、`rootCauseToTicket` P90 ≈ 8.2s，达标率 100%
- `autoDiagRate 93.3%`、`autoTicketRate 100%`、`inspectCronRate 66.7%`、`diagSuccessRate 100%`
- 前端 `tsc --noEmit` + `vite build` 通过；`http://127.0.0.1:5173/api/metrics/kpi?days=7` 经 dev 代理正常返回

### 9.8 S4 落地记录（2026-11-04）

| 项 | 实现 |
|---|---|
| 双通道采集 | 新增 `internal/alarm/alertmanager.go`：`fetchAlertmanager` 主动 GET `/api/v2/alerts?active=true&silenced=true&inhibited=true&unprocessed=true`；`PollOnce` 先取 `list_alerts`，再与 Alertmanager 结果按 fingerprint 合并 |
| webhook 入站 | `POST /api/alarms/webhook` 兼容 Alertmanager v4 载荷：`firing` 幂等入库并触发自动排障，`resolved` 直接标记恢复；`groupLabels` 排序序列化后写入 `labels.group` |
| 抑制识别 | `status.silencedBy` → `suppressedBy="silenced"`，`status.inhibitedBy` → `"inhibited"`；与平台手动静默（`silencedUntil`）相互独立 |
| 零排障 | `autoDiagnoseEvents` 新增准入：`SuppressedBy != ""` 直接跳过；手动静默逻辑保持不变，人工「强制开排障」仍可用 |
| 去重 | fingerprint 统一为 `alertname\|instance\|severity`，与 `list_alerts` 完全一致；Alertmanager 只补全抑制态与分组，不新增重复行 |
| 恢复安全 | 被抑制告警仍计入本轮 `seen`，避免 miss 检测把「静默」误判为「恢复」；解除静默后自动恢复为可排障状态 |
| 前端 | 状态列新增「上游静默 / 上游抑制」紫色标签；详情抽屉顶部提示；来源标签支持 `alertmanager`；`/api/alarms/stats` 新增 `suppressed` 计数 |
| 配置 | `ALERTMANAGER_ENABLED`（默认 `true`）、`ALERTMANAGER_URL`（默认 `http://127.0.0.1:9093`）、`ALERTMANAGER_TIMEOUT_SEC`（3）、`ALERTMANAGER_GROUP_LABEL`（`group`） |
| 与方案偏差 | 方案原文为「`silenced`/`inhibited` 不入库」，实现改为「入库但标记抑制态、不进自动排障」。原因：完全不入库会让原有 firing 事件被恢复检测误判为已恢复；标记态同时满足「零排障 / 零工单」与「解除后恢复采集」 |

#### 验证结果（本地 Alertmanager Stub + Mock）

- Stub 返回 4 类样本（正常 / 静默 / 抑制 / 与现有 `TargetDown` 同 fp）；`POST /api/alarms/poll` → `upserted=8, autoDiag=0`
- `AMSuppressedTest` / `AMInhibitedTest`：`suppressedBy=silenced/inhibited`，`diagSessionId` 与 `ticketId` 均为空，`labels.group=am-test`
- 同 fp 的 `TargetDown`（172.17.228.214:9100）未新增行，`labels.group=rocketmq-cluster`，证明与 `list_alerts` 不重复
- 将 `AMSuppressedTest` 解除静默后再 poll：`suppressedBy` 清空、`autoDiag=1`，成功创建排障会话
- webhook firing → `{ingested:1, autoDiag:1}`；同 fp resolved → `{resolved:1}`，告警状态转 `resolved`
- `/api/alarms/stats` 返回 `suppressed=1`；`/api/metrics/kpi` 正常，`bySource` 出现 `alertmanager`
- 后端 `gofmt + go build ./...`、前端 `tsc --noEmit + vite build` 通过；浏览器验证 `AMInhibitedTest` 行显示「上游抑制」标签、详情可见 `labels.group`

---

## 十、后续完整开发规划（阶段四 → 阶段八）

> 阶段三（S1–S4）已把「告警运营化」主链路打通，剩余 S5 为外部依赖项。以下按「先闭环、再扩面、后平台化」排序，均复用现有 store / 统一接入网关 / diagnose / tickets / topo / inspect 底座，不新增外部中间件。

### 10.1 阶段四：运营闭环补强（P1，建议下一批）

| 编号 | 任务 | 内容 | 验收 |
|---|---|---|---|
| T8 | 指标劣化独立页面 | `/degradation` 从占位转真实页：展示当前劣化目标、指标曲线、斜率、恢复状态，支持手动触发 `POST /api/alarms/degradation/scan` | 页面可触发扫描并看到劣化明细，与告警中心同源 |
| T9 | 巡检 → 隐患告警联动 | 巡检 `finding` 达到风险阈值时写入 `AlarmEvent{Source:"inspection"}`，复用同一套准入/工单 | 高风险发现自动生成告警/工单，低风险不打扰 |
| T10 | 根因准确率人工回填 | 在排障会话/工单增加「根因是否正确」标注，KPI 增加 `rootCauseAccuracy`（分母为已标注样本） | 有标注时看板显示准确率，无标注显示「待标注」 |
| T11 | 告警通知出站 | 基于现有 webhook 能力增加「出站通知」配置（飞书/企业微信/邮件二选一），按 severity + group 订阅 | 静默/抑制告警不通知，恢复时通知一次 |

#### 阶段四落地记录（2026-11-04）

| 项 | 实现 |
|---|---|
| T8 劣化页面 | `/degradation` 从占位转真实页：扫描概况（目标数 / 劣化数 / 受影响主机 / 自动排障数）、劣化中明细、最近恢复、最近一轮扫描参数；支持手动扫描与 30s 自动刷新 |
| T8 状态持久化 | 新增 kind `degradation_scan`（key `latest`）：`SweepDegradation` 结束即落库，`GET /api/alarms/degradation` 返回 `lastScan` + `firing` + `resolved` + `affectedHosts`；页面刷新不丢结果 |
| T9 巡检联动 | `inspect.Engine` 新增 `SetRiskPublisher` 钩子（避免 inspect → alarm 反向依赖），由 `cmd/server/main.go` 注入；批次结束后把 `Verdict=risk` 的 finding 发布到告警中心 |
| T9 去重与生命周期 | fingerprint 为 `inspection\|taskId\|ruleCode\|objectKey`；同一任务重复巡检命中同一事件，本轮未再命中的旧风险直接置 `resolved`；`PollOnce` 的恢复检测跳过 `source=inspection`，生命周期由巡检批次维护 |
| T9 噪音控制 | RISK 事件复用 `autoDiagnoseEvents` 的四道准入（severity / 冷却 / 同 host 去重 / 单轮限流 5）；P0/P1→critical、P2→warning、其余→info |
| T10 人工回填 | `POST /api/diagnose/sessions/:id/feedback`，字段 `accurate`（必填）/`actualRootCause`/`note`/`operator`，重复标注以最后一次为准；会话模型新增 `feedback` |
| T10 KPI | `kpi.Result` 新增 `accuracy{labeled, accurate, rate, coverage}`；分母为「已标注会话」，`labeled=0` 时 `rate=0` 并在 `note` 中说明，避免误读为 0% 准确率；看板新增「根因准确率（人工标注）」进度条 |
| T11 通知出站 | 通用 HTTP POST JSON（不绑定 IM）：`ALARM_NOTIFY_URL` / `ALARM_NOTIFY_TOKEN` / `ALARM_NOTIFY_TIMEOUT_SEC`；载荷 `{source,type,count,alarms[],at}`；未配置时整条链路零网络调用 |
| T11 去重 | 仅在 `isNew` 场景推送：首次出现、`resolved → firing`、上游解除静默；持续 firing 不重复打扰；单次超过 20 条截断 |
| T11 投递记录 | 新增 kind `alarm_notification`，只保留最近 200 条；`GET /api/alarms/notifications` 返回 `{enabled, items}`，`POST /api/alarms/notifications/test` 走同一条发送链路并落记录 |
| 前端 | 新增 `pages/Degradation.tsx`；告警中心操作条新增「通知记录」抽屉（启用状态、投递列表、HTTP 状态、错误、告警明细、发送测试）；排障工作台会话详情新增「根因准确率回填」卡片 |

#### 验证结果（阶段四）

- T8：`POST /api/alarms/degradation/scan` → `scanned=10, degraded=9, created=9`；随后 `GET /api/alarms/degradation` 返回 `lastScan.at` 与 `affectedHosts=9`，重启后仍可读取
- T9：新建全规则巡检任务并执行 → `risk=7 published=7`；`GET /api/alarms` 出现 7 条 `source=inspection` 且 `state=firing`，其中 1 条在准入内自动创建排障会话
- T10：`POST /api/diagnose/sessions/:id/feedback {accurate:true}` 返回 `feedback.operator=tester`；`GET /api/metrics/kpi?days=7` 返回 `accuracy={labeled:1,accurate:1,rate:1,coverage:0.036}`
- T11：本地 Stub `127.0.0.1:9199` 收到 `Authorization: Bearer test-token`，载荷 `type=alarm, count=7`；`/api/alarms/notifications` 记录 `status=sent, httpStatus=200`；测试通知 `type=test, count=1`
- 前端 `tsc --noEmit + vite build`、后端 `go build ./...` 通过；浏览器验证 `/degradation` 四卡与明细表、告警中心通知抽屉（3 条记录）、排障工作台回填卡片均正常渲染

#### 排障链路联调与缺陷修复（2026-11-04）

| 问题 | 定位 | 修复 |
|---|---|---|
| 同一排障会话重复生成工单 | `tickets.SyncFromSessions` 被 `/api/tickets` 与 `/api/metrics/kpi` 并发触发，且无并发保护 | `tickets.Engine` 增加 `sync.Mutex`，`SyncFromSessions` 入口串行化；并发 8 轮验证同一会话仅 1 张工单 |
| 无拓扑 / Pinpoint 未匹配仍判 `root_caused` 并开 P1 | 根因排序中告警关键字命中即给 0.6，不区分证据链是否完整 | `diagnose.Engine` 新增弱证据判定：`impact.TotalApps==0 && !pinpointOK` 时最高置信度压到 0.5 并追加原因，排序结果输出 `weakEvidence`；此类会话转为 `investigating` + P2 工单 |
| 排障表单选择中间件类型即白屏 | `Diagnosis.tsx` 中 `Form.useWatch('middlewareType') && Form.useWatch('addr')` 条件调用 Hook，Hook 数量随表单值变化 | 三个 `useWatch` 改为无条件调用后再组合布尔值，Hook 顺序稳定 |

联调验证（真实 appId `apollo-ehome-pro` + `ehome-pro.mysql.ehomeinner.cn:13303`）：

- 接口：7 步证据链前 6 步 `ok`、第 7 步日志证据 `skipped`（外部 log_analyzer 配置问题，不阻断）；`status=root_caused, confidence=0.6`（2026-11-03 口径；现行链路已扩为 10–11 步，见下方 M2–M5 复测）
- 浏览器：表单填写 → 提交 → 4 秒完成；会话详情、7 条步骤、根因结论、回填卡片均正常，`pageerror` / `console.error` 为 0
- 弱证据用例 `appId=ehome-web`（快照中不存在）：`status=investigating, confidence=0.5`，工单降为 P2

M2–M5 复测（2026-11-06，mock 四服务）：

- `apollo-ehome-pro` + `ehome-pro.mysql.ehomeinner.cn:13303`：11 条证据链步骤全 `ok`（含验真、CMDB、Pinpoint、基线、热力图、调用链、ES 日志），4 条 `sourceHealth` 记录（promthus ×3 + es ×1），`status=root_caused, confidence=0.75`，问题单 `confirmed/warning`
- Prometheus 拒绝连接：`sourceHealth[promthus]=unavailable`，证据链仍可跑完，置信度封顶 `0.50`、`status=investigating`、问题单 `degraded`
- 未知应用 `no-such-app-xyz`：`status=no_data, confidence=0.09`（无候选主机时不伪造日志证据）
- 外部问题（已处理）：`/tmp/aiops-logs/{host}.log` 在 macOS 上因 `/tmp → /private/tmp` 被日志服务白名单拒绝，改为显式传 `logPath`；`log_analyzer` 的 `'dict' object has no attribute 'api_url'` 已通过对齐 `config/settings.py` 与 `config.json` 结构修复，详见 10.2 节「S5 日志证据落地记录」


### 10.2 阶段五：监控感知扩面（P2）

| 编号 | 任务 | 内容 | 验收 |
|---|---|---|---|
| T12 | 外部业务接口监控 | 用 Prometheus 上游服务采集错误率 / 请求量 / TP99，配置阈值后写入告警表；新增接口健康度看板 | 超阈值自动告警并关联拓扑影响面 |
| T13 | 质差分析 | 复用指标 + 拓扑，识别服务/组件/云资源质差根因，输出质差工单 | 至少 3 类质差场景可演示 |
| T14 | 告警 → Dashboard/拓扑联动 | 拓扑节点按 firing 告警聚合描边，点击钻取到告警中心筛选结果 | 告警与拓扑双向可达 |

#### 阶段五落地记录（2026-11-04）

| 项 | 实现 |
|---|---|
| T12 规则模型 | 新增 kind `biz_rule`：`{id,name,appId,metric(error_rate/tp99/request_total),comparator(gt/lt),threshold,severity,windowMin,enabled}`；`GET /api/alarms/biz/rules` 首次访问自动初始化 3 条演示规则（应用 `apollo-ehome-pro`） |
| T12 采集与判定 | `ScanBiz` 通过上游接口 `query` 拉取瞬时指标（`http_requests_total` / `histogram_quantile` 推导 TP99），按规则的比较符与阈值判定越界；采集走 `Gateway.CallSilent`，不污染 `tool_call_log` |
| T12 告警写入 | 统一通道 `alarm.IngestExternalEvents`：`source=biz`，`fingerprint=biz\|{ruleId}\|{appId}`；越界即 upsert firing，本轮未再越界的旧事件置 `resolved`（`PollOnce` 跳过 `source=biz`，生命周期由扫描维护） |
| T12 扫描留痕 | 新增 kind `biz_scan`（key `latest`）：`{at,rules,breaches,created,items[]}`，`GET /api/alarms/biz` 返回规则/统计/明细/当前告警；`POST /api/alarms/biz/scan` 手动触发，后台按 `BIZ_MON_INTERVAL_SEC`（默认 60s）周期扫描 |
| T12 前端 | `/biz-monitor` 从占位转真实页：4 张统计卡、规则表（新建/删除）、扫描明细（越界标红）、当前告警表，30s 自动刷新 |
| T13 质差分析 | `AnalyzeQuality(appId,windowMin)`：Prometheus `range_query` 取错误率 / TP99 / 请求量，按前 2/3 为基线、后 1/3 为近期做比对，评分权重 `0.35/0.35/0.2/0.1`，`≥0.7` 判 `degraded`、`≥0.4` 判 `suspected` |
| T13 根因归因 | 结合拓扑 `middlewares` 与指标劣化方向给出根因候选（database / cache / mq / gateway / application）+ 目标实例，输出 `metrics[] / rootCause / evidence[]` |
| T13 落库与联动 | kind `quality_report`（每应用保留最近 20 份）与 `quality_scan`（key `latest`）；质差降级写入 `source=quality`、`fingerprint=quality\|{appId}`，复用自动排障/建单 |
| T13 前端 | `/quality` 从占位转真实页：应用 + 窗口筛选、等级/评分卡、3 指标基线对比条（纯 CSS）、根因卡、证据列表、历史报告表 |
| T14 告警 → 拓扑 | 告警中心操作列新增「查看拓扑」，跳 `/op-topology?host=&fp=`；拓扑页 `onLoad` 向 iframe postMessage `{type:'aiops-focus',host,fingerprint}`，G6 侧 `focusByHost` 定位并高亮节点 |
| T14 拓扑 → 告警 | 拓扑节点详情抽屉新增「查看该节点告警」，跳 `/alert-center?host=`；告警中心用 `useSearchParams` 预置关键字并给出可关闭的定位提示 |
| 配置项 | `BIZ_MON_ENABLED`(true) / `BIZ_MON_INTERVAL_SEC`(60) / `QUALITY_ENABLED`(true) / `QUALITY_WINDOW_MIN`(60) |

#### 验证结果（阶段五）

- T12：`POST /api/alarms/biz/scan` → `rules=3, breaches=3, created=3`；连续第二次扫描 `created=0`（指纹幂等）；`GET /api/alarms?state=firing` 可见 `source=biz`
- T13：`POST /api/quality/scan {appId:"apollo-ehome-pro",windowMin:60}` → `level=degraded, score≈1.0`，`rootCause={type:"database", target:"ehome-pro.mysql.ehomeinner.cn:13303"}`，错误率 +800% / TP99 +750% / 请求量 -40%；`/api/alarms` 出现 `source=quality`
- T14：接口与路由参数校验通过（`/alert-center?host=...` ↔ `/op-topology?host=&fp=`）；浏览器复测因本地浏览器插件连接失败未执行，页面由 `tsc --noEmit` + `vite build` 保证可编译
- 前端 `tsc --noEmit`、`npm run build`，后端 `go build ./...` 均通过

#### S5 日志证据落地记录（2026-11-04）

| 项 | 实现 |
|---|---|
| 客户端 | 新增 `internal/diagnose/logevidence.go`：`POST /api/process-from-path` 提交任务、轮询 `GET /api/task/{id}`，从 `reports[].is_combined` 的 Markdown 中解析总行数 / 错误数 / 警告数 / 错误类型 TopN / 高频类 / 错误模式 / 关键错误 |
| 配置 | `LOG_ANALYZER_ENABLED`(true) / `LOG_ANALYZER_URL`(默认 `http://127.0.0.1:8000`) / `LOG_ANALYZER_TIMEOUT_SEC` / `LOG_ANALYZER_WAIT_SEC` / `LOG_PATH_TEMPLATE`；支持 `Request.LogPath` 显式指定文件，缺省按模板用主机推导 |
| 引擎接入 | 排障第 5 步（证据链第 6 条）新增「日志证据（错误 TopN / 异常模式）」；服务未配置、路径未解析、任务失败或超时统一 `skipped` 并以 `error` 说明原因，不阻断排障与置信度计算 |
| 前端 | 排障页新增「日志路径（可选）」输入；会话详情新增日志证据卡（文件数/行数/错误数/警告数、错误类型 Top5、高频类、错误模式、关键问题、任务 ID） |
| 外部服务修复 | `log_analyzer/config/settings.py` 与 `config.json` 结构对齐（`api_url/model_name/backup_model`、`server_path.allowed_directories` 等，未知字段忽略）；补上 `process_files_from_path` 缺失的 `is_cancelled()`，服务恢复可用 |
| 外部服务遗留 | LLM 接口返回 `404`（`api.modelarts-maas.com/openai/v1/chat/completions` 与模型名不匹配），报告仍可回落到规则生成；`report/generator.py` 的 `_create_timeline_section` 对 `ProcessingResult` 误用 `.get`，单文件报告生成报错但不影响综合报告 |

验证结果（S5）：

- 成功路径（真实服务）：`POST /api/diagnose/sessions {appId:"apollo-ehome-pro",logPath:".../test.log"}` → 第 6 条「日志证据」`status=ok`，`lines=10, errors=0, taskId=path_aiops-platform_20261104_132308_894273`，耗时 1.4s
- 解析路径：以日志服务桩验证 Markdown 解析 → `lines=128430, errors=1287, topClasses=[OrderService 356, PayService 218], patterns=[数据库连接池耗尽 356, 下游依赖超时 218], keyIssues=2`
- 降级路径：日志服务返回错误时第 6 条 `status=skipped`，`error=日志证据缺失：日志分析服务错误：路径不存在: /tmp/notfound.log（不阻断排障）`，会话仍为 `root_caused`
- 前端 `tsc --noEmit` 与 `npm run build` 通过

### 10.3 阶段六：执行与治理（P2，含外部依赖）

| 编号 | 任务 | 内容 | 前置 |
|---|---|---|---|
| T15 | 割接执行与验证 | 操作编排、拨测验证清单、执行结果判定与回退 | 需受控执行通道（SSH 跳板 / Ansible） |
| T16 | 关系修正 | 基于 tprel Diff 生成修正建议、执行与留痕，无法修正时告警 | 无 |
| T17 | 历史故障库与知识库 | 会话归档为案例（现象/根因/处置/验证），支持检索与相似推荐 | 无 |

#### 阶段六落地记录（2026-11-04，T16/T17 已落地，T15 仍待外部执行通道）

| 项 | 实现 |
|---|---|
| T16 引擎 | 新增 `internal/relations`：`Scan(from,to,env)` 缺省取 `topo.Dates()` 最后两个快照；`topo.Diff` 的 `added → missing`、`removed → stale`；最近一次稽核 `AUDIT_C3` + `RISK` → `orphan`（`autoFix=false`，仅人工） |
| T16 幂等 | 稳定指纹 `sha1(kind\|appId\|middlewareType\|addr\|depProject)`，ID 由指纹派生；`upsert` 命中已有建议只计 `skipped`，**不覆盖人工审批** |
| T16 状态机 | `pending → approved/rejected → applied`；仅 `approved` 且 `autoFix=true` 可执行，`applied` 不可再审批；重复 `apply` 幂等返回 |
| T16 留痕 | 执行写 kind `relation_override`（`action=add/remove`，带 `proposalId`、快照日期、执行时间），**不直接回写 topo/CMDB** |
| T16 接口 | `POST /api/relations/scan`、`GET /api/relations?status=&kind=`、`GET /api/relations/stats`、`GET /api/relations/overrides`、`PATCH /api/relations/:id`、`POST /api/relations/:id/apply`；CORS 方法补 `PATCH` |
| T16 前端 | `/relations` 从占位转真实页：6 张统计卡、快照日期选择 + 扫描、状态/类型筛选、建议表（类型/对象/来源/说明/修正方式/状态/时间/操作）、审批弹窗（通过/驳回 + 备注 + 孤儿警告）、修正留痕表 |
| T17 引擎 | 新增 `internal/knowledge`：`Reindex()` 遍历 `diagnose.List(0)`，`status=root_caused` 或已有 `Feedback` → 写 kind `knowledge_case`（ID 复用会话 ID，天然幂等）；列表/统计内部 Lazy Reindex |
| T17 检索 | 中文按子串打分：应用命中 +5、应用包含 +3、关键词命中 +2；最多抽 10 个关键词（应用 + 中间件 + 症状/根因片段）；`Similar` 复用同一打分 |
| T17 关联 | 工单只读 `ticket` 表（`sourceType=diagnosis`），**不调用** `tickets.Engine.List()`，避免懒同步副作用 |
| T17 接口 | `GET /api/knowledge?q=&appId=&limit=`、`GET /api/knowledge/stats`、`GET /api/knowledge/similar?appId=&symptom=&limit=`、`GET /api/knowledge/:id`、`POST /api/knowledge/reindex` |
| T17 前端 | `/knowledge` 从占位转真实页：搜索框（现象/根因/应用）、应用筛选、重新归档、4 张统计卡、案例表、Top 应用标签、详情抽屉（描述项 + 检索关键词 + 证据链 List + 「以该对象重新排障」） |

验证结果（阶段六）：

- T16（真实拓扑）：`POST /api/relations/scan` → `added=0, removed=0, orphans=7, created=7`；二次扫描 `created=0, skipped=7`
- T16（构造差异，临时实例）：`added=1, removed=1, created=2`；`missing` 审批通过 → `apply` 成功置 `applied` 并新增 `relation_override`；重复 `apply` 返回 200 且不新增留痕；孤儿 `apply` 返回 400；`applied` 后再 `PATCH` 返回 400
- T17：`stats → total=30, apps=2, reviewed=1, accuracy=100.0`；`q=告警` 命中 29；`similar?appId=apollo-ehome-pro&symptom=告警` 返回打分结果；详情含 7 条证据；`reindex` 幂等
- 前端：`/relations` 与 `/knowledge` 浏览器复测通过（Chrome headless + CDP），截图见临时目录；`tsc --noEmit`、`npm run build`、`go build ./...` 均通过
- 遗留：T15 割接执行与验证仍缺 SSH/Ansible 底座；关系修正写侧仍落 overrides，回写 topo/CMDB、回滚与「无法修正自动告警」待补

### 10.4 阶段七：通用能力平台化（P3）

样本库 → 特征库 → 算法管理 → 模型训练/模型库 → 数据预处理，按「先能登记、再能评估、最后能替换规则」三步走；每步都以「现有规则结果」为基线做 A/B 对比，避免平台化空转。

### 10.5 阶段八：交付化（P2）

鉴权与租户隔离、OpenAPI、审计与权限矩阵、容器化部署与运维手册、演示脚本与验收材料。

### 10.6 里程碑与验收节奏

| 里程碑 | 范围 | 验收口径 |
|---|---|---|
| M1（已完成） | 阶段一 + 阶段二 + 阶段三 S1–S4 | 告警采集 → 自动排障 → 自动建单 → 恢复 → 度量 → 抑制联动全链路可演示 |
| M2 | S5 + 阶段四 | 日志证据真实可用；劣化与巡检发现进入同一告警闭环；准确率可标注 |
| M3 | 阶段五 | 外部接口监控与质差分析形成第二个可演示场景 |
| M4 | 阶段六 + 阶段八 | 治理闭环（T16 关系修正 / T17 知识库 已完成）；执行侧 T15 待外部通道；具备交付与权限能力 |
| M5 | 阶段七 | 通用能力可登记、可评估、可替换规则 |

