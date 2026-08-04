#!/bin/bash
# cmdb-mcp-server 一键部署脚本（支持 Compose V1/V2）
# 用法: chmod +x deploy-prod.sh && ./deploy-prod.sh
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# 1. 检测 Compose 版本
if docker compose version >/dev/null 2>&1; then
    dc="docker compose"
    echo "[Compose V2] $(docker compose version)"
elif command -v docker-compose >/dev/null 2>&1; then
    dc="docker-compose"
    echo "[Compose V1] $(docker-compose --version)"
else
    echo "错误: 未检测到 Docker Compose"; exit 1
fi

command -v docker >/dev/null 2>&1 || { echo "错误: 未检测到 Docker"; exit 1; }

# 2. 配置 .env
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

# 3. 停旧容器、构建并启动
$dc down 2>/dev/null || true
$dc up -d --build

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
