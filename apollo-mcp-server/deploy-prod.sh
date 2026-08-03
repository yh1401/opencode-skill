#!/bin/bash
# ================================================
# Apollo MCP Server - 生产一键部署脚本
# 使用方法: chmod +x deploy-prod.sh && ./deploy-prod.sh
# ================================================
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║     Apollo MCP Server  生产一键部署脚本         ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

# 1. 前置检查
echo -e "${YELLOW}[1/5] 环境检查${NC}"
command -v docker >/dev/null 2>&1 || { echo -e "${RED}❌ 请先安装 Docker${NC}"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo -e "${RED}❌ 请先安装 Docker Compose${NC}"; exit 1; }
echo -e "✅ Docker: $(docker --version)"
echo -e "✅ Docker Compose: $(docker compose version)"

# 2. 配置 .env (如果不存在则交互式创建)
echo -e "\n${YELLOW}[2/5] 配置环境变量${NC}"
if [ ! -f .env ]; then
    echo "请输入 Apollo OpenAPI Token (格式: mcp-token-xxx):"
    read -r TOKEN
    if [ -z "$TOKEN" ]; then
        echo -e "${RED}❌ Token 不能为空${NC}"
        exit 1
    fi
    
    cat > .env << EOF
MCP_USE_MOCK=false
APOLLO_CONFIG_HOST=http://apollo-config.tech.ctseelink.cn:8080
APOLLO_OPENAPI_HOST=http://apollo-config.tech.ctseelink.cn:8070
APOLLO_OPENAPI_TOKEN=${TOKEN}
LOG_LEVEL=INFO
EOF
    chmod 600 .env
    echo -e "✅ .env 文件已创建"
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
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 http://localhost:8062/health)

if [ "$HEALTH" = "200" ]; then
    IP=$(hostname -I | awk '{print $1}')
    echo ""
    echo -e "${GREEN}╔══════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║             🎉 部署成功！                       ║${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  服务地址: ${CYAN}http://${IP}:8062${NC}"
    echo -e "  MCP 端点: ${CYAN}http://${IP}:8062/mcp${NC}"
    echo -e "  健康检查: ${CYAN}http://localhost:8062/health${NC}"
    echo ""
    echo -e "${YELLOW}下一步:${NC}"
    echo "  1. 查看日志: docker compose logs -f"
    echo "  2. 注册 MCP: 在 StarAgent 平台填写服务地址"
    echo "  3. 绑定 Skill: 将 apollo-config-query 技能绑定到此 MCP"
else
    echo -e "${RED}❌ 健康检查失败 (HTTP $HEALTH)${NC}"
    echo "查看日志排查:"
    echo "  docker compose logs apollo-mcp"
    exit 1
fi