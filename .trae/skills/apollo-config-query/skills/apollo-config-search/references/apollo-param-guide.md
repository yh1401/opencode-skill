# Apollo 配置查询参数转换完整指南

**⚠️ 本文件是 Apollo 配置查询参数转换规则的唯一权威来源！**

本文档定义了从用户自然语言查询到 Apollo OpenAPI 请求参数的完整转换规则。

---

## 一、概览

### 1.1 职责

定义 Apollo 配置查询的参数转换规则和映射表，将用户自然语言查询转换为 Apollo OpenAPI 请求参数。

### 1.2 配置文件说明

| 文件 | 用途 | 说明 |
|------|------|------|
| `SKILL.md` | 技能说明文档 | 引用本文档作为参数转换规则来源 |
| `references/apollo-param-guide.md` | 参数映射指南 | 完整参数转换规则 |
| `references/app-options.md` | 应用可选值 | 常见 AppId 列表 |
| `references/namespace-options.md` | Namespace 可选值 | 常见 Namespace 列表 |
| `registry/skills.json` | 技能注册中心 | 技能路由和启用状态管理 |

### 1.3 接口信息

配置查询通过 **EasyOps 代理接口**转发到 Apollo OpenAPI（MCP 内部处理），调用方无需持有 Apollo Token：

| 属性 | 值 |
|------|------|
| **配置查询接口** | `GET {EASYOPS_BASE_URL}/thirdApi/apollo/namespace?apolloHostId=&env=&appId=&clusterName=&namespaceName=` |
| **应用列表接口** | `GET {EASYOPS_BASE_URL}/thirdApi/apollo/apps?apolloHostId=` |
| **认证方式** | Cookie `sessionId`（EasyOps 统一保存 Apollo 地址与 Token，调用方无需传 Token） |
| **Content-Type** | `application/json;charset=UTF-8` |
| **失败提示** | 代理接口调用不通时，MCP 直接返回错误提示（无备用直连通道） |

> `apolloHostId` 即第三方 `getApolloHostInfo` 返回的记录 `id`，来自 `apollo_host_list` 工具。

---

## 二、字段总览

Apollo 配置查询 API 共支持以下查询字段，按处理方式分为 4 类：

| 分类 | 字段数 | 处理方式 | 说明 |
|------|-------|---------|------|
| **Apollo 套映射** | 1 | 动态获取 | Apollo 服务（apolloHostId） |
| **环境参数** | 1 | 原值传递 | 环境（env）不固定，以目标 Apollo 实际环境为准 |
| **默认值参数** | 2 | 默认值兜底 | 集群、Namespace |
| **模糊匹配** | 1 | 原值传递 | 配置项 Key |

---

## 三、关键词匹配列表

| 关键词类型 | 关键词 |
|-----------|--------|
| Apollo 配置 | Apollo、配置、配置中心、配置项 |
| 应用相关 | appid、应用配置、应用 |
| Namespace | namespace、配置空间、配置命名空间 |
| Apollo 套相关 | 贵州、广州4、P2P、百川、看家、小A平台、有哪些环境、哪套（→ 套/apolloHostId） |
| 环境相关（env） | 开发环境、测试环境、生产环境、预发、集成、DEV、FAT、SIT、UAT、PRO |
| 应用列表 | 应用列表、有哪些应用（→ 调 `apollo_app_list`） |
| 配置项 | key、配置key、配置项key |

---

## 四、参数转换规则

### 0. 环境术语判定规则（先判定，再查 4.1/4.2）

| 用户表述 | 判定 | 目标参数 |
|---------|------|---------|
| 提到 **开发/测试/生产/预发/集成** 或 **DEV/FAT/UAT/PRO/SIT** 等环境名关键词 | **应用环境** | env |
| 其他提到"环境"的表述（"贵州环境"、"有哪些环境"、"广州4那套"） | **哪套 Apollo 服务** | apolloHostId（调 `apollo_host_list`） |

> 一句话：**"环境"带环境名关键词 → env；否则"环境"都指多套 Apollo 服务（apolloHostId）。**
> 边界情况：同时出现地名+环境名（如"广州4生产"）→ 地名→套(apolloHostId)、环境名→env，两者都生效。

### 4.1 Apollo 套映射（apolloHostId）

> 查询链路第一步：先确定哪套 Apollo，再进入后续参数转换。`apolloHostId` 通过 `apollo_host_list` 工具实时获取。

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 地名/产品关键词（贵州、广州4、P2P、百川、看家等） | apolloHostId | 调用 `apollo_host_list` 获取对应套的 `apolloHostId` | 见 [apollo-hosts.md](apollo-hosts.md) |
| 未指定 Apollo 套 | apolloHostId | 不传 | MCP 使用默认 Apollo |

### 4.2 环境映射

> 注意：不同 Apollo 套的环境命名可能不同（如 PRO/SIT/DEV 或 PRO/DEV/FAT/UAT），下表为常见映射，实际传参以目标 Apollo 的环境名为准。

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 开发环境、DEV、开发 | env | DEV | 开发环境 |
| 测试环境、FAT、测试 | env | FAT | 测试环境 |
| 集成测试、SIT | env | SIT | 集成测试环境 |
| 预发环境、UAT、预发 | env | UAT | 预发环境 |
| 生产环境、PRO、生产 | env | PRO | 生产环境（默认） |

### 4.3 集群映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 默认集群、default | clusterName | default | 默认集群（默认值） |
| 其他集群名称 | clusterName | 原值传递 | 直接传递用户输入 |

### 4.4 Namespace 映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 默认、application | namespaceName | application | 默认 Namespace |
| 数据库配置、datasource | namespaceName | datasource | 数据源配置 |
| Redis配置、redis | namespaceName | redis | Redis 配置 |
| 日志配置、logging | namespaceName | logging | 日志配置 |
| 公共配置、common | namespaceName | common | 公共配置 |
| 其他 Namespace | namespaceName | 原值传递 | 直接传递用户输入 |

> **完整 Namespace 列表**：见 [namespace-options.md](namespace-options.md)

### 4.5 应用列表查询

| 输入关键词 | 工具 | 说明 |
|-----------|------|------|
| 应用列表、有哪些应用 | `apollo_app_list` | 查询应用列表（可传 `apolloHostId` 指定某套 Apollo） |

### 4.6 应用 ID 映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 常见应用名称 | appId | 对应 AppId | 见 [app-options.md](app-options.md) |
| 其他应用名称 | appId | 原值传递 | 模糊匹配 |

### 4.7 配置项搜索（模糊匹配）

| 用户输入模式 | API参数 | 示例 | 说明 |
|------------|---------|------|------|
| 搜索 xxx、查找 xxx 配置 | key | "timeout" | 从用户输入中提取关键词 |

---

## 五、完整参数转换示例

### 示例 0：指定 Apollo 套查询（查询链路完整示例）

```
输入：查一下贵州那套 Apollo 上 rule-engine 的生产配置
第一步：调用 apollo_host_list → 命中"天翼云眼贵州测试Apollo-亿讯专用"，apolloHostId=15
第二步：调用 apollo_config_query → {"apolloHostId": 15, "appId": "rule-engine", "env": "PRO", "namespaceName": "application"}
```

### 示例 1：基本配置查询（默认 Apollo）

```
输入：查询 rule-engine 在生产环境的配置
输出：{"appId": "rule-engine", "env": "PRO", "namespaceName": "application"}
```
（未指定 Apollo 套，apolloHostId 缺省，MCP 使用默认 Apollo）

### 示例 2：搜索特定配置项

```
输入：查一下 rule-engine 的 timeout 配置
输出：{"appId": "rule-engine", "env": "PRO", "namespaceName": "application", "key": "timeout"}
```

### 示例 3：查询特定 Namespace

```
输入：rule-engine 在测试环境的数据库配置
输出：{"appId": "rule-engine", "env": "FAT", "namespaceName": "datasource"}
```

### 示例 4：查询应用列表

```
输入：Apollo 里有哪些应用
工具调用：{"tool_name": "apollo_app_list", "parameters": {}}
```
（默认 Apollo；如需指定某套，先调 apollo_host_list 取 apolloHostId 传入）
