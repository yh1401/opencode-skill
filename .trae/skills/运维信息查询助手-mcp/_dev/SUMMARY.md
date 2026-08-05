# 开发目录概览（_dev/）- MCP 版运维信息查询助手

> 本目录存放所有开发、测试、打包相关产物。打包时自动排除，不参与线上发布。

---

## 目录结构

```
_dev/
├── SUMMARY.md                         # 本文档 - 开发目录概览
├── package.json                       # 项目元数据
├── package.sh                         # 打包脚本（自动递增版本号）
└── mcp/
    └── cmdb-mcp-schema.json           # cmdb_server_query 工具 Schema（与 MCP 服务对齐）
```

---

## 说明

### 与「查询运维信息助手」原版的区别

| 维度 | 原版（查询运维信息助手） | MCP 版（本技能） |
|------|--------------------------|------------------|
| 数据获取 | 技能直接调用 CMDB 业务 API | 通过 MCP 工具 `cmdb_server_query`（平台路由到 cmdb-mcp-server） |
| 依赖 | 无 | 需在平台注册 MCP 服务 cmdb-mcp-server |
| 连接/鉴权 | 技能文档内配置 | MCP 服务统一管理 |
| 异常降级 | 技能自身处理 | MCP 服务处理（超时/网络异常降级 Mock） |

### MCP 服务部署

MCP 服务代码位于 `cmdb-mcp-server/`（本仓库根目录），部署步骤：
1. `cd cmdb-mcp-server && ./package.sh` 打包
2. 上传服务器解压，`cd deploy && ./deploy-prod.sh` 启动
3. 健康检查：`curl http://<MCP地址>:8061/health` 返回 200

### 平台注册

在 StarAgent 平台注册 MCP 服务：
- 服务名：`cmdb-mcp-server`
- 地址：`http://<MCP地址>:8061/mcp`
- 验证：`tools/list` 应返回 `cmdb_server_query` 工具

---

## 已知待办

- [ ] 其余 4 个子技能（server-public-ip-query / project-deployment-query / product-query / project-basis-query）对应的 MCP 工具已在 cmdb-mcp-server 实现，后续启用时补充子技能文档
- [ ] `_dev/mcp/cmdb-mcp-schema.json` 与 MCP 服务 `tools/list` 保持同步（当前以服务端实际返回为准）
