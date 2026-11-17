# Apollo MCP Server curl 测试命令

服务器上验证 MCP 服务各工具与接口的 curl 命令集合。

## 1. 前置说明

- **服务地址**：`http://localhost:8062`（在部署服务器本机测试；经 StarAgent 网关则替换为 `http://180.101.21.13:8080/gateway/apollo/mcp`）
- **鉴权**：无（内网部署）；所有命令均为只读查询
- **格式化输出**：服务端返回的 JSON 默认将中文转义为 `\uXXXX`（ASCII 转义，属正常现象），请使用下方解码命令格式化，可直接显示中文；也可先定义别名 `alias jf='python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"'`，后续用 `| jf` 即可
- **超时**：所有命令建议加 `-m 15`（15 秒超时）
- **Mock 模式**（端口 8063）下 appId 必须存在于 `mock/mock_responses.json`，如 `user-service`、`order-service`

***

## 2. 基础检查

### 2.1 健康检查

```bash
curl -sS -m 15 http://localhost:8062/health | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

**正常返回**：`"status": "healthy"`，并包含 `config_source`（第三方接口地址/是否远程配置）、`api_stats`（请求统计）。

### 2.2 服务信息

```bash
curl -sS -m 15 http://localhost:8062/ | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

**正常返回**：服务名、版本 `3.0.0`、已注册工具列表（3 个）、`mock_enabled`。

### 2.3 日志开关（动态开启/关闭，无需重启）

> 关闭后完全静默：不再记录任何日志（含出入参数、错误日志、uvicorn 访问日志）。

```bash
# 查看当前状态（开启/关闭）
curl -sS -m 15 http://localhost:8062/log/status

# 关闭日志（立即生效）
curl -sS -m 15 -X POST http://localhost:8062/log/off

# 开启日志（立即生效）
curl -sS -m 15 -X POST http://localhost:8062/log/on

# 健康检查中也可查看 log_enabled 字段
curl -sS -m 15 http://localhost:8062/health
```

***

## 3. MCP 标准协议（POST /mcp，JSON-RPC 2.0）

> StarAgent 平台即通过该端点调用，与平台测试连接行为一致。

### 3.1 握手 initialize

```bash
curl -sS -m 15 -X POST http://localhost:8062/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "capabilities": {},
      "clientInfo": {"name": "curl", "version": "1.0"}
    }
  }' | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

**正常返回**：`serverInfo.name = apollo-config-query-mcp`，`capabilities.tools` 存在。

### 3.2 工具列表 tools/list

```bash
curl -sS -m 15 -X POST http://localhost:8062/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}' | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

**正常返回**：3 个工具 `apollo_host_list` / `apollo_config_query` / `apollo_app_list`。

### 3.3 工具：apollo\_host\_list（Apollo 服务列表）

```bash
curl -sS -m 15 -X POST http://localhost:8062/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "apollo_host_list",
      "arguments": {}
    }
  }' | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

**正常返回**：`data.hosts` 为各套 Apollo 列表，每条含 `apolloHostId`、`name`、`host`、`has_token`、`is_default`。

> 该工具用于确定后续查询的 `apolloHostId`（查询链路第一步）。

### 3.4 工具：apollo\_app\_list（应用列表）

```bash
curl -sS -m 15 -X POST http://localhost:8062/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "apollo_app_list",
      "arguments": {}
    }
  }' | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

- `apolloHostId` **可选**：不传时工具自动遍历所有可用套，取第一个能成功返回应用列表的套（默认套不可用时会自动切换到其它可用套）
- **正常返回**：`data` 为该套 Apollo 的应用列表（appId、name、orgName 等）；未传 `apolloHostId` 时结果顶层带 `usedHostId` / `hostName` 标注实际使用的套
- **用途**：用户未提供 appId 时，先调用本工具拿到真实应用列表，选择 appId 后再用 `apollo_config_query` 查询配置（两阶段引导）

### 3.5 工具：apollo\_config\_query（配置查询）

```bash
curl -sS -m 15 -X POST http://localhost:8062/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "apollo_config_query",
      "arguments": {
        "appId": "vcp-openapi",
        "env": "PRO",
        "namespaceName": "application",
        "key": "timeout"
      }
    }
  }' | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

**参数说明**：

| 参数            | 必填 | 默认值           | 说明                         |
| ------------- | -- | ------------- | -------------------------- |
| appId         | 是  | -             | 应用 ID，来自 `apollo_app_list` |
| apolloHostId  | 否  | 默认套           | 指定哪套 Apollo                |
| env           | 否  | `PRO`         | 环境                         |
| clusterName   | 否  | `default`     | 集群                         |
| namespaceName | 否  | `application` | Namespace                  |
| key           | 否  | 全部            | 配置项 Key 模糊过滤               |

**正常返回**：`data.configurations` 为配置项字典；带 `key` 时含 `_total_count` / `_filtered_count`。

***

## 4. 平台兼容层

### 4.1 工具列表 GET /tools/list

```bash
curl -sS -m 15 http://localhost:8062/tools/list | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

### 4.2 调用工具 POST /tools/call

```bash
# 例：apollo_host_list
curl -sS -m 15 -X POST http://localhost:8062/tools/call \
  -H "Content-Type: application/json" \
  -d '{"params": {"name": "apollo_host_list", "arguments": {}}}' | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"

# 例：apollo_config_query
curl -sS -m 15 -X POST http://localhost:8062/tools/call \
  -H "Content-Type: application/json" \
  -d '{"params": {"name": "apollo_config_query", "arguments": {"appId": "user-service", "env": "PRO"}}}' | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

***

## 5. REST 兼容层

### 5.1 配置查询 POST /api/config

```bash
curl -sS -m 15 -X POST http://localhost:8062/api/config \
  -H "Content-Type: application/json" \
  -d '{"appId": "user-service", "env": "PRO", "namespaceName": "application" | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

### 5.2 应用列表 GET /api/apps

```bash
curl -sS -m 15 http://localhost:8062/api/apps | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"
```

***

## 6. 快速全流程测试

一条命令验证「服务列表 → 应用列表 → 配置查询」完整链路：

```bash
# 1. 健康检查
curl -sS -m 15 http://localhost:8062/health -o /dev/null -w "health: %{http_code}\n"

# 2. 工具列表
curl -sS -m 15 http://localhost:8062/tools/list -o /dev/null -w "tools/list: %{http_code}\n"

# 3. apollo_host_list（获取默认 apolloHostId）
curl -sS -m 15 -X POST http://localhost:8062/tools/call \
  -H "Content-Type: application/json" \
  -d '{"params": {"name": "apollo_host_list", "arguments": {}}}' | python3 -c "import json,sys; d=json.load(sys.stdin); hosts=d['result']['content'][0]['text']; print(hosts[:500])"

# 4. apollo_app_list
curl -sS -m 15 -X POST http://localhost:8062/tools/call \
  -H "Content-Type: application/json" \
  -d '{"params": {"name": "apollo_app_list", "arguments": {}}}' | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['result']['content'][0]['text'][:500])"

# 5. apollo_config_query
curl -sS -m 15 -X POST http://localhost:8062/tools/call \
  -H "Content-Type: application/json" \
  -d '{"params": {"name": "apollo_config_query", "arguments": {"appId": "user-service"}}}' | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['result']['content'][0]['text'][:500])"
```

***

## 7. 常见返回对照

| 返回                                                  | 含义                                             | 处理                                                    |
| --------------------------------------------------- | ---------------------------------------------- | ----------------------------------------------------- |
| `{"code": 200, "data": {...}}`                      | 查询成功                                           | -                                                     |
| `{"code": 503, "message": "...EasyOps 代理接口不可用..."}` | 代理接口调用不通                                       | 见下方排查                                                 |
| `{"code": 404, ...}`                                | apolloHostId / appId / env / namespace 不存在或未授权 | 用 `apollo_host_list` 核对 apolloHostId；检查 Apollo 开放平台授权 |
| `{"code": 401, ...}`                                | EasyOps 保存的该套 Apollo Token 无效                  | 检查第三方接口中该套 Token                                      |
| `{"jsonrpc":"2.0","error":{"code":-32602,...}}`     | 参数错误（如 appId 缺失）                               | 检查必填参数                                                |

> 代理接口 503（EasyOps 无法连接目标 Apollo）：先用 `apollo_host_list` 确认 `apolloHostId`，再用 `curl -m 10 -b "sessionId=..." "https://easyops.tech.ctseelink.cn/thirdApi/apollo/apps?apolloHostId=15"` 复现；重点检查该套 Apollo 的服务状态、网络与白名单（EasyOps 出口 IP 需在 Apollo 白名单内）。







curl -sS -m 30 -X POST http://localhost:8062/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"apollo_app_list","arguments":{"apolloHostId": 2}}}' \
  | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"



session_id="e5e27a7d1805758400287ae86741f889"
for hid in $(curl -s -m 10 -b "sessionId=$session_id" \
  "https://easyops.tech.ctseelink.cn/thirdApi/getApolloHostInfo" \
  | python3 -c "import sys,json; [print(x['id']) for x in json.load(sys.stdin)['list']]"); do
  echo "===== apolloHostId=$hid ====="
  curl -m 15 -b "sessionId=$session_id" \
    "https://easyops.tech.ctseelink.cn/thirdApi/apollo/apps?apolloHostId=$hid"
  echo
done



curl -G \
  -H "Cookie: sessionId=e5e27a7d1805758400287ae86741f889" \
  --data-urlencode "apolloHostId=2" \
  "https://easyops.tech.ctseelink.cn/thirdApi/apollo/apps" \
    | python3 -c "import sys,json; print(json.dumps(json.load(sys.stdin), ensure_ascii=False, indent=2))"



---
现在的问题是请求的地址是不对的
 -- https://easyops.tech.ctseelink.cn/thirdApi/apollo/apps
 还有这个也不对 -- https://easyops.tech.ctseelink.cn/thirdApi/apollo/apps




