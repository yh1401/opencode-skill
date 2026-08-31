# Apollo MCP 外部接口文档

## 1. 概述

apollo-mcp-server 通过 EasyOps 代理转发接口查询 Apollo 配置，避免调用方直连多套 Apollo 服务。
MCP 4 个工具共调用 **EasyOps 3 个外部接口**：

```text
MCP 工具层
├── apollo_host_list           ──→ 接口1: GET /thirdApi/getApolloHostInfo
├── apollo_app_list            ──→ 接口2: GET /thirdApi/apollo/apps
├── apollo_config_query        ──→ 接口3: GET /thirdApi/apollo/namespace
└── apollo_config_global_search ──→ 接口1 + 接口2 + 接口3（组合调用）

EasyOps 代理层（https://easyops.tech.ctseelink.cn）
    │
    └── 根据 apolloHostId 转发到对应的 Apollo OpenAPI
        └── Apollo 服务（10+ 套，EasyOps 内置 Token 加解密）
```

## 2. 公共约定

### 2.1 基础地址

```text
https://easyops.tech.ctseelink.cn
```

代码里可通过 `APOLLO_HOST_API_BASE` 环境变量覆盖。

### 2.2 鉴权方式

所有 3 个接口使用 **Cookie sessionId** 鉴权：

```http
Cookie: sessionId=e5e27a7d1805758400287ae86741f889
Content-Type: application/json
```

EasyOps 后端同时校验：
- `sessionId` 有效性
- 请求路径
- HTTP 方法
- 来源 IP（配置了 IP 白名单时）

> ⚠️ sessionId **同时是 Apollo Token 的加解密密钥**（见第 6 节）。

### 2.3 HTTP 客户端参数

apollo-mcp-server 内部使用 `requests.get()` 调用，关键参数：

| 项目 | 值 | 说明 |
|---|---|---|
| `verify` | `False` | HTTPS 跳过证书校验（EasyOps 自签证书） |
| 超时 | 各接口独立（见下文） | 避免某个接口超时影响其他 |
| 重试 | Host 列表 1 次，其余 0 次 | 快速失败 |

---

## 3. 接口 1：获取 Apollo Host 列表

MCP 工具：`apollo_host_list`、`apollo_config_global_search`（阶段1）

### 3.1 请求

```http
GET /thirdApi/getApolloHostInfo
```

### 3.2 Query 参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `paginator` | bool | 否 | `false` | 是否分页查询（MCP 固定传 `false` 获取全部） |
| `pageIndex` | int | 否 | `1` | 页码 |
| `pageSize` | int | 否 | `100` | 每页条数 |

> 接口还支持可选过滤参数 `name` / `secondProductId` / `host` / `user` / `token`，MCP 未使用。

### 3.3 curl 示例

```bash
curl -G \
  -H "Cookie: sessionId=e5e27a7d1805758400287ae86741f889" \
  --data-urlencode "paginator=false" \
  --data-urlencode "pageIndex=1" \
  --data-urlencode "pageSize=100" \
  "https://easyops.tech.ctseelink.cn/thirdApi/getApolloHostInfo"
```

### 3.4 代码调用（mcp_server.py）

```python
# L335-356 _fetch_host_list_raw
url = f"{self.api_base}/thirdApi/getApolloHostInfo"
params = {"paginator": False, "pageIndex": 1, "pageSize": 100}
resp = self._send_request(url, params, timeout=self.host_timeout=8, tag="Host列表")
data = resp.json()
if data.get('code') != 'success':
    raise RuntimeError(f"第三方接口返回失败: {data.get('message')}")
host_list = data.get('list', [])
```

- 超时：`APOLLO_HOST_LIST_TIMEOUT` 默认 8s
- 重试：`APOLLO_MAX_RETRIES` 默认 1 次
- 注意：返回的 `list[i].token` 是**加密后**的值，需用 sessionId 解密（见第 6 节）

### 3.5 成功响应

HTTP 状态码：`200`

```json
{
  "code": "success",
  "list": [
    {
      "id": 15,
      "del_flag": false,
      "create_time": "2026-05-29 10:31:20",
      "update_time": "2026-05-29 10:38:51",
      "name": "天翼云眼贵州测试Apollo-亿讯专用",
      "secondProductId": ["518"],
      "host": "https://tyyy-guizh-apollo-test.ctseelink.cn:8154",
      "user": "eshore-user",
      "token": "Gw8CBg4ADgIOBg4ADgIOBg4ADgIOBg==",
      "operation": "",
      "operator": 0
    },
    {
      "id": 13,
      "del_flag": false,
      "name": "天翼看家广州4多AZ生产Apollo",
      "secondProductId": ["705", "532", "641"],
      "host": "https://tykj-guangzhou4d-apollo-prod.ctseelink.cn:8109",
      "user": "easyops",
      "token": "AwZWAQVWAAEDAVQHBgZbBQlRA1lWUlBdDlUNAFMMCwlVBFJRUVkBVQ=="
    }
  ],
  "pageTotal": 10
}
```

**关键字段说明：**

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | int | **即 `apolloHostId`**，是后续接口 2、3 的必填参数 |
| `name` | string | Apollo 服务名称（MCP 展示给用户的"套名"） |
| `host` | string | Apollo OpenAPI 地址 |
| `token` | string | **加密后的 Apollo Token**，需用 sessionId 解密（见第 6 节） |

### 3.6 失败响应

```json
{"code": "fail", "message": "未记录的访问"}
{"code": "fail", "message": "未认证的IP访问!"}
```

---

## 4. 接口 2：获取 Apollo 应用列表

MCP 工具：`apollo_app_list`、`apollo_config_global_search`（阶段2）

### 4.1 请求

```http
GET /thirdApi/apollo/apps?apolloHostId={id}
```

### 4.2 Query 参数

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `apolloHostId` | integer | 是 | EasyOps 中配置的 Apollo 服务 ID（来自接口 1 的 `list[i].id`） |

### 4.3 curl 示例

```bash
curl -G \
  -H "Cookie: sessionId=e5e27a7d1805758400287ae86741f889" \
  --data-urlencode "apolloHostId=13" \
  "https://easyops.tech.ctseelink.cn/thirdApi/apollo/apps"
```

### 4.4 代码调用（mcp_server.py）

```python
# L591-601 fetch_proxy_apps
url = f"{self.api_base}/thirdApi/apollo/apps"
resp = self._proxy_get(url, {"apolloHostId": apollo_host_id},
                       timeout=self.apps_timeout=6, max_retries=0, tag="应用列表")
return resp.json()
```

- 超时：`APOLLO_APPS_TIMEOUT` 默认 6s
- 重试：0 次（快速失败）
- **自动 fallback**：指定套返回 5xx / 超时 / ApolloRequestError 时，MCP 自动切换到第一个探活可用的套

### 4.5 成功响应

HTTP 状态码：`200`

`data` 字段有两种可能的结构，MCP 代码均已兼容：

**结构 A — 直接数组：**
```json
{
  "code": 200,
  "message": "success",
  "data": [
    {"appId": "rule-engine", "name": "规则引擎", "ownerName": "apollo", "orgName": "研发部"},
    {"appId": "user-service", "name": "用户服务", "orgName": "用户中心"}
  ]
}
```

**结构 B — 嵌套 items 对象：**
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "total": 7,
    "items": [
      {"appId": "rule-engine", "name": "规则引擎"},
      {"appId": "user-service", "name": "用户服务"}
    ]
  }
}
```

### 4.6 失败响应

```json
{"code": 404, "message": "Apollo服务不存在", "data": {}}
```

| HTTP code | 含义 | 处理建议 |
|---|---|---|
| `404` | apolloHostId 不存在 / Apollo Token 未授权 | 检查 ID 和 Token |
| `503` | EasyOps 无法连接 Apollo | 检查 Apollo 地址、白名单 |
| `504` | Apollo 响应超时 | 检查 Apollo 服务状态 |

---

## 5. 接口 3：获取单个 Namespace 配置

MCP 工具：`apollo_config_query`、`apollo_config_global_search`（阶段3 扫描每个 namespace）

### 5.1 请求

```http
GET /thirdApi/apollo/namespace
```

### 5.2 Query 参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `apolloHostId` | integer | 是 | - | Apollo 服务 ID |
| `env` | string | 是 | - | 环境，如 `PRO`、`SIT`、`DEV` |
| `appId` | string | 是 | - | Apollo 应用 ID |
| `clusterName` | string | 否 | `default` | 集群名 |
| `namespaceName` | string | 否 | `application` | Namespace 名 |

### 5.3 curl 示例

```bash
# 最简形式（默认 cluster + application namespace）
curl -G \
  -H "Cookie: sessionId=e5e27a7d1805758400287ae86741f889" \
  --data-urlencode "apolloHostId=13" \
  --data-urlencode "env=PRO" \
  --data-urlencode "appId=rule-engine" \
  "https://easyops.tech.ctseelink.cn/thirdApi/apollo/namespace"

# 完整参数
curl -G \
  -H "Cookie: sessionId=e5e27a7d1805758400287ae86741f889" \
  --data-urlencode "apolloHostId=13" \
  --data-urlencode "env=PRO" \
  --data-urlencode "appId=rule-engine" \
  --data-urlencode "clusterName=default" \
  --data-urlencode "namespaceName=datasource" \
  "https://easyops.tech.ctseelink.cn/thirdApi/apollo/namespace"
```

### 5.4 代码调用（mcp_server.py）

```python
# L656-672 fetch_proxy_namespace
url = f"{self.api_base}/thirdApi/apollo/namespace"
params = {
    "apolloHostId": apollo_host_id,
    "env": env,
    "appId": app_id,
    "clusterName": cluster_name,    # 默认 "default"
    "namespaceName": namespace_name, # 默认 "application"
}
resp = self._proxy_get(url, params, timeout=self.timeout=6, max_retries=0, tag="配置查询")
return resp.json()
```

- 超时：`APOLLO_REQUEST_TIMEOUT` 默认 6s
- 重试：0 次
- **自动 fallback**：同接口 2，指定套异常时 MCP 自动切换到可用套
- **全局搜索扩展 namespace**：MCP 全局搜索时会额外扩展扫描 `datasource`、`redis`、`logging`、`common` 等常见 namespace

### 5.5 成功响应

HTTP 状态码：`200`

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "appId": "rule-engine",
    "clusterName": "default",
    "namespaceName": "application",
    "items": [
      {
        "key": "server.port",
        "value": "9000",
        "dataChangeCreatedTime": "2026-08-13T10:00:00.000+0800"
      },
      {
        "key": "max.retry",
        "value": "3",
        "dataChangeCreatedTime": "2026-08-13T10:00:00.000+0800"
      },
      {
        "key": "spring.datasource.url",
        "value": "jdbc:mysql://db-master:3306/mydb",
        "dataChangeCreatedTime": "2026-08-13T10:05:00.000+0800"
      }
    ]
  }
}
```

**关键字段说明：**

| 字段 | 类型 | 说明 |
|---|---|---|
| `data.items` | array | 配置项列表，每项包含 `key` / `value` / `dataChangeCreatedTime` |
| `data.items[].value` | string | **可能包含敏感信息**（数据库密码、Redis 密码、Token 等），MCP 返回给用户时会脱敏 |

### 5.6 失败响应

```json
{"code": 404, "message": "Namespace不存在或应用未授权", "data": {}}
```

| HTTP code | 含义 | 处理建议 |
|---|---|---|
| `404` | appId / namespaceName / env 不存在，或应用未授权 | Apollo 开放平台检查授权 |
| `503` | EasyOps 无法连接 Apollo | 检查白名单 |
| `504` | Apollo 超时 | 检查服务状态 |

---

## 6. Token 加密解密

接口 1 返回的 `token` 字段是 **EasyOps 加密后**的值，加密密钥就是请求时带的 `sessionId`。

### 6.1 算法

1. 原始 token 的每个字符与 sessionId **循环 XOR**
2. XOR 结果做 **Base64 编码**

```
encrypted_bytes[i] = token[i] XOR sessionId[i mod len(sessionId)]
encrypted_token = Base64Encode(encrypted_bytes)
```

### 6.2 解密（MCP 代码）

```python
import base64

def decrypt_token(encrypted_token, session_id):
    decoded = base64.b64decode(encrypted_token)
    original = ''.join(
        chr(decoded[i] ^ ord(session_id[i % len(session_id)]))
        for i in range(len(decoded))
    )
    return original
```

### 6.3 代码位置

`decrypt_token()` — mcp_server.py 内部方法，在组装 Apollo OpenAPI 客户端时自动调用，MCP **对外不透出原始 Token**。

---

## 7. MCP 工具与 API 映射

| MCP 工具 | 调用的 EasyOps 接口 | 说明 |
|---|---|---|
| `apollo_host_list` | 接口 1（host_list） | 获取全部 Apollo 服务列表，MCP 自动解密 token |
| `apollo_app_list` | 接口 1 → 接口 2 | 先查 host_list 获取 apolloHostId，再查 apps；指定套异常时自动 fallback |
| `apollo_config_query` | 接口 1 → 接口 2 → 接口 3 | 先查 apps 获取 namespace 列表（MCP 内置默认集合），再查 config |
| `apollo_config_global_search` | 接口 1 + 接口 2 + 接口 3 | **三阶段全链路组合调用**，总预算 60s 硬上限，超时返回部分结果 |

### 全局搜索三阶段预算（60s）

```
阶段1: host_list    ← 接口1，预算检查 + 最多 8s 超时
阶段2: probe+apps   ← 接口2，预算检查 + 并发探活标记可用套
阶段3: 扫描 config  ← 接口3，wait(timeout) 硬截断，返回 partial
```

每个阶段都有**预算检查点**，任何阶段耗尽预算立即返回 `partial=true` + 标注哪个阶段耗尽。

---

## 8. 超时与容错配置

apollo-mcp-server 内部的超时/重试/fallback 全部可通过环境变量配置：

| 环境变量 | 默认值 | 作用范围 |
|---|---|---|
| `APOLLO_HOST_LIST_TIMEOUT` | 8 | 接口 1（host_list）单次请求超时秒数 |
| `APOLLO_MAX_RETRIES` | 1 | 接口 1 失败重试次数（接口 2、3 固定 0 重试） |
| `APOLLO_APPS_TIMEOUT` | 6 | 接口 2（apps）超时秒数 |
| `APOLLO_REQUEST_TIMEOUT` | 6 | 接口 3（namespace）超时秒数 |
| `APOLLO_TRAVERSAL_BUDGET` | 20 | 遍历可用套的总预算秒数（apps/config fallback 时） |
| `APOLLO_GLOBAL_SEARCH_BUDGET` | 60 | 全局搜索总预算秒数（覆盖接口 1+2+3） |
| `APOLLO_GLOBAL_SEARCH_WORKERS` | 8 | 全局扫描并发线程数 |
| `APOLLO_GLOBAL_SEARCH_MAX_NS` | 5 | 全局搜索每个 app 扫描的 namespace 数量 |
| `APOLLO_HOST_PROBE_ENABLED` | true | 是否开启 host 探活（标记可用/不可用后跳过无效套） |
| `APOLLO_HOST_PROBE_TTL` | 60 | 探活结果缓存秒数 |

### fallback 机制

- **指定套异常**（5xx / 超时 / ApolloRequestError）：接口 2、3 自动切换到第一个探活可用的套
- **全部套不可用**：接口 2、3 逐层向上抛 `RuntimeError`，MCP 返回 `code=503`
- **全局搜索超时**：返回 `partial=true` + `unfinishedApps` 标注未完成的应用，不会卡死

---

## 9. 鉴权权限配置

EasyOps 侧需在 `t_ops_middleware_third_party_access` 表中为调用方配置以下 GET 权限：

```text
/thirdApi/getApolloHostInfo
/thirdApi/apollo/apps
/thirdApi/apollo/namespace
```

每条配置至少包含：
- 调用方使用的 `session_id`
- `method_allow = GET`
- `url_require = /thirdApi/...`（完整路径）
- `ip_allow` = 调用方出口 IP（多个 IP 用英文逗号分隔）
- `is_allow = 1`

---

## 10. 注意事项

1. **apolloHostId ≠ appId**：apolloHostId 是 EasyOps 内部 Apollo 服务 ID（接口 1 返回的 `list[i].id`），appId 是 Apollo 应用 ID
2. **Token 解密是内部行为**：MCP 对外只返回配置项，不暴露 Apollo OpenAPI Token
3. **敏感配置脱敏**：数据库密码、Redis 密码、Token 等敏感配置项值，MCP 返回给用户时做前 2 后 2 脱敏（中间替换为 `******`）
4. **只读**：三个接口均为 GET 只读，不支持增删改
5. **DNS 兜底**：接口 1 若因 EasyOps DNS 解析挂导致超时，全局搜索阶段 2 的探活会标记所有套不可用 → 快速返回 partial（不会卡死）
