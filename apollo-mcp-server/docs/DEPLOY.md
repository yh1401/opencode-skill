# Apollo MCP Server 部署指南

---

## 📋 环境要求

部署前请确认服务器满足以下条件：

| 条件 | 要求 | 验证命令 |
|------|------|---------|
| Docker | 已安装并运行 | `docker info` |
| Docker Compose | V2 版本 | `docker compose version` |
| 端口 8062 | 未被占用 | `ss -tlnp \| grep 8062` |
| 第三方接口（推荐） | 内网可达，用于获取各套 Apollo 地址/Token | `curl -m 10 -b "sessionId=e5e27a7d1805758400287ae86741f889" https://easyops.tech.ctseelink.cn/thirdApi/getApolloHostInfo` |
| 磁盘空间 | >1GB 可用 | `df -h /` |

> 如果第三方接口不可达，需先配置内网 DNS 或在 `/etc/hosts` 中添加解析；配置/应用查询均通过 EasyOps 代理接口转发，不直连 Apollo。

---

## 🚀 一键部署（推荐）

在服务器上依次执行：

```bash
# 1. 克隆或上传代码
cd /opt
git clone https://github.com/yh1401/opencode-skill.git
cd opencode-skill/apollo-mcp-server

# 2. 执行部署脚本（默认通过第三方接口获取 Apollo 地址/Token，无需手动配置）
cd deploy
chmod +x deploy-prod.sh
./deploy-prod.sh

# 3. 记录输出的服务地址，前往 StarAgent 注册
# 例如: http://180.101.21.13:8080/gateway/apollo/mcp
```

脚本会自动完成：环境检查 → 配置生成 → 镜像构建 → 服务启动 → 健康验证。

---

## ⚙️ 手动部署（备选）

如果不想用脚本，可按以下步骤操作：

### 步骤 1: 创建配置

```bash
cd /opt/opencode-skill/apollo-mcp-server/deploy

cat > .env << 'EOF'
MCP_USE_MOCK=false
LOG_LEVEL=INFO
EOF
chmod 600 .env
```

> 默认通过第三方「Apollo Host 信息查询接口」获取各套 Apollo 地址/Token，无需填写 Token。
> 如需指定默认 Apollo 套，追加 `APOLLO_HOST_NAME=天翼云眼贵州`。
> `.env` 必须放在 `deploy/` 目录下（与 `docker-compose.yml` 同级），docker compose 从这里读取。

### 步骤 2: 构建并启动

```bash
# 在 deploy/ 目录下执行
docker compose up -d
```

### 步骤 3: 验证

```bash
# 健康检查
curl http://localhost:8062/health

# 查看日志
docker compose logs -f
```

---

## 🔌 StarAgent 注册

1. 打开 **StarAgent 平台** → **MCP 管理** → **新建**
2. 填写：
   - **服务名称**: `apollo-config-query-mcp`
   - **服务地址**: `http://180.101.21.13:8080/gateway/apollo/mcp`
   - **传输协议**: `Streamable HTTP`
3. 点击 **测试连接** → **获取工具列表** → **保存**
4. 绑定 Skill: 将 `apollo-config-query` 技能关联到此 MCP 服务

---

## 📋 常用命令

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

## ⚠️ 常见问题

| 问题 | 解决方案 |
|------|----------|
| `Mock 模式: true` | 检查 `.env` 中 `MCP_USE_MOCK=false` |
| 代理接口提示"未记录的访问" | 在 `t_ops_middleware_third_party_access` 配置 `/thirdApi/apollo/apps`、`/thirdApi/apollo/namespace` 两条 GET 权限 |
| 第三方接口不可达 | 检查 `APOLLO_HOST_API_BASE` / `APOLLO_HOST_SESSION_ID` 及出口网络；代理调用不通时 MCP 会直接返回错误提示 |
| 端口被占用 | 修改 `docker-compose.yml` 中的端口映射 |

---

## 📝 环境变量说明

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MCP_USE_MOCK` | Mock 数据开关 | `false` |
| `APOLLO_ENV` | Apollo 环境（PRO/DEV/SIT/LOCAL） | 配置文件 `default_env` |
| `APOLLO_HOST_API_BASE` | 第三方「Apollo Host 信息查询接口」地址（推荐方式） | `https://easyops.tech.ctseelink.cn` |
| `APOLLO_HOST_SESSION_ID` | 第三方鉴权 sessionId（32 位，同时是 token 解密密钥） | 内置默认值 |
| `APOLLO_HOST_NAME` | 默认 Apollo 套的模糊过滤名称（不填取第一条） | - |
| `CONFIG_SERVICE_REFRESH_SEC` | 第三方接口配置刷新间隔（秒） | `60` |
| `LOG_LEVEL` | 日志级别 | `INFO` |