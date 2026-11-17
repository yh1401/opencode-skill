---
name: "host-metrics-test"
description: "提供主机监控测试的常用话术快捷命令。包含实时监控测试、趋势查询测试、服务启动等快捷操作。输入 #测试主机监控 即可执行测试。"
---

# 主机监控测试技能

提供 Prometheus MCP Server 的主机监控测试快捷操作，连接到 VictoriaMetrics。

## 配置信息

### MCP Server 配置
```yaml
mcp:
  url: http://localhost:8080/mcp
  transport: http
  timeout: 30s
```

### VictoriaMetrics 数据源
```yaml
prometheus:
  url: http://14.29.115.34:48082
  backend: prometheus
  timeout: 1m
```

## 快捷命令

### 1. 启动 MCP 服务
```powershell
# 启动 HTTP 模式的 MCP 服务，连接到 VictoriaMetrics
.\prometheus-mcp-server.exe `
  --prometheus.url="http://14.29.115.34:48082" `
  --prometheus.backend="prometheus" `
  --mcp.transport="http" `
  --web.listen-address=":8080" `
  --log.level="info"
```

### 2. 一键测试实时监控工具
```powershell
# 测试实时指标查询
go test -v -run "TestHostMetricsRealtimeHandler" ./pkg/mcp/...
```

### 3. 一键测试趋势查询工具
```powershell
# 测试趋势指标查询
go test -v -run "TestHostMetricsTrendHandler" ./pkg/mcp/...
```

### 4. 完整测试套件
```powershell
# 运行所有主机监控相关测试
go test -v -run "TestHostMetrics" ./pkg/mcp/...
```

### 5. 查询指定节点指标
```powershell
# 查询指定IP的主机指标
.\query_real_metrics.ps1 -IP "192.168.7.66"

# 查询所有节点
.\query_real_metrics.ps1
```

### 6. 服务状态检查
```powershell
# 检查 MCP Server 是否运行
Invoke-WebRequest http://localhost:8080/metrics -UseBasicParsing | Select-Object StatusCode

# 检查 VictoriaMetrics 连接
Invoke-WebRequest http://14.29.115.34:48082/api/v1/query?query=up -UseBasicParsing | Select-Object StatusCode
```

### 7. 清理缓存目录数据
```powershell
# 清理测试缓存
Remove-Item -Recurse -Force .\testdata\* -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force .\*.test -ErrorAction SilentlyContinue
```

## 使用方法

1. **启动服务**：输入 `#测试主机监控` 或手动执行启动命令
2. **快速执行测试**：使用上述命令测试各项功能
3. **查询指标**：使用 `query_real_metrics.ps1` 查询实时数据

## 可用工具列表

| 工具名 | 功能 |
|--------|------|
| `prometheus_query` | 执行 PromQL 查询 |
| `prometheus_query_range` | 查询时间范围数据 |
| `prometheus_label_values` | 获取标签值 |
| `prometheus_targets` | 获取监控目标 |
| `prometheus_alerts` | 获取告警信息 |
| `host_metrics_realtime` | 查询主机实时性能指标 |
| `host_metrics_trend` | 查询主机历史趋势数据 |

## 测试结果验证

测试通过标准：
- ✅ MCP Server 启动成功，监听端口 8080
- ✅ 成功连接 VictoriaMetrics
- ✅ `TestHostMetricsRealtimeHandler` - 实时指标查询测试通过
- ✅ `TestHostMetricsTrendHandler` - 趋势指标查询测试通过
- ✅ 所有子测试用例通过

## Trae 连接配置

在 Trae 中配置 MCP 连接：

1. **打开 Trae 设置**
2. **添加 MCP 服务**
3. **配置地址**：`http://localhost:8080/mcp`
4. **选择传输类型**：HTTP
5. **保存配置**

配置完成后，AI 助手即可调用所有 Prometheus 工具查询主机监控数据。