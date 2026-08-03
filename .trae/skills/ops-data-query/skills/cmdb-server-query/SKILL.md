---
name: cmdb-server-query
description: 查询 CMDB 服务器信息，包括主机名、IP、机房、配置、状态等。支持按机房、状态、类型、产品、项目等多维度筛选。
version: 4.0.0
author: Skill Agent Team
---

# CMDB 服务器查询技能

> ⚠️ **v4.0 说明**：数据获取层已迁移至 MCP 服务器，本文件仅保留指令层（触发条件、参数映射、输出格式化）。

## 功能描述

查询服务器主机信息，包括：
- 基础信息：主机名、内网IP、公网IP、VIP
- 机房信息：节点、机架、带宽
- 配置信息：CPU、内存、操作系统、服务器类型
- 业务关联：所属产品、关联项目
- 运维信息：运维负责人、资产编号

## 触发条件

**关键词**：服务器、主机、机器、设备、IP配置、CPU内存、操作系统

**排他条件**：
- 查公网IP → 使用 `server-public-ip-query`
- 查部署记录 → 使用 `project-deployment-query`
- 查产品信息 → 使用 `product-query`
- 查项目代码 → 使用 `project-basis-query`

## MCP 工具调用

**工具名**：`cmdb_server_query`

**参数映射**：

| 参数名 | 类型 | 说明 | 默认值 | 来源 |
|--------|------|------|--------|------|
| hostName | string | 主机名（模糊匹配） | - | 用户输入 |
| ip | string | 内网 IP | - | 用户输入 |
| node | string | 机房节点 | - | 参数映射 |
| state | string | 服务器状态 | - | 参数映射 |
| serverType | string | 服务器类型 | - | 参数映射 |
| belong | integer | 归属 | - | 参数映射 |
| xc | string | 信创标识 | - | 参数映射 |
| productName | string | 产品名称 | - | 用户输入 |
| projectName | string | 项目名称 | - | 用户输入 |
| currentPage | integer | 页码 | 1 | 自动 |
| pageSize | integer | 每页条数 | 15 | 数量词映射 |

**调用示例**：

**示例 1：按 IP 查询**
```json
{
  "name": "cmdb_server_query",
  "parameters": {
    "ip": "192.168.7.101",
    "currentPage": 1,
    "pageSize": 15
  }
}
```

**示例 2：按机房 + 状态查询**
```json
{
  "name": "cmdb_server_query",
  "parameters": {
    "node": "云公司->贵州",
    "state": "0",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 3：按产品 + 项目查询**
```json
{
  "name": "cmdb_server_query",
  "parameters": {
    "productName": "天翼看家",
    "projectName": "tykj-api",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

**示例 4：按服务器类型 + 归属查询**
```json
{
  "name": "cmdb_server_query",
  "parameters": {
    "serverType": "1",
    "belong": 1,
    "xc": "1",
    "currentPage": 1,
    "pageSize": 40
  }
}
```

## 输出格式

### 标准输出

```
| 主机名 | IP | 机房 | 状态 | 类型 | CPU | 内存 | 产品 |
|--------|-----|------|------|------|-----|------|------|
```

### 详细输出

增加字段：公网IP、操作系统、环境、项目、运维人、资产编号、机架、带宽

### 状态图标

| 状态代码 | 图标 | 含义 |
|----------|------|------|
| 0 | 🟢 | 在线 |
| 1 | 🟡 | 库存 |
| 2 | 🟠 | 计划上线 |
| 3 | 🔧 | 维修中 |
| 4 | ⚫ | 已报废 |

## 响应字段

| 字段 | 类型 | 说明 | 格式化 |
|------|------|------|--------|
| hostName | string | 主机名 | - |
| ip | string | 内网 IP | - |
| publicIp | string | 公网 IP | - |
| vip | string | 虚拟 IP | 空值显示 `-` |
| node | string | 机房节点 | - |
| state | string | 状态代码 | 状态图标 |
| serverType | string | 服务器类型 | 物理机/虚拟机/云服务器 |
| cpuCores | string | CPU 核数 | `{N}核` |
| memory | string | 内存 | `{N}GB` |
| os | string | 操作系统 | - |
| environment | string | 环境 | 测试/灰度/生产 |
| productName | string | 产品名称 | - |
| projectName | string | 项目名称 | - |
| operA | string | 运维负责人A | - |
| operB | string | 运维负责人B | - |
| assetNo | string | 资产编号 | - |
| rack | string | 机架位置 | - |
| bandWidth | string | 带宽 | - |

## 参考文件

通用输出模板、错误处理、参数映射规则请参考父级 `ops-data-query/SKILL.md`。

## 版本历史

| 版本 | 日期 | 更新内容 |
|------|------|---------|
| 4.0 | 2026-06-26 | 迁移数据获取层至 MCP 服务器，精简为薄指令层 |
| 3.0 | 2026-06-24 | 抽离共享内容 |
| 2.0 | 2026-06-18 | 统一字段为英文 key |
| 1.0 | 2026-05-20 | 初始版本 |
