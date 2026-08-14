# config 目录说明

本目录已不再存放 Apollo 连接配置（原 api_endpoints.json / auth.json 已移除）。

v3.0.0 起，Apollo 地址与 Token 由 EasyOps 第三方接口统一提供：
- `apollo_host_list`：`GET {api_base}/thirdApi/getApolloHostInfo`（Cookie: sessionId）
- 配置/应用查询：`GET {api_base}/thirdApi/apollo/apps`、`GET {api_base}/thirdApi/apollo/namespace`（EasyOps 代理，无需 Token）

目录保留是为了 Docker 构建（Dockerfile `COPY config/`）与挂载结构完整。
