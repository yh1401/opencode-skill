# Log Analysis Skill 使用指南

> 版本: 1.0.0 | 更新日期: 2026-06-16

---

## 概述

Log Analysis Skill 是一个**无需启动服务**的日志分析工具。通过执行脚本即可完成对日志文件的深度分析，生成包含错误统计、根因推断和处置建议的结构化报告。

## 目录结构

```
skill/log_anglyz_skill/
├── SKILL.md              # 技能定义文件
├── config.yaml           # 运行配置
├── requirements.txt      # Python 依赖
├── scripts/              # 可执行脚本
│   ├── init_skill.py     # 初始化脚本
│   ├── analyze_log.py    # 单文件分析脚本
│   └── batch_analyze.py  # 批量分析脚本
├── src/                  # 核心代码库
│   ├── handler.py        # 分析处理器
│   ├── tools.py          # 外部工具/API封装
│   └── utils.py          # 通用工具函数
├── references/           # 参考文档
│   ├── guide.md          # 本文件
│   └── config_guide.md   # 配置说明
└── test/                 # 测试用例
```

## 快速开始

### 第一步：初始化环境

```bash
cd skill/log_anglyz_skill
python scripts/init_skill.py
```

`init_skill.py` 会：
- 检查 Python 版本（需要 3.8+）
- 检查核心模块是否可导入
- 安装缺少的依赖
- 创建必要的目录（output/、checkpoints/、logs/）

### 第二步：分析日志文件

#### 单文件分析

```bash
python scripts/analyze_log.py --file /var/log/application.log
```

#### 批量分析

```bash
python scripts/batch_analyze.py --files "/var/log/app1.log,/var/log/app2.log"
```

## 脚本详解

### init_skill.py — 环境初始化

**用途**: 检查运行环境并安装依赖

**参数**:

| 参数 | 说明 |
|------|------|
| `--check` | 仅检查环境，不安装依赖 |
| `--install` | 强制重新安装所有依赖 |
| `--verbose` / `-v` | 显示详细日志 |

**示例**:

```bash
# 仅检查环境
python scripts/init_skill.py --check

# 强制重新安装
python scripts/init_skill.py --install --verbose
```

### analyze_log.py — 单文件分析

**用途**: 分析单个日志文件

**参数**:

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--file` / `-f` | 是 | - | 日志文件路径 |
| `--output` / `-o` | 否 | markdown | 输出格式：markdown/word/pdf |
| `--chunk-size` / `-c` | 否 | 配置文件值 | 分块大小（行数） |
| `--use-llm` / `-l` | 否 | false | 启用 LLM 深度分析 |
| `--config` | 否 | config.yaml | 配置文件路径 |
| `--verbose` / `-v` | 否 | false | 显示详细日志 |

**示例**:

```bash
# 基础分析（生成 Markdown 报告）
python scripts/analyze_log.py --file /var/log/app.log

# 生成 Word 报告
python scripts/analyze_log.py --file /var/log/app.log --output word

# 启用 LLM 分析
python scripts/analyze_log.py --file /var/log/app.log --use-llm

# 指定分块大小
python scripts/analyze_log.py --file /var/log/app.log --chunk-size 500000

# 显示详细日志
python scripts/analyze_log.py --file /var/log/app.log --verbose
```

**输出示例**:

```
============================================================
Log Analysis Skill - 单文件分析
============================================================
文件路径: /var/log/app.log
文件大小: 128.50 MB
输出格式: markdown
------------------------------------------------------------

[分析完成]
  文件: /var/log/app.log
  总行数: 1,500,000
  处理行数: 1,500,000
  总错误数: 1,234
  总警告数: 567
  分析报告: ./output/analysis_report_20260616_143052.md
  摘要: 共发现 1234 条错误记录，涉及 8 种错误类型

  错误级别分布:
    ERROR: 890
    FATAL: 344
    WARN: 567

  错误类型统计 (Top 10):
    NullPointerException: 456
    SQLException: 234
    TimeoutException: 189

报告已保存至: /path/to/output/analysis_report_20260616_143052.md
```

### batch_analyze.py — 批量分析

**用途**: 批量分析多个日志文件，生成综合报告

**参数**:

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--files` | 是 | - | 日志文件路径列表，逗号分隔 |
| `--output` / `-o` | 否 | markdown | 输出格式 |
| `--config` | 否 | config.yaml | 配置文件路径 |
| `--verbose` / `-v` | 否 | false | 显示详细日志 |

**示例**:

```bash
# 批量分析两个文件
python scripts/batch_analyze.py --files "/var/log/app1.log,/var/log/app2.log"

# 批量分析并生成 Word 报告
python scripts/batch_analyze.py --files "service1.log,service2.log" --output word

# 分析整个目录下的日志文件
python scripts/batch_analyze.py --files "$(ls /var/log/*.log | tr '\n' ',')"
```

## 分析报告格式

### Markdown 报告

默认输出格式，包含以下章节：

1. **处理概览**: 处理状态、进度、时间
2. **统计分析**: 错误级别分布、错误类型统计、高频错误
3. **关键错误分析**: 根因推断、证据链、置信度评估
4. **综合处置建议**: 运维建议、开发建议、预防措施

### Word 报告

可编辑的文档格式，适合团队共享和修改。

需要安装 `python-docx`:
```bash
pip install python-docx>=1.1.0
```

### PDF 报告

格式化排版的文档，适合存档和打印。

需要安装 `weasyprint` 和系统依赖:
```bash
pip install weasyprint>=59.0
```

注意：PDF 生成需要额外的系统字体和依赖，在部分环境中可能不可用。

## 配置说明

### 配置文件位置

默认配置文件: `config.yaml`

可通过 `--config` 参数指定其他配置文件:
```bash
python scripts/analyze_log.py --file app.log --config /path/to/custom_config.yaml
```

### 常用配置项

```yaml
# 日志解析配置
parser:
  chunk_size: 1000000        # 每块处理的日志行数
  enable_mmap: true          # 启用内存映射加速大文件读取

# 处理配置
processor:
  parallel_workers: 4        # 并行处理线程数
  use_llm: false             # 是否使用 LLM 分析
  enable_checkpoint: true    # 启用断点续传

# 报告配置
report:
  output_dir: ./output       # 报告输出目录
  enable_word: true          # 启用 Word 报告
  enable_pdf: false          # 启用 PDF 报告

# LLM 配置（仅在 use_llm 为 true 时生效）
llm:
  provider: openai
  api_key: "your-api-key"
  model: gpt-4o-mini
  temperature: 0.3
```

### 自定义错误模式

在 `config.yaml` 中添加自定义错误识别规则：

```yaml
error_patterns:
  - pattern_type: custom_error
    regex: "CustomException|custom.*error"
    description: Custom business error
    severity: medium
```

## 分析模式

### 规则模式（默认）

- **零成本**: 不调用外部 API
- **快速响应**: <1 秒/1000 条日志
- **预定义分类**: 8 种根因类型自动识别
- **适用场景**: 快速筛查、日常运维

### LLM 模式

- **深度分析**: 语义理解，根因推断
- **结构化报告**: 包含时间线、因果链、证据链
- **响应时间**: 5-30 秒/1000 条日志
- **适用场景**: 复杂故障诊断、深度分析

启用方式:
```bash
python scripts/analyze_log.py --file app.log --use-llm
```

## 性能优化

### 大文件处理

- 自动分块处理，内存占用稳定
- 支持断点续传，中断后可继续
- 可通过 `chunk_size` 调整分块大小

### 并行处理

- 多线程并行分析多个块
- 可通过 `parallel_workers` 调整线程数

### 内存控制

- 流式解析，不一次性加载整个文件
- 内存映射加速，减少 I/O 开销

## 常见问题

### Q: 分析大文件时内存不足？

**A**: 
1. 减小编码块大小：`--chunk-size 500000`
2. 减少并行线程数：在 `config.yaml` 中设置 `parallel_workers: 2`
3. 关闭内存映射：在 `config.yaml` 中设置 `enable_mmap: false`

### Q: LLM 分析失败？

**A**:
1. 检查 `config.yaml` 中的 API key 是否配置
2. 检查网络连接
3. 系统会自动降级到规则模式

### Q: 如何查看详细日志？

**A**: 使用 `--verbose` 参数：
```bash
python scripts/analyze_log.py --file app.log --verbose
```

### Q: 报告输出目录可以修改吗？

**A**: 可以，修改 `config.yaml` 中的 `report.output_dir`：
```yaml
report:
  output_dir: /path/to/your/output
```

### Q: 支持哪些日志格式？

**A**: 默认支持以下格式：
```
2026-06-14 10:30:45.123 [thread-01] ERROR a1b2c3d4 ClassName - Error message
```

支持自定义正则表达式匹配其他格式，在 `config.yaml` 中配置 `error_patterns`。

## 测试

运行测试：
```bash
python -m pytest test/test_log_analysis.py -v
```

---

> 更多信息请查看 [SKILL.md](../SKILL.md) 和 [config_guide.md](config_guide.md)
