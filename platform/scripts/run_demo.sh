#!/usr/bin/env bash
# 本地文件模式一键启动（不依赖 Docker）：
#   mock MCP（数据=backend/mockdata） + 平台后端（拓扑=仓库 topo/tprel/data，状态=backend/data/demo）
#
# 用法：
#   ./scripts/run_demo.sh            # 只起 mock + 后端
#   ./scripts/run_demo.sh --frontend # 额外起前端 dev server (5173)
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BACKEND="$ROOT/backend"
WORKSPACE=$(cd "$ROOT/.." && pwd)
BIN="$ROOT/.bin"
STORE_DIR="${STORE_DIR:-$BACKEND/data/demo}"

export MOCKDATA_DIR="$BACKEND/mockdata"
export STORE_DIR
export TOPO_MODE=file
export TOPO_DATA_DIR="${TOPO_DATA_DIR:-$WORKSPACE/topo/tprel/data}"
export PINPOINT_MCP_URL="${PINPOINT_MCP_URL:-http://127.0.0.1:8888/mcp}"
export PROMETHEUS_MCP_URL="${PROMETHEUS_MCP_URL:-http://127.0.0.1:8090/mcp}"
export CMDB_MCP_URL="${CMDB_MCP_URL:-http://127.0.0.1:8061/mcp}"
export ES_MCP_URL="${ES_MCP_URL:-http://127.0.0.1:9201/mcp}"
# 演示时关闭巡检定时任务，避免 20s 一次的 RISK 告警占满自动排障名额
export INSPECT_CRON_ENABLED="${INSPECT_CRON_ENABLED:-false}"

python3 "$BACKEND/scripts/seed_demo.py"
mkdir -p "$BIN" "$STORE_DIR"
(cd "$BACKEND" && go build -o "$BIN/mockmcp" ./cmd/mockmcp && go build -o "$BIN/aiops-server" ./cmd/server)

"$BIN/mockmcp" &
MOCK_PID=$!
"$BIN/aiops-server" &
API_PID=$!
PIDS=("$MOCK_PID" "$API_PID")

cleanup() { kill "${PIDS[@]}" 2>/dev/null || true; }
trap cleanup EXIT INT TERM

if [[ "${1:-}" == "--frontend" ]]; then
  (cd "$ROOT/frontend" && npm run dev) &
  PIDS+=($!)
fi

echo "[run_demo] mock=:8888/:8090/:8061/:9201  api=:8080  store=$STORE_DIR"
wait
