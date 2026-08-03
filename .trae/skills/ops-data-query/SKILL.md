---
name: ops-data-query
description: 企业 CMDB 运维数据综合查询技能。提供服务器查询、公网IP查询、部署记录查询、产品信息查询和工程项目基础信息查询五大功能，支持跨维度关联查询和智能路由。
version: 4.0.0
author: Skill Agent Team
---

# ops-data-query 运维数据查询技能（主入口）

## 功能概述

本技能是 CMDB 运维数据的统一查询入口，通过智能路由将用户请求分发到对应的子技能，同时支持**跨技能关联查询**。

### 五大子技能

| 子技能 | 工具名 | 主要查询维度 |
|--------|--------|------------|
| cmdb-server-query | `cmdb_server_query` | 服务器主机、机房、配置、状态 |
| server-public-ip-query | `server_public_ip_query` | 公网 IP、带宽、网络类型 |
| project-deployment-query | `project_deployment_query` | 部署记录、版本、环境、状态 |
| product-query | `product_query` | 产品信息、负责人、所属部门 |
| project-basis-query | `project_basis_query` | 工程项目、代码仓库、父项目 |

### 共享资源

| 文件 | 用途 | 归属 |
|------|------|------|
| `config/param_mappings.json` | 通用参数映射（机房、状态、环境等） | 子技能 |
| `references/output_templates.md` | 标准输出模板（所有子技能共用） | 子技能 |
| `references/error_scenarios.md` | 统一错误处理方案 | 子技能 |
| `references/param_guides.md` | 参数映射参考文档 | 子技能 |

> ⚠️ **注意**：API 端点配置、Mock 数据、字段映射已迁移至 MCP Server（`mcp-server/` 目录），由 MCP Server 统一管理。

---

## 智能路由规则

### 路由优先级

当用户请求涉及多个维度时，按以下优先级判断：

1. **server-public-ip-query** — 优先识别公网 IP 相关意图
2. **project-deployment-query** — 优先识别部署相关意图
3. **product-query** — 优先识别产品信息意图
4. **project-basis-query** — 识别项目和代码仓库意图
5. **cmdb-server-query** — 默认兜底（服务器信息）

### 意图冲突规则

当用户描述跨越多个技能时，使用以下规则判断：

| 用户意图 | 匹配技能 | 说明 |
|---------|---------|------|
| 产品的服务器 | cmdb-server-query | 按产品查服务器 |
| 项目的服务器 | cmdb-server-query | 按项目查服务器 |
| 服务器的公网IP | server-public-ip-query | 查服务器对应的公网IP |
| 服务器的部署 | project-deployment-query | 查服务器上的部署记录 |
| 产品的部署 | project-deployment-query | 按产品查部署 |
| 项目的部署 | project-deployment-query | 按项目查部署 |
| 产品的项目 | project-basis-query | 按产品查工程项目 |
| 项目的代码 | project-basis-query | 查项目的代码仓库 |

### 排他关键词

如果用户输入包含以下关键词，直接排除对应技能：

| 排除关键词 | 排除的技能 |
|-----------|-----------|
| 公网IP、外网、带宽 | cmdb-server-query |
| 部署、发布、上线 | cmdb-server-query、product-query、project-basis-query |
| SVN、GIT、代码 | cmdb-server-query、project-deployment-query、product-query |

---

## 跨技能关联查询

> 这是 v3.0 新增的核心能力，支持多步关联查询。

### 支持的联合查询场景

| 用户请求示例 | 执行流程 | 涉及技能 |
|------------|---------|---------|
| "贵州机房的规则引擎项目部署了哪些服务器" | 项目 → 关联服务器 → 查详情 | project-basis-query → cmdb-server-query |
| "查天翼看家产品的所有服务器状态" | 产品 → 关联项目 → 关联服务器 | product-query → cmdb-server-query |
| "某台服务器上部署了哪些项目" | 服务器 → 关联部署 | cmdb-server-query → project-deployment-query |
| "最近7天哪些项目部署失败了" | 时间范围 → 筛选失败 | project-deployment-query（直接筛选） |

### 联合查询执行策略

1. **分析用户意图**：识别需要几步查询
2. **确定执行顺序**：根据依赖关系排序（产品→项目→服务器→IP）
3. **逐步执行**：每个子技能独立调用
4. **智能合并**：将多次结果合并为统一输出
5. **上下文传递**：中间结果自动作为下一步的筛选条件

### 联合查询输出示例

```
## 查询结果：贵州机房规则引擎项目的服务器

### 执行摘要

共涉及 2 个查询步骤：
1. ✅ 项目查询：找到规则引擎平台关联的项目 `guizh-rules-api`
2. ✅ 服务器查询：找到 2 台服务器

---

### 服务器列表

| 主机名 | IP | 状态 | 公网IP |
|--------|-----|------|--------|
| gz-server-01 | 192.168.7.101 | 🟢 在线 | 113.12.13.14 |
| gz-server-02 | 192.168.7.102 | 🟢 在线 | 113.12.13.15 |

---

💡 您可以说：
- "查看详细信息" - 显示 CPU、内存、磁盘等完整配置
- "查这些服务器的公网IP" - 自动查公网 IP
- "查这些服务器的部署记录" - 自动查部署历史
```

---

## 反向查询能力

### 服务器相关反向查询

| 用户请求 | 执行方式 |
|---------|---------|
| "查 192.168.7.101 的服务器详情" | cmdb-server-query，按 IP 筛选 |
| "查 gz-server-01 在哪" | cmdb-server-query，按主机名筛选 |
| "这台服务器关联哪些产品" | cmdb-server-query → product-query（基于返回数据） |

### 部署相关反向查询

| 用户请求 | 执行方式 |
|---------|---------|
| "查最近失败的部署" | project-deployment-query，筛选 statusCode=1 |
| "查某台服务器上的最新部署" | cmdb-server-query（得项目名） → project-deployment-query |
| "查某项目的所有环境部署" | project-deployment-query，按 projectName 筛选 |

---

## 参数映射规则

### 通用规则（所有子技能共用）

> 详细映射见 `config/param_mappings.json`

| 参数 | 映射规则 |
|------|---------|
| 机房名 → node | 贵州→云公司->贵州，上海→省公司->上海 |
| 状态词 → state | 在线→0，库存→1，维修→3 |
| 环境词 → environment | 测试→1，灰度→2，生产→3 |
| 数量词 → pageSize | 一台→1，几台→15，所有→100 |

### 响应字段标准化

> 处理 API 响应时，使用 `config/field_mappings.json` 统一字段名

| 标准字段名 | 各 API 实际字段 |
|-----------|---------------|
| currentPage | currentPage（cmdb/deploy）或 current（pub-ip/product/basis） |
| pageSize | pageSize 或 size |
| total | total |
| records | records |

---

## 输出规范

### 标准输出格式

使用 `references/output_templates.md` 中的标准模板：

```
## {子技能名称}查询结果

**查询条件**：{用户原始输入}
**匹配技能**：{子技能名称}

---

**结果摘要**：共查询到 {N} 条记录，当前显示前 {M} 条

---

| 字段1 | 字段2 | ... |
|-------|-------|-----|
| 值1 | 值2 | ... |

---

💡 您可以说：
- "查看详细信息" - 显示完整字段
- "下一页" - 查看更多
- "查看全部" - 显示所有结果
```

### 状态格式化规则

| 状态值 | 格式化 |
|-------|--------|
| state=0 | 🟢 在线 |
| state=1 | 🟡 库存 |
| state=3 | 🔧 维修中 |
| state=4 | ⚫ 已报废 |
| statusCode=0 | ✅ 成功 |
| statusCode=1 | ❌ 失败（红色高亮） |
| statusCode=2 | 🔄 进行中 |
| duration | 330秒 → 5分30秒 |

### 错误处理

使用 `references/error_scenarios.md` 中的统一错误场景：

| 场景 | 处理 |
|------|------|
| 缺少参数 | 引导用户提供，列出可选值 |
| 参数格式错误 | 提示正确格式 |
| API 超时/失败 | 自动降级到 Mock 数据（`references/mock_responses.json`） |
| 无结果 | 返回空结果 + 智能引导 |

---

## 上下文管理

### 上下文保留规则

- **保留时间**：5 分钟（300 秒）
- **保留内容**：最近 3 次查询的条件和结果
- **重置条件**：用户说"新的"、"重新"、"重置"、"换一个"

### 上下文推断

当用户说"查更多"或"下一页"时：
1. 读取上下文中的最新查询条件
2. 将 `currentPage` + 1
3. 重新执行相同查询

当用户说"查其他机房"时：
1. 识别当前查询条件中的非变化部分（其他筛选项）
2. 替换机房参数
3. 重新执行

---

## 分页参数

| 用户指令 | 参数变化 |
|---------|---------|
| 默认 | currentPage=1，pageSize=10 |
| "更多"/"下一页" | currentPage + 1 |
| "上一页"/"返回" | currentPage - 1（最小 1） |
| "查看全部" | pageSize=100 |
| "只看一台" | pageSize=1 |

---

## 子技能索引

### 快速导航

| 技能 | 文件路径 | 主要用途 |
|------|---------|---------|
| cmdb-server-query | `skills/cmdb-server-query/SKILL.md` | 主机/机房/配置/状态 |
| server-public-ip-query | `skills/server-public-ip-query/SKILL.md` | 公网IP/带宽 |
| project-deployment-query | `skills/project-deployment-query/SKILL.md` | 部署记录/版本 |
| product-query | `skills/product-query/SKILL.md` | 产品/部门/负责人 |
| project-basis-query | `skills/project-basis-query/SKILL.md` | 工程/仓库/父项目 |

---

## MCP 服务集成

### 架构说明

本技能已改造为 **薄指令层 + MCP 服务** 的分离架构：

```
┌─────────────────────────────────────────────────────────────┐
│                     StarAgent 平台                          │
├─────────────────────────────────────────────────────────────┤
│  ops-data-query (主技能)                                    │
│  ├── cmdb-server-query          (薄指令层)                  │
│  │   - 触发条件、参数映射、输出格式化                         │
│  ├── server-public-ip-query     (薄指令层)                  │
│  ├── project-deployment-query   (薄指令层)                  │
│  ├── product-query              (薄指令层)                  │
│  └── project-basis-query        (薄指令层)                  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼ MCP 工具调用
┌─────────────────────────────────────────────────────────────┐
│                   MCP Server (独立服务)                      │
│  位置: mcp-server/scripts/mcp_server.py                     │
│  ├── cmdb_server_query          (数据获取)                  │
│  ├── server_public_ip_query     (数据获取)                  │
│  ├── project_deployment_query   (数据获取)                  │
│  ├── product_query              (数据获取)                  │
│  └── project_basis_query        (数据获取)                  │
│  ├── API 调用 + Mock 降级                                   │
│  └── 分页字段统一处理                                       │
└─────────────────────────────────────────────────────────────┘
```

### 职责划分

| 职责 | 子技能 (薄指令层) | MCP Server |
|------|------------------|------------|
| 触发条件识别 | ✅ | ❌ |
| 参数映射转换 | ✅ | ❌ |
| 输出格式化 | ✅ | ❌ |
| API 调用 | ❌ | ✅ |
| Mock 数据降级 | ❌ | ✅ |
| 分页字段统一 | ❌ | ✅ |
| 连接管理 | ❌ | ✅ |

### 服务端配置

**启动命令**：

```bash
cd mcp-server

# 安装依赖
pip install -r requirements.txt

# 启动 MCP 服务器 (HTTP 模式，供 StarAgent 注册)
python3 scripts/mcp_server.py --transport http --port 8000

# 或启动 MCP stdio 模式
python3 scripts/mcp_server.py --transport mcp
```

**默认端口**：8000

### 注册的工具列表

| 工具名 | 说明 |
|--------|------|
| `cmdb_server_query` | 查询 CMDB 服务器信息 |
| `server_public_ip_query` | 查询服务器公网 IP |
| `project_deployment_query` | 查询项目部署记录 |
| `product_query` | 查询产品信息 |
| `project_basis_query` | 查询工程项目基础信息 |

### StarAgent 平台配置

在 StarAgent 平台添加 MCP 连接：

```json
{
  "name": "ops-data-query",
  "type": "mcp",
  "url": "http://localhost:8000",
  "description": "企业 CMDB 运维数据综合查询"
}
```

### 工具调用示例

```json
{
  "name": "cmdb_server_query",
  "parameters": {
    "ip": "192.168.7.101",
    "currentPage": 1,
    "pageSize": 10
  }
}
```

### 特性

- ✅ **自动降级**：API 失败时自动使用 Mock 数据
- ✅ **连接复用**：使用 requests.Session 保持连接
- ✅ **标准 Schema**：自动生成工具描述和参数定义
- ✅ **超时控制**：30秒超时保护
- ✅ **分页统一**：自动处理不同 API 的分页字段差异
- ✅ **HTTP/MCP 双模式**：支持 HTTP API 和 MCP stdio 两种模式

---

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|---------|
| 4.0 | 2026-06-25 | 改造为薄指令层+MCP Server 分离架构，数据获取迁移至 MCP Server |
| 3.1 | 2026-06-25 | 新增 MCP 服务支持，提供标准化工具调用接口 |
| 3.0 | 2026-06-24 | 共享模板抽离，配置统一，跨技能查询能力，反向查询 |
| 2.0 | 2026-06-18 | 统一字段为英文 key，修复数据模型 |
| 1.0 | 2026-05-20 | 初始版本 |
