---
name: apollo-config-search
description: 查询 Apollo 配置中心的配置信息，支持配置项查询、发布历史查询和应用列表查询
version: 2.0.0
author: Skill Agent Team
---

# Apollo 配置查询技能

## 功能描述

本技能通过 **MCP 工具** 与 Apollo 配置中心进行交互，提供以下能力：

1.  **配置查询** (`apollo_config_query`)：查询指定应用、环境、集群、Namespace 的配置项
2.  **发布历史** (`apollo_release_history`)：查询配置的发布版本记录
3.  **应用列表** (`apollo_app_list`)：获取 Apollo 中所有可用应用列表

## 触发条件

当用户查询包含以下关键词时触发：
- Apollo、配置中心
- appid、应用配置
- namespace、配置项
- key、配置 key
- 发布历史、变更记录
- 应用列表、有哪些应用

**排他条件**：
- 如果用户明确提到"服务器"、"主机"、"机房"，应触发 `cmdb-server-query` 技能
- 如果用户明确提到"部署"、"发布记录"，应触发 `project-deployment-query` 技能

## 工具调用 Schema

本技能依赖 3 个 MCP 工具，通过 `apollo-mcp-server` 提供服务。

### 工具 1: apollo_config_query

查询指定应用的 Namespace 配置项列表。

```json
{
  "name": "apollo_config_query",
  "description": "查询 Apollo 配置中心的配置项列表。支持按应用ID、环境、集群、Namespace 和 Key 关键词过滤。",
  "inputSchema": {
    "type": "object",
    "properties": {
      "appId": {
        "type": "string",
        "description": "应用ID，如 'rule-engine'。必填参数"
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

### 工具 2: apollo_release_history

查询指定应用的 Namespace 发布历史。

```json
{
  "name": "apollo_release_history",
  "description": "查询 Apollo 配置的发布历史记录，包含发布版本、发布人、发布时间等信息。",
  "inputSchema": {
    "type": "object",
    "properties": {
      "appId": {
        "type": "string",
        "description": "应用ID，如 'rule-engine'。必填参数"
      },
      "env": {
        "type": "string",
        "description": "环境，PRO=生产, DEV=开发, FAT=测试, UAT=预发",
        "enum": ["PRO", "DEV", "FAT", "UAT"]
      },
      "clusterName": {
        "type": "string",
        "description": "集群名称，默认 'default'"
      },
      "namespaceName": {
        "type": "string",
        "description": "Namespace 名称，默认 'application'"
      }
    },
    "required": ["appId"]
  }
}
```

### 工具 3: apollo_app_list

获取 Apollo 中所有可用应用列表。

```json
{
  "name": "apollo_app_list",
  "description": "获取 Apollo 配置中心中所有可用的应用列表（AppId、应用名称、所属部门）。",
  "inputSchema": {
    "type": "object",
    "properties": {}
  }
}
```

## 交互引导

### 参数检查

| 工具 | 必填参数 | 缺失时处理 |
|------|----------|-----------|
| apollo_config_query | appId | 反问"想查哪个应用"，或先调用 `apollo_app_list` |
| apollo_release_history | appId | 同上 |
| apollo_app_list | 无 | 直接调用 |

### 交互话术

**缺少应用 ID**：
```
请问您想查询哪个应用的配置？
您可以说应用名称（如"SampleApp"）、AppId（如"rule-engine"），
或输入"应用列表"查看所有可用应用。
```

## 参数映射规则

### 环境映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "开发环境"、"DEV"、"开发" | env | "DEV" |
| "测试环境"、"FAT"、"测试" | env | "FAT" |
| "预发环境"、"UAT"、"预发" | env | "UAT" |
| "生产环境"、"PRO"、"生产" | env | "PRO" |

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

### 标准输出（配置查询）

```
## Apollo 配置查询结果

**查询条件**：{用户原始查询}

**应用**：{appId} | **集群**：{clusterName} | **Namespace**：{namespaceName}

---

**结果摘要**：共查询到 N 个配置项

---

| 配置 Key | 值 |
|----------|----|
| timeout | 3000 |
| max.retry | 3 |

---

💡 您可以说：
- "搜索 timeout" - 按关键词过滤配置项
- "查看发布历史" - 获取配置变更记录
- "查看 datasource 配置" - 切换到其他 Namespace
```

### 发布历史输出

```
## Apollo 发布历史

**应用**：{appId} | **环境**：{env} | **Namespace**：{namespaceName}

---

| 版本 | 发布标题 | 发布人 | 发布时间 |
|------|----------|--------|----------|
| v3 | 2026-01配置更新 | zhangsan | 2026-01-15 10:30:00 |
| v2 | 初始配置 | admin | 2025-11-01 09:00:00 |
```

### 应用列表输出

```
## Apollo 应用列表

---

| AppId | 应用名称 | 部门 |
|-------|----------|------|
| SampleApp | 样例应用 | 技术中台 |
| rule-engine | 规则引擎 | 技术中台 |
```

## 完整调用示例

### 示例 1：查询应用配置

**用户输入**："查询 rule-engine 的配置"

**工具调用**：
```json
{
  "tool_name": "apollo_config_query",
  "parameters": {
    "appId": "rule-engine",
    "clusterName": "default",
    "namespaceName": "application"
  }
}
```

### 示例 2：搜索特定配置项

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

### 示例 3：查询发布历史

**用户输入**："rule-engine 的配置变更记录"

**工具调用**：
```json
{
  "tool_name": "apollo_release_history",
  "parameters": {
    "appId": "rule-engine",
    "env": "PRO",
    "namespaceName": "application"
  }
}
```

### 示例 4：查看应用列表

**用户输入**："有哪些应用"

**工具调用**：
```json
{
  "tool_name": "apollo_app_list",
  "parameters": {}
}
```

## 错误处理

### API 调用失败

```
## 查询失败

**错误类型**：API 调用失败

**错误原因**：Apollo 服务暂不可用

**建议**：请稍后重试，如问题持续请联系管理员。
```

### 无查询结果

```
## 查询结果

未查询到符合条件的配置项（appId={appId}, namespace={namespaceName}）。

建议检查：
- 应用名称是否正确？
- Namespace 名称是否正确？
```
