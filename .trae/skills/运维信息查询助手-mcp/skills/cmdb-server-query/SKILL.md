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
      "nodeIpStr": {"type": "string", "description": "复合查询：机房+IP 精确组合，格式为『机房名称 IP地址』（空格分隔），多个组合用换行分隔，如 '云公司-广州 192.168.1.1'。仅当用户同时给出机房和 IP 的组合条件时使用；单独按 IP 或机房查询请分别用 ip / node"},
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
      "productId": {"type": "string", "description": "产品ID（需先将产品名称在 product-options 文档中匹配为ID后传入，单选）。注意：不要传产品名称，传ID才生效。若不确定ID，先查 skill 的 product-options.md"},
      "productIdList": {"type": "array", "items": {"type": "string"}, "description": "产品ID数组（多选，与 productId 二选一勿同时传）。后端只支持产品单选，MCP 内部会按每个产品ID分别查询后按服务器id去重合并。示例: [\"产品A的ID\",\"产品B的ID\"]"},
      "projectIdList": {"type": "array", "items": {"type": "string"}, "description": "项目ID数组（需先将项目名称匹配为ID数组后传入，多选）"},
      "rack": {"type": "string", "description": "机架位置（单选），直接传机架名称，如'龙晖九楼B01机架'"},
      "operABList": {"type": "array", "items": {"type": "string"}, "description": "负责人A或B姓名数组（多选，数组内为或），直接传用户提到的姓名。用户只说'XX负责的服务器'、不区分主备时优先用这个。与 operAList/operBList 一般三选一，勿叠加"},
      "operAList": {"type": "array", "items": {"type": "string"}, "description": "负责人A（主负责人）姓名数组（多选），仅匹配 operA 字段。仅当用户明确要'主负责人'时使用"},
      "operBList": {"type": "array", "items": {"type": "string"}, "description": "负责人B（备/协同负责人）姓名数组（多选），仅匹配 operB 字段。仅当用户明确要'B角负责人'时使用"},
      "contractType": {"type": "integer", "description": "合同类型（单选），传合同类型ID。取值参考 contract-options 文档（当前技能未内置，无法确定ID时不传）"},
      "fields": {"type": "array", "items": {"type": "string"}, "description": "【性能优化】只返回这些字段，可大幅减小响应体积。不传则返回全部字段。常见字段: hostName,ip,node,type,state,belong,os,osType,cpuCores,memory,gpus,hardDisk,usingBy,productProjectList,operA"},
      "currentPage": {"type": "integer", "description": "页码", "minimum": 1},
      "pageSize": {"type": "integer", "description": "每页条数", "minimum": 1, "maximum": 100}
    },
    "required": []
  }
}
```

> **注意**：以上 schema 必须与 MCP 服务 `tools/list` 返回的 `cmdb_server_query` 工具定义一致。若不一致，以 MCP 服务实际返回为准，并同步更新 [mcp schema](../_dev/mcp/cmdb-mcp-schema.json)。

> **✅ 产品过滤入参**：`cmdb_server_query` 支持按产品过滤，参数是 **`productId`（产品ID，单选）** 与 **`projectIdList`（项目ID数组，多选）**——必须传 **ID**，不认产品名称模糊。
> 使用前需先做"名称 → ID"文档匹配（见下方「产品名称处理」），**转换完成后调用时传 ID，不要传名称**。

## 输入参数

### 可选参数

| 参数名 | 类型 | 说明 | 默认值 | 可选值 |
|--------|------|------|--------|--------|
| node | string | 机房位置 | - | 详见 [references/node-options.md](references/node-options.md)（263个机房） |
| nodeIpStr | string | 机房+IP 精确组合（复合查询） | - | 格式『机房名称 IP地址』（空格分隔），多个用换行分隔，如 `云公司-广州 192.168.1.1`；单独按 IP/机房查请用 ip / node |
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
| productId | string | 产品ID（单选） | - | 需先从 [references/product-options.md](references/product-options.md) 名称匹配出ID |
| productIdList | array[string] | 产品ID数组（多选，与 productId 二选一） | - | 需先名称匹配出ID数组；MCP 内部按各产品分别查询后按服务器ID去重合并 |
| projectIdList | array[string] | 项目ID数组（多选） | - | 需先名称匹配出ID数组 |
| rack | string | 机架位置（单选） | - | 直接传机架名称，如"龙晖九楼B01机架" |
| operABList | array[string] | 负责人A或B（多选） | - | 直接传姓名；"XX负责的服务器"不区分主备时优先用 |
| operAList | array[string] | 负责人A-主负责人（多选） | - | 仅匹配 operA 字段 |
| operBList | array[string] | 负责人B-备/协同（多选） | - | 仅匹配 operB 字段 |
| contractType | integer | 合同类型（单选） | - | 传合同类型ID（contract-options 文档未内置，无法确定时不传） |
| fields | array[string] | 只返回指定字段（性能优化） | 全部字段 | 如 `["hostName","type","cpuCores","gpus","os","usingBy","productProjectList"]` |
| currentPage | integer | 页码 | 1 | ≥1 |
| pageSize | integer | 每页条数 | 15 | 1~100 |

> **负责人筛选**：`operABList` / `operAList` / `operBList` 是同一组负责人的三种筛法（A或B / 仅A / 仅B），**一般三选一，勿叠加**。用户说"XX负责的服务器"而不区分主备时，用 `operABList: ["XX"]`。

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

### 产品名称处理（先转ID，再查询）

`cmdb_server_query` 按产品过滤**必须传 `productId`（产品ID），不认产品名称**。流程：

1. **名称匹配**：从 [references/product-options.md](references/product-options.md) 中按用户提到的产品名做**包含匹配**（放宽：产品表行含"显示名/父级>产品名"两种路径，同一 ID 可能有多行，**去重取 ID**）
2. **口径判定（重要，先判断用户意图）**：
   - 用户只点名**单个具体产品**、未提"平台/整树/含子孙/全部XX系" → **精确口径**：字面命中取该产品 ID（不自动含子孙）
   - 用户点名**平台/集约/集团产品名**，或**明确要含子孙/全部子产品** → **平台口径（含子孙）**：`productId` 单选无法覆盖整棵产品树，改用 belong 域全量 + `productProjectList` 归属识别（见「产品/平台维度统计执行模式 → 模式B」）
3. **匹配失败处理**：产品词在文档中**完全匹配不到**时，**显式告知用户**"该产品未在产品表中匹配到，无法按产品过滤"，并询问准确名称——**禁止静默跳过然后全量拉取**

> **产品层级说明（实测）**：产品树形层级如「视频云网 > 天翼看家 > 党建/统一sdk/行业版…」；不同层级产品 productId 不同。
> 2026-09 实测：`productId=天翼看家` → 404 台、`productId=天翼云眼` → 13 台，而视联域(belong=2)在用非信创全量 = **473 台**（多出的 56 台即挂在看家/云眼子孙及其他视联产品下）。
> **父级聚合能力未经确认**：不能假设"天翼看家"包含其子孙的服务器；要"含子孙"必须走模式B（belong 域全量拉取识别），或先探测父级 total 是否 ≈ 域 total。

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

## 产品/平台维度统计执行模式

**适用场景**：用户要求统计某产品/平台下的数量（"统计 XX 平台的已使用非信创产品数量""XX 产品有多少台服务器"）。

**执行前先判定口径**：

| 用户意图 | 口径 | 走哪个模式 |
|----------|------|-----------|
| 点名单个具体产品（"天翼看家有几台在用非信创"） | **精确口径** | 模式A |
| 用户点名平台/集约名 或 明确"含子孙/全部子产品/整个XX系"（"天翼视联集约平台(看家/云眼)含子孙"） | **平台口径（含子孙）** | 模式B |
| 平台口径下还要"CPU/VCPU/GPU/操作系统/数据库/内存"等资源指标（"…产品数量，包括：服务器(台数)、CPU、VCPU…"） | **资源盘点口径** | 模式B+ |
| 用户还要"列出所有服务器信息/明细" | 明细展示 | 模式A/B 后补明细翻页 |

---

### 模式A：精确口径（productId 单产品读 total）

1. **名称 → ID**：product-options.md 字面命中取 ID（如"天翼看家" → `1661fea29df44946ab2ff2bb9577179f`），匹配不到 → 显式告知
2. **单次查询读 total**：叠加用户条件，`pageSize=1` 即可
   - 示例：`{"productId": "1661fea29df44946ab2ff2bb9577179f", "state": "0", "isFromOutside": "0", "pageSize": 1}`
   - "已使用"→`state=0`、"非信创"→`isFromOutside=0`
3. **有效性质检**：带 productId 的 total 明显 < 不带时的全量 → 过滤生效；几乎一致 → 后端未过滤，告知并按"模式B"兜底
4. 多个产品（看家+云眼）→ 各查一次 `data.total` 相加，**注明"按产品分别求和，多归属可能重复"**

### 模式B：平台口径（含子孙，2026-09 实测验证）

**背景（重要）**：
- `productId` 单选且**父级聚合未确认**（实测看家404+云眼13=417）；要"含子孙"必须覆盖整棵产品树
- **`belong`(1=云网/2=视联) 是"事业部域"，不等于"某平台产品树"**！实测视联域(belong=2)在用非信创全量=473 台，但域内混有**平台外产品**的服务器（如 `AI产品(行业版)`、`综合平台公共` 为独立根产品，不在看家/云眼树下）
- 因此 **belong 全量只能作"粗筛候选集"，最终口径必须按"产品白名单"过滤**（见下）

**目标产品树成员（权威来源 product-options.md 路径列，勿按名称前缀推断）**：

**L1 视频云网树内（路径父级=视频云网/天翼看家/天翼云眼）**：

| 产品 | productId |
|---|---|
| 天翼看家 | `1661fea29df44946ab2ff2bb9577179f` |
| 天翼云眼 | `0bd177f3a1654780abb77de8bfc3cc5e` |
| 党建 | `b4d825f23baf41d8baca1a51e9e58ede` |
| 天翼看家统一sdk | `3772fae89c614aed96a3de47df2472ea` |
| 天翼看家能力开放 | `8c4a88ea265a4517af7099d7008ba83c` |
| 天翼看家公共 | `ff13e51be85c48f188a0ae3e2f0f6ff9`（注意是"公共"非"公众"） |

**L2 同族自根产品（名称含"天翼看家/云眼"但 product-options 路径=自身，是否纳入需与用户确认）**：

| 产品 | productId |
|---|---|
| 天翼看家行业版 | `74f00b7272d04527805af16034c9c4c8` |
| 天翼看家公众版 | `0b75a4332ed34a21aa1aea6c2b11d0dc` |
| 天翼看家基础接入功能服务 | `669746241c2f4ee5862ad5514abee40b` |
| 天翼看家能力平台-南京 | `b46e7d8cd5b94ce6a64bf9e1104dbadd` |
| 天翼云眼基础接入功能服务 | `e6ebb7352be8472184eb2ba81c94627d` |

**口径规则**：
- 默认按 **L1（树内 6 产品）** 输出主结果；**L2 单独统计并排展示**（"另有同族自根 N 台，若算入则合计 M"），由用户定最终口径
- **禁止按名称前缀猜归属**（例：树表显示"天翼看家行业版"路径=自身，不在视频云网树下）；L1/L2 归属以本表为准
- 独立根产品（AI产品(行业版/公众版)、综合平台公共、智慧工程行业版、流量平台 等）**不在 L1 也不在 L2**，一律排除并单列

1. **定位候选域**：先按业务域粗筛——平台属视联 → `belong=2`；云网 → `belong=1`（见"归属类型映射"）。无法判断域时向用户确认
2. **探测父级聚合（可选）**：查父级 productId 的 total，若 ≈ 树内过滤后总量 → 父级可聚合（后续可直接父级单查）
3. **拉取候选（两种方式，优先 a）**：
   - **a. `productIdList` 一步合并（MCP ≥ 支持后推荐）**：把 L1/L2 产品 ID 数组直接传入，MCP 内部逐个产品查询后按服务器 id 去重合并，返回精确并集 total：
     ```
     {"belong": 2, "state": "0", "isFromOutside": "0",
      "productIdList": ["1661fea...", "0bd177f3...", "..."],
      "pageSize": 100, "fields": ["hostName", "type", "cpuCores", "os", "productProjectList"]}
     ```
     返回 `total` 即树内去重台数（message 含"MCP 合并"标识）。**注意：后端不认 productIdList（会静默忽略返回全量），只有 MCP 层实现了合并才生效**——判断：返回 message 含"MCP 合并"= 生效；若 total 等于域全量且无标识 → 服务端未升级，退回方式 b
   - **b. 域全量翻页（后端仅支持单选 productId 时的兜底）**：
     ```
     {"belong": 2, "state": "0", "isFromOutside": "0", "currentPage": n, "pageSize": 100,
      "fields": ["hostName", "ip", "type", "cpuCores", "gpus", "os", "usingBy", "productProjectList"]}
     ```
     从第 1 页拉到 `currentPage*pageSize >= total`（视联域约 5 页 473 台——**这只是候选集，非最终数**）。
   ⚠️ **不传 fields 时单页≈400KB（120+字段含 vipList 嵌套），灌入 agent 上下文会极慢（每页分钟级）且触发 spill；统计/翻页必须传 fields 白名单裁剪**（单页可降到 ~30KB）

**⚠️ fields 使用纪律（防"字段被裁导致这次看不到"）**：
- **每次调用前先列出本次分析/统计需要的全部字段，一次带齐，宁多勿少**；字段裁剪只影响"本次回显哪些字段"，不影响查询命中（过滤条件照常透传），但这次没带的字段这页就看不到，需要时得再查一次
- 不确定后续还要用什么时，保底带：`["id", "hostName", "node", "type", "state", "isFromOutside", "productProjectList"]`（id 用于去重/定位）
- 资源盘点类按模式B+ 字段口径表把要聚合的字段全带上（cpuCores/gpus/os/memory/usingBy...）
- 只确认是否存在/计数、不需要明细时，可只带 `["id"]`（最快最小）
- 需要看某台完整信息：**不传 fields** 且用 `hostName`/`ip` 缩到单台查
4. **归属识别与树过滤（关键）**：每台记录读 `records[].productProjectList[].productId/productName`；**仅保留 productId ∈ 目标树成员表 的记录**；完全不在树内的整机排除；多产品归属中含树内产品的保留并注明
5. **分组汇总输出**：
   - 过滤后总数（如看家/云眼树内 ≈ 417+，需实测）
   - 按 productProjectList 中的产品名分组统计各产品服务器数
   - **单列被排除的平台外产品及台数**（如 AI产品(行业版) X 台），并说明"该部分不属于看家/云眼树，已按口径排除"
6. **明细展示**：用户要求列出所有服务器时，输出 `hostName/ip/node/usingBy/产品归属` 表格；>20 条建议分批展示或用 `pageSize=100` 汇总后给文件级结论

**翻页保护**：模式B 总翻页上限 **20 次**（约 2000 台）；超限停止并提示"按 node/机房分批统计"，严禁无限翻页。

### 模式B+：资源盘点口径（用户要求"包括 CPU/VCPU/GPU/操作系统/数据库/内存/磁盘"等资源指标时）

在模式B 全量拉取的基础上，对每台记录按字段聚合。**字段口径先看清，禁止臆造**：

| 用户指标 | 取值字段 | 口径 |
|----------|----------|------|
| 服务器台数 | records 计数 | 按 `type` 拆分：0=物理机 / 1=自有虚拟机 / 2=第三方云机 |
| VCPU(核数) | `cpuCores`(int) | 逐台求和 |
| CPU(颗数) | **无物理颗数字段** | 记录仅 `cpuCores`(核) 与 `cpuCode`；**把核数当颗数上报=错误**。口径（2026-09 确认）：**不估算**，如实标注"CPU 颗数无数据源，物理机 X 台需人工确认"，只报 VCPU 核数 |
| GPU(卡数) | `gpus`(int，可空) | 逐台求和，null 计 0 |
| 操作系统(套) | `os`(string) | 每台 1 套 → 按 os 版本分组输出 |
| 内存 | `memory`(int,GB) | 求和 → 输出"共 X GB" |
| 磁盘 | `hardDisk` | 字符串不规整，不可靠则如实说明未统计 |
| 数据库(套) | **记录无实例字段** | 仅 `usingBy`(如 "haproxy,mysql") 可做**承载台数近似**（mysql/oracle/redis/…），并注明"近似口径，非实例数"；实例级需另行数据源 |
| 产品归属 | `productProjectList[].productName` | 需要按产品分组时使用（多归属可能重复） |

**执行步骤**：全量翻页（同模式B）→ 逐台提取上表字段 → 数值字段 `int()` 容错（null/非数计 0）→ 输出汇总表。数据量超 2000 台时按 node 分批并提示。

**输出模板**：

```markdown
## 平台资源统计（含子孙口径）

**对象**：天翼视联集约平台（天翼看家/天翼云眼及子孙）
**口径**：belong=2 + state=0(已使用) + isFromOutside=0(非信创)，域名全量 N 台

| 维度 | 数量 |
|------|------|
| 服务器 | 物理机 X / 虚拟机 Y / 第三方云机 Z，合计 N |
| CPU(颗数) | 无数据源，物理机 X 台需人工确认（VCPU 核数为 V） |
| VCPU(核数) | V |
| GPU(卡) | G |
| 操作系统(套) | M（按版本：A 版 a 套、B 版 b 套…） |
| 数据库 | 承载服务器 C 台（usingBy 近似，非实例数） |

**说明**：CPU 颗数与数据库实例数服务器接口无字段，未精确统计；如需精确值请提供对应数据源。
```

### 实测基准（2026-09，天翼视联集约平台=视联域 belong=2 候选，state=0 + isFromOutside=0）

> agent 统计后可与此基准比对量级，异常悬殊时核查口径再回答（不是照抄数字）：
> ⚠️ 以下为 **belong=2 候选域**数字，其中混有 AI产品(行业版)/综合平台公共 等平台外产品，**最终口径需按看家/云眼树白名单过滤后为准**（上表为基准，非最终答案）：

| 指标 | 候选域实测值 |
|------|--------|
| 服务器 | 物理机 14 / 虚拟机 2 / 云机 457 = 473（候选域） |
| VCPU(核) | 6,238 |
| GPU(卡) | 24 |
| OS(套) | 473（CentOS7.3 226、7.6 90、ctyunos 64、未知 23、7.4 17、7.8 16、7.9 9、7.5 8、CentOS6.9 6、7.1 5、7.7 4、openEuler 2、其余 4） |
| DB 承载(台) | mysql 44 / redis 41 / dm 6 / mongodb 5 / oracle 1 |
| 台数分解(近似) | 看家 404 + 云眼 13 = 417（productId 直查），树内其余子孙待实测 |

### 参考示例（含子孙）

**用户输入**："统计天翼视联集约平台(天翼看家/天翼云眼)的已使用非信创产品数量，含子孙的也要统计，并列出所有服务器"

**执行（模式B）**：
1. belong 域：视联 → `belong=2` 全量翻页 → 5 页 total=473（**候选域**）
2. 每台读 productProjectList 判断归属
3. **树过滤**：保留 productId ∈ 看家/云眼树成员表的记录；`AI产品(行业版)`/`综合平台公共` 等平台外产品整机排除并单列
4. 输出：

```markdown
## 统计结果（含子孙口径）

**统计对象**：天翼视联集约平台（天翼看家/天翼云眼 及其子孙）
**统计口径**：视联域(belong=2) 候选 + L1/L2 白名单过滤 + 非信创(isFromOutside=0) + 已使用(state=0)
**候选域全量：473 台（含平台外产品，已按口径排除）**

**L1 视频云网树内（主结果）**
| 产品（树内） | 服务器数 |
|--------------|----------|
| 天翼看家 | 404 |
| 天翼云眼 | 13 |
| 党建/统一sdk/能力开放/公共 | N3 |
| **L1 合计** | **N_L1** |

**L2 同族自根（若算入集约平台）**
| 产品（自根族） | 服务器数 |
|----------------|----------|
| 行业版/公众版/基础接入/能力平台-南京/云眼基础接入 | N4 |
| **L1+L2 合计** | **N_L1+N4** |

**已排除（平台外独立根）**：AI产品(行业版) X 台、综合平台公共 Y 台、流量平台 Z 台……（共 473-N_L1-N4 台）
```

**说明**：若用户口径明确为"看家/云眼树"→ 报 L1 合计；未明确 → 报 L1 并附 L2 供确认；多产品归属服务器在各产品组内重复，合计以白名单去重数为准。

### 禁止事项

- ❌ **混淆产品/项目参数**：产品层用 `productId`/`productIdList`（看家/云眼这类产品树节点）；项目层才用 `projectIdList`（产品>工程>项目 的最细层）。**禁止把产品ID传进 `projectIdList`**（两参数仅差一字母，填错返回 total=0，已发生真实事故）
- ❌ 把**产品名称**作为 `productId` 的值传入（必须传 ID，先查 product-options.md 转换）
- ❌ 用户要"含子孙/整个平台"时用单个 `productId` 查询冒充全量（会漏掉子孙产品下的服务器）
- ❌ 产品词匹配不到时静默跳过、退回全量拉取——必须显式询问/告知
- ❌ 无任何过滤条件就全量拉取并逐条翻页到几千上万条
- ❌ 用户要"平台含子孙"时忽略 belong 域、只答看家+云眼两个 productId 的合计（必须提示平台域可能还有子孙产品服务器）
- ❌ **把 belong 业务域全量（473）直接当"看家/云眼平台"的最终数**——域内混有 AI产品(行业版)/综合平台公共 等平台外产品，必须按产品树白名单过滤并单列排除项
- ❌ 把 `cpuCores`(核数) 上报成"CPU 颗数"——记录无物理颗数字段，必须如实说明（见模式B+ 口径表）
- ❌ 编造"数据库实例数"——服务器记录无实例字段，只能给 usingBy 承载台数**近似**并注明口径，实例级需另行数据源
- ❌ 用户要"服务器(台数)"细分物理机/虚拟机时只给总数——用 `type` 拆分（0/1/2）

## MCP 工具响应格式

调用 `cmdb_server_query` 工具后返回统一 JSON 结构：

```json
{
  "code": 200,
  "message": "成功状态",
  "fail": false,
  "data": {
    "records": [
      {
        "id": "0000ce6886dd4ef1a52195dc4ac0056f",
        "hostName": "ecs-72e0-1226340",
        "ip": "192.168.3.173,",
        "publicIp": null,
        "vip": "192.168.3.191",
        "node": "云公司->贵州",
        "state": 0,
        "type": 2,
        "belong": 2,
        "xc": 0,
        "isFromOutside": 0,
        "osType": "linux",
        "os": "CentOS Linux 7.3.1611 Core",
        "environment": "生产",
        "cpuCores": 8,
        "memory": 32,
        "serverId": "ecs-72e0-1226340",
        "usingBy": "haproxy,mysql",
        "productRate": "天翼看家:100",
        "productProjectList": [
          {
            "productId": "1661fea29df44946ab2ff2bb9577179f",
            "productName": "天翼看家",
            "projectBasisList": [
              {
                "projectBasisId": "6cac566e4d35461593d3fe4f4eca11ba",
                "projectBasisName": "tykj-conf-direct-mysql-db",
                "projectList": [
                  {
                    "projectId": "ae3a205c2c644582934e0f94a030cf75",
                    "projectName": "guizh-tykj-conf-direct-mysql-db",
                    "projectServerType": "生产"
                  }
                ]
              }
            ]
          }
        ],
        "operA": "余良彬",
        "operB": "余良彬,傅翔,廖明礼,梁晨,王伟杰",
        "onlineDate": "2019-12-30 12:21:04"
      }
    ],
    "total": 404,
    "size": 1,
    "current": 1,
    "pages": 404
  }
}
```

> **字段为真实生产返回**（2026-09 实测）。注意：`state/isFromOutside/xc/belong/type` 等为 **int**；产品归属看 **`productRate`**（如 `"天翼看家:100"` 含占比）与 **`productProjectList`**（产品→工程(projectBasis)→项目(project)三层数组）。

### 响应字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| code | integer | 状态码，200 表示成功 |
| message | string | 响应消息 |
| data | object | 响应数据体 |
| data.records | array | 服务器列表 |
| data.total | integer | 总记录数（带 productId 时即该产品过滤后的总数，直接用于统计） |
| data.currentPage | integer | 当前页码 |
| data.pageSize | integer | 每页条数 |

### records 中的每条记录

> 字段为真实生产样例（2026-09 实测，见上方示例 JSON）。产品归属用 **`productRate`**（字符串 `"产品名:占比"`，多产品逗号分隔）和 **`productProjectList`**（结构化数组）判断。以下为常用字段，完整字段以实际返回为准。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string | 服务器唯一 ID |
| hostName | string | 主机名 |
| ip | string | 内网 IP 地址 |
| publicIp | string/null | 公网 IP 地址 |
| vip | string | 虚 IP |
| node | string | 机房位置 |
| state | int | 状态码（0=在线...） |
| type | int | 服务器类型（0=物理机, 1=自有虚拟机, 2=第三方云机） |
| belong | int | 归属（1=云网, 2=视联） |
| xc / isFromOutside | int | 信创标识（0=非信创, 1=信创） |
| osType / os | string | 操作系统 |
| environment | string | 环境（生产/测试/灰度） |
| cpuCores | int | CPU 核数（VCPU 求和用） |
| memory | int | 内存大小（GB） |
| gpus | int/null | GPU 卡数（盘点 GPU 用，null 计 0） |
| usingBy | string | 承载用途（如 "haproxy,mysql"——数据库近似统计唯一线索） |
| hardDisk | string/null | 磁盘（字符串描述，盘点磁盘不可靠） |
| productRate | string | 产品占比，如 `"天翼看家:100"`，多产品 `"A:50,B:50"` |
| productProjectList | array | **产品归属列表**：`productId/productName` + `projectBasisList[]`（工程）内含 `projectList[]`（项目） |
| operA / operB | string | 运维A角/B角 |
| onlineDate | string | 上线时间 |
| assetNo / serial / rack | string/null | 资产编号/序列号/机架位置 |

> **规格字段说明**：`specification`（规格，如 `8C32GSAS(600G)`）因取值与实际磁盘信息不一致，已停止返回。用户询问"规格/配置"时，用 `cpuCores` + `memory` + `hardDisk` 组合回答，不要编造或引用历史值。

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
1. 检查 MCP 服务（cmdb-mcp-server）是否已部署且健康检查通过（`curl http://180.101.21.13:8080/gateway/cmdb/health` 应返回 200）
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
