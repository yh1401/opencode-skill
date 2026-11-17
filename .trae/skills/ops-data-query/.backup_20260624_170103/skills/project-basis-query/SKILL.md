---
name: project-basis-query
description: 查询工程项目基础信息，包括项目名称、中文名、所属产品、SVN/GIT 仓库地址、项目类型、父项目等。支持按项目名称、产品名称等条件查询。
version: 2.0.0
author: Skill Agent Team
---

# 工程项目信息查询技能

## 功能描述

本技能用于查询工程项目的基础信息，包括：
- 项目名称、中文名
- 所属产品
- SVN/GIT 仓库地址
- 项目类型、项目描述
- 父项目、分组信息

## 触发条件

当用户查询包含以下关键词时触发：
- 工程项目、项目信息、项目配置
- 代码仓库、SVN、GIT、仓库地址
- 项目名称、项目类型

**排他条件**：
- 如果用户查询的是项目部署记录，应触发 `project-deployment-query` 技能
- 如果用户查询的是产品信息，应触发 `product-query` 技能

## 输入参数

### 必填参数

无

### 可选参数

| 参数名 | 类型 | 说明 | 默认值 | 可选值 |
|--------|------|------|--------|--------|
| id | string | 项目 ID | - | - |
| name | string | 工程项目名称（模糊匹配） | - | - |
| productId | string | 产品 ID | - | - |
| productName | string | 产品名称（模糊匹配） | - | - |
| currentPage | integer | 页码 | 1 | ≥1 |
| pageSize | integer | 每页条数 | 40 | 1~100 |

## 工具调用 Schema

```json
{
  "name": "project_basis_query",
  "description": "查询工程项目基础信息，包括项目名称、中文名、所属产品、SVN/GIT仓库地址、项目类型、父项目等",
  "parameters": {
    "type": "object",
    "properties": {
      "id": {
        "type": "string",
        "description": "项目ID"
      },
      "name": {
        "type": "string",
        "description": "工程项目名称（模糊匹配）"
      },
      "productId": {
        "type": "string",
        "description": "产品ID"
      },
      "productName": {
        "type": "string",
        "description": "产品名称（模糊匹配）"
      },
      "currentPage": {
        "type": "integer",
        "description": "页码",
        "minimum": 1
      },
      "pageSize": {
        "type": "integer",
        "description": "每页条数",
        "minimum": 1,
        "maximum": 100
      }
    },
    "required": []
  }
}
```

## 参数映射规则

### 数量词识别规则

| 用户输入 | 对应的 pageSize |
|----------|----------------|
| "最近的"、"最新的" | 10 |
| "所有"、"全部" | 100 |
| "一些"、"几条" | 40 (默认) |

## API 接口

### 接口地址

**生产环境**：POST https://oss.tech.ctseelink.cn/a/cmdb/baseProjectBasis/

### 请求格式

```json
{
  "name": "tykj-kafka-test",
  "currentPage": 1,
  "pageSize": 40
}
```

### 请求参数说明

| 参数名 | 说明 |
|--------|------|
| id | 项目 ID |
| name | 工程项目名称（模糊匹配） |
| productId | 产品 ID |
| productName | 产品名称（模糊匹配） |
| currentPage | 页码 |
| pageSize | 每页条数 |

### 参数映射（用户输入 → API 参数）

| 用户参数 | API 参数 |
|----------|----------|
| id | id |
| name | name |
| productId | productId |
| productName | productName |
| page | currentPage |
| pageSize | pageSize |

### 响应格式

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [
      {
        "projectName": "tykj-kafka-test",
        "chineseName": "天翼看家Kafka测试",
        "productName": "天翼看家",
        "repoPath": "svn://svn.tech.ctseelink.cn/tykj/kafka-test",
        "description": "Kafka消息队列测试项目",
        "projectType": "中间件",
        "parentProject": "tykj-base",
        "group": "消息队列组"
      }
    ],
    "total": 100,
    "current": 1,
    "size": 40,
    "pages": 3
  }
}
```

### 响应字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| code | integer | 状态码，200 表示成功 |
| message | string | 响应消息 |
| data | object | 响应数据体 |
| data.records | array | 项目记录列表 |
| data.total | integer | 总记录数 |
| data.current | integer | 当前页码 |
| data.size | integer | 每页条数 |
| data.pages | integer | 总页数 |

### records 中的每条记录

| 字段 | 类型 | 说明 |
|------|------|------|
| projectName | string | 工程项目名（唯一标识） |
| chineseName | string | 中文名 |
| productName | string | 所属产品 |
| repoPath | string | SVN/GIT 路径/URL |
| description | string | 项目描述 |
| projectType | string | 项目类型，如 "核心组件"、"中间件" |
| parentProject | string | 父项目名 |
| group | string | 分组 |

## 输出格式

### 标准输出（简单模式 - 默认）

```
## 项目基础信息查询结果

**查询条件**：{用户原始查询}

**匹配技能**：项目基础信息查询

**查询范围**：项目名称=tykj-kafka-test（显示前10条）

---

**结果摘要**：共查询到 100 条项目记录，当前显示前 10 条

---

| 项目名称 | 中文名 | 所属产品 | 项目类型 |
|----------|--------|----------|----------|
| tykj-kafka-test | 天翼看家Kafka测试 | 天翼看家 | 中间件 |
| guizh-rules-basis | 规则引擎基础项目 | 规则引擎平台 | 核心组件 |
| ... | ... | ... | ... |

---

💡 您可以说：
- "查看详细信息" - 显示代码仓库、父项目、分组等完整信息
- "下一页" - 查看更多项目
- "查看全部" - 显示所有查询结果
```

### 详细输出（用户要求"查看详细信息"时）

```
## 项目基础信息查询结果（详细）

**查询条件**：{用户原始查询}

**匹配技能**：项目基础信息查询

**查询范围**：项目名称=tykj-kafka-test

---

**结果摘要**：共查询到 100 条项目记录，当前显示前 10 条

---

| 项目名称 | 中文名 | 所属产品 | 代码仓库 | 项目类型 | 父项目 | 分组 |
|----------|--------|----------|----------|----------|--------|------|
| tykj-kafka-test | 天翼看家Kafka测试 | 天翼看家 | svn://... | 中间件 | tykj-base | 消息队列组 |
| guizh-rules-basis | 规则引擎基础项目 | 规则引擎平台 | git@... | 核心组件 | guizh-base | 规则引擎组 |
| ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 CMDB 系统。
```

### 降级输出（API 失败时）

```
## 项目基础信息查询结果

> ⚠️ 提示：API 接口调用失败，已切换为模拟数据

**查询条件**：{用户原始查询}

**匹配技能**：项目基础信息查询

**查询参数**：{"name":"tykj-kafka-test","pageSize":40}

---

**结果摘要**：共查询到 N 条项目记录（模拟数据）

---

| 项目名称 | 中文名 | 所属产品 | 代码仓库 | 项目类型 | 父项目 |
|----------|--------|----------|----------|----------|--------|
| tykj-kafka-test | 天翼看家Kafka测试 | 天翼看家 | svn://... | 中间件 | tykj-base |
| ... | ... | ... | ... | ... | ... |

---

**说明**：当前显示的是模拟数据，真实数据需要部署服务可用时才能获取。
```

## 完整调用示例

### 示例 1：按项目名称查询

**用户输入**："查 tykj-kafka-test 项目信息"

**工具调用**：
```json
{
  "tool_name": "project_basis_query",
  "parameters": {
    "name": "tykj-kafka-test",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**API 请求**：
```json
{
  "name": "tykj-kafka-test",
  "currentPage": 1,
  "pageSize": 40
}
```

**API 响应**：
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [
      {
        "projectName": "tykj-kafka-test",
        "chineseName": "天翼看家Kafka测试",
        "productName": "天翼看家",
        "repoPath": "svn://svn.tech.ctseelink.cn/tykj/kafka-test",
        "description": "Kafka消息队列测试项目",
        "projectType": "中间件",
        "parentProject": "tykj-base",
        "group": "消息队列组"
      }
    ],
    "total": 1,
    "current": 1,
    "size": 40,
    "pages": 1
  }
}
```

**最终输出**：
```
## 项目基础信息查询结果

**查询条件**：查 tykj-kafka-test 项目信息

**匹配技能**：项目基础信息查询

**查询参数**：{"name":"tykj-kafka-test","pageSize":40}

---

**结果摘要**：共查询到 1 条项目记录

---

| 项目名称 | 中文名 | 所属产品 | 代码仓库 | 项目类型 | 父项目 |
|----------|--------|----------|----------|----------|--------|
| tykj-kafka-test | 天翼看家Kafka测试 | 天翼看家 | svn://svn.tech.ctseelink.cn/tykj/kafka-test | 中间件 | tykj-base |

---

**说明**：数据来源于 CMDB 系统。
```

### 示例 2：按产品名称查询

**用户输入**："找天翼看家的工程项目"

**工具调用**：
```json
{
  "tool_name": "project_basis_query",
  "parameters": {
    "productName": "天翼看家",
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**API 请求**：
```json
{
  "productName": "天翼看家",
  "currentPage": 1,
  "pageSize": 100
}
```

**API 响应**：
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [
      {
        "projectName": "tykj-kafka-test",
        "chineseName": "天翼看家Kafka测试",
        "productName": "天翼看家",
        "repoPath": "svn://svn.tech.ctseelink.cn/tykj/kafka-test",
        "description": "Kafka消息队列测试项目",
        "projectType": "中间件",
        "parentProject": "tykj-base",
        "group": "消息队列组"
      }
    ],
    "total": 20,
    "current": 1,
    "size": 100,
    "pages": 1
  }
}
```

**最终输出**：
```
## 项目基础信息查询结果

**查询条件**：找天翼看家的工程项目

**匹配技能**：项目基础信息查询

**查询参数**：{"productName":"天翼看家","pageSize":100}

---

**结果摘要**：共查询到 20 条项目记录

---

| 项目名称 | 中文名 | 所属产品 | 代码仓库 | 项目类型 | 父项目 |
|----------|--------|----------|----------|----------|--------|
| tykj-kafka-test | 天翼看家Kafka测试 | 天翼看家 | svn://svn.tech.ctseelink.cn/tykj/kafka-test | 中间件 | tykj-base |
| ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 CMDB 系统。
```

## 错误处理

### 错误场景 1：参数格式错误

**输入**："查询无效的项目 ID"

**输出**：
```
## 查询失败

**错误类型**：参数格式错误

**错误原因**：项目 ID 格式不正确

**建议**：请输入有效的项目 ID，如"proj-001"或完整的 UUID
```

### 错误场景 2：API 调用失败

**输出**：
```
## 查询失败

**错误类型**：网络错误

**错误原因**：CMDB 服务响应失败，请稍后重试

**建议**：
1. 检查网络连接
2. 稍后重新查询
3. 如问题持续，请联系管理员
```

### 错误场景 3：无查询结果

**输入**："查询不存在的项目"

**输出**：
```
## 查询结果

**查询条件**：查询不存在的项目

**匹配技能**：项目基础信息查询

**查询参数**：{"name":"不存在的项目","pageSize":40}

---

**结果摘要**：未查询到符合条件的项目记录

---

**说明**：当前系统中不存在该项目信息。
```

## Mock 数据（API 降级备用）

当 API 不可用时，使用以下模拟数据返回：

```json
{
  "code": 200, "message": "success",
  "data": {
    "records": [
      {
        "projectName": "tykj-kafka-test", "chineseName": "天翼看家Kafka测试",
        "productName": "天翼看家",
        "repoPath": "svn://svn.tech.ctseelink.cn/tykj/kafka-test",
        "description": "Kafka消息队列测试项目", "projectType": "中间件",
        "parentProject": "tykj-base", "group": "消息队列组"
      },
      {
        "projectName": "guizh-rules-basis", "chineseName": "规则引擎基础项目",
        "productName": "规则引擎平台",
        "repoPath": "git@git.tech.ctseelink.cn:guizh/rules-basis.git",
        "description": "规则引擎核心基础组件", "projectType": "核心组件",
        "parentProject": "guizh-base", "group": "规则引擎组"
      },
      {
        "projectName": "5g-industry-basis", "chineseName": "5G工业视宽基础项目",
        "productName": "5G工业视宽平台",
        "repoPath": "git@git.tech.ctseelink.cn:5g/industry-basis.git",
        "description": "5G工业视宽平台基础组件", "projectType": "核心组件",
        "parentProject": "5g-base", "group": "5G应用组"
      },
      {
        "projectName": "edge-compute-basis", "chineseName": "边缘计算基础项目",
        "productName": "边缘计算平台",
        "repoPath": "svn://svn.tech.ctseelink.cn/edge/compute-basis",
        "description": "边缘计算核心组件", "projectType": "核心组件",
        "parentProject": "edge-base", "group": "边缘计算组"
      }
    ],
    "total": 4, "size": 40, "current": 1, "pages": 1
  }
}
```
