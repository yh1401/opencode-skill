# LangChain 集成使用指南

## 概述

Skill Agent 现在提供完整的 LangChain 框架集成，支持通过 LangChain Tool 原生接口调用所有查询技能。

## 文件结构

```
skill-agent/
├── langchain_integration.py  ← LangChain 集成模块（新增）
├── requirements.txt          ← Python 依赖（新增）
├── test_local.py            ← 完整测试脚本（新增）
├── test_simple.py           ← 简化测试脚本（新增）
└── ...
```

## 快速开始

### 1. 安装依赖

```bash
cd skill-agent
pip install -r requirements.txt
```

### 2. 运行测试

#### 选项 A：简化测试（无需依赖）

```bash
python test_simple.py
```

#### 选项 B：完整测试（需要 LangChain）

```bash
python test_local.py
```

### 3. 在代码中使用

```python
from langchain_integration import LangChainSkillAdapter

# 创建适配器
adapter = LangChainSkillAdapter()

# 获取 LangChain Tools
tools = adapter.get_langchain_tools()

# 或直接调用指定技能
result = adapter._invoke_skill("cmdb-server-query", "查找贵州机房的服务器")
print(result)
```

## API 参考

### LangChainSkillAdapter 类

#### 初始化

```python
adapter = LangChainSkillAdapter(skill_agent_path=None)
```

**参数**:
- `skill_agent_path`: Skill Agent 根目录路径，默认使用当前目录

#### 方法

##### get_langchain_tools()

获取 LangChain Tool 列表。

```python
tools = adapter.get_langchain_tools()
```

**返回**: `List[BaseTool]` - LangChain 工具列表

##### get_enabled_skills()

获取已启用的技能列表。

```python
skills = adapter.get_enabled_skills()
```

**返回**: `List[SkillConfig]` - 技能配置列表

##### _invoke_skill(skill_id, query)

调用指定技能（内部方法）。

```python
result = adapter._invoke_skill("cmdb-server-query", "查找贵州机房的服务器")
```

**参数**:
- `skill_id`: 技能 ID
- `query`: 查询字符串

**返回**: `str` - 技能执行结果

### SkillConfig 数据类

```python
@dataclass
class SkillConfig:
    id: str                    # 技能唯一标识
    name: str                  # 技能显示名称
    description: str            # 技能描述
    enabled: bool              # 是否启用
    path: str                  # SKILL.md 相对路径
    keywords: List[str]         # 关键词列表
    version: str = "1.0.0"    # 版本号
```

## 完整示例

### 示例 1: 基本使用

```python
from langchain_integration import LangChainSkillAdapter

# 初始化适配器
adapter = LangChainSkillAdapter()

# 获取所有 LangChain Tools
tools = adapter.get_langchain_tools()
print(f"加载了 {len(tools)} 个工具")

# 查看每个工具
for tool in tools:
    print(f"\n工具名: {tool.name}")
    print(f"描述: {tool.description[:80]}...")
```

### 示例 2: 与 LangChain Agent 集成

```python
from langchain_integration import LangChainSkillAdapter
from langchain.agents import initialize_agent
from langchain.llms import OpenAI

# 初始化适配器
adapter = LangChainSkillAdapter()
tools = adapter.get_langchain_tools()

# 创建 LLM
llm = OpenAI(temperature=0)

# 初始化 Agent
agent = initialize_agent(
    tools,
    llm,
    agent="zero-shot-react-description",
    verbose=True
)

# 运行查询
result = agent.run("查找贵州机房的服务器")
print(result)
```

### 示例 3: 直接调用单个技能

```python
from langchain_integration import LangChainSkillAdapter

adapter = LangChainSkillAdapter()

# 调用 CMDB 服务器查询
result = adapter._invoke_skill(
    "cmdb-server-query",
    "查询贵州机房的生产环境在线服务器"
)
print(result)
```

## 运行测试

### 简化测试（推荐先运行）

```bash
python test_simple.py
```

**测试内容**:
- ✓ 技能注册中心加载
- ✓ Mock 数据文件检查
- ✓ 目录结构验证

### 完整测试

```bash
pip install -r requirements.txt
python test_local.py
```

**测试内容**:
- ✓ 模块导入
- ✓ 技能加载
- ✓ LangChain Tools 创建
- ✓ 技能调用模拟

## 配置说明

### 技能启用/禁用

编辑 `registry/skills.json`，设置 `enabled` 字段：

```json
{
  "id": "cmdb-server-query",
  "name": "CMDB服务器查询",
  "enabled": true,   // 设置为 false 禁用
  ...
}
```

## 迁移指南

如果您已有 LangChain 项目，只需：

1. 将 `skill-agent` 目录添加到您的项目中
2. 添加 `langchain_integration.py` 到您的 Python 路径
3. 按照上述示例使用

## 故障排查

### 问题 1: ImportError - 找不到模块

**解决**:
```bash
# 确保 skill-agent 目录在 Python 路径中
export PYTHONPATH=/path/to/skill-agent:$PYTHONPATH
```

### 问题 2: ModuleNotFoundError - langchain 未安装

**解决**:
```bash
pip install -r requirements.txt
```

### 问题 3: FileNotFoundError - 找不到 skills.json

**解决**: 检查 `skill_agent_path` 参数是否指向正确的 Skill Agent 根目录

## 下一步

- 查看 [compatibility-report.md](./compatibility-report.md) 了解兼容性评估
- 查看 [architecture.md](./architecture.md) 了解系统架构
- 查看 [quick-reference-for-adding-skills.md](./quick-reference-for-adding-skills.md) 了解如何添加新技能

## 版本信息

- **版本**: LangChain Integration v1.0
- **兼容**: Skill Agent v2.0
- **更新日期**: 2026-05-20
