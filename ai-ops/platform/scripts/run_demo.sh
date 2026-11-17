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
# 数据面总开关：mock（默认，脚本内起 mockmcp）| real（不启动 mock，按环境变量连真实上游）
export DATA_MODE="${DATA_MODE:-mock}"
export PINPOINT_MCP_URL="${PINPOINT_MCP_URL:-}"
export PROMETHEUS_MCP_URL="${PROMETHEUS_MCP_URL:-}"
export CMDB_MCP_URL="${CMDB_MCP_URL:-}"
export ES_MCP_URL="${ES_MCP_URL:-}"
# 演示时关闭巡检定时任务，避免 20s 一次的 RISK 告警占满自动排障名额
export INSPECT_CRON_ENABLED="${INSPECT_CRON_ENABLED:-false}"

if [[ "$DATA_MODE" == "mock" ]]; then
  python3 "$BACKEND/scripts/seed_demo.py"
fi
mkdir -p "$BIN" "$STORE_DIR"
(cd "$BACKEND" && go build -o "$BIN/aiops-server" ./cmd/server)
if [[ "$DATA_MODE" == "mock" ]]; then
  (cd "$BACKEND" && go build -o "$BIN/mockmcp" ./cmd/mockmcp)
fi

PIDS=()
if [[ "$DATA_MODE" == "mock" ]]; then
  "$BIN/mockmcp" &
  PIDS+=($!)
fi
"$BIN/aiops-server" &
PIDS+=($!)

cleanup() { kill "${PIDS[@]}" 2>/dev/null || true; }
trap cleanup EXIT INT TERM

if [[ "${1:-}" == "--frontend" ]]; then
  (cd "$ROOT/frontend" && npm run dev) &
  PIDS+=($!)
fi

echo "[run_demo] data_mode=$DATA_MODE  api=:8080  store=$STORE_DIR"
wait
