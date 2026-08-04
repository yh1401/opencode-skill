#!/usr/bin/env python3
"""
标准 MCP 服务器 - cmdb-mcp-server
为企业 CMDB 运维数据提供标准化的 MCP 工具调用接口

遵循标准 MCP 协议规范：
- SSE 传输模式：GET /sse + POST /messages
- JSON-RPC 2.0 协议格式
- initialize/initialized 握手流程
- tools/list 和 tools/call 方法

启动方式：
    python3 scripts/mcp_server.py --port 8000
    或
    ./start.sh

文档：http://localhost:8000/docs
"""

import json
import os
import sys
import argparse
import asyncio
import time
from typing import Optional, Dict, Any, List, Tuple
from datetime import datetime

_script_dir = os.path.dirname(os.path.abspath(__file__))
_root_dir = os.path.dirname(_script_dir)
_config_dir = os.path.join(_root_dir, 'config')
_references_dir = os.path.join(_root_dir, 'mock')

sys.path.insert(0, _script_dir)
from mcp_logger import get_logger

logger = get_logger()

USE_MOCK = os.environ.get('MCP_USE_MOCK', 'false').lower() == 'true'
if USE_MOCK:
    logger.log_warning("Mock 数据模式已启用")

try:
    import requests
    HAS_REQUESTS = True
except ImportError:
    HAS_REQUESTS = False
    logger.log_error("未安装 requests 模块，请先安装: pip install requests")

try:
    from fastapi import FastAPI, HTTPException
    from fastapi.responses import JSONResponse, StreamingResponse
    from pydantic import BaseModel
    import uvicorn
    HAS_FASTAPI = True
except ImportError:
    HAS_FASTAPI = False




class APIConfig:
    def __init__(self):
        self.base_url = "https://oss.tech.ctseelink.cn"
        self.endpoints = {}
        self.auth_headers = {}
        self.load_config()
    
    def load_config(self):
        config_path = os.path.join(_config_dir, 'api_endpoints.json')
        if os.path.exists(config_path):
            with open(config_path, 'r', encoding='utf-8') as f:
                data = json.load(f)
                
                self.base_url = data.get('base_url', self.base_url)
                self.endpoints = data.get('endpoints', {})
                self.auth_headers = data.get('auth_headers', {})
                
                env_overrides = data.get('env_overrides', {})
                
                for config_key, env_key in env_overrides.items():
                    env_value = os.environ.get(env_key)
                    if env_value is not None:
                        if config_key == 'base_url':
                            self.base_url = env_value
                            logger.log_info(f"环境变量覆盖配置: {env_key}={self.base_url}")
                        elif config_key == 'mock_mode':
                            mock_value = env_value.lower() == 'true'
                            logger.log_info(f"环境变量覆盖配置: {env_key}={mock_value}")
                
                for endpoint_id, endpoint in self.endpoints.items():
                    if 'path' in endpoint:
                        endpoint['full_url'] = f"{self.base_url}{endpoint['path']}"
                
            logger.log_info(f"加载 API 配置: base_url={self.base_url}, endpoints={list(self.endpoints.keys())}")
        else:
            logger.log_warning(f"API 配置文件不存在: {config_path}")


def _normalize_pagination(skill_id: str, result: dict) -> dict:
    data = result.get('data', {})
    if skill_id in ['server-public-ip-query', 'product-query', 'project-basis-query']:
        if 'current' in data:
            data['currentPage'] = data.pop('current')
        if 'size' in data:
            data['pageSize'] = data.pop('size')
    return result


class CMDBAPIClient:
    def __init__(self):
        self.config = APIConfig()
        self._session = None
        self._request_count = 0
        self._success_count = 0
        self._mock_count = 0
    
    def _ensure_session(self):
        if HAS_REQUESTS and self._session is None:
            self._session = requests.Session()
            if self.config.auth_headers:
                self._session.headers.update(self.config.auth_headers)
            
            proxy_env = os.environ.get('HTTP_PROXY') or os.environ.get('http_proxy')
            if proxy_env:
                self._session.proxies = {'http': proxy_env, 'https': proxy_env}
                logger.log_info(f"HTTP 会话已配置代理: {proxy_env}")
            
            logger.log_info(f"创建 HTTP 会话，认证头: {list(self.config.auth_headers.keys()) if self.config.auth_headers else '无'}")
    
    def _get_mock_response(self, skill_id: str) -> dict:
        mock_path = os.path.join(_references_dir, 'mock_responses.json')
        if os.path.exists(mock_path):
            with open(mock_path, 'r', encoding='utf-8') as f:
                mock_data = json.load(f)
                result = mock_data.get(skill_id, {"code": 500, "message": "Mock 数据不存在", "data": {"records": [], "total": 0}})
                self._mock_count += 1
                logger.log_mock_usage(skill_id, "API 请求失败或 Mock 模式启用")
                return _normalize_pagination(skill_id, result)
        logger.log_error(f"Mock 数据文件不存在: {mock_path}")
        return {"code": 500, "message": "Mock 数据文件不存在", "data": {"records": [], "total": 0}}
    
    def _request(self, skill_id: str, params: dict) -> dict:
        self._request_count += 1
        endpoint = self.config.endpoints.get(skill_id, {})
        url = endpoint.get('full_url', '')
        method = endpoint.get('method', 'POST').upper()
        description = endpoint.get('description', '')
        
        logger.log_api_request(skill_id, method, url, params)
        
        if not url:
            if USE_MOCK:
                logger.log_warning(f"[API] [{skill_id}] 未找到 API 端点配置，description={description}, 使用 Mock 数据")
                return self._get_mock_response(skill_id)
            error_msg = f"[API] [{skill_id}] 未找到 API 端点配置"
            logger.log_error(error_msg)
            return {"code": 500, "message": error_msg, "data": {"records": [], "total": 0}}
        
        if not HAS_REQUESTS:
            if USE_MOCK:
                logger.log_warning("[API] requests 模块未安装，使用 Mock 数据")
                return self._get_mock_response(skill_id)
            error_msg = "[API] requests 模块未安装，请先安装: pip install requests"
            logger.log_error(error_msg)
            return {"code": 500, "message": error_msg, "data": {"records": [], "total": 0}}
        
        try:
            start_time = time.time()
            self._ensure_session()
            
            logger.log_info(f"[API] [{skill_id}] 请求开始: method={method}, url={url}, params_keys={list(params.keys())}, description={description}")
            
            resp = self._session.request(
                method, url,
                json=params,
                headers=self.config.auth_headers,
                timeout=30
            )
            duration = time.time() - start_time
            
            logger.log_info(f"[API] [{skill_id}] 请求完成: status_code={resp.status_code}, duration_ms={round(duration*1000, 2)}, url={url}")
            
            resp.raise_for_status()
            
            try:
                result = resp.json()
                data_size = len(json.dumps(result)) if result else 0
                self._success_count += 1
                
                logger.log_api_response(skill_id, resp.status_code, duration, True)
                logger.log_info(f"[API] [{skill_id}] 响应解析成功: code={result.get('code')}, data_total={result.get('data', {}).get('total', 0)}, response_size={data_size} bytes")
                
                return _normalize_pagination(skill_id, result)
            except ValueError as json_error:
                raw_content = resp.text[:500] if resp.text else ""
                error_msg = f"[API] [{skill_id}] 响应 JSON 解析失败: {str(json_error)}, raw_response={raw_content}"
                logger.log_error(error_msg)
                
                if USE_MOCK:
                    logger.log_warning(f"[API] [{skill_id}] JSON 解析失败，降级使用 Mock 数据")
                    return self._get_mock_response(skill_id)
                return {"code": 500, "message": "API 响应解析失败", "data": {"records": [], "total": 0}}
                
        except requests.exceptions.Timeout as e:
            duration = time.time() - start_time
            logger.log_api_response(skill_id, None, duration, False, "Timeout")
            logger.log_error(f"[API] [{skill_id}] 请求超时: {str(e)}, duration_ms={round(duration*1000, 2)}, url={url}")
            
            if USE_MOCK:
                logger.log_warning(f"[API] [{skill_id}] 请求超时，降级使用 Mock 数据")
                return self._get_mock_response(skill_id)
            return {"code": 504, "message": "API 请求超时", "data": {"records": [], "total": 0}}
            
        except requests.exceptions.SSLError as e:
            logger.log_api_response(skill_id, None, 0, False, "SSL Error")
            logger.log_error(f"[API] [{skill_id}] SSL 连接失败: {str(e)}, url={url}, 请检查网络环境或配置代理")
            
            if USE_MOCK:
                logger.log_warning(f"[API] [{skill_id}] SSL 连接失败，降级使用 Mock 数据")
                return self._get_mock_response(skill_id)
            return {"code": 500, "message": "SSL 连接失败", "data": {"records": [], "total": 0}}
            
        except requests.exceptions.ConnectionError as e:
            logger.log_api_response(skill_id, None, 0, False, "Connection Error")
            logger.log_error(f"[API] [{skill_id}] 网络连接失败: {str(e)}, url={url}, 请检查网络连通性或配置代理")
            
            if USE_MOCK:
                logger.log_warning(f"[API] [{skill_id}] 网络连接失败，降级使用 Mock 数据")
                return self._get_mock_response(skill_id)
            return {"code": 503, "message": "网络连接失败", "data": {"records": [], "total": 0}}
            
        except requests.exceptions.RequestException as e:
            duration = time.time() - start_time
            logger.log_api_response(skill_id, None, duration, False, str(e))
            logger.log_error(f"[API] [{skill_id}] 请求异常: {str(e)}, duration_ms={round(duration*1000, 2)}, url={url}")
            
            if USE_MOCK:
                logger.log_warning(f"[API] [{skill_id}] 请求异常，降级使用 Mock 数据")
                return self._get_mock_response(skill_id)
            return {"code": 500, "message": f"API 请求失败: {str(e)}", "data": {"records": [], "total": 0}}
            
        except Exception as e:
            logger.log_api_response(skill_id, None, 0, False, str(e))
            logger.log_error(f"[API] [{skill_id}] 未知异常: {str(e)}, url={url}")
            
            if USE_MOCK:
                logger.log_warning(f"[API] [{skill_id}] 未知异常，降级使用 Mock 数据")
                return self._get_mock_response(skill_id)
            return {"code": 500, "message": f"API 处理异常: {str(e)}", "data": {"records": [], "total": 0}}
    
    def query_server(self, **params) -> dict:
        return self._request('cmdb-server-query', params)
    
    def query_public_ip(self, **params) -> dict:
        return self._request('server-public-ip-query', params)
    
    def query_deployment(self, **params) -> dict:
        return self._request('project-deployment-query', params)
    
    def query_product(self, **params) -> dict:
        return self._request('product-query', params)
    
    def query_project_basis(self, **params) -> dict:
        return self._request('project-basis-query', params)


client = CMDBAPIClient()

# ============================================================================
# 标准 MCP 协议定义
# ============================================================================

MCP_TOOLS = [
    {
        "name": "cmdb_server_query",
        "description": "查询 CMDB 服务器信息，包括主机名、IP、机房、配置、状态等",
        "inputSchema": {
            "type": "object",
            "properties": {
                "hostName": {"type": "string", "description": "主机名（模糊匹配）"},
                "ip": {"type": "string", "description": "IP 地址"},
                "node": {"type": "string", "description": "机房名称"},
                "state": {"type": "string", "description": "状态：运行中/已关机/故障"},
                "serverType": {"type": "string", "description": "服务器类型：物理机/虚拟机"},
                "belong": {"type": "integer", "description": "所属业务线"},
                "xc": {"type": "string", "description": "线路类型"},
                "productName": {"type": "string", "description": "产品名称"},
                "projectName": {"type": "string", "description": "项目名称"},
                "currentPage": {"type": "integer", "description": "页码", "default": 1},
                "pageSize": {"type": "integer", "description": "每页条数", "default": 15}
            }
        }
    },
    {
        "name": "server_public_ip_query",
        "description": "查询服务器公网 IP 信息，包括公网IP、带宽、网络类型等",
        "inputSchema": {
            "type": "object",
            "properties": {
                "ip": {"type": "string", "description": "内网 IP 地址"},
                "hostName": {"type": "string", "description": "主机名（模糊匹配）"},
                "node": {"type": "string", "description": "机房名称"},
                "currentPage": {"type": "integer", "description": "页码", "default": 1},
                "pageSize": {"type": "integer", "description": "每页条数", "default": 40}
            }
        }
    },
    {
        "name": "project_deployment_query",
        "description": "查询项目部署记录，包括版本、环境、状态、部署人等",
        "inputSchema": {
            "type": "object",
            "properties": {
                "projectName": {"type": "string", "description": "项目名称（模糊匹配）"},
                "environment": {"type": "integer", "description": "环境：1=测试, 2=灰度, 3=生产, 4=研发"},
                "deploymentStatus": {"type": "integer", "description": "部署状态：0=成功, 1=失败, 2=进行中, 3=待部署"},
                "deployer": {"type": "string", "description": "部署人姓名"},
                "startTime": {"type": "string", "description": "开始时间，格式 YYYY-MM-DD"},
                "endTime": {"type": "string", "description": "结束时间，格式 YYYY-MM-DD"},
                "currentPage": {"type": "integer", "description": "页码", "default": 1},
                "pageSize": {"type": "integer", "description": "每页条数", "default": 40}
            }
        }
    },
    {
        "name": "product_query",
        "description": "查询产品信息，包括产品名称、负责人、所属部门等",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string", "description": "产品 ID"},
                "name": {"type": "string", "description": "产品名称（模糊匹配）"},
                "flag": {"type": "integer", "description": "启用标志：0=禁用, 1=启用"},
                "department": {"type": "string", "description": "所属部门"},
                "currentPage": {"type": "integer", "description": "页码", "default": 1},
                "pageSize": {"type": "integer", "description": "每页条数", "default": 40}
            }
        }
    },
    {
        "name": "project_basis_query",
        "description": "查询工程项目基础信息，包括代码仓库、父项目、所属组等",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string", "description": "项目 ID"},
                "name": {"type": "string", "description": "项目名称（模糊匹配）"},
                "productId": {"type": "string", "description": "产品 ID"},
                "productName": {"type": "string", "description": "产品名称（模糊匹配）"},
                "projectType": {"type": "string", "description": "项目类型"},
                "currentPage": {"type": "integer", "description": "页码", "default": 1},
                "pageSize": {"type": "integer", "description": "每页条数", "default": 40}
            }
        }
    }
]

MCP_CAPABILITIES = {
    "tools/list": {},
    "tools/call": {}
}


class MCPProtocolHandler:
    """标准 MCP 协议处理器"""
    
    def __init__(self):
        self.sse_clients = []
    
    def register_sse_client(self, client):
        """注册 SSE 客户端连接"""
        self.sse_clients.append(client)
        logger.log_info(f"SSE 客户端已注册，当前连接数: {len(self.sse_clients)}")
    
    def unregister_sse_client(self, client):
        """注销 SSE 客户端连接"""
        if client in self.sse_clients:
            self.sse_clients.remove(client)
            logger.log_info(f"SSE 客户端已注销，当前连接数: {len(self.sse_clients)}")
    
    def broadcast(self, message: dict):
        """广播消息到所有 SSE 客户端"""
        msg_str = json.dumps(message, ensure_ascii=False)
        for client in self.sse_clients:
            try:
                client.put(f"data: {msg_str}\n\n")
            except Exception as e:
                logger.log_error(f"广播消息失败: {e}")
                self.unregister_sse_client(client)
    
    def _build_response(self, request_id: int, result: Any = None, error: dict = None) -> dict:
        """构建标准 JSON-RPC 2.0 响应"""
        response = {
            "jsonrpc": "2.0",
            "id": request_id
        }
        if error is not None:
            response["error"] = error
        else:
            response["result"] = result
        return response
    
    def _build_error(self, code: int, message: str, data: Any = None) -> dict:
        """构建错误响应"""
        error = {"code": code, "message": message}
        if data is not None:
            error["data"] = data
        return error
    
    def _execute_tool(self, tool_name: str, arguments: dict) -> dict:
        """执行工具调用"""
        if tool_name == "cmdb_server_query":
            query_params = {k: v for k, v in arguments.items() if v is not None}
            result = client.query_server(**query_params)
        elif tool_name == "server_public_ip_query":
            query_params = {k: v for k, v in arguments.items() if v is not None}
            result = client.query_public_ip(**query_params)
        elif tool_name == "project_deployment_query":
            query_params = {k: v for k, v in arguments.items() if v is not None}
            if "deploymentStatus" in query_params:
                query_params["statusCode"] = query_params.pop("deploymentStatus")
            result = client.query_deployment(**query_params)
        elif tool_name == "product_query":
            query_params = {k: v for k, v in arguments.items() if v is not None}
            result = client.query_product(**query_params)
        elif tool_name == "project_basis_query":
            query_params = {k: v for k, v in arguments.items() if v is not None}
            result = client.query_project_basis(**query_params)
        else:
            raise ValueError(f"Unknown tool: {tool_name}")
        
        return result
    
    def handle_message(self, message: dict) -> Tuple[dict, bool]:
        """
        处理 MCP 协议消息
        返回 (response, should_broadcast)
        """
        jsonrpc = message.get("jsonrpc")
        request_id = message.get("id")
        method = message.get("method")
        params = message.get("params", {})
        
        if jsonrpc != "2.0":
            return self._build_response(
                request_id,
                error=self._build_error(-32600, "Invalid Request")
            ), False
        
        try:
            if method == "initialize":
                logger.log_mcp_request(method, request_id)
                response = self._build_response(request_id, {
                    "name": "cmdb-mcp-server",
                    "version": "2.0.0",
                    "capabilities": MCP_CAPABILITIES
                })
                logger.log_mcp_response(method, request_id, True)
                return response, True
            
            elif method == "tools/list":
                logger.log_mcp_request(method, request_id)
                response = self._build_response(request_id, {
                    "tools": MCP_TOOLS
                })
                logger.log_mcp_response(method, request_id, True)
                return response, True
            
            elif method == "tools/call":
                tool_name = params.get("name")
                arguments = params.get("arguments", {})
                logger.log_mcp_request(method, request_id, {"name": tool_name})
                
                if not tool_name:
                    logger.log_mcp_response(method, request_id, False, "tool name is required")
                    return self._build_response(
                        request_id,
                        error=self._build_error(-32602, "Invalid params: tool name is required")
                    ), True
                
                try:
                    start_time = time.time()
                    result = self._execute_tool(tool_name, arguments)
                    duration = time.time() - start_time
                    
                    logger.log_tool_call(tool_name, arguments, result, duration, success=True)
                    logger.log_mcp_response(method, request_id, True)
                    
                    response = self._build_response(request_id, {
                        "content": [
                            {
                                "type": "text",
                                "text": json.dumps(result, ensure_ascii=False)
                            }
                        ],
                        "isError": False
                    })
                    return response, True
                except ValueError as e:
                    logger.log_mcp_response(method, request_id, False, str(e))
                    return self._build_response(
                        request_id,
                        error=self._build_error(-32602, str(e))
                    ), True
                except Exception as e:
                    logger.log_tool_call(tool_name, arguments, {}, 0, success=False)
                    logger.log_mcp_response(method, request_id, False, str(e))
                    return self._build_response(
                        request_id,
                        error=self._build_error(-32603, f"Internal error: {str(e)}")
                    ), True
            
            else:
                return self._build_response(
                    request_id,
                    error=self._build_error(-32601, f"Method not found: {method}")
                ), False
        
        except Exception as e:
            logger.log_error(f"MCP 消息处理异常: {e}")
            return self._build_response(
                request_id,
                error=self._build_error(-32603, f"Internal error: {str(e)}")
            ), False


mcp_handler = MCPProtocolHandler()

# ============================================================================
# HTTP 服务层（包含标准 MCP 协议接口）
# ============================================================================

if HAS_FASTAPI:
    app = FastAPI(
        title="cmdb-mcp-server",
        description="企业 CMDB 运维数据综合查询服务 - 标准 MCP 协议实现",
        version="2.0.0"
    )
    
    class ServerQueryParams(BaseModel):
        hostName: Optional[str] = None
        ip: Optional[str] = None
        node: Optional[str] = None
        state: Optional[str] = None
        serverType: Optional[str] = None
        belong: Optional[int] = None
        xc: Optional[str] = None
        productName: Optional[str] = None
        projectName: Optional[str] = None
        currentPage: int = 1
        pageSize: int = 15
    
    class PublicIPQueryParams(BaseModel):
        ip: Optional[str] = None
        hostName: Optional[str] = None
        node: Optional[str] = None
        currentPage: int = 1
        pageSize: int = 40
    
    class DeploymentQueryParams(BaseModel):
        projectName: Optional[str] = None
        environment: Optional[int] = None
        deploymentStatus: Optional[int] = None
        deployer: Optional[str] = None
        startTime: Optional[str] = None
        endTime: Optional[str] = None
        currentPage: int = 1
        pageSize: int = 40
    
    class ProductQueryParams(BaseModel):
        id: Optional[str] = None
        name: Optional[str] = None
        flag: Optional[int] = None
        department: Optional[str] = None
        currentPage: int = 1
        pageSize: int = 40
    
    class ProjectBasisQueryParams(BaseModel):
        id: Optional[str] = None
        name: Optional[str] = None
        productId: Optional[str] = None
        productName: Optional[str] = None
        projectType: Optional[str] = None
        currentPage: int = 1
        pageSize: int = 40
    
    @app.get("/", tags=["基础"])
    def root():
        return {
            "service": "cmdb-mcp-server",
            "version": "2.0.0",
            "description": "企业 CMDB 运维数据综合查询服务 - 标准 MCP 协议实现",
            "mcp_protocol": "standard",
            "capabilities": ["tools/list", "tools/call"],
            "endpoints": {
                "mcp": {
                    "sse": "/sse",
                    "messages": "/messages"
                },
                "api": [
                    "/api/cmdb-server-query",
                    "/api/server-public-ip-query",
                    "/api/project-deployment-query",
                    "/api/product-query",
                    "/api/project-basis-query"
                ],
                "docs": "/docs",
                "health": "/health"
            },
            "tools": [tool["name"] for tool in MCP_TOOLS]
        }
    
    @app.get("/health", tags=["基础"])
    def health():
        return {
            "status": "healthy",
            "service": "cmdb-mcp-server",
            "version": "2.0.0",
            "mock_enabled": USE_MOCK,
            "protocol": "standard_mcp",
            "sse_clients": len(mcp_handler.sse_clients)
        }
    
    @app.get("/sse", tags=["MCP 协议"])
    async def sse_endpoint():
        """
        标准 MCP SSE 传输端点
        客户端通过此连接接收服务器推送的消息
        """
        from queue import Queue
        
        client_queue = Queue()
        mcp_handler.register_sse_client(client_queue)
        
        async def event_generator():
            try:
                while True:
                    message = client_queue.get()
                    yield message
            except asyncio.CancelledError:
                mcp_handler.unregister_sse_client(client_queue)
                logger.log_info("SSE 连接已关闭")
        
        return StreamingResponse(
            event_generator(),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "Access-Control-Allow-Origin": "*"
            }
        )
    
    @app.post("/messages", tags=["MCP 协议"])
    async def messages_endpoint(request: dict):
        """
        标准 MCP 消息端点（SSE 传输模式配套）
        客户端通过此端点发送 MCP 协议消息（initialize、tools/list、tools/call）
        """
        response, should_broadcast = mcp_handler.handle_message(request)
        
        if should_broadcast:
            mcp_handler.broadcast(response)
        
        return JSONResponse(content=response)
    
    @app.post("/mcp", tags=["MCP 协议"])
    async def mcp_endpoint(request: dict):
        """
        标准 MCP 单一入口端点（Streamable HTTP 传输模式）
        所有 MCP 协议消息通过此单一端点发送（initialize、tools/list、tools/call）
        符合 MCP 2025-03-26 规范
        """
        response, should_broadcast = mcp_handler.handle_message(request)
        
        if should_broadcast:
            mcp_handler.broadcast(response)
        
        return JSONResponse(content=response)
    
    @app.get("/tools", tags=["平台兼容"])
    def platform_tools_endpoint():
        """
        Agent 平台工具发现端点
        平台通过 GET /tools 获取工具列表
        """
        return JSONResponse(content={"tools": MCP_TOOLS})
    
    @app.post("/tools", tags=["平台兼容"])
    def platform_tools_endpoint_post(request: dict):
        """
        Agent 平台工具发现端点（POST 版本）
        平台通过 POST /tools 获取工具列表或调用工具
        """
        method = request.get("method", "")
        if method == "tools/list":
            request_id = request.get("id", 1)
            return JSONResponse(content={
                "jsonrpc": "2.0",
                "id": request_id,
                "result": {"tools": MCP_TOOLS}
            })
        elif method == "tools/call":
            request_id = request.get("id", 1)
            params = request.get("params", {})
            tool_name = params.get("name")
            arguments = params.get("arguments", {})
            try:
                result = mcp_handler._execute_tool(tool_name, arguments)
                return JSONResponse(content={
                    "jsonrpc": "2.0",
                    "id": request_id,
                    "result": {
                        "content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]
                    }
                })
            except Exception as e:
                return JSONResponse(content={
                    "jsonrpc": "2.0",
                    "id": request_id,
                    "error": {"code": -32603, "message": str(e)}
                })
        else:
            return JSONResponse(content={"tools": MCP_TOOLS})
    
    @app.post("/tools/list", tags=["平台兼容"])
    def platform_tools_list_endpoint(request: dict = None):
        """
        Agent 平台工具列表端点
        平台通过 POST /tools/list 获取工具列表
        """
        request_id = request.get("id", 1) if request else 1
        return JSONResponse(content={
            "jsonrpc": "2.0",
            "id": request_id,
            "result": {"tools": MCP_TOOLS}
        })
    
    @app.get("/tools/list", tags=["平台兼容"])
    def platform_tools_list_endpoint_get():
        """
        Agent 平台工具列表端点（GET 版本）
        """
        return JSONResponse(content={"tools": MCP_TOOLS})
    
    @app.post("/tools/call", tags=["平台兼容"])
    def platform_tools_call_endpoint(request: dict):
        """
        Agent 平台工具调用端点
        平台通过 POST /tools/call 调用工具
        """
        request_id = request.get("id", 1)
        params = request.get("params", {})
        tool_name = params.get("name")
        arguments = params.get("arguments", {})
        
        try:
            result = mcp_handler._execute_tool(tool_name, arguments)
            return JSONResponse(content={
                "jsonrpc": "2.0",
                "id": request_id,
                "result": {
                    "content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]
                }
            })
        except Exception as e:
            return JSONResponse(content={
                "jsonrpc": "2.0",
                "id": request_id,
                "error": {"code": -32603, "message": str(e)}
            })
    
    @app.post("/api/cmdb-server-query", tags=["兼容层 - 服务器查询"])
    def http_cmdb_server_query(params: ServerQueryParams):
        query_params = {
            "hostName": params.hostName,
            "ip": params.ip,
            "node": params.node,
            "state": params.state,
            "serverType": params.serverType,
            "belong": params.belong,
            "xc": params.xc,
            "productName": params.productName,
            "projectName": params.projectName,
            "currentPage": params.currentPage,
            "pageSize": params.pageSize
        }
        query_params = {k: v for k, v in query_params.items() if v is not None}
        result = client.query_server(**query_params)
        return JSONResponse(content=result)
    
    @app.post("/api/server-public-ip-query", tags=["兼容层 - 公网IP查询"])
    def http_server_public_ip_query(params: PublicIPQueryParams):
        query_params = {
            "ip": params.ip,
            "hostName": params.hostName,
            "node": params.node,
            "current": params.currentPage,
            "size": params.pageSize
        }
        query_params = {k: v for k, v in query_params.items() if v is not None}
        result = client.query_public_ip(**query_params)
        return JSONResponse(content=result)
    
    @app.post("/api/project-deployment-query", tags=["兼容层 - 部署查询"])
    def http_project_deployment_query(params: DeploymentQueryParams):
        query_params = {
            "projectName": params.projectName,
            "environment": params.environment,
            "statusCode": params.deploymentStatus,
            "deployer": params.deployer,
            "startTime": params.startTime,
            "endTime": params.endTime,
            "currentPage": params.currentPage,
            "pageSize": params.pageSize
        }
        query_params = {k: v for k, v in query_params.items() if v is not None}
        result = client.query_deployment(**query_params)
        return JSONResponse(content=result)
    
    @app.post("/api/product-query", tags=["兼容层 - 产品查询"])
    def http_product_query(params: ProductQueryParams):
        query_params = {
            "id": params.id,
            "name": params.name,
            "flag": params.flag,
            "department": params.department,
            "current": params.currentPage,
            "size": params.pageSize
        }
        query_params = {k: v for k, v in query_params.items() if v is not None}
        result = client.query_product(**query_params)
        return JSONResponse(content=result)
    
    @app.post("/api/project-basis-query", tags=["兼容层 - 项目查询"])
    def http_project_basis_query(params: ProjectBasisQueryParams):
        query_params = {
            "id": params.id,
            "name": params.name,
            "productId": params.productId,
            "productName": params.productName,
            "projectType": params.projectType,
            "current": params.currentPage,
            "size": params.pageSize
        }
        query_params = {k: v for k, v in query_params.items() if v is not None}
        result = client.query_project_basis(**query_params)
        return JSONResponse(content=result)


def run_http(host: str, port: int):
    """运行 HTTP 服务器（标准 MCP 协议 + 兼容层）"""
    if not HAS_FASTAPI:
        logger.log_error("未安装 fastapi 模块，请先安装: pip install fastapi uvicorn")
        sys.exit(1)
    
    logger.log_info("🚀 启动标准 MCP HTTP 服务器...")
    logger.log_info(f"📍 监听地址: http://{host}:{port}")
    logger.log_info(f"📖 API 文档: http://{host}:{port}/docs")
    logger.log_info(f"🔌 MCP SSE: http://{host}:{port}/sse")
    logger.log_info(f"📤 MCP 消息: http://{host}:{port}/messages")
    logger.log_info(f"✅ 注册工具: {[tool['name'] for tool in MCP_TOOLS]}")
    
    # 直接使用 uvicorn.run，避免 get_event_loop 在 Python 3.12+ 的兼容性问题
    uvicorn.run(app, host=host, port=port, log_level="info")


def main():
    parser = argparse.ArgumentParser(description="cmdb-mcp-server - CMDB 运维数据 MCP 服务器")
    parser.add_argument("--transport", default="http", choices=["http"], help="传输模式")
    parser.add_argument("--host", default="0.0.0.0", help="监听地址")
    parser.add_argument("--port", type=int, default=8061, help="监听端口")
    
    args = parser.parse_args()
    
    if args.transport == "http":
        run_http(args.host, args.port)


if __name__ == "__main__":
    main()
