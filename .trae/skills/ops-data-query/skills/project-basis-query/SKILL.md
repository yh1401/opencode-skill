---
name: project-basis-query
description: 查询工程项目基础信息，包括项目名称、中文名、所属产品、SVN/GIT 仓库地址、项目类型、父项目等。
version: 4.0.0
author: Skill Agent Team
---

# 工程项目信息查询技能

> ⚠️ **v4.0 说明**：数据获取层已迁移至 MCP 服务器，本文件仅保留指令层（触发条件、参数映射、输出格式化）。

## 功能描述

查询工程项目基础信息，包括：
- 项目名称（唯一标识）、中文名
- 所属产品
- SVN/GIT 仓库地址
- 项目类型（核心组件/中间件等）
- 父项目、分组信息

## 触发条件

**关键词**：工程项目、项目信息、项目配置、代码仓库、SVN、GIT、仓库地址、项目名称

**排他条件**：
- 查服务器配置 → 使用 `cmdb-server-query`
- 查公网IP → 使用 `server-public-ip-query`
- 查部署记录 → 使用 `project-deployment-query`
- 查产品信息 → 使用 `product-query`

## MCP 工具调用

**工具名**：`project_basis_query`

**参数映射**：

| 参数名 | 类型 | 说明 | 默认值 | 来源 |
|--------|------|------|--------|------|
| id | string | 项目 ID | - | 用户输入 |
| name | string | 工程项目名称（模糊匹配） | - | 用户输入 |
| productId | string | 产品 ID | - | 用户输入 |
| productName | string | 产品名称（模糊匹配） | - | 用户输入 |
| currentPage | integer | 页码 | 1 | 自动 |
| pageSize | integer | 每页条数 | 40 | 数量词映射 |

**调用示例**：

**示例 1：按产品名称查询**
```json
{
  "name": "project_basis_query",
  "parameters": {
    "productName": "天翼看家",
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**示例 2：按项目名称查询**
```json
{
  "name": "project_basis_query",
  "parameters": {
    "name": "tykj-kafka-test"
  }
}
```

**示例 3：按产品 ID 查询**
```json
{
  "name": "project_basis_query",
  "parameters": {
    "productId": "prod-001",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 4：按项目类型查询**
```json
{
  "name": "project_basis_query",
  "parameters": {
    "projectType": "核心组件",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

## 输出格式

### 标准输出

```
| 项目名称 | 中文名 | 所属产品 | 项目类型 |
|----------|--------|----------|----------|
```

### 详细输出

增加字段：代码仓库、父项目、分组、项目描述

## 响应字段

| 字段 | 类型 | 说明 | 格式化 |
|------|------|------|--------|
| projectName | string | 项目名称（唯一标识） | - |
| chineseName | string | 中文名 | - |
| productName | string | 所属产品 | - |
| repoPath | string | SVN/GIT 仓库地址 | - |
| description | string | 项目描述 | - |
| projectType | string | 项目类型 | 核心组件/中间件等 |
| parentProject | string | 父项目名 | - |
| group | string | 分组 | - |

## 参考文件

通用输出模板、错误处理、参数映射规则请参考父级 `ops-data-query/SKILL.md`。

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|---------|
| 4.0 | 2026-06-26 | 迁移数据获取层至 MCP 服务器，精简为薄指令层 |
| 3.0 | 2026-06-24 | 抽离共享内容 |
| 2.0 | 2026-06-18 | 统一字段为英文 key |
| 1.0 | 2026-05-20 | 初始版本 |
