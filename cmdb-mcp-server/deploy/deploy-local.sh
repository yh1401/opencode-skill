#!/bin/bash
# cmdb-mcp-server 本地一键部署脚本（Python 原生启动）
# 用法: chmod +x deploy-local.sh && ./deploy-local.sh
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"
cd ..

command -v python3 >/dev/null 2>&1 || { echo "错误: 未检测到 python3"; exit 1; }

# 2. 依赖检查
if python3 -c "import fastapi, uvicorn, requests, pydantic" 2>/dev/null; then
    echo "依赖已安装"
else
    echo "安装依赖..."
    python3 -m pip install -r requirements.txt -q -i https://pypi.tuna.tsinghua.edu.cn/simple
fi

# 3. 端口检查
PORT=8061
if lsof -i :$PORT >/dev/null 2>&1; then
    echo "端口 $PORT 被占用，请先释放"; exit 1
fi

# 4. 启动服务
mkdir -p logs
pkill -f "mcp_server.py" 2>/dev/null || true
sleep 1

LOG_FILE="logs/deploy-$(date +%Y%m%d-%H%M%S).log"
nohup python3 scripts/mcp_server.py --transport http --host 0.0.0.0 --port $PORT > "$LOG_FILE" 2>&1 &
echo "服务已启动 (PID: $!)"
echo "日志: $LOG_FILE"

# 5. 查看日志并验证
sleep 3
echo "--- 启动日志 ---"
tail -10 "$LOG_FILE"
echo "----------------"

HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 http://localhost:$PORT/health || echo "000")
if [ "$HEALTH" = "200" ]; then
    echo "部署成功: http://localhost:$PORT"
else
    echo "错误: 健康检查失败 (HTTP $HEALTH)"
    echo "查看日志: tail -f $LOG_FILE"
fi
