# Elasticsearch MCP Server 工具参考文档

> 本文档整理自 `src/servers/elasticsearch/base_tools.rs`，列出该 MCP 服务对外暴露的全部工具及其入参配置。

## 一、工具总览

该 MCP 服务内置 **5 个工具**，均为只读（read\_only\_hint = true）：

| 工具名            | 功能                        | 必填参数                 | 可选参数     |
| -------------- | ------------------------- | -------------------- | -------- |
| `list_indices` | 列出 Elasticsearch 中匹配模式的索引 | `index_pattern`      | 无        |
| `get_mappings` | 获取指定索引的字段映射（Mapping）      | `index`              | 无        |
| `search`       | 使用 Query DSL 执行搜索         | `index`、`query_body` | `fields` |
| `esql`         | 执行 ES\|QL 查询              | `query`              | 无        |
| `get_shards`   | 获取全部或指定索引的分片信息            | 无                    | `index`  |

通用调用方式（JSON-RPC 2.0）：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "<工具名>",
    "arguments": {
      "...": "..."
    }
  }
}
```

***

## 二、工具详细说明

### 1. list\_indices —— 列出索引

对应 ES API：`GET /_cat/indices/<index_pattern>?h=index,status,docs.count&format=json`

| 参数              | 类型     | 必填  | 说明                             |
| --------------- | ------ | --- | ------------------------------ |
| `index_pattern` | string | ✅ 是 | 索引通配符模式，支持 `*`、`logs-*`、逗号分隔多个 |

调用示例：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "list_indices",
    "arguments": {
      "index_pattern": "*"
    }
  }
}
```

返回内容：索引名、状态（open/close）、文档数（`docs.count`）。

***

### 2. get\_mappings —— 获取索引映射

对应 ES API：`GET /<index>/_mapping`

| 参数      | 类型     | 必填  | 说明                        |
| ------- | ------ | --- | ------------------------- |
| `index` | string | ✅ 是 | 要获取映射的索引名称，支持通配符，取第一个结果返回 |

调用示例：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "get_mappings",
    "arguments": {
      "index": "my-index"
    }
  }
}
```

返回内容：该索引的字段映射 JSON（字段名、类型 `type`、映射设置）。

***

### 3. search —— Query DSL 搜索

对应 ES API：`POST /<index>/_search`（body 为 `query_body`）

| 参数           | 类型             | 必填  | 说明                                                                  |
| ------------ | -------------- | --- | ------------------------------------------------------------------- |
| `index`      | string         | ✅ 是 | 要搜索的索引名称，支持通配符                                                      |
| `query_body` | object         | ✅ 是 | 完整 Query DSL 对象，可包含 `query`、`size`、`from`、`sort`、`_source`、`aggs` 等 |
| `fields`     | array\<string> | ❌ 否 | 需要返回的字段列表。传入后会自动追加到 `query_body._source`（若已有 `_source` 数组则合并，否则新增）  |

调用示例：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "search",
    "arguments": {
      "index": "logs-2026.08",
      "query_body": {
        "query": { "match": { "level": "error" } },
        "size": 10,
        "sort": [{ "@timestamp": "desc" }]
      },
      "fields": ["message", "@timestamp"]
    }
  }
}
```

返回内容：

- 命中总数与当前返回条数（纯聚合查询时不返回统计）；
- 命中文档的 `_source` 列表；
- 若有 `aggs`，额外返回聚合结果。

***

### 4. esql —— ES|QL 查询

对应 ES API：`POST /_query`（body 为 `{ "query": "<query>" }`）

| <br />  | 参数     | 类型  | 必填                              | 说明                       |
| :------ | ------ | --- | ------------------------------- | ------------------------ |
| `query` | string | ✅ 是 | 完整 ES\|QL 查询语句，如 \`FROM logs-\* | WHERE level == "error"\` |

调用示例：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "esql",
    "arguments": {
      "query": "FROM my-index | WHERE status == \"error\" | LIMIT 100"
    }
  }
}
```

返回内容：结果统一转换为「数组对象」格式，每行以列名作为 key。

***

### 5. get\_shards —— 获取分片信息

对应 ES API：`GET /_cat/shards[/<index>]?h=index,shard,prirep,state,docs,store,node&format=json`

| 参数      | 类型     | 必填  | 说明                  |
| ------- | ------ | --- | ------------------- |
| `index` | string | ❌ 否 | 指定索引名；不传则返回集群全部分片信息 |

调用示例（全部）：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "get_shards",
    "arguments": {}
  }
}
```

调用示例（指定索引）：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "get_shards",
    "arguments": {
      "index": "my-index"
    }
  }
}
```

返回内容：索引、分片号、主副本（prirep）、状态、文档数、存储大小、所在节点。

***

## 三、预留配置（未启用）

配置文件（`elastic-mcp.json5` → `elasticsearch.tools.custom`）中预留了两种**自定义工具**结构，当前版本为 WIP，尚未实际注册为可调用工具：

| 类型 `type`         | 字段                                                    | 说明                                             |
| ----------------- | ----------------------------------------------------- | ---------------------------------------------- |
| `esql`            | `description`、`query`、`parameters`、`format`           | 固定 ES\|QL 查询模板；`format` 取值 `json`（默认）/ `value` |
| `search_template` | `description`、`parameters`、`template_id` 或 `template` | 引用已存储的搜索模板，或内联模板（`{{param}}` 占位符）              |

配置示例：

```json5
"tools": {
  "exclude": ["search"],              // 预留：排除内置工具（当前未生效）
  "custom": {
    "add-42": {
      "type": "esql",
      "description": "Adds 42 to the input value",
      "query": "row value = ?value | eval result = value + 42 | keep result",
      "format": "json",
      "parameters": {
        "value": { "title": "The value", "type": "number" }
      }
    }
  }
}
```

***

## 四、服务端配置（供参考）

工具依赖的服务端连接配置（`elastic-mcp.json5` / 环境变量）：

| 配置项                     | 环境变量                          | 说明                       |
| ----------------------- | ----------------------------- | ------------------------ |
| `url`                   | `ES_URL`                      | ES 集群地址，必填               |
| `api_key`               | `ES_API_KEY`                  | API Key（与用户名密码二选一）       |
| `username` / `password` | `ES_USERNAME` / `ES_PASSWORD` | Basic 认证凭据               |
| `ssl_skip_verify`       | `ES_SSL_SKIP_VERIFY`          | 是否跳过 SSL 证书校验，默认 `false` |

