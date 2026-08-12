#!/usr/bin/env python3
"""
本地 Mock：模拟第三方「Apollo Host 信息查询接口」/api/getApolloHostInfo

用途：
    第三方线上接口 https://easyops.tech.ctseelink.cn 在本地（无代理）不可达，
    本 Mock 服务用于本地全链路测试：MCP -> Mock(第三方接口) -> 解密组装 -> 查询 Apollo。

行为：
    - 监听端口 8066（可通过 --port 修改）
    - GET /api/getApolloHostInfo 返回文档同构的 list 数据
    - host 指向本地 Apollo (http://localhost:8070)，token 为「用 sessionId 加密后的本地 Token」
      （MCP 拉取后会用 sessionId 解密还原，链路完整）

启动方式：
    python3 mock/mock_host_service.py --port 8066
"""

import argparse
import base64
import logging
import os

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
import uvicorn

logger = logging.getLogger("mock-apollo-host-api")
logging.basicConfig(level=logging.INFO, format="%(asctime)s | %(levelname)-8s | %(name)s | %(message)s")

# 本地 Apollo 的连接信息（模拟第三方库中记录的一条数据）
MOCK_HOST_NAME = "本地Apollo测试-亿讯专用"
MOCK_HOST = "http://localhost:8070"
MOCK_USER = "eshore-user"
MOCK_REAL_TOKEN = "mcp-token-1785390342"  # 本地 Apollo OpenAPI Token（与 config/auth.json 一致）


def _encrypt_token(token: str, session_id: str) -> str:
    """与服务端一致的加密：token 逐字节 XOR sessionId 后 Base64"""
    xor_bytes = bytearray(
        ord(token[i]) ^ ord(session_id[i % len(session_id)])
        for i in range(len(token))
    )
    return base64.b64encode(bytes(xor_bytes)).decode('utf-8')


def _load_session_id() -> str:
    """从环境变量读取 sessionId（与 MCP ConfigClient 保持一致），默认线上 sessionId"""
    return os.environ.get('APOLLO_HOST_SESSION_ID', 'e5e27a7d1805758400287ae86741f889')


app = FastAPI(title="mock-apollo-host-api", description="Mock 第三方 Apollo Host 信息查询接口")


@app.get("/api/getApolloHostInfo", tags=["第三方接口"])
def get_apollo_host_info(request: Request, paginator: bool = True, pageIndex: int = 1, pageSize: int = 10,
                         name: str = "", secondProductId: str = "", host: str = "", user: str = "", token: str = ""):
    """模拟第三方接口：校验 Cookie sessionId，返回一条 Mock 的 Apollo Host 记录"""
    cookie = request.cookies.get("sessionId", "")
    if not cookie:
        return JSONResponse(status_code=401, content={"code": "fail", "message": "未认证的IP访问!"})

    session_id = cookie
    encrypted_token = _encrypt_token(MOCK_REAL_TOKEN, session_id)

    record = {
        "id": 15,
        "del_flag": False,
        "create_time": "2026-08-11 10:00:00",
        "update_time": "2026-08-11 10:00:00",
        "name": MOCK_HOST_NAME,
        "secondProductId": ["518"],
        "host": MOCK_HOST,
        "user": MOCK_USER,
        "token": encrypted_token,
        "operation": "",
        "operator": 0
    }

    # 模拟 name 模糊查询过滤
    if name and name.lower() not in MOCK_HOST_NAME.lower():
        return {"code": "success", "list": [], "pageTotal": 0}

    return {"code": "success", "list": [record], "pageTotal": 1}


@app.get("/health", tags=["基础"])
def health():
    return {"status": "healthy", "service": "mock-apollo-host-api"}


def main():
    parser = argparse.ArgumentParser(description="Mock Apollo Host 信息查询接口")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8066)
    args = parser.parse_args()

    logger.info(f"🚀 Mock Apollo Host API 启动: http://{args.host}:{args.port}")
    logger.info(f"🔑 sessionId: {_load_session_id()}")
    logger.info(f"🏠 Mock Host: {MOCK_HOST} (token 将用 sessionId 加密)")
    uvicorn.run(app, host=args.host, port=args.port, log_level="info", access_log=True)


if __name__ == "__main__":
    main()
