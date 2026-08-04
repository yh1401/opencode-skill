#!/bin/bash
# ================================================
# cmdb-mcp-server - 打包脚本
# 生成可上传到测试/生产服务器的安装包
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
PKG_NAME="cmdb-mcp-server-${VERSION}-${TIMESTAMP}.tar.gz"
DIST_DIR="dist"

echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║        cmdb-mcp-server 打包脚本                ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

# 1. 检查必需文件
echo -e "${YELLOW}[1/3] 检查必需文件${NC}"
REQUIRED_FILES=(
    "scripts/mcp_server.py"
    "config/api_endpoints.json"
    "docker-compose.yml"
    "Dockerfile"
    "requirements.txt"
    "start.sh"
    "deploy/deploy-prod.sh"
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

# 2. 打包
echo -e "\n${YELLOW}[2/3] 打包安装包${NC}"
echo "  版本: ${VERSION}"
echo "  包名: ${PKG_NAME}"
mkdir -p "$DIST_DIR"

tar czf "${DIST_DIR}/${PKG_NAME}" \
    --exclude='.env' \
    --exclude='logs' \
    --exclude='*.log' \
    --exclude='.git' \
    --exclude='.gitignore' \
    --exclude='.dockerignore' \
    --exclude='__pycache__' \
    --exclude='*.pyc' \
    --exclude='.DS_Store' \
    --exclude='dist' \
    --exclude='package.sh' \
    scripts config deploy mock docs requirements.txt .env.example Dockerfile docker-compose.yml 2>/dev/null || true

# 检查打包结果
if [ ! -f "${DIST_DIR}/${PKG_NAME}" ]; then
    echo -e "${RED}❌ 打包失败${NC}"
    exit 1
fi
SIZE=$(du -h "${DIST_DIR}/${PKG_NAME}" | cut -f1)
echo -e "  ${GREEN}✅ 打包完成: ${DIST_DIR}/${PKG_NAME} (${SIZE})${NC}"

# 3. 汇总
echo -e "\n${YELLOW}[3/3] 打包汇总${NC}"
echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
echo -e "  安装包: ${GREEN}${DIST_DIR}/${PKG_NAME}${NC}"
echo -e "  大小:   ${SIZE}"
echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}📤 上传到服务器:${NC}"
echo "  scp ${DIST_DIR}/${PKG_NAME} user@server:/opt/"
echo ""
echo -e "${GREEN}🔧 服务器上解压部署:${NC}"
echo "  cd /opt"
echo "  mkdir -p cmdb-mcp-server && cd cmdb-mcp-server"
echo "  tar xzf ${PKG_NAME}"
echo "  cd deploy && chmod +x deploy-prod.sh && ./deploy-prod.sh"
