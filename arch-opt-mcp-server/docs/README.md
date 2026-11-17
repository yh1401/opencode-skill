# arch-opt-mcp-server

架构优化智能体 MCP 服务器。采集副本可用性、容量瓶颈、负载分布、OOM、服务依赖、宿主机故障域等维度数据，由**规则引擎做确定性数值判定**，为上层 skill 与大模型提供带证据、可追溯的四态结果。

**版本**：v2.0.0
**协议**：MCP（JSON-RPC 2.0，Streamable HTTP + SSE）
**端口**：8062

---

## 一、什么是 MCP

MCP (Model Context Protocol) 是一个开放协议，用于标准化应用程序如何为 LLM 提供上下文，相当于 AI 应用的 "USB-C" 接口。本服务将架构巡检的数据采集与规则判定能力封装为标准 MCP 工具，供 Agent 平台自动发现与调用。

### 核心概念

```
┌─────────────────────────────────────────────────────────────┐
│                       LLM / AI 模型                          │
└─────────────────────────────────────────────────────────────┘
                          │ 需要访问外部工具/数据
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                    Agent 平台（MCP 客户端）                   │
└─────────────────────────────────────────────────────────────┘
                          │ MCP 协议通信
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                 arch-opt-mcp-server                          │
│  Tools：mcp_module_topology / mcp_replica / mcp_capacity ... │
│  规则引擎：四态判定 + 阈值 + 证据留存                          │
└─────────────────────────────────────────────────────────────┘
                          │ 实际数据访问
                          ▼
┌──────────────────┬──────────────────────────────────────────┐
│  统一拓扑代理     │  Prometheus                              │
│ （模块/节点/项目）│ （容器 CPU/内存/副本/OOM 指标）           │
└──────────────────┴──────────────────────────────────────────┘
```

---

## 二、职责划分

本服务是「薄指令层 + MCP Server」分离架构中的重执行侧。**数值计算与阈值判定全部在此完成，大模型只负责解释与建议。**

| 职责 | 薄指令层 (Skill) | 本 MCP Server |
|------|------------------|---------------|
| 触发识别、工具编排、Prompt 组装 | ✅ | ❌ |
| 数据采集（拓扑 / Prometheus） | ❌ | ✅ |
| 数值计算与阈值判定 | ❌ | ✅ |
| 四态判定与证据留存 | ❌ | ✅ |
| 超时/重试/Mock 降级 | ❌ | ✅ |
| 报告生成与建议 | ✅（大模型） | ❌ |

---

## 三、数据源

| 数据源 | 用途 | 调用方式 |
|--------|------|----------|
| **统一拓扑代理** | 模块/节点/项目/宿主机/链路业务数据 | POST 网关，透传目标业务接口 |
| **Prometheus** | 容器运行指标 | GET `/api/v1/query?query=<PromQL>` |

- 代理网关：`https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince`
- Prometheus：`http://183.56.210.90:8082`
- 持续类规则一律使用**时间窗口表达式**（如 `[15m]`），不使用单次即时值

> 每个维度的完整 PromQL 与接口取值路径见 [数据查询对照表.md](../数据查询对照表.md)。

---

## 四、特性

- ✅ **标准 MCP 协议**：支持 `initialize` / `tools/list` / `tools/call`
- ✅ **多传输模式**：`POST /mcp`（单一入口）、`GET /sse` + `POST /messages`（SSE）
- ✅ **平台兼容层**：`/tools/list`、`/tools/call`、`/api/*` 端点
- ✅ **四态可追溯**：每条结论带 `status` + 证据 + `collected_at` + `rule_version`
- ✅ **阈值外部化**：口径调整只改 `config/thresholds.json`
- ✅ **超时重试**：统一封装超时（默认 8s）与重试（默认 1 次）
- ✅ **Mock 降级**：接口不可用时降级到 `mock/` 数据（受 `MCP_USE_MOCK` 控制）
- ✅ **数据缺口显式化**：缺失数据返回 🟨，不误报「无风险」
- ✅ **工具白名单**：`MCP_ENABLED_TOOLS` 控制启用哪些工具
- ✅ **Swagger 文档**：HTTP 模式提供 `/docs`

---

## 五、快速开始

### 1. 安装依赖

```bash
pip install -r requirements.txt
```

### 2. 启动服务

```bash
# 方式一：启动脚本（默认 0.0.0.0:8062）
./start.sh

# 方式二：后台启动
./start.sh -d

# 方式三：直接运行
python3 scripts/mcp_server.py --transport http --host 0.0.0.0 --port 8062
```

### 3. 验证

```bash
curl http://localhost:8062/health
# {"status":"ok","time":"...","mock":false}

curl http://localhost:8062/     # 服务信息 + 工具列表
curl http://localhost:8062/docs # Swagger 文档
```

---

## 六、MCP 协议接口

### 6.1 端点一览

#### 标准 MCP 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/mcp` | POST | **标准单一入口**，所有 MCP 协议消息（Streamable HTTP） |
| `/sse` | GET | SSE 连接端点，接收服务器推送 |
| `/messages` | POST | MCP 消息端点（SSE 模式配套） |

#### 平台兼容端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/tools/list` | POST/GET | 工具发现 |
| `/tools/call` | POST | 工具调用 |
| `/tools` | GET | 工具兼容端点 |

#### 兼容层 / 基础端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/mcp_module_topology` 等 | POST | 10 个工具的一一映射（`/api/<工具名>`） |
| `/api/config` | GET | 返回当前阈值配置与规则版本 |
| `/health` | GET | 健康检查（含 mock 状态） |
| `/` | GET | 服务信息首页 |
| `/docs` | GET | Swagger 文档 |

### 6.2 协议示例

**initialize**
```json
// 请求
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
// 响应
{"jsonrpc":"2.0","id":1,"result":{"name":"arch-opt-mcp-server","version":"2.0.0","capabilities":{"tools/list":{},"tools/call":{}}}}
```

**tools/list**
```json
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

**tools/call**
```json
{
  "jsonrpc": "2.0", "id": 3,
  "method": "tools/call",
  "params": { "name": "mcp_replica", "arguments": { "node": "云公司->贵州" } }
}
```

---

## 七、工具列表

| 工具名 | 维度 | 说明 |
|--------|------|------|
| `mcp_module_topology` | 入口 | 模块归属与应用解析（产出 `deploy_node`、`-dep` 应用） |
| `mcp_replica` | D1 | 副本数量与可用性 |
| `mcp_capacity` | D2 | CPU 限流 / 内存 / HPA |
| `mcp_load_balance` | D3 | Pod 负载不均衡 |
| `mcp_oom` | D4 | 近期 OOMKilled |
| `mcp_link_dependency` | D5 | 链路依赖（Pinpoint 拓扑） |
| `mcp_node_list` | D6 辅助 | 省市节点列表 |
| `mcp_virtual_structure` | D6 辅助 | 物理宿主机 ↔ 云主机映射 |
| `mcp_topology_info` | D6 辅助 | 项目 ↔ 云主机部署关系 |
| `mcp_fault_domain` | D6 | 故障域集中度 |

### 参数说明

| 工具 | 参数 |
|------|------|
| `mcp_module_topology` | `node`（必填）、`product_name`（默认「天翼看家」）、`only_dep`（默认 true） |
| `mcp_replica` / `mcp_capacity` / `mcp_load_balance` | `node`（必填）、`deployments`（缺省自动派生 `-dep`）、`product_name` |
| `mcp_oom` | 同上 + `days`（近期窗口天数，默认取阈值） |
| `mcp_link_dependency` | `deploy_node`（必填）、`projects`、`minute`（默认 5） |
| `mcp_node_list` | `filter_prefix`（默认「云公司」） |
| `mcp_virtual_structure` | `ctyun_node_code3`（必填） |
| `mcp_topology_info` | `node`（必填） |
| `mcp_fault_domain` | `node`（必填）、`min_instances` |

---

## 八、结果四态

所有工具返回均带 `status` 与 `conclusion`，语义统一：

| 状态 | 标识 | 含义 |
|------|------|------|
| `risk` | 🟥 | 已发现风险（阈值命中，含证据） |
| `ok` | 🟩 | 未触发规则 |
| `insufficient` | 🟨 | 数据不足（接口失败 / 指标缺失 / 映射中断） |
| `not_applicable` | ⚪ | 不适用（无 HPA、非 `-dep` 应用等） |

返回体还包含 `collected_at`、`rule_version`、`counts`（四态计数）、`threshold`、`evidence` 等字段。

> 关键约束：数据缺失一律返回 `insufficient`，**不得**推断为「无风险」。

---

## 九、阈值配置

所有判定阈值集中在 [thresholds.json](../config/thresholds.json)（version 2.0.0）：

| 分组 | 关键项 | 默认值 | 状态 |
|------|--------|--------|------|
| `replica` | `min_replicas` | 2 | 已确认（至少 2 副本，2026-10-19） |
| `capacity` | `cpu_throttle_ratio` / `memory_usage_ratio` / `duration_minutes` | 0.20 / 0.85 / 15 | 已定稿 |
| `load_balance` | `max_avg_ratio` / `duration_minutes` | 2.0 / 15 | 已定稿 |
| `oom` | `lookback_days` | 7 | 待业务确认 |
| `link_dependency` | `error_rate_threshold` / `slow_call_threshold_ms` | null | 待联调制定 |
| `fault_domain` | `min_instance_to_check` | 1 | 已定稿 |
| `container_check` | `dep_suffix_filter` | true | 阶段一仅 `-dep` 应用 |

可通过 `GET /api/config` 在线查看当前生效配置。

---

## 十、环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `MCP_USE_MOCK` | 否 | `false` | Mock 降级开关 |
| `ARCH_PROXY_URL` | 否 | 拓扑代理网关 | 代理地址覆盖 |
| `ARCH_APP_ID` | 是（真实数据） | — | 代理凭据 appId |
| `ARCH_APP_SECRET` | 是（真实数据） | — | 代理凭据 appSecret |
| `ARCH_PROVINCE` | 否 | `大模型` | 代理省份标识 |
| `ARCH_PROMETHEUS_URL` | 否 | `http://183.56.210.90:8082` | Prometheus 地址 |
| `ARCH_REQUEST_TIMEOUT` | 否 | `8` | 请求超时（秒） |
| `ARCH_MAX_RETRIES` | 否 | `1` | 失败重试次数 |
| `ARCH_RETRY_DELAY` | 否 | `1` | 重试间隔（秒） |
| `MCP_ENABLED_TOOLS` | 否 | 全部 | 启用工具白名单（逗号分隔） |
| `LOG_LEVEL` | 否 | `INFO` | 日志级别 |
| `LOG_TO_FILE` | 否 | `true` | 是否写文件日志 |

> 敏感凭据请放入 `.env`（参考 `.env.example`），勿提交到版本库。

---

## 十一、目录结构

```
arch-opt-mcp-server/
├── config/
│   ├── datasources.json      # 拓扑代理/Prometheus/业务端点/中心节点映射
│   └── thresholds.json       # 判定阈值（唯一来源）
├── mock/
│   ├── mock_responses.json   # 业务接口 Mock 数据
│   └── prometheus_mock.json  # Prometheus 指标 Mock 数据
├── scripts/
│   ├── mcp_server.py         # 主程序（工具实现 + 规则引擎 + HTTP 层）
│   └── mcp_logger.py         # 统一日志组件
├── docs/
│   └── README.md             # 本文档
├── 数据查询对照表.md          # 各维度查询语句与判定口径
├── requirements.txt
├── start.sh
├── Dockerfile
├── docker-compose.yml
└── .env.example
```

---

## 十二、测试

```bash
# 以 Mock 模式启动
MCP_USE_MOCK=true python3 scripts/mcp_server.py --port 8062

# 健康检查
curl http://localhost:8062/health

# 工具列表
curl -s -X POST http://localhost:8062/tools/list -H 'Content-Type: application/json' -d '{}'

# 工具调用（JSON-RPC）
curl -s -X POST http://localhost:8062/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mcp_node_list","arguments":{}}}'

# 兼容层调用
curl -s -X POST http://localhost:8062/api/mcp_replica -H 'Content-Type: application/json' \
  -d '{"node":"云公司->贵州"}'
```

---

## 十三、部署

### Docker Compose（推荐）

```bash
docker compose up -d
# 服务监听 8062，含 healthcheck
```

### Python 直接运行

```bash
pip install -r requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple
./start.sh -d
```

---

## 十四、日志

日志组件 `mcp_logger.py` 提供分层日志：控制台日志 + 文件日志（按日期目录 `logs/{date}/`）。关键标签包括 `[TOOL_CALL]`、`[MCP_REQUEST]`、`[MCP_RESPONSE]`、`[API_REQUEST]`、`[API_RESPONSE]`、`[MOCK_USAGE]`、`[SSE_CONNECTION]`。

```bash
tail -f logs/$(date +%Y-%m-%d)/mcp_server.log
```

---

## 十五、故障排查

| 现象 | 排查方向 |
|------|----------|
| 端口被占用 | `lsof -i :8062` 后释放 PID |
| 接口全返回 🟨 | 检查 `ARCH_APP_ID`/`ARCH_APP_SECRET` 是否配置、Prometheus 是否可达 |
| 依赖安装失败 | 使用清华源 `-i https://pypi.tuna.tsinghua.edu.cn/simple` |
| 想快速联调 | 设 `MCP_USE_MOCK=true` 使用 `mock/` 数据 |

---

## 许可证

MIT License
