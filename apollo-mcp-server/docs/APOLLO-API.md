# Apollo 接口对接文档

Apollo MCP 服务调用 Apollo 的 2 个接口（查询配置、应用列表），统一返回 `{code, message, data}` 格式。两个接口均走 **OpenAPI（8070）**，共用同一个服务地址，仅需配置一个地址 + 一个 Token。

## 接口地址

两个接口均由 Apollo 官方 **OpenAPI（8070）** 提供，共用同一个服务地址，仅需配置一个地址 + 一个 Token。

## 接口总览

| # | 接口 | 服务 | 认证 |
|---|------|------|------|
| 1 | 获取配置项 | OpenAPI (8070) | Token |
| 2 | 获取应用列表 | OpenAPI (8070) | Token |

## 环境地址

统一使用 OpenAPI 服务地址（MCP 端只需配置这一个地址 + Token）：

| 环境 | OpenAPI (8070) |
|------|----------------|
| PRO | `http://apollo-config.tech.ctseelink.cn:8070` |
| SIT | `http://apollo-sit.tech.ctseelink.cn:8070` |
| DEV | `http://apollo-dev.tech.ctseelink.cn:8070` |

认证：所有接口 Header 传 `Authorization: <Token>`（裸 Token，不加 Bearer）。Token 在 Portal「管理员工具 → 开放平台授权」获取，且 **appId 需在授权列表中**，否则查询会失败。

## 接口详情

### 1. 获取配置项（需 Token）

```
GET /openapi/v1/envs/{env}/apps/{appId}/clusters/{clusterName}/namespaces/{namespaceName}
```

| 参数 | 必填 | 默认 | 说明 |
|------|------|------|------|
| env | ✅ | - | 环境（PRO/SIT/DEV） |
| appId | ✅ | - | 应用 ID |
| clusterName | ❌ | `default` | 集群名 |
| namespaceName | ❌ | `application` | Namespace 名 |

```bash
curl -H "Authorization: <TOKEN>" "http://apollo-config.tech.ctseelink.cn:8070/openapi/v1/envs/PRO/apps/rule-engine/default/application"
```

```json
{"appId":"rule-engine","clusterName":"default","namespaceName":"application","items":[{"key":"server.port","value":"8080","dataChangeCreatedTime":"..."}]}
```

> 返回 `items` 数组，MCP 端会转换为 `configurations` 字典（`{key: value}`），与 ConfigService 的返回格式保持一致。

### 2. 获取应用列表（需 Token）

```
GET /openapi/v1/apps
```

```bash
curl -H "Authorization: <TOKEN>" "http://apollo-config.tech.ctseelink.cn:8070/openapi/v1/apps"
```

```json
[{"appId":"rule-engine","name":"规则引擎","ownerName":"apollo","orgName":"研发部"}]
```

## 不支持的能力说明

- **发布历史查询**：OpenAPI 中不存在"发布历史列表"接口（`/releases` 是 POST 发布配置，不是查询历史）。发布历史仅在 Apollo Portal 网页端（需登录态）提供，MCP 端不支持该能力。如确实需要，可在 Portal 网页查看，或后续在网关层对接 AdminService（8090）时补充。
- **ConfigService (8080)**：MCP 端不直接调用，仅用于应用 SDK 读取配置。

## 错误码

| code | 含义 |
|------|------|
| 200 | 成功 |
| 400 | 请求参数错误 |
| 401 | Token 无效或未配置 |
| 404 | 资源不存在（appId/env 错误，或 appId 未在开放平台授权） |
| 503 | 网络连接失败 |
| 504 | 请求超时（30s） |

## 对接要点

1. 两个接口均为 **GET**，无请求体，全部需 Token（Header `Authorization`，裸 Token）
2. 两个接口共用同一个 OpenAPI 地址，仅需在 Portal 开放平台授权后使用同一 Token
3. 401 查 Token，404 查 appId/env 拼写或开放平台授权
