# 项目部署查询 API 文档

## API 信息

- **端点**: `POST https://oss.tech.ctseelink.cn/a/cmdb/baseProject/`
- **Content-Type**: `application/x-www-form-urlencoded`
- **认证**: 需要登录获取 token

## 状态值说明

| 值 | 状态 |
|----|------|
| 0 | 成功 |
| 1 | 失败 |
| 2 | 进行中 |
| 3 | 待部署 |

## 环境值说明

| 值 | 环境 |
|----|------|
| 1 | 测试 |
| 2 | 灰度 |
| 3 | 生产 |
| 4 | 研发 |

## 请求格式

```
projectName=guizh-rules-api&environment=3
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
        "部署项目名": "guizh-rules-api",
        "整机项目名": "guizh-rules",
        "工程项目名": "tykj-rules-test",
        "所属产品": "规则引擎平台",
        "部署环境类型": "传统",
        "所在机房": "云公司->贵州",
        "研发": "张三",
        "负责人A": "李四",
        "负责人B": "王五",
        "反应地址": "http://192.168.7.101:8080",
        "程序包": "guizh-rules-api-2.5.1.jar"
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

- [项目部署查询子技能](../skills/project-deployment-query/SKILL.md)
- [字段映射表](../skills/project-deployment-query/references/deployment-fields.md)