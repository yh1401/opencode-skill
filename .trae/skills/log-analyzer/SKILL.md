---
name: "日志文件分析工具"
description: "日志文件深度分析工具，支持大文件流式解析、多格式自动识别、错误统计、根因推断及多格式报告生成。当用户提供日志文件路径或需要分析错误日志时调用。适用于：(1) 排查线上故障，分析异常日志 (2) 批量检查多个日志文件中的错误模式 (3) 为运维报告生成结构化的错误分析文档 (4) 快速了解系统日志的整体健康状况。"
---

# 日志文件分析工具 — Log Analyzer

日志深度分析工具，支持大文件流式解析（>100MB 自动流式处理）、多格式自动识别、错误统计聚合、规则驱动的根因推断，以及 Markdown / JSON / HTML / Word / PDF 五种格式报告生成。

## 触发方式

当用户提出以下需求时自动触发本 Skill：

- "帮我分析一下这个日志文件"
- "看看日志里有什么错误"
- "排查一下这个日志的异常"
- "我的应用报错了，日志路径是 xxx"
- "这个 error.log 里有什么问题"
- 用户提供日志文件路径、上传日志文件、或粘贴日志内容

## 用法

### 基本用法

```bash
# 分析单个日志文件，输出 Markdown 报告到终端
python3 scripts/analyze.py /path/to/app.log

# 生成 JSON 和 HTML 报告
python3 scripts/analyze.py /path/to/app.log -o json html

# 生成 Word 报告
python3 scripts/analyze.py /path/to/app.log -o word

# 批量分析目录下所有日志文件
python3 scripts/analyze.py "./logs/**/*.log" -o markdown json

# 指定分块大小和最大处理行数
python3 scripts/analyze.py /path/to/app.log --chunk-size 50000 --max-lines 1000000
```

### 脚本参数

| 参数 | 说明 |
|------|------|
| `file` | 日志文件路径或 glob 模式（**必填**） |
| `--output / -o` | 输出格式：`markdown` / `json` / `html` / `word` / `pdf`（默认 markdown, json） |
| `--chunk-size` | 分块大小（行数），默认 10000 |
| `--max-lines` | 最大处理行数（可选） |
| `--batch` | 批量模式（处理匹配的多个文件） |
| `--json-output` | 输出 JSON 格式结果到终端 |

## 目录结构

```
.trae/skills/log-analyzer/
├── SKILL.md                  # 本技能定义文件
├── config.yaml               # 运行配置
├── requirements.txt          # Python 依赖
├── scripts/                  # 核心脚本模块
│   ├── analyze.py            # 主入口：参数解析 + 文件查找 + 流式解析 + 编排
│   ├── parsers.py            # 日志格式解析器（java_app / iso / syslog / ncsa / json_log）
│   ├── stats.py              # 统计引擎（级别分布 / 模块聚合 / 时间趋势 / 高频错误）
│   ├── root_cause.py         # 根因推断（规则驱动的错误模式匹配）
│   └── reporters.py          # 报告生成器（Markdown / JSON / HTML / Word / PDF）
├── docs/                     # 文档
│   ├── patterns.md           # 错误模式规则参考
│   ├── guide.md              # 使用指南
│   └── config_guide.md       # 配置说明
├── test/                     # 测试
│   └── test_log_analysis.py  # 测试用例
├── reports/                  # 生成的报告输出目录
└── logs/                     # 运行日志输出目录
```

## 能力

### 核心功能
| 能力 | 描述 |
|------|------|
| 大文件流式解析 | 采用分块流式处理，支持 GB 级日志文件，内存占用可控 |
| 多格式自动识别 | 自动识别 Java 应用日志、ISO 时间戳、Syslog、NCSA、JSON 日志等多种格式 |
| 分块并行处理 | 支持多线程并行分析，提升处理效率 |
| 错误统计 | 自动统计错误级别分布、错误类型、高频错误类、模块分布 |
| 趋势分析 | 按小时的日志量分布，检测峰值和异常波动 |
| 规则分析 | 基于预定义规则的错误分类、根因推断和处置建议 |
| 错误归一化 | 智能合并相似错误（自动屏蔽变量如 UUID、IP、路径、时间戳） |
| 健康度评分 | 基于错误率和警告率计算日志健康度评分（0-100） |
| 多格式报告 | Markdown / JSON / HTML / Word / PDF 五种格式输出 |

### 支持的日志格式

| 格式 | 示例 |
|------|------|
| **Java 应用日志** | `2026-06-14 10:30:45.123 [thread-01] ERROR ClassName - NullPointerException: null object` |
| **ISO 时间戳日志** | `[2024-01-01 12:00:00,123] ERROR [com.example.Service] Something broke` |
| **Syslog** | `Jan 15 10:30:45 hostname java[12345]: ERROR: null pointer` |
| **Nginx/Apache 访问日志** | `192.168.1.1 - - [10/Jan/2024:13:55:36 +0000] "GET /api HTTP/1.1" 200 1234` |
| **Java 异常堆栈** | 自动识别 `at com.example.Class.method(Class.java:42)` 等堆栈行 |
| **Caused by 链** | 自动跟踪 `Caused by: java.lang.NullPointerException: ...` |
| **JSON 日志** | `{"level": "ERROR", "timestamp": "2024-01-01T12:00:00Z", "message": "..."}` |

解析字段：`timestamp` | `thread` | `level` | `trace_id` | `module` | `message`

自动提取：`error_type`、`error_message`、`stack_trace`、`http_status`、`http_path`

## 分析报告结构

生成的报告包含以下主要板块：

1. **概览** — 源文件、总行数、错误行数、错误率、时间范围
2. **健康度评分** — 基于错误率和警告率的综合评分（0-100）
3. **日志级别分布** — FATAL / ERROR / WARN / INFO / DEBUG / TRACE 的数量和占比
4. **时间趋势分析** — 按小时的日志量分布，检测峰值和异常波动
5. **高频错误 Top N** — 归一化后的高频错误签名（自动屏蔽变量）
6. **模块分布** — 各模块/类的错误数排行
7. **根因分析** — 基于规则库匹配已知错误模式，给出建议
8. **综合建议** — 基于错误率和根因的文本建议

### 根因规则库

内置 12+ 条常见错误模式，覆盖以下类别：

- **数据库**: Database Connection Failed, SQLException, Deadlock
- **内存**: OutOfMemoryError, Memory Leak
- **网络**: SocketTimeout, Connection Timeout, SSL Certificate Error
- **线程**: Thread Pool Exhausted
- **磁盘**: Disk Full
- **认证**: Authentication Failed, JWT Expired
- **配置**: Configuration Error
- **缓存**: Cache Miss
- **限流**: Rate Limit Exceeded
- **依赖**: Service Unavailable, Downstream Error

自定义规则请编辑 `docs/patterns.md`。

### 严重程度分级
| 级别 | 描述 | 示例错误类型 |
|------|------|-------------|
| critical | 致命错误，服务崩溃或不可用 | NullPointerException, OutOfMemoryError, StackOverflowError, Deadlock |
| high | 高优先级错误，影响核心功能 | SQLException, IOException, TimeoutException, AuthenticationException |
| medium | 中等优先级错误，功能异常 | IllegalArgumentException, ValidationException, Cache Miss |
| low | 低优先级警告，不影响核心功能 | Warning, Deprecation, Rate Limit Exceeded |

## 配置

`config.yaml` 控制所有行为，关键配置：

```yaml
parser:
  chunk_size: 10000           # 每块处理行数
  max_lines: null             # 最大处理行数（null 表示不限制）

processor:
  enable_checkpoint: true     # 断点续传
  parallel_workers: 4         # 并行线程数

report:
  output_dir: ./reports       # 报告输出目录
```

## 性能

- **流式处理**: 分块逐行解析，内存占用可控
- **进度反馈**: 每 10000 行输出进度
- **实际测试**: 500MB 日志文件约 15-30 秒完成全流程分析（视日志复杂度而定）

## 扩展接口

### 添加自定义日志格式

在 `scripts/parsers.py` 的 `FORMAT_PATTERNS` 列表中添加新的格式定义：

```python
("custom_format", re.compile(
    r'^(?P<ts>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) '
    r'(?P<level>[A-Z]+) '
    r'(?P<message>.+)',
), {}),
```

### 添加自定义根因规则

在 `scripts/root_cause.py` 的 `RootCauseEngine._load_patterns()` 方法中添加新规则：

```python
{
    "id": "custom_error",
    "name": "自定义错误",
    "severity": "medium",
    "patterns": [r"CustomException.*"],
    "cause": "自定义错误原因",
    "remediation": ["建议措施"],
    "impact": "影响描述",
}
```

### 添加自定义报告格式

在 `scripts/reporters.py` 中添加新的报告生成方法，并在 `format_handlers` 字典中注册。

## 触发条件

当用户遇到以下场景时调用此 skill：
1. 提供了日志文件路径，要求分析错误日志
2. 需要统计日志中的错误类型和频率
3. 需要从日志中定位故障根因
4. 需要生成格式化的日志分析报告
5. 排查系统异常、服务故障等问题
6. 批量检查多个日志文件中的错误模式
7. 需要了解系统日志的整体健康状况

## LLM 提示词

当与 LLM 集成时，使用以下提示词模板：

```
你是一个专业的日志分析助手。请根据以下分析结果，为用户提供清晰、专业的日志分析报告：

分析结果：
{analysis_result}

请输出：
1. 问题概述：简要说明发现的主要问题
2. 严重问题：列出 critical 和 high 级别的问题
3. 建议措施：针对每个问题提供具体的解决建议
4. 后续步骤：建议的下一步操作
```