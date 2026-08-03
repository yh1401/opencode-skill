# Mock 数据文档

本文件提供 cmdb-server-query 子技能调用接口的模拟返回数据，供开发和测试使用。

---

## 服务器查询接口

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

## 使用说明

1. **用途**: 仅供开发/测试参考接口字段格式，线上技能 API 失败时直接报错，不使用模拟数据
2. **非侵入性**: 本文件不参与打包，不影响线上技能运行逻辑
3. **更新维护**: 当接口字段变更时，同步更新本文件中的 mock 数据格式
