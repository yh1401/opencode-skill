# 部署

```bash
# 1. 解压
tar -xzf ops-data-query-mcp.tar.gz
cd ops-data-query-mcp

# 2. 启动（自动安装依赖）
./start.sh -d
```

## 验证

```bash
curl http://localhost:8061/health
```