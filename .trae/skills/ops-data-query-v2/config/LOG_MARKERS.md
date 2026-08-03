# 日志标识说明文档

## 概述

为了方便追踪和调试技能调用流程，系统在关键日志点添加了图标标识。这些图标可以帮助快速识别日志类型和调用状态。

## 图标标识列表

### 技能代理核心模块

| 图标 | 标识名称 | 含义 | 文件位置 |
|------|----------|------|----------|
| 🤖 | SkillAgent | 技能代理初始化 | `skill_agent.ts` |
| 🔮 | run_llm_start | LLM 模式执行开始 | `skill_agent.ts` |
| 🔗 | tool_call_start | 子技能工具调用开始 | `skill_agent.ts` |
| 📊 | tool_result | 工具调用结果返回 | `skill_agent.ts` |

### 技能桥接器模块

| 图标 | 标识名称 | 含义 | 文件位置 |
|------|----------|------|----------|
| 🚀 | initialized | SkillAgentBridge 初始化 | `skillAgentBridge.ts` |
| 📋 | skills_loaded | 技能配置加载完成 | `skillAgentBridge.ts` |
| 📦 | mock_data_mapped | Mock 数据映射 | `skillAgentBridge.ts` |
| 📥 | mock_data_loaded | Mock 数据加载完成 | `skillAgentBridge.ts` |
| 🔗 | invoke_skill_start | 开始调用子技能（分隔线开始） | `skillAgentBridge.ts` |
| 📋 | skill_info | 技能详细信息 | `skillAgentBridge.ts` |
| 📦 | mock_data_found | Mock 数据已加载 | `skillAgentBridge.ts` |
| 🔍 | filter_start | 开始根据查询条件过滤数据 | `skillAgentBridge.ts` |
| 🔍 | filter_complete | 数据过滤完成 | `skillAgentBridge.ts` |
| ✅ | invoke_skill_success | 子技能调用成功完成 | `skillAgentBridge.ts` |
| ❌ | skill_not_found | 技能不存在 | `skillAgentBridge.ts` |
| ❌ | skill_not_enabled | 技能未启用 | `skillAgentBridge.ts` |
| ❌ | mock_data_not_found | 未找到 Mock 数据 | `skillAgentBridge.ts` |

### 工具调用模块

| 图标 | 标识名称 | 含义 | 文件位置 |
|------|----------|------|----------|
| 🔧 | call_start | 工具调用开始 | `skillAgentTool.ts` |
| ✅ | call_end | 工具调用成功完成 | `skillAgentTool.ts` |
| ❌ | call_error | 工具调用出错 | `skillAgentTool.ts` |

### 外部 API 调用模块

| 图标 | 标识名称 | 含义 | 文件位置 |
|------|----------|------|----------|
| 🌐 | query_start | 查询开始 | `project_deployment_tool.ts` |
| 📡 | using_mock | 使用 Mock 数据 | `project_deployment_tool.ts` |
| 🔌 | using_real_api | 使用真实 API（分隔线标识） | `project_deployment_tool.ts` |
| 📤 | api_request_start | API 请求开始（包含请求ID、URL、方法） | `project_deployment_tool.ts` |
| 📤 | api_request_headers | 请求头信息 | `project_deployment_tool.ts` |
| 📤 | api_request_params | 请求参数明细 | `project_deployment_tool.ts` |
| 📤 | api_request_full | 请求结束标记 | `project_deployment_tool.ts` |
| 📥 | api_response_start | API 响应开始（包含状态码、耗时） | `project_deployment_tool.ts` |
| 📥 | api_response_meta | 响应元数据（code、message、数据量） | `project_deployment_tool.ts` |
| 📥 | api_response_summary | 响应摘要 | `project_deployment_tool.ts` |
| ❌ | api_request_failed | API 请求失败 | `project_deployment_tool.ts` |
| ❌ | api_response_error | 错误响应详情 | `project_deployment_tool.ts` |

## 调用流程追踪

通过图标可以快速追踪完整的技能调用流程：

```
🤖 (初始化) → 🔮 (LLM执行) → 🔗 (工具调用) → 🌐 (查询开始)
                                           ↓
                              ┌────────────┴────────────┐
                              ↓                         ↓
                         📡 (Mock数据)              🔌 (真实API)
                              ↓                         ↓
                         ✅ (完成)              📤 (请求) → 📥 (响应) → ✅ (完成)
                              ↓                         ↓
                             ❌ (出错)                  ❌ (出错)
```

### 成功调用链示例（Mock 模式）

```
[LOG] [🤖 SkillAgent] initialized - 技能代理初始化完成
[LOG] [🔮 SkillAgent] run_llm_start - 开始LLM模式执行
[LOG] [🔗 SkillAgent] tool_call_start - 调用子技能工具
[LOG] [🔧 SkillAgentTool] call_start - 工具调用开始
[LOG] [🔗 SkillAgentBridge] invoke_skill_start - ┌─ 🎯 开始调用子技能 ──────────────────────
[LOG] [📋 SkillAgentBridge] skill_info - │  技能信息
[LOG] [📦 SkillAgentBridge] mock_data_found - │  ✅ Mock数据已加载
[LOG] [🔍 SkillAgentBridge] filter_start - │  开始过滤数据
[LOG] [🔍 SkillAgentBridge] filter_complete - │  过滤完成: 10 -> 3 条
[LOG] [✅ SkillAgentBridge] invoke_skill_success - └──────────────────────────────────
[LOG] [✅ SkillAgentTool] call_end - 工具调用成功完成
[LOG] [📊 SkillAgent] tool_result - 工具执行完成
```

### 成功调用链示例（真实 API 模式）

```
[LOG] [🤖 SkillAgent] initialized - 技能代理初始化完成
[LOG] [🔮 SkillAgent] run_llm_start - 开始LLM模式执行
[LOG] [🔗 SkillAgent] tool_call_start - 调用子技能工具
[LOG] [🔧 SkillAgentTool] call_start - 工具调用开始
[LOG] [🌐 ProjectDeploymentQueryTool] query_start - 开始查询项目部署信息
[LOG] [🔌 ProjectDeploymentQueryTool] using_real_api - ========== 使用真实 API ==========
[LOG] [📤 ProjectDeploymentQueryTool] api_request_start - ┌─ 🔔 发起 API 请求 ──────────────────────
[LOG] [📤 ProjectDeploymentQueryTool] api_request_headers - │  请求头信息
[LOG] [📤 ProjectDeploymentQueryTool] api_request_params - │  请求参数
[LOG] [📤 ProjectDeploymentQueryTool] api_request_full - └──────────────────────────────────
[LOG] [📥 ProjectDeploymentQueryTool] api_response_start - ┌─ ✅ API 响应成功 ──────────────────────
[LOG] [📥 ProjectDeploymentQueryTool] api_response_meta - │  响应元数据
[LOG] [📥 ProjectDeploymentQueryTool] api_response_summary - └──────────────────────────────────
[LOG] [✅ ProjectDeploymentQueryTool] query_success - 查询成功
```

### 错误调用链示例

```
[LOG] [🤖 SkillAgent] initialized - 技能代理初始化完成
[LOG] [🔮 SkillAgent] run_llm_start - 开始LLM模式执行
[LOG] [🔗 SkillAgent] tool_call_start - 调用子技能工具
[LOG] [🔧 SkillAgentTool] call_start - 工具调用开始
[LOG] [🌐 ProjectDeploymentQueryTool] query_start - 开始查询项目部署信息
[LOG] [🔌 ProjectDeploymentQueryTool] using_real_api - ========== 使用真实 API ==========
[LOG] [📤 ProjectDeploymentQueryTool] api_request_start - ┌─ 🔔 发起 API 请求 ──────────────────────
[LOG] [📤 ProjectDeploymentQueryTool] api_request_headers - │  请求头信息
[LOG] [📤 ProjectDeploymentQueryTool] api_request_params - │  请求参数
[LOG] [📤 ProjectDeploymentQueryTool] api_request_full - └──────────────────────────────────
[LOG] [❌ ProjectDeploymentQueryTool] api_request_failed - ┌─ ❌ API 请求失败 ──────────────────────
[LOG] [❌ ProjectDeploymentQueryTool] api_response_error - │  错误响应详情
[LOG] [❌ ProjectDeploymentQueryTool] api_request_end - └──────────────────────────────────
[LOG] [❌ ProjectDeploymentQueryTool] query_error - 查询失败
```

## 日志级别说明

| 级别 | 图标 | 用途 |
|------|------|------|
| INFO | 无特殊图标 | 一般信息记录 |
| DEBUG | 无特殊图标 | 调试详细信息 |
| WARN | ⚠️ | 警告信息 |
| ERROR | ❌ | 错误信息 |

## 使用建议

1. **快速定位问题**：通过 ❌ 图标快速定位错误日志
2. **追踪调用链**：通过 🤖 → 🔮 → 🔗 → 📊 → ✅ 的顺序追踪完整调用流程
3. **性能分析**：通过 ✅ 图标后的 duration 字段分析调用耗时

## 维护说明

- 新增日志点时应考虑添加合适的图标标识
- 图标应与其代表的含义直观相关
- 避免过度使用图标，仅在关键节点添加

---

*文档版本: 1.0*  
*创建时间: 2026-05-21*
