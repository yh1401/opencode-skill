---
name: Apollo配置查询助手
description: 查询 Apollo 配置中心的配置数据（应用配置/Namespace/配置项/应用列表），通过 MCP 工具与 Apollo 交互。当用户询问 Apollo、配置、appid、namespace、配置项 key、应用列表等信息时调用。
version: 2.0.0
author: Skill Agent Team
---

# Apollo 配置查询助手

## 技能概述

本技能通过 **MCP 工具** 与 Apollo 配置中心交互，提供配置查询、应用列表两大能力。

| 技能 ID | 功能描述 | 状态 | 触发关键词 |
|---------|----------|------|------------|
| apollo-config-search | Apollo 配置查询/应用列表 | ✅ 可用 | Apollo、配置、appid、namespace、配置项、key、应用列表 |

---

## 路由规则

当用户输入包含「Apollo/配置/appid/namespace/配置项/key/应用列表」等关键词且意图为查询 Apollo 信息时，匹配 `apollo-config-search`。

**排他条件**：
- 如果用户明确提到"服务器"、"主机"、"机房"，应触发 `cmdb-server-query` 技能
- 如果用户明确提到"部署"、"发布记录"，应触发 `project-deployment-query` 技能

**业务处理**：参数映射、MCP 工具调用、输出格式化等业务逻辑由子技能 [apollo-config-search/SKILL.md](skills/apollo-config-search/SKILL.md) 负责。

**MCP 服务**：`apollo-mcp-server` 提供 2 个工具：`apollo_config_query`、`apollo_app_list`

---

## 输出格式

### 标准输出（配置列表模式 - 默认）

```markdown
## Apollo 配置查询结果

**查询条件**：{用户原始查询}

**应用**：{appId} | **环境**：{env} | **集群**：{clusterName} | **Namespace**：{namespaceName}

---

**结果摘要**：共查询到 N 个配置项

---

| 配置 Key | 值 | 说明 |
|----------|----|------|
| key1     | val1 | comment1 |
| key2     | val2 | comment2 |

---
💡 您可以说："搜索 timeout"按关键词过滤，或"查看应用列表"查看所有应用
```

---

## 上下文规则

多轮对话中保留以下参数（有效期 5 分钟）：
- 应用 ID（appId）
- 环境（env）
- 集群名称（clusterName）
- Namespace 名称（namespaceName）

**继承规则**：用户未明确指定参数时自动继承上下文。

| 用户输入 | 处理逻辑 |
|----------|----------|
| "搜索 {keyword}" | 保持当前查询条件，按 keyword 过滤配置项 |
| "查看 {appId} 的配置" | 更新 appId，保持其他参数 |
| "切换到 {env} 环境" | 更新 env，保持其他参数 |

**重置触发**：用户使用"新的/重新/重置"等关键词，或输入全新查询条件时重置上下文。

---

## 交互流程

### 核心原则

1. **先问再查**：参数不明确时先反问澄清，避免盲目调用 API
2. **逐步收窄**：优先确定 appId，再逐步明确 env/namespace/key
3. **能查就不问**：如果参数足以执行查询，先查再说，结果再优化

### 参数收集优先级

当用户输入缺少多个参数时，按以下优先级逐级询问：

```
优先级 1：应用（appId）→ 缺少时先问，这是查询的前提
优先级 2：查询内容（queryMode）→ 不确定时默认查配置列表
优先级 3：环境/集群/Namespace → 有默认值，不急着问
优先级 4：具体配置 Key → 结果多时才需要收窄
```

### 交互场景速查表

| 场景 | 触发条件 | 处理方式 | 示例话术 |
|------|---------|----------|---------|
| **完全无上下文** | 用户只说"查配置"，无任何参数 | 先问查哪个应用 | "请问您想查哪个应用的配置？" |
| **缺少应用** | 未指定 appId 且上下文无继承 | 反问用户 | [话术 1](#1-缺少应用-id) |
| **应用模糊匹配** | 用户描述匹配到多个应用 | 列出候选 | [话术 2](#2-应用名模糊匹配到多个) |
| **范围过大** | 用户只说了 appId，没具体查什么 | 查默认 namespace 配置，结果多时提示收窄 | [话术 3](#3-范围过大-仅指定应用) |
| **查询模式不明** | 无法判断查配置/应用列表 | 默认 config，提示其他模式 | [话术 4](#4-查询模式不确定) |
| **无查询结果** | API 返回空或 404 | 提示检查条件 | [话术 5](#5-无查询结果) |
| **结果过多** | 配置项 > 15 条 | 提示搜索收窄 | [话术 6](#6-结果过多) |
| **API 失败** | 服务不可用 | 直接报错，不用模拟数据 | [话术 7](#7-api-调用失败) |

### 交互话术

#### 1. 缺少应用 ID

```
请问您想查询哪个应用的配置？

您可以说：
- 应用的名称，如"SampleApp"、"规则引擎平台"
- 应用的 AppId，如"SampleApp"、"rule-engine"
- 或者输入"应用列表"查看所有可用应用
```

#### 2. 应用名模糊匹配到多个

```
您说的"xxx"匹配到多个应用，请确认是哪个：

| 序号 | 应用名称 | AppId | 部门 |
|------|----------|-------|------|
| 1 | 规则引擎平台 | rule-engine | 技术中台 |
| 2 | 配置管理服务 | config-service | 技术中台 |

请回复序号或完整的应用名称。
```

#### 3. 范围过大（仅指定应用）

```
已为您查询 {appId} 的默认配置（Namespace: application，环境: PRO），
共找到 N 个配置项。

您可以说：
- "查看 {namespace} 配置" - 切换到指定 Namespace
- "搜索 {keyword}" - 按关键词过滤
- "切换到 {DEV/FAT/UAT} 环境" - 切换环境
```

#### 4. 查询模式不确定

```
您是想查看配置内容还是应用列表？

您可以说：
- "查看配置" - 获取当前配置项列表
- "应用列表" - 查看所有应用
```

#### 5. 无查询结果

```
## 查询结果

未查询到符合条件的配置项。

建议检查：
- 应用名称是否正确？可输入"应用列表"查看
- 环境是否正确？当前使用：{env}
- Namespace 名称是否正确？（常见：application、datasource、redis）

您可以直接补充修改某个条件，例如："切换到生产环境"或"查看 datasource 配置"
```

#### 6. 结果过多

```
**提示**：共查询到 N 个配置项（较多），您可以使用关键词或切换 Namespace 缩小范围：

- "搜索 {keyword}" - 按配置 Key 过滤（如"搜索 timeout"）
- "查看 {namespace} 配置" - 切换到指定 Namespace（如"查看 datasource 配置"）
```

#### 7. API 调用失败

```
## 查询失败

**错误类型**：API 调用失败
**错误原因**：Apollo 服务暂不可用

**建议**：请稍后重试，如问题持续请联系管理员。
```

### 执行决策树

```
用户输入
  │
  ├─ 检查: appId 是否明确？
  │    ├─ 否 → 输出[话术1]，等待用户补充
  │    └─ 是 → 继续
  │
  ├─ 检查: queryMode 是否明确？
  │    ├─ 否 → 默认 "config"，输出中提示[话术4]
  │    └─ 是 → 继续
  │
  ├─ 如果 queryMode = "apps"
  │    └─ 直接调用 API 返回应用列表
  │
  ├─ 如果 queryMode = "config"
  │    ├─ env/hamespace 有默认值，直接用
  │    └─ 调用 API
  │         ├─ 成功 → 检查结果数量
  │         │    ├─ 结果 = 0 → 输出[话术5]
  │         │    ├─ 结果 > 15 → 输出配置 + [话术6]
  │         │    └─ 结果 ≤ 15 → 输出配置 + 提示[话术3]
  │         └─ 失败 → 输出[话术7]
  │
  └─ 输出结果，附带后续可操作的提示
```

---

## MCP 服务部署说明

本技能依赖 `apollo-mcp-server` 提供 MCP 工具服务。

### 服务地址

```
http://180.101.21.13:8080/gateway/apollo/mcp
```

> 打包时按环境自动写入 `MCP_ENDPOINT`（生产走网关，本地测试走 `http://localhost:8062/mcp`），无需修改本文件。

### 启动方式

```bash
# 进入 MCP 服务目录
cd apollo-mcp-server

# 启动服务（默认端口 8062）
./start.sh

# 启用 Mock 模式（无需 Apollo 服务即可测试）
MCP_USE_MOCK=true ./start.sh
```

### 配置 Apollo 地址

编辑 `apollo-mcp-server/config/api_endpoints.json`：

```json
{
  "base_url": "http://your-apollo-host:8080",
  "env_overrides": {
    "base_url": "APOLLO_HOST"
  }
}
```

或通过环境变量覆盖：
```bash
APOLLO_HOST=http://your-apollo-host:8080 ./start.sh
```

### 配置 OpenAPI Token

编辑 `apollo-mcp-server/config/auth.json`：

```json
{
  "openapi_token": "your-apollo-openapi-token"
}
```

> ConfigService (端口 8080) 无需 Token，OpenAPI (端口 8070) 查询配置和应用列表需要 Token。

### API 文档

启动后访问：`http://180.101.21.13:8080/gateway/apollo/docs`

---

## 目录结构

```
apollo-config-query/
├── SKILL.md                              # 主技能入口（本文件）
├── registry/
│   └── skills.json                       # 技能注册中心
├── skills/
│   └── apollo-config-search/
│       ├── SKILL.md                      # Apollo 配置查询工具（含完整工具定义）
│       └── references/
│           ├── app-options.md            # 应用可选值列表
│           └── namespace-options.md      # Namespace 可选值列表
└── _dev/                                 # 开发/打包相关（不参与打包）
    └── package.sh                        # 打包脚本
```

---

## 版本信息

- **版本**: 2.0.0
- **可用技能**: 1 个（apollo-config-search）
- **MCP 工具**: 2 个（apollo_config_query, apollo_app_list）
- **架构**: MCP 工具调用（apollo-mcp-server）
- **设计模式**: 渐进式披露
