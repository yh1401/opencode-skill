# 产品查询 API 文档

## API 信息

- **端点**: `POST https://oss.tech.ctseelink.cn/a/cmdb/baseProduct/`
- **Content-Type**: `application/x-www-form-urlencoded`
- **认证**: 需要登录获取 token

## 请求格式

```
productName=规则引擎平台&enabled=true
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
        "产品名": "规则引擎平台",
        "产品主贡": "规则引擎",
        "上级产品": "天翼云",
        "产品级别": "一级",
        "启用": "是",
        "所属单位/部门": "云公司",
        "产品经理": "张三",
        "运维负责人": "李四"
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

- [产品查询子技能](../skills/product-query/SKILL.md)
- [字段映射表](../skills/product-query/references/product-fields.md)