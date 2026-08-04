#!/bin/bash
# ================================================
# cmdb-mcp-server - 生产/测试环境一键部署脚本
# 使用方法: chmod +x deploy-prod.sh && ./deploy-prod.sh
# ================================================
set -e

# 切换到脚本所在目录，保证 .env 和 docker-compose.yml 定位正确
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║       cmdb-mcp-server 一键部署脚本             ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

# 1. 前置检查
echo -e "${YELLOW}[1/5] 环境检查${NC}"
command -v docker >/dev/null 2>&1 || { echo -e "${RED}❌ 请先安装 Docker${NC}"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo -e "${RED}❌ 请先安装 Docker Compose${NC}"; exit 1; }
echo -e "✅ Docker: $(docker --version)"
echo -e "✅ Docker Compose: $(docker compose version)"

# 2. 配置 .env (如果不存在则创建默认配置)
echo -e "\n${YELLOW}[2/5] 配置环境变量${NC}"
if [ ! -f .env ]; then
    cat > .env << 'EOF'
MCP_USE_MOCK=false
MCP_API_BASE_URL=https://oss.tech.ctseelink.cn
LOG_LEVEL=INFO
EOF
    chmod 600 .env
    echo -e "✅ .env 文件已创建（默认关闭 Mock，使用生产 API）"
else
    echo -e "✅ .env 文件已存在，跳过配置"
fi

# 3. 构建与启动
echo -e "\n${YELLOW}[3/5] 构建并启动服务${NC}"
docker compose build -q
docker compose up -d

# 4. 等待启动
echo -e "\n${YELLOW}[4/5] 等待服务启动${NC}"
sleep 3

# 5. 健康检查
echo -e "\n${YELLOW}[5/5] 健康检查${NC}"
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 http://localhost:8061/health || echo "000")

if [ "$HEALTH" = "200" ]; then
    IP=$(hostname -I | awk '{print $1}')
    echo ""
    echo -e "${GREEN}╔══════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║             🎉 部署成功！                       ║${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  服务地址: ${CYAN}http://${IP}:8061${NC}"
    echo -e "  MCP 端点: ${CYAN}http://${IP}:8061/mcp${NC}"
    echo -e "  健康检查: ${CYAN}http://localhost:8061/health${NC}"
    echo ""
    echo -e "${YELLOW}下一步:${NC}"
    echo "  1. 查看日志: docker compose logs -f"
    echo "  2. 注册 MCP: 在 StarAgent 平台填写服务地址"
    echo "  3. 绑定 Skill: 将 ops-data-query 技能绑定到此 MCP"
else
    echo -e "${RED}❌ 健康检查失败 (HTTP $HEALTH)${NC}"
    echo "查看日志排查:"
    echo "  docker compose logs cmdb-mcp"
    exit 1
fi
