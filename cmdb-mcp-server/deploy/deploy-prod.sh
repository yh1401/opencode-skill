#!/bin/bash
# cmdb-mcp-server 一键部署脚本（支持 Compose V1/V2）
# 用法: chmod +x deploy-prod.sh && ./deploy-prod.sh
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# 1. 检测 Docker 环境
if ! command -v docker >/dev/null 2>&1; then
    echo "错误: 未检测到 Docker，请先安装 Docker"
    echo "  CentOS/RHEL:  yum install -y docker-ce docker-ce-cli"
    echo "  Ubuntu/Debian: apt install -y docker.io"
    exit 1
fi
echo "[Docker] $(docker --version 2>/dev/null)"

# 2. 检测 Compose 版本 (V2 优先，V1 兜底)
if docker compose version >/dev/null 2>&1; then
    dc="docker compose"
    echo "[Compose V2] $(docker compose version)"
elif command -v docker-compose >/dev/null 2>&1; then
    dc="docker-compose"
    echo "[Compose V1] $(docker-compose --version)"
else
    echo "错误: 未检测到 Docker Compose，请先安装"
    echo "  CentOS/RHEL:  yum install -y docker-compose-plugin"
    echo "  Ubuntu/Debian: apt install -y docker-compose-v2"
    echo "  或手动安装:  curl -L https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64 -o /usr/local/bin/docker-compose && chmod +x /usr/local/bin/docker-compose"
    exit 1
fi

# 3. 配置 .env
if [ ! -f .env ]; then
    cat > .env << 'EOF'
MCP_USE_MOCK=false
MCP_API_BASE_URL=https://oss.tech.ctseelink.cn
LOG_LEVEL=INFO
EOF
    chmod 600 .env
    echo ".env 已创建"
else
    echo ".env 已存在，跳过"
fi

# 3. 停旧容器、构建并启动（禁用 BuildKit 避免 rpc EOF）
export DOCKER_BUILDKIT=0
$dc down 2>/dev/null || true
$dc build
$dc up -d

# 4. 等待启动并查看日志
sleep 3
echo "--- 启动日志 ---"
$dc logs --tail=30 cmdb-mcp
echo "----------------"

# 5. 健康检查
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 http://localhost:8061/health || echo "000")

if [ "$HEALTH" = "200" ]; then
    IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "localhost")
    echo ""
    echo "部署成功: http://${IP}:8061"
    echo "MCP 端点: http://${IP}:8061/mcp"
else
    echo "错误: 健康检查失败 (HTTP $HEALTH)"
    echo "查看日志: $dc logs cmdb-mcp"
    exit 1
fi
