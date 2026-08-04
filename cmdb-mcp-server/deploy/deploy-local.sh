#!/bin/bash
# ================================================
# cmdb-mcp-server - 一键部署脚本（支持本地 Python / Docker Compose）
# 使用方法: chmod +x deploy-local.sh && ./deploy-local.sh
# ================================================
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"
cd .. # 切回项目根目录

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║       cmdb-mcp-server 本地部署脚本             ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

PYTHON="python3"
LOG_FILE="logs/deploy-$(date +%Y%m%d-%H%M%S).log"

# 1. 环境检查
echo -e "${YELLOW}[1/4] 环境检查${NC}"
command -v $PYTHON >/dev/null 2>&1 || { echo -e "${RED}❌ 请先安装 Python3${NC}"; exit 1; }
echo "✅ Python: $($PYTHON --version)"

if $PYTHON -c "import fastapi, uvicorn, requests, pydantic" 2>/dev/null; then
    echo "✅ 依赖: 已安装"
else
    echo "⏳ 安装依赖..."
    $PYTHON -m pip install -r requirements.txt -q -i https://pypi.tuna.tsinghua.edu.cn/simple
fi

# 2. 端口占用检查
echo -e "\n${YELLOW}[2/4] 端口检查${NC}"
PORT=8061
if lsof -i :$PORT >/dev/null 2>&1; then
    echo -e "${RED}❌ 端口 $PORT 被占用，请先释放或修改端口${NC}"
    exit 1
fi
echo "✅ 端口 $PORT 空闲"

# 3. 启动服务
echo -e "\n${YELLOW}[3/4] 启动服务${NC}"
mkdir -p logs

# 清理可能的残留进程
pkill -f "mcp_server.py" 2>/dev/null || true
sleep 1

nohup $PYTHON scripts/mcp_server.py --transport http --host 0.0.0.0 --port $PORT > "$LOG_FILE" 2>&1 &
PID=$!
echo "✅ 服务已启动 (PID: $PID)"
echo "   日志文件: $LOG_FILE"

# 4. 健康检查
echo -e "\n${YELLOW}[4/4] 健康检查${NC}"
sleep 3
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 http://localhost:$PORT/health || echo "000")

if [ "$HEALTH" = "200" ]; then
    echo -e "${GREEN}╔══════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║             🎉 启动成功！                       ║${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  服务地址: ${CYAN}http://localhost:$PORT${NC}"
    echo -e "  MCP 端点: ${CYAN}http://localhost:$PORT/mcp${NC}"
    echo -e "  健康检查: ${CYAN}http://localhost:$PORT/health${NC}"
    echo -e "  Swagger: ${CYAN}http://localhost:$PORT/docs${NC}"
    echo ""
    echo -e "${YELLOW}下一步:${NC}"
    echo "  1. 查看日志: tail -f $LOG_FILE"
    echo "  2. 停止服务: kill $PID"
    echo "  3. 测试接口: curl http://localhost:$PORT/"
else
    echo -e "${RED}❌ 健康检查失败 (HTTP $HEALTH)${NC}"
    echo "查看日志: cat $LOG_FILE"
    kill $PID 2>/dev/null
    exit 1
fi
