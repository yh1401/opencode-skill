# Apollo OpenAPI 转发接口对接文档

## 1. 接口说明

EasyOps 后端统一转发 Apollo OpenAPI 请求，用于解决多个 Apollo 服务均配置访问白名单时，新服务器需要重复申请白名单的问题。

调用链路：

```text
调用方服务器 -> EasyOps 后端 -> 指定的 Apollo OpenAPI 服务
```

- 调用方只访问 EasyOps 后端，无需直接访问 Apollo。
- Apollo 地址及 Token 由 EasyOps 后端保存，调用方不需要传递 Apollo Token。
- `apolloHostId` 用于选择 EasyOps 已配置的 Apollo 服务。
- Apollo 侧只需为 EasyOps 后端服务器配置访问白名单。
- 两个接口均为只读 GET 接口。

## 2. 接口总览

| 接口 | 方法 | EasyOps 路径 |
|---|---|---|
| 获取 Apollo 应用列表 | GET | `/thirdApi/apollo/apps` |
| 获取单个 Namespace 配置 | GET | `/thirdApi/apollo/namespace` |

实际访问地址：

```text
{EASYOPS_BASE_URL}/thirdApi/apollo/apps
{EASYOPS_BASE_URL}/thirdApi/apollo/namespace
```

其中 `{EASYOPS_BASE_URL}` 为 EasyOps 后端服务地址。

## 3. 调用鉴权

接口使用 EasyOps 第三方接口鉴权。调用方需要在 Cookie 中传递分配的 `sessionId`：

```http
Cookie: sessionId=<THIRD_PARTY_SESSION_ID>
```

EasyOps 后端会同时校验：

- `sessionId`
- 请求路径
- HTTP 方法
- 来源 IP（配置了 IP 限制时）

调用方不应传递 Apollo 的 `Authorization` Token。

## 4. 获取 Apollo 应用列表

### 4.1 请求

```http
GET /thirdApi/apollo/apps?apolloHostId={apolloHostId}
```

### 4.2 Query 参数

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `apolloHostId` | integer | 是 | EasyOps 中配置的 Apollo 服务 ID |

### 4.3 curl 示例

```bash
curl -G \
  -H "Cookie: sessionId=<THIRD_PARTY_SESSION_ID>" \
  --data-urlencode "apolloHostId=1" \
  "https://easyops.example.com/thirdApi/apollo/apps"
```

### 4.4 成功响应

HTTP 状态码：`200`

```json
{
  "code": 200,
  "message": "success",
  "data": [
    {
      "appId": "rule-engine",
      "name": "规则引擎",
      "ownerName": "apollo",
      "orgName": "研发部"
    }
  ]
}
```

## 5. 获取单个 Namespace 配置

### 5.1 请求

```http
GET /thirdApi/apollo/namespace
```

### 5.2 Query 参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| `apolloHostId` | integer | 是 | - | EasyOps 中配置的 Apollo 服务 ID |
| `env` | string | 是 | - | Apollo 环境，例如 `PRO`、`SIT`、`DEV` |
| `appId` | string | 是 | - | Apollo 应用 ID |
| `clusterName` | string | 否 | `default` | Apollo 集群名 |
| `namespaceName` | string | 否 | `application` | Apollo Namespace 名 |

### 5.3 使用默认 Cluster 和 Namespace

```bash
curl -G \
  -H "Cookie: sessionId=<THIRD_PARTY_SESSION_ID>" \
  --data-urlencode "apolloHostId=1" \
  --data-urlencode "env=PRO" \
  --data-urlencode "appId=rule-engine" \
  "https://easyops.example.com/thirdApi/apollo/namespace"
```

### 5.4 指定 Cluster 和 Namespace

```bash
curl -G \
  -H "Cookie: sessionId=<THIRD_PARTY_SESSION_ID>" \
  --data-urlencode "apolloHostId=1" \
  --data-urlencode "env=PRO" \
  --data-urlencode "appId=rule-engine" \
  --data-urlencode "clusterName=default" \
  --data-urlencode "namespaceName=application" \
  "https://easyops.example.com/thirdApi/apollo/namespace"
```

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
      }
    ]
  }
}
```

`data` 内容为 Apollo OpenAPI 的原始 Namespace 响应，配置项位于 `data.items`。

## 6. 错误响应

标准错误响应：

```json
{
  "code": 404,
  "message": "Apollo服务不存在",
  "data": {}
}
```

| HTTP 状态码 / code | 含义 | 处理建议 |
|---|---|---|
| `400` | 请求参数缺失或格式错误 | 检查必填参数及参数类型 |
| `401` | EasyOps 保存的 Apollo Token 无效 | 检查 Apollo 服务配置中的 Token |
| `404` | Apollo 服务、应用、环境或 Namespace 不存在，或者应用未授权 | 检查 `apolloHostId`、`env`、`appId` 及 Apollo 开放平台授权 |
| `500` | EasyOps 或 Apollo 响应处理异常 | 根据响应信息检查服务日志 |
| `503` | EasyOps 无法连接 Apollo | 检查 Apollo 地址、网络和 Apollo 白名单 |
| `504` | Apollo 请求超过 30 秒 | 检查 Apollo 服务状态和网络延迟 |

第三方鉴权失败时，当前鉴权中间件返回：

```json
{
  "code": "fail",
  "message": "未记录的访问"
}
```

或：

```json
{
  "code": "fail",
  "message": "未认证的IP访问!"
}
```

## 7. 上线配置

### 7.1 Apollo 服务配置

确认 EasyOps 的 Apollo 管理中已经录入目标服务，且包含正确的：

- Apollo OpenAPI 地址
- Apollo OpenAPI Token
- 对应的 `apolloHostId`

Apollo OpenAPI Token 必须已授权需要查询的 `appId`，否则 Apollo 可能返回 `404`。

### 7.2 Apollo 白名单

在每套 Apollo 服务中，将 EasyOps 后端服务器的出口 IP 加入白名单。调用方服务器无需加入 Apollo 白名单。

### 7.3 EasyOps 第三方接口权限

在表 `t_ops_middleware_third_party_access` 中为调用方配置以下两条 GET 权限：

```text
/thirdApi/apollo/apps
/thirdApi/apollo/namespace
```

每条配置至少需要包含：

- 调用方使用的 `session_id`
- `method_allow = GET`
- `url_require` 为完整接口路径
- `ip_allow` 为调用方出口 IP，多个 IP 使用英文逗号分隔
- `is_allow = 1`

建议始终配置 `ip_allow`，避免配置内容被未授权来源读取。

## 8. 注意事项

1. `apolloHostId` 是 EasyOps 内部 Apollo 服务 ID，不是 Apollo 的 `appId`。
2. 调用方不得在请求参数、Header 或日志中保存 Apollo Token。
3. 接口返回的配置项可能包含数据库地址等敏感内容，调用方应限制日志输出和数据传播范围。
4. 两个接口只提供查询能力，不支持配置新增、修改、发布或回滚。
5. Apollo Token 更新后只需更新 EasyOps 中的 Apollo 服务配置，调用方无需修改。
