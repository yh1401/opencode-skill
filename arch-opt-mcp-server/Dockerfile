FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt -i https://pypi.tuna.tsinghua.edu.cn/simple

COPY scripts/ scripts/
COPY config/ config/
COPY mock/ mock/

ENV MCP_USE_MOCK=false
ENV LOG_TO_FILE=false

EXPOSE 8062

CMD ["python3", "scripts/mcp_server.py", "--transport", "http", "--host", "0.0.0.0", "--port", "8062"]
