#!/usr/bin/env python3
"""
Apollo 配置查询 MCP 服务器
为 Apollo 配置中心提供标准化的 MCP 工具调用接口

遵循标准 MCP 协议规范：
- Streamable HTTP 传输模式
- JSON-RPC 2.0 协议格式
- initialize/initialized 握手流程
- tools/list 和 tools/call 方法

启动方式：
    python3 scripts/mcp_server.py --port 8062
    或
    ./start.sh

文档：http://localhost:8062/docs
"""

import json
import os
import sys
import argparse
import time
from typing import Optional, Dict, Any, List, Tuple
from datetime import datetime

_script_dir = os.path.dirname(os.path.abspath(__file__))
_root_dir = os.path.dirname(_script_dir)
_config_dir = os.path.join(_root_dir, 'config')
_references_dir = os.path.join(_root_dir, 'mock')

USE_MOCK = os.environ.get('MCP_USE_MOCK', 'false').lower() == 'true'
if USE_MOCK:
    print("[WARN] Mock 数据模式已启用")

try:
    import requests
    HAS_REQUESTS = True
except ImportError:
    HAS_REQUESTS = False
    print("[ERROR] 未安装 requests 模块，请先安装: pip install requests")

try:
    from fastapi import FastAPI, HTTPException
    from fastapi.responses import JSONResponse
    from pydantic import BaseModel
    import uvicorn
    HAS_FASTAPI = True
except ImportError:
    HAS_FASTAPI = False


# ============================================================================
# 日志配置
# ============================================================================
import logging

_log_format = "%(asctime)s | %(levelname)-8s | %(name)s | %(message)s"
_log_level = os.environ.get('LOG_LEVEL', 'INFO').upper()

logging.basicConfig(
    level=getattr(logging, _log_level, logging.INFO),
    format=_log_format,
    datefmt="%Y-%m-%d %H:%M:%S"
)
logger = logging.getLogger("apollo-mcp")

# 同时输出到控制台和文件
_log_dir = os.environ.get('LOG_DIR', os.path.join(_root_dir, 'logs'))
os.makedirs(_log_dir, exist_ok=True)
_log_file = os.path.join(_log_dir, 'apollo-mcp.log')
_file_handler = logging.FileHandler(_log_file)
_file_handler.setFormatter(logging.Formatter(_log_format))
logger.addHandler(_file_handler)


class ApolloConfig:
    def __init__(self):
        self.openapi_base = "http://localhost:8070"
        self.openapi_token = ""
        self.endpoints = {}
        self.current_env = os.environ.get('APOLLO_ENV', '')  # 先留空，load_config 中从配置文件补充
        self.load_config()
        self.load_auth()
    
    def load_config(self):
        config_path = os.path.join(_config_dir, 'api_endpoints.json')
        if os.path.exists(config_path):
            with open(config_path, 'r', encoding='utf-8') as f:
                data = json.load(f)
                
            # 1. 优先使用环境变量（生产部署方式），配置查询和应用列表统一走 OpenAPI（8070）
            env_openapi_host = os.environ.get('APOLLO_OPENAPI_HOST')
            if env_openapi_host:
                self.openapi_base = env_openapi_host
                logger.info(f"环境变量 APOLLO_OPENAPI_HOST 覆盖: {self.openapi_base}")
            
            # 2. 如果环境变量没设，从配置文件读取对应环境
            if not env_openapi_host:
                environments = data.get('environments', {})
                # 如果 APOLLO_ENV 未设，使用配置文件的 default_env
                if not self.current_env:
                    self.current_env = data.get('default_env', 'PRO')
                    logger.info(f"未设置 APOLLO_ENV，使用默认环境: {self.current_env}")
                env_key = self.current_env
                if env_key in environments:
                    env_config = environments[env_key]
                    self.openapi_base = env_config.get('openapi_service', self.openapi_base)
                    logger.info(f"加载 {env_key} 环境配置: OpenAPI={self.openapi_base}")
            
            # 3. 兼容旧格式（base_url）
            if not env_openapi_host and not data.get('environments'):
                old_base = data.get('base_url')
                if old_base:
                    self.openapi_base = old_base.replace(':8080', ':8070')
            
            # 4. 加载 endpoints 模板
            self.endpoints = data.get('endpoints', {})
            
            # 5. 构建完整 URL（全部基于 OpenAPI 统一地址）
            for endpoint in self.endpoints.values():
                if 'path' in endpoint:
                    endpoint['full_url'] = f"{self.openapi_base}{endpoint['path']}"
            
            logger.info(f"Apollo 配置加载完成 [{self.current_env}]: OpenAPI={self.openapi_base}")
    
    def load_auth(self):
        auth_path = os.path.join(_config_dir, 'auth.json')
        
        # 优先使用环境变量
        env_token = os.environ.get('APOLLO_OPENAPI_TOKEN')
        if env_token:
            self.openapi_token = env_token
            logger.info("从环境变量加载 Apollo OpenAPI Token")
            return
        
        if os.path.exists(auth_path):
            try:
                with open(auth_path, 'r', encoding='utf-8') as f:
                    data = json.load(f)
                    self.openapi_token = data.get('openapi_token', '')
                    if self.openapi_token:
                        logger.info("从配置文件加载 Apollo OpenAPI Token")
                    else:
                        logger.warning("Apollo OpenAPI Token 未配置（配置查询和应用列表将失败）")
            except Exception as e:
                logger.warning(f"auth.json 读取失败，忽略配置文件: {e}")


class ApolloAPIClient:
    def __init__(self):
        self.config = ApolloConfig()
        self._session = None
        self._request_count = 0
        self._success_count = 0
    
    def _ensure_session(self):
        if HAS_REQUESTS and self._session is None:
            self._session = requests.Session()
            proxy_env = os.environ.get('HTTP_PROXY') or os.environ.get('http_proxy')
            if proxy_env:
                self._session.proxies = {'http': proxy_env, 'https': proxy_env}
                print(f"[INFO] HTTP 会话已配置代理: {proxy_env}")
    
    def _load_mock_data(self) -> dict:
        mock_path = os.path.join(_references_dir, 'mock_responses.json')
        if os.path.exists(mock_path):
            with open(mock_path, 'r', encoding='utf-8') as f:
                return json.load(f)
        return {}
    
    def _get_mock_response(self, tool_id: str, **kwargs) -> dict:
        mock_data = self._load_mock_data()
        appId = kwargs.get('appId', '')
        namespaceName = kwargs.get('namespaceName', 'application')
        key = kwargs.get('key')
        
        if tool_id == 'apollo-app-list':
            apps = mock_data.get('_apps', [])
            print(f"[MOCK] [apollo-app-list] 返回 {len(apps)} 个应用")
            return {"code": 200, "message": "success", "data": apps}
        
        elif tool_id == 'apollo-config-query':
            configs_data = mock_data.get('_configs', {})
            app_configs = configs_data.get(appId, {})
            namespace_configs = app_configs.get(namespaceName, {})
            
            if not app_configs:
                print(f"[MOCK] [apollo-config-query] 应用不存在: {appId}")
                return {"code": 404, "message": f"应用 {appId} 不存在", "data": {}}
            
            if not namespace_configs:
                available_ns = list(app_configs.keys())
                print(f"[MOCK] [apollo-config-query] Namespace 不存在: {namespaceName}, 可用: {available_ns}")
                return {"code": 404, "message": f"Namespace {namespaceName} 不存在，可用: {', '.join(available_ns)}", "data": {}}
            
            configurations = dict(namespace_configs)
            
            if key:
                filtered = {k: v for k, v in configurations.items() if key.lower() in k.lower()}
                total_count = len(configurations)
                configurations = filtered
                print(f"[MOCK] [apollo-config-query] appId={appId}, namespace={namespaceName}, key={key}, total={total_count}, filtered={len(filtered)}")
            else:
                total_count = len(configurations)
                print(f"[MOCK] [apollo-config-query] appId={appId}, namespace={namespaceName}, configs={total_count}")
            
            result = {
                "code": 200,
                "message": "success",
                "data": {
                    "appId": appId,
                    "cluster": kwargs.get('clusterName', 'default'),
                    "namespaceName": namespaceName,
                    "configurations": configurations,
                    "releaseKey": f"mock-{appId}-{namespaceName}"
                }
            }
            if key:
                result["data"]["_filtered_by"] = key
                result["data"]["_total_count"] = total_count
                result["data"]["_filtered_count"] = len(configurations)
            return result
        
        return {"code": 500, "message": f"Mock 数据不存在: {tool_id}", "data": {}}
    
    def _request(self, tool_id: str, url: str, params: dict = None, headers: dict = None, **mock_kwargs) -> dict:
        self._request_count += 1
        
        if USE_MOCK:
            logger.debug(f"[{tool_id}] Mock 模式启用，返回 Mock 数据")
            return self._get_mock_response(tool_id, **mock_kwargs)
        
        if not HAS_REQUESTS:
            logger.error(f"[{tool_id}] requests 模块未安装")
            return {"code": 500, "message": "requests 模块未安装", "data": {}}
        
        try:
            self._ensure_session()
            
            logger.debug(f"[{tool_id}] 请求: GET {url}")
            start_time = time.time()
            
            resp = self._session.get(url, params=params, headers=headers, timeout=30)
            duration = time.time() - start_time
            
            logger.debug(f"[{tool_id}] 响应: status={resp.status_code}, duration={round(duration*1000)}ms")
            resp.raise_for_status()
            
            try:
                result = resp.json()
                self._success_count += 1
                logger.info(f"[{tool_id}] 请求成功: {round(duration*1000)}ms")
                # 统一包装为 {code, message, data} 格式
                if isinstance(result, list):
                    return {"code": 200, "message": "success", "data": result}
                elif isinstance(result, dict) and 'code' not in result:
                    return {"code": 200, "message": "success", "data": result}
                return result
            except ValueError:
                text = resp.text
                if text.strip().startswith('{'):
                    try:
                        return {"code": 200, "message": "success", "data": json.loads(text)}
                    except:
                        pass
                return {"code": 200, "message": "success", "data": {"raw": text}}
                
        except requests.exceptions.Timeout as e:
            logger.warning(f"[{tool_id}] 请求超时: {str(e)}")
            return {"code": 504, "message": "API 请求超时", "data": {}}
            
        except requests.exceptions.ConnectionError as e:
            logger.error(f"[{tool_id}] 连接失败: {str(e)}")
            return {"code": 503, "message": "网络连接失败", "data": {}}
            
        except requests.exceptions.RequestException as e:
            # 提取 HTTP 状态码，提供更有针对性的错误信息
            status_code = getattr(getattr(e, 'response', None), 'status_code', None)
            if status_code == 400:
                logger.warning(f"[{tool_id}] 请求参数错误: {str(e)}")
                return {"code": 400, "message": f"请求参数错误: {str(e)}", "data": {}}
            elif status_code == 401:
                logger.warning(f"[{tool_id}] 认证失败，请检查 OpenAPI Token")
                return {"code": 401, "message": "OpenAPI Token 无效或未配置", "data": {}}
            elif status_code == 404:
                logger.warning(f"[{tool_id}] 资源不存在: {str(e)}")
                return {"code": 404, "message": "请求的资源不存在（应用/Namespace/环境可能不正确）", "data": {}}
            logger.error(f"[{tool_id}] 请求异常: {str(e)}")
            return {"code": 500, "message": f"API 请求失败: {str(e)}", "data": {}}
    
    def query_config(self, appId: str, env: str = "PRO", clusterName: str = "default", namespaceName: str = "application", key: str = None) -> dict:
        """
        查询 Apollo 配置项 (使用 OpenAPI，需要 Token)
        GET /openapi/v1/envs/{env}/apps/{appId}/clusters/{clusterName}/namespaces/{namespaceName}
        """
        url = f"{self.config.openapi_base}/openapi/v1/envs/{env}/apps/{appId}/clusters/{clusterName}/namespaces/{namespaceName}"
        
        headers = {}
        if self.config.openapi_token:
            headers['Authorization'] = self.config.openapi_token
        
        logger.info(f"查询配置: appId={appId}, env={env}, namespace={namespaceName}, key={key or '(全部)'}")
        
        result = self._request('apollo-config-query', url, headers=headers,
                                appId=appId, env=env,
                                clusterName=clusterName, namespaceName=namespaceName, key=key)
        
        # 将 OpenAPI 返回的 items 数组转换为 configurations 字典（保持输出格式稳定）
        if result.get('code') == 200:
            data = result.get('data', {})
            if isinstance(data, dict) and 'items' in data:
                data['configurations'] = {
                    item['key']: item['value']
                    for item in data['items'] if isinstance(item, dict) and 'key' in item
                }
        
        # 如果有 key 参数，在非 Mock 模式下做本地过滤（Mock 模式已在数据层处理）
        if key and result.get('code') == 200 and not USE_MOCK:
            data = result.get('data', {})
            if isinstance(data, dict) and 'configurations' in data:
                configurations = data['configurations']
                total_count = len(configurations)
                filtered = {k: v for k, v in configurations.items() if key.lower() in k.lower()}
                data['configurations'] = filtered
                data['_filtered_by'] = key
                data['_total_count'] = total_count
                data['_filtered_count'] = len(filtered)
                result['data'] = data
        
        return result
    
    def query_apps(self) -> dict:
        """
        查询 Apollo 应用列表 (使用 OpenAPI，需要 Token)
        GET /openapi/v1/apps
        """
        endpoint = self.config.endpoints.get('apollo-app-list', {})
        url = endpoint.get('full_url', f"{self.config.openapi_base}/openapi/v1/apps")
        
        headers = {}
        if self.config.openapi_token:
            headers['Authorization'] = self.config.openapi_token
        
        return self._request('apollo-app-list', url, headers=headers)


client = ApolloAPIClient()

# ============================================================================
# 标准 MCP 工具定义
# ============================================================================

MCP_TOOLS = [
    {
        "name": "apollo_config_query",
        "description": "查询 Apollo 配置中心的配置项列表。支持按应用ID、环境、集群、Namespace 和 Key 关键词过滤。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "appId": {"type": "string", "description": "应用ID，如 'rule-engine'。必填参数"},
                "env": {"type": "string", "description": "环境，PRO=生产, DEV=开发, FAT=测试, UAT=预发", "enum": ["PRO", "DEV", "FAT", "UAT"]},
                "clusterName": {"type": "string", "description": "集群名称，默认 'default'"},
                "namespaceName": {"type": "string", "description": "Namespace 名称，默认 'application'"},
                "key": {"type": "string", "description": "配置项 Key 关键词（模糊匹配），用于过滤配置项"}
            },
            "required": ["appId"]
        }
    },
    {
        "name": "apollo_app_list",
        "description": "获取 Apollo 配置中心中所有可用的应用列表（AppId、应用名称、所属部门）。",
        "inputSchema": {
            "type": "object",
            "properties": {}
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
        self.sse_clients.append(client)
        print(f"[MCP] SSE 客户端已注册，当前连接数: {len(self.sse_clients)}")
    
    def unregister_sse_client(self, client):
        if client in self.sse_clients:
            self.sse_clients.remove(client)
            print(f"[MCP] SSE 客户端已注销，当前连接数: {len(self.sse_clients)}")
    
    def broadcast(self, message: dict):
        msg_str = json.dumps(message, ensure_ascii=False)
        for client in self.sse_clients:
            try:
                client.put(f"data: {msg_str}\n\n")
            except Exception as e:
                print(f"[ERROR] 广播消息失败: {e}")
                self.unregister_sse_client(client)
    
    def _build_response(self, request_id: int, result: Any = None, error: dict = None) -> dict:
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
        error = {"code": code, "message": message}
        if data is not None:
            error["data"] = data
        return error
    
    def _execute_tool(self, tool_name: str, arguments: dict) -> dict:
        if tool_name == "apollo_config_query":
            appId = arguments.get("appId")
            if not appId:
                raise ValueError("appId 是必填参数，请指定应用ID")
            return client.query_config(
                appId=appId,
                env=arguments.get("env", "PRO"),
                clusterName=arguments.get("clusterName", "default"),
                namespaceName=arguments.get("namespaceName", "application"),
                key=arguments.get("key")
            )
        elif tool_name == "apollo_app_list":
            return client.query_apps()
        else:
            raise ValueError(f"Unknown tool: {tool_name}")
    
    def handle_message(self, message: dict) -> Tuple[dict, bool]:
        jsonrpc = message.get("jsonrpc")
        request_id = message.get("id")
        method = message.get("method")
        params = message.get("params", {})
        
        if jsonrpc != "2.0":
            return self._build_response(request_id, error=self._build_error(-32600, "Invalid Request")), False
        
        try:
            if method == "initialize":
                print(f"[MCP] initialize")
                response = self._build_response(request_id, {
                    "name": "apollo-config-query-mcp",
                    "version": "2.0.0",
                    "capabilities": MCP_CAPABILITIES
                })
                return response, True
            
            elif method == "tools/list":
                print(f"[MCP] tools/list")
                response = self._build_response(request_id, {"tools": MCP_TOOLS})
                return response, True
            
            elif method == "tools/call":
                tool_name = params.get("name")
                arguments = params.get("arguments", {})
                print(f"[MCP] tools/call: {tool_name}")
                
                if not tool_name:
                    return self._build_response(request_id, error=self._build_error(-32602, "tool name is required")), True
                
                try:
                    start_time = time.time()
                    result = self._execute_tool(tool_name, arguments)
                    duration = time.time() - start_time
                    print(f"[MCP] tool [{tool_name}] executed in {round(duration*1000)}ms")
                    
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
                    print(f"[MCP] tool [{tool_name}] validation error: {str(e)}")
                    return self._build_response(request_id, error=self._build_error(-32602, str(e))), True
                except Exception as e:
                    print(f"[MCP] tool [{tool_name}] error: {str(e)}")
                    return self._build_response(request_id, error=self._build_error(-32603, f"Internal error: {str(e)}")), True
            
            else:
                return self._build_response(request_id, error=self._build_error(-32601, f"Method not found: {method}")), False
        
        except Exception as e:
            print(f"[ERROR] MCP 消息处理异常: {e}")
            return self._build_response(request_id, error=self._build_error(-32603, f"Internal error: {str(e)}")), False


mcp_handler = MCPProtocolHandler()

# ============================================================================
# HTTP 服务层
# ============================================================================

if HAS_FASTAPI:
    app = FastAPI(
        title="apollo-config-query-mcp",
        description="Apollo 配置查询 MCP 服务 - 为 Apollo 配置中心提供标准化工具调用接口",
        version="2.0.0"
    )
    
    class ApolloConfigQueryParams(BaseModel):
        appId: str
        env: Optional[str] = "PRO"
        clusterName: Optional[str] = "default"
        namespaceName: Optional[str] = "application"
        key: Optional[str] = None
    
    @app.get("/", tags=["基础"])
    def root():
        return {
            "service": "apollo-config-query-mcp",
            "version": "2.0.0",
            "description": "Apollo 配置查询 MCP 服务",
            "mcp_protocol": "standard",
            "capabilities": ["tools/list", "tools/call"],
            "tools": [tool["name"] for tool in MCP_TOOLS],
            "mock_enabled": USE_MOCK
        }
    
    @app.get("/health", tags=["基础"])
    def health():
        return {
            "status": "healthy",
            "service": "apollo-config-query-mcp",
            "version": "2.0.0",
            "environment": client.config.current_env,
            "mock_enabled": USE_MOCK,
            "openapi_base": client.config.openapi_base,
            "has_openapi_token": bool(client.config.openapi_token),
            "api_stats": {
                "total_requests": client._request_count,
                "successful_requests": client._success_count,
                "uptime_start": datetime.now().isoformat(),
            },
            "tools": [
                {"name": t["name"], "description": t["description"]}
                for t in MCP_TOOLS
            ]
        }
    
    @app.post("/mcp", tags=["MCP 协议"])
    async def mcp_endpoint(request: dict):
        response, should_broadcast = mcp_handler.handle_message(request)
        if should_broadcast:
            mcp_handler.broadcast(response)
        return JSONResponse(content=response)
    
    @app.get("/tools", tags=["平台兼容"])
    def platform_tools_endpoint():
        return JSONResponse(content={"tools": MCP_TOOLS})
    
    @app.get("/tools/list", tags=["平台兼容"])
    def platform_tools_list_endpoint():
        return JSONResponse(content={"tools": MCP_TOOLS})
    
    @app.post("/tools/call", tags=["平台兼容"])
    def platform_tools_call(request: dict):
        params = request.get("params", {})
        tool_name = params.get("name")
        arguments = params.get("arguments", {})
        try:
            result = mcp_handler._execute_tool(tool_name, arguments)
            return JSONResponse(content={
                "jsonrpc": "2.0",
                "id": request.get("id", 1),
                "result": {
                    "content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]
                }
            })
        except Exception as e:
            return JSONResponse(content={
                "jsonrpc": "2.0",
                "id": request.get("id", 1),
                "error": {"code": -32603, "message": str(e)}
            })
    
    @app.post("/api/config", tags=["兼容层 - 配置查询"])
    def http_config_query(params: ApolloConfigQueryParams):
        result = client.query_config(
            appId=params.appId,
            env=params.env,
            clusterName=params.clusterName,
            namespaceName=params.namespaceName,
            key=params.key
        )
        return JSONResponse(content=result)
    
    @app.get("/api/apps", tags=["兼容层 - 应用列表"])
    def http_apps_list():
        result = client.query_apps()
        return JSONResponse(content=result)


def run_http(host: str, port: int):
    if not HAS_FASTAPI:
        logger.error("未安装 fastapi 模块，请先安装: pip install fastapi uvicorn")
        sys.exit(1)
    
    logger.info(f"🚀 Apollo MCP 服务器启动中...")
    logger.info(f"📍 地址: http://{host}:{port}")
    logger.info(f"📖 API 文档: http://{host}:{port}/docs")
    logger.info(f"🔌 MCP 端点: http://{host}:{port}/mcp")
    logger.info(f"✅ 注册工具: {[t['name'] for t in MCP_TOOLS]}")
    logger.info(f"🎯 Mock 模式: {'开启' if USE_MOCK else '关闭'}")
    
    uvicorn.run(app, host=host, port=port, log_level="info", access_log=True)


def main():
    parser = argparse.ArgumentParser(description="Apollo 配置查询 MCP 服务器")
    parser.add_argument("--host", default="0.0.0.0", help="监听地址")
    parser.add_argument("--port", type=int, default=8062, help="监听端口")
    
    args = parser.parse_args()
    run_http(args.host, args.port)


if __name__ == "__main__":
    main()
