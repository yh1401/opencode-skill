---
name: cmdb-server-query
description: 查询 CMDB 服务器信息，支持按机房、状态、类型等多条件组合查询，通过 MCP 工具获取数据
version: 1.0.0
author: Skill Agent Team
---

# CMDB 服务器查询技能（MCP 版）

## 功能描述

本技能用于查询服务器的详细配置信息，包括：
- 基本信息：主机名、IP地址、机房位置
- 硬件配置：CPU核数、内存大小、服务器类型
- 运行状态：在线状态、所属环境
- 业务信息：所属产品、项目、负责人

**数据获取方式**：通过 MCP 工具 `cmdb_server_query` 获取数据（由平台路由到已注册的 cmdb-mcp-server 服务），**技能自身不直接调用 CMDB 业务 API**。

## 触发条件

当用户查询包含以下关键词时触发：
- 服务器、主机、机器、设备
- 机房、数据中心、位置
- IP地址、内网IP、公网IP
- 配置、规格、硬件

**排他条件**：
- 如果用户明确提到"外网"、"带宽"，应触发 `server-public-ip-query` 技能（MCP 版暂未启用）
- 如果用户明确提到"部署"、"发布"、"上线"，应触发 `project-deployment-query` 技能（MCP 版暂未启用）

## MCP 工具调用

### 工具信息

| 属性 | 值 |
|------|------|
| **工具名** | `cmdb_server_query` |
| **调用方式** | 平台 MCP 工具路由（tools/call） |
| **数据来源** | cmdb-mcp-server → CMDB 业务系统 |
| **返回格式** | JSON（code/message/data.records） |

### 工具 Schema

```json
{
  "name": "cmdb_server_query",
  "description": "查询CMDB服务器信息，支持按机房、状态、类型等多条件组合查询",
  "inputSchema": {
    "type": "object",
    "properties": {
      "node": {"type": "string", "description": "机房位置，如'云公司->贵州'"},
      "state": {"type": "string", "description": "服务器状态，0=在线, 1=库存, 2=计划上线, 3=维修中, 4=已报废, 5=待分配, 6=待清退", "enum": ["0", "1", "2", "3", "4", "5", "6"]},
      "osType": {"type": "string", "description": "操作系统", "enum": ["linux", "windows"]},
      "type": {"type": "string", "description": "服务器类型，0=物理机, 1=自有虚拟机, 2=第三方云机", "enum": ["0", "1", "2"]},
      "hostName": {"type": "string", "description": "主机名（模糊匹配）"},
      "ip": {"type": "string", "description": "内网IP/公网IP（模糊匹配）"},
      "assetNo": {"type": "string", "description": "资产编号"},
      "serial": {"type": "string", "description": "序列号"},
      "belong": {"type": "integer", "description": "归属类型，1=云网, 2=视联"},
      "isFromOutside": {"type": "string", "description": "信创标识，0=非信创, 1=信创", "enum": ["0", "1"]},
      "isReplaceForChinaProduction": {"type": "string", "description": "国产化替换状态，0=未替换, 1=已替换", "enum": ["0", "1"]},
      "currentPage": {"type": "integer", "description": "页码", "minimum": 1},
      "pageSize": {"type": "integer", "description": "每页条数", "minimum": 1, "maximum": 100}
    },
    "required": []
  }
}
```

> **注意**：以上 schema 必须与 MCP 服务 `tools/list` 返回的 `cmdb_server_query` 工具定义一致。若不一致，以 MCP 服务实际返回为准，并同步更新 [mcp schema](../_dev/mcp/cmdb-mcp-schema.json)。

## 输入参数

### 可选参数

| 参数名 | 类型 | 说明 | 默认值 | 可选值 |
|--------|------|------|--------|--------|
| node | string | 机房位置 | - | 详见 [references/node-options.md](references/node-options.md)（263个机房） |
| state | string | 服务器状态 | "0" | "0"=在线, "1"=库存, "2"=计划上线, "3"=维修中, "4"=已报废, "5"=待分配, "6"=待清退 |
| osType | string | 操作系统 | - | "linux", "windows" |
| type | string | 服务器类型 | - | "0"=物理机, "1"=自有虚拟机, "2"=第三方云机 |
| hostName | string | 主机名（模糊匹配） | - | - |
| ip | string | 内网IP/公网IP（模糊匹配） | - | - |
| assetNo | string | 资产编号 | - | - |
| serial | string | 序列号 | - | - |
| belong | integer | 归属类型 | - | 1=云网, 2=视联 |
| isFromOutside | string | 信创标识 | - | "0"=非信创, "1"=信创 |
| isReplaceForChinaProduction | string | 国产化替换状态 | - | "0"=未替换, "1"=已替换 |
| currentPage | integer | 页码 | 1 | ≥1 |
| pageSize | integer | 每页条数 | 15 | 1~100 |

## 参数映射规则

> 完整的 20 字段参数转换规则（含文档可选值映射）见 [references/cmdb-param-guide.md](references/cmdb-param-guide.md)。

### 机房节点映射

| 用户输入 | 目标参数 | 参数值 | 说明 |
|----------|----------|--------|------|
| "贵州"、"贵阳"、"贵州机房" | node | "云公司->贵州" | 常见节点，直接映射 |
| "北京"、"北京机房" | node | "云公司->北京" | 常见节点，直接映射 |
| "上海"、"上海机房" | node | "省公司->上海" | 常见节点，直接映射 |
| "广州"、"广州机房" | node | "云公司->广州" | 常见节点，直接映射 |
| "朝阳"、"朝阳机房" | node | "云公司->朝阳1" | 常见节点，直接映射 |
| 用户输入的其他名称 | node | 原值传递 | 完整机房列表见 [references/node-options.md](references/node-options.md) |

> **完整机房列表**：共 263 个机房节点，按供应商分组。

### 产品名称映射

| 用户输入 | 目标参数 | 参数值 | 说明 |
|----------|----------|--------|------|
| 产品名称关键词 | productName | 产品名称 | 模糊匹配，支持320个产品名称 |

> **产品列表**：共 320 个产品，支持树形层级。完整列表见 [references/product-options.md](references/product-options.md)

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
| "IP"、"IP地址"、"服务器IP"、"公网IP"、"外网IP" | ip | 提取 IPv4 格式地址（同时匹配内网IP和公网IP） |
| "主机名"、"hostname"、"服务器名称" | hostName | 提取主机名 |
| "资产编号"、"资产号" | assetNo | 提取资产编号 |
| "序列号"、"SN" | serial | 提取序列号 |

## MCP 工具响应格式

调用 `cmdb_server_query` 工具后返回统一 JSON 结构：

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

**匹配技能**：CMDB服务器查询（MCP）

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

**匹配技能**：CMDB服务器查询（MCP）

**查询范围**：机房=贵州，状态=在线

---

**结果摘要**：共查询到 100 台服务器，当前显示前 10 条

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 | CPU | 内存 | 操作系统 | 环境 | 产品 | 项目 |
|--------|--------|------|------|------|-----|------|----------|------|------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 | 32核 | 128GB | CentOS 7.9 | 生产 | 规则引擎平台 | guizh-rules-api |
| ... | ... | ... | ... | ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 MCP 服务（cmdb-mcp-server）。
```

## 完整调用示例

### 示例 1：简单查询

**用户输入**："查询贵州机房的服务器"

**MCP 工具调用**：
```json
{
  "tool_name": "cmdb_server_query",
  "arguments": {
    "node": "云公司->贵州",
    "state": "0",
    "currentPage": 1,
    "pageSize": 15
  }
}
```

**工具返回**：
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

**匹配技能**：CMDB服务器查询（MCP）

**查询参数**：{"node":"云公司->贵州","state":"0","pageSize":15}

---

**结果摘要**：共查询到 100 台服务器

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 | CPU | 内存 | 操作系统 | 环境 | 产品 | 项目 |
|--------|--------|------|------|------|-----|------|----------|------|------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 | 32核 | 128GB | CentOS 7.9 | 生产 | 规则引擎平台 | guizh-rules-api |
| ... | ... | ... | ... | ... | ... | ... | ... | ... | ... | ... |

---

**说明**：数据来源于 MCP 服务。
```

### 示例 2：多条件查询

**用户输入**："找一台贵州机房的在线物理机"

**MCP 工具调用**：
```json
{
  "tool_name": "cmdb_server_query",
  "arguments": {
    "node": "云公司->贵州",
    "state": "0",
    "type": "0",
    "currentPage": 1,
    "pageSize": 1
  }
}
```

**最终输出**：
```
## 服务器查询结果

**查询条件**：找一台贵州机房的在线物理机

**匹配技能**：CMDB服务器查询（MCP）

**查询参数**：{"node":"云公司->贵州","state":"0","type":"0","pageSize":1}

---

**结果摘要**：共查询到 50 台服务器（显示第1条）

---

| 主机名 | IP地址 | 机房 | 状态 | 类型 | CPU | 内存 | 操作系统 | 环境 |
|--------|--------|------|------|------|-----|------|----------|------|
| gz-server-01 | 192.168.7.101 | 云公司->贵州 | 在线 | 物理机 | 32核 | 128GB | CentOS 7.9 | 生产 |

---

**说明**：数据来源于 MCP 服务。
```

## 错误处理

### 错误场景 1：无查询结果（无过滤条件时）

当用户查询条件过于宽泛（如仅"查询服务器"无任何过滤参数），建议引导补充筛选条件：

**输入**："查询服务器"

**处理**：不带过滤条件直接调用 MCP 工具，根据返回结果灵活处理
- 若结果较多，可提示用户提供更多筛选条件
- 可选 "机房"、"状态"、"类型"、"IP" 等条件缩小范围

### 错误场景 2：MCP 工具调用异常

**输出**：
```
## 查询失败

**错误类型**：MCP 服务异常

**错误原因**：{工具返回的错误信息}

**建议**：
1. 检查 MCP 服务（cmdb-mcp-server）是否已部署且健康检查通过（`curl http://localhost:5235/gateway/cmdb/health` 应返回 200）
2. 稍后重新查询
3. 如问题持续，请联系管理员
```

### 错误场景 3：无查询结果

**输入**："查询火星机房的服务器"

**输出**：
```
## 查询结果

**查询条件**：查询火星机房的服务器

**匹配技能**：CMDB服务器查询（MCP）

**查询参数**：{"node":"火星","pageSize":15}

---

**结果摘要**：未查询到符合条件的服务器

---

**说明**：当前系统中不存在"火星"机房的服务器信息。
可用机房：贵州、北京、上海、广州、朝阳
```
