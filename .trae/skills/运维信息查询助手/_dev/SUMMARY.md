# 开发目录概览（_dev/）

> 本目录存放所有开发、测试、打包相关产物。打包时自动排除，不参与线上发布。

---

## 目录结构

```
_dev/
├── SUMMARY.md                         # 本文档 - 开发目录概览
├── package.json                       # 项目元数据
├── package.sh                         # 打包脚本（自动递增版本号）
├── MOCK_DATA.md                       # Mock 数据文档
├── skill-agent-dify.zip               # 预构建的 Dify 平台包
└── mcp/                               # MCP Schema 定义
    ├── cmdb-mcp-api.md                # CMDB 服务器查询 - API 文档
    ├── cmdb-mcp-schema.json           # CMDB 服务器查询 - JSON Schema
    ├── product-mcp-api.md             # 产品信息查询 - API 文档（开发中）
    ├── product-mcp-schema.json        # 产品信息查询 - JSON Schema（开发中）
    ├── project-basis-mcp-api.md       # 工程项目基础信息 - API 文档（开发中）
    ├── project-basis-mcp-schema.json  # 工程项目基础信息 - JSON Schema（开发中）
    ├── project-deployment-mcp-api.md  # 项目部署记录 - API 文档（开发中）
    ├── project-deployment-mcp-schema.json # 项目部署记录 - JSON Schema（开发中）
    ├── server-public-ip-mcp-api.md    # 服务器公网IP - API 文档（开发中）
    └── server-public-ip-mcp-schema.json # 服务器公网IP - JSON Schema（开发中）
```

---

## 各文件说明

### package.json

项目元数据，含名称、版本、模块类型。当前版本 `5.0.0`。

### package.sh

打包脚本，用于生成 Dify/StarAgent 平台可上传的 `.zip` 包。

**用法：**
```bash
./_dev/package.sh              # 生产环境（默认）
./_dev/package.sh development  # 开发环境
./_dev/package.sh test         # 测试环境
```

**打包逻辑：**
1. 复制 `SKILL.md`、`registry/`、`skills/` 到临时构建目录
2. `_dev/` 目录自动排除
3. 创建 `.env` 环境配置文件
4. 验证 `registry/skills.json` 中各子技能的启用状态
5. 输出 zip 到 `release/` 目录，版本号自动递增

**环境对应的 API 地址：**

| 环境 | API 地址 |
|------|----------|
| production | `https://oss.tech.ctseelink.cn/api/v2/cmdbServer/getCmdbServerPageList` |
| development | `http://localhost:3000/api/v2/cmdbServer/getCmdbServerBaseMessageList` |
| test | `http://test-server:3001/api/v2/cmdbServer/getCmdbServerBaseMessageList` |

### MOCK_DATA.md

CMDB 服务器查询接口的 Mock 响应数据，含 4 条示例服务器记录，覆盖贵州、上海、新疆多个机房和物理机/虚拟机类型。用于开发和测试时参考接口字段格式。

### mcp/ 目录

每个子技能对应一对文件：`*-mcp-api.md`（接口文档）和 `*-mcp-schema.json`（JSON Schema 定义）。

**当前可用（cmdb-server-query）：**
- **API 端**：`POST /api/v2/cmdbServer/getCmdbServerPageList`
- **认证**：需登录获取 token
- **响应字段**：hostName、ip、publicIp、node、state、serverType、cpuCores、memory、os、environment、productName、projectName、assetNo、rack 等
- **注意**：MCP Schema (`cmdb-mcp-schema.json`) 中 `required: ["node"]` 标记了 `node` 为必填，但 `skills/cmdb-server-query/SKILL.md` 中已改为可选（`required: []`），后续需同步更新此 Schema
- **注意**：MCP Schema 中 `ip` 的描述仍为"内网IP（模糊匹配）"，未更新为"内网IP/公网IP"，需同步

**开发中（4 个）：**
- `server-public-ip-query` — 公网IP查询
- `project-deployment-query` — 部署记录查询
- `product-query` — 产品信息查询
- `project-basis-query` — 工程项目基础信息查询

---

## 已知待办

- [ ] 同步 `mcp/cmdb-mcp-schema.json`：`required` 改为 `[]`，`ip` 描述改为"内网IP/公网IP"
- [ ] MCP API 文档引用了 3 个不存在的 reference 文件（`cmdb-fields.md`、`cmdb-operators.md`、`cmdb-json-to-params.md`），需清理或创建
- [ ] MCP Schema (`cmdb-mcp-schema.json`) 和 MCP API 文档 (`cmdb-mcp-api.md`) 指向了不同的 API 端点（`getCmdbServerPageList` vs `getCmdbServerBaseMessageList`），需确认统一
- [ ] 其他 4 个子技能开发完成后，取消 registry 中的注释并打包发布
