---
name: project-deployment-query
description: 查询项目部署记录，包括部署时间、版本、环境、状态、部署人等。支持按项目名、环境、状态、时间范围筛选。
version: 4.0.0
author: Skill Agent Team
---

# 项目部署查询技能

> ⚠️ **v4.0 说明**：数据获取层已迁移至 MCP 服务器，本文件仅保留指令层（触发条件、参数映射、输出格式化）。

## 功能描述

查询项目部署记录，包括：
- 部署基本信息：项目名、环境、状态
- 时间信息：开始时间、结束时间、耗时
- 版本信息：Git 分支、版本号、部署人
- 服务器列表：每台部署目标服务器的状态

## 触发条件

**关键词**：部署、部署记录、部署状态、部署历史、发布、上线

**排他条件**：
- 查服务器配置 → 使用 `cmdb-server-query`
- 查公网IP → 使用 `server-public-ip-query`
- 查产品信息 → 使用 `product-query`
- 查项目代码 → 使用 `project-basis-query`

## MCP 工具调用

**工具名**：`project_deployment_query`

**参数映射**：

| 参数名 | 类型 | 说明 | 默认值 | 来源 |
|--------|------|------|--------|------|
| projectName | string | 项目名称（模糊匹配） | - | 用户输入 |
| environment | integer | 环境代码 | - | 参数映射 |
| deploymentStatus | integer | 部署状态 | - | 参数映射 |
| deployer | string | 部署人 | - | 用户输入 |
| startTime | string | 开始时间（YYYY-MM-DD） | - | 用户输入 |
| endTime | string | 结束时间（YYYY-MM-DD） | - | 用户输入 |
| currentPage | integer | 页码 | 1 | 自动 |
| pageSize | integer | 每页条数 | 40 | 数量词映射 |

**调用示例**：

**示例 1：按项目名 + 环境查询**
```json
{
  "name": "project_deployment_query",
  "parameters": {
    "projectName": "guizh-rules-api",
    "environment": 3,
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 2：按环境 + 部署状态查询**
```json
{
  "name": "project_deployment_query",
  "parameters": {
    "environment": 3,
    "deploymentStatus": 1,
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 3：按部署人查询**
```json
{
  "name": "project_deployment_query",
  "parameters": {
    "deployer": "张三",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 4：按时间范围查询**
```json
{
  "name": "project_deployment_query",
  "parameters": {
    "startTime": "2026-06-01",
    "endTime": "2026-06-30",
    "currentPage": 1,
    "pageSize": 100
  }
}
```

## 输出格式

### 标准输出

```
| 项目名 | 环境 | 状态 | 部署时间 | 耗时 | 版本 | 部署人 |
|--------|------|------|---------|------|------|--------|
```

### 详细输出

增加字段：Git分支、服务器列表（成功/失败详情）、错误信息

> ⚠️ `statusCode=1`（失败）时，`errorMessage` 必须用 ❌ 红色高亮显示

### 状态图标

| 状态代码 | 图标 | 含义 |
|----------|------|------|
| 0 | ✅ | 成功 |
| 1 | ❌ | 失败 |
| 2 | 🟡 | 进行中 |
| 3 | ⏳ | 待部署 |

## 响应字段

| 字段 | 类型 | 说明 | 格式化 |
|------|------|------|--------|
| projectName | string | 项目名称 | - |
| environment | string | 环境名称 | 测试/灰度/生产/研发 |
| environmentCode | integer | 环境代码 | - |
| deploymentStatus | string | 部署状态（中文） | ✅成功/❌失败 |
| statusCode | integer | 状态代码 | 0=成功, 1=失败, 2=进行中 |
| startTime | string | 开始时间 | YYYY-MM-DD HH:mm:ss |
| endTime | string | 结束时间 | YYYY-MM-DD HH:mm:ss |
| duration | integer | 耗时（秒） | 格式化为 X分Y秒 |
| deployer | string | 部署人 | - |
| version | string | 版本号 | - |
| gitBranch | string | Git 分支 | - |
| serverList | array | 部署目标服务器列表 | `主机名(IP) ✅/❌` |
| errorMessage | string | 错误信息 | ❌ + 红色高亮 |

## 参考文件

通用输出模板、错误处理、参数映射规则请参考父级 `ops-data-query/SKILL.md`。

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|---------|
| 4.0 | 2026-06-26 | 迁移数据获取层至 MCP 服务器，精简为薄指令层 |
| 3.0 | 2026-06-24 | 抽离共享内容 |
| 2.0 | 2026-06-18 | 统一字段为英文 key |
| 1.0 | 2026-05-20 | 初始版本 |
