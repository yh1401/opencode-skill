# cmdb-mcp-server 部署指南

---

## 环境要求

| 条件 | 要求 | 验证命令 |
|------|------|---------|
| Docker | 已安装并运行 | `docker info` |
| Docker Compose | V2 版本 | `docker compose version` |
| 端口 8061 | 未被占用 | `ss -tlnp \| grep 8061` |
| CMDB API | 内网/公网可达 | `curl -s -o /dev/null -w "%{http_code}" https://oss.tech.ctseelink.cn` |
| 磁盘空间 | >1GB 可用 | `df -h /` |

---

## 一键部署（推荐）

在服务器上依次执行：

```bash
# 1. 上传部署包并解压
cd /opt
mkdir -p cmdb-mcp-server && cd cmdb-mcp-server
tar xzf cmdb-mcp-server-*.tar.gz

# 2. 进入 deploy 目录，配置环境变量
cd deploy
cp .env.example .env
# 编辑 .env，按需修改 MCP_API_BASE_URL 和 MCP_USE_MOCK

# 3. 执行部署脚本
chmod +x deploy-prod.sh
./deploy-prod.sh
```

脚本会自动完成：环境检查 → 镜像构建 → 服务启动 → 健康验证。

---

## 手动部署（备选）

### 步骤 1: 创建配置

```bash
cd /opt/cmdb-mcp-server/deploy

cat > .env << 'EOF'
MCP_USE_MOCK=false
MCP_API_BASE_URL=https://oss.tech.ctseelink.cn
LOG_LEVEL=INFO
EOF
chmod 600 .env
```

> `.env` 必须放在 `deploy/` 目录下（与 `docker-compose.yml` 同级）。

### 步骤 2: 构建并启动

```bash
# 在 deploy/ 目录下执行
docker compose up -d
```

### 步骤 3: 验证

```bash
# 健康检查
curl http://localhost:8061/health

# 查看日志
docker compose logs -f
```

---

## StarAgent 注册

1. 打开 **StarAgent 平台** → **MCP 管理** → **新建**
2. 填写：
   - **服务名称**: `cmdb-mcp-server`
   - **服务地址**: `http://{服务器IP}:8061/mcp`
   - **传输协议**: `Streamable HTTP`
3. 点击 **测试连接** → **获取工具列表** → **保存**
4. 绑定 Skill: 将 `ops-data-query` 技能关联到此 MCP 服务

---

## 常用命令

> 以下命令均需在 `deploy/` 目录下执行

```bash
cd deploy

# 查看状态
docker compose ps

# 查看日志
docker compose logs -f

# 重启服务
docker compose restart

# 停止服务
docker compose down
```

---

## 常见问题

| 问题 | 解决方案 |
|------|----------|
| `Mock 模式: true` | 检查 `.env` 中 `MCP_USE_MOCK=false` |
| API 返回 503/504 | 检查服务器到 `MCP_API_BASE_URL` 的网络连通性 |
| 端口被占用 | 修改 `docker-compose.yml` 中的端口映射 |
| 健康检查失败 | `docker compose logs cmdb-mcp` 查看日志 |

---

## 环境变量说明

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MCP_USE_MOCK` | Mock 数据开关 | `false` |
| `MCP_API_BASE_URL` | CMDB API 基础地址 | `https://oss.tech.ctseelink.cn` |
| `LOG_LEVEL` | 日志级别 | `INFO` |
