# cmdb-mcp-server 部署指南

## 环境要求

| 条件 | 要求 |
|------|------|
| Docker | 已安装并运行 |
| Docker Compose | V2（`docker compose`）或 V1（`docker-compose`）均可 |
| 端口 8061 | 未被占用 |
| CMDB API | 服务器可访问 `https://oss.tech.ctseelink.cn` |

> **未安装 Docker Compose 时**（`docker-compose: command not found`）：
> - CentOS/RHEL: `yum install -y docker-compose-plugin`
> - Ubuntu/Debian: `apt install -y docker-compose-v2`
> - 或手动安装: `curl -L https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64 -o /usr/local/bin/docker-compose && chmod +x /usr/local/bin/docker-compose`

## 一键部署（推荐）

```bash
# 1. 上传部署包并解压
cd /opt
tar xzf cmdb-mcp-server-*.tar.gz
cd cmdb-mcp-server/deploy

# 2. 配置环境变量
cp ../.env.example .env
# 按需编辑 .env

# 3. 执行部署脚本（自动适配 Compose V1/V2）
chmod +x deploy-prod.sh
./deploy-prod.sh
```

## 手动部署

### 1. 检测 Compose 版本

```bash
cd /opt/cmdb-mcp-server/deploy

if docker compose version >/dev/null 2>&1; then
    dc="docker compose"
    echo "[V2] $($dc version)"
elif command -v docker-compose >/dev/null 2>&1; then
    dc="docker-compose"
    echo "[V1] $(docker-compose --version)"
else
    echo "未安装 Docker Compose"; exit 1
fi
```

### 2. 配置环境变量

```bash
cp ../.env.example .env
# 按需修改: vi .env
```

### 3. 构建并启动

```bash
$dc up -d --build
```

### 4. 查看启动日志

```bash
$dc logs --tail=30 cmdb-mcp
```

成功标志日志：
```
✅ 加载 API 配置: base_url=https://..., endpoints=[...]
🚀 启动标准 MCP HTTP 服务器...
📍 监听地址: http://0.0.0.0:8061
Uvicorn running on http://0.0.0.0:8061
```

### 5. 健康检查

```bash
curl -s http://localhost:8061/health
```

## 常用命令

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

# 进入容器排查
$dc exec cmdb-mcp bash
```

## 常见问题

| 问题 | 解决方案 |
|------|----------|
| `docker compose` 报 unknown command | 服务器用的是 Compose V1，改用 `docker-compose` |
| 容器 Started 但 curl 不通 | `$dc logs --tail=100 cmdb-mcp` 查看启动错误 |
| 容器 Exited/立即退出 | `$dc logs cmdb-mcp` 查看 Python Traceback |
| `$dc ps` 无容器 | 确认在 `deploy/` 目录执行 |
| 接口返回 503/504 | `$dc exec cmdb-mcp python -c "import requests; print(requests.get('https://oss.tech.ctseelink.cn', timeout=5).status_code)"` |
| 端口被占用 | 修改 docker-compose.yml 的 ports 映射 |
| 一直走 Mock | 确认 `.env` 中 `MCP_USE_MOCK=false` |
