# Apollo MCP Server 部署文档

## 1. 概述

Apollo MCP Server 为 Apollo 配置中心提供标准化的 MCP (Model Context Protocol) 工具调用接口，供 StarAgent 平台上的 Skill 进行调用。

**查询链路**：哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)

- 支持**多套 Apollo**（各产品线/环境独立部署），通过 `apolloHostId` 指定任意一套
- 配置/应用查询统一走 **EasyOps 代理接口**（无需本地保存 Apollo Token，按 `apolloHostId` 指定任意一套）；调用不通时直接返回错误提示
- 提供 3 个工具：`apollo_host_list` / `apollo_config_query` / `apollo_app_list`

**版本**：v3.0.0
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

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `MCP_USE_MOCK` | 否 | `false` | Mock 模式（无需 Apollo 服务） |
| `APOLLO_ENV` | 否 | 配置文件 `default_env` | 环境标识（用于日志与展示） |
| `APOLLO_HOST_API_BASE` | 否 | `https://easyops.tech.ctseelink.cn` | 第三方「Apollo Host 信息查询接口」地址 |
| `APOLLO_HOST_SESSION_ID` | 否 | 内置默认值 | 第三方鉴权 sessionId（32 位），同时是 token 解密密钥 |
| `APOLLO_HOST_NAME` | 否 | - | 默认 Apollo 套的模糊过滤名称（多套中指定一套，不填取第一条） |
| `CONFIG_SERVICE_REFRESH_SEC` | 否 | `60` | 第三方接口配置刷新间隔（秒） |
| `LOG_LEVEL` | 否 | `INFO` | 日志级别：DEBUG/INFO/WARNING/ERROR |

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
    "version": "3.0.0",
    "capabilities": {
      "tools/list": {},
      "tools/call": {}
    }
  }
}
```

### 步骤 4：验证工具列表

点击 **获取工具列表**，平台会调用 `tools/list`。

**成功响应**：3 个工具
- `apollo_host_list` - 实时查询可用的 Apollo 服务列表（查询链路第一步，返回 `apolloHostId`）
- `apollo_config_query` - 查询 Apollo 配置项（`apolloHostId` + env + appId + cluster + namespace + key）
- `apollo_app_list` - 获取应用列表（`apolloHostId` 可选）

### 步骤 5：绑定到 Skill

1. 进入 **Skill 管理** → 找到 `apollo-config-query` 技能
2. 编辑技能配置 → **绑定 MCP 服务**
3. 选择刚才注册的 `apollo-config-query-mcp`
4. 保存

### 步骤 6：测试调用

在 Skill 测试面板中输入：
- `"有哪些 Apollo 环境"` → 触发 `apollo_host_list`（确定哪套 Apollo）
- `"查一下 user-service 的配置"` → 触发 `apollo_config_query`
- `"当前有哪些应用"` → 触发 `apollo_app_list`
- `"查一下贵州那套 Apollo 上 user-service 的配置"` → `apollo_host_list` 取 apolloHostId → `apollo_config_query`（完整链路）

---

## 5. MCP 工具 Schema

### Tool: `apollo_host_list`

实时查询所有可用的 Apollo 服务列表（查询链路第一步，用于确定哪套 Apollo）。

**参数**：无

**返回要点**：每条记录含 `apolloHostId`（即第三方记录 `id`）、`name`、`host`、`has_token`、`secondProductId`、`user`、`is_default`。

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

### Tool: `apollo_app_list`

获取指定 Apollo 服务中的所有应用列表（AppId、应用名称、所属部门）。

**参数**：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| apolloHostId | integer | 否 | 默认 Apollo | Apollo 服务 ID，指定查询哪一套 |

**调用路径**：EasyOps 代理接口 `/thirdApi/apollo/apps`（仅需 Cookie `sessionId`），调用不通时返回错误提示。

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