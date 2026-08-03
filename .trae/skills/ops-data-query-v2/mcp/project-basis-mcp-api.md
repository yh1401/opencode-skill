# 工程项目查询 API 文档

## API 信息

- **端点**: `POST https://oss.tech.ctseelink.cn/a/cmdb/baseProjectBasis/`
- **Content-Type**: `application/x-www-form-urlencoded`
- **认证**: 需要登录获取 token

## 请求格式

```
projectName=tykj-kafka-test&productName=天翼看家
```

## 响应格式

### 成功响应

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [
      {
        "工程项目名": "tykj-kafka-test",
        "中文名": "天翼看家Kafka测试",
        "所属产品": "天翼看家",
        "svngit路径": "svn://svn.tech.ctseelink.cn/tykj/kafka-test",
        "项目描述": "Kafka消息队列测试项目",
        "项目类型": "中间件",
        "父项目": "tykj-base",
        "分组": "消息队列组",
        "自定义字段": "",
        "操作": "编辑|删除"
      }
    ],
    "total": 1,
    "size": 40,
    "current": 1,
    "pages": 1
  }
}
```

### 错误响应

```json
{
  "code": 500,
  "message": "Internal server error",
  "data": null
}
```

## 相关文档

- [工程项目查询子技能](../skills/project-basis-query/SKILL.md)
- [字段映射表](../skills/project-basis-query/references/project-basis-fields.md)