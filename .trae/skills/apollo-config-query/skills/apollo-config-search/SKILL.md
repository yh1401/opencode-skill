---
name: apollo-config-search
description: 查询 Apollo 配置中心的配置信息，支持配置项查询和应用列表查询
version: 3.2.0
author: Skill Agent Team
---

# Apollo 配置查询技能

## 功能描述

本技能通过 **MCP 工具** 与 Apollo 配置中心进行交互，提供以下能力：

1.  **配置查询** (`apollo_config_query`)：查询指定 Apollo 套(apolloHostId)、应用、环境(env)、集群、Namespace 的配置项
2.  **Apollo 服务实例列表** (`apollo_host_list`)：获取所有可用的 Apollo 服务实例（多套，按产品线/地域划分）
3.  **应用列表** (`apollo_app_list`)：获取 Apollo 中所有可用应用列表

## 触发条件

当用户查询包含以下关键词时触发：
- Apollo、配置中心
- appid、应用配置
- namespace、配置项
- key、配置 key
- 应用列表、有哪些应用

**排他条件**：
- 如果用户明确提到"服务器"、"主机"、"机房"，应触发 `cmdb-server-query` 技能
- 如果用户明确提到"部署"、"发布记录"，应触发 `project-deployment-query` 技能

## 工具调用 Schema

本技能依赖 3 个 MCP 工具，通过 `apollo-mcp-server` 提供服务。

### 工具 1: apollo_config_query

查询指定 Apollo 套（apolloHostId）的 Namespace 配置项列表。

```json
{
  "name": "apollo_config_query",
  "description": "查询 Apollo 配置中心的配置项列表。查询链路：哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 具体配置(key)。apolloHostId 缺省时自动遍历所有可用套定位应用所在套（结果带 usedHostId/hostName 标注实际使用的套），env 缺省为 PRO。支持按 Key 关键词过滤。当用户未提供 appId 时，先调用 apollo_app_list 获取可选应用列表（其中 usedHostId 为实际可用套），让用户选择 appId 后再用本工具查询。",
  "inputSchema": {
    "type": "object",
    "properties": {
      "apolloHostId": {
        "type": "integer",
        "description": "Apollo 服务 ID，来自 apollo_host_list 返回的 apolloHostId，用于指定查询哪一套 Apollo；不传时自动遍历所有可用套定位应用所在套"
      },
      "appId": {
        "type": "string",
        "description": "应用ID，如 'rule-engine'。必填参数"
      },
      "env": {
        "type": "string",
        "description": "环境，如 PRO(生产)、DEV(开发)、SIT(集成测试)、FAT(测试)、UAT(预发)，以目标 Apollo 实际环境名为准"
      },
      "clusterName": {
        "type": "string",
        "description": "集群名称，默认 'default'"
      },
      "namespaceName": {
        "type": "string",
        "description": "Namespace 名称，默认 'application'"
      },
      "key": {
        "type": "string",
        "description": "配置项 Key 关键词（模糊匹配），用于过滤配置项"
      }
    },
    "required": ["appId"]
  }
}
```

### 工具 2: apollo_host_list

实时查询所有可用的 Apollo 服务实例列表（多套），并**并发探活标记每套是否可用**，是查询链路第一步（确定哪套 Apollo）。

```json
{
  "name": "apollo_host_list",
  "description": "实时查询所有可用的 Apollo 服务列表（apolloHostId、服务名称、地址、Token 是否有、所属产品线、是否默认、是否可用 available），每次调用都会实时获取并并发探活各套可用性（结果缓存 60s）。available=true 表示该套代理接口可达、可正常查询配置/应用；available=false 表示超时或空响应，后续查询遍历时会自动跳过不可用套、优先走可用套。用于确定查询哪一套 Apollo（查询链路第一步）。",
  "inputSchema": {
    "type": "object",
    "properties": {}
  }
}
```

### 工具 3: apollo_app_list

获取指定 Apollo 套中所有可用应用列表。

```json
{
  "name": "apollo_app_list",
  "description": "获取 Apollo 服务中的应用列表（AppId、应用名称、所属部门）。apolloHostId 缺省时自动遍历所有可用套，取第一个能成功返回应用列表的套（结果顶层带 usedHostId/hostName 标注实际使用的套）。当用户想查配置但未提供 appId 时，先调用本工具获取可选应用列表，让用户选择后再用选中的 appId 调用 apollo_config_query。",
  "inputSchema": {
    "type": "object",
    "properties": {
      "apolloHostId": {
        "type": "integer",
        "description": "Apollo 服务 ID，来自 apollo_host_list 返回的 apolloHostId，用于指定查询哪一套 Apollo；不传时使用默认 Apollo"
      }
    }
  }
}
```

### 工具 4: apollo_config_global_search

**全局配置搜索**——解决"哪些应用用到了某个地址/关键词"的日常排障高频需求。自动并发遍历所有可用 Apollo 套 × 所有应用 × 配置，按 keyword 匹配 key 或 value。单次调用返回结果，比逐个应用调 apollo_config_query 快几十倍。

**何时使用**：用户说"全局查""哪些应用用到了""全部应用比对""搜 X 在哪些应用的配置里"，或用户提到"所有""全部"等全局关键词且意图是配置层面的搜索/比对。

```json
{
  "name": "apollo_config_global_search",
  "description": "【全局配置搜索】并发遍历所有可用 Apollo 套的所有应用配置，按关键词匹配 Key 或 Value。解决高频排障需求：'哪些应用的配置用到了某个地址/关键词'、'比对所有应用是否都有某个配置项'。keyword 必填；自动扫描所有 host × 所有 app × 配置，结果每条标注 host_id/app_id/namespace/key/value；超时返回部分结果+未完成标记。敏感字段已脱敏。预计耗时 30-60 秒。",
  "inputSchema": {
    "type": "object",
    "properties": {
      "keyword": {"type": "string", "description": "搜索关键词（必填），如 'vcpinner.cn:13302'、'password'、'redis.url'、'timeout'"},
      "searchField": {"type": "string", "description": "'key' 只匹配配置项 Key（配置名），'value' 只匹配值（如数据库地址），'both' 都匹配。默认 'both'"},
      "env": {"type": "string", "description": "环境，默认 'PRO'。如需搜索开发/预发环境配置传 'DEV'/'UAT'"},
      "maxResults": {"type": "integer", "description": "最多返回多少条匹配结果（默认 100），避免结果过多超出界面限制"},
      "namespaceName": {"type": "string", "description": "搜索哪个 namespace 的配置。默认 'application'（设为 application 时自动扩展扫描 datasource/redis/common/logging 等常见 namespace）"}
    },
    "required": ["keyword"]
  }
}
```

**返回数据结构**：
- `data.summary`：扫描统计（hostCount/appCount/scannedApps/matchedCount/maskedCount/elapsedMs）
- `data.matches[]`：每条含 `apolloHostId`/`hostName`/`appId`/`appName`/`namespaceName`/`key`/`value`/`masked`
- `data.partial=true`：超预算提前终止，结果可能不全，附 `unfinishedApps` 列未完成应用
- `data.truncated=true`：结果超过 maxResults 被截断

**交互前提示**：全局搜索耗时较长（30-60 秒），回复前先告知用户"正在全局搜索所有应用配置，可能需要几十秒..."

## 交互引导

### 参数检查（按数据层级自顶向下递进）

数据层级：**Apollo 实例(host) → 应用(app) → 配置内容(config)**。参数缺失时按层级自顶向下补齐，每层用上层数据：

| 工具 | 必填参数 | 缺失时处理（层级递进） |
|------|----------|-----------|
| apollo_host_list | 无 | 直接调用（实时查询+探活，返回每套 available 标记） |
| apollo_app_list | 无 | **先检查上层 apolloHostId 是否已知**：已指定则带 apolloHostId 调用（精准查该套应用）；未指定则工具自动遍历可用套（结果带 usedHostId）。用于层级 2 引导用户选应用 |
| apollo_config_query | appId | **层级递进引导**：①未指定哪套(host)且用户想明确→先调 apollo_host_list 出套列表引导选择（话术0）；②未指定应用(appId)→调 apollo_app_list（带上层 apolloHostId）出应用列表引导选择（话术1）；③用户只给了 appId→直接查（工具自动遍历定位 host，结果带 usedHostId） |

> 关键：缺哪层补哪层，每层带上层数据。但"能查就不问"优先——只给 appId 也能直接查（config_query 自动定位 host），不强制先走 host 层。

### 交互话术

**层级 1：未指定哪套 Apollo（先出套列表再选择）**：
```
当前有 N 套 Apollo 服务（已探活，M 套可用）：

| 序号 | 服务名称 | 是否可用 | 备注 |
|------|---------|---------|------|
| 1 | 天翼云眼贵州测试Apollo-亿讯专用 | ✅ 可用 | 默认套 |
| 2 | 天翼云眼广州4多AZ生产Apollo | ❌ 不可用 | 超时/空响应 |
| 3 | 3.0 P2P 易联家Apollo | ✅ 可用 | |

请告诉我要查询哪一套（如"贵州"、"P2P"），我会将对应的 apolloHostId 带入配置/应用查询；
建议优先选择"✅ 可用"的套；或直接说"默认"使用默认 Apollo 继续。

（提示：available=false 的套即使指定也可能超时失败；这里的"环境"指哪一套 Apollo 服务实例）
```

**层级 2：未指定应用 appId（先出应用列表再选择，带上层 apolloHostId）**：
```
已为您加载可用的应用列表，请选择要查询的应用：

| 序号 | AppId | 应用名称 | 部门 |
|------|-------|----------|------|
| 1 | rule-engine | 规则引擎 | 技术中台 |
| 2 | config-service | 配置管理服务 | 技术中台 |
| ... | ... | ... | ... |

请回复序号、AppId 或应用名称，我将查询该应用的配置。
（列表来自 apollo_app_list；若上层已选 Apollo 套则带 apolloHostId 查询，否则列表来自 usedHostId 标注的可用套）
```

> 层级 2 调 apollo_app_list 时若已有上层 apolloHostId（用户在层级 1 选过套），应带上使查询精准；未带则工具自动遍历可用套。

## 参数映射规则

> **术语总纲**（两个概念务必区分）：
> - **apolloHostId** = **Apollo 服务实例**（即"哪一套" Apollo，按产品线/地域独立部署多套，如"贵州"、"广州4"、"P2P"）→ 查询链路第一层，决定连哪套 Apollo
> - **env** = **应用环境**，取值 PRO（生产）、DEV（开发）、SIT（集成测试）、FAT（测试）、UAT（预发）等 → 查询链路第二层，决定该套内的哪个环境
> - 一句话：**apolloHostId 决定"哪一套服务"，env 决定"哪个环境"。**

### 环境术语判定规则（消除歧义，先判定再映射）

| 用户表述 | 判定 | 目标参数 |
|---------|------|---------|
| 提到 **开发/测试/生产/预发/集成** 或 **DEV/FAT/UAT/PRO/SIT** 等环境名关键词 | **应用环境** | env |
| 其他提到"环境"的表述（"贵州环境"、"有哪些环境"、"广州4那套"） | **Apollo 服务实例** | apolloHostId（调 `apollo_host_list`） |

> 一句话：**"环境"带环境名关键词（DEV/PRO 或 开发/测试/生产等）→ env；否则"环境"都指 Apollo 服务实例（apolloHostId）。**
> 边界情况：同时出现地名+环境名（如"广州4生产"）→ 地名→套(apolloHostId)、环境名→env，两者都生效。

### Apollo 套映射（查询链路第一步）

用户提到的地名/产品线需要先映射到某一套 Apollo 的 `apolloHostId`，再带入后续查询。

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| 地名/产品关键词（"贵州"、"广州4"、"P2P"、"百川"、"看家"等） | apolloHostId | 调用 `apollo_host_list` 实时查询，从返回中取对应套的 `apolloHostId` |
| 未指定 Apollo 套 | apolloHostId | 不传，MCP 使用默认 Apollo |

> **关键词 → 套名映射参考**：见 [references/apollo-hosts.md](references/apollo-hosts.md)

### 环境映射

> 注意：不同 Apollo 套的环境命名可能不同（如 PRO/SIT/DEV 或 PRO/DEV/FAT/UAT），下表为常见映射，实际传参以目标 Apollo 的环境名为准；无法确认时可先问用户或使用默认 PRO。

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "开发环境"、"DEV"、"开发" | env | "DEV" |
| "测试环境"、"FAT"、"测试" | env | "FAT" |
| "集成测试"、"SIT" | env | "SIT" |
| "预发环境"、"UAT"、"预发" | env | "UAT" |
| "生产环境"、"PRO"、"生产" | env | "PRO"（默认） |

### Namespace 映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "默认"、"application" | namespaceName | "application" |
| "数据库配置"、"datasource" | namespaceName | "datasource" |
| "Redis配置"、"redis" | namespaceName | "redis" |
| "日志配置"、"logging" | namespaceName | "logging" |
| "公共配置"、"common" | namespaceName | "common" |
| 其他 Namespace 名称 | namespaceName | 原值传递 |

> **常见 Namespace 列表**：见 [references/namespace-options.md](references/namespace-options.md)

## 输出格式

### 敏感信息脱敏（必做）

部分配置含敏感信息（数据库密码、Redis 密码、SDK 密钥、AES 密钥等），**输出配置值前必须脱敏**：

1. **判定**：配置 Key（忽略大小写）包含 `password`/`passwd`/`pwd`/`secret`/`token`/`api_key`/`apikey`/`access_key`/`accesskey`/`aes_key`/`aeskey`/`credential`/`encrypt`/`密码`/`密钥`/`私钥`/`凭据`/`证书` 等关键词 → 视为敏感字段
2. **掩码**：敏感字段值**统一显示 `******`**（不展示任何明文）；非敏感字段正常展示
3. **标注**：输出末尾追加 `（已对 {N} 个敏感字段脱敏，如需查看明文请联系管理员确认）`
4. **硬性**：即使用户要求"显示明文"，也先脱敏展示并提示安全风险

### 标准输出（配置查询）

```
## Apollo 配置查询结果

**查询条件**：{用户原始查询}

**应用**：{appId} | **环境**：{env} | **集群**：{clusterName} | **Namespace**：{namespaceName}

---

**结果摘要**：共查询到 N 个配置项（含 M 个敏感字段，已脱敏）

---

| 配置 Key | 值 |
|----------|----|
| timeout | 3000 |
| max.retry | 3 |
| spring.datasource.password | ****** |

（已对 1 个敏感字段脱敏，如需查看明文请联系管理员确认）

---

💡 您可以说：
- "搜索 timeout" - 按关键词过滤配置项
- "查看 datasource 配置" - 切换到其他 Namespace
```

### 应用列表输出（选择引导模式）

```
## Apollo 应用列表（Apollo 套: {usedHostId} - {hostName}）

---

| AppId | 应用名称 | 部门 |
|-------|----------|------|
| SampleApp | 样例应用 | 技术中台 |
| rule-engine | 规则引擎 | 技术中台 |

---

💡 请选择要查询的应用（回复序号/AppId/应用名称），我将继续查询其配置。
```

## 完整调用示例

### 示例 1：查询应用配置（默认 Apollo）

**用户输入**："查询 rule-engine 的配置"

**工具调用**：
```json
{
  "tool_name": "apollo_config_query",
  "parameters": {
    "appId": "rule-engine",
    "env": "PRO",
    "clusterName": "default",
    "namespaceName": "application"
  }
}
```
（未指定 Apollo 套时工具自动遍历所有可用套定位应用所在套；env 缺省时为 PRO 生产环境；用户提到开发/测试/预发环境时按[环境映射表](#环境映射)传 DEV/FAT/UAT/SIT）

### 示例 2：查看可用 Apollo 服务实例并指定某套查询

**用户输入**："有哪些 Apollo 服务实例" / "查一下贵州那套的配置"

**工具调用**（第一步，确定 apolloHostId）：
```json
{
  "tool_name": "apollo_host_list",
  "parameters": {}
}
```
（`apollo_host_list` 每次调用实时查询，返回所有 Apollo 服务实例及对应 `apolloHostId`，供用户确认）

用户确认"贵州"后，**第二步**用 apolloHostId 查询配置：
```json
{
  "tool_name": "apollo_config_query",
  "parameters": {
    "apolloHostId": 15,
    "appId": "rule-engine",
    "env": "PRO"
  }
}
```
（链路：哪套Apollo → 环境 → 应用 → 集群 → Namespace → 配置）

### 示例 3：搜索特定配置项

**用户输入**："查一下 rule-engine 的 timeout 配置"

**工具调用**：
```json
{
  "tool_name": "apollo_config_query",
  "parameters": {
    "appId": "rule-engine",
    "namespaceName": "application",
    "key": "timeout"
  }
}
```
（默认 Apollo；如需指定某套 Apollo，补传 `apolloHostId`）

### 示例 4：查看应用列表

**用户输入**："有哪些应用"

**工具调用**：
```json
{
  "tool_name": "apollo_app_list",
  "parameters": {}
}
```
（默认 Apollo；如需查看指定某套 Apollo 的应用，补传 `apolloHostId`）

### 示例 5：自顶向下三阶段递进（用户既未指定哪套也未指定应用）

**用户输入**："帮我查下配置"（既没说哪套 Apollo，也没说哪个应用）

> 自顶向下逐层补齐：层级1 选套 → 层级2 选应用 → 查配置。每层用上层数据。

**第一步（层级1：选哪套 Apollo）**：调用 `apollo_host_list`（实时查询+探活）：
```json
{
  "tool_name": "apollo_host_list",
  "parameters": {}
}
```
返回含 `available` 标记，把可用套以菜单展示，等待用户选套：
```
当前有 3 套 Apollo 服务（已探活，2 套可用）：

| 序号 | 服务名称 | 是否可用 | 备注 |
|------|---------|---------|------|
| 1 | 天翼云眼贵州测试Apollo-亿讯专用 | ✅ 可用 | 默认套 |
| 2 | 天翼云眼广州4多AZ生产Apollo | ❌ 不可用 | 超时 |
| 3 | 3.0 P2P 易联家Apollo | ✅ 可用 | |

请告诉我要查询哪一套（如"贵州"、"P2P"），或直接说"默认"。
```

**第二步（层级2：选应用，带上层 apolloHostId）**：用户选"贵州"(apolloHostId=15) 后，调用 `apollo_app_list` 带上 apolloHostId：
```json
{
  "tool_name": "apollo_app_list",
  "parameters": {"apolloHostId": 15}
}
```
返回该套应用列表，以菜单展示等待用户选应用：
```
已为您加载贵州那套的应用列表：

| 序号 | AppId | 应用名称 | 部门 |
|------|-------|----------|------|
| 1 | rule-engine | 规则引擎 | 技术中台 |
| 2 | config-service | 配置管理服务 | 技术中台 |

请回复序号、AppId 或应用名称。
```

**第三步（查配置，带上层 apolloHostId + appId）**：用户选"rule-engine"后，调用 `apollo_config_query`：
```json
{
  "tool_name": "apollo_config_query",
  "parameters": {
    "apolloHostId": 15,
    "appId": "rule-engine",
    "env": "PRO"
  }
}
```
（链路：哪套Apollo(15-贵州) → 环境(PRO) → 应用(rule-engine) → 集群 → Namespace → 配置）

> 对比旧逻辑：原来示例5直接调 app_list（用默认套），跳过了 host 层；现在自顶向下先选套再选应用，查询更精准，且用户能避开不可用套。若用户只给了 appId（如"查 rule-engine 配置"），可直接跳到第三步（config_query 自动遍历定位 host），不强制走完层级1。

## 错误处理

### 示例 6：全局配置搜索（跨所有 Apollo 套 × 所有应用）

**用户输入**："哪些应用的配置用到了 vcpinner.cn:13302 这个地址？" / "全局搜一下 redis.password 相关的配置"

**意图判定**：用户提到"全局/哪些应用/全部"→ 触发全局配置搜索场景。先告知用户预计耗时。

**先回复用户**：
```
正在全局搜索所有 Apollo 套和应用的配置，可能需要 30-60 秒，请稍候...
```

**工具调用**（搜 VALUE 匹配 + 限制 50 条）：
```json
{
  "tool_name": "apollo_config_global_search",
  "parameters": {
    "keyword": "vcpinner.cn:13302",
    "searchField": "value",
    "env": "PRO",
    "maxResults": 50
  }
}
```

**返回结构**：
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "keyword": "vcpinner.cn:13302",
    "searchField": "value",
    "summary": {"hostCount": 8, "appCount": 728, "scannedApps": 728, "matchedCount": 12, "maskedCount": 0, "elapsedMs": 42350},
    "matches": [
      {"apolloHostId": 15, "hostName": "贵州", "appId": "user-service", "appName": "用户服务", "namespaceName": "datasource", "key": "spring.datasource.url", "value": "jdbc:mysql://vcpinner.cn:13302/user_db"},
      {"apolloHostId": 14, "hostName": "广州4", "appId": "order-service", "appName": "订单服务", "namespaceName": "datasource", "key": "spring.datasource.url", "value": "jdbc:mysql://vcpinner.cn:13302/order_db"}
    ],
    "partial": false,
    "truncated": false
  }
}
```

**输出用户**（参考主 SKILL.md 话术 8 格式）。

> 对比旧方案：原来 agent 生成脚本逐个应用调 apollo_config_query（700+ 应用 × 6s = 小时级），现在 MCP 内部 ThreadPoolExecutor 并行扫描（8 workers × 10 hosts ≈ 30-60 秒），单次调用返回。

### API 调用失败（按 errorType 差异化处理）

**⚠️ 严禁盲目重试！** 收到以下错误时，**绝不**用相同参数重试同一个工具，按主 SKILL.md 执行纪律处理。

| code | errorType | retryable | 正确处理 | 错误处理（禁止） |
|---|---|---|---|---|
| 504 | `timeout` | false | 话术 7a → 建议缩小范围/换套/稍后 | ❌ 同样参数重试 |
| 503 | `unavailable` | false | 话术 7b → 告知代理暂不可用 | ❌ 换参数重试 |
| 200 + `data.fallback=true` | - | - | 话术 7c → 正常展示 + 标注切换 | ❌ 告知用户"套有问题" |
| 200 + `data.partial=true` | - | - | 话术 7d → 展示已有结果 + 建议缩小范围 | ❌ 等超时后重试 |

**错误返回结构**（MCP 层已自动构建）：
```json
{
  "code": 504,
  "message": "配置查询超时：Apollo 代理接口在 6 秒内未响应",
  "errorType": "timeout",
  "retryable": false,
  "suggestion": "配置查询已快速失败，无需重试。...建议告知用户稍后再试"
}
```

**进度提示**：调 `apollo_config_global_search` 前**必须先回复用户**"正在全局搜索，预计 30-60 秒..."。调其他工具（config_query/app_list）通常秒级返回，无需进度提示。

### 无查询结果

```
## 查询结果

未查询到符合条件的配置项（appId={appId}, namespace={namespaceName}）。

建议检查：
- 应用名称是否正确？
- Namespace 名称是否正确？
```
