# Mock 数据文档

本文件提供五个子技能调用接口的模拟返回数据，供开发和测试使用。

---

## 1. 服务器查询接口

**接口地址**: `POST /api/v2/cmdbServer/getCmdbServerPageList`

**Mock 返回数据**:
```json
{
  "code": 200,
  "message": "success",
  "fail": false,
  "data": {
    "records": [
      {
        "id": "srv-001",
        "hostName": "gz-server-01",
        "ip": "192.168.7.101",
        "publicIp": "113.12.13.14",
        "vip": "",
        "node": "云公司->贵州",
        "state": "0",
        "serverType": "物理机",
        "cpuCores": "32",
        "memory": "128",
        "os": "CentOS 7.9",
        "environment": "生产",
        "productName": "规则引擎平台",
        "projectName": "guizh-rules-api",
        "operA": "张三",
        "operB": "李四",
        "assetNo": "SN2024001",
        "rack": "R01-A01",
        "bandWidth": "100Mbps"
      },
      {
        "id": "srv-002",
        "hostName": "gz-server-02",
        "ip": "192.168.7.102",
        "publicIp": "113.12.13.15",
        "vip": "10.0.0.1",
        "node": "云公司->贵州",
        "state": "0",
        "serverType": "虚拟机",
        "cpuCores": "16",
        "memory": "64",
        "os": "Ubuntu 20.04",
        "environment": "测试",
        "productName": "天翼看家",
        "projectName": "tykj-kafka-test",
        "operA": "王五",
        "operB": "赵六",
        "assetNo": "SN2024002",
        "rack": "R01-A02",
        "bandWidth": "50Mbps"
      },
      {
        "id": "srv-003",
        "hostName": "sh-server-01",
        "ip": "192.168.8.101",
        "publicIp": "114.25.36.47",
        "vip": "",
        "node": "省公司->上海",
        "state": "0",
        "serverType": "物理机",
        "cpuCores": "48",
        "memory": "256",
        "os": "CentOS 8.2",
        "environment": "生产",
        "productName": "5G工业视宽平台",
        "projectName": "5g-industry-api",
        "operA": "钱七",
        "operB": "孙八",
        "assetNo": "SN2024003",
        "rack": "R02-B01",
        "bandWidth": "200Mbps"
      },
      {
        "id": "srv-004",
        "hostName": "xj-server-01",
        "ip": "192.168.9.101",
        "publicIp": "115.36.47.58",
        "vip": "10.0.0.2",
        "node": "省公司->新疆乌鲁木齐",
        "state": "1",
        "serverType": "物理机",
        "cpuCores": "24",
        "memory": "64",
        "os": "CentOS 7.9",
        "environment": "灰度",
        "productName": "边缘计算平台",
        "projectName": "edge-compute",
        "operA": "周九",
        "operB": "吴十",
        "assetNo": "SN2024004",
        "rack": "R03-C01",
        "bandWidth": "100Mbps"
      }
    ],
    "total": 4,
    "size": 15,
    "current": 1,
    "pages": 1
  }
}
```

---

## 2. 服务器公网IP查询接口

**接口地址**: `POST /a/cmdb/serverPublicIp/`

**Mock 返回数据**:
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [
      {
        "publicIp": "113.12.13.14",
        "publicIpv6": "2409:8c00:1234:5678::1",
        "ip": "192.168.7.101",
        "vip": "",
        "node": "云公司->贵州",
        "privatePort": "8080",
        "publicPort": "80",
        "sharedBandwidthId": "bw-shared-001",
        "bandwidth": "100Mbps",
        "bandwidthType": "独享",
        "billingType": "按带宽"
      },
      {
        "publicIp": "113.12.13.15",
        "publicIpv6": "2409:8c00:1234:5678::2",
        "ip": "192.168.7.102",
        "vip": "10.0.0.1",
        "node": "云公司->贵州",
        "privatePort": "9090",
        "publicPort": "443",
        "sharedBandwidthId": "bw-shared-001",
        "bandwidth": "50Mbps",
        "bandwidthType": "共享",
        "billingType": "按流量计费"
      },
      {
        "publicIp": "114.25.36.47",
        "publicIpv6": "",
        "ip": "192.168.8.101",
        "vip": "",
        "node": "省公司->上海",
        "privatePort": "8081",
        "publicPort": "8080",
        "sharedBandwidthId": "bw-shared-002",
        "bandwidth": "200Mbps",
        "bandwidthType": "独享",
        "billingType": "按带宽"
      }
    ],
    "total": 3,
    "size": 40,
    "current": 1,
    "pages": 1
  }
}
```

---

## 3. 产品查询接口

**接口地址**: `POST /a/cmdb/baseProduct/`

**Mock 返回数据**:
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
      },
      {
        "productName": "5G工业视宽平台",
        "productFunction": "5G应用",
        "parentProduct": "5G产品",
        "productLevel": "一级",
        "enabled": "是",
        "department": "省公司",
        "productManager": "钱七",
        "opsLead": "孙八"
      },
      {
        "productName": "边缘计算平台",
        "productFunction": "边缘计算",
        "parentProduct": "天翼云",
        "productLevel": "二级",
        "enabled": "是",
        "department": "云公司",
        "productManager": "周九",
        "opsLead": "吴十"
      }
    ],
    "total": 4,
    "size": 40,
    "current": 1,
    "pages": 1
  }
}
```

---

## 4. 项目部署信息接口

**接口地址**: `POST /a/cmdb/baseProject/`

**Mock 返回数据**:
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
        "gitBranch": "main",
        "serverList": [
          {"hostName": "prod-guizhou-app-01", "ip": "192.168.7.101", "status": "成功"},
          {"hostName": "prod-guizhou-app-02", "ip": "192.168.7.102", "status": "成功"}
        ]
      },
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
      },
      {
        "id": "deploy-003",
        "projectName": "guizh-rules-api",
        "environment": "测试",
        "environmentCode": 1,
        "deploymentStatus": "成功",
        "statusCode": 0,
        "startTime": "2026-05-10 09:00:00",
        "endTime": "2026-05-10 09:02:00",
        "duration": 120,
        "deployer": "王五",
        "version": "v1.2.2",
        "gitBranch": "develop"
      }
    ],
    "total": 3,
    "size": 40,
    "current": 1,
    "pages": 1
  }
}
```

---

## 5. 工程项目信息接口

**接口地址**: `POST /a/cmdb/baseProjectBasis/`

**Mock 返回数据**:
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
      },
      {
        "projectName": "guizh-rules-basis",
        "chineseName": "规则引擎基础项目",
        "productName": "规则引擎平台",
        "repoPath": "git@git.tech.ctseelink.cn:guizh/rules-basis.git",
        "description": "规则引擎核心基础组件",
        "projectType": "核心组件",
        "parentProject": "guizh-base",
        "group": "规则引擎组"
      },
      {
        "projectName": "5g-industry-basis",
        "chineseName": "5G工业视宽基础项目",
        "productName": "5G工业视宽平台",
        "repoPath": "git@git.tech.ctseelink.cn:5g/industry-basis.git",
        "description": "5G工业视宽平台基础组件",
        "projectType": "核心组件",
        "parentProject": "5g-base",
        "group": "5G应用组"
      },
      {
        "projectName": "edge-compute-basis",
        "chineseName": "边缘计算基础项目",
        "productName": "边缘计算平台",
        "repoPath": "svn://svn.tech.ctseelink.cn/edge/compute-basis",
        "description": "边缘计算核心组件",
        "projectType": "核心组件",
        "parentProject": "edge-base",
        "group": "边缘计算组"
      }
    ],
    "total": 4,
    "size": 40,
    "current": 1,
    "pages": 1
  }
}
```

---

## 使用说明

1. **引用方式**: 子技能在需要调用接口时，可参考本文件中的mock数据格式进行测试
2. **数据特点**: 每个接口返回3-5条模拟数据，符合接口文档定义的字段结构
3. **非侵入性**: 本文件仅作为参考数据，不影响实际接口调用逻辑
4. **更新维护**: 当接口字段变更时，同步更新本文件中的mock数据格式

---

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|----------|
| 2.0.0 | 2026-06-18 | 统一字段为英文key，修复项目部署数据模型，修正字段错别字 |
| 1.0.0 | 2026-05-20 | 初始版本，包含5个接口的mock数据 |
