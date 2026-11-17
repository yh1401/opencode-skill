# syntax=docker/dockerfile:1

# ============================================================
# 构建阶段：从源码编译 linux/amd64 静态二进制
# ============================================================
FROM golang:1.26-alpine AS builder

# 国内环境可改用镜像代理：--build-arg GOPROXY=https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY=${GOPROXY}

WORKDIR /src

# 先复制依赖清单，最大化利用构建层缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制全部源码（含 //go:embed 所需的 cmd/prometheus-mcp-server/external/docs 目录）
COPY . .

ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG COMMIT=unknown

RUN go build -trimpath \
        -ldflags "-s -w \
          -X github.com/tjhop/prometheus-mcp-server/internal/version.Version=${VERSION} \
          -X github.com/tjhop/prometheus-mcp-server/internal/version.BuildDate=${BUILD_DATE} \
          -X github.com/tjhop/prometheus-mcp-server/internal/version.Commit=${COMMIT}" \
        -o /out/prometheus-mcp-server ./cmd/prometheus-mcp-server

# ============================================================
# 运行阶段：极简运行时，仅保留二进制与 CA 证书
# ============================================================
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 65532 nonroot

COPY --from=builder /out/prometheus-mcp-server /usr/local/bin/prometheus-mcp-server

USER nonroot
EXPOSE 8080

# 默认以 HTTP 传输对外提供 MCP 服务；Prometheus 地址与 easyviews 凭据通过环境变量注入
ENV PROMETHEUS_MCP_SERVER_MCP_TRANSPORT=http \
    PROMETHEUS_MCP_SERVER_WEB_LISTEN_ADDRESS=:8080 \
    PROMETHEUS_MCP_SERVER_PROMETHEUS_URL=http://127.0.0.1:9090

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/metrics || exit 1

ENTRYPOINT ["/usr/local/bin/prometheus-mcp-server"]
