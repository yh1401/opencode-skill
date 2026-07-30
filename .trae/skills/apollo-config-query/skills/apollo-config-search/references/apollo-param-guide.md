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

| 属性 | 值 |
|------|------|
| **接口地址** | `GET /openapi/v1/envs/{env}/apps/{appId}/clusters/{clusterName}/namespaces/{namespaceName}` |
| **认证方式** | `Authorization: {token}` |
| **Content-Type** | `application/json;charset=UTF-8` |

---

## 二、字段总览

Apollo 配置查询 API 共支持以下查询字段，按处理方式分为 4 类：

| 分类 | 字段数 | 处理方式 | 说明 |
|------|-------|---------|------|
| **固定可选值映射** | 1 | 技能内直接映射 | 环境（env） |
| **默认值参数** | 2 | 默认值兜底 | 集群、Namespace |
| **模糊匹配** | 1 | 原值传递 | 配置项 Key |
| **查询模式** | 1 | 模式切换 | config/release/apps |

---

## 三、关键词匹配列表

| 关键词类型 | 关键词 |
|-----------|--------|
| Apollo 配置 | Apollo、配置、配置中心、配置项 |
| 应用相关 | appid、应用配置、应用 |
| Namespace | namespace、配置空间、配置命名空间 |
| 环境相关 | 开发环境、测试环境、生产环境、DEV、FAT、PRO |
| 查询模式 | 查看配置、发布历史、变更记录、应用列表 |
| 配置项 | key、配置key、配置项key |

---

## 四、参数转换规则

### 4.1 环境映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 开发环境、DEV、开发 | env | DEV | 开发环境 |
| 测试环境、FAT、测试 | env | FAT | 测试环境 |
| 预发环境、UAT、预发 | env | UAT | 预发环境 |
| 生产环境、PRO、生产 | env | PRO | 生产环境（默认） |

### 4.2 集群映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 默认集群、default | clusterName | default | 默认集群（默认值） |
| 其他集群名称 | clusterName | 原值传递 | 直接传递用户输入 |

### 4.3 Namespace 映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 默认、application | namespaceName | application | 默认 Namespace |
| 数据库配置、datasource | namespaceName | datasource | 数据源配置 |
| Redis配置、redis | namespaceName | redis | Redis 配置 |
| 日志配置、logging | namespaceName | logging | 日志配置 |
| 公共配置、common | namespaceName | common | 公共配置 |
| 其他 Namespace | namespaceName | 原值传递 | 直接传递用户输入 |

> **完整 Namespace 列表**：见 [namespace-options.md](namespace-options.md)

### 4.4 查询模式映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 查看配置、配置列表 | queryMode | config | 查询配置项列表（默认） |
| 发布历史、变更记录 | queryMode | release | 查询发布历史 |
| 应用列表、有哪些应用 | queryMode | apps | 查询应用列表 |

### 4.5 应用 ID 映射

| 输入关键词 | API参数 | 映射值 | 说明 |
|-----------|---------|--------|------|
| 常见应用名称 | appId | 对应 AppId | 见 [app-options.md](app-options.md) |
| 其他应用名称 | appId | 原值传递 | 模糊匹配 |

### 4.6 配置项搜索（模糊匹配）

| 用户输入模式 | API参数 | 示例 | 说明 |
|------------|---------|------|------|
| 搜索 xxx、查找 xxx 配置 | key | "timeout" | 从用户输入中提取关键词 |

---

## 五、完整参数转换示例

### 示例 1：基本配置查询

```
输入：查询 rule-engine 在生产环境的配置
输出：{"appId": "rule-engine", "env": "PRO", "queryMode": "config", "namespaceName": "application"}
```

### 示例 2：搜索特定配置项

```
输入：查一下 rule-engine 的 timeout 配置
输出：{"appId": "rule-engine", "env": "PRO", "queryMode": "config", "namespaceName": "application", "key": "timeout"}
```

### 示例 3：查询发布历史

```
输入：rule-engine 的配置变更记录
输出：{"appId": "rule-engine", "env": "PRO", "queryMode": "release", "namespaceName": "application"}
```

### 示例 4：查询特定 Namespace

```
输入：rule-engine 在测试环境的数据库配置
输出：{"appId": "rule-engine", "env": "FAT", "queryMode": "config", "namespaceName": "datasource"}
```

### 示例 5：查询应用列表

```
输入：Apollo 里有哪些应用
输出：{"appId": "", "queryMode": "apps"}
```
