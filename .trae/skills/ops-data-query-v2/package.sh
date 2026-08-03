#!/bin/bash

# ==============================================================================
# Skill Agent 打包脚本 - 用于 Dify 平台
# ==============================================================================
# 执行此脚本前，请确保：
# 1. MCP 服务器已配置完成 (mcp_server/)
# 2. cmdb-server-query 技能已正确配置 HTTP 接口
# 
# 使用方法:
#   ./package.sh                    # 默认使用 production 环境打包
#   ./package.sh development        # 使用开发环境打包
#   ./package.sh test               # 使用测试环境打包
# ==============================================================================

set -e

# 解析命令行参数，确定目标环境
TARGET_ENV="${1:-production}"

# 验证环境参数
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

SKILL_AGENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ZIP_NAME="skill-agent-dify.zip"
BUILD_DIR="${SKILL_AGENT_DIR}/build"
SKILL_AGENT_BUILD_DIR="${BUILD_DIR}/skill-agent"

echo "=========================================="
echo "  Skill Agent 打包脚本 (Dify)"
echo "=========================================="
echo "当前目录: ${SKILL_AGENT_DIR}"
echo "目标环境: ${TARGET_ENV}"
echo ""

# 创建构建目录（包含 skill-agent 子目录）
rm -rf "${BUILD_DIR}"
mkdir -p "${SKILL_AGENT_BUILD_DIR}"

# 复制核心文件到 skill-agent 目录下
echo "[1/5] 复制核心文件..."
cp -r "${SKILL_AGENT_DIR}/registry" "${SKILL_AGENT_BUILD_DIR}/"
cp -r "${SKILL_AGENT_DIR}/skills" "${SKILL_AGENT_BUILD_DIR}/"
cp -r "${SKILL_AGENT_DIR}/config" "${SKILL_AGENT_BUILD_DIR}/"
cp -r "${SKILL_AGENT_DIR}/docs" "${SKILL_AGENT_BUILD_DIR}/"
cp -r "${SKILL_AGENT_DIR}/mcp" "${SKILL_AGENT_BUILD_DIR}/"
cp "${SKILL_AGENT_DIR}/SKILL.md" "${SKILL_AGENT_BUILD_DIR}/"
cp "${SKILL_AGENT_DIR}/SKILL.html" "${SKILL_AGENT_BUILD_DIR}/"
cp "${SKILL_AGENT_DIR}/ROUTING.md" "${SKILL_AGENT_BUILD_DIR}/"
cp "${SKILL_AGENT_DIR}/MOCK_DATA.md" "${SKILL_AGENT_BUILD_DIR}/"

# 清理测试文件和不必要的文件
echo "[2/5] 清理不必要的文件..."
rm -rf "${SKILL_AGENT_BUILD_DIR}/skills/cmdb-server-query/postman_collection.json" 2>/dev/null || true
rm -rf "${SKILL_AGENT_BUILD_DIR}/skills/cmdb-server-query/POSTMAN_GUIDE.md" 2>/dev/null || true
rm -rf "${SKILL_AGENT_BUILD_DIR}/skills/cmdb-server-query/USAGE.md" 2>/dev/null || true

# 根据目标环境更新配置文件
echo "[3/5] 配置环境变量..."

# 更新 api.yaml 中的环境配置
API_CONFIG="${SKILL_AGENT_BUILD_DIR}/skills/cmdb-server-query/config/api.yaml"
if [ -f "${API_CONFIG}" ]; then
    # 将环境变量名称替换为目标环境
    sed -i.bak "s/name: \"\\\${SKILL_ENV:-development}\"/name: \"${TARGET_ENV}\"/g" "${API_CONFIG}"
    rm -f "${API_CONFIG}.bak"
    echo " ✅ 已更新 API 配置为 ${TARGET_ENV} 环境"
fi

# 创建环境变量配置文件
echo "SKILL_ENV=${TARGET_ENV}" > "${SKILL_AGENT_BUILD_DIR}/.env"
echo " ✅ 已创建环境变量配置文件"

# 检查配置
echo "[4/6] 验证配置..."

# 检查 cmdb-server-query 是否已启用
ENABLED=$(grep -A5 '"id": "cmdb-server-query"' "${SKILL_AGENT_BUILD_DIR}/registry/skills.json" | grep '"enabled":' | awk '{print $2}' | tr -d ',')
if [ "${ENABLED}" != "true" ]; then
    echo " ⚠️ 警告: cmdb-server-query 未启用"
fi

# 检查 API 配置
if [ -f "${API_CONFIG}" ]; then
    echo " ✅ API配置文件存在: ${API_CONFIG}"
    # 显示当前配置的API地址
    BASE_URL=$(grep -A2 " ${TARGET_ENV}:" "${API_CONFIG}" | grep baseUrl | awk '{print $2}' | tr -d '"')
    ENDPOINT=$(grep -A3 " ${TARGET_ENV}:" "${API_CONFIG}" | grep endpoint | awk '{print $2}' | tr -d '"')
    echo "   └─ API地址: ${BASE_URL}${ENDPOINT}"
else
    echo " ⚠️ 警告: API配置文件不存在"
fi

# 打包（进入 build 目录，打包 skill-agent 文件夹）
echo "[5/6] 打包文件..."
cd "${BUILD_DIR}"
zip -r "${ZIP_NAME}" skill-agent > /dev/null
mv "${ZIP_NAME}" "${SKILL_AGENT_DIR}/"

# 清理构建目录
echo "[6/6] 清理临时文件..."
cd "${SKILL_AGENT_DIR}"
rm -rf "${BUILD_DIR}"

echo ""
echo "=========================================="
echo " 打包完成!"
echo "=========================================="
echo "输出文件: ${SKILL_AGENT_DIR}/${ZIP_NAME}"
echo "目标环境: ${TARGET_ENV}"
echo ""

# 显示环境配置摘要
case "${TARGET_ENV}" in
    development)
        echo "📋 开发环境配置:"
        echo "   - API地址: http://localhost:3000/api/v2/cmdbServer/getCmdbServerBaseMessageList"
        echo "   - 使用Mock数据: 是"
        echo "   - 用途: 本地开发测试"
        ;;
    production)
        echo "📋 生产环境配置:"
        echo "   - API地址: https://oss.tech.ctseelink.cn/api/v2/cmdbServer/getCmdbServerPageList"
        echo "   - 使用Mock数据: 否"
        echo "   - 用途: Dify平台部署"
        ;;
    test)
        echo "📋 测试环境配置:"
        echo "   - API地址: http://test-server:3001/api/v2/cmdbServer/getCmdbServerBaseMessageList"
        echo "   - 使用Mock数据: 是"
        echo "   - 用途: 测试环境验证"
        ;;
esac

echo ""
echo "使用方法:"
echo "1. 在 Dify 中上传 ${ZIP_NAME}"
echo ""
echo "2. 测试查询:"
echo "   - 查找贵州机房的服务器"
echo "   - 查询在线的服务器"
echo "   - 查询生产环境的Linux服务器"
echo ""
echo "当前启用的技能:"
grep -A5 '"enabled": true' "${SKILL_AGENT_DIR}/registry/skills.json" | grep '"name":' | awk '{print "  ✅ " $2}' | tr -d '",'
echo ""
echo "已禁用的技能:"
grep -A5 '"enabled": false' "${SKILL_AGENT_DIR}/registry/skills.json" | grep '"name":' | awk '{print "  ❌ " $2}' | tr -d '",'