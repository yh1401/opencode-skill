# CMDB API 文档

## API 信息

- **端点**: `POST https://oss.tech.ctseelink.cn/api/v2/cmdbServer/getCmdbServerBaseMessageList`
- **Content-Type**: `application/json`
- **认证**: 需要登录获取 token

## 请求格式

```json
{
  "currentPage": 1,
  "pageSize": 100,
  "nodeIpParamDtoList": [
    {
      "node": "云公司->贵州",
      "ip": "192.168.7.201"
    }
  ],
  "productNameList": ["规则引擎平台"],
  "projectBasisNameList": ["guizh-rules-api"],
  "projectNameList": ["guizh-yapi"],
  "projectServerType": 3,
  "stateList": [0]
}
```

## 响应格式

### 成功响应

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 1000,
    "pageNum": 1,
    "pageSize": 100,
    "list": [
      {
        "id": "server-001",
        "hostName": "prod-guizhou-web-01",
        "node": "云公司->贵州",
        "ip": "192.168.7.201",
        "cpuCores": 32,
        "memory": 128,
        "state": "在线",
        "projectServerType": "生产",
        "serverType": "GPU主机",
        "manufacturer": "Huawei",
        "model": "2288H V5",
        "os": "CentOS Linux 7.9",
        "usingBy": "规则引擎平台",
        "owner": "基础平台部",
        "dockerized": "20.10.12",
        "zabbix": "1",
        "agent": "1",
        "gpus": 2,
        "productName": "规则引擎平台",
        "projectName": "guizh-yapi"
      }
    ]
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

## 状态值说明

| 值 | 状态 |
|----|------|
| 0 | 在线 |
| 1 | 库存 |
| 2 | 计划上线 |
| 3 | 维修中 |
| 4 | 已报废 |
| 5 | 待分配 |

## 环境值说明

| 值 | 环境 |
|----|------|
| 1 | 测试 |
| 2 | 灰度 |
| 3 | 生产 |
| 4 | 研发 |

## 相关文档

- [CMDB 子技能](../skills/cmdb-server-query/SKILL.md)
- [字段映射表](../skills/cmdb-server-query/references/cmdb-fields.md)
- [操作符参考](../skills/cmdb-server-query/references/cmdb-operators.md)
- [参数转换](../skills/cmdb-server-query/references/cmdb-json-to-params.md)