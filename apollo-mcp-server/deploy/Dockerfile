# Apollo MCP Server - Dockerfile
# 多阶段构建，最小化镜像体积

FROM python:3.11-slim AS builder

WORKDIR /app

# 替换为阿里云 Debian 镜像源（国内加速）
RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || \
    sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list 2>/dev/null || true

# 安装编译依赖
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

# 安装 Python 依赖
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt \
    -i https://pypi.tuna.tsinghua.edu.cn/simple

# 生产镜像
FROM python:3.11-slim

WORKDIR /app

# 创建非 root 用户运行
RUN groupadd -r apollo && useradd -r -g apollo -d /app apollo

# 从 builder 复制 Python 包
COPY --from=builder /usr/local/lib/python3.11/site-packages /usr/local/lib/python3.11/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin

# 复制应用代码
COPY scripts/ scripts/
COPY config/ config/
COPY mock/ mock/

# 创建日志目录
RUN mkdir -p /app/logs && chown -R apollo:apollo /app

# 切换到非 root 用户
USER apollo

# 暴露端口
EXPOSE 8062

# 健康检查（使用标准库 socket，不依赖 requests，避免模块加载失败导致不健康）
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import socket,sys; s=socket.socket(socket.AF_INET,socket.SOCK_STREAM); s.settimeout(2); sys.exit(0 if s.connect_ex(('localhost',8062))==0 else 1)"

# 默认 Mock 模式（生产环境通过环境变量关闭）
ENV MCP_USE_MOCK=true
ENV PYTHONUNBUFFERED=1

# 启动
CMD ["python", "scripts/mcp_server.py", "--host", "0.0.0.0", "--port", "8062"]