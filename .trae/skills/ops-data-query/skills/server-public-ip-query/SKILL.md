---
name: server-public-ip-query
description: 查询服务器公网 IP、带宽、网络类型、计费方式等信息。支持按机房节点、内网 IP 精确查询。
version: 4.0.0
author: Skill Agent Team
---

# 服务器公网 IP 查询技能

> ⚠️ **v4.0 说明**：数据获取层已迁移至 MCP 服务器，本文件仅保留指令层（触发条件、参数映射、输出格式化）。

## 功能描述

查询服务器公网 IP 配置信息，包括：
- 公网 IP 地址（含 IPv6）
- 内网 IP 映射
- 机房节点
- 带宽规格和类型（独享/共享）
- 计费方式（按带宽/按流量）
- 端口映射

## 触发条件

**关键词**：公网IP、外网IP、带宽、出口IP、网络类型、公网IP列表

**排他条件**：
- 查服务器配置 → 使用 `cmdb-server-query`
- 查部署记录 → 使用 `project-deployment-query`
- 查产品信息 → 使用 `product-query`
- 查项目代码 → 使用 `project-basis-query`

## MCP 工具调用

**工具名**：`server_public_ip_query`

**参数映射**：

| 参数名 | 类型 | 说明 | 默认值 | 来源 |
|--------|------|------|--------|------|
| ip | string | 内网 IP | - | 用户输入 |
| hostName | string | 主机名（模糊匹配） | - | 用户输入 |
| node | string | 机房节点 | - | 参数映射 |
| currentPage | integer | 页码 | 1 | 自动 |
| pageSize | integer | 每页条数 | 40 | 数量词映射 |

**调用示例**：

**示例 1：按内网 IP 查询**
```json
{
  "name": "server_public_ip_query",
  "parameters": {
    "ip": "192.168.7.101",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 2：按主机名查询**
```json
{
  "name": "server_public_ip_query",
  "parameters": {
    "hostName": "gz-server-01",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 3：按机房节点查询**
```json
{
  "name": "server_public_ip_query",
  "parameters": {
    "node": "云公司->贵州",
    "currentPage": 1,
    "pageSize": 100
  }
}
```

## 输出格式

### 标准输出

```
| 公网IP | 内网IP | 机房 | 带宽 | 带宽类型 | 计费方式 |
|--------|--------|------|------|---------|---------|
```

### 详细输出

增加字段：IPv6地址、内网端口、公网端口、共享带宽ID

## 响应字段

| 字段 | 类型 | 说明 | 格式化 |
|------|------|------|--------|
| publicIp | string | 公网 IPv4 | - |
| publicIpv6 | string | 公网 IPv6 | 空值显示 `-` |
| ip | string | 内网 IP | - |
| vip | string | 虚拟 IP | 空值显示 `-` |
| node | string | 机房节点 | - |
| privatePort | string | 内网端口 | - |
| publicPort | string | 公网端口 | - |
| sharedBandwidthId | string | 共享带宽 ID | - |
| bandwidth | string | 带宽值 | `{N}Mbps` |
| bandwidthType | string | 带宽类型 | 独享/共享 |
| billingType | string | 计费方式 | 按带宽/按流量计费 |

## 参考文件

通用输出模板、错误处理、参数映射规则请参考父级 `ops-data-query/SKILL.md`。

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|---------|
| 4.0 | 2026-06-26 | 迁移数据获取层至 MCP 服务器，精简为薄指令层 |
| 3.0 | 2026-06-24 | 抽离共享内容 |
| 2.0 | 2026-06-18 | 统一字段为英文 key |
| 1.0 | 2026-05-20 | 初始版本 |
