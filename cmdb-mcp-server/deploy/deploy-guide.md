# cmdb-mcp-server 手动部署操作手册

> 覆盖从打包上传到启动验证的完整流程，纯手动命令操作。

## 环境要求

| 条件 | 要求 |
|------|------|
| Docker | 已安装并运行 |
| Docker Compose | V1（`docker-compose`）或 V2（`docker compose`）均可 |
| 端口 8061 | 未被占用 |
| CMDB API | 服务器可访问 `https://oss.tech.ctseelink.cn` |

## 一、检测 Compose 版本

```bash
# 先执行此检测，后续命令用 $dc 变量
if docker compose version >/dev/null 2>&1; then
    dc="docker compose"; echo "[V2] $($dc version)"
elif command -v docker-compose >/dev/null 2>&1; then
    dc="docker-compose"; echo "[V1] $(docker-compose --version)"
else
    echo "未安装 Docker Compose"; exit 1
fi
```

## 二、打包（开发机）

```bash
cd cmdb-mcp-server
./package.sh
# 生成 dist/cmdb-mcp-server-<版本>-<时间戳>.tar.gz
```

## 三、上传到服务器

```bash
scp dist/cmdb-mcp-server-*.tar.gz user@服务器IP:/opt/
```

## 四、解压

```bash
cd /opt
tar xzf cmdb-mcp-server-*.tar.gz
cd cmdb-mcp-server/deploy
```

## 五、配置环境变量

```bash
cp ../.env.example .env
# 按需修改: vi .env
```

## 六、构建并启动

```bash
$dc up -d --build
```

## 七、查看启动日志（必做）

```bash
$dc logs --tail=30 cmdb-mcp
```

成功标志：
```
✅ 加载 API 配置: base_url=https://..., endpoints=[...]
🚀 启动标准 MCP HTTP 服务器...
📍 监听地址: http://0.0.0.0:8061
Uvicorn running on http://0.0.0.0:8061
```

## 八、健康检查

```bash
curl -s http://localhost:8061/health
```

## 常用运维命令

```bash
cd deploy

# 查看状态
$dc ps

# 查看日志
$dc logs -f cmdb-mcp

# 重启
$dc restart

# 停止
$dc down

# 重新部署
$dc up -d --build

# 进入容器
$dc exec cmdb-mcp bash
```

## 常见问题

| 问题 | 解决方案 |
|------|----------|
| `docker compose` 报 unknown command | 服务器是 Compose V1，改用 `docker-compose` |
| `$dc ps` 无容器 | 确认在 `deploy/` 目录执行 |
| 容器 Started 但 curl 不通 | `$dc logs --tail=100 cmdb-mcp` 看启动错误 |
| 容器 Exited/立即退出 | `$dc logs cmdb-mcp` 查看 Python Traceback |
| 接口返回 503/504 | `$dc exec cmdb-mcp python -c "import requests; print(requests.get('https://oss.tech.ctseelink.cn', timeout=5).status_code)"` |
| 端口被占用 | 修改 docker-compose.yml 的 ports 映射 |
| 一直走 Mock | 确认 `.env` 中 `MCP_USE_MOCK=false` |
