# 项目精简计划

## 一、项目结构分析

通过对项目目录结构的分析，识别出以下可删除的文件和目录：

### 可删除的文件/目录

| 类别 | 路径 | 说明 | 建议 |
|------|------|------|------|
| CI/CD配置 | `.github/` | GitHub Actions 工作流 | 删除 |
| IDE配置 | `.trae/` | Trae 技能配置 | **保留** |
| Helm Chart | `charts/` | Kubernetes Helm 部署配置 | 删除 |
| 示例文件 | `examples/` | 示例部署配置 | 删除 |
| Grafana配置 | `grafana/` | Grafana 仪表板 | **保留** |
| 文档图片 | `images/` | README 使用的图片 | 保留 |
| 打包配置 | `packaging/` | systemd 服务配置 | 删除 |
| 工具脚本 | `utils/` | count_tokens.py | 删除 |
| 测试脚本 | `*.ps1` | PowerShell 测试脚本 | **保留** |
| 备份文件 | `prometheus-mcp-server.exe~` | 可执行文件备份 | 删除 |
| 示例配置 | `prometheus.yml` | Prometheus 示例配置 | 删除 |

### 需要保留的核心文件

| 类别 | 路径 | 说明 |
|------|------|------|
| 主程序 | `cmd/prometheus-mcp-server/main.go` | 程序入口 |
| 核心包 | `pkg/mcp/` | MCP 服务器核心实现 |
| 核心包 | `pkg/prometheus/` | Prometheus API 客户端 |
| 内部模块 | `internal/` | 版本和指标管理 |
| Go配置 | `go.mod`, `go.sum` | 依赖管理 |
| 构建脚本 | `Makefile` | 构建命令 |
| 文档 | `README.md`, `README_zh.md` | 项目说明 |
| 许可证 | `LICENSE` | 开源许可证 |
| 可执行文件 | `prometheus-mcp-server.exe` | 编译后的二进制 |

## 二、删除步骤

### 步骤1：删除目录

```powershell
Remove-Item -Recurse -Force ".github"
Remove-Item -Recurse -Force ".trae"
Remove-Item -Recurse -Force "charts"
Remove-Item -Recurse -Force "examples"
Remove-Item -Recurse -Force "grafana"
Remove-Item -Recurse -Force "packaging"
Remove-Item -Recurse -Force "utils"
```

### 步骤2：删除测试脚本

```powershell
Remove-Item -Force "test_real_metrics.ps1"
Remove-Item -Force "test_vm.ps1"
Remove-Item -Force "scan_ports.ps1"
Remove-Item -Force "start_mcp_server.ps1"
Remove-Item -Force "query_real_metrics.ps1"
Remove-Item -Force "check_node_labels.ps1"
```

### 步骤3：删除其他不需要的文件

```powershell
Remove-Item -Force "prometheus-mcp-server.exe~"
Remove-Item -Force "prometheus.yml"
Remove-Item -Force ".envrc"
Remove-Item -Force ".golangci.yml"
Remove-Item -Force ".goreleaser.yaml"
Remove-Item -Force "AGENTS.md"
Remove-Item -Force "CLAUDE.md"
Remove-Item -Force "Dockerfile"
Remove-Item -Force "ct.yaml"
Remove-Item -Force "devbox.json"
Remove-Item -Force "devbox.lock"
```

## 三、精简后项目结构

```
prometheus-mcp-server-main/
├── cmd/
│   └── prometheus-mcp-server/
│       ├── external/docs/
│       │   └── README.md
│       └── main.go
├── images/
│   ├── claude_high_cardinality.gif
│   ├── demo-usage-with-prometheus-demo-server.gif
│   ├── gemini_docs_search.gif
│   ├── gemini_slo.gif
│   ├── logo-small.png
│   └── logo.png
├── internal/
│   ├── metrics/
│   │   └── metrics.go
│   └── version/
│       └── version.go
├── pkg/
│   ├── mcp/
│   │   ├── assets/
│   │   │   └── instructions.md
│   │   ├── mcptest/
│   │   │   └── harness.go
│   │   ├── api_mock_test.go
│   │   ├── docs.go
│   │   ├── docs_updater.go
│   │   ├── docs_updater_test.go
│   │   ├── errors.go
│   │   ├── errors_test.go
│   │   ├── handlers.go
│   │   ├── handlers_test.go
│   │   ├── logging.go
│   │   ├── logging_test.go
│   │   ├── middleware.go
│   │   ├── middleware_test.go
│   │   ├── registration.go
│   │   ├── registration_test.go
│   │   ├── resources.go
│   │   ├── server.go
│   │   ├── tools.go
│   │   └── types.go
│   └── prometheus/
│       ├── api.go
│       ├── utils.go
│       └── utils_test.go
├── .gitignore
├── .gitmodules
├── LICENSE
├── Makefile
├── README.md
├── README_zh.md
├── assets
├── go.mod
├── go.sum
└── prometheus-mcp-server.exe
```

## 四、风险评估

| 风险 | 影响 | 对策 |
|------|------|------|
| 删除测试脚本 | 无法使用PowerShell测试 | 可重新生成 |
| 删除CI配置 | 无法自动构建部署 | 如需CI可重新配置 |
| 删除Helm Chart | 无法K8s部署 | 如需K8s可重新添加 |

## 五、注意事项

1. 如果需要恢复删除的文件，可以从Git历史中恢复
2. 删除前建议备份重要文件
3. 删除后建议运行 `make build` 验证项目仍可正常编译