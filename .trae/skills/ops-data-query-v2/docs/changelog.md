# Skill Agent 变更日志

## v3.0 (2026-06-18)

### 核心重构

- ✅ **执行流程明确化**: 主技能 SKILL.md 定义 Step 1-6 执行流程（意图识别→参数提取→参数校验→API调用→结果转换→输出响应）
- ✅ **配置文档化**: 移除 YAML 配置依赖，参数映射规则和 API 配置整合到 SKILL.md 文档中
- ✅ **路由规则标准化**: 定义技能路由优先级表（priority 1-5）和排他规则（exclusive_keywords、exclusive_skills）
- ✅ **输出标准化**: 定义统一的输出格式模板和错误输出格式

### Dify 兼容性增强

- ✅ **工具调用 Schema**: 5个子技能均添加标准 JSON Schema，支持 Agent 平台自动生成工具调用代码
- ✅ **完整调用示例**: 5个子技能均添加完整调用示例（用户输入→工具调用→API请求→API响应→最终输出）
- ✅ **MCP Schema 定义**: mcp/ 目录新增 5 个标准 MCP Schema JSON 文件

### 功能增强

- ✅ **上下文感知规则**: 支持多轮对话上下文保留（机房位置、项目名称、产品名称），有效期 5 分钟
- ✅ **参数组合规则**: 添加互斥参数、依赖参数、推荐组合规则
- ✅ **大数据量处理策略**: 超过 50 条记录自动分页，提供导航提示和筛选建议
- ✅ **数据导出支持**: 超过 100 条记录或用户明确要求时提供 CSV/Excel 导出选项

### 测试完善

- ✅ **路由测试**: 创建 `tests/test_routing.py`，验证技能匹配正确性
- ✅ **参数提取测试**: 创建 `tests/test_param_extraction.py`，验证参数提取和映射
- ✅ **输出格式测试**: 创建 `tests/test_output_format.py`，验证输出格式一致性
- ✅ **测试数据**: 创建 `tests/fixtures/test_cases.json`，包含完整测试用例

### 文档更新

- ✅ **架构文档更新**: 更新 `docs/architecture.md`，移除 YAML 配置引用，同步最新目录结构
- ✅ **优化指南**: 更新 `docs/optimization-guide.md`，添加 Agent 平台视角深度分析和改进方案
- ✅ **版本号管理**: 统一版本号为 3.0.0（主技能）/ 2.0.0（子技能）

### 向后兼容性

- 技能调用方式保持不变
- API 调用接口保持不变
- 只需重新注册技能元数据即可升级

---

## v2.0 (2026-05-20)

### 注册中心重大优化

- ✅ **精简注册中心字段**: 从 20+ 字段精简至 7 个核心字段（减少 65%）
  - 删除冗余字段：`schemaPath`, `examplesPath`, `category`, `tags`, `author`, `dependencies`, `parameters`, `api`, `responseFormat`, `toolType`
  - 保留核心字段：`id`, `name`, `description`, `enabled`, `path`, `keywords`, `version`
- ✅ **消除数据重复**: 删除与子技能 SKILL.md 中重复的 `keywords`, `parameters`, `description` 定义
- ✅ **简化顶层元数据**: 从 15+ 字段精简至 3 个字段
- ✅ **文件体积优化**: `registry/skills.json` 从 478 行精简至 52 行（减少 89%）

### 文档优化

- ✅ **新增快速添加技能指南**: 新增 `docs/quick-reference-for-adding-skills.md`，简化新增技能流程
  - 只需 3 步即可添加新技能
  - 提供精简字段说明和模板
  - 包含完成后检查清单
- ✅ **精简架构文档**: 更新 `docs/architecture.md`
  - 删除冗长的元数据结构示例
  - 简化目录结构说明
  - 聚焦核心架构信息
- ✅ **更新主 SKILL.md**: 精简目录结构说明，添加快速参考链接

### 设计原则

- **单一数据源原则**: 参数和 Schema 定义统一在子技能 SKILL.md 中管理
- **最小化配置**: 只保留技能发现和路由匹配的必要信息
- **易于维护**: 添加新技能只需填写 6 个核心字段

### 向后兼容性

- 技能调用方式保持不变
- API 调用接口保持不变
- 只需重新注册技能元数据即可升级

---

## v1.1.1 (2026-05-19)

### 文档完善

- ✅ **MCP API文档补全**: 完善5个接口的MCP API文档
  - `cmdb-mcp-api.md`: CMDB服务器查询接口
  - `server-public-ip-mcp-api.md`: 服务器公网IP查询接口
  - `product-mcp-api.md`: 产品查询接口
  - `project-deployment-mcp-api.md`: 项目部署查询接口
  - `project-basis-mcp-api.md`: 工程项目查询接口



### 文档更新

- ✅ 更新主 `SKILL.md`，添加完整的MCP文档列表
- ✅ 更新 `docs/architecture.md`，同步MCP文档目录结构
- ✅ 更新各技能的 `SKILL.md`，同步API文档说明

---

## v1.1.0 (2026-05-19)

### 新增功能

- ✅ **新增三个子技能**:
  - `server-public-ip-query`: 服务器公网IP查询技能
  - `product-query`: 产品查询技能
  - `project-basis-query`: 工程项目信息查询技能
- ✅ **技能关键词配置**: 所有技能新增 `keywords` 字段，提升意图识别准确率
- ✅ **技能名称统一**: 所有技能使用统一的 `name` 格式（如 `cmdb_server_search`）
- ✅ **单元测试覆盖**: 新增 `tests/test_skills.py`，覆盖所有技能的 Schema 验证和参数配置
- ✅ **聊天模拟测试**: 新增 `tests/test_chat_simulation.py`，支持模拟用户交互测试

### 架构优化

- ✅ **技能注册中心扩展**: 更新 `registry/skills.json`，添加所有5个技能的完整元数据
- ✅ **子技能标准化**: 所有子技能遵循统一的目录结构和文件格式
- ✅ **字段映射文档**: 每个子技能新增 `references/*.md` 字段映射文档
- ✅ **Schema 统一**: 所有子技能的 `schema.json` 遵循统一的 JSON Schema 规范

### Bug 修复

- ✅ **技能加载器 keywords 解析**: 修复 `skill_loader.ts` 中 YAML frontmatter 的 keywords 解析问题
- ✅ **重复工具注册**: 修复技能名称重复导致的工具重复注册问题
- ✅ **参数提取错误**: 修复自然语言参数提取不准确的问题

### 文档更新

- ✅ 更新 `SKILL.md`，添加完整的5个技能说明和使用示例
- ✅ 更新 `docs/architecture.md`，添加系统概述和技能列表
- ✅ 更新 `skills/README.md`，添加所有子技能的详细说明
- ✅ 更新版本信息，版本号升级至 v1.1.0

### 向后兼容性

- 原有的技能调用方式保持兼容
- API 调用接口保持不变

---

## v1.0.0 (2026-05-12)

### 新增功能

- ✅ **技能注册中心**: 新增 `registry/skills.json`，实现技能元数据集中管理
- ✅ **统一配置管理**: 新增 `config/` 目录，包含 `api.yaml`
- ✅ **参数校验配置**: 新增 `skills/*/config/params.yaml`，支持参数定义和校验规则


### 架构优化

- ✅ **主技能入口重构**: 更新 `SKILL.md`，整合技能路由和协调逻辑
- ✅ **子技能标准化**: 完善 `skills/cmdb-server-query/SKILL.md`，遵循标准技能格式
- ✅ **文档结构化**: 完善 `docs/architecture.md`，添加详细架构说明
- ✅ **目录结构优化**: 新增 `registry/` 和 `config/` 目录，规范文件组织

### 配置优化

- ✅ **API 配置集中化**: 将 API 端点、超时、重试配置统一到 `config/api.yaml`

- ✅ **日志配置增强**: 支持配置日志级别和数据显示长度

### Bug 修复

- ✅ **Dify 上传验证错误**: 修复参数类型定义问题，确保符合 Dify 规范


### 文档更新

- ✅ 更新 `SKILL.md`，添加技能注册中心说明
- ✅ 更新 `docs/architecture.md`，添加详细架构图和数据流说明
- ✅ 添加 `docs/changelog.md`，记录版本变更
- ✅ 更新 `mcp/cmdb-mcp-api.md`，完善 API 文档

### 向后兼容性

- 原有的技能调用方式保持兼容
- API 调用接口保持不变

---

## v0.1.0 (2026-05-10)

### 初始版本

- ✅ 创建 Skill Agent 基础架构
- ✅ 实现 CMDB 服务器查询技能

- ✅ 支持 Dify skill-agent 上传
- ✅ 实现基本的字段映射和参数转换