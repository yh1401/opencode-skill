#!/usr/bin/env bash
# Docker 全链路启动：造数 → 宿主机交叉编译 linux 二进制 → 构建前端 dist → docker compose up。
# 基础镜像只用本机已有的 alpine / nginx:alpine，不依赖外网镜像仓库。
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BACKEND="$ROOT/backend"
FRONTEND="$ROOT/frontend"
BUILD="$BACKEND/build"

# 目标架构与 docker 一致（Apple Silicon → arm64）
case "$(docker version --format '{{.Server.Arch}}')" in
  aarch64|arm64) GOARCH=arm64 ;;
  x86_64|amd64)  GOARCH=amd64 ;;
  *) GOARCH=amd64 ;;
esac

if [[ "${DATA_MODE:-mock}" == "mock" ]]; then
  python3 "$BACKEND/scripts/seed_demo.py"
fi

mkdir -p "$BUILD"
(cd "$BACKEND" \
  && GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BUILD/aiops-server" ./cmd/server \
  && GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BUILD/mockmcp" ./cmd/mockmcp)

# Go 以 TZ=Asia/Shanghai 解析时区，alpine 基础镜像无 tzdata，从宿主机拷入（-L 展开 macOS 的系统符号链接）
rm -rf "$BUILD/zoneinfo"
cp -RL /usr/share/zoneinfo "$BUILD/zoneinfo"

if [ ! -d "$FRONTEND/node_modules" ]; then
  (cd "$FRONTEND" && npm ci --no-audit --no-fund)
fi
(cd "$FRONTEND" && npm run build)

docker compose -f "$ROOT/docker-compose.yml" up -d --build "$@"

echo "[docker_up] 等待后端就绪 ..."
for _ in $(seq 1 40); do
  if curl -sf -m 2 http://127.0.0.1:8080/api/health >/dev/null; then
    echo "[docker_up] 后端 OK"
    break
  fi
  sleep 2
done

echo "[docker_up] 前端 http://127.0.0.1:5173   后端 http://127.0.0.1:8080   mock MCP :8888/:8090/:8061/:9201"
