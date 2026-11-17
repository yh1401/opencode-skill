# 工具数据格式说明

## 1. QueryHandler（瞬时查询工具）

### 输入参数
```json
{
  "query": "node_cpu_seconds_total{mode=\"idle\",instance=\"192.168.7.66:9100\"}",
  "timestamp": "2026-06-23T10:00:00Z",
  "truncation_limit": 1000
}
```

### 输出格式
```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {
          "instance": "192.168.7.66:9100",
          "mode": "idle"
        },
        "value": [1782115200.0, "1234.56"]
      }
    ]
  }
}
```

---

## 2. HostMetricsTrendHandler（趋势查询工具）

### 输入参数
```json
{
  "node": "",
  "ip": "192.168.7.66",
  "start_time": "2026-06-23T09:00:00Z",
  "end_time": "2026-06-23T10:00:00Z",
  "step": "5m"
}
```

### 输出格式（JSON 对象，包含 12 个指标）
```json
{
  "cpu_cores": "{\"result\":\"...\",\"warnings\":null}",
  "cpu_cores_latest": 32,
  "cpu_usage_percent": "{\"result\":\"...\",\"warnings\":null}",
  "cpu_usage_percent_latest": 27.5,
  "memory_total_bytes": "{\"result\":\"...\",\"warnings\":null}",
  "memory_total_bytes_latest": 68719476736,
  "memory_usage_percent": "{\"result\":\"...\",\"warnings\":null}",
  "memory_usage_percent_latest": 24.9,
  "disk_total_bytes": "{\"result\":\"...\",\"warnings\":null}",
  "disk_total_bytes_latest": 1099511627776,
  "disk_usage_percent": "{\"result\":\"...\",\"warnings\":null}",
  "disk_usage_percent_latest": 45.2,
  "network_connections": "{\"result\":\"...\",\"warnings\":null}",
  "network_connections_latest": 15000,
  "network_traffic_bytes_per_second": "{\"result\":\"...\",\"warnings\":null}",
  "network_traffic_bytes_per_second_latest": 12582912,
  "disk_io_bytes_per_second": "{\"result\":\"...\",\"warnings\":null}",
  "disk_io_bytes_per_second_latest": 5242880,
  "load_1m": "{\"result\":\"...\",\"warnings\":null}",
  "load_1m_latest": 1.2,
  "load_5m": "{\"result\":\"...\",\"warnings\":null}",
  "load_5m_latest": 1.5,
  "load_15m": "{\"result\":\"...\",\"warnings\":null}",
  "load_15m_latest": 1.3,
  "tcp_listen_overflow": "{\"result\":\"...\",\"warnings\":null}",
  "tcp_listen_overflow_latest": 0,
  "total_tcp_connections": "{\"result\":\"...\",\"warnings\":null}",
  "total_tcp_connections_latest": 15500,
  "ip": "192.168.7.66",
  "start_time": "2026-06-23T09:00:00Z",
  "end_time": "2026-06-23T10:00:00Z",
  "step": "5m0s"
}
```

---

## 3. HostMetricsRealtimeHandler（实时查询工具）

### 输入参数
```json
{
  "node": "",
  "ip": "192.168.7.66"
}
```

### 输出格式（JSON 对象，包含 12 个指标）
```json
{
  "cpu_cores": 32,
  "cpu_usage_percent": 27.5,
  "memory_total_bytes": 68719476736,
  "memory_usage_percent": 24.9,
  "disk_total_bytes": 1099511627776,
  "disk_usage_percent": 45.2,
  "network_connections": 15000,
  "network_traffic_bytes_per_second": 12582912,
  "disk_io_bytes_per_second": 5242880,
  "load_1m": 1.2,
  "load_5m": 1.5,
  "load_15m": 1.3,
  "tcp_listen_overflow": 0,
  "total_tcp_connections": 15500,
  "ip": "192.168.7.66",
  "timestamp": "2026-06-23T10:00:00Z"
}
```

---

## 格式差异总结

| 特性 | QueryHandler | HostMetricsTrend | HostMetricsRealtime |
|------|-------------|------------------|---------------------|
| 返回类型 | JSON 字符串 | JSON 字符串 | JSON 字符串 |
| 数据结构 | Prometheus API 响应 | 自定义指标对象 | 自定义指标对象 |
| 指标数量 | 单个查询结果 | 12 个指标（含趋势+最新值） | 12 个指标（仅最新值） |
| 时间信息 | 单个时间点 | 时间范围 + 步长 | 单个时间戳 |
| 输出包装 | 直接返回 API 响应 | 封装为指标 map | 封装为指标 map |

---

## Dify 代码执行节点使用示例

```python
import json

def main(raw_data: list) -> dict:
    if not raw_data or not raw_data[0]:
        return {"result": "❌ 未获取到数据"}
    
    data = raw_data[0]
    
    # QueryHandler 数据解析
    if "status" in data and "data" in data:
        result_type = data.get("data", {}).get("resultType", "")
        result = data.get("data", {}).get("result", [])
        return {"result": f"查询结果类型: {result_type}, 数据点数量: {len(result)}"}
    
    # HostMetrics 数据解析
    cpu_usage = data.get("cpu_usage_percent_latest", data.get("cpu_usage_percent", "N/A"))
    memory_usage = data.get("memory_usage_percent_latest", data.get("memory_usage_percent", "N/A"))
    
    return {
        "result": f"""## 主机指标摘要
        - CPU 使用率: {cpu_usage}%
        - 内存使用率: {memory_usage}%"""
    }
}
```