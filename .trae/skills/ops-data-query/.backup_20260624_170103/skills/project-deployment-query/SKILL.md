---
name: project-deployment-query
description: 查询项目部署信息，包括部署记录、部署环境、状态、版本、负责人、耗时等。支持按项目名称、环境、状态、时间范围组合查询。
version: 2.0.0
author: Skill Agent Team
---

# 项目部署信息查询技能

## 功能描述

本技能用于查询项目的部署记录和状态信息，包括：
- 部署记录：项目名称、部署时间、版本号
- 部署状态：成功、失败、进行中、待部署
- 部署环境：测试、灰度、生产、研发
- 部署详情：负责人、耗时、Git分支、服务器列表

## 触发条件

当用户查询包含以下关键词时触发：
- 部署、发布、上线
- 部署记录、部署历史、部署状态
- 发布记录、发布历史

**排他条件**：
- 如果用户查询的是服务器配置信息，应触发 `cmdb-server-query` 技能
- 如果用户查询的是产品信息，应触发 `product-query` 技能

## 输入参数

### 必填参数

无

### 可选参数

| 参数名 | 类型 | 说明 | 默认值 | 可选值 |
|--------|------|------|--------|--------|
| projectName | string | 项目名称（模糊匹配） | - | - |
| deployEnv | integer | 部署环境 | - | 1=测试, 2=灰度, 3=生产, 4=研发 |
| status | integer | 部署状态 | - | 0=成功, 1=失败, 2=进行中, 3=待部署 |
| startTime | string | 开始时间 | - | YYYY-MM-DD HH:mm:ss |
| endTime | string | 结束时间 | - | YYYY-MM-DD HH:mm:ss |
| currentPage | integer | 页码 | 1 | ≥1 |
| pageSize | integer | 每页条数 | 100 | 1~1000 |
| exportFormat | string | 输出格式 | "markdown" | "markdown", "excel" |

## 工具调用 Schema

```json
{
  "name": "project_deployment_query",
  "description": "查询项目部署信息，包括部署记录、部署环境、状态、版本、负责人、耗时等",
  "parameters": {
    "type": "object",
    "properties": {
      "projectName": {
        "type": "string",
        "description": "项目名称（模糊匹配）"
      },
      "deployEnv": {
        "type": "integer",
        "description": "部署环境，1=测试, 2=灰度, 3=生产, 4=研发",
        "enum": [1, 2, 3, 4]
      },
      "status": {
        "type": "integer",
        "description": "部署状态，0=成功, 1=失败, 2=进行中, 3=待部署",
        "enum": [0, 1, 2, 3]
      },
      "startTime": {
        "type": "string",
        "description": "开始时间，格式 YYYY-MM-DD HH:mm:ss"
      },
      "endTime": {
        "type": "string",
        "description": "结束时间，格式 YYYY-MM-DD HH:mm:ss"
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
        "maximum": 1000
      },
      "exportFormat": {
        "type": "string",
        "description": "输出格式",
        "enum": ["markdown", "excel"]
      }
    },
    "required": []
  }
}
```

## 参数映射规则

### 部署环境映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "测试"、"测试环境" | deployEnv | 1 |
| "灰度"、"灰度环境" | deployEnv | 2 |
| "生产"、"生产环境" | deployEnv | 3 |
| "研发"、"研发环境" | deployEnv | 4 |

### 部署状态映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "成功"、"部署成功" | status | 0 |
| "失败"、"部署失败" | status | 1 |
| "进行中"、"部署中" | status | 2 |
| "待部署"、"待发布" | status | 3 |

### 时间范围映射

| 用户输入模式 | 目标参数 | 提取规则 |
|--------------|----------|----------|
| "从A到B" | startTime, endTime | A为开始时间，B为结束时间 |
| "今天"、"今日" | startTime, endTime | 当天00:00:00到23:59:59 |
| "最近N天" | startTime | 当前时间往前推N天 |

### 数量词识别规则

| 用户输入 | 对应的 pageSize |
|----------|----------------|
| "最近的"、"最新的" | 10 |
| "所有"、"全部" | 1000 |
| "一些"、"几条" | 100 (默认) |

## API 接口

### 接口地址

**生产环境**：POST https://oss.tech.ctseelink.cn/a/cmdb/baseProject/

### 请求格式

```json
{
  "name": "guizh-rules-api",
  "currentPage": 1,
  "pageSize": 100
}
```

### 请求参数说明

| 参数名 | 说明 |
|--------|------|
| name | 项目名称（映射自 projectName） |
| deployEnv | 部署环境编码 |
| status | 部署状态编码 |
| startTime | 开始时间 |
| endTime | 结束时间 |
| currentPage | 页码 |
| pageSize | 每页条数 |

### 参数映射（用户输入 → API 参数）

| 用户参数 | API 参数 |
|----------|----------|
| projectName | name |
| deployEnv | deployEnv |
| status | status |
| startTime | startTime |
| endTime | endTime |
| currentPage | currentPage |
| pageSize | pageSize |

### 响应格式

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [
      {
        "id": "deploy-001",
        "projectName": "guizh-rules-api",
        "environment": "生产",
        "environmentCode": 3,
        "deploymentStatus": "成功",
        "statusCode": 0,
        "startTime": "2026-05-12 10:00:00",
        "endTime": "2026-05-12 10:05:30",
        "duration": 330,
        "deployer": "张三",
        "version": "v1.2.3",
        "gitBranch": "main"
      }
    ],
    "total": 100,
    "currentPage": 1,
    "pageSize": 100
  }
}
```

### 响应字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| code | integer | 状态码，200 表示成功 |
| message | string | 响应消息 |
| data | object | 响应数据体 |
| data.records | array | 部署记录列表 |
| data.total | integer | 总记录数 |
| data.currentPage | integer | 当前页码 |
| data.pageSize | integer | 每页条数 |

### records 中的每条记录

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string | 部署记录唯一 ID |
| projectName | string | 项目名称 |
| environment | string | 部署环境（中文） |
| environmentCode | integer | 环境编码 |
| deploymentStatus | string | 部署状态（中文） |
| statusCode | integer | 状态编码 |
| startTime | string | 部署开始时间 |
| endTime | string | 部署结束时间 |
| duration | integer | 耗时（秒） |
| deployer | string | 负责人 |
| version | string | 部署版本 |
| gitBranch | string | Git 分支 |
| gitCommit | string | Git 提交号 |
| serverList | array | 部署服务器列表 |
| errorMessage | string | 失败时的错误信息 |
| logUrl | string | 部署日志 URL |
| progress | integer | 进行中的进度百分比 |

## 状态码映射

### 环境编码

| 编码 | 中文 | 含义 |
|------|------|------|
| 1 | 测试 | 测试环境 |
| 2 | 灰度 | 灰度环境 |
| 3 | 生产 | 生产环境 |
| 4 | 研发 | 研发环境 |

### 部署状态编码

| 编码 | 中文 | 含义 |
|------|------|------|
| 0 | 成功 | 部署成功 |
| 1 | 失败 | 部署失败 |
| 2 | 进行中 | 部署进行中 |
| 3 | 待部署 | 等待部署 |

## 输出格式

### 标准输出（简单模式 - 默认）

```
## 部署记录查询结果

**查询条件**：{用户原始查询}

**匹配技能**：项目部署查询

**查询范围**：项目=guizh-rules-api，环境=生产（显示前10条）

---

**结果摘要**：共查询到 100 条部署记录，当前显示前 10 条

---

| 项目名称 | 环境 | 状态 | 部署时间 | 版本 |
|----------|------|------|----------|------|
| guizh-rules-api | 生产 | 成功 | 2026-05-12 10:00:00 | v1.2.3 |
| guizh-yapi | 测试 | 失败 | 2026-05-11 15:30:00 | v2.1.0 |
| ... | ... | ... | ... | ... |

---

💡 您可以说：
- "查看详细信息" - 显示负责人、耗时、分支等完整信息
- "下一页" - 查看更多部署记录
- "查看全部" - 显示所有查询结果
```

### 详细输出（用户要求"查看详细信息"时）

```
## 部署记录查询结果（详细）

**查询条件**：{用户原始查询}

**匹配技能**：项目部署查询

**查询范围**：项目=guizh-rules-api，环境=生产

---

**结果摘要**：共查询到 100 条部署记录，当前显示前 10 条

---

| 项目名称 | 环境 | 状态 | 部署时间 | 版本 | 负责人 | 耗时 | Git分支 |
|----------|------|------|----------|------|--------|------|---------|
| guizh-rules-api | 生产 | 成功 | 2026-05-12 10:00:00 | v1.2.3 | 张三 | 5分30秒 | main |
| guizh-yapi | 测试 | 失败 | 2026-05-11 15:30:00 | v2.1.0 | 李四 | 5分0秒 | release/v2.1 |
| ... | ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于部署系统。
```

### 降级输出（API 失败时）

```
## 部署记录查询结果

> ⚠️ 提示：API 接口调用失败，已切换为模拟数据

**查询条件**：{用户原始查询}

**匹配技能**：项目部署查询

**查询参数**：{"projectName":"guizh-rules-api","deployEnv":3,"pageSize":100}

---

**结果摘要**：共查询到 N 条部署记录（模拟数据）

---

| 项目名称 | 环境 | 状态 | 部署时间 | 版本 | 负责人 | 耗时 |
|----------|------|------|----------|------|--------|------|
| guizh-rules-api | 生产 | 成功 | 2026-05-12 10:00:00 | v1.2.3 | 张三 | 5分30秒 |
| ... | ... | ... | ... | ... | ... | ... |

---

**说明**：当前显示的是模拟数据，真实数据需要部署服务可用时才能获取。
```

## 完整调用示例

### 示例 1：按项目名称查询

**用户输入**："查 guizh-rules-api 的部署记录"

**工具调用**：
```json
{
  "tool_name": "project_deployment_query",
  "parameters": {
    "projectName": "guizh-rules-api",
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**API 请求**：
```json
{
  "name": "guizh-rules-api",
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
        "id": "deploy-001",
        "projectName": "guizh-rules-api",
        "environment": "生产",
        "environmentCode": 3,
        "deploymentStatus": "成功",
        "statusCode": 0,
        "startTime": "2026-05-12 10:00:00",
        "endTime": "2026-05-12 10:05:30",
        "duration": 330,
        "deployer": "张三",
        "version": "v1.2.3",
        "gitBranch": "main"
      }
    ],
    "total": 100,
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**最终输出**：
```
## 部署记录查询结果

**查询条件**：查 guizh-rules-api 的部署记录

**匹配技能**：项目部署查询

**查询参数**：{"projectName":"guizh-rules-api","pageSize":100}

---

**结果摘要**：共查询到 100 条部署记录

---

| 项目名称 | 环境 | 状态 | 部署时间 | 版本 | 负责人 | 耗时 |
|----------|------|------|----------|------|--------|------|
| guizh-rules-api | 生产 | 成功 | 2026-05-12 10:00:00 | v1.2.3 | 张三 | 5分30秒 |
| ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于部署系统。
```

### 示例 2：按环境和状态查询

**用户输入**："查询生产环境失败的部署"

**工具调用**：
```json
{
  "tool_name": "project_deployment_query",
  "parameters": {
    "deployEnv": 3,
    "status": 1,
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**API 请求**：
```json
{
  "deployEnv": 3,
  "status": 1,
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
        "id": "deploy-002",
        "projectName": "guizh-yapi",
        "environment": "生产",
        "environmentCode": 3,
        "deploymentStatus": "失败",
        "statusCode": 1,
        "startTime": "2026-05-11 15:30:00",
        "endTime": "2026-05-11 15:35:00",
        "duration": 300,
        "deployer": "李四",
        "version": "v2.1.0",
        "gitBranch": "release/v2.1",
        "errorMessage": "服务启动超时"
      }
    ],
    "total": 5,
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**最终输出**：
```
## 部署记录查询结果

**查询条件**：查询生产环境失败的部署

**匹配技能**：项目部署查询

**查询参数**：{"deployEnv":3,"status":1,"pageSize":100}

---

**结果摘要**：共查询到 5 条部署记录

---

| 项目名称 | 环境 | 状态 | 部署时间 | 版本 | 负责人 | 耗时 | 错误信息 |
|----------|------|------|----------|------|--------|------|----------|
| guizh-yapi | 生产 | 失败 | 2026-05-11 15:30:00 | v2.1.0 | 李四 | 5分0秒 | 服务启动超时 |
| ... | ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于部署系统。
```

## 错误处理

### 错误场景 1：缺少查询条件

**输入**："查询部署记录"

**输出**：
```
我需要更多信息来完成查询。

请告诉我您想查询哪个项目的部署记录？例如：guizh-rules-api

您也可以按环境查询，如"查询生产环境的部署"
```

### 错误场景 2：参数格式错误

**输入**："查询2026-13-01到2026-13-02的部署"

**输出**：
```
## 查询失败

**错误类型**：参数格式错误

**错误原因**：日期格式错误，月份不能超过12

**建议**：请使用正确的日期格式，如"2026-05-10到2026-05-12"
```

### 错误场景 2：API 调用失败

**输出**：
```
## 查询失败

**错误类型**：网络错误

**错误原因**：部署服务响应失败，请稍后重试

**建议**：
1. 检查网络连接
2. 稍后重新查询
3. 如问题持续，请联系管理员
```

### 错误场景 3：无查询结果

**输入**："查询不存在的项目部署记录"

**输出**：
```
## 查询结果

**查询条件**：查询不存在的项目部署记录

**匹配技能**：项目部署查询

**查询参数**：{"projectName":"不存在的项目","pageSize":100}

---

**结果摘要**：未查询到符合条件的部署记录

---

**说明**：当前系统中不存在该项目的部署记录。
```

## Mock 数据（API 降级备用）

当 API 不可用时，使用以下模拟数据返回：

```json
{
  "code": 200, "message": "success",
  "data": {
    "records": [
      {
        "id": "deploy-001", "projectName": "guizh-rules-api",
        "environment": "生产", "environmentCode": 3,
        "deploymentStatus": "成功", "statusCode": 0,
        "startTime": "2026-05-12 10:00:00", "endTime": "2026-05-12 10:05:30",
        "duration": 330, "deployer": "张三", "version": "v1.2.3",
        "gitBranch": "main",
        "serverList": [
          {"hostName": "prod-guizhou-app-01", "ip": "192.168.7.101", "status": "成功"},
          {"hostName": "prod-guizhou-app-02", "ip": "192.168.7.102", "status": "成功"}
        ]
      },
      {
        "id": "deploy-002", "projectName": "guizh-yapi",
        "environment": "生产", "environmentCode": 3,
        "deploymentStatus": "失败", "statusCode": 1,
        "startTime": "2026-05-11 15:30:00", "endTime": "2026-05-11 15:35:00",
        "duration": 300, "deployer": "李四", "version": "v2.1.0",
        "gitBranch": "release/v2.1",
        "errorMessage": "服务启动超时"
      },
      {
        "id": "deploy-003", "projectName": "guizh-rules-api",
        "environment": "测试", "environmentCode": 1,
        "deploymentStatus": "成功", "statusCode": 0,
        "startTime": "2026-05-10 09:00:00", "endTime": "2026-05-10 09:02:00",
        "duration": 120, "deployer": "王五", "version": "v1.2.2",
        "gitBranch": "develop"
      }
    ],
    "total": 3, "size": 40, "current": 1, "pages": 1
  }
}
```
