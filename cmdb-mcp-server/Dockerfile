# cmdb-mcp-server Dockerfile
# 多阶段构建，最小化镜像体积

FROM python:3.11-slim AS builder

WORKDIR /app

# 替换为阿里云 Debian 镜像源（国内加速）
RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || \
    sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list 2>/dev/null || true

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt \
    -i https://pypi.tuna.tsinghua.edu.cn/simple

FROM python:3.11-slim

WORKDIR /app

RUN groupadd -r cmdb && useradd -r -g cmdb -d /app cmdb

COPY --from=builder /usr/local/lib/python3.11/site-packages /usr/local/lib/python3.11/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin

COPY scripts/ scripts/
COPY config/ config/
COPY mock/ mock/

RUN mkdir -p /app/logs && chown -R cmdb:cmdb /app

USER cmdb

EXPOSE 8061

# 健康检查: 使用 Python 标准库避免对 requests 的依赖
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import socket,sys; s=socket.socket(socket.AF_INET,socket.SOCK_STREAM); s.settimeout(2); sys.exit(0 if s.connect_ex(('localhost',8061))==0 else 1)"

ENV MCP_USE_MOCK=false
ENV PYTHONUNBUFFERED=1

CMD ["python", "scripts/mcp_server.py", "--host", "0.0.0.0", "--port", "8061"]
