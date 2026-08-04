#!/bin/bash

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

PYTHON="python3"

# 检查并安装所有必需依赖
for pkg in requests fastapi uvicorn pydantic; do
    if ! $PYTHON -c "import ${pkg}" 2>/dev/null; then
        echo "安装依赖: ${pkg}..."
        $PYTHON -m pip install -r requirements.txt -q -i https://pypi.tuna.tsinghua.edu.cn/simple
        break
    fi
done

HOST=${1:-0.0.0.0}
PORT=${2:-8061}
DAEMON=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        -d|--daemon) DAEMON=true; shift ;;
        *) shift ;;
    esac
done

if $DAEMON; then
    nohup $PYTHON scripts/mcp_server.py --transport http --host "$HOST" --port "$PORT" > /dev/null 2>&1 &
    sleep 3
    if curl -s http://$HOST:$PORT/health > /dev/null; then
        echo "服务已启动: http://$HOST:$PORT"
    else
        echo "启动失败，请查看 logs/ 目录下的日志"
        exit 1
    fi
else
    $PYTHON scripts/mcp_server.py --transport http --host "$HOST" --port "$PORT"
fi
