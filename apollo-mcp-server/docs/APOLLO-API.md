# Apollo 接口对接文档

Apollo 配置中心对外提供 **OpenAPI（8070）**，本文档对接其中 2 个接口：获取配置项、获取应用列表。两个接口均为 GET、均需 Token，共用同一个服务地址。

## 接口总览

| # | 接口 | 方法 | 地址 | 认证 |
|---|------|------|------|------|
| 1 | 获取配置项 | GET | `/openapi/v1/envs/{env}/apps/{appId}/clusters/{clusterName}/namespaces/{namespaceName}` | Token |
| 2 | 获取应用列表 | GET | `/openapi/v1/apps` | Token |

## 环境地址与认证

| 环境 | OpenAPI (8070) |
|------|----------------|
| PRO | `http://apollo-config.tech.ctseelink.cn:8070` |
| SIT | `http://apollo-sit.tech.ctseelink.cn:8070` |
| DEV | `http://apollo-dev.tech.ctseelink.cn:8070` |

认证方式：所有请求 Header 传 `Authorization: <Token>`（裸 Token，不加 `Bearer`）。

Token 获取：登录 Portal（8070）→ 管理员工具 → 开放平台授权 → 创建授权后生成。

> 注意：**appId 需在开放平台授权列表中**，否则查询会返回 404。

## 接口详情

### 1. 获取配置项

```
GET /openapi/v1/envs/{env}/apps/{appId}/clusters/{clusterName}/namespaces/{namespaceName}
```

**参数**

| 参数 | 必填 | 默认 | 说明 |
|------|------|------|------|
| env | ✅ | - | 环境：PRO / SIT / DEV |
| appId | ✅ | - | 应用 ID |
| clusterName | ❌ | `default` | 集群名，不传默认 `default` |
| namespaceName | ❌ | `application` | Namespace 名，不传默认 `application` |

**请求示例**

```bash
curl -H "Authorization: <TOKEN>" \
  "http://apollo-config.tech.ctseelink.cn:8070/openapi/v1/envs/PRO/apps/rule-engine/default/application"
```

clusterName、namespaceName 可不传，接口按默认值 `default` / `application` 处理。

**响应示例**

```json
{
  "appId": "rule-engine",
  "clusterName": "default",
  "namespaceName": "application",
  "items": [
    { "key": "server.port", "value": "9000", "dataChangeCreatedTime": "..." },
    { "key": "max.retry", "value": "3", "dataChangeCreatedTime": "..." }
  ]
}
```

> 返回 `items` 数组（key/value 列表）。如通过 apollo-mcp-server 调用，会转换为 `configurations` 字典（`{key: value}`），并统一包装为 `{code, message, data}`。

### 2. 获取应用列表

```
GET /openapi/v1/apps
```

无参数。

**请求示例**

```bash
curl -H "Authorization: <TOKEN>" "http://apollo-config.tech.ctseelink.cn:8070/openapi/v1/apps"
```

**响应示例**

```json
[
  { "appId": "rule-engine", "name": "规则引擎", "ownerName": "apollo", "orgName": "研发部" }
]
```

## 返回格式（通过 apollo-mcp-server 调用）

统一返回 `{code, message, data}`：

```json
{
  "code": 200,
  "message": "success",
  "data": {}
}
```

## 错误码

| code | 含义 |
|------|------|
| 200 | 成功 |
| 400 | 请求参数错误 |
| 401 | Token 无效或未配置 |
| 404 | 资源不存在（appId/env 错误，或 appId 未在开放平台授权） |
| 500 | 服务内部异常 |
| 503 | 网络连接失败 |
| 504 | 请求超时（30s） |

## 对接要点

1. 两个接口均为 GET，无请求体，均需 Token（Header `Authorization`，裸 Token）
2. 共用同一个 OpenAPI 地址（8070），使用同一 Token
3. 401 查 Token 是否有效；404 查 appId/env 拼写，或 appId 是否在开放平台授权列表
4. 发布历史不在 OpenAPI 能力范围内（`/releases` 为 POST 发布接口，非查询历史），如需查看请使用 Apollo Portal 网页端
