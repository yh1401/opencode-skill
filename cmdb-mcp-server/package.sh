#!/bin/bash
# cmdb-mcp-server 打包脚本
# 生成可上传到测试/生产服务器的安装包
# 用法: chmod +x package.sh && ./package.sh
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

VERSION="2.0.0"
TIMESTAMP=$(date +%Y%m%d%H%M%S)
PKG_NAME="cmdb-mcp-server-${VERSION}-${TIMESTAMP}.tar.gz"
DIST_DIR="dist"

echo "打包: $PKG_NAME"

# 检查必需文件
REQUIRED_FILES=(
    "scripts/mcp_server.py"
    "config/api_endpoints.json"
    "deploy/docker-compose.yml"
    "Dockerfile"
    "requirements.txt"
    "start.sh"
    "deploy/deploy-prod.sh"
    "deploy/deploy-guide.md"
)
for f in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "$f" ]; then
        echo "错误: 缺少 $f"; exit 1
    fi
done
echo "必需文件检查通过"

# 打包
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

if [ ! -f "${DIST_DIR}/${PKG_NAME}" ]; then
    echo "错误: 打包失败"; exit 1
fi
SIZE=$(du -h "${DIST_DIR}/${PKG_NAME}" | cut -f1)
echo "打包完成: ${DIST_DIR}/${PKG_NAME} (${SIZE})"
echo ""
echo "使用方法:"
echo "  scp ${DIST_DIR}/${PKG_NAME} user@server:/opt/"
echo "  cd /opt && tar xzf ${PKG_NAME}"
echo "  cd cmdb-mcp-server/deploy && cp ../.env.example .env"
echo "  ./deploy-prod.sh"
