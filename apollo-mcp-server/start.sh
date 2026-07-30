#!/bin/bash
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

PYTHON="python3"

# 创建日志目录
mkdir -p logs

# 检查依赖
if ! $PYTHON -c "import fastapi, uvicorn, requests" 2>/dev/null; then
    echo "📦 安装 Python 依赖..."
    $PYTHON -m pip install -r requirements.txt -q -i https://pypi.tuna.tsinghua.edu.cn/simple
fi

HOST=${1:-0.0.0.0}
PORT=${2:-8062}

echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║   Apollo MCP Server v2.0.0                  ║"
echo "╚══════════════════════════════════════════════╝"
echo ""

# 显示当前配置
echo "📍 地址:         http://${HOST}:${PORT}"
echo "📖 API 文档:     http://${HOST}:${PORT}/docs"
echo "🔌 MCP 端点:     http://${HOST}:${PORT}/mcp"
echo "🔧 当前环境:     ${APOLLO_ENV:-PRO}"
echo "🎯 Mock 模式:    ${MCP_USE_MOCK:-false}"
echo ""

# 启动
exec $PYTHON scripts/mcp_server.py --host "$HOST" --port "$PORT"