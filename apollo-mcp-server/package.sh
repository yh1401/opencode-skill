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
DIST_DIR="dist"

echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║      Apollo MCP Server 打包脚本                ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

# 1. 检查必需文件
echo -e "${YELLOW}[1/4] 检查必需文件${NC}"
REQUIRED_FILES=(
    "scripts/mcp_server.py"
    "config/api_endpoints.json"
    "deploy/docker-compose.yml"
    "deploy/Dockerfile"
    "requirements.txt"
    "deploy/start.sh"
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

# ================================================
# 第三方「Apollo Host 信息查询接口」配置（推荐方式）
# 接口地址与 sessionId 默认值已内置在 mcp_server.py（ConfigClient），
# 默认不需要配置；如需覆盖（如本地测试指向 Mock: http://localhost:8066）再取消注释：
# ================================================

# 第三方接口地址（默认 https://easyops.tech.ctseelink.cn）
# APOLLO_HOST_API_BASE=https://easyops.tech.ctseelink.cn

# 第三方接口路径（默认 /thirdApi/getApolloHostInfo；本地 mock 测试改为 /api/getApolloHostInfo）
# APOLLO_HOST_PATH=/thirdApi/getApolloHostInfo

# 第三方鉴权 sessionId（32位），同时是 token 解密密钥
# APOLLO_HOST_SESSION_ID=e5e27a7d1805758400287ae86741f889

# 可选：按服务名称模糊过滤第三方返回的 host（不填取第一条）
# APOLLO_HOST_NAME=天翼云眼

# 可选：强制替换 host 端口（第三方返回端口与 OpenAPI 实际端口不一致时）
# APOLLO_HOST_PORT_OVERRIDE=8070

# ================================================
# 备用方式：直接指定 Apollo 地址与 Token（仅当第三方接口不可用时才需要）
# ================================================

# Apollo OpenAPI 统一服务地址（端口 8070，查询配置/应用列表共用）
APOLLO_OPENAPI_HOST=http://apollo-config.tech.ctseelink.cn:8070

# Apollo OpenAPI Token（备用方式必填；推荐方式下留空，由第三方接口解密提供）
APOLLO_OPENAPI_TOKEN=

# 日志级别：DEBUG/INFO/WARNING/ERROR
LOG_LEVEL=INFO
EOF
echo -e "  ${GREEN}✅ .env.example 已生成${NC}"

# 3. 打包
echo -e "\n${YELLOW}[3/4] 打包安装包${NC}"
echo "  版本: ${VERSION}"
echo "  包名: ${PKG_NAME}"
mkdir -p "$DIST_DIR"

# 排除敏感文件和非必要文件（保留 mock_responses.json，Dockerfile 构建需要 mock/ 目录）
tar czf "${DIST_DIR}/${PKG_NAME}" \
    --exclude='.env' \
    --exclude='config/auth.json' \
    --exclude='config/auth.json.bak' \
    --exclude='config/auth.json.example' \
    --exclude='logs' \
    --exclude='*.log' \
    --exclude='.git' \
    --exclude='.gitignore' \
    --exclude='.dockerignore' \
    --exclude='mock/*.sql' \
    --exclude='tools' \
    --exclude='__pycache__' \
    --exclude='*.pyc' \
    --exclude='.DS_Store' \
    --exclude='dist' \
    --exclude='package.sh' \
    --exclude="$PKG_NAME" \
    scripts config deploy docs mock requirements.txt .env.example 2>/dev/null || true

# 检查打包结果
if [ ! -f "${DIST_DIR}/${PKG_NAME}" ]; then
    echo -e "${RED}❌ 打包失败${NC}"
    exit 1
fi
SIZE=$(du -h "${DIST_DIR}/${PKG_NAME}" | cut -f1)
echo -e "  ${GREEN}✅ 打包完成: ${DIST_DIR}/${PKG_NAME} (${SIZE})${NC}"

# 4. 汇总
echo -e "\n${YELLOW}[4/4] 打包汇总${NC}"
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
echo "  mkdir -p apollo-mcp-server && cd apollo-mcp-server"
echo "  tar xzf ${PKG_NAME}"
echo "  cd deploy && chmod +x deploy-prod.sh && ./deploy-prod.sh"
echo ""
echo -e "${YELLOW}⚠️ 注意: 打包已排除 config/auth.json，服务器上通过 deploy-prod.sh 交互输入或 .env 配置 Token${NC}"
