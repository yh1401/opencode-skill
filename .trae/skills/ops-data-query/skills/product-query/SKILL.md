---
name: product-query
description: 查询产品信息，包括产品名称、产品级别、所属部门、产品经理、运维负责人等。支持按产品名称、ID、启用状态筛选。
version: 4.0.0
author: Skill Agent Team
---

# 产品查询技能

> ⚠️ **v4.0 说明**：数据获取层已迁移至 MCP 服务器，本文件仅保留指令层（触发条件、参数映射、输出格式化）。

## 功能描述

查询产品信息，包括：
- 产品名称、功能/主责
- 产品级别（一级/二级）
- 上级产品
- 所属单位/部门
- 产品经理、运维负责人
- 启用状态

## 触发条件

**关键词**：产品、产品信息、产品名称、产品ID、产品列表、产品线

**排他条件**：
- 查服务器配置 → 使用 `cmdb-server-query`
- 查公网IP → 使用 `server-public-ip-query`
- 查部署记录 → 使用 `project-deployment-query`
- 查项目代码 → 使用 `project-basis-query`

## MCP 工具调用

**工具名**：`product_query`

**参数映射**：

| 参数名 | 类型 | 说明 | 默认值 | 来源 |
|--------|------|------|--------|------|
| id | string | 产品 ID | - | 用户输入 |
| name | string | 产品名称（模糊匹配） | - | 用户输入 |
| flag | integer | 启用状态 | - | 参数映射（0=禁用, 1=启用） |
| currentPage | integer | 页码 | 1 | 自动 |
| pageSize | integer | 每页条数 | 40 | 数量词映射 |

**调用示例**：

**示例 1：查询所有启用的产品**
```json
{
  "name": "product_query",
  "parameters": {
    "flag": 1,
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**示例 2：按产品名称查询**
```json
{
  "name": "product_query",
  "parameters": {
    "name": "天翼看家",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 3：按产品 ID 查询**
```json
{
  "name": "product_query",
  "parameters": {
    "id": "prod-001"
  }
}
```

**示例 4：按部门查询**
```json
{
  "name": "product_query",
  "parameters": {
    "department": "云网部",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

## 输出格式

### 标准输出

```
| 产品名称 | 上级产品 | 级别 | 状态 |
|----------|----------|------|------|
```

### 详细输出

增加字段：产品功能、所属部门、产品经理、运维负责人

## 响应字段

| 字段 | 类型 | 说明 | 格式化 |
|------|------|------|--------|
| productName | string | 产品名称 | - |
| productFunction | string | 产品功能/主责 | - |
| parentProduct | string | 上级产品 | - |
| productLevel | string | 产品级别 | 一级/二级 |
| enabled | string | 启用状态 | 是/否 |
| department | string | 所属部门 | - |
| productManager | string | 产品经理 | - |
| opsLead | string | 运维负责人 | - |

## 参考文件

通用输出模板、错误处理、参数映射规则请参考父级 `ops-data-query/SKILL.md`。

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|---------|
| 4.0 | 2026-06-26 | 迁移数据获取层至 MCP 服务器，精简为薄指令层 |
| 3.0 | 2026-06-24 | 抽离共享内容 |
| 2.0 | 2026-06-18 | 统一字段为英文 key |
| 1.0 | 2026-05-20 | 初始版本 |
