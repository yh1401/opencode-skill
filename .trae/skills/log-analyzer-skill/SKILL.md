---
name: log-analyzer-skill
description: 日志文件深度分析工具，支持大文件流式解析、错误统计、趋势分析、健康度评分、根因推断及多格式报告生成。当用户提供日志文件路径或需要分析错误日志时调用。
version: 4.0.0
author: dev-team
compatibility: platform-agnostic
---

# Log Analyzer Skill

## 技能概述

本技能是一个**平台无关**的日志分析工具，可以在任何支持自然语言工具调用的 Agent 平台（Trae、Dify、LangChain、OpenCode 等）上运行。

**核心设计原则**：
- 日志的**解析/统计/根因推断**由自包含的 Python 脚本完成（零外部依赖）
- 日志的**深度分析/根因报告生成**由平台的 LLM 使用内置的专家提示词完成
- 大文件采用**流式分块处理**（64KB chunks），内存占用稳定
- 支持**多格式自动识别**：ISO 时间戳、Syslog、NCSA (Apache/Nginx)

## 能力矩阵

| 能力 | 实现方式 | 说明 |
|------|---------|------|
| 大文件流式解析 | Python 脚本 | 64KB 分块，>100MB 自动流式 |
| 多格式自动识别 | Python 脚本 | ISO / Syslog / NCSA / Java 堆栈 |
| 错误类型提取 | Python 脚本 | 正则提取 Exception/Error 类型 |
| 错误频率统计 | Python 脚本 | 按级别、类型、模块维度统计 |
| 错误归一化 | Python 脚本 | 自动屏蔽 UUID / IP / ID 等变量 |
| **趋势分析** | Python 脚本 | 按小时统计，识别峰值/上升/下降/稳定趋势 |
| **健康度评分** | Python 脚本 | 0-100 分评分，综合错误率和警告率 |
| **详细根因分析** | Python 脚本 | 包含原因、影响、修复措施的详细规则 |
| 规则根因推断 | Python 脚本 | 18+ 条内建规则覆盖常见错误模式 |
| **多格式报告** | Python 脚本 | Markdown / JSON / HTML / Word / PDF 五种格式 |
| 批量处理 | Python 脚本 | 支持目录、glob 通配符批量分析 |
| 根因深度分析 | LLM 提示词 | 基于证据的因果链分析 |
| 处置与整改建议 | LLM 提示词 | 应急止血、根因修复、架构改进 |

## 目录结构

```
.trae/skills/log-analyzer-skill/
├── SKILL.md                # 本文件 - 技能定义（含完整分析提示词）
├── requirements.txt        # 最小依赖（仅 pyyaml 可选）
├── scripts/
│   ├── analyze.py          # 主入口：参数解析 + 文件查找 + 流式解析 + 编排
│   ├── parsers.py          # 日志格式解析器（ISO / Syslog / NCSA / Java Stack）
│   ├── stats.py            # 统计引擎（级别分布 / 模块聚合 / 时间趋势 / 高频错误）
│   ├── root_cause.py       # 根因推断引擎（18+ 条规则驱动的错误模式匹配）
│   └── reporters.py        # 报告生成器（Markdown / JSON / HTML 三种格式）
└── references/
    └── patterns.md         # 错误模式规则参考文档
```

## 工作流程

```
用户提供日志文件路径
       │
       ▼
步骤1: Python 脚本解析日志 (analyze.py)
       ├── 流式读取（64KB chunks，>100MB 自动启用）
       ├── 自动识别格式（ISO / Syslog / NCSA / Java Stack）
       ├── 提取字段（时间戳、级别、模块、错误类型、HTTP 状态码）
       ├── 错误归一化（屏蔽 UUID / IP / ID 等变量）
       └── 输出结构化 JSON / Markdown / HTML
       │
       ▼
步骤2: 统计引擎 (stats.py)
       ├── 按级别统计分布
       ├── 按错误类型聚合
       ├── 按模块/类名聚合
       ├── 按时间窗口统计趋势
       └── 计算错误率
       │
       ▼
步骤3: 根因推断 (root_cause.py)
       ├── 18+ 条内置规则匹配常见模式
       ├── 按严重度排序
       ├── 生成综合建议
       └── 输出规则驱动的根因分析
       │
       ▼
步骤4: 报告生成 (reporters.py)
       ├── Markdown 报告（默认，终端输出）
       ├── JSON 报告（结构化数据）
       └── HTML 报告（美观可视化）
       │
       ▼
步骤5: LLM 深度分析（可选）
       ├── 将步骤4数据注入 LLM 专家提示词
       ├── 根因推断（直接原因 + 根本原因）
       ├── 故障因果链与证据链构建
       ├── 处置动作建议
       └── 整改建议（立即/短期/长期）
```

## 支持的日志格式

| 格式 | 示例 |
|------|------|
| **ISO 时间戳日志** | `2024-01-01 12:00:00.123 [thread-1] ERROR com.example.Service - NullPointerException` |
| **Syslog** | `Jan 15 10:30:45 hostname java[12345]: ERROR: null pointer at ...` |
| **Nginx/Apache 访问日志** | `192.168.1.1 - - [10/Jan/2024:13:55:36 +0000] "GET /api HTTP/1.1" 200 1234` |
| **Java 异常堆栈** | 自动识别 `at com.example.Class.method(Class.java:42)` 等堆栈行 |
| **Caused by 链** | 自动跟踪 `Caused by: java.lang.NullPointerException: ...` |
| **JSON 日志** | `{"timestamp": "2024-01-15T10:00:00Z", "level": "ERROR", "message": "error message", "module": "service-name"}` |

## 用法

### 基本用法

```bash
# 分析单个日志文件，输出 Markdown 报告到终端
python3 scripts/analyze.py /path/to/app.log

# 生成 JSON 结构化报告
python3 scripts/analyze.py /path/to/app.log --format json

# 生成美观的 HTML 报告
python3 scripts/analyze.py /path/to/app.log --format html --output-dir ./reports

# 生成 Word 报告（需要 python-docx）
python3 scripts/analyze.py /path/to/app.log --format word --output-dir ./reports

# 生成 PDF 报告（需要 weasyprint）
python3 scripts/analyze.py /path/to/app.log --format pdf --output-dir ./reports

# 批量分析目录下所有日志文件
python3 scripts/analyze.py /var/log/myapp/ --format markdown --output-dir ./reports --verbose

# 支持 glob 通配符
python3 scripts/analyze.py "./logs/**/*.log" --format html --output-dir ./reports
```

### 脚本参数

| 参数 | 说明 |
|------|------|
| `path` | 日志文件路径、目录路径或 glob 通配符（**必填**） |
| `--format` | 输出格式：`markdown` / `json` / `html` / `word` / `pdf`（默认 markdown） |
| `--output-dir` / `-o` | 输出目录，指定后保存到文件而非打印到终端 |
| `--verbose` / `-v` | 显示详细进度信息 |

### 输出 JSON 结构

```json
{
  "report_meta": {
    "tool": "log-analyzer-skill",
    "source_file": "/var/log/app.log",
    "generated_at": "2026-06-24T15:00:00",
    "duration_sec": 1.23
  },
  "overview": {
    "total_lines": 1500000,
    "error_lines": 1234,
    "error_rate": 0.082,
    "time_range": {
      "first": "2026-06-24T08:00:00",
      "last": "2026-06-24T14:30:00"
    }
  },
  "health_score": 65,
  "trend_analysis": {
    "trend_type": "increasing",
    "description": "日志量呈上升趋势，需关注",
    "peak_hours": ["2026-06-24T13:00"],
    "low_hours": ["2026-06-24T08:00"],
    "avg_count": 100000,
    "max_count": 150000,
    "min_count": 50000
  },
  "level_distribution": {
    "ERROR": 890,
    "FATAL": 344,
    "WARN": 567,
    "INFO": 1482199
  },
  "error_types": {
    "NullPointerException": 456,
    "SQLException": 234,
    "TimeoutError": 123
  },
  "top_errors": [
    {"message": "Cannot invoke method on null object", "count": 456, "severity": "ERROR"}
  ],
  "root_causes": [
    {
      "id": "null_pointer",
      "name": "NullPointerException",
      "severity": "high",
      "category": "code_bug",
      "count": 456,
      "cause": "对象引用为 null 但被访问，通常是未初始化、返回 null 未校验或并发修改",
      "impact": "当前请求失败，可能导致级联错误",
      "remediation": [
        "检查空指针引用的对象是否已正确初始化",
        "确认是否为上游返回 null 未加校验",
        "使用 Optional 包装可能为 null 的返回值"
      ],
      "suggestion": "检查空指针引用的对象是否已正确初始化..."
    }
  ],
  "summary": {
    "total_issues": 5,
    "critical_count": 1,
    "high_count": 2,
    "medium_count": 1,
    "low_count": 1,
    "description": "发现 1 个严重问题、2 个高风险问题，需要立即处理。"
  },
  "recommendation": "⚠️ 错误率 0.08%...🔍 最频繁的错误模式：NullPointerException..."
}
```

## 根因规则库

内置 18+ 条常见错误模式，覆盖以下类别：

- **Code Bugs**: NullPointerException, IndexOutOfBounds, ClassCastException, IllegalArgumentException
- **Resource**: OutOfMemoryError, Disk Full, OOM Killer
- **Network**: Connection Timeout, Connection Refused, Connection Reset
- **Database**: SQLException, Deadlock, Connection Pool Exhausted
- **Deploy**: ClassNotFoundException, NoSuchMethodError, NoClassDefFoundError
- **Config**: FileNotFoundException, Permission Denied
- **Validation**: Validation Error, Illegal Argument
- **Traffic**: Rate Limited, Service Unavailable

自定义规则请编辑 `references/patterns.md`。

## 性能

- **流式处理**: 文件 > 100MB 自动启用 chunk-by-chunk 流式解析（64KB 每块）
- **小文件优化**: < 100MB 的全量加载模式
- **进度反馈**: verbose 模式下每 50 万行输出进度
- 实际测试：500MB 日志文件约 15-30 秒完成全流程分析

## 集成到 Agent 平台

### Trae IDE（原生）

在对话中直接说"分析这个日志"，Trae 会自动检测到 `log-analyzer-skill` 并调用。

### Dify / LangChain / 其他平台

```python
import subprocess, json

# 步骤1: 调用脚本解析日志
result = subprocess.run(
    ["python3", "scripts/analyze.py", "--format", "json", "/var/log/app.log"],
    capture_output=True, text=True
)
analysis_data = json.loads(result.stdout)

# 步骤2: 将 analysis_data 注入 SKILL.md 中的 LLM 专家提示词
# 发送给 LLM 进行深度分析（可选）

# 步骤3: LLM 返回结构化 JSON 报告
```

## LLM 深度分析提示词

将 `scripts/analyze.py` 输出的 JSON 数据注入以下系统提示词，可以让 LLM 进行更深度的分析。

### System Prompt

你是一个资深的专业日志分析工程师和故障排查专家，擅长分析应用程序错误日志、定位故障根因、构建证据链，并提供可落地的处置和整改方案。

你的任务是对提供的错误日志数据进行深度分析，生成一份结构化的故障分析总结报告。

#### 一、故障时间线
- **首次异常时间** / **故障确认时间** / **错误峰值时间** / **故障恢复时间** / **时间跨度分析**

#### 二、根因推断
- **直接原因**: 直接触发故障的表面原因
- **根本原因**: 深层次的系统性问题
- **置信度**: 高/中/低，并说明依据

#### 三、故障因果链与证据链

**因果链**: `[故障源头] → [中间环节] → ... → [最终表现]`
每个环节包含：原因、结果、证据（关键日志）

**证据链**: 时间戳、证据类型（日志/异常/指标）、证据内容、关联度（直接/间接）

#### 四、处置动作建议
1. **应急止血**（立即）：降级/熔断/切换/隔离
2. **排查定位**（止血后）：日志检索/监控/链路追踪
3. **恢复验证**（修复后）：功能验证/监控观察/灰度发布

#### 五、整改建议
- **立即处置**（1小时内）：配置调整/服务重启
- **根因解决**（1-2周）：代码修复/异常处理
- **架构改进**（1-3月）：熔断降级/监控告警

#### 输出格式

```json
{
  "summary": "故障分析摘要",
  "timeline": {
    "key_events": [{"time": "", "event_type": "", "description": ""}],
    "total_duration": "总时长"
  },
  "root_cause": {
    "direct_cause": "",
    "fundamental_cause": "",
    "confidence": "high|medium|low",
    "reasoning": ""
  },
  "causal_chain": {
    "chain_steps": [{"step": 1, "cause": "", "effect": "", "evidence": ""}]
  },
  "evidence_chain": {
    "evidences": [{"timestamp": "", "type": "", "content": "", "relevance": ""}]
  },
  "response_actions": {
    "emergency": [],
    "troubleshooting": [],
    "recovery": []
  },
  "remediation": {
    "immediate": [],
    "root_cause_fix": [],
    "architecture_monitoring": []
  }
}
```

## 使用示例

### 示例 1：基础分析

**用户输入**："分析 /var/log/app.log 的错误日志"

**执行步骤**：
1. `python3 scripts/analyze.py /var/log/app.log --format json`
2. 将 JSON 输出展示给用户

### 示例 2：大文件分析

**用户输入**："分析这个 2GB 的日志文件，最近一小时错误很多"

**执行步骤**：
1. `python3 scripts/analyze.py huge.log --format json --verbose`（自动启用流式模式）
2. 提取错误样本和统计信息
3. 聚焦分析高峰时段的错误模式

### 示例 3：批量分析目录

**用户输入**："分析 /var/log/myapp/ 目录下所有日志"

**执行步骤**：
1. `python3 scripts/analyze.py /var/log/myapp/ --format html --output-dir ./reports --verbose`
2. 生成所有日志文件的 HTML 报告

## 常见问题

### 如何支持自定义错误模式？

编辑 `scripts/root_cause.py` 中的 `ERROR_PATTERNS` 列表，添加自定义模式：

```python
ERROR_PATTERNS.append({
    "id": "my_custom_error",
    "name": "MyCustomError",
    "pattern": re.compile(r'my custom error pattern'),
    "severity": "high",
    "category": "custom",
    "suggestion": "如何处理这个错误",
})
```

### 如何处理 GB 级文件？

脚本会自动检测文件大小，>100MB 自动启用 64KB 分块流式处理。
处理 2GB 文件的内存占用通常在 50-100MB。

### 如何集成到不同平台？

| 平台 | 集成方式 |
|------|---------|
| Dify | 创建自定义工具 → 执行 `scripts/analyze.py` → 解析输出 |
| LangChain | 定义 `StructuredTool` → 调用脚本 → 构建 Prompt |
| Trae IDE | 原生支持，自动检测 SKILL.md |
| 其他 | 调用脚本获取 JSON → 发送给 LLM |

## 版本信息

| 版本 | 日期 | 更新内容 |
|------|------|----------|
| 4.0.0 | 2026-06-25 | 新增 JSON 日志解析、趋势分析、健康度评分、详细根因（原因+影响+修复措施）、Word/PDF 报告 |
| 3.0.0 | 2026-06-24 | 模块化重构，多格式解析/根因引擎/HTML报告/批量处理 |
| 2.0.0 | 2026-06-24 | 平台无关版本，零外部依赖 |
| 1.0.0 | - | 原始 log-analyzer 版本 |
