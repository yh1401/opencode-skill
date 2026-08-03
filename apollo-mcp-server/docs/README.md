# Apollo MCP Server 部署文档

## 1. 概述

Apollo MCP Server 为 Apollo 配置中心提供标准化的 MCP (Model Context Protocol) 工具调用接口，供 StarAgent 平台上的 Skill 进行调用。

**版本**：v2.0.0
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

# 创建 .env 配置文件（生产环境必须关闭 Mock 并填写 Token）
cat > .env << 'EOF'
MCP_USE_MOCK=false
APOLLO_CONFIG_HOST=http://apollo-config.tech.ctseelink.cn:8080
APOLLO_OPENAPI_HOST=http://apollo-config.tech.ctseelink.cn:8070
APOLLO_OPENAPI_TOKEN=your_openapi_token_here
LOG_LEVEL=INFO
EOF
chmod 600 .env

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
docker run -d \
  --name apollo-mcp \
  -p 8062:8062 \
  -e MCP_USE_MOCK=false \
  -e APOLLO_OPENAPI_TOKEN="your_token" \
  -e APOLLO_CONFIG_HOST="http://apollo-config.tech.ctseelink.cn:8080" \
  -e APOLLO_OPENAPI_HOST="http://apollo-config.tech.ctseelink.cn:8070" \
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

# 生产模式（需要 Apollo 网络访问权限）
APOLLO_OPENAPI_TOKEN="your_token" python3 scripts/mcp_server.py --port 8062
```

---

## 3. 环境变量说明

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `MCP_USE_MOCK` | 否 | `false` | Mock 模式（无需 Apollo 服务） |
| `APOLLO_ENV` | 否 | `PRO` | 环境标识：PRO/SIT/DEV |
| `APOLLO_CONFIG_HOST` | 否 | 配置文件中的值 | Apollo ConfigService 地址（端口 8080） |
| `APOLLO_OPENAPI_HOST` | 否 | 配置文件中的值 | Apollo OpenAPI 地址（端口 8070） |
| `APOLLO_OPENAPI_TOKEN` | Mock 模式否 / 生产模式是 | - | Apollo OpenAPI Token |
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
| **服务地址** | `http://{服务器IP}:8062/mcp` |
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
    "version": "2.0.0",
    "capabilities": ["tools"]
  }
}
```

### 步骤 4：验证工具列表

点击 **获取工具列表**，平台会调用 `tools/list`。

**成功响应**：3 个工具
- `apollo_config_query` - 查询 Apollo 配置项
- `apollo_release_history` - 查询发布历史
- `apollo_app_list` - 获取应用列表

### 步骤 5：绑定到 Skill

1. 进入 **Skill 管理** → 找到 `apollo-config-query` 技能
2. 编辑技能配置 → **绑定 MCP 服务**
3. 选择刚才注册的 `apollo-config-query-mcp`
4. 保存

### 步骤 6：测试调用

在 Skill 测试面板中输入：
- `"查一下 user-service 的配置"` → 触发 `apollo_config_query`
- `"rule-engine 最近的发布历史"` → 触发 `apollo_release_history`
- `"当前有哪些应用"` → 触发 `apollo_app_list`

---

## 5. MCP 工具 Schema

### Tool: `apollo_config_query`

查询指定应用的 Namespace 配置项列表。

**参数**：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| appId | string | 是 | - | Apollo 应用 ID |
| clusterName | string | 否 | `default` | 集群名称 |
| namespaceName | string | 否 | `application` | Namespace 名称 |
| key | string | 否 | - | 配置项 Key（模糊匹配） |

### Tool: `apollo_release_history`

查询指定应用的 Namespace 发布历史。

**参数**：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| appId | string | 是 | - | Apollo 应用 ID |
| env | string | 否 | `PRO` | 环境 |
| clusterName | string | 否 | `default` | 集群名称 |
| namespaceName | string | 否 | `application` | Namespace 名称 |

### Tool: `apollo_app_list`

获取 Apollo 中所有可用应用列表。

**参数**：无

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
| `/api/releases` | POST | 发布历史（REST 兼容） |
| `/api/apps` | GET | 应用列表（REST 兼容） |
| `/docs` | GET | Swagger API 文档 |

---

## 7. 故障排查

### 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 健康检查失败 | 容器未启动 | `docker compose up -d` |
| 连接超时 | 网络/防火墙 | 检查端口 8062 是否开放 |
| 返回 401/403 | Apollo Token 无效 | 检查 `APOLLO_OPENAPI_TOKEN` |
| 返回 502 | Apollo 后端未启动 | 检查 Apollo 服务状态 |
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

## 8. 配置 Apollo OpenAPI Token

1. 登录 Apollo Portal（`http://apollo-config.tech.ctseelink.cn:8070`）
2. 进入 **管理员工具** → **开放平台授权**
3. 创建授权，记录生成的 Token
4. 将 Token 配置到 `deploy/.env` 的 `APOLLO_OPENAPI_TOKEN`（Compose 部署）或环境变量（`docker run` / Python 直接运行）

**注意**：Token 不要写入代码或 Git 仓库。`deploy/.env` 已加入忽略规则，且需设置权限 `chmod 600 .env`。

---

## 9. 生产环境检查清单

- [ ] Apollo 生产环境网络可达（ConfigService:8080, OpenAPI:8070）
- [ ] APOLLO_OPENAPI_TOKEN 已配置
- [ ] 服务器端口 8062 已开放
- [ ] Docker / Docker Compose 已安装
- [ ] 日志目录有写权限
- [ ] 健康检查接口可访问
- [ ] StarAgent MCP 注册完成
- [ ] Skill 绑定 MCP 服务完成
- [ ] 端到端调用测试通过