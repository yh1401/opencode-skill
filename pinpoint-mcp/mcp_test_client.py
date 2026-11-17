"""Pinpoint MCP Server 连通性测试脚本

用法（在服务器 pinpoint-mcp-server 的 venv 中执行）：
    .venv/bin/python mcp_test_client.py
    .venv/bin/python mcp_test_client.py http://127.0.0.1:8888/mcp   # 自定义地址
"""
import asyncio
import sys

from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client


async def main(url: str) -> None:
    print(f"== 连接 MCP Server: {url}")
    async with streamable_http_client(url) as (read, write, _):
        async with ClientSession(read, write) as session:
            # 1. 握手
            init = await session.initialize()
            print(f"== 握手成功: server={init.serverInfo.name} {init.serverInfo.version}")

            # 2. 列出工具
            tools = await session.list_tools()
            print(f"== 发现 {len(tools.tools)} 个工具:")
            for t in tools.tools:
                print(f"   - {t.name}: {t.description}")

            # 3. 调用 list_applications_tool 验证数据链路
            print("\n== 调用 list_applications_tool ...")
            result = await session.call_tool("list_applications_tool", {})
            print(f"== isError: {result.isError}")
            for content in result.content:
                print(f"== 返回: {content.text[:3000]}")
            print("\n== 测试完成")


if __name__ == "__main__":
    url = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8888/mcp"
    asyncio.run(main(url))
