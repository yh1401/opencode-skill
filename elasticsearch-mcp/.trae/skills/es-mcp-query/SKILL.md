---
name: "es-mcp-query"
description: "通过 es-mcp 提供的 5 个只读工具（list_indices / get_mappings / search / esql / get_shards）查询 Elasticsearch 数据。当用户提出查看索引、查询字段、检索日志、统计分析、分片信息等需求时调用。"
---

# ES-MCP 查询技能

本技能指导助手如何利用 es-mcp 工具查询 Elasticsearch 中的数据，所有操作均为只读。

## 可用工具

| 工具 | 参数 | 用途 |
| --- | --- | --- |
| `list_indices` | `index_pattern`(必填，支持 `*` 通配符) | 列出匹配的索引及文档数 |
| `get_mappings` | `index`(必填，支持通配符) | 获取索引的字段映射（字段名、类型） |
| `search` | `index`(必填)、`query_body`(必填，Query DSL 对象)、`fields`(可选，返回字段列表) | 使用 Query DSL 检索数据 |
| `esql` | `query`(必填，完整 ES\|QL 语句) | 执行 ES\|QL 查询，结果按行返回 |
| `get_shards` | `index`(可选) | 获取分片信息，不传则返回全部 |

## 决策规则

1. 用户问"有哪些索引/库" → 调用 `list_indices`，索引模式优先 `*`。
2. 用户问"某索引的字段/结构" → 先 `list_indices` 确认索引名，再 `get_mappings`（两步串行，不可并行）。
3. 用户要"检索数据/查日志/统计" → 优先用 `esql`（语句简洁）；需要复杂条件或分页排序时用 `search`。若目标集群为 OpenSearch 1.x / ES 7.10.2，`esql` 不可用，回退到 `search`。
4. 用户问"分片/存储分布" → 调用 `get_shards`。
5. 索引名不确定时，先 `list_indices` 探索，不要凭空猜测索引名。
6. 默认串行执行：上一工具返回确认索引名/字段名后，再发起下一查询，避免在未知索引名时盲目并行。
7. 仅当满足以下全部条件才允许并行：索引名已确认、字段名已确认、各查询相互独立、每个查询均带 `size` 或 `LIMIT` 上限。否则串行。

## 调用示例

### 列出索引
```
list_indices(index_pattern="*")
```

### 查询最近 10 条 error 日志
```
search(
  index="logs-*",
  query_body={
    "query": {"match": {"level": "error"}},
    "size": 10,
    "sort": [{"@timestamp": "desc"}]
  },
  fields=["message", "@timestamp"]
)
```

### 精确/范围/组合查询（Query DSL）
- 精确匹配：`{"query": {"term": {"status": "failed"}}}`
- 范围查询：`{"query": {"range": {"@timestamp": {"gte": "now-1h"}}}}`
- 组合查询：`{"query": {"bool": {"must": [{"match": {"level": "error"}}], "filter": [{"range": {"@timestamp": {"gte": "now-1h"}}}]}}}`
- 分页排序：`{"query": {"match_all": {}}, "from": 0, "size": 50, "sort": [{"@timestamp": "desc"}]}`
- 聚合统计：`{"query": {"match_all": {}}, "aggs": {"by_status": {"terms": {"field": "status"}}}}`

### 统计各状态文档数（ES|QL）
```
esql(query="FROM logs-* | STATS count = COUNT(*) BY status | SORT count DESC | LIMIT 20")
```

### 分片信息
```
get_shards(index="logs-2026.08")
```

## 注意事项

- 所有工具为只读，禁止编造不存在的索引名或字段名。
- ES|QL 语句必须显式加 `LIMIT`（建议 `LIMIT 20`），`search` 必须显式 `size`（建议 ≤ 50），防止返回海量数据导致会话中断或超时。
- `search` 的 `query_body` 必须是完整 Query DSL 对象；OpenSearch 1.x / ES 7.10.2 的 `_search` 端点要求 POST + body，最小 body 为 `{"size":1}`。
- 时间过滤建议用 `range` + `gte`/`lte`，结合 `@timestamp` 字段。
- 统计类需求优先用 `aggs`（search）或 `STATS`（esql）。
- 返回结果为 JSON 数组/对象，需转述为易读的自然语言，不要直接堆砌原始 JSON。

## 并行调用规则（避免工具调用被中断）

- **默认串行**：`list_indices` → `get_mappings` → `search` 必须三步串行，前一步未返回前不发起下一步。
- **并行前置条件**：索引名、字段名已确认，且每个查询都带 `size` 或 `LIMIT` 上限。
- **单次并行上限**：同时调用不超过 2 个工具，避免 MCP 会话因并发响应积压而中断。
- **失败隔离**：单个工具返回 403/超时/空结果时，不重复重试同一调用，改为告知用户卡点（权限/索引不存在/集群不支持）并给出下一步建议。
- **超时控制**：每次调用预留 15 秒超时，若超过则停止后续并行计划，改为串行重试。
- **OpenSearch 兼容**：检测到 `esql` 返回 `not_supported` / `parse_exception` 时，立即停止 `esql` 路线，回退 `search` + `aggs`，不要重试 `esql`。

## 错误处理（`transport error: context canceled` 专项）

`transport error: context canceled` 表明底层 HTTP transport 被 Rust context 取消（上游断开 / 超时 / cancel scope 触发 / 连接池重置），**不是查询语法错误**，重试同一调用通常会以同样错误收尾。
 
### 触发后的处置流程

1. **立即停止后续并行计划**，不要在同一会话内继续发起其他工具调用，避免 cancel scope 连锁触发。
2. **不重试原查询**，改为降级：
   - `search` → 把 `size` 降到 ≤ 10、字段只留必要 `_source`/`fields`、加上 `@timestamp` 范围过滤（如 `gte: now-15m`）缩小扫描区间。
   - `list_indices(index_pattern="*")` → 改用具体前缀（如 `logs-*`），避免 cat 命中海量索引导致 transport 阻塞。
   - `esql` → 直接放弃，改走 `search + aggs`。
3. **降级后串行重试一次**，若仍返回 `context canceled`，判定为 ES 侧不可达或权限拒绝，停止重试并告知用户：
   - 可能原因：MCP 容器到 ES 网络抖动、ES 节点过载、`sre_mcp` 权限被回收、上游网关超时。
   - 建议排查方向：`docker logs <mcp_container>`、`curl -s -o /dev/null -w '%{http_code}' https://<ES_URL>/_cluster/health`、ES 管理员复核 `sre_mcp` 角色。
4. **会话重建**：若两次降级均失败，重新执行 `initialize` 获取新 `mcp-session-id`，再从 `list_indices` 串行开始，禁止沿用旧会话继续调用。

### 预防约束

- 任何 `search` 必须同时具备 `size` 上限与时间范围过滤，禁止裸 `match_all`。
- `list_indices` 优先用具体前缀，仅在用户明确要"所有索引"时才用 `*`。
- 单次会话内连续出现 2 次 `context canceled` 即判定为不可恢复，停止本轮查询，向用户报告卡点。
