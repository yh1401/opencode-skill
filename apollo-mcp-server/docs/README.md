# Apollo MCP Server 部署文档

## 1. 概述

Apollo MCP Server 为 Apollo 配置中心提供标准化的 MCP (Model Context Protocol) 工具调用接口，供 StarAgent 平台上的 Skill 进行调用。

**查询链路**：哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)

- 支持**多套 Apollo 服务实例**（各产品线/地域独立部署），通过 `apolloHostId` 指定任意一套
- 配置/应用查询统一走 **EasyOps 代理接口**（无需本地保存 Apollo Token，按 `apolloHostId` 指定任意一套）；调用不通时直接返回错误提示
- 提供 4 个工具：`apollo_host_list` / `apollo_app_list` / `apollo_config_query` / `apollo_config_global_search`（跨套跨应用全局搜索）

**版本**：v3.2.1
**协议**：MCP Streamable HTTP (JSON-RPC 2.0)
**端口**：8062

---

## 2. 部署方式

### 方式一：Docker Compose 部署（推荐）

> `docker-compose.yml` 与 `.env` 均位于 `deploy/` 目录下，以下命令需在 `deploy/` 下执行。

#### 生产环境

```bash
# 克隆或上传代码到服务器
cd apollo-mcp-server

# 进入部署目录（docker-compose.yml 与 .env 均位于此）
cd deploy

# 创建 .env 配置文件（生产环境必须关闭 Mock）
# 默认通过第三方「Apollo Host 信息查询接口」获取各套 Apollo 地址与 Token，无需填写
cat > .env << 'EOF'
MCP_USE_MOCK=false
LOG_LEVEL=INFO
EOF
chmod 600 .env

# 如需指定默认 Apollo 套，追加 APOLLO_HOST_NAME（模糊匹配，如"天翼云眼贵州"）
# 如需覆盖第三方接口地址/sessionId，追加 APOLLO_HOST_API_BASE / APOLLO_HOST_SESSION_ID

# 构建并启动
docker compose up -d

# 查看日志
docker compose logs -f apollo-mcp

# 健康检查
curl http://localhost:8062/health
```

#### Mock 模式（开发/测试）

```bash
# 在 deploy/ 目录下启动 Mock 模式容器（端口 8063）
docker compose --profile mock up -d

# 健康检查
curl http://localhost:8063/health
```

### 方式二：Docker 命令部署

```bash
# 在项目根目录（apollo-mcp-server）构建镜像，Dockerfile 位于 deploy/ 下
docker build -t apollo-mcp:latest -f deploy/Dockerfile .

# 生产模式运行（Dockerfile 默认 Mock 模式，必须显式关闭）
# 默认通过第三方接口获取 Apollo 地址/Token，无需配置 Token
docker run -d \
  --name apollo-mcp \
  -p 8062:8062 \
  -e MCP_USE_MOCK=false \
  -v $(pwd)/config:/app/config:ro \
  -v $(pwd)/mock:/app/mock:ro \
  -v $(pwd)/logs:/app/logs \
  apollo-mcp:latest

# Mock 模式运行
docker run -d \
  --name apollo-mcp-mock \
  -p 8063:8062 \
  -e MCP_USE_MOCK=true \
  apollo-mcp:latest
```

### 方式三：Python 直接运行

```bash
# 在项目根目录（apollo-mcp-server）下执行
# 安装依赖
pip install -r requirements.txt

# Mock 模式
MCP_USE_MOCK=true python3 scripts/mcp_server.py --port 8062

# 生产模式（默认通过第三方接口获取地址/Token，需可访问 easyops）
python3 scripts/mcp_server.py --port 8062
```

---

## 3. 环境变量说明

### 3.1 基础配置

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `MCP_USE_MOCK` | 否 | `false` | Mock 模式（无需 Apollo 服务） |
| `APOLLO_ENV` | 否 | 配置文件 `default_env` | 环境标识（用于日志与展示） |
| `APOLLO_HOST_API_BASE` | 否 | `https://easyops.tech.ctseelink.cn` | 第三方「Apollo Host 信息查询接口」地址 |
| `APOLLO_HOST_SESSION_ID` | 否 | 内置默认值 | 第三方鉴权 sessionId（32 位），同时是 token 解密密钥 |
| `APOLLO_HOST_NAME` | 否 | - | 默认 Apollo 套的模糊过滤名称（多套中指定一套，不填取第一条） |
| `CONFIG_SERVICE_REFRESH_SEC` | 否 | `60` | 第三方接口配置刷新间隔（秒） |
| `LOG_LEVEL` | 否 | `INFO` | 日志级别：DEBUG/INFO/WARNING/ERROR |
| `LOG_TO_FILE` | 否 | `false` | 文件日志开关：false 时不写日志文件，仅控制台输出（Docker json-file 自动轮转） |

### 3.2 超时与重试（影响 agent 响应速度）

> 正常请求毫秒级返回不受影响；以下仅在异常/超时时触发，快速失败避免 agent 卡死。

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `APOLLO_REQUEST_TIMEOUT` | `6` | 业务查询单次超时（秒）：`apollo_config_query`，6s 不重试快速失败 |
| `APOLLO_APPS_TIMEOUT` | `6` | 应用列表查询单次超时（秒）：`apollo_app_list`，6s 不重试快速失败 |
| `APOLLO_HOST_LIST_TIMEOUT` | `8` | Apollo Host 列表单次超时（秒）：`apollo_host_list`，8s + 1 次重试 |
| `APOLLO_MAX_RETRIES` | `1` | 单请求失败重试次数（0=不重试） |
| `APOLLO_RETRY_DELAY` | `1` | 重试间隔（秒） |
| `APOLLO_TRAVERSAL_BUDGET` | `20` | 遍历可用套总预算（秒）：超过则提前终止，避免多套全超时累加 |

### 3.3 Host 探活

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `APOLLO_HOST_PROBE_ENABLED` | `true` | 是否开启 Host 探活（关闭后 host_list 不探活，遍历回退原全量逻辑） |
| `APOLLO_HOST_PROBE_TTL` | `60` | 探活结果缓存秒数（60s 内复用，避免每次 host_list 重复探活） |

### 3.4 全局配置搜索（`apollo_config_global_search`）

> 控制跨套跨应用全局搜索的并发度、预算、缓存。默认值：8 并发 / 60s 预算（看门狗 +约15s 宽限）/ 5 个常见 namespace / apps 缓存 5min / config 缓存 2min。

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `APOLLO_GLOBAL_SEARCH_WORKERS` | `8` | 全局扫描并发线程数：**应用级并行**（任务下沉到每个 app，由共享线程池消化），不限套数 |
| `APOLLO_GLOBAL_SEARCH_BUDGET` | `60` | 全局搜索总预算（秒）：覆盖 host_list + probe + app 列表 + 扫描全阶段。整次搜索放在独立线程中执行，外层**硬性看门狗**最多再等约 15s 宽限（即最长 ~75s）强制返回 partial，保证调用不会被平台超时掐断（DNS/host 列表挂起等极端情况也不会卡死）。也可通过入参 `scanBudget` 覆盖为小值（如 15s）做快速摸底 |
| `APOLLO_GLOBAL_SEARCH_MAX_NS` | `5` | 每个应用扫描的 namespace 上限（namespaceName=application 时自动扩展：application → datasource → redis → common → logging） |
| `APOLLO_APPS_CACHE_TTL` | `300` | apps 列表缓存秒数（同套多 app 扫描时复用，避免重复拉取） |
| `APOLLO_CONFIG_CACHE_TTL` | `120` | config 结果缓存秒数（同 (host, app, env, ns) 扫描时复用） |

> **调优建议**：并发线程把每个 app 的 namespace 拉取请求打到代理上，EasyOps 代理对高并发敏感时不要盲目调大 `WORKERS`（可试 4-6）；`APOLLO_HOST_PROBE_ENABLED` 关闭后走全量遍历；请求链路过宽时优先用入参 `apolloHostIds`（按套收窄）+ `namespaces`（只扫指定 ns，如只扫 application 时请求量约为扩展模式的 1/5）+ `scanBudget`（分阶段多次扫）来控制单次调用规模。

---

## 4. StarAgent 平台注册

### 步骤 1：访问 StarAgent 平台

打开 StarAgent 平台 → 进入 **MCP 管理** → 点击 **新建 MCP 服务**

### 步骤 2：填写服务信息

| 字段 | 值 |
|------|-----|
| **服务名称** | `apollo-config-query-mcp` |
| **服务描述** | Apollo 配置查询 MCP 服务 |
| **服务地址** | `http://180.101.21.13:8080/gateway/apollo/mcp` |
| **传输协议** | Streamable HTTP |
| **认证方式** | 无（内网部署） / 可选 API Key |

### 步骤 3：验证连接

点击 **测试连接**，平台会发送 `initialize` 请求到 MCP 端点。

**成功响应示例**：
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "name": "apollo-config-query-mcp",
    "version": "3.2.1",
    "capabilities": {
      "tools/list": {},
      "tools/call": {}
    }
  }
}
```

### 步骤 4：验证工具列表

点击 **获取工具列表**，平台会调用 `tools/list`。

**成功响应**：4 个工具
- `apollo_host_list` - 实时查询可用的 Apollo 服务列表（查询链路第一步，返回 `apolloHostId`）
- `apollo_app_list` - 获取应用列表（`apolloHostId` 可选）
- `apollo_config_query` - 查询 Apollo 配置项（`apolloHostId` + env + appId + cluster + namespace + key）
- `apollo_config_global_search` - 跨套跨应用全局搜索（按 `keyword` 匹配 key/value，并行扫描所有可用套的所有应用）

### 步骤 5：绑定到 Skill

1. 进入 **Skill 管理** → 找到 `apollo-config-query` 技能
2. 编辑技能配置 → **绑定 MCP 服务**
3. 选择刚才注册的 `apollo-config-query-mcp`
4. 保存

### 步骤 6：测试调用

在 Skill 测试面板中输入：
- `"有哪些 Apollo 服务实例"` → 触发 `apollo_host_list`（确定哪套 Apollo）
- `"查一下 user-service 的配置"` → 触发 `apollo_config_query`
- `"当前有哪些应用"` → 触发 `apollo_app_list`
- `"查一下贵州那套 Apollo 上 user-service 的配置"` → `apollo_host_list` 取 apolloHostId → `apollo_config_query`（完整链路）
- `"哪些应用的配置用到了 vcpinner.cn:13302"` → 触发 `apollo_config_global_search`（范围大时建议先 `scanBudget=15` 摸底再分阶段补扫）

---

## 5. MCP 工具 Schema

### Tool: `apollo_host_list`

实时查询所有可用的 Apollo 服务列表（查询链路第一步，用于确定哪套 Apollo），并**并发探活标记每套可用性**。

**参数**：无

**返回要点**：每条记录含 `apolloHostId`（即第三方记录 `id`）、`name`、`host`、`has_token`、`secondProductId`、`user`、`is_default`、`available`（true=该套代理接口可达可查询，false=超时/空响应）、`appCount`（探活时获取的应用数）、`availableDetail`（不可用时的错误说明）。探活结果缓存 60s（`APOLLO_HOST_PROBE_TTL`），后续 `apollo_app_list`/`apollo_config_query` 遍历时自动跳过 `available=false` 的套、优先走可用套。

### Tool: `apollo_config_query`

查询指定应用的 Namespace 配置项列表。查询链路：哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)。

**参数**：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| apolloHostId | integer | 否 | 默认 Apollo | Apollo 服务 ID，来自 `apollo_host_list`，指定查询哪一套 |
| appId | string | 是 | - | Apollo 应用 ID |
| env | string | 否 | `PRO` | 环境：PRO/DEV/SIT/FAT/UAT（以目标 Apollo 实际环境名为准） |
| clusterName | string | 否 | `default` | 集群名称 |
| namespaceName | string | 否 | `application` | Namespace 名称 |
| key | string | 否 | - | 配置项 Key（模糊匹配） |

**调用路径**：EasyOps 代理接口 `/thirdApi/apollo/namespace`（仅需 Cookie `sessionId`），调用不通时返回错误提示。

**Fallback 机制**：指定 `apolloHostId` 的套 EasyOps 返回 5xx/超时/`ApolloRequestError` 时，自动 fallback 到第一个探活可用的套，返回结果带 `data.fallback=true` + `fallbackTip` 标注切换信息。404（应用不存在/无此环境）和业务错误不触发 fallback（说明套是好的，只是没这个 app）。

**错误返回结构**（`_build_proxy_error` 统一构建）：

```json
{
  "code": 504,
  "message": "配置查询超时：Apollo 代理接口在 6 秒内未响应",
  "errorType": "timeout",
  "retryable": false,
  "suggestion": "配置查询已快速失败，无需重试..."
}
```

- `errorType=timeout`：代理超时（code=504），建议缩小范围/换套/稍后
- `errorType=unavailable`：代理不可用（code=503），告知用户稍后再试
- `retryable=false`：agent 平台不应盲目重试

### Tool: `apollo_app_list`

获取指定 Apollo 服务中的所有应用列表（AppId、应用名称、所属部门）。

**参数**：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| apolloHostId | integer | 否 | 默认 Apollo | Apollo 服务 ID，指定查询哪一套 |

**调用路径**：EasyOps 代理接口 `/thirdApi/apollo/apps`（仅需 Cookie `sessionId`），调用不通时返回错误提示。

**Fallback 机制**：指定 `apolloHostId` 的套 EasyOps 返回 5xx/超时/连接失败时，自动 fallback 到第一个探活可用的套，返回结果带 `data.fallback=true` + `fallbackTip` 标注切换信息。

### Tool: `apollo_config_global_search` （跨套跨应用全局搜索）

**用途**：解决"哪些应用的配置用到了某个地址/关键词"的高频排障需求。单次调用并行扫描指定/全部可用 Apollo 套的所有应用——无需 agent 逐个应用调 `apollo_config_query`（700+ 应用原来小时级，现在 30-60 秒，收窄范围后 10-20 秒）。

**参数**：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| keyword | string | 是 | - | 搜索关键词，如 `vcpinner.cn:13302`、`password`、`redis.url`、`timeout` |
| searchField | string | 否 | `both` | 匹配字段：`key`（只匹配配置项名）/ `value`（只匹配值）/ `both`（都匹配） |
| env | string | 否 | `PRO` | 环境：PRO/DEV/SIT/FAT/UAT |
| maxResults | integer | 否 | `100` | 最多返回多少条匹配，避免结果过长 |
| namespaceName | string | 否 | `application` | 搜索哪个 namespace；设为 `application` 时**自动扩展扫描** `datasource`/`redis`/`common`/`logging`（最多 `APOLLO_GLOBAL_SEARCH_MAX_NS` 个） |
| apolloHostIds | array\<integer\> | 否 | 全部可用套 | 只扫描指定的 Apollo 套 ID 列表（如 `[3, 14]` 只扫天翼云眼两套）；传了不存在的 ID 返回 `code=404` + 可用套列表。缩小范围可显著减少扫描量 |
| scanBudget | integer | 否 | 服务端 60s | 本次扫描预算（秒）：传小值（如 10-20s）做**快速摸底/分批扫**，预算到点即返回 `partial=true` + `unfinishedApps`（已扫过的部分有 120s 缓存，补扫只花未完成部分的时间），用于分阶段搜索避免单次跑满 |
| namespaces | array\<string\> | 否 | - | 只扫描指定 namespace 列表（如 `["application"]` 或 `["application","redis"]`），用于收窄链路减少请求量；不传时按 namespaceName 默认行为（application 自动扩展常见 ns） |

**调用路径**：内部并行复用 EasyOps 三个代理接口（`getApolloHostInfo` + `apollo/apps` × N 套 + `apollo/namespace` × N 应用 × M namespace），**任务下沉到每个 app** 由 `ThreadPoolExecutor(max_workers=APOLLO_GLOBAL_SEARCH_WORKERS)` 共享线程池消化。namespace 拉取失败**不入缓存**，对瞬时错误（429/5xx、网络异常）自动重试 1 次后再失败才计入诊断统计。

**返回结构**：

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "keyword": "vcpinner.cn:13302",
    "searchField": "value",
    "env": "PRO",
    "namespace": "application",
    "status": "complete",
    "summary": {
      "hostCount": 2,
      "appCount": 151,
      "scannedApps": 151,
      "nsErrorCount": 0,
      "matchedCount": 12,
      "maskedCount": 0,
      "elapsedMs": 42350
    },
    "hostsDetail": [
      {
        "apolloHostId": 3,
        "hostName": "天翼云眼Apollo",
        "probeAvailable": true,
        "appListStatus": "ok",
        "appListCount": 151,
        "tasksSubmitted": 151,
        "scannedNamespaces": 151,
        "nsErrors": 0,
        "errorSample": null,
        "status": "ok"
      }
    ],
    "matches": [
      {
        "apolloHostId": 15,
        "hostName": "天翼云眼Apollo",
        "appId": "user-service",
        "appName": "用户服务",
        "namespaceName": "datasource",
        "key": "spring.datasource.url",
        "value": "jdbc:mysql://vcpinner.cn:13302/user_db",
        "masked": false
      }
    ],
    "partial": false,
    "truncated": false
  }
}
```

**关键字段**：
- `data.status` — 本次扫描状态，四种取值：
  - `complete`：预算内扫完，结果完整；
  - `partial`：超预算/超时提前终止（附 `unfinishedApps` 未完成项），建议缩小范围补扫；
  - `scan_failed`：应用已获取（appCount>0）但 namespace 拉取全部失败（`summary.nsErrorCount>0`），此时 `message` 带首个失败样例，疑似 env 与目标套不对应或代理接口异常，**不是"确无匹配"**，勿直接判工具不可用；
  - `empty_scan`：应用已获取但所有 namespace 内容为空（无错误），疑似 env 不匹配或配置在其它 namespace（可传 `namespaces` 指定）。
- `data.hostsDetail` — 每套诊断统计（探活结果/应用列表条数与状态/实际提交扫描数/扫到内容的 ns 数/失败数与首条失败样例），用于快速定位是哪套在扫不动
- `summary.scannedApps` — 实际扫到内容的 namespace 数（用于判断是否扫完整）；`summary.nsErrorCount` — 各 namespace 拉取失败总次数（重试后仍失败）
- `summary.maskedCount` — 敏感字段脱敏条数（前2后2，如 `SuperSecretPwd` → `Su****wd`）
- `partial=true` — 超预算提前终止，结果可能不全（附 `unfinishedApps`）
- `truncated=true` — 匹配数超过 `maxResults`，结果被截断

**预算控制与容错**：预算覆盖 host_list + probe + app 列表 + 扫描全阶段，**任务下沉到每个 app** 由共享线程池并行；整次搜索置于独立线程，外层**硬性看门狗**（预算 + 约 15s 宽限，默认最长 ~75s）兜底，DNS/单请求挂起也会强制返回 `partial` 而非卡死。单次 namespace 拉取失败不入缓存，瞬时错误（429/5xx/网络异常）自动重试 1 次。扫出 0 条时返回 `scan_failed` / `empty_scan` 显式诊断（带 `hostsDetail.errorSample`），不再静默返回 `success`。

**分阶段使用建议**（大范围搜索按此收敛，避免单次全量）：
1. 摸底：`scanBudget=10~20` + `apolloHostIds` + `namespaces=["application"]`，先拿 partial 结果与 `hostsDetail`；
2. 精查：对命中的 app 用 `apollo_config_query` 查具体 namespace/key；
3. 收尾：确需"无遗漏"时，去掉 namespaces/加大 scanBudget 补扫（已扫过的 ns 有 120s 缓存，补扫只花增量时间）；
4. 多次调用聚合时按 `apolloHostId + appId + namespaceName + key` 去重。

---

## 6. API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/` | GET | 服务信息 |
| `/mcp` | POST | MCP 协议端点（JSON-RPC 2.0） |
| `/tools/list` | GET | 获取工具列表（平台兼容） |
| `/tools/call` | POST | 调用工具（平台兼容） |
| `/api/config` | POST | 配置查询（REST 兼容） |
| `/api/apps` | GET | 应用列表（REST 兼容） |
| `/log/status` | GET | 查询日志开关状态 |
| `/log/off` | POST | 关闭日志（立即生效，完全静默） |
| `/log/on` | POST | 开启日志（立即生效） |
| `/docs` | GET | Swagger API 文档 |

---

## 7. 故障排查

### 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 健康检查失败 | 容器未启动 | `docker compose up -d` |
| 连接超时 | 网络/防火墙 | 检查端口 8062 是否开放 |
| 代理接口返回 404 | apolloHostId 错误或 Token 未授权 | 先用 `apollo_host_list` 确认 apolloHostId；检查 Apollo 开放平台授权 |
| 代理接口返回 401 | EasyOps 保存的 Apollo Token 无效 | 检查第三方接口中该套 Apollo 的 Token |
| 代理接口提示"未记录的访问" | 调用方未配置第三方接口权限 | 在 `t_ops_middleware_third_party_access` 配置 `/thirdApi/apollo/apps`、`/thirdApi/apollo/namespace` 两条 GET 权限 |
| 代理接口不可达 | 第三方接口网络/权限问题 | 检查 `APOLLO_HOST_API_BASE` / `APOLLO_HOST_SESSION_ID` 及出口网络 |
| 返回空数据 | appId 或 namespaceName 错误 | 先用 `apollo_app_list` 验证 |
| Mock 查询失败 | Mock 数据文件格式错误 | 检查 `mock/mock_responses.json` |
| **`code=504` / `errorType=timeout`** | EasyOps 代理单次请求超时（6s/8s 内未响应） | **不要重试**：建议缩小 apolloHostId 范围、指定 key 过滤、或稍后 1-2 分钟再试 |
| **`code=503` / `errorType=unavailable`** | EasyOps 代理整体不可用 | **不要重试**：告知用户 Apollo 配置查询代理暂时异常 |
| **`data.fallback=true`** | 指定套 EasyOps 返回 5xx，已自动切到可用套 | 正常展示结果 + 标注切换，无需处理（若原套必须可稍后重试） |
| **`data.partial=true`**（全局搜索） | 60s 预算超时，提前终止扫描 | 展示已有匹配 + 建议缩小 apolloHostId/namespaceName 范围重试 |
| **全局搜索一直慢** | host 数 > 10 或 EasyOps 延迟高 | 调高 `APOLLO_GLOBAL_SEARCH_WORKERS`（10-12）和 `APOLLO_GLOBAL_SEARCH_BUDGET`（90s） |

### 日志

> 以下命令需在 `deploy/` 目录下执行（docker-compose.yml 位于此处）。

```bash
# 进入部署目录
cd deploy

# Docker 日志
docker compose logs -f apollo-mcp

# 查看特定时间的日志
docker compose logs --since="1h" apollo-mcp

# 查看日志文件
tail -f logs/apollo-mcp.log
```

---

## 8. 配置 Apollo 接入

### 方式一（推荐）：第三方「Apollo Host 信息查询接口」

默认值已内置在代码中，**无需任何配置**。可通过环境变量覆盖：

- `APOLLO_HOST_API_BASE`：第三方接口地址
- `APOLLO_HOST_SESSION_ID`：第三方鉴权 sessionId（同时是 token 解密密钥）
- `APOLLO_HOST_NAME`：指定默认 Apollo 套（模糊匹配，不填取第一条）

> 配置/应用查询均通过 EasyOps 代理接口转发，无需向 Apollo 开放平台申请 Token；代理接口调用不通时，MCP 直接返回错误提示。

---

## 9. 生产环境检查清单

- [ ] 服务器可访问第三方接口（`easyops.tech.ctseelink.cn`）
- [ ] 已确认默认 Apollo 套（`APOLLO_HOST_NAME` 过滤或第一条）
- [ ] 服务器端口 8062 已开放
- [ ] Docker / Docker Compose 已安装
- [ ] 日志目录有写权限
- [ ] 健康检查接口可访问
- [ ] StarAgent MCP 注册完成
- [ ] Skill 绑定 MCP 服务完成
- [ ] 端到端调用测试通过（`apollo_host_list` → `apollo_config_query`）
- [ ] 全局搜索测试通过（`apollo_config_global_search`，30-60 秒返回）
- [ ] host 数 > 10 时已调高 `APOLLO_GLOBAL_SEARCH_WORKERS`（建议 10-12）
- [ ] EasyOps 延迟高时已调高 `APOLLO_GLOBAL_SEARCH_BUDGET`（建议 90s）

---

## 10. 调用过程链

### 10.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────────┐
│ 调用方（StarAgent 平台 / Skill / curl）                                    │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │ JSON-RPC 2.0 / REST
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ FastAPI HTTP 服务层（uvicorn, 端口 8062）                                 │
│  /mcp │ /tools/call │ /api/config │ /api/apps │ /health │ /log/*        │
└──────────────────────────────┬──────────────────────────────────────────┘
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ MCP 协议层 MCPProtocolHandler                                            │
│  handle_message → _execute_tool（工具分发）                                │
└──────────────────────────────┬──────────────────────────────────────────┘
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ 业务层 ApolloAPIClient                                                   │
│  query_hosts │ query_config │ query_apps │ query_global_search           │
└──────────────────────────────┬──────────────────────────────────────────┘
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ 数据源层 ConfigClient（日志最详细的一层）                                   │
│  fetch_host_list │ get_default_host_id │ _proxy_get │ fetch_global_search │
│  _probe_hosts_concurrent │ _cached_fetch_apps │ _cached_fetch_namespace   │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │ HTTPS + Cookie sessionId
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ EasyOps 代理接口（easyops.tech.ctseelink.cn）                              │
│  /thirdApi/getApolloHostInfo │ /thirdApi/apollo/apps │ /thirdApi/apollo/namespace │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │ 由 EasyOps 统一转发（内部）
                               ▼
                        ┌────────────────┐
                        │   Apollo OpenAPI │
                        │ （多套，按 apolloHostId 选择）│
                        └────────────────┘
```

**关键设计**：调用方只连 EasyOps 代理，**不直连 Apollo**；Apollo 地址和 Token 由 EasyOps 统一保存，解决了多套 Apollo 白名单重复申请的问题。

### 10.2 入口层（3 种入口，汇入同一业务层）

| 入口 | 端点 | 代码（scripts/mcp_server.py） | 用途 |
|---|---|---|---|
| MCP 标准 | `POST /mcp` | `mcp_endpoint` | StarAgent 平台主用，JSON-RPC 2.0 |
| 平台兼容 | `GET /tools/list`、`POST /tools/call` | `platform_tools_call` | 兼容非标准 MCP 客户端 |
| REST 兼容 | `POST /api/config`、`GET /api/apps` | `http_config_query` / `http_apps_list` | 简单 HTTP 调试 |

每次 HTTP 请求进来，FastAPI 先把请求体交给 `handle_message`（/mcp）或直接 `_execute_tool`（/tools/call、/api/config），并打印 `[HTTP]` 层出入参数日志。4 个工具（`apollo_host_list` / `apollo_app_list` / `apollo_config_query` / `apollo_config_global_search`）均可通过任一入口调用。

### 10.3 MCP 协议层

`handle_message` 负责 JSON-RPC 消息路由：

1. 校验 `jsonrpc == "2.0"`，否则返回 `-32600 Invalid Request`
2. 按 `method` 分发：
   - `initialize` → 返回协议版本、能力、服务端信息（握手）
   - `notifications/initialized` → 无响应（通知类）
   - `tools/list` → 返回 `MCP_TOOLS`（4 个工具 Schema）
   - `tools/call` → 解析 `params.name` + `params.arguments`，交给 `_execute_tool`
   - 其它 → `-32601 Method not found`
3. 工具结果包装成 MCP 标准格式：`{"content": [{"type": "text", "text": json.dumps(result)}], "isError": false}`
4. 校验失败（如 appId 缺失）→ `-32602`；执行异常 → `-32603`

### 10.4 工具分发（_execute_tool）

| 工具 | 业务方法 | 参数 |
|---|---|---|
| `apollo_host_list` | `query_hosts()` | 无 |
| `apollo_config_query` | `query_config(appId, env, clusterName, namespaceName, key, apolloHostId)` | appId 必填 |
| `apollo_app_list` | `query_apps(apolloHostId)` | 可选 |
| `apollo_config_global_search` | `query_global_search(keyword, searchField, env, maxResults, namespaceName, apolloHostIds, scanBudget, namespaces)` | keyword 必填；返回 `status`（complete/partial/scan_failed/empty_scan）+ `hostsDetail` 诊断 |

`apolloHostId` 统一经 `_to_int()` 安全转换（兼容字符串形式）。

### 10.5 业务层（4 个工具的具体路径）

**apollo_host_list → query_hosts**（实时查询不缓存，每次调用都打第三方接口，是查询链路第一步）：

```
query_hosts → fetch_host_list() → _fetch_host_list_raw()
→ GET {api_base}/thirdApi/getApolloHostInfo（Cookie: sessionId）
→ 返回全部套 Apollo 记录（id/name/host/has_token/is_default）
```

**apollo_config_query → query_config**（最核心，查询链路：哪套Apollo → 环境 → 应用 → 集群 → Namespace → 配置项）：

```
query_config(apolloHostId?, env=PRO, appId, cluster=default, namespace=application, key?)
   │
   ├─ apolloHostId 缺省 → get_default_host_id()（未缓存时先请求 getApolloHostInfo 取第一条）
   │
   ├─ fetch_proxy_namespace(hostId, env, appId, cluster, namespace)
   │     └─ _proxy_get() → GET {api_base}/thirdApi/apollo/namespace?apolloHostId=&env=&appId=&clusterName=&namespaceName=
   │           └─ EasyOps 后端 → 目标 Apollo OpenAPI → 返回 {"code":200,"data":{"items":[{key,value},...]}}
   │
   ├─ 成功（code=200/success）→ _normalize_config_result()
   │     └─ items 数组 → configurations 字典；key 模糊过滤 + _total_count/_filtered_count 统计
   │
   └─ 失败：
         ├─ 指定套 5xx/超时/ApolloRequestError → 自动 fallback 到第一个探活可用的套
         │     └─ 返回结果带 data.fallback=true + fallbackTip
         ├─ HTTP 异常 → _build_proxy_error() 返回 code=504 + errorType=timeout + retryable=false
         └─ 业务错误（404 未授权/无此环境）→ 原样透传（不触发 fallback）
```

**apollo_app_list → query_apps**：

```
query_apps(apolloHostId?) → fetch_proxy_apps(hostId) → _proxy_get()
→ GET {api_base}/thirdApi/apollo/apps?apolloHostId=
→ 返回该套 Apollo 全部应用（appId/name/orgName）
└─ 指定套 5xx/超时 → 自动 fallback 到第一个探活可用的套（与 config_query 对齐）
```

**apollo_config_global_search → query_global_search**（跨套跨应用全局搜索，单次调用并行扫描）：

```
query_global_search(keyword, searchField=both, env=PRO, maxResults=100, namespaceName=application,
                    apolloHostIds=None, scanBudget=None, namespaces=None)
   │
   ├─ fetch_global_search（对外入口）：整次搜索放入独立 daemon 线程，外层"硬性看门狗"
   │     └─ 最多等 预算(默认60s，可被 scanBudget 覆盖) + 约15s 宽限 → 卡死/挂起也强制返回 partial
   │
   ├─ _global_search_impl：start = time.time()（预算覆盖 host_list + probe + app 列表 + 扫描全阶段）
   │     ├─ fetch_host_list_raw() + _probe_hosts_concurrent()
   │     │     └─ 取可用套；指定 apolloHostIds 时只处理目标套（不存在返回 code=404+可用列表）
   │     ├─ 阶段1 并行拉各套应用列表（_cached_fetch_apps，缓存 5min），按套记录 appList 状态
   │     ├─ 阶段2 构建应用级任务 app_tasks（去重 appId）
   │     ├─ 阶段3 ThreadPoolExecutor(max_workers=APOLLO_GLOBAL_SEARCH_WORKERS=8) 并行扫每个 app：
   │     │     └─ _scan_app: ns_list = namespaces 或默认 application+扩展（≤ MAX_NS）
   │     │           ├─ _cached_fetch_namespace(host, app, env, ns)  ← config 缓存 2min，失败不入缓存
   │     │           ├─     瞬时错误(429/5xx/网络异常)自动重试 1 次
   │     │           └─ 匹配 keyword → 追加 matches（含 host/app/ns/key/value，敏感字段自动脱敏）
   │     └─ wait(timeout=剩余预算) → 预算到点立即返回已完成结果，pending 标记 unfinishedApps
   │
   └─ 汇总：pool.shutdown(wait=False, cancel_futures=True)；按套聚合 hostsDetail（appList 状态/
       任务数/扫到的 ns 数/失败数与样例）；scannedApps=0 且确有应用时返回 scan_failed/empty_scan
       显式诊断（不再静默 success）
```

**Mock 分支**：每个工具开头都有 `if USE_MOCK:` 分支（`_get_mock_response` / `_mock_global_search`），Mock 模式下直接读 `mock/mock_responses.json`，不请求任何外部接口——这就是日志里"Mock 模式启用"的来源。

### 10.6 数据源层（ConfigClient，日志最详细）

`_proxy_get` 是代理请求的统一出口，所有出入参数日志都在这层：

```
[ConfigClient] 代理接口请求: url=..., params={...}, sessionId=e5e27a7d...(脱敏)
[ConfigClient] 代理接口响应: status=503, 耗时=3ms, url=..., body={...}
[ConfigClient] 代理接口返回异常状态码: status=503, body="..."   ← ERROR 级
```

要点：

- 鉴权靠 **Cookie: sessionId**（第三方接口统一保存各套 Apollo 地址与 Token）
- `timeout=APOLLO_REQUEST_TIMEOUT/APOLLO_APPS_TIMEOUT`（6s），host_list 用 `APOLLO_HOST_LIST_TIMEOUT=8s`，0 重试快速失败
- 成功/失败都打印**响应体**（排查 503 的关键——能看到 EasyOps 返回的 `{"code":503,"message":"无法连接Apollo服务"}`）

### 10.7 一次完整请求的旅程（以 apollo_config_query 为例）

```
① Skill 发起 → POST /mcp
② [HTTP] /mcp 收到请求: {"method":"tools/call",...}
③ [MCP] 收到 tools/call: tool=apollo_config_query, arguments={appId...}
④ [MCP] 执行工具: tool=apollo_config_query, arguments={...}
⑤ [apollo-config-query] 查询配置: apolloHostId=15, env=PRO, appId=vcp-openapi...
⑥ [ConfigClient] 代理接口请求: url=.../thirdApi/apollo/namespace, params={apolloHostId:15, env:PRO, appId:vcp-openapi...}
   └─ EasyOps → Apollo OpenAPI 实际执行查询
⑦ [ConfigClient] 代理接口响应: status=503, body={"code":503,"message":"无法连接Apollo服务"}
⑧ [apollo-config-query] 代理接口调用失败: HTTPError, ...
⑨ [MCP] 工具执行完成: result={"code":503,"message":"查询失败，EasyOps 代理接口不可用: ..."}
⑩ [HTTP] /mcp 返回: {...} → 调用方收到 JSON
```

每次请求在日志里按 `[HTTP] → [MCP] → [模块标识] → [ConfigClient] → [模块标识] → [MCP] → [HTTP]` 顺序打点，出入参数齐全。

### 10.8 异常与日志开关

- **503 链路**：`_proxy_get` 打印异常状态码和 body → `raise_for_status()` 抛 HTTPError → 业务层 catch 后返回 `{"code":503,...}`（**对外格式稳定**，客户端无感知）
- **日志开关**：`_apply_log_enabled` 通过 `logger.disabled` 控制 apollo-mcp + uvicorn 三件套（`uvicorn` / `uvicorn.error` / `uvicorn.access`），关闭后整条链路静默，不影响业务。HTTP 端点：`POST /log/off`、`POST /log/on`、`GET /log/status`

### 10.9 部署拓扑

```
堡垒机/服务器 → Docker(host 网络) → uvicorn:8062 → EasyOps 代理 → 多套 Apollo
                              ↓
                  日志文件 /app/logs/apollo-mcp.log（docker volume）
```

`docker-compose.yml` 用 `network_mode: host` 复用宿主机网络，解决容器访问内网域名（easyops/各套 Apollo）的 DNS/路由问题。