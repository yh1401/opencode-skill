#!/bin/bash

# ==============================================================================
# Skill Agent 打包脚本（MCP 版）- 用于 Dify/StarAgent 平台
# 本技能通过 MCP 服务 (cmdb-mcp-server) 获取数据，需先在平台注册 MCP 服务
# 打包产物输出到 release/cmdb-query-mcp-v{n}.zip（自动递增版本号）
# ==============================================================================
# 使用方法:
#   ./_dev/package.sh                    # 默认 production 环境
#   ./_dev/package.sh development        # 开发环境
#   ./_dev/package.sh test               # 测试环境
# ==============================================================================

set -e

TARGET_ENV="${1:-production}"

case "${TARGET_ENV}" in
    development|production|test)
        echo "目标环境: ${TARGET_ENV}"
        ;;
    *)
        echo "错误: 无效的环境参数 '${TARGET_ENV}'"
        echo "支持的环境: development, production, test"
        exit 1
        ;;
esac

# 技能根目录 = 本脚本所在目录(_dev)的上一级
DEV_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILL_AGENT_DIR="$(cd "${DEV_DIR}/.." && pwd)"
BUILD_DIR="${DEV_DIR}/build"
SKILL_AGENT_BUILD_DIR="${BUILD_DIR}/skill-agent"
RELEASE_DIR="${SKILL_AGENT_DIR}/release"

# 自动生成版本号：检测 release/ 目录下已存在的 cmdb-query-mcp-v{n}.zip
find_next_version() {
    local base_name="cmdb-query-mcp"
    local version=1
    local pattern="${base_name}-v*.zip"

    # 检查 release/ 目录下的已存在版本
    if [ -d "${RELEASE_DIR}" ]; then
        local max_version=$(ls "${RELEASE_DIR}" | grep -E "^${base_name}-v([0-9]+)\.zip$" | sed "s/^${base_name}-v\\([0-9]*\\)\\.zip/\1/" | sort -n | tail -1)
        if [ -n "${max_version}" ] && [ "${max_version}" -ge "${version}" ]; then
            version=$((max_version + 1))
        fi
    fi

    echo "${version}"
}

VERSION=$(find_next_version)
ZIP_NAME="cmdb-query-mcp-v${VERSION}.zip"

echo "=========================================="
echo "  Skill Agent 打包脚本（MCP 版）"
echo "=========================================="
echo "技能根目录: ${SKILL_AGENT_DIR}"
echo "目标环境: ${TARGET_ENV}"
echo ""

# 创建构建目录
rm -rf "${BUILD_DIR}"
mkdir -p "${SKILL_AGENT_BUILD_DIR}"

# 复制核心文件（_dev/ 目录不在列表中，自然排除 mcp/、打包脚本等开发产物）
echo "[1/4] 复制核心文件..."
cp -r "${SKILL_AGENT_DIR}/registry" "${SKILL_AGENT_BUILD_DIR}/"
cp -r "${SKILL_AGENT_DIR}/skills" "${SKILL_AGENT_BUILD_DIR}/"
cp "${SKILL_AGENT_DIR}/SKILL.md" "${SKILL_AGENT_BUILD_DIR}/"

# 创建环境变量配置文件（MCP 版：记录 MCP 服务标识，由平台路由）
echo "[2/4] 配置环境变量..."
cat > "${SKILL_AGENT_BUILD_DIR}/.env" << EOF
SKILL_ENV=${TARGET_ENV}
MCP_SERVER=cmdb-mcp-server
MCP_TOOL=cmdb_server_query
MCP_ENDPOINT=http://localhost:5235/gateway/cmdb/mcp
EOF

# 验证配置
echo "[3/4] 验证配置..."
ENABLED=$(grep -A8 '"id": "cmdb-server-query"' "${SKILL_AGENT_BUILD_DIR}/registry/skills.json" | grep '"enabled":' | awk '{print $2}' | tr -d ',')
if [ "${ENABLED}" = "true" ]; then
    echo " ✅ cmdb-server-query 已启用"
else
    echo " ⚠️ 警告: cmdb-server-query 未启用"
fi

# 打包
echo "[4/4] 打包文件..."
cd "${BUILD_DIR}"
zip -r "${ZIP_NAME}" skill-agent > /dev/null

# 确保 release 目录存在
mkdir -p "${RELEASE_DIR}"
mv "${ZIP_NAME}" "${RELEASE_DIR}/"

# 清理构建目录
cd "${DEV_DIR}"
rm -rf "${BUILD_DIR}"

echo ""
echo "=========================================="
echo " 打包完成!"
echo "=========================================="
echo "输出文件: ${RELEASE_DIR}/${ZIP_NAME}"
echo "目标环境: ${TARGET_ENV}"
echo ""

case "${TARGET_ENV}" in
    development)
        echo "📋 开发环境配置:"
        echo "   - MCP 服务: cmdb-mcp-server（http://localhost:5235/gateway/cmdb/mcp）"
        ;;
    production)
        echo "📋 生产环境配置:"
        echo "   - MCP 服务: cmdb-mcp-server（http://localhost:5235/gateway/cmdb/mcp）"
        ;;
    test)
        echo "📋 测试环境配置:"
        echo "   - MCP 服务: cmdb-mcp-server（http://localhost:5235/gateway/cmdb/mcp）"
        ;;
esac

echo ""
echo "使用方法:"
echo "1. 部署 cmdb-mcp-server（参考 cmdb-mcp-server/deploy/deploy-guide.md）"
echo "2. 在 StarAgent 平台注册 MCP 服务 cmdb-mcp-server，地址 http://localhost:5235/gateway/cmdb/mcp"
echo "3. 在平台上传 ${ZIP_NAME}"
echo "4. 测试查询:"
echo "   - 查找贵州机房的服务器"
echo "   - 查询在线的服务器"
echo "   - 查询生产环境的Linux服务器"
echo ""
echo "当前启用的技能:"
grep -B5 '"enabled": true' "${SKILL_AGENT_DIR}/registry/skills.json" | grep '"name":' | awk '{print "  ✅ " $2}' | tr -d '",'
echo ""
