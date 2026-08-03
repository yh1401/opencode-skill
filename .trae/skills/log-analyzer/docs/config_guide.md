# Log Analysis Skill 配置说明

> 版本: 1.0.0 | 更新日期: 2026-06-16

---

## 配置文件

默认配置文件路径: `config.yaml`

可以通过 `--config` 参数指定其他配置文件：
```bash
python scripts/analyze_log.py --file app.log --config /path/to/custom_config.yaml
```

## 完整配置示例

```yaml
# ============================================
# Log Analysis Skill 配置
# ============================================

# 服务配置
service:
  host: 0.0.0.0              # 服务监听地址（保留字段，脚本模式不生效）
  port: 8080                 # 服务端口（保留字段，脚本模式不生效）
  debug: false               # 调试模式：true 时输出详细日志

# 日志解析配置
parser:
  chunk_size: 1000000        # 每块处理的日志行数
  enable_mmap: true          # 启用内存映射加速大文件读取
  encoding: utf-8            # 文件编码格式
  error_handling: replace    # 编码错误处理策略

# 分块处理配置
processor:
  enable_checkpoint: true    # 启用断点续传功能
  parallel_workers: 4        # 并行处理线程数
  use_llm: false             # 是否使用 LLM 深度分析
  merge_threshold: 5         # 分块数少于此值时合并分析
  checkpoint_dir: ./checkpoints  # 断点续传数据目录

# 错误合并配置
error_merger:
  semantic_similarity_threshold: 0.75  # 语义相似度阈值
  max_examples_per_group: 3           # 每组最大示例数
  max_groups: 10                      # 最大分组数
  enable_semantic_merging: true       # 启用语义合并
  merge_by_error_type: true           # 按错误类型合并
  merge_by_message_pattern: true      # 按消息模式合并

# 报告生成配置
report:
  output_dir: ./output       # 报告输出目录
  enable_word: true          # 启用 Word 报告生成
  enable_pdf: false          # 启用 PDF 报告生成（需要 WeasyPrint）
  pdf_font: Noto Serif SC    # PDF 字体

# LLM 配置（当 use_llm: true 时启用）
llm:
  provider: openai           # LLM 提供商
  api_key: ""                # API 密钥（请替换为实际密钥）
  model: gpt-4o-mini         # 模型名称
  temperature: 0.3           # 生成温度
  max_tokens: 4096           # 最大 Token 数
  timeout: 60                # 请求超时时间（秒）

# 错误模式配置
error_patterns:
  - pattern_type: device_offline
    regex: "设备不在线|device.*offline|device.*not.*online"
    description: Device is offline
    severity: high

  - pattern_type: permission_denied
    regex: "无设备权限|permission.*denied|access.*denied|无权限"
    description: Permission denied
    severity: high

  - pattern_type: null_pointer
    regex: "NullPointerException|NPE|null.*pointer"
    description: Null pointer exception
    severity: critical

  - pattern_type: timeout
    regex: "timeout|超时|Connection.*timeout|SocketTimeout"
    description: Timeout error
    severity: medium

  - pattern_type: validation_error
    regex: "id不能为空|validation.*error|参数.*错误|必填"
    description: Validation error
    severity: medium

  - pattern_type: database_error
    regex: "SQLException|SQL.*error|数据库.*异常|Database.*error"
    description: Database error
    severity: high

  - pattern_type: network_error
    regex: "Network.*error|网络.*异常|ConnectException|Connection.*refused"
    description: Network error
    severity: medium

  - pattern_type: authentication_error
    regex: "authentication|认证.*失败|token.*invalid|登录.*失败"
    description: Authentication error
    severity: high

# 性能配置
performance:
  max_file_size_mb: 10240    # 最大支持文件大小（MB）
  max_concurrent_tasks: 10   # 最大并发任务数
  memory_limit_mb: 8192      # 内存限制（MB）
```

## 配置项详解

### service 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `debug` | bool | false | 调试模式。true 时输出详细日志信息 |

### parser 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `chunk_size` | int | 1000000 | 每块处理的日志行数。大文件会分成多个块处理 |
| `enable_mmap` | bool | true | 启用内存映射。加速大文件读取，减少内存占用 |
| `encoding` | string | utf-8 | 日志文件编码格式 |
| `error_handling` | string | replace | 编码错误处理策略。可选：replace/ignore/strict |

**chunk_size 建议值**:
- 小文件（< 100MB）: 1000000（默认）
- 中等文件（100MB - 1GB）: 500000
- 大文件（> 1GB）: 200000

### processor 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enable_checkpoint` | bool | true | 启用断点续传。中断后可继续处理 |
| `parallel_workers` | int | 4 | 并行处理线程数。建议不超过 CPU 核心数 |
| `use_llm` | bool | false | 是否使用 LLM 深度分析 |
| `merge_threshold` | int | 5 | 分块数少于此值时合并分析 |
| `checkpoint_dir` | string | ./checkpoints | 断点续传数据目录 |

**parallel_workers 建议值**:
- 4 核 CPU: 2-4
- 8 核 CPU: 4-6
- 16 核 CPU: 6-8

### error_merger 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `semantic_similarity_threshold` | float | 0.75 | 语义相似度阈值。高于此值的错误会被合并 |
| `max_examples_per_group` | int | 3 | 每组保留的最大示例数 |
| `max_groups` | int | 10 | 最大分组数 |
| `enable_semantic_merging` | bool | true | 启用基于语义相似度的合并 |
| `merge_by_error_type` | bool | true | 按错误类型合并 |
| `merge_by_message_pattern` | bool | true | 按消息模式合并 |

### report 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `output_dir` | string | ./output | 报告输出目录 |
| `enable_word` | bool | true | 启用 Word 报告生成 |
| `enable_pdf` | bool | false | 启用 PDF 报告生成 |
| `pdf_font` | string | Noto Serif SC | PDF 字体名称 |

**输出格式说明**:
- `markdown`: 默认格式，纯文本，易于阅读和编辑
- `word`: 需要安装 `python-docx`，可编辑的文档
- `pdf`: 需要安装 `weasyprint`，格式化排版文档

### llm 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `provider` | string | openai | LLM 提供商 |
| `api_key` | string | "" | API 密钥 |
| `model` | string | gpt-4o-mini | 模型名称 |
| `temperature` | float | 0.3 | 生成温度。越低越确定，越高越创意 |
| `max_tokens` | int | 4096 | 最大 Token 数 |
| `timeout` | int | 60 | 请求超时时间（秒） |

**支持的提供商**:
- `openai`: OpenAI API
- `anthropic`: Anthropic Claude API
- 其他兼容 OpenAI 格式的 API

### error_patterns 配置

自定义错误识别规则，支持正则表达式匹配：

```yaml
error_patterns:
  - pattern_type: <模式名称>      # 唯一标识符
    regex: "<正则表达式>"          # 匹配规则
    description: "<描述>"          # 错误描述
    severity: <严重程度>           # critical/high/medium/low
```

**严重程度说明**:
- `critical`: 致命错误，需要立即处理
- `high`: 高严重程度，需要尽快处理
- `medium`: 中等严重程度，需要计划处理
- `low`: 低严重程度，可延后处理

### performance 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `max_file_size_mb` | int | 10240 | 最大支持文件大小（MB） |
| `max_concurrent_tasks` | int | 10 | 最大并发任务数 |
| `memory_limit_mb` | int | 8192 | 内存限制（MB） |

## 环境变量配置

部分配置项也可以通过环境变量设置：

| 环境变量 | 对应配置 | 说明 |
|----------|----------|------|
| `LOG_ANALYZER_LLM_API_KEY` | `llm.api_key` | LLM API 密钥 |
| `LOG_ANALYZER_LLM_MODEL` | `llm.model` | LLM 模型名称 |
| `LOG_ANALYZER_OUTPUT_DIR` | `report.output_dir` | 报告输出目录 |
| `LOG_ANALYZER_DEBUG` | `service.debug` | 调试模式 |

环境变量优先级高于配置文件。

## 多环境配置

可以为不同环境创建不同的配置文件：

```bash
# 开发环境
python scripts/analyze_log.py --file app.log --config config.dev.yaml

# 生产环境
python scripts/analyze_log.py --file app.log --config config.prod.yaml

# 测试环境
python scripts/analyze_log.py --file app.log --config config.test.yaml
```

## 配置验证

运行 `init_skill.py` 时会自动验证配置：

```bash
python scripts/init_skill.py --check --verbose
```

配置验证规则：
- 服务端口必须在 1-65535 范围内
- 解析分块大小必须大于 0
- 并行工作线程数必须大于 0
- 内存限制必须大于 0

## 默认配置

如果配置文件不存在，系统会使用内置的默认配置：

```python
{
    "service": {
        "host": "0.0.0.0",
        "port": 8080,
        "debug": False
    },
    "parser": {
        "chunk_size": 1000000,
        "enable_mmap": True,
        "encoding": "utf-8"
    },
    "processor": {
        "enable_checkpoint": True,
        "parallel_workers": 4,
        "use_llm": False,
        "merge_threshold": 5,
        "checkpoint_dir": "./checkpoints"
    },
    "report": {
        "output_dir": "./output",
        "enable_word": True,
        "enable_pdf": False
    },
    "llm": {
        "provider": "openai",
        "api_key": "",
        "model": "gpt-4o-mini",
        "temperature": 0.3,
        "max_tokens": 4096,
        "timeout": 60
    },
    "error_patterns": [],
    "performance": {
        "max_file_size_mb": 10240,
        "max_concurrent_tasks": 10,
        "memory_limit_mb": 8192
    }
}
```
