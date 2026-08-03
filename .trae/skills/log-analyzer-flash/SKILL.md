---
name: log-analyzer
description: 日志文件深度分析工具，支持大文件流式解析、错误统计、根因推断及多格式报告生成。当用户提供日志文件路径或需要分析错误日志时调用。适用于：(1) 排查线上故障，分析异常日志 (2) 批量检查多个日志文件中的错误模式 (3) 为运维报告生成结构化的错误分析文档 (4) 快速了解系统日志的整体健康状况。
---

# Log Analyzer — 日志深度分析工具

日志深度分析工具，支持大文件流式解析（>100MB 自动流式处理）、多格式自动识别、错误统计聚合、规则驱动的根因推断，以及 Markdown / JSON / HTML 三种格式报告生成。

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

# 生成 JSON 结构化报告
python3 scripts/analyze.py /path/to/app.log --format json

# 生成美观的 HTML 报告
python3 scripts/analyze.py /path/to/app.log --format html --output-dir ./reports

# 批量分析目录下所有日志文件
python3 scripts/analyze.py /var/log/myapp/ --format markdown --output-dir ./reports --verbose

# 支持 glob 通配符
python3 scripts/analyze.py "./logs/**/*.log" --format html --output-dir ./reports
```

### 脚本参数

| 参数 | 说明 |
|------|------|
| `path` | 日志文件路径、目录路径或 glob 通配符（**必填**） |
| `--format` | 输出格式：`markdown` / `json` / `html`（默认 markdown） |
| `--output-dir` / `-o` | 输出目录，指定后保存到文件而非打印到终端 |
| `--verbose` / `-v` | 显示详细进度信息 |

## 支持的日志格式

| 格式 | 示例 |
|------|------|
| **ISO 时间戳日志** | `[2024-01-01 12:00:00,123] ERROR [com.example.Service] Something broke` |
| **Syslog** | `Jan 15 10:30:45 hostname java[12345]: ERROR: null pointer` |
| **Nginx/Apache 访问日志** | `192.168.1.1 - - [10/Jan/2024:13:55:36 +0000] "GET /api HTTP/1.1" 200 1234` |
| **Java 异常堆栈** | 自动识别 `at com.example.Class.method(Class.java:42)` 等堆栈行 |
| **Caused by 链** | 自动跟踪 `Caused by: java.lang.NullPointerException: ...` |

## 分析报告结构

生成的报告包含以下主要板块：

1. **概览** — 源文件、总行数、错误行数、错误率、时间范围
2. **日志级别分布** — FATAL / ERROR / WARN / INFO / DEBUG / TRACE 的数量和占比
3. **高频错误 Top N** — 归一化后的高频错误签名（自动屏蔽变量如 UUID、IP、路径）
4. **根因分析** — 基于规则库匹配已知错误模式，给出建议
5. **模块分布** — 各模块/类的错误数排行（关联到具体错误级别）
6. **时间线趋势** — 按小时的日志量分布，用 ASCII 柱状图呈现
7. **综合建议** — 基于错误率和根因的文本建议

### 根因规则库

内置 15+ 条常见错误模式，覆盖以下类别：

- **Code Bugs**: NullPointerException, IndexOutOfBounds, ClassCastException
- **Resource**: OutOfMemoryError, Disk Full, OOM Killer
- **Network**: Connection Timeout, Connection Refused, Service Unavailable
- **Database**: SQLException, Deadlock, Connection Pool Exhausted
- **Deploy**: ClassNotFoundException, NoSuchMethodError
- **Config**: FileNotFoundException, Permission Denied

自定义规则请编辑 `references/patterns.md`。

## 架构说明

```
scripts/
├── analyze.py      # 主入口：参数解析 + 文件查找 + 流式解析 + 编排
├── parsers.py      # 日志格式解析器（iso / syslog / ncsa / java stack）
├── stats.py        # 统计引擎（级别分布 / 模块聚合 / 时间趋势 / 高频错误）
├── root_cause.py   # 根因推断（规则驱动的错误模式匹配）
└── reporters.py    # 报告生成器（Markdown / JSON / HTML 三种格式）
references/
└── patterns.md     # 错误模式规则参考
```

## 性能

- **流式处理**: 文件 > 100MB 自动启用 chunk-by-chunk 流式解析（64KB 每块）
- **小文件优化**: < 100MB 的全量加载模式
- **进度反馈**: verbose 模式下每 50 万行输出进度
- 实际测试：500MB 日志文件约 15-30 秒完成全流程分析（视日志复杂度而定）

## 示例

### 示例 1：分析单文件 (终端输出)

```bash
$ python3 scripts/analyze.py /var/log/app/error.log
```

输出（Markdown 摘要）:

```
📋 日志分析报告
- 源文件: /var/log/app/error.log
- 分析时间: 2026-06-24 15:00:00
- 耗时: 1.23s

## 概览
| 指标 | 值 |
|------|-----|
| 总行数 | 12,345 |
| 错误行数 | 234 |
| 错误率 | 1.9% |
| 时间范围 | 2026-06-23 08:00:00 ~ 2026-06-24 14:30:00 |
```

### 示例 2：生成 HTML 报告

```bash
$ python3 scripts/analyze.py /var/log/nginx/access.log --format html -o ./reports
📝 报告已保存: ./reports/access.log_report.html
```

### 示例 3：批量分析

```bash
$ python3 scripts/analyze.py ./logs/ --format json -o ./reports --verbose
📄 文件大小: 1.2 GB
🚰 流式模式: 启用
✅ 解析完成: 2,345,678 行 → 890,123 条结构化记录 (18.45s)
📝 报告已保存: ./reports/app.log_report.json
📝 报告已保存: ./reports/server.log_report.json
📊 批量分析完成: 2 个文件, 共 3,456,789 行, 12,345 条错误, 耗时 25.67s
```
