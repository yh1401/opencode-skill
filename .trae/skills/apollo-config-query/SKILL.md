---
name: Apollo配置查询助手
description: 查询 Apollo 配置中心的配置数据（应用配置/Namespace/配置项/应用列表），通过 MCP 工具与 Apollo 交互。当用户询问 Apollo、配置、appid、namespace、配置项 key、应用列表等信息时调用。
version: 3.2.0
author: Skill Agent Team
---

# Apollo 配置查询助手

## 技能概述

本技能通过 **MCP 工具** 与 Apollo 配置中心交互，提供配置查询、Apollo 服务实例列表、应用列表三大能力。

| 技能 ID | 功能描述 | 状态 | 触发关键词 |
|---------|----------|------|------------|
| apollo-config-search | Apollo 配置查询/Apollo服务(套)列表/应用列表 | ✅ 可用 | Apollo、配置、appid、namespace、配置项、key、应用列表、Apollo服务、Apollo套 |

---

## 路由规则

当用户输入包含「Apollo/配置/appid/namespace/配置项/key/应用列表」等关键词且意图为查询 Apollo 信息时，匹配 `apollo-config-search`。

**排他条件**：
- 如果用户明确提到"服务器"、"主机"、"机房"，应触发 `cmdb-server-query` 技能
- 如果用户明确提到"部署"、"发布记录"，应触发 `project-deployment-query` 技能

**业务处理**：参数映射、MCP 工具调用、输出格式化等业务逻辑由子技能 [apollo-config-search/SKILL.md](skills/apollo-config-search/SKILL.md) 负责。

**MCP 服务**：`apollo-mcp-server` 提供 3 个工具：`apollo_config_query`、`apollo_host_list`（并发探活标记每套 available）、`apollo_app_list`

### Apollo 服务实例（多套）与应用环境（ENV）的区别

> **术语总纲**（两个概念务必区分）：
> - **apolloHostId** = **Apollo 服务实例**（即"哪一套" Apollo，按产品线/地域独立部署多套，如"贵州"、"广州4"、"P2P"）→ 查询链路第一层，决定连哪套 Apollo
> - **env** = **应用环境**，取值 PRO（生产）、DEV（开发）、SIT（集成测试）、FAT（测试）、UAT（预发）等 → 查询链路第二层，决定该套内的哪个环境
> - 一句话：**apolloHostId 决定"哪一套服务"，env 决定"哪个环境"。**

> **术语判定规则**（消除"环境"歧义，务必遵守）：
> - 用户提到 **开发/测试/生产/预发/集成** 或 **DEV/FAT/UAT/PRO/SIT** 等**环境名关键词** → 指 **应用环境 env**
> - 其他提到"环境"的表述（如"贵州环境"、"有哪些环境"、"广州4那套"）→ 指 **Apollo 服务实例**（apolloHostId）
> - 一句话：**"环境"不带环境名关键词 = 哪一套 Apollo；带环境名关键词 = env。**
> - 边界情况：同时出现地名+环境名（如"广州4生产"）→ 地名→套(apolloHostId)、环境名→env，两者都生效

第三方接口会返回多套 Apollo 服务实例（各产品线/地域独立部署）。`apollo_host_list` 工具**每次调用实时查询**所有可用实例，供展示与确认：

1. **实时查询**：用户想看有哪些 Apollo 服务实例时，调用 `apollo_host_list`（实时获取，不读缓存），返回每条记录的 `apolloHostId`
2. **完整查询链路**：配置查询/应用列表通过 `apolloHostId` 指定任意一套 Apollo，链路为：**哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)**
3. **默认兜底**：未指定 `apolloHostId` 时，`apollo_app_list` 自动遍历可用套取第一个可用套，`apollo_config_query` 自动遍历可用套定位应用所在套（结果均带 `usedHostId`/`hostName` 标注）

---

## 敏感信息脱敏（必做）

部分配置包含敏感信息（数据库密码、Redis 密码、SDK 密钥、AES 密钥等），**向用户展示配置值时必须脱敏**。

### 敏感字段判定

配置 Key（忽略大小写）包含以下关键词之一 → 视为敏感字段：

| 类别 | 关键词 |
|------|--------|
| 密码类 | `password`、`passwd`、`pwd`、`密码` |
| 密钥类 | `secret`、`secret_key`、`私钥`、`密钥`、`credential`、`凭据` |
| Token 类 | `token`、`api_key`、`apikey`、`access_key`、`accesskey`、`session_key`、`sign_key` |
| 加密类 | `aes_key`、`aeskey`、`encrypt`、`加密`、`证书`、`cert`、`iv` |

### 掩码规则

- 敏感字段的值**统一显示为 `******`**（不展示任何明文，包括前后几位）
- 非敏感字段正常展示
- 输出末尾追加一行：`（已对 {N} 个敏感字段脱敏，如需查看明文请联系管理员确认）`

### 输出示例

```
| 配置 Key | 值 |
|----------|----|
| spring.datasource.password | ****** |
| redis.password | ****** |
| sdk.api.secret | ****** |
| server.port | 8080 |
| app.timeout | 5000 |

（已对 3 个敏感字段脱敏，如需查看明文请联系管理员确认）
```

> 注意：脱敏是硬性规则，即使用户明确要求"显示明文"，也必须先脱敏展示，并提示安全风险。

---

## 输出格式

### 标准输出（配置列表模式 - 默认）

```markdown
## Apollo 配置查询结果

**查询条件**：{用户原始查询}

**应用**：{appId} | **环境**：{env} | **集群**：{clusterName} | **Namespace**：{namespaceName}

---

**结果摘要**：共查询到 N 个配置项（含 M 个敏感字段，已脱敏）

---

| 配置 Key | 值 |
|----------|----|
| key1     | val1 |
| key2     | val2 |
| db.password | ****** |

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
| "切换到 {DEV/FAT/UAT/PRO/SIT} 环境" | 更新应用环境(env)，保持其他参数 |

**重置触发**：用户使用"新的/重新/重置"等关键词，或输入全新查询条件时重置上下文。

---

## 交互流程

### 核心原则

1. **自顶向下递进**：查询链路是层级结构（哪套 Apollo → 哪个应用 → 具体配置），引导也按层级自顶向下——先确定哪套，再确定哪个应用，最后查配置
2. **缺哪层补哪层**：用户已提供的层不重复问；缺哪层就调对应工具拿真实数据引导选择，每层用上层数据（如查 app 时带上层 apolloHostId）
3. **用数据反问**：缺参数时不空口问，先调工具拿真实数据（host_list/app_list），再让用户在真实数据中选
4. **能查就不问**：参数足以查询时直接查，结果再优化（如只给 appId 也能查，apollo_config_query 会自动遍历定位 host）

### 参数收集优先级（按数据层级自顶向下）

数据层级：**Apollo 实例(host) → 应用(app) → 配置内容(config)**。当用户输入缺少多个参数时，按层级自顶向下逐级补齐：

```
层级 1：哪套 Apollo（apolloHostId）→ 缺时调 apollo_host_list 展示可用套（含探活标记 available），引导用户选择
层级 2：哪个应用（appId）→ 缺时调 apollo_app_list（带上层 apolloHostId）展示应用列表，引导用户选择
层级 3：环境/集群/Namespace → 有默认值(PRO/default/application)，不急着问；用户提到开发/测试等环境名时按映射传 env
层级 4：具体配置 Key → 结果多时才需要收窄
```

> 关键改进：原来"缺 appId 直接出 app 列表（用默认套）"会跳过顶层 host 引导；现在**缺 host 先出 host 列表，缺 app 再出 app 列表（带上层 hostId）**，自顶向下，每层用上层数据，查询更精准更快。
> 但保留"能查就不问"：用户只说"查 rule-engine 配置"（有 appId）时，不强制先问 host，apollo_config_query 会自动遍历定位应用所在套。

### 交互场景速查表

| 场景 | 触发条件 | 处理方式 | 示例话术 |
|------|---------|----------|---------|
| **查看 Apollo 服务实例** | 用户想看有哪些 Apollo 服务实例/哪一套 | 调用 apollo_host_list 实时查询+探活展示 | [话术 0](#0-apollo-服务实例列表多套) |
| **完全无上下文** | 用户只说"查配置"，无任何参数 | 自顶引导：先出 host 列表(层级1) → 用户选套 → 再出 app 列表(层级2) → 用户选应用 → 查配置 | [话术 1](#1-缺少应用-id) |
| **缺少哪套(host)但指定了应用** | 用户给了 appId 但没说哪套 | 直接查配置（apollo_config_query 自动遍历定位 host，结果带 usedHostId）；也可先出 host 列表让用户选套使查询更精准 | [话术 3](#3-范围过大-仅指定应用) |
| **缺少应用(appId)** | 未指定 appId 且上下文无继承 | 调 apollo_app_list（带 apolloHostId）出应用列表引导选择 | [话术 1](#1-缺少应用-id) |
| **应用模糊匹配** | 用户描述匹配到多个应用 | 列出候选 | [话术 2](#2-应用名模糊匹配到多个) |
| **范围过大** | 用户只说了 appId，没具体查什么 | 查默认 namespace 配置，结果多时提示收窄 | [话术 3](#3-范围过大-仅指定应用) |
| **查询模式不明** | 无法判断查配置/应用列表 | 默认 config，提示其他模式 | [话术 4](#4-查询模式不确定) |
| **无查询结果** | API 返回空或 404 | 提示检查条件 | [话术 5](#5-无查询结果) |
| **结果过多** | 配置项 > 15 条 | 提示搜索收窄 | [话术 6](#6-结果过多) |
| **API 失败** | 服务不可用 | 直接报错，不用模拟数据 | [话术 7](#7-api-调用失败) |

### 交互话术

#### 0. Apollo 服务实例列表（多套）

```
已为您实时查询并探活当前 N 套 Apollo 服务实例（多套，按产品线/地域划分），其中 M 套可用：

| 序号 | 服务名称 | 地址 | Token | 是否可用 | 是否默认 |
|------|---------|------|-------|---------|---------|
| 1 | 天翼云眼贵州测试Apollo-亿讯专用 | https://... | 有 | ✅ 可用 | 是 |
| 2 | 天翼云眼广州4多AZ生产Apollo | https://... | 有 | ❌ 不可用 | 否 |
| 3 | 3.0 P2P 易联家Apollo | https://... | 有 | ✅ 可用 | 否 |
...

请告诉我要查询哪一套（如"贵州"、"广州4"、"P2P"），
我将把对应的 apolloHostId 带入后续的配置/应用查询；
建议优先选择"✅ 可用"的套（不可用套即使指定也可能超时失败）；
或直接说"默认"，使用默认 Apollo 继续。

（提示：这里的"环境"指哪一套 Apollo 服务实例；如果您说的是生产/DEV 等应用环境，直接说环境名即可）
```

#### 1. 缺少应用 ID（先出列表再选择）

先调用 `apollo_app_list`（不传 apolloHostId，工具自动遍历可用套并标注 usedHostId），把真实应用列表展示给用户选择：

```
已为您加载可用的应用列表（Apollo 套: {usedHostId} - {hostName}）：

| 序号 | AppId | 应用名称 | 部门 |
|------|-------|----------|------|
| 1 | rule-engine | 规则引擎 | 技术中台 |
| 2 | config-service | 配置管理服务 | 技术中台 |
...

请回复序号、AppId 或应用名称，我将查询该应用的配置。
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
- "切换到 {DEV/FAT/UAT} 环境" - 切换应用环境(env)
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
- 若未指定 Apollo 套，可尝试不传 apolloHostId 重新查询（工具会自动遍历所有可用套定位应用所在套）

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

### 执行决策树（自顶向下，按数据层级递进）

```
用户输入
  │
  ├─ 判定查询意图：查服务实例 / 查应用列表 / 查配置？
  │    ├─ 查服务实例（"有哪些 Apollo"/"哪套"）→ 调 apollo_host_list（探活展示）→ 输出[话术0]
  │    ├─ 查应用列表（"有哪些应用"）→ 调 apollo_app_list（结果带 usedHostId）→ 输出应用列表
  │    └─ 查配置 → 进入下方层级递进 ↓
  │
  └─ 查配置：按层级自顶向下补齐缺失参数
       │
       ├─ 层级 1：是否指定哪套 Apollo（apolloHostId）？
       │    ├─ 否 → 调 apollo_host_list 展示可用套（含探活 available 标记）→ 输出[话术0]
       │    │       → 用户选套获得 apolloHostId，或说"默认"用默认套
       │    └─ 是 → 继续
       │
       ├─ 层级 2：是否指定哪个应用（appId）？
       │    ├─ 否 → 调 apollo_app_list（带上层 apolloHostId）展示应用列表 → 输出[话术1]
       │    │       → 用户选应用获得 appId
       │    │    （若 appId 模糊匹配多个 → 输出[话术2]列候选）
       │    └─ 是 → 继续
       │
       ├─ 层级 3：env/namespace 有默认值(PRO/application)，直接用
       │         用户提到开发/测试/预发等环境名 → 按映射传 env
       │
       ├─ 层级 4：具体 key？有则过滤，无则查全部
       │
       └─ 调用 apollo_config_query
            ├─ 用户未指定 apolloHostId 时 → 工具自动遍历定位应用所在套（结果带 usedHostId/hostName）
            ├─ 成功 → 检查结果数量
            │    ├─ 结果 = 0 → 输出[话术5]
            │    ├─ 结果 > 15 → 输出配置 + [话术6]
            │    └─ 结果 ≤ 15 → 输出配置 + 提示[话术3]
            └─ 失败 → 输出[话术7]
```

> 设计要点：层级 1/2 是"缺哪层补哪层"——用户已提供的层不重复问；但"能查就不问"优先——若用户只给了 appId（没指定 host），可直接跳到层级 3/4 调 apollo_config_query（工具自动遍历定位 host），不必强制走完层级 1。只有用户既没 host 也没明确 app 时，才自顶逐层引导。

---

## MCP 服务部署说明

本技能依赖 `apollo-mcp-server` 提供 MCP 工具服务。

### 服务地址

```
http://localhost:8062/mcp
```

### 启动方式

```bash
# 进入 MCP 服务目录
cd apollo-mcp-server

# 启动服务（默认端口 8062）
./start.sh

# 启用 Mock 模式（无需 Apollo 服务即可测试）
MCP_USE_MOCK=true ./start.sh
```

### 生产环境部署

```bash
# 1. 上传安装包 apollo-mcp-server-*.tar.gz 到服务器并解压
cd deploy && chmod +x deploy-prod.sh && ./deploy-prod.sh
```

### 地址与 Token 获取（自动）

MCP 启动时通过第三方接口 `GET {api_base}/thirdApi/getApolloHostInfo`（Cookie 携带 sessionId）自动获取 Apollo 地址并解密 Token，**无需手动配置**。

需要覆盖时，在 `deploy/.env` 中配置：

```ini
# 第三方接口（推荐，默认值已内置在代码中）
# APOLLO_HOST_API_BASE=https://easyops.tech.ctseelink.cn
# APOLLO_HOST_SESSION_ID=<32位sessionId>
```

### API 文档

仅本地调试访问：`http://localhost:8062/docs`（生产环境经网关部署，不暴露该端点）

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
│           ├── apollo-hosts.md           # Apollo 服务实例（多套）关键词映射 → apolloHostId
│           ├── apollo-param-guide.md     # 参数转换完整指南（唯一权威来源）
│           ├── app-options.md            # 应用可选值列表
│           └── namespace-options.md      # Namespace 可选值列表
```

---

## 版本信息

- **版本**: 3.2.0
- **可用技能**: 1 个（apollo-config-search）
- **MCP 工具**: 3 个（apollo_config_query, apollo_host_list, apollo_app_list）
- **查询链路**: 哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)
- **架构**: MCP 工具调用（apollo-mcp-server v3.2.0，EasyOps 代理接口）
- **设计模式**: 渐进式披露 + 自动定位（apollo_app_list 缺省时自动遍历可用套；apollo_config_query 缺省时自动遍历定位应用所在套，结果标注 usedHostId，支持"只传 appId 也能查到配置"）
