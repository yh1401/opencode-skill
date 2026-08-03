# 工程项目信息查询 - 接口规范

## 1. 技能基本信息

| 项 | 值 |
|---|---|
| **技能ID** | project-basis-query |
| **技能名称** | 工程项目信息查询 |
| **API 地址** | POST /api/project/basis/query |

## 2. 请求参数

| 参数名 | 类型 | 必填 | 说明 |
|--------|------|------|------|
| id | string | 否 | 项目唯一标识 |
| name | string | 否 | 工程项目名称（模糊匹配） |
| productId | string | 否 | 产品ID |
| productName | string | 否 | 产品名称（模糊匹配） |
| page | int | 否 | 页码，默认1 |
| pageSize | int | 否 | 每页数量，默认40，范围1-100 |

## 3. 响应字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| 工程项目名 | string | 项目名称 |
| 中文名 | string | 中文名称 |
| 所属产品 | string | 产品名称 |
| svngit路径 | string | 代码仓库路径 |
| 项目描述 | string | 项目描述 |

## 4. 用户输入映射

用户自然语言 → API 参数：

| 用户表达 | 对应参数 | 示例 |
|----------|---------|------|
| 项目名称、项目、项目名 | name | "tykj-kafka-test" |
| 产品ID | productId | "1661fea29df44946ab2ff2bb9577179f" |
| 产品名称、产品、所属产品 | productName | "天翼看家" |

## 5. 返回数据格式

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [...],
    "total": 100,
    "current": 1,
    "pages": 10
  }
}
```
