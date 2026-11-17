---
name: product-query
description: 查询产品信息，包括产品名称、产品级别、所属部门、产品经理、运维负责人等。支持按产品名称、ID、启用状态等条件查询。
version: 2.0.0
author: Skill Agent Team
---

# 产品查询技能

## 功能描述

本技能用于查询产品的基本信息，包括：
- 产品名称、产品功能/主责
- 产品级别、上级产品
- 所属单位/部门
- 产品经理、运维负责人
- 启用状态

## 触发条件

当用户查询包含以下关键词时触发：
- 产品信息、产品列表、产品负责人
- 产品名称、产品ID、产品线

**排他条件**：
- 如果用户查询的是项目部署信息，应触发 `project-deployment-query` 技能
- 如果用户查询的是服务器配置信息，应触发 `cmdb-server-query` 技能

## 输入参数

### 必填参数

无

### 可选参数

| 参数名 | 类型 | 说明 | 默认值 | 可选值 |
|--------|------|------|--------|--------|
| id | string | 产品 ID | - | - |
| name | string | 产品名称（模糊匹配） | - | - |
| flag | integer | 启用标志 | - | 0=禁用, 1=启用 |
| currentPage | integer | 页码 | 1 | ≥1 |
| pageSize | integer | 每页条数 | 40 | 1~100 |

## 工具调用 Schema

```json
{
  "name": "product_query",
  "description": "查询产品信息，包括产品名称、产品级别、所属部门、产品经理、运维负责人等",
  "parameters": {
    "type": "object",
    "properties": {
      "id": {
        "type": "string",
        "description": "产品ID"
      },
      "name": {
        "type": "string",
        "description": "产品名称（模糊匹配）"
      },
      "flag": {
        "type": "integer",
        "description": "启用标志，0=禁用, 1=启用",
        "enum": [0, 1]
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

### 启用状态映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "启用"、"启用状态"、"启用中" | flag | 1 |
| "禁用"、"停用"、"未启用" | flag | 0 |

### 数量词识别规则

| 用户输入 | 对应的 pageSize |
|----------|----------------|
| "最近的"、"最新的" | 10 |
| "所有"、"全部" | 100 |
| "一些"、"几条" | 40 (默认) |

## API 接口

### 接口地址

**生产环境**：POST https://oss.tech.ctseelink.cn/a/cmdb/baseProduct/

### 请求格式

```json
{
  "name": "天翼看家",
  "currentPage": 1,
  "pageSize": 40
}
```

### 请求参数说明

| 参数名 | 说明 |
|--------|------|
| id | 产品 ID |
| name | 产品名称（模糊匹配） |
| flag | 启用标志：0=禁用, 1=启用 |
| currentPage | 页码 |
| pageSize | 每页条数 |

### 参数映射（用户输入 → API 参数）

| 用户参数 | API 参数 |
|----------|----------|
| id | id |
| name | name |
| flag | flag |
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
        "productName": "规则引擎平台",
        "productFunction": "规则引擎",
        "parentProduct": "天翼云",
        "productLevel": "一级",
        "enabled": "是",
        "department": "云公司",
        "productManager": "张三",
        "opsLead": "李四"
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
| data.records | array | 产品记录列表 |
| data.total | integer | 总记录数 |
| data.current | integer | 当前页码 |
| data.size | integer | 每页条数 |
| data.pages | integer | 总页数 |

### records 中的每条记录

| 字段 | 类型 | 说明 |
|------|------|------|
| productName | string | 产品名称 |
| productFunction | string | 产品功能/主责 |
| parentProduct | string | 上级产品 |
| productLevel | string | 产品级别，如 "一级"、"二级" |
| enabled | string | 启用状态，"是" 或 "否" |
| department | string | 所属单位/部门 |
| productManager | string | 产品经理 |
| opsLead | string | 运维负责人 |

## 输出格式

### 标准输出（简单模式 - 默认）

```
## 产品查询结果

**查询条件**：{用户原始查询}

**匹配技能**：产品查询

**查询范围**：产品名称=天翼看家（显示前10条）

---

**结果摘要**：共查询到 100 条产品记录，当前显示前 10 条

---

| 产品名称 | 上级产品 | 级别 | 状态 |
|----------|----------|------|------|
| 规则引擎平台 | 天翼云 | 一级 | 启用 |
| 天翼看家 | 天翼智家 | 二级 | 启用 |
| ... | ... | ... | ... |

---

💡 您可以说：
- "查看详细信息" - 显示部门、产品经理、运维负责人等完整信息
- "下一页" - 查看更多产品
- "查看全部" - 显示所有查询结果
```

### 详细输出（用户要求"查看详细信息"时）

```
## 产品查询结果（详细）

**查询条件**：{用户原始查询}

**匹配技能**：产品查询

**查询范围**：产品名称=天翼看家

---

**结果摘要**：共查询到 100 条产品记录，当前显示前 10 条

---

| 产品名称 | 上级产品 | 级别 | 所属部门 | 产品经理 | 运维负责人 | 状态 |
|----------|----------|------|----------|----------|------------|------|
| 规则引擎平台 | 天翼云 | 一级 | 云公司 | 张三 | 李四 | 启用 |
| 天翼看家 | 天翼智家 | 二级 | 云公司 | 王五 | 赵六 | 启用 |
| ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 CMDB 系统。
```

### 降级输出（API 失败时）

```
## 产品查询结果

> ⚠️ 提示：API 接口调用失败，已切换为模拟数据

**查询条件**：{用户原始查询}

**匹配技能**：产品查询

**查询参数**：{"name":"天翼看家","pageSize":40}

---

**结果摘要**：共查询到 N 条产品记录（模拟数据）

---

| 产品名称 | 上级产品 | 级别 | 所属部门 | 产品经理 | 运维负责人 | 状态 |
|----------|----------|------|----------|----------|------------|------|
| 规则引擎平台 | 天翼云 | 一级 | 云公司 | 张三 | 李四 | 启用 |
| ... | ... | ... | ... | ... | ... | ... |

---

**说明**：当前显示的是模拟数据，真实数据需要部署服务可用时才能获取。
```

## 完整调用示例

### 示例 1：按产品名称查询

**用户输入**："查一下天翼看家的产品信息"

**工具调用**：
```json
{
  "tool_name": "product_query",
  "parameters": {
    "name": "天翼看家",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**API 请求**：
```json
{
  "name": "天翼看家",
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
        "productName": "天翼看家",
        "productFunction": "视频监控",
        "parentProduct": "天翼智家",
        "productLevel": "二级",
        "enabled": "是",
        "department": "云公司",
        "productManager": "王五",
        "opsLead": "赵六"
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
## 产品查询结果

**查询条件**：查一下天翼看家的产品信息

**匹配技能**：产品查询

**查询参数**：{"name":"天翼看家","pageSize":40}

---

**结果摘要**：共查询到 1 条产品记录

---

| 产品名称 | 上级产品 | 级别 | 所属部门 | 产品经理 | 运维负责人 | 状态 |
|----------|----------|------|----------|----------|------------|------|
| 天翼看家 | 天翼智家 | 二级 | 云公司 | 王五 | 赵六 | 启用 |

---

**说明**：数据来源于 CMDB 系统。
```

### 示例 2：查询所有启用的产品

**用户输入**："查询所有启用的产品"

**工具调用**：
```json
{
  "tool_name": "product_query",
  "parameters": {
    "flag": 1,
    "currentPage": 1,
    "pageSize": 100
  }
}
```

**API 请求**：
```json
{
  "flag": 1,
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
        "productName": "规则引擎平台",
        "productFunction": "规则引擎",
        "parentProduct": "天翼云",
        "productLevel": "一级",
        "enabled": "是",
        "department": "云公司",
        "productManager": "张三",
        "opsLead": "李四"
      },
      {
        "productName": "天翼看家",
        "productFunction": "视频监控",
        "parentProduct": "天翼智家",
        "productLevel": "二级",
        "enabled": "是",
        "department": "云公司",
        "productManager": "王五",
        "opsLead": "赵六"
      }
    ],
    "total": 100,
    "current": 1,
    "size": 100,
    "pages": 1
  }
}
```

**最终输出**：
```
## 产品查询结果

**查询条件**：查询所有启用的产品

**匹配技能**：产品查询

**查询参数**：{"flag":1,"pageSize":100}

---

**结果摘要**：共查询到 100 条产品记录

---

| 产品名称 | 上级产品 | 级别 | 所属部门 | 产品经理 | 运维负责人 | 状态 |
|----------|----------|------|----------|----------|------------|------|
| 规则引擎平台 | 天翼云 | 一级 | 云公司 | 张三 | 李四 | 启用 |
| 天翼看家 | 天翼智家 | 二级 | 云公司 | 王五 | 赵六 | 启用 |
| ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 CMDB 系统。
```

## 错误处理

### 错误场景 1：参数格式错误

**输入**："查询无效的产品 ID"

**输出**：
```
## 查询失败

**错误类型**：参数格式错误

**错误原因**：产品 ID 格式不正确

**建议**：请输入有效的产品 ID，如"prod-001"或完整的 UUID
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

**输入**："查询不存在的产品"

**输出**：
```
## 查询结果

**查询条件**：查询不存在的产品

**匹配技能**：产品查询

**查询参数**：{"name":"不存在的产品","pageSize":40}

---

**结果摘要**：未查询到符合条件的产品记录

---

**说明**：当前系统中不存在该产品信息。
```

## Mock 数据（API 降级备用）

当 API 不可用时，使用以下模拟数据返回：

```json
{
  "code": 200, "message": "success",
  "data": {
    "records": [
      {
        "productName": "规则引擎平台", "productFunction": "规则引擎",
        "parentProduct": "天翼云", "productLevel": "一级",
        "enabled": "是", "department": "云公司",
        "productManager": "张三", "opsLead": "李四"
      },
      {
        "productName": "天翼看家", "productFunction": "视频监控",
        "parentProduct": "天翼智家", "productLevel": "二级",
        "enabled": "是", "department": "云公司",
        "productManager": "王五", "opsLead": "赵六"
      },
      {
        "productName": "5G工业视宽平台", "productFunction": "5G应用",
        "parentProduct": "5G产品", "productLevel": "一级",
        "enabled": "是", "department": "省公司",
        "productManager": "钱七", "opsLead": "孙八"
      },
      {
        "productName": "边缘计算平台", "productFunction": "边缘计算",
        "parentProduct": "天翼云", "productLevel": "二级",
        "enabled": "是", "department": "云公司",
        "productManager": "周九", "opsLead": "吴十"
      }
    ],
    "total": 4, "size": 40, "current": 1, "pages": 1
  }
}
```
