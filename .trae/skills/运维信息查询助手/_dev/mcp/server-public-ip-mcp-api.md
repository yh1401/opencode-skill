# 服务器公网IP查询 API 文档

## API 信息

- **端点**: `POST https://oss.tech.ctseelink.cn/a/cmdb/serverPublicIp/`
- **Content-Type**: `application/x-www-form-urlencoded`
- **认证**: 需要登录获取 token

## 请求格式

```
node=云公司->贵州&ip=192.168.7.101
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
        "公网ip": "113.12.13.14",
        "公网ipv6": "2409:8c00:1234:5678::1",
        "ip": "192.168.7.101",
        "vip": "",
        "机房": "云公司->贵州",
        "内网映射端口": "8080",
        "公网映射端口": "80",
        "共享带宽id": "bw-shared-001",
        "带宽": "100Mbps",
        "带宽类型": "独享",
        "监控计费类型": "按带宽"
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

- [服务器公网IP查询子技能](../skills/server-public-ip-query/SKILL.md)
- [字段映射表](../skills/server-public-ip-query/references/public-ip-fields.md)