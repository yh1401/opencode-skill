# Prometheus MCP Server

<br />

这是一个 [MCP (Model Context Protocol)](https://modelcontextprotocol.io/introduction) 服务器，用于允许 LLM 与运行中的 [Prometheus](https://prometheus.io/) 实例通过 API 进行交互，实现生成和执行 PromQL 查询、列出和分析指标等功能。

<br />

#### 让 Claude 调查慢查询

使用的提示词：

> querying my metrics is slow, can you help me figure out why?

<br />

#### 调查 MCP 服务器自身产生的指标并为 SLO 创建记录规则

使用的提示词：

> use the tools from the prometheus mcp server to investigate the metrics from
> the mcp server and suggest prometheus recording rules for SLOs

#### 总结 Prometheus 指标/标签命名最佳实践

使用的提示词：

> summarize prometheus metric/label name best practices

#### 报告 prometheus.demo.prometheus.io 实例的健康状态

使用的提示词：

> please provide a comprehensive review and summary of the prometheus server.
> review it's configuration, flags, runtime/build info, and anything else that
> you feel may provide insight into the status of the prometheus instance,
> including analyzing metrics and executing queries

<br />

# 工具说明

Prometheus HTTP API 输出 JSON 数据，MCP 服务器中的工具将这些 JSON 返回给 LLM 进行处理，因为 JSON 结构清晰、易于 LLM 理解。

#### LLM 与 Token/上下文效率

本 MCP 服务器支持以下可能有助于减少 token/上下文使用量的选项：

##### TOON 编码

如果对 token/上下文使用量有顾虑，本 MCP 服务器还支持将 API 的 JSON 数据转换为 [Token-Oriented Object Notation (TOON) 格式](https://github.com/toon-format/toon)。
虽然不能保证减少 token 使用量，但它专门为 token 效率而设计。
正如 TOON 文档中所述，它在统一的对象数组表现出色；非统一/复杂对象使用 JSON 可能更节省 token。
实际 token 使用量取决于使用模式，请查看常见工作流程以确定 TOON 输出是否有益。
请参阅[命令行参数](#命令行参数)了解可用参数及其对应的环境变量。

##### API 响应截断

此功能允许设置从 Prometheus API 返回的最大行数或条目数，有助于减少发送给 LLM 的数据量。
将限制设置为 `0` 可禁用截断。
默认情况下截断是禁用的。
请注意，具有处理工具请求参数能力的 LLM 可以按工具调用覆盖此全局截断限制。
请参阅[命令行参数](#命令行参数)了解可用参数及其对应的环境变量。

#### 完整工具列表

| 工具名称            | 描述                                                                        |
| ------------------- | --------------------------------------------------------------------------- |
| `alertmanagers`     | 获取 Prometheus Alertmanager 发现概览                                       |
| `build_info`        | 获取 Prometheus 构建信息                                                    |
| `config`            | 获取 Prometheus 配置                                                        |
| `docs_list`         | 官方 Prometheus 文档文件列表                                                |
| `docs_read`         | 读取 prometheus/docs 仓库中的官方 Prometheus 文档 markdown 文件             |
| `docs_search`       | 搜索 prometheus/docs 仓库中的官方 Prometheus 文档 markdown 文件             |
| `exemplar_query`    | 根据给定的查询和时间范围执行样本查询                                        |
| `flags`             | 获取运行时标志                                                              |
| `healthy`           | 可用于检查 Prometheus 健康状况的管理 API 端点                               |
| `label_names`       | 返回给定时间范围和匹配器按排序顺序存在的唯一标签名称                        |
| `label_values`      | 执行给定标签、时间范围和匹配器的值查询                                      |
| `list_alerts`       | 列出所有活动告警                                                            |
| `list_rules`        | 列出所有已加载的告警和记录规则                                              |
| `list_targets`      | 获取 Prometheus 目标发现概览                                                |
| `metric_metadata`   | 返回当前按指标名称抓取的指标的元数据                                        |
| `query`             | 对 Prometheus 数据源执行即时查询                                            |
| `quit`              | 可用于触发 Prometheus 优雅关闭的管理 API 端点                               |
| `range_query`       | 对 Prometheus 数据源执行范围查询                                            |
| `ready`             | 可用于检查 Prometheus 是否已准备好提供服务的（即可响应查询的）管理 API 端点 |
| `reload`            | 可用于触发 Prometheus 配置和规则文件重新加载的管理 API 端点                 |
| `runtime_info`      | 获取 Prometheus 运行时信息                                                  |
| `series`            | 通过标签匹配器查找序列                                                      |
| `targets_metadata`  | 返回当前按目标抓取的指标的元数据                                            |
| `tsdb_stats`        | 从 TSDB 获取使用情况和基数统计信息                                          |
| `wal_replay_status` | 获取当前 WAL 重放状态                                                       |

**注意：**

> 由于 [TSDB Admin API 端点](https://prometheus.io/docs/prometheus/latest/querying/api/#tsdb-admin-apis)
> 允许可能具有破坏性的操作（如删除数据），默认情况下它们是禁用的。
> 要启用 TSDB Admin API 端点，必须使用 `--dangerous.enable-tsdb-admin-tools` 参数启动 MCP 服务器，
> 以确认与这些端点相关的风险。

| 工具名称           | 描述                                                                                        |
| ------------------ | ------------------------------------------------------------------------------------------- |
| `clean_tombstones` | 从磁盘删除已删除的数据并清理现有的墓碑                                                      |
| `delete_series`    | 删除时间范围内所选序列的数据                                                                |
| `snapshot`         | 在 TSDB 数据目录下创建所有当前数据的快照到 `snapshots/<datetime>-<rand>` 并返回目录作为响应 |

#### easyviews 架构分析工具

easyviews 平台提供基础设施拓扑与部署关系数据（节点、模块、调用链路、宿主机与云主机关系等），用于架构优化分析。这些工具通过统一网关 `getTykjProvince` 转发调用，需要配置鉴权信息后才能使用；未配置时工具会返回明确的未配置错误。

| 工具名称                | 描述                                                                                                                                  |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `get_module_topology`   | 获取指定节点的业务模块拓扑，返回模块标识、中文名称、对象类型及关联的应用项目名称列表（projectName），用于建立节点、模块与应用归属关系 |
| `get_chain_topology`    | 获取指定节点与项目列表的应用调用链路拓扑，用于分析服务依赖、集中依赖、长调用链与异常依赖                                              |
| `get_node_list`         | 获取节点列表及其标识映射（`node`、`ctyun_node_code3`、节点 ID、省份信息），支持按节点名称前缀过滤                                     |
| `get_virtual_structure` | 获取指定节点下物理宿主机与云主机的关系，返回物理宿主机 IP 与其承载的云主机 IP 列表，用于识别故障域集中部署                            |
| `get_project_topology`  | 获取指定节点下的项目列表与云主机部署关系，用于分析多实例是否集中于同一宿主机                                                          |

**鉴权与配置参数：**

| 参数                     | 说明                                             | 默认值                                                                    |
| ------------------------ | ------------------------------------------------ | ------------------------------------------------------------------------- |
| `--easyviews.url`        | easyviews 统一网关地址（`getTykjProvince` 接口） | `https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince` |
| `--easyviews.province`   | 省份标识（`provinceDetail.province`）            | `大模型`                                                                  |
| `--easyviews.app-id`     | 应用 ID（`provinceDetail.appId`）                | 空（未配置时工具不可用）                                                  |
| `--easyviews.app-secret` | 应用密钥（`provinceDetail.appSecret`）           | 空（未配置时工具不可用）                                                  |
| `--easyviews.timeout`    | easyviews API 调用的超时时间                     | `30s`                                                                     |

示例：

```shell
prometheus-mcp-server --easyviews.app-id="<appId>" --easyviews.app-secret="<appSecret>"
```

#### 工具集

服务器暴露了许多与 Prometheus 交互的工具。除了通过 API 与 Prometheus 交互的工具外，还有其他工具用于读取文档等操作。
默认情况下，它们都会被注册并可用（TSDB Admin API 工具需要额外的参数）。

为了照顾具有较小上下文窗口的 LLM，可以传入一个白名单来指定要向服务器注册的具体工具。
以下"核心"工具始终被加载：`[docs_list, docs_read, docs_search, query, range_query, metric_metadata, label_names, label_values, series]`。
可以使用 [`--mcp.tools`](#命令行参数) [参数](#命令行参数)指定其他工具。

例如，命令行：

```shell
prometheus-mcp-server --mcp.tools=build_info --mcp.tools=flags --mcp.tools=runtime_info
```

将导致加载以下工具：

- `build_info`
- `docs_list`
- `docs_read`
- `docs_search`
- `flags`
- `label_names`
- `label_values`
- `metric_metadata`
- `query`
- `range_query`
- `runtime_info`
- `series`

#### Prometheus 兼容后端

有许多 Prometheus 兼容后端可用于以各种方式扩展 Prometheus，通常目标是提供长期存储或多个 Prometheus 实例的查询聚合。
一些示例可以在 Prometheus 文档的 [Remote Storage](https://prometheus.io/docs/operating/integrations/#remote-endpoints-and-storage) 中找到。

许多这些服务也提供"Prometheus 兼容"API，可用于使用原生 promQL 查询/交互数据。
一般来说，这个 MCP 服务器至少可以用于其他 Prometheus API 兼容服务执行查询和与序列/标签/元数据端点交互以进行指标和标签发现。
除此之外，由于不同系统实现其不同需求的不同部分/扩展 API，可能存在 API 差异。

示例：

- Thanos 不使用集中式配置，因此未实现配置端点，配置工具会返回 `404`。
- Mimir 和 Cortex 实现了额外的端点来管理/添加/删除规则

为了解决这个问题并在一些常用的 Prometheus 兼容系统上提供更好的体验，本项目可能添加对特定系统的直接支持，以提供不同/更多的工具。
可以使用 [`--prometheus.backend`](#命令行参数) [参数](#命令行参数)选择特定的 Prometheus 后端实现。
给定 MCP 服务器版本上可用后端实现的列表可以在 [`--help`](#命令行参数) [参数](#命令行参数)的输出中找到。
资格和支持标准仍在讨论中，请提交 issue 请求对特定后端的支持/功能以进一步讨论。

##### Prometheus 后端实现差异

| 后端                           | 工具                | 添加/删除/更改 | 备注                                                                            |
| ------------------------------ | ------------------- | -------------- | ------------------------------------------------------------------------------- |
| `prometheus`                   | n/a                 | 无             | 标准 Prometheus 工具。功能上等同于 `--mcp.tools="all"`。默认 MCP 服务器工具集。 |
| [`thanos`](https://thanos.io/) | `alertmanagers`     | 删除           | Thanos 未实现该端点，工具返回 `404`。                                           |
| [`thanos`](https://thanos.io/) | `config`            | 删除           | Thanos 不使用集中式配置，因此未实现该端点，工具返回 `404`。                     |
| [`thanos`](https://thanos.io/) | `wal_replay_status` | 删除           | Thanos 未实现该端点，工具返回 `404`。                                           |
| [`thanos`](https://thanos.io/) | `list_stores`       | 添加           | Thanos 提供了额外的端点来列出 store API 服务器。                                |
| [`thanos`](https://thanos.io/) | `reload`            | 删除           | Thanos 未实现该端点，工具返回 `404`。                                           |
| [`thanos`](https://thanos.io/) | `quit`              | 删除           | Thanos 未实现该端点，工具返回 `404`。                                           |

### 资源

| 资源名称                     | 资源 URI                    | 描述                               |
| ---------------------------- | --------------------------- | ---------------------------------- |
| 官方 Prometheus 文档文件列表 | `prometheus://docs`         | 官方 Prometheus 文档文件列表       |
| 阅读官方 Prometheus 文档     | `prometheus://docs/{+file}` | 按名称阅读官方 Prometheus 文档文件 |

## 项目架构

### 代码结构

```
prometheus-mcp-server/
├── cmd/
│   └── prometheus-mcp-server/
│       └── main.go                 # 主程序入口
├── pkg/
│   ├── mcp/                        # MCP 服务器核心代码
│   │   ├── server.go               # ServerContainer（依赖注入结构）、中间件链、HTTP + stdio 传输
│   │   ├── tools.go                # *mcp.Tool 工具定义及注解
│   │   ├── types.go                # 工具输入类型（带 jsonschema 标签和 slog.LogValuer 实现）
│   │   ├── handlers.go             # 工具处理方法
│   │   ├── registration.go         # 工具集组合、后端工具集（prometheus, thanos）、CoreTools 列表
│   │   ├── resources.go            # MCP 资源（指标列表、目标、文档）
│   │   ├── docs.go                 # Bleve 索引文档搜索
│   │   ├── docs_updater.go         # 可选的实时文档自动更新
│   │   ├── middleware.go           # 遥测中间件
│   │   ├── errors.go               # 优雅的 404 处理
│   │   ├── logging.go              # MCP 客户端日志
│   │   └── api_mock_test.go        # 测试用的 Mock Prometheus
│   └── prometheus/                 # Prometheus API 客户端构建器（自定义 User-Agent）
├── internal/
│   ├── metrics/                    # 指标注册表和命名空间
│   └── version/                    # 版本信息
├── charts/
│   └── prometheus-mcp-server/      # Helm 图表
├── examples/                       # 配置示例
└── grafana/                        # Grafana 仪表板
```

### 核心组件

#### ServerContainer (`pkg/mcp/server.go`)

依赖注入结构，包含：

- Prometheus API 客户端和 RoundTripper
- 日志记录器
- Bleve 文档索引（带 RWMutex 保护，支持运行时交换）
- 服务器配置

#### 工具处理流程

```
工具请求 → 中间件链（authContextMiddleware 等）→ Handler 方法 → Prometheus API 调用 → 响应
```

#### 后端工具集机制

- **Base Toolset**: Prometheus 工具集作为基础
- **Backend-specific**: 每个后端有 `<backend>RemovedTools` 列表和 `init<Backend>Toolset` 初始化器
- **添加新后端**: 扩展 `PrometheusBackends`，添加移除列表 + 初始化器，接入 `getToolset` 的 switch

### 设计模式

1. **依赖注入**: `ServerContainer` 作为中心依赖注入容器
2. **中间件链**: 请求通过一系列中间件处理（认证、遥测、错误处理）
3. **工具注册模式**: 使用 `toolRegistration` 结构分离工具定义和注册函数
4. **RWMutex 保护状态**: 文档索引状态使用读写锁保护

## 安装和使用

本 MCP 服务器在与其他工具集成或作为工具服务器安装时最为有用。
安装程序和集成支持因使用的工具而异。

### 二进制文件

从 [Releases](https://github.com/tjhop/prometheus-mcp-server/releases) 页面下载适合您系统的版本。

```shell
/path/to/prometheus-mcp-server <参数>

# 或使用环境变量
PROMETHEUS_MCP_SERVER_PROMETHEUS_URL="https://$yourPrometheus:9090" /path/to/prometheus-mcp-server
```

### Docker

```shell
# Stdio 传输
docker run --rm -i ghcr.io/tjhop/prometheus-mcp-server:latest --prometheus.url "https://$yourPrometheus:9090"

# 或使用环境变量
docker run --rm -i -e PROMETHEUS_MCP_SERVER_PROMETHEUS_URL="https://$yourPrometheus:9090" ghcr.io/tjhop/prometheus-mcp-server:latest
```

```shell
# 可流式传输的 HTTP 传输（也支持 SSE）
docker run --rm -p 8080:8080 ghcr.io/tjhop/prometheus-mcp-server:latest --prometheus.url "https://$yourPrometheus:9090" --mcp.transport "http" --web.listen-address ":8080"

# 或使用环境变量
docker run --rm -p 8080:8080 -e PROMETHEUS_MCP_SERVER_PROMETHEUS_URL="https://$yourPrometheus:9090" -e PROMETHEUS_MCP_SERVER_MCP_TRANSPORT="http" -e PROMETHEUS_MCP_SERVER_WEB_LISTEN_ADDRESS=":8080" ghcr.io/tjhop/prometheus-mcp-server:latest
```

### Helm 图表 (Kubernetes)

Helm 图表在每次发布时作为 OCI artifact 发布。

```bash
helm install prometheus-mcp-server oci://ghcr.io/tjhop/charts/prometheus-mcp-server \
  --version <版本> \
  --set prometheus.url=http://prometheus:9090
```

详情请参阅 [chart README](./charts/prometheus-mcp-server/README.md)。

### Kubernetes (原始清单)

如 prefer不使用 Helm 部署，可使用示例 Kubernetes 清单：

- [Kubernetes Deployment](./examples/k8s-deployment.yml)
- [OpenShift Deployment](./examples/openshift-deployment.yml)

### 系统包

从 [Releases](https://github.com/tjhop/prometheus-mcp-server/releases) 页面下载适合您系统的版本。系统包中包含了 Systemd service 文件。

```shell
# 安装系统包（假设基于 Debian）
apt install /path/to/package
# 创建单元覆盖，添加任何需要的参数或环境变量
systemctl edit prometheus-mcp-server.service
systemctl enable --now prometheus-mcp-server.service
```

## 安全与认证

### 连接到安全的 Prometheus 实例

MCP 服务器支持 [Prometheus HTTP 配置](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_config) 文件以连接到受保护的 Prometheus 实例。
示例配置可在[此处](./examples/http-config.yml)的示例文件夹中找到。
使用 `--http.config` 命令行参数提供 HTTP 配置文件。

### 保护 MCP 服务器端点

MCP 服务器支持 [Prometheus Web 配置文件](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md) 以在可选的基本认证和自定义 TLS 配置背后暴露其端点。
使用 `--web.config.file` 命令行参数提供 HTTP 配置文件。

## 遥测

### 指标

服务器运行后，会在配置好的监听地址和遥测路径（默认为 `:8080/metrics`）上暴露 Prometheus 指标。

<details>
<summary>Prometheus MCP Server 指标</summary>

| 指标名称                                      | 类型              | 描述                                                                         | 标签                                           |
| --------------------------------------------- | ----------------- | ---------------------------------------------------------------------------- | ---------------------------------------------- |
| `prom_mcp_build_info`                         | `Gauge`           | 带标签的常量 '1' 值，用于标识 prometheus-mcp-server 的版本、提交和构建日期。 | `version`, `commit`, `build_date`, `goversion` |
| `prom_mcp_server_ready`                       | `Gauge`           | 如果 MCP 服务器已就绪则为静态 '1'，否则为 '0'。                              | <br />                                         |
| `prom_mcp_api_calls_failed_total`             | `Counter`         | Prometheus API 失败总数，按端点分。                                          | `target_path`                                  |
| `prom_mcp_api_call_duration_seconds`          | `Histogram`       | Prometheus API 调用持续时间，按端点，单位秒。                                | `target_path`                                  |
| `prom_mcp_tool_calls_failed_total`            | `Counter`         | 每个工具的失败总数。                                                         | `tool_name`                                    |
| `prom_mcp_tool_call_duration_seconds`         | `Histogram`       | 工具调用持续时间，按工具，单位秒。                                           | `tool_name`                                    |
| `prom_mcp_resource_calls_failed_total`        | `Counter`         | 每个资源的失败总数。                                                         | `resource_uri`                                 |
| `prom_mcp_resource_call_duration_seconds`     | `Histogram`       | 资源调用持续时间，按资源，单位秒。                                           | `resource_uri`                                 |
| `prom_mcp_docs_last_update_timestamp_seconds` | `Gauge`           | 上次成功文档自动更新的 Unix 时间戳。                                         | <br />                                         |
| `prom_mcp_docs_update_failures_total`         | `Counter`         | 文档自动更新失败总数。                                                       | <br />                                         |
| `go_*`                                        | `Gauge`/`Counter` | 来自 `client_golang` 库的标准 Go 运行时指标。                                | <br />                                         |
| `process_*`                                   | `Gauge`/`Counter` | 来自 `client_golang` 库的标准进程指标。                                      | <br />                                         |

</details>

### Grafana 仪表板

[`grafana/`](./grafana/) 目录中包含了一个预构建的 [Grafana](https://grafana.com/) 仪表板，用于可视化 MCP 服务器暴露的指标。将仪表板 json 导入 Grafana 即可使用。

### 日志

本项目大量使用结构化分级日志。请参阅[命令行参数](#命令行参数)了解如何设置日志格式、级别和可选文件。

## 开发

### 使用 Devbox + Direnv 的开发环境

如果您使用 [Devbox](https://www.jetify.com/devbox) 和 [Direnv](https://direnv.net/)，则只需进入仓库目录即可设置所需的软件。

### 使用 Ollama 的本地 LLM

有关与工具一起使用的示例 MCP 配置，请参阅 [`mcp.json`](./examples/mcp.json)。
需要安装 [`ollama`](https://github.com/ollama/ollama)。

**注意：**

> 要覆盖默认 LLM (`ollama:gpt-oss:20b`)，请在运行 `make` 之前运行 `export OLLAMA_MODEL="ollama:your_model"`。

<details>
<summary>使用 MCP Inspector 和本地 ollama 实例：</summary>

需要安装 [MCP Inspector](https://github.com/modelcontextprotocol/inspector)：

```bash
make inspector
```

</details>

<details>
<summary>使用 mcphost 和本地 ollama 实例：</summary>

需要安装 [`mcphost`](https://github.com/mark3labs/mcphost)：

```bash
make mcphost
```

</details>

### Gemini 与 gemini-cli

有关使用 gemini-cli 运行 prometheus-mcp-server 的示例配置文件，请参阅 [`settings.json`](./examples/.gemini/settings.json)。
需要安装 [gemini-cli](https://github.com/google-gemini/gemini-cli)。

<details>
<summary>使用 `gemini-cli` 和托管模型：</summary>

```bash
make gemini
```

</details>

### 手动设置

所需软件：

- 工作的 Go 环境
- Docker（用于本地测试/linting/镜像构建等）
- GNU Make
- [ollama](https://github.com/ollama/ollama)
- [mcp inspector](https://github.com/modelcontextprotocol/inspector)
- [mcphost](https://github.com/mark3labs/mcphost)

### 构建

包含的 Makefile 有几个目标来辅助开发：

```bash
~/go/src/github.com/tjhop/prometheus-mcp-server (main [ ]) -> make

用法：
  make <目标>

目标：
  help                           打印此帮助信息
  submodules                     确保 git 子模块已初始化并更新
  tidy                           整理模块
  fmt                            应用 go 代码格式化
  lint                           运行 linter
  binary                         构建二进制文件
  build                          binary 的别名
  build-all                      用 goreleaser 测试发布过程，不发布/上传
  container                      用 goreleaser 构建容器镜像，build-all 的别名
  image                          container 的别名
  test                           运行测试
  mcphost                        使用 mcphost 针对本地 ollama 模型运行 prometheus-mcp-server
  inspector                      使用 inspector 以 STDIO 传输模式运行 prometheus-mcp-server
  inspector-http                 使用 inspector 以可流式传输的 HTTP 传输模式运行 prometheus-mcp-server
  open-webui                     使用 open-webui 运行 prometheus-mcp-server
  gemini                         使用 gemini-cli 针对 Google Gemini 模型运行 prometheus-mcp-server
```

## 命令行参数

可用命令行参数在帮助参数中有文档说明：

```bash
~/go/src/github.com/tjhop/prometheus-mcp-server (main [ ]) -> ./prometheus-mcp-server --help
用法: prometheus-mcp-server [<参数>]


参数：
  -h, --[no-]help                显示上下文相关帮助（也可尝试
                                 --help-long 和 --help-man）。
                                 ($PROMETHEUS_MCP_SERVER_HELP)
      --mcp.tools=all ...        要加载的 mcp 工具列表。目标
                                 `all` 可用于加载所有工具。
                                 目标 `core` 仅加载核心工具：
                                 docs_list,docs_read,docs_search,query,range_query,metric_metadata,label_names,label_values,series
                                 否则，将其视为要加载的工具的允许列表，
                                 以及核心工具。请参阅项目 README 获取更多信息
                                 和完整工具列表。
                                 ($PROMETHEUS_MCP_SERVER_MCP_TOOLS)
      --[no-]mcp.enable-toon-output
                                 启用 Token-Oriented Object Notation
                                 (TOON) 输出而不是 JSON
                                 ($PROMETHEUS_MCP_SERVER_MCP_ENABLE_TOON_OUTPUT)
      --[no-]mcp.enable-client-logging
                                 启用将日志消息作为协议通知发送到
                                 连接的 MCP 客户端。启用后，工具执行日志
                                 同时发送到服务器的主日志输出和 MCP 客户端，
                                 允许 LLM 观察服务器活动。
                                 ($PROMETHEUS_MCP_SERVER_MCP_ENABLE_CLIENT_LOGGING)
      --mcp.transport="stdio"    传输类型 [`stdio`, `http`]。
                                 ($PROMETHEUS_MCP_SERVER_MCP_TRANSPORT)
      --prometheus.backend=PROMETHEUS.BACKEND
                                 为特定 Prometheus API 兼容后端
                                 定制工具集。
                                 支持的后端包括：prometheus,thanos
                                 ($PROMETHEUS_MCP_SERVER_PROMETHEUS_BACKEND)
      --prometheus.url="http://127.0.0.1:9090"
                                 要连接的 Prometheus 实例的 URL
                                 ($PROMETHEUS_MCP_SERVER_PROMETHEUS_URL)
      --prometheus.timeout=1m    Prometheus 后端 API 调用的超时时间
                                 ($PROMETHEUS_MCP_SERVER_PROMETHEUS_TIMEOUT)
      --prometheus.truncation-limit=0
                                 如果启用，控制从 API 响应提供
                                 给 LLM 的最大查询响应大小（行数/条目数）。
                                 LLM 可以根据需要通过工具
                                 请求参数在支持的工具上覆盖截断限制。
                                 要禁用截断限制，请设置为 0。
                                 ($PROMETHEUS_MCP_SERVER_PROMETHEUS_TRUNCATION_LIMIT)
      --http.config=HTTP.CONFIG  用于设置
                                 Prometheus HTTP 客户端选项的配置文件路径
                                 ($PROMETHEUS_MCP_SERVER_HTTP_CONFIG)
      --web.telemetry-path="/metrics"
                                 暴露指标的路径。
                                 ($PROMETHEUS_MCP_SERVER_WEB_TELEMETRY_PATH)
      --web.max-requests=40      最大并行抓取请求数。设为 0 禁用。
                                 ($PROMETHEUS_MCP_SERVER_WEB_MAX_REQUESTS)
      --[no-]dangerous.enable-tsdb-admin-tools
                                 启用并允许使用访问 Prometheus' TSDB Admin API
                                 端点的工具（`snapshot`、`delete_series` 和
                                 `clean_tombstones` 工具）。这是危险的，
                                 允许破坏性操作如删除数据。
                                 如果您连接的 LLM 破坏了您的所有数据，
                                 本 MCP 服务器不承担责任。
                                 文档：https://prometheus.io/docs/prometheus/latest/querying/api/#tsdb-admin-apis
                                 ($PROMETHEUS_MCP_SERVER_DANGEROUS_ENABLE_TSDB_ADMIN_TOOLS)
      --mcp.keepalive-interval=30s
                                 向连接的 MCP 会话发送保持连接 ping 的间隔。
                                 如果对等方未能响应，会话将关闭。
                                 对于 HTTP 传输最有用，可以防止空闲连接断开。
                                 ($PROMETHEUS_MCP_SERVER_MCP_KEEPALIVE_INTERVAL)
      --mcp.session-timeout=10m  HTTP 传输 MCP 会话的空闲超时时间。
                                 ($PROMETHEUS_MCP_SERVER_MCP_SESSION_TIMEOUT)
      --[no-]mcp.stateless       启用无状态（stateless）HTTP 传输：
                                 不再下发或校验 Mcp-Session-Id。
                                 适用于无需服务端反向调用客户端的纯工具服务。
                                 ($PROMETHEUS_MCP_SERVER_MCP_STATELESS)
      --[no-]docs.auto-update    启用从官方 prometheus/docs
                                 仓库自动更新文档。每 24h0m0s 检查一次。
                                 ($PROMETHEUS_MCP_SERVER_DOCS_AUTO_UPDATE)
      --log.file=LOG.FILE        要记录的文件名（文件
                                 轮换策略应使用 logrotate 等外部工具配置）
                                 ($PROMETHEUS_MCP_SERVER_LOG_FILE)
      --[no-]web.systemd-socket  使用 systemd 套接字激活监听器
                                 而不是端口监听器（仅限 Linux）。
                                 ($PROMETHEUS_MCP_SERVER_WEB_SYSTEMD_SOCKET)
      --web.listen-address=:8080 ...
                                 暴露指标和 Web 接口的地址。
                                 可重复用于多个地址。示例：`:9100` 或 `[::1]:9100`
                                 用于 http，`vsock://:9100` 用于 vsock
                                 ($PROMETHEUS_MCP_SERVER_WEB_LISTEN_ADDRESS)
      --web.config.file=""       可启用 TLS 或认证的配置文件路径。见：
                                 https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md
                                 ($PROMETHEUS_MCP_SERVER_WEB_CONFIG_FILE)
      --log.level=info           仅记录给定严重程度或更高级别的消息。应为：[debug, info, warn, error]
                                 ($PROMETHEUS_MCP_SERVER_LOG_LEVEL)
      --log.format=logfmt        日志消息的输出格式。应为：[logfmt, json]
                                 ($PROMETHEUS_MCP_SERVER_LOG_FORMAT)
      --[no-]version             显示应用程序版本。
                                 ($PROMETHEUS_MCP_SERVER_VERSION)
```

## 无状态模式（取消 SessionID）

MCP 的 Streamable HTTP 传输默认启用会话：客户端初始化时，服务端生成随机 `Mcp-Session-Id` 并随响应下发，
客户端需在后续请求中回带该 ID，服务端据此保存会话状态，并在空闲超过 `--mcp.session-timeout`（默认 `10m`）后清理。

对于纯工具服务（无需服务端反向调用客户端），可通过 `--mcp.stateless` 取消会话管理：

```shell
prometheus-mcp-server --mcp.transport "http" --web.listen-address ":8090" --mcp.stateless
```

```shell
# 或使用环境变量
PROMETHEUS_MCP_SERVER_MCP_STATELESS="true" prometheus-mcp-server --mcp.transport "http"
```

启用后行为变化：

| 项目                                                 | 行为                                         |
| ---------------------------------------------------- | -------------------------------------------- |
| `Mcp-Session-Id`                                     | 不再下发，也不再校验；客户端无需管理会话     |
| 请求处理                                             | 每个 POST 请求创建临时会话，处理完成后即销毁 |
| `GET`（SSE 长连接）                                  | 返回 `405 Method Not Allowed`                |
| 服务端主动通知（如客户端日志）                       | 不可用                                       |
| `--mcp.session-timeout` / `--mcp.keepalive-interval` | 对 HTTP 会话不再生效                         |

## 添加新工具

1. **输入类型** (`types.go`): 带 `jsonschema` 标签的结构体，以及 `LogValue() slog.Value` 方法以便结构化日志分组字段。尽可能重用 `TimeRangeInput`、`TruncatableInput`。
2. **工具定义** (`tools.go`): 带 `Annotations.ReadOnlyHint` 或 `Annotations.DestructiveHint` 的 `*mcp.Tool`（使用 `ptr(true)` 辅助函数——不能取常量地址）。零输入工具使用 `InputSchema: emptyInputSchema`，而不是 SDK 的 `EmptyInput`。
3. **处理器方法** (`handlers.go`): `*ServerContainer` 上的方法，签名 `(ctx, req, input) (*mcp.CallToolResult, any, error)`。使用 `s.GetAPIClient(ctx)` 获取 prom Go 客户端和 `http.RoundTripper`。
4. **注册** (`registration.go`): 在 `initPrometheusToolset()` 中注册。如果 Thanos 不支持该工具，将其名称添加到 `thanosRemovedTools`。
5. **测试** (`handlers_test.go`): 使用 `api_mock_test.go` 中的 mock Prometheus。`registration_test.go` 验证工具集组合。
6. 更新 README.md 中的工具表。
