# cmdb-mcp-server

企业 CMDB 运维数据综合查询 MCP 服务器，遵循标准 MCP 协议规范实现。

**版本**：v2.0.0  
**协议**：MCP Streamable HTTP (JSON-RPC 2.0)  
**端口**：8061

## 什么是 MCP

MCP (Model Context Protocol) 是一个开放协议，用于标准化应用程序如何为 LLM 提供上下文。它定义了 AI 模型与应用程序之间交互的标准格式，就像 AI 应用的 "USB-C" 接口。

### 核心概念

```
┌─────────────────────────────────────────────────────────────────┐
│                          LLM / AI 模型                           │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ 需要访问外部工具/数据
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        StarAgent 平台                           │
│                     (MCP 客户端实现)                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ MCP 协议通信
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        MCP 服务器                                │
│                     (提供工具和资源)                             │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Tools (工具) - 可调用的功能                              │  │
│  │    • cmdb_server_query                                    │  │
│  │    • server_public_ip_query                               │  │
│  │    • project_deployment_query                            │  │
│  │    • product_query                                        │  │
│  │    • project_basis_query                                 │  │
│  └──────────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Resources (资源) - 可访问的数据                          │  │
│  │    • config/api_endpoints.json                            │  │
│  │    • references/mock_responses.json                       │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ 实际数据访问
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        外部系统                                  │
│    ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐     │
│    │ CMDB API │  │ 公网IP   │  │ 部署记录  │  │ 产品信息  │     │
│    └──────────┘  └──────────┘  └──────────┘  └──────────┘     │
└─────────────────────────────────────────────────────────────────┘
```

## 架构说明

### 新架构（推荐）

```
┌─────────────────────────────────────────────────────────────────┐
│                     StarAgent 平台                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ MCP Tool Call
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│              cmdb-mcp-server (独立部署)                │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  MCP 协议层（工具注册 & 参数定义）                        │  │
│  │    • cmdb_server_query                                    │  │
│  │    • server_public_ip_query                               │  │
│  │    • project_deployment_query                            │  │
│  │    • product_query                                        │  │
│  │    • project_basis_query                                 │  │
│  └──────────────────────────────────────────────────────────┘  │
│                              │                                    │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  CMDBAPIClient（统一请求处理）                            │  │
│  │    • 连接池管理  • 超时控制  • 自动降级                   │  │
│  └──────────────────────────────────────────────────────────┘  │
│                              │                                    │
│  ┌──────────────┬──────────────┬──────────────┬──────────────┐  │
│  │  CMDB API    │  公网IP API  │  部署记录API │  产品信息API │  │
│  └──────────────┴──────────────┴──────────────┴──────────────┘  │
│                              │                                    │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Mock 数据降级 (references/mock_responses.json)           │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

**优势**：
- ✅ 标准化协议，易于集成
- ✅ 连接复用，性能提升
- ✅ 统一错误处理和降级
- ✅ 独立部署，易于扩展和监控
- ✅ 支持 MCP 和 HTTP 双模式

## 特性

- ✅ **标准 MCP 协议**：遵循 MCP 2025-03-26 规范，支持 initialize、tools/list、tools/call
- ✅ **Streamable HTTP 模式**：POST /mcp 单一入口，所有协议消息通过此端点发送
- ✅ **SSE 传输模式**：GET /sse + POST /messages，支持服务器推送
- ✅ **JSON-RPC 2.0**：标准协议格式，完整错误处理
- ✅ **平台兼容**：支持 StarAgent 平台的 /tools/list、/tools/call 端点映射
- ✅ **自动降级**：API 请求失败时自动使用 Mock 数据
- ✅ **连接复用**：使用 requests.Session 保持长连接
- ✅ **超时控制**：30秒超时保护
- ✅ **兼容层**：保留原有 /api/* 端点，支持向后兼容
- ✅ **Swagger 文档**：HTTP 模式提供交互式 API 文档

## 快速开始

### 1. 安装依赖

```bash
pip install -r requirements.txt
```

### 2. 启动服务器

```bash
# 方式一：使用启动脚本
./start.sh

# 方式二：指定端口启动
./start.sh 0.0.0.0 8080

# 方式三：直接启动 Python 脚本
python3 scripts/mcp_server.py --port 8000

# 方式四：后台启动
./start.sh -d
```

### 3. 访问文档

- API 文档：http://localhost:8061/docs
- 服务信息：http://localhost:8061/
- 健康检查：http://localhost:8061/health

## 标准 MCP 协议接口

### 协议规范

遵循标准 MCP 协议：
- **传输层**：HTTP/SSE
- **协议格式**：JSON-RPC 2.0
- **端点**：GET /sse（服务器推送）、POST /messages（客户端消息）、POST /mcp（单一入口）

### 通信流程

```
Agent 平台                  MCP 服务器
      │                       │
      │  1. 建立 SSE 连接       │
      ├─────────────────────>│  GET /sse
      │<─────────────────────┤  SSE 连接建立
      │                       │
      │  2. 初始化握手          │
      ├─────────────────────>│  POST /messages {"method":"initialize"}
      │<─────────────────────┤  返回 capabilities
      │                       │
      │  3. 获取工具列表        │
      ├─────────────────────>│  POST /messages {"method":"tools/list"}
      │<─────────────────────┤  返回工具定义（含 inputSchema）
      │                       │
      │  4. 调用工具            │
      ├─────────────────────>│  POST /messages {"method":"tools/call"}
      │<─────────────────────┤  返回工具执行结果
      │                       │
```

### API 端点

#### 标准 MCP 端点（推荐）

| 端点 | 方法 | 说明 |
|------|------|------|
| `/mcp` | POST | **标准单一入口**，所有 MCP 协议消息通过此端点发送（Streamable HTTP 模式） |
| `/sse` | GET | SSE 连接端点，接收服务器推送消息 |
| `/messages` | POST | MCP 消息端点（SSE 模式配套） |

#### StarAgent 平台兼容端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/tools/list` | POST/GET | 平台工具发现端点，返回工具列表 |
| `/tools/call` | POST | 平台工具调用端点，执行工具调用 |
| `/tools` | POST/GET | 平台工具兼容端点 |

#### 兼容层端点（向后兼容）

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/cmdb-server-query` | POST | 查询 CMDB 服务器信息 |
| `/api/server-public-ip-query` | POST | 查询服务器公网 IP |
| `/api/project-deployment-query` | POST | 查询项目部署记录 |
| `/api/product-query` | POST | 查询产品信息 |
| `/api/project-basis-query` | POST | 查询项目基础信息 |

#### 基础端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/docs` | GET | Swagger 文档 |
| `/health` | GET | 健康检查 |
| `/` | GET | 服务信息首页 |

### MCP 协议消息示例

#### initialize（初始化）

**请求**：
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {}
}
```

**响应**：
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "name": "cmdb-mcp-server",
    "version": "2.0.0",
    "capabilities": {
      "tools/list": {},
      "tools/call": {}
    }
  }
}
```

#### tools/list（获取工具列表）

**请求**：
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/list",
  "params": {}
}
```

**响应**：
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "cmdb_server_query",
        "description": "查询 CMDB 服务器信息...",
        "inputSchema": {
          "type": "object",
          "properties": {
            "ip": {"type": "string", "description": "IP 地址"},
            "hostName": {"type": "string", "description": "主机名"},
            "currentPage": {"type": "integer", "description": "页码", "default": 1},
            "pageSize": {"type": "integer", "description": "每页条数", "default": 15}
          }
        }
      }
    ]
  }
}
```

#### tools/call（调用工具）

**请求**：
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "cmdb_server_query",
    "arguments": {
      "ip": "192.168.7.101",
      "currentPage": 1,
      "pageSize": 10
    }
  }
}
```

**响应**：
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"code\":200,\"data\":{...}}"
      }
    ]
  }
}
```

## 工具列表

| 工具名 | 说明 | 参数示例 |
|--------|------|---------|
| `cmdb_server_query` | 查询 CMDB 服务器信息 | `ip`, `hostName`, `node`, `state` |
| `server_public_ip_query` | 查询服务器公网 IP | `ip`, `hostName`, `node` |
| `project_deployment_query` | 查询项目部署记录 | `projectName`, `environment`, `deploymentStatus` |
| `product_query` | 查询产品信息 | `productName`, `department` |
| `project_basis_query` | 查询项目基础信息 | `projectName`, `productName`, `projectType` |

## Agent 平台配置

### SSE 模式配置（推荐）

在 Agent 平台添加 MCP 连接：

```json
{
  "name": "ops-data-query",
  "type": "mcp",
  "transport": "http",
  "url": "http://localhost:8061",
  "description": "企业 CMDB 运维数据综合查询",
  "enabled": true
}
```

### 配置字段说明

| 字段 | 说明 |
|------|------|
| `name` | 服务唯一标识，平台内全局唯一 |
| `type` | 固定为 `"mcp"` |
| `transport` | 传输模式：`http`（当前实现） |
| `url` | MCP 服务基础地址，平台自动拼接 `/sse` 和 `/messages` |
| `description` | 服务描述 |
| `enabled` | 是否启用 |

## 兼容层 API

原有 HTTP API 端点保持不变，支持向后兼容：

```bash
# 查询服务器信息
curl -X POST http://localhost:8061/api/cmdb-server-query \
  -H "Content-Type: application/json" \
  -d '{"ip": "192.168.7.101", "currentPage": 1, "pageSize": 10}'

# 查询公网 IP
curl -X POST http://localhost:8061/api/server-public-ip-query \
  -H "Content-Type: application/json" \
  -d '{"ip": "192.168.7.101"}'

# 查询部署记录
curl -X POST http://localhost:8061/api/project-deployment-query \
  -H "Content-Type: application/json" \
  -d '{"projectName": "guizh-rules-api"}'
```

## 测试

### 启动服务

```bash
MCP_USE_MOCK=true python3 scripts/mcp_server.py --port 8000
```

### 测试标准 MCP 协议

```bash
# 1. 初始化握手
curl -X POST http://localhost:8061/messages \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'

# 2. 获取工具列表
curl -X POST http://localhost:8061/messages \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

# 3. 调用工具
curl -X POST http://localhost:8061/messages \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cmdb_server_query","arguments":{"ip":"192.168.7.101"}}}'
```

### 测试兼容层

```bash
# 健康检查
curl http://localhost:8061/health

# API 端点
curl -X POST http://localhost:8061/api/cmdb-server-query \
  -H "Content-Type: application/json" \
  -d '{"ip": "192.168.7.101"}'
```

## 部署

### 方式一：Docker Compose 部署（推荐）

```bash
# 1. 上传/解压部署包到服务器
cd /opt
mkdir -p cmdb-mcp-server && cd cmdb-mcp-server
tar xzf cmdb-mcp-server-*.tar.gz

# 2. 进入 deploy 目录，按需修改 .env
cd deploy
cp .env.example .env
# 编辑 .env：MCP_USE_MOCK=false, MCP_API_BASE_URL=...

# 3. 一键部署
chmod +x deploy-prod.sh
./deploy-prod.sh
```

### 方式二：Python 直接运行

```bash
cd cmdb-mcp-server

# 安装依赖
pip install -r requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple

# 启动（默认端口 8061）
./start.sh -d

# 或指定端口
./start.sh 0.0.0.0 8061
```

### 方式三：Systemd 部署

创建 `/etc/systemd/system/cmdb-mcp-server.service`:

```ini
[Unit]
Description=cmdb-mcp-server
After=network.target

[Service]
Type=simple
User=appuser
WorkingDirectory=/opt/cmdb-mcp-server
ExecStart=/usr/bin/python3 /opt/cmdb-mcp-server/scripts/mcp_server.py --transport http --port 8061
Restart=always
RestartSec=10
Environment=MCP_USE_MOCK=false

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable cmdb-mcp-server
sudo systemctl start cmdb-mcp-server
```

## 环境变量说明

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `MCP_USE_MOCK` | 否 | `false` | Mock 模式开关（API 失败时也会自动降级） |
| `MCP_API_BASE_URL` | 否 | `https://oss.tech.ctseelink.cn` | CMDB API 基础地址 |
| `LOG_LEVEL` | 否 | `INFO` | 日志级别：DEBUG/INFO/WARNING/ERROR |

## 配置文件

- `config/api_endpoints.json` - API 端点配置
- `config/field_mappings.json` - 响应字段映射配置
- `config/param_mappings.json` - 参数映射与路由规则
- `mock/mock_responses.json` - Mock 数据文件

## 目录结构

```
cmdb-mcp-server/
├── config/                # 配置
│   ├── api_endpoints.json
│   ├── field_mappings.json
│   └── param_mappings.json
├── mock/                  # Mock 数据
│   └── mock_responses.json
├── scripts/               # 主程序
│   ├── mcp_server.py
│   ├── mcp_logger.py
│   └── test_client.py
├── deploy/                # 部署文件
│   ├── deploy-prod.sh
│   └── docker-compose.yml
├── docs/                  # 文档
│   ├── README.md
│   └── DEPLOY.md
├── logs/                  # 日志目录（自动创建）
├── requirements.txt
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── .dockerignore
├── package.sh
└── start.sh
```

## 日志功能

### 日志架构

服务器采用分层日志设计，支持多种日志类型和输出目标：

| 日志类型 | 说明 | 输出目标 |
|---------|------|---------|
| 控制台日志 | 实时输出，简洁格式 | 标准输出 |
| 文件日志 | 详细记录，包含行号 | `logs/{date}/mcp_server.log` |
| 错误日志 | 仅记录错误级别 | `logs/{date}/mcp_server_error.log` |

### 日志存储策略

- **按日期存储**：日志文件按日期目录组织（如 `logs/2026-07-03/mcp_server.log`）
- **总大小控制**：日志总大小不超过 1GB，超过后自动删除最旧的日志文件
- **编码格式**：UTF-8

### 日志类型说明

| 日志标签 | 说明 | 包含字段 |
|---------|------|---------|
| `[TOOL_CALL]` | 工具调用记录 | tool_name, arguments, duration_ms, success, result_code |
| `[MCP_REQUEST]` | MCP 请求记录 | method, request_id, params |
| `[MCP_RESPONSE]` | MCP 响应记录 | method, request_id, success, error |
| `[API_REQUEST]` | API 请求记录 | skill_id, method, url, params |
| `[API_RESPONSE]` | API 响应记录 | skill_id, code, duration_ms, success |
| `[SSE_CONNECTION]` | SSE 连接记录 | client_id, action |
| `[MOCK_USAGE]` | Mock 使用记录 | skill_id, reason |

### 日志示例

```json
// 工具调用日志
[TOOL_CALL] {"timestamp": "2026-07-02T17:07:36.123", "type": "tool_call", "tool_name": "cmdb_server_query", "arguments": {"ip": "192.168.7.101"}, "duration_ms": 156.23, "success": true, "result_code": 200, "result_count": 4}

// MCP 请求日志
[MCP_REQUEST] {"timestamp": "2026-07-02T17:07:36.000", "type": "mcp_request", "method": "tools/call", "request_id": 1, "params": {"name": "cmdb_server_query"}}
```

### 查看日志

```bash
# 查看实时日志
tail -f logs/$(date +%Y-%m-%d)/mcp_server.log

# 查看错误日志
tail -f logs/$(date +%Y-%m-%d)/mcp_server_error.log

# 搜索特定工具的调用记录
grep "cmdb_server_query" logs/*/mcp_server.log

# 统计工具调用次数
grep -c "\[TOOL_CALL\]" logs/*/mcp_server.log
```

## 为什么需要 MCP 协议

### 传统方式 vs MCP 方式

| 特性 | 传统 HTTP API | MCP 协议 |
|------|-------------|---------|
| **Skill 代码复杂度** | 需要实现 HTTP 调用 | 只需调用工具名 |
| **工具发现** | 手动配置 | 自动发现 |
| **参数验证** | 需要自己实现 | Schema 自动验证 |
| **连接管理** | 每次调用新建连接 | 连接复用 |
| **错误处理** | 分散在各 Skill | 集中处理 |
| **降级机制** | 需要自己实现 | 服务器统一管理 |
| **监控审计** | 难以统一 | 集中管理 |
| **扩展性** | 改 API 需改所有 Skill | 只需更新 MCP 服务器 |
| **安全性** | API Key 分散 | 统一认证 |

## 常见问题

### Q1: MCP 服务器和 HTTP 服务器有什么区别？

**MCP 服务器**：
- 使用 MCP 协议（JSON-RPC over stdio/SSE）
- 需要支持 MCP 的客户端（如 StarAgent 平台）
- 工具自动发现，Schema 自动定义
- 适合 AI 平台集成

**HTTP 服务器**：
- 使用标准 HTTP REST API
- 可以被任何 HTTP 客户端调用
- 需要手动定义 API 文档
- 适合传统 Web 应用集成

我们的 `mcp_server.py` 同时支持两种模式！

### Q2: Skill 如何调用 MCP 工具？

Skill 不直接调用 MCP 服务器，而是通过 StarAgent 平台的工具调用接口：

```python
# Skill 代码
def my_skill_function():
    result = platform.call_tool(
        mcp_server="ops-data-query",
        tool_name="cmdb_server_query",
        arguments={"ip": "192.168.7.101"}
    )
    return result
```

### Q3: MCP 协议的数据格式是什么？

MCP 协议使用 JSON-RPC 2.0 格式：

```json
// 请求
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "cmdb_server_query",
    "arguments": {"ip": "192.168.7.101"}
  }
}

// 响应
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"code\":200,\"data\":{...}}"
      }
    ]
  }
}
```

## 故障排查

### 依赖安装失败

```bash
pip install -r requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple
```

### 端口被占用

```bash
lsof -i :8000
kill -9 <PID>
```

### API 请求失败

服务器会自动降级到 Mock 数据，检查日志：

```bash
tail -f logs/$(date +%Y-%m-%d)/mcp_server.log
```

## 许可证

MIT License