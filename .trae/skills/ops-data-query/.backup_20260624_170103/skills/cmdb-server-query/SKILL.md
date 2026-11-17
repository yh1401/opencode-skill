---
name: cmdb-server-query
description: 查询 CMDB 服务器信息，支持按机房、状态、类型等多条件组合查询
version: 2.0.0
author: Skill Agent Team
---

# CMDB 服务器查询技能

## 功能描述

本技能用于查询服务器的详细配置信息，包括：
- 基本信息：主机名、IP地址、机房位置
- 硬件配置：CPU核数、内存大小、服务器类型
- 运行状态：在线状态、所属环境
- 业务信息：所属产品、项目、负责人

## 触发条件

当用户查询包含以下关键词时触发：
- 服务器、主机、机器、设备
- 机房、数据中心、位置
- IP地址、内网IP
- 配置、规格、硬件

**排他条件**：
- 如果用户明确提到"公网IP"、"外网"、"带宽"，应触发 `server-public-ip-query` 技能
- 如果用户明确提到"部署"、"发布"、"上线"，应触发 `project-deployment-query` 技能

## 输入参数

### 必填参数

| 参数名 | 类型 | 说明 | 示例 |
|--------|------|------|------|
| node | string | 机房位置 | "云公司->贵州" |

### 可选参数

| 参数名 | 类型 | 说明 | 默认值 | 可选值 |
|--------|------|------|--------|--------|
| state | string | 服务器状态 | "0" | "0"=在线, "1"=库存, "2"=计划上线, "3"=维修中, "4"=已报废, "5"=待分配, "6"=待清退 |
| osType | string | 操作系统 | - | "linux", "windows" |
| type | string | 服务器类型 | - | "0"=物理机, "1"=自有虚拟机, "2"=第三方云机 |
| hostName | string | 主机名（模糊匹配） | - | - |
| ip | string | 内网IP（模糊匹配） | - | - |
| assetNo | string | 资产编号 | - | - |
| serial | string | 序列号 | - | - |
| belong | integer | 归属类型 | - | 1=云网, 2=视联 |
| isFromOutside | string | 信创标识 | - | "0"=非信创, "1"=信创 |
| isReplaceForChinaProduction | string | 国产化替换状态 | - | "0"=未替换, "1"=已替换 |
| currentPage | integer | 页码 | 1 | ≥1 |
| pageSize | integer | 每页条数 | 15 | 1~100 |

## 工具调用 Schema

```json
{
  "name": "cmdb_server_query",
  "description": "查询CMDB服务器信息，支持按机房、状态、类型等多条件组合查询",
  "parameters": {
    "type": "object",
    "properties": {
      "node": {
        "type": "string",
        "description": "机房位置，如'云公司->贵州'"
      },
      "state": {
        "type": "string",
        "description": "服务器状态，0=在线, 1=库存, 2=计划上线, 3=维修中, 4=已报废, 5=待分配, 6=待清退",
        "enum": ["0", "1", "2", "3", "4", "5", "6"]
      },
      "osType": {
        "type": "string",
        "description": "操作系统",
        "enum": ["linux", "windows"]
      },
      "type": {
        "type": "string",
        "description": "服务器类型，0=物理机, 1=自有虚拟机, 2=第三方云机",
        "enum": ["0", "1", "2"]
      },
      "hostName": {
        "type": "string",
        "description": "主机名（模糊匹配）"
      },
      "ip": {
        "type": "string",
        "description": "内网IP（模糊匹配）"
      },
      "assetNo": {
        "type": "string",
        "description": "资产编号"
      },
      "serial": {
        "type": "string",
        "description": "序列号"
      },
      "belong": {
        "type": "integer",
        "description": "归属类型，1=云网, 2=视联"
      },
      "isFromOutside": {
        "type": "string",
        "description": "信创标识，0=非信创, 1=信创",
        "enum": ["0", "1"]
      },
      "isReplaceForChinaProduction": {
        "type": "string",
        "description": "国产化替换状态，0=未替换, 1=已替换",
        "enum": ["0", "1"]
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
    "required": ["node"]
  }
}
```

## 参数映射规则

### 机房节点映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "贵州"、"贵阳"、"贵州机房" | node | "云公司->贵州" |
| "北京"、"北京机房" | node | "云公司->北京" |
| "上海"、"上海机房" | node | "省公司->上海" |
| "广州"、"广州机房" | node | "云公司->广州" |
| "朝阳"、"朝阳机房" | node | "云公司->朝阳1" |

### 服务器状态映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "在线"、"在用"、"使用中" | state | "0" |
| "库存"、"空闲"、"未用" | state | "1" |
| "计划上线" | state | "2" |
| "维修中"、"维修" | state | "3" |
| "已报废" | state | "4" |
| "待分配" | state | "5" |
| "待清退" | state | "6" |

### 操作系统映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "linux"、"Linux" | osType | "linux" |
| "windows"、"Windows" | osType | "windows" |

### 服务器类型映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "物理机"、"实体机" | type | "0" |
| "虚拟机"、"自有虚拟机" | type | "1" |
| "第三方云机"、"云机"、"云服务器" | type | "2" |

### 归属类型映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "视联" | belong | 2 |
| "云网" | belong | 1 |

### 信创/国产化映射

| 用户输入 | 目标参数 | 参数值 |
|----------|----------|--------|
| "信创"、"XC"、"国产化" | isFromOutside | "1" |
| "非信创"、"非XC"、"非国产" | isFromOutside | "0" |

### 数量词识别规则

| 用户输入 | 对应的 pageSize |
|----------|----------------|
| "一台"、"一个" | 1 |
| "几台"、"一些" | 15 (默认) |
| "所有"、"全部" | 100 |
| "最新的"、"最近的" | 10 |
| "N台"、"N个" | N |

### 模糊匹配字段

| 用户输入模式 | 目标参数 | 提取规则 |
|--------------|----------|----------|
| "IP"、"IP地址"、"服务器IP" | ip | 提取 IPv4 格式地址 |
| "主机名"、"hostname"、"服务器名称" | hostName | 提取主机名 |
| "资产编号"、"资产号" | assetNo | 提取资产编号 |
| "序列号"、"SN" | serial | 提取序列号 |

## API 接口

### 接口地址

**生产环境**：POST https://oss.tech.ctseelink.cn/api/v2/cmdbServer/getCmdbServerPageList

### 请求格式

```json
{
  "node": "云公司->贵州",
  "state": "0",
  "currentPage": 1,
  "pageSize": 15
}
```

### 请求参数说明

| 参数名 | 说明 |
|--------|------|
| node | 机房位置 |
| state | 服务器状态 |
| osType | 操作系统类型 |
| type | 服务器类型 |
| hostName | 主机名（模糊匹配） |
| ip | 内网IP（模糊匹配） |
| assetNo | 资产编号 |
| serial | 序列号 |
| belong | 归属类型 |
| isFromOutside | 信创标识 |
| isReplaceForChinaProduction | 国产化替换状态 |
| currentPage | 页码 |
| pageSize | 每页条数 |

### 响应格式

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "records": [
      {
        "id": "srv-001",
        "hostName": "gz-server-01",
        "ip": "192.168.7.101",
        "publicIp": "113.12.13.14",
        "node": "云公司->贵州",
        "state": "0",
        "serverType": "物理机",
        "cpuCores": "32",
        "memory": "128",
        "os": "CentOS 7.9",
        "environment": "生产",
        "productName": "规则引擎平台",
        "projectName": "guizh-rules-api"
      }
    ],
    "total": 100,
    "currentPage": 1,
    "pageSize": 15
  }
}
```

### 响应字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| code | integer | 状态码，200 表示成功 |
| message | string | 响应消息 |
| data | object | 响应数据体 |
| data.records | array | 服务器列表 |
| data.total | integer | 总记录数 |
| data.currentPage | integer | 当前页码 |
| data.pageSize | integer | 每页条数 |

### records 中的每条记录

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string | 服务器唯一 ID |
| hostName | string | 主机名 |
| ip | string | 内网 IP 地址 |
| publicIp | string | 公网 IP 地址 |
| vip | string | 虚 IP |
| node | string | 机房位置 |
| state | string | 状态码 |
| serverType | string | 服务器类型 |
| cpuCores | string | CPU 核数 |
| memory | string | 内存大小（GB） |
| os | string | 操作系统版本 |
| environment | string | 环境 |
| productName | string | 所属产品名称 |
| projectName | string | 所属项目名称 |
| assetNo | string | 资产编号 |
| rack | string | 机架位置 |

## 状态码映射

| 状态码 | 中文 | 含义 |
|--------|------|------|
| 0 | 在线 | 正常运行中 |
| 1 | 库存 | 已入库未使用 |
| 2 | 计划上线 | 即将上线 |
| 3 | 维修中 | 故障维修 |
| 4 | 已报废 | 已退役 |
| 5 | 待分配 | 尚未分配用途 |
| 6 | 待清退 | 准备回收 |

## 输出格式

### 标准输出（简单模式 - 默认）

```
## 服务器查询结果

**查询条件**：{用户原始查询}

**匹配技能**：CMDB服务器查询

**查询范围**：机房=贵州，状态=在线（显示前10条）

---

**结果摘要**：共查询到 100 台服务器，当前显示前 10 条

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 |
|--------|--------|------|------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 |
| gz-server-02 | 192.168.7.102 | 云公司->贵州 | 在线 | 虚拟机 |
| ... | ... | ... | ... | ... |

---

💡 您可以说：
- "查看详细信息" - 显示 CPU、内存、操作系统等完整信息
- "下一页" - 查看更多服务器
- "查看全部" - 显示所有查询结果
```

### 详细输出（用户要求"查看详细信息"时）

```
## 服务器查询结果（详细）

**查询条件**：{用户原始查询}

**匹配技能**：CMDB服务器查询

**查询范围**：机房=贵州，状态=在线

---

**结果摘要**：共查询到 100 台服务器，当前显示前 10 条

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 | CPU | 内存 | 操作系统 | 环境 | 产品 | 项目 |
|--------|--------|------|------|------|-----|------|----------|------|------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 | 32核 | 128GB | CentOS 7.9 | 生产 | 规则引擎平台 | guizh-rules-api |
| gz-server-02 | 192.168.7.102 | 云公司->贵州 | 在线 | 虚拟机 | 16核 | 64GB | Ubuntu 20.04 | 测试 | 天翼看家 | tykj-kafka-test |
| ... | ... | ... | ... | ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 CMDB 系统。
```

### 降级输出（API 失败时）

```
## 服务器查询结果

> ⚠️ 提示：API 接口调用失败，已切换为模拟数据

**查询条件**：{用户原始查询}

**匹配技能**：CMDB服务器查询

**查询范围**：机房=贵州，状态=在线（显示前10条）

---

**结果摘要**：共查询到 N 台服务器（模拟数据，当前显示前 10 条）

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 |
|--------|--------|------|------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 |
| ... | ... | ... | ... | ... |

---

💡 您可以说"查看详细信息"获取更多字段，或"下一页"查看更多结果

**说明**：当前显示的是模拟数据，真实数据需要 CMDB 服务可用时才能获取。
```

## 完整调用示例

### 示例 1：简单查询

**用户输入**："查询贵州机房的服务器"

**工具调用**：
```json
{
  "tool_name": "cmdb_server_query",
  "parameters": {
    "node": "云公司->贵州",
    "state": "0",
    "currentPage": 1,
    "pageSize": 15
  }
}
```

**API 请求**：
```json
{
  "node": "云公司->贵州",
  "state": "0",
  "currentPage": 1,
  "pageSize": 15
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
        "id": "srv-001",
        "hostName": "gz-server-01",
        "ip": "192.168.7.101",
        "publicIp": "113.12.13.14",
        "node": "云公司->贵州",
        "state": "0",
        "serverType": "物理机",
        "cpuCores": "32",
        "memory": "128",
        "os": "CentOS 7.9",
        "environment": "生产",
        "productName": "规则引擎平台",
        "projectName": "guizh-rules-api"
      }
    ],
    "total": 100,
    "currentPage": 1,
    "pageSize": 15
  }
}
```

**最终输出**：
```
## 服务器查询结果

**查询条件**：查询贵州机房的服务器

**匹配技能**：CMDB服务器查询

**查询参数**：{"node":"云公司->贵州","state":"0","pageSize":15}

---

**结果摘要**：共查询到 100 台服务器

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 | CPU | 内存 | 操作系统 | 环境 | 产品 | 项目 |
|--------|--------|------|------|------|-----|------|----------|------|------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 | 32核 | 128GB | CentOS 7.9 | 生产 | 规则引擎平台 | guizh-rules-api |
| ... | ... | ... | ... | ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 CMDB 系统，状态"0"表示在线运行中。
```

### 示例 2：多条件查询

**用户输入**："找一台贵州机房的在线物理机"

**工具调用**：
```json
{
  "tool_name": "cmdb_server_query",
  "parameters": {
    "node": "云公司->贵州",
    "state": "0",
    "type": "0",
    "currentPage": 1,
    "pageSize": 1
  }
}
```

**API 请求**：
```json
{
  "node": "云公司->贵州",
  "state": "0",
  "type": "0",
  "currentPage": 1,
  "pageSize": 1
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
        "id": "srv-001",
        "hostName": "gz-server-01",
        "ip": "192.168.7.101",
        "node": "云公司->贵州",
        "state": "0",
        "serverType": "物理机",
        "cpuCores": "32",
        "memory": "128",
        "os": "CentOS 7.9",
        "environment": "生产"
      }
    ],
    "total": 50,
    "currentPage": 1,
    "pageSize": 1
  }
}
```

**最终输出**：
```
## 服务器查询结果

**查询条件**：找一台贵州机房的在线物理机

**匹配技能**：CMDB服务器查询

**查询参数**：{"node":"云公司->贵州","state":"0","type":"0","pageSize":1}

---

**结果摘要**：共查询到 50 台服务器（显示第1条）

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 | CPU | 内存 | 操作系统 | 环境 |
|--------|--------|------|------|------|-----|------|----------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 | 32核 | 128GB | CentOS 7.9 | 生产 |

---

**说明**：数据来源于 CMDB 系统。
```

## 错误处理

### 错误场景 1：缺少必要参数

**输入**："查询服务器"

**输出**：
```
我需要更多信息来完成查询。

请告诉我您想查询哪个机房的服务器？可选机房：贵州、北京、上海、广州、朝阳

您也可以直接补充说："查询贵州机房的服务器"
```

### 错误场景 2：API 调用超时

**输出**：
```
## 查询失败

**错误类型**：网络超时

**错误原因**：CMDB 服务响应超时，请稍后重试

**建议**：
1. 检查网络连接
2. 稍后重新查询
3. 如问题持续，请联系管理员
```

### 错误场景 3：无查询结果

**输入**："查询火星机房的服务器"

**输出**：
```
## 查询结果

**查询条件**：查询火星机房的服务器

**匹配技能**：CMDB服务器查询

**查询参数**：{"node":"火星","pageSize":15}

---

**结果摘要**：未查询到符合条件的服务器

---

**说明**：当前系统中不存在"火星"机房的服务器信息。
可用机房：贵州、北京、上海、广州、朝阳
```

## Mock 数据（API 降级备用）

当 API 不可用时，使用以下模拟数据返回：

```json
{
  "code": 200, "message": "success", "fail": false,
  "data": {
    "records": [
      {
        "id": "srv-001", "hostName": "gz-server-01", "ip": "192.168.7.101",
        "publicIp": "113.12.13.14", "vip": "", "node": "云公司->贵州",
        "state": "0", "serverType": "物理机", "cpuCores": "32", "memory": "128",
        "os": "CentOS 7.9", "environment": "生产",
        "productName": "规则引擎平台", "projectName": "guizh-rules-api",
        "assetNo": "SN2024001", "rack": "R01-A01", "bandWidth": "100Mbps"
      },
      {
        "id": "srv-002", "hostName": "gz-server-02", "ip": "192.168.7.102",
        "publicIp": "113.12.13.15", "vip": "10.0.0.1", "node": "云公司->贵州",
        "state": "0", "serverType": "虚拟机", "cpuCores": "16", "memory": "64",
        "os": "Ubuntu 20.04", "environment": "测试",
        "productName": "天翼看家", "projectName": "tykj-kafka-test",
        "assetNo": "SN2024002", "rack": "R01-A02", "bandWidth": "50Mbps"
      },
      {
        "id": "srv-003", "hostName": "sh-server-01", "ip": "192.168.8.101",
        "publicIp": "114.25.36.47", "vip": "", "node": "省公司->上海",
        "state": "0", "serverType": "物理机", "cpuCores": "48", "memory": "256",
        "os": "CentOS 8.2", "environment": "生产",
        "productName": "5G工业视宽平台", "projectName": "5g-industry-api",
        "assetNo": "SN2024003", "rack": "R02-B01", "bandWidth": "200Mbps"
      },
      {
        "id": "srv-004", "hostName": "xj-server-01", "ip": "192.168.9.101",
        "publicIp": "115.36.47.58", "vip": "10.0.0.2", "node": "省公司->新疆乌鲁木齐",
        "state": "1", "serverType": "物理机", "cpuCores": "24", "memory": "64",
        "os": "CentOS 7.9", "environment": "灰度",
        "productName": "边缘计算平台", "projectName": "edge-compute",
        "assetNo": "SN2024004", "rack": "R03-C01", "bandWidth": "100Mbps"
      }
    ],
    "total": 4, "size": 15, "current": 1, "pages": 1
  }
}
```
