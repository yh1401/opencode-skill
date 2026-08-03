#!/bin/bash
# ================================================
# Apollo MCP Server - 打包脚本
# 生成可上传到生产服务器的安装包
# 使用方法: chmod +x package.sh && ./package.sh
# ================================================
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

VERSION="2.0.0"
TIMESTAMP=$(date +%Y%m%d%H%M%S)
PKG_NAME="apollo-mcp-server-${VERSION}-${TIMESTAMP}.tar.gz"

echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║      Apollo MCP Server 打包脚本                ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

# 1. 检查必需文件
echo -e "${YELLOW}[1/4] 检查必需文件${NC}"
REQUIRED_FILES=(
    "scripts/mcp_server.py"
    "config/api_endpoints.json"
    "docker-compose.yml"
    "Dockerfile"
    "requirements.txt"
    "start.sh"
    "deploy-prod.sh"
)
MISSING=0
for f in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "$f" ]; then
        echo -e "  ${RED}❌ 缺少: $f${NC}"
        MISSING=1
    fi
done
if [ "$MISSING" = "1" ]; then
    echo -e "${RED}❌ 必需文件缺失，打包中止${NC}"
    exit 1
fi
echo -e "  ${GREEN}✅ 所有必需文件存在${NC}"

# 2. 生成 .env.example 模板
echo -e "\n${YELLOW}[2/4] 生成 .env.example 模板${NC}"
cat > .env.example << 'EOF'
# ================================================
# Apollo MCP Server 生产环境配置模板
# 复制为 .env 并填写真实值: cp .env.example .env
# ================================================

# Mock 数据开关（生产必须为 false）
MCP_USE_MOCK=false

# Apollo 环境（PRO/DEV/SIT/LOCAL，用于日志标识）
APOLLO_ENV=PRO

# Apollo ConfigService 地址（端口 8080）
APOLLO_CONFIG_HOST=http://apollo-config.tech.ctseelink.cn:8080

# Apollo OpenAPI 地址（端口 8070）
APOLLO_OPENAPI_HOST=http://apollo-config.tech.ctseelink.cn:8070

# Apollo OpenAPI Token（必填，从 Apollo Portal 管理员工具->开放平台授权获取）
APOLLO_OPENAPI_TOKEN=your_token_here

# 日志级别：DEBUG/INFO/WARNING/ERROR
LOG_LEVEL=INFO
EOF
echo -e "  ${GREEN}✅ .env.example 已生成${NC}"

# 3. 打包
echo -e "\n${YELLOW}[3/4] 打包安装包${NC}"
echo "  版本: ${VERSION}"
echo "  包名: ${PKG_NAME}"

# 排除敏感文件和非必要文件
tar czf "$PKG_NAME" \
    --exclude='.env' \
    --exclude='config/auth.json' \
    --exclude='config/auth.json.bak' \
    --exclude='config/auth.json.example' \
    --exclude='logs' \
    --exclude='*.log' \
    --exclude='.git' \
    --exclude='.gitignore' \
    --exclude='.dockerignore' \
    --exclude='sql_output' \
    --exclude='references/mock_responses.json' \
    --exclude='__pycache__' \
    --exclude='*.pyc' \
    --exclude='.DS_Store' \
    --exclude='test_output_format.py' \
    --exclude='package.sh' \
    --exclude="$PKG_NAME" \
    scripts config references DEPLOY.md README.md Dockerfile docker-compose.yml requirements.txt start.sh deploy-prod.sh .env.example 2>/dev/null || true

# 检查打包结果
if [ ! -f "$PKG_NAME" ]; then
    echo -e "${RED}❌ 打包失败${NC}"
    exit 1
fi
SIZE=$(du -h "$PKG_NAME" | cut -f1)
echo -e "  ${GREEN}✅ 打包完成: ${PKG_NAME} (${SIZE})${NC}"

# 4. 汇总
echo -e "\n${YELLOW}[4/4] 打包汇总${NC}"
echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
echo -e "  安装包: ${GREEN}${PKG_NAME}${NC}"
echo -e "  大小:   ${SIZE}"
echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}📤 上传到服务器:${NC}"
echo "  scp ${PKG_NAME} user@server:/opt/"
echo ""
echo -e "${GREEN}🔧 服务器上解压部署:${NC}"
echo "  cd /opt"
echo "  tar xzf ${PKG_NAME}"
echo "  cd apollo-mcp-server"
echo "  cp .env.example .env   # 然后编辑 .env 填入真实 Token"
echo "  vim .env"
echo "  chmod +x deploy-prod.sh && ./deploy-prod.sh"
echo ""
echo -e "${YELLOW}⚠️ 注意: 打包已排除 config/auth.json，服务器上需通过 .env 或 auth.json 配置 Token${NC}"
