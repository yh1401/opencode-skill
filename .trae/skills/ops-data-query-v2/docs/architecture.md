# Skill Agent 架构文档

## 概述

Skill Agent 是一个基于 MCP (Model Context Protocol) 的智能代理系统，专为 Dify 平台和 LangChain 设计。系统包含 **5 个核心技能**，支持运维领域的各种查询需求。

**核心技能列表**:
- **cmdb-server-query**: CMDB服务器查询
- **server-public-ip-query**: 服务器公网IP查询
- **product-query**: 产品查询
- **project-deployment-query**: 项目部署信息查询
- **project-basis-query**: 工程项目信息查询

## 系统架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        用户输入                                 │
│              (自然语言查询，如"查找贵州机房的服务器")             │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Skill Agent (主技能)                        │
│  - 意图识别                                                    │
│  - 技能路由（查询 registry/skills.json，含优先级）               │
│  - 技能协调                                                    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    技能注册中心 (registry/skills.json)          │
│  - 技能元数据管理（含优先级、排他关键词、排他技能）               │
│  - 技能启用/禁用控制                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    子技能层 (skills/*/SKILL.md)                  │
│  - 参数提取与校验                                               │
│  - 字段映射与操作符处理                                         │
│  - 工具调用 Schema 定义                                         │
│  - API调用与结果处理                                            │
│  - 错误处理与返回格式化                                         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      MCP 适配层                                 │
│  - 请求/响应 Schema 定义 (mcp/*-schema.json)                    │
│  - API调用（真实接口或Mock数据）                                │
└─────────────────────────────────────────────────────────────────┘
```

## 目录结构

```
skill-agent/
├── SKILL.md                     # 主技能入口（技能路由和协调）
├── SKILL.html                   # 技能文档 HTML 版本
├── MOCK_DATA.md                # 集中式Mock数据存储
├── registry/                    # 技能注册中心
│   └── skills.json             # 技能元数据（含优先级、排他规则）
├── config/                      # 配置文件
│   └── LOG_MARKERS.md          # 日志标识说明文档
├── skills/                      # 子技能目录
│   ├── cmdb-server-query/      # CMDB 服务器查询
│   │   ├── SKILL.md           # 技能定义（含工具调用Schema）
│   │   └── references/         # 字段映射表
│   ├── server-public-ip-query/ # 服务器公网IP查询
│   ├── product-query/          # 产品查询
│   ├── project-deployment-query/ # 项目部署查询
│   └── project-basis-query/    # 工程项目查询
├── docs/                        # 文档目录
│   ├── architecture.md         # 架构文档
│   ├── optimization-guide.md   # 优化指南
│   └── quick-reference-for-adding-skills.md  # 新增技能快速参考
├── mcp/                        # MCP API定义
│   ├── cmdb-mcp-api.md
│   ├── cmdb-mcp-schema.json
│   ├── project-deployment-mcp-api.md
│   ├── project-deployment-mcp-schema.json
│   ├── server-public-ip-mcp-api.md
│   ├── server-public-ip-mcp-schema.json
│   ├── product-mcp-api.md
│   ├── product-mcp-schema.json
│   ├── project-basis-mcp-api.md
│   └── project-basis-mcp-schema.json
└── tests/                      # 测试用例
```

## 架构层次

| 层级 | 组件 | 职责 |
|------|------|------|
| 入口层 | SKILL.md | 技能路由和协调（含完整路由规则） |
| 注册层 | registry/skills.json | 技能元数据注册中心（含优先级、排他规则） |
| 技能层 | skills/*/SKILL.md | 具体业务技能定义（含工具调用Schema） |
| 文档层 | docs/, mcp/, references/ | API文档、Schema定义和参考资料 |

## 数据流

### 1. 用户查询流程

```
用户输入 → 意图识别 → 技能路由（按优先级） → 参数校验 → 子技能处理 → MCP调用 → 结果格式化 → 返回
```

### 2. 参数转换流程

```
用户输入 → 条件提取 → 字段映射 → 操作符转换 → API参数构建 → 调用执行 → 后置过滤 → 返回
```

## 技能注册中心

### 设计原则

- **单一数据源**：参数和Schema定义统一在子技能 SKILL.md 中管理
- **路由优先级**：支持技能优先级排序，解决冲突匹配
- **排他规则**：支持排他关键词和排他技能，避免误匹配
- **易于维护**：添加新技能只需填写必要字段

### 注册中心字段

```json
{
  "id": "cmdb-server-query",           // 技能唯一标识
  "name": "CMDB服务器查询",             // 显示名称
  "description": "查询服务器信息...",   // 功能描述
  "enabled": true,                      // 启用状态
  "path": "skills/cmdb-server-query/SKILL.md",  // 技能文件路径
  "keywords": ["服务器", "机房", "CMDB"],  // 路由关键词
  "version": "2.0.0",                   // 技能版本
  "priority": 5,                        // 路由优先级（数字越小优先级越高）
  "exclusive_keywords": ["公网IP", "部署"],  // 排他关键词
  "exclusive_skills": ["server-public-ip-query"]  // 排他技能
}
```

### 完整示例

```json
{
  "version": "2.0",
  "lastUpdated": "2026-06-18",
  "description": "技能元数据注册中心",
  "skills": [
    {
      "id": "cmdb-server-query",
      "name": "CMDB服务器查询",
      "description": "查询CMDB服务器信息，支持按机房、状态、类型等多条件组合查询",
      "enabled": true,
      "path": "skills/cmdb-server-query/SKILL.md",
      "keywords": ["服务器", "主机", "机房", "IP", "配置"],
      "version": "2.0.0",
      "priority": 5,
      "exclusive_keywords": ["公网IP", "外网", "带宽", "部署", "发布", "上线"],
      "exclusive_skills": ["server-public-ip-query", "project-deployment-query"]
    }
  ]
}
```

## 快速添加新技能

只需3步即可添加新技能：

1. **创建目录结构**：在 `skills/` 下创建新技能目录
2. **编写 SKILL.md**：定义技能的触发条件、工具调用Schema、执行流程、示例等
3. **注册技能**：在 `registry/skills.json` 中添加技能元数据（含优先级、排他规则）

详见：[新增技能快速参考](./quick-reference-for-adding-skills.md)

## 双平台兼容

```
┌─────────────────────────────────────────────────────────────┐
│              skill-agent/ (共享定义)                        │
│  ├── SKILL.md          ← 主技能入口（含路由规则）            │
│  ├── registry/skills.json  ← 技能元数据（含优先级、排他规则）│
│  ├── skills/*/SKILL.md  ← 子技能定义（含工具调用Schema）     │
│  └── mcp/*-schema.json  ← MCP Schema定义                   │
└─────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
    ┌──────────┐       ┌───────────┐       ┌───────────┐
    │   Dify   │       │ LangChain │       │  测试脚本  │
    └──────────┘       └───────────┘       └───────────┘
```
