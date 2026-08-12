#!/bin/bash
# ================================================
# Apollo MCP Server - 生产/测试环境一键部署脚本
# 使用方法: chmod +x deploy-prod.sh && ./deploy-prod.sh
# 支持 Docker Compose V1(docker-compose) / V2(docker compose)
# ================================================
set -e

# 切换到脚本所在目录（deploy/），保证 .env 和 docker-compose.yml 定位正确
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# 1. 检测 Compose 命令（V1/V2 兼容）
if docker compose version >/dev/null 2>&1; then
    DC="docker compose"
    echo "[Compose V2] $(docker compose version | head -1)"
elif command -v docker-compose >/dev/null 2>&1; then
    DC="docker-compose"
    echo "[Compose V1] $(docker-compose --version)"
else
    echo "错误: 未检测到 Docker Compose"
    echo "安装方式: yum install -y docker-compose-plugin  (或 apt install -y docker-compose-v2)"
    exit 1
fi

# 2. 配置 .env (不存在则从 .env.example 生成)
if [ ! -f .env ]; then
    if [ -f ../.env.example ]; then
        cp ../.env.example .env
    else
        cat > .env << 'EOF'
MCP_USE_MOCK=false
APOLLO_ENV=PRO
# 推荐方式：第三方「Apollo Host 信息查询接口」（默认值已内置在代码中，无需配置）
# APOLLO_HOST_API_BASE=https://easyops.tech.ctseelink.cn
# APOLLO_HOST_SESSION_ID=e5e27a7d1805758400287ae86741f889
# 备用方式：直接指定 Apollo 地址与 Token
APOLLO_OPENAPI_HOST=http://apollo-config.tech.ctseelink.cn:8070
APOLLO_OPENAPI_TOKEN=
LOG_LEVEL=INFO
EOF
    fi
    chmod 600 .env
    echo "⚠️  .env 已生成（使用第三方接口获取 Apollo 地址/Token，默认无需填写）"
    echo "   如需覆盖第三方接口地址或 sessionId，请编辑 .env 后重新执行本脚本"
    echo "   vi .env"
    exit 0
fi

# 校验/自动修正：优先推荐使用第三方接口（默认值已内置在代码中，无需配置）
# 若 .env 中 Token 仍是占位符（从旧版 .env.example 复制导致），自动清空并继续部署
if grep -q "your_token_here\|your_openapi_token_here" .env 2>/dev/null; then
    sed -i 's/^\(APOLLO_OPENAPI_TOKEN=\).*/\1/' .env
    echo "⚠️  检测到 .env 中 APOLLO_OPENAPI_TOKEN 为占位符，已自动清空"
    echo "   将使用第三方「Apollo Host 信息查询接口」获取地址与 Token（默认无需配置）"
fi

# 校验：第三方接口 sessionId 与备用 Token 均未配置时给出提示（不再阻断，代码内置默认第三方接口）
ENV_SESSION_ID=$(grep -E '^\s*APOLLO_HOST_SESSION_ID=' .env 2>/dev/null | head -1 | cut -d'=' -f2- | tr -d '"' | tr -d "'" | xargs)
ENV_TOKEN=$(grep -E '^\s*APOLLO_OPENAPI_TOKEN=' .env 2>/dev/null | head -1 | cut -d'=' -f2- | tr -d '"' | tr -d "'" | xargs)
case "$ENV_TOKEN" in
    your_token_here|your_openapi_token_here) ENV_TOKEN="" ;;
esac
if [ -z "$ENV_SESSION_ID" ] && [ -z "$ENV_TOKEN" ]; then
    echo "ℹ️  未配置第三方接口 sessionId 与备用 Token，使用代码内置默认第三方接口（https://easyops.tech.ctseelink.cn）"
fi

# 3. 构建与启动（禁用 BuildKit 避免 rpc EOF；先停旧容器保证干净）
echo "[3/5] 构建并启动服务"
DOCKER_BUILDKIT=0 $DC down 2>/dev/null || true
DOCKER_BUILDKIT=0 $DC up -d --build

# 4. 等待启动并查看日志
echo "[4/5] 等待服务启动"
sleep 3
echo "--- 启动日志 ---"
$DC logs --tail=50 apollo-mcp || true

# 5. 健康检查
echo "[5/5] 健康检查"
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 http://localhost:8062/health || echo "000")

if [ "$HEALTH" = "200" ]; then
    IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    echo ""
    echo "✅ 部署成功！"
    echo ""
    echo "  服务地址: http://${IP:-<服务器IP>}:8062"
    echo "  MCP 端点: http://${IP:-<服务器IP>}:8062/mcp"
    echo "  健康检查: http://localhost:8062/health"
    echo ""
    echo "下一步:"
    echo "  1. 查看日志: $DC logs -f apollo-mcp"
    echo "  2. 注册 MCP: 在 StarAgent 平台填写服务地址"
    echo "  3. 绑定 Skill: 将 apollo-config-query 技能绑定到此 MCP"
else
    echo "❌ 健康检查失败 (HTTP $HEALTH)"
    echo "查看日志排查:"
    echo "  $DC logs --tail=100 apollo-mcp"
    echo "  或进入容器: $DC exec apollo-mcp sh"
    exit 1
fi
