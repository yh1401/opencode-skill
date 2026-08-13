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
import threading
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


class ConfigClient:
    """通过第三方「Apollo Host 信息查询接口」获取 Apollo 连接配置（地址 + Token）的客户端

    完整链路：skill → MCP → 第三方接口（返回 Apollo 地址+加密 Token）→ MCP 解密组装 → Apollo → 返回

    设计目标：
        - Token / 服务地址由第三方接口集中管理，MCP 通过接口获取（不再在本地暴露明文 Token）
        - 启动时拉取一次并缓存，之后定时刷新
        - 第三方接口不可用时回退到本地兜底配置（config/api_endpoints.json + auth.json），保证可用性

    环境变量：
        APOLLO_HOST_API_BASE      第三方接口地址，默认 https://easyops.tech.ctseelink.cn
        APOLLO_HOST_SESSION_ID    第三方鉴权 sessionId（32位），同时是 token 解密密钥
        APOLLO_HOST_NAME          可选，按服务名称模糊过滤第三方返回的 host（不填取第一条）
        APOLLO_HOST_PORT_OVERRIDE 可选，强制替换 host 端口（如第三方返回 8154，OpenAPI 实际 8070 时配置）
        CONFIG_SERVICE_REFRESH_SEC 刷新间隔（秒），默认 60
    """

    _DEFAULT_API_BASE = "https://easyops.tech.ctseelink.cn"
    _DEFAULT_SESSION_ID = "e5e27a7d1805758400287ae86741f889"

    def __init__(self, refresh_sec: int = None):
        # 注意：docker-compose 会以空字符串注入环境变量，需用 or 回退内置默认值
        self.api_base = (os.environ.get('APOLLO_HOST_API_BASE') or self._DEFAULT_API_BASE).rstrip('/')
        self.session_id = os.environ.get('APOLLO_HOST_SESSION_ID') or self._DEFAULT_SESSION_ID
        self.refresh_sec = refresh_sec or int(os.environ.get('CONFIG_SERVICE_REFRESH_SEC', '60'))
        self._cache: Optional[dict] = None          # 最近一次成功拉取的完整配置
        self._last_fetch_ts: float = 0.0
        self.last_error: str = ""                   # 最近一次拉取失败原因（用于健康检查排查）
        self._lock = threading.Lock()

    @staticmethod
    def decrypt_token(encrypted_token: str, session_id: str) -> str:
        """解密 token：Base64 解码后与 sessionId 循环逐字节 XOR（与服务端加密逻辑一致）"""
        if not encrypted_token or not session_id:
            return ""
        try:
            import base64
            decoded = base64.b64decode(encrypted_token)
            return ''.join(
                chr(decoded[i] ^ ord(session_id[i % len(session_id)]))
                for i in range(len(decoded))
            )
        except Exception as e:
            logger.warning(f"token 解密失败: {e}")
            return ""

    def _fetch(self) -> dict:
        """调用第三方接口获取 Apollo Host 信息，组装成与本地模板同构的配置结构"""
        # 路径可配置：线上 /thirdApi/getApolloHostInfo，本地 mock 为 /api/getApolloHostInfo
        host_path = (os.environ.get('APOLLO_HOST_PATH') or '/thirdApi/getApolloHostInfo').strip()
        url = f"{self.api_base}{host_path}"
        logger.info(f"[ConfigClient] 从第三方接口拉取 Apollo Host 信息: {url}")

        # 鉴权：Cookie sessionId（既是认证凭证，也是 token 解密密钥）
        logger.info(f"[ConfigClient] 请求 Cookie: sessionId={self.session_id[:8]}...(脱敏)")

        resp = requests.get(
            url,
            params={"paginator": False, "pageIndex": 1, "pageSize": 100},
            headers={"Content-Type": "application/json"},
            cookies={"sessionId": self.session_id},
            timeout=10,
            verify=False
        )
        resp.raise_for_status()
        data = resp.json()

        if data.get('code') != 'success':
            raise RuntimeError(f"第三方接口返回失败: {data.get('message', '未知错误')}")
        host_list = data.get('list', [])
        logger.info(f"[ConfigClient] 第三方接口返回 {len(host_list)} 条 Apollo Host 记录")
        if not host_list:
            raise RuntimeError("第三方接口返回的 Apollo Host 列表为空")

        # 选择目标记录：APOLLO_HOST_NAME 模糊过滤，否则取第一条
        name_filter = os.environ.get('APOLLO_HOST_NAME', '')
        target = None
        if name_filter:
            for item in host_list:
                if name_filter.lower() in item.get('name', '').lower():
                    target = item
                    break
        if target is None:
            target = host_list[0]

        host = target.get('host', '')
        raw_token = self.decrypt_token(target.get('token', ''), self.session_id)

        # 可选：端口覆盖（第三方返回端口与 OpenAPI 实际端口不一致时）
        port_override = os.environ.get('APOLLO_HOST_PORT_OVERRIDE', '')
        if port_override and host:
            from urllib.parse import urlsplit, urlunsplit
            parts = urlsplit(host)
            host = urlunsplit((parts.scheme, f"{parts.hostname}:{port_override}", parts.path, parts.query, parts.fragment))
            logger.info(f"[ConfigClient] 已覆盖 host 端口为 {port_override}: {host}")

        # 以本地模板为基底组装（environments/endpoints/timeout 等静态结构）
        template = self._load_local_template()
        env_key = template.get('default_env', 'PRO')
        environments = dict(template.get('environments', {}))
        environments[env_key] = {
            **environments.get(env_key, {}),
            'openapi_service': host,
            'label': target.get('name', env_key)
        }
        configs = dict(template)
        configs['environments'] = environments
        configs['openapi_token'] = raw_token

        logger.info(f"[ConfigClient] 组装远程配置: 环境={env_key}, OpenAPI={host}, 有Token={bool(raw_token)}, 来源={target.get('name', '')}")
        return configs

    def _load_local_template(self) -> dict:
        """加载本地模板 api_endpoints.json（作为组装远程配置的基底）"""
        config_path = os.path.join(_config_dir, 'api_endpoints.json')
        if os.path.exists(config_path):
            with open(config_path, 'r', encoding='utf-8') as f:
                return json.load(f)
        return {}

    def get_configs(self, force: bool = False) -> dict:
        """获取配置（完整字典，含 environments/token/endpoints 等）。

        优先级：缓存（未过期）> 定时刷新 > 失败回退缓存 > 失败无缓存返回空。
        """
        now = time.time()
        with self._lock:
            # 缓存仍有效：直接返回
            if not force and self._cache and (now - self._last_fetch_ts) < self.refresh_sec:
                return self._cache

            try:
                data = self._fetch()
                if data:
                    self._cache = data
                    self._last_fetch_ts = time.time()
                    self.last_error = ""
                    envs = list(data.get('environments', {}).keys())
                    logger.info(f"[ConfigClient] 配置拉取成功: 环境={envs}, default_env={data.get('default_env')}, 有Token={bool(data.get('openapi_token'))}")
                else:
                    self.last_error = "第三方接口返回空配置"
                    logger.warning("[ConfigClient] 第三方接口返回空配置")
            except Exception as e:
                self.last_error = str(e)
                logger.warning(f"[ConfigClient] 第三方接口不可达({self.api_base}): {e}")

            # 有缓存则回退缓存
            if self._cache:
                logger.info("[ConfigClient] 使用上次成功拉取的缓存配置")
                return self._cache

        return {}


class ApolloConfig:
    def __init__(self):
        self.openapi_base = "http://localhost:8070"
        self.openapi_token = ""
        self.endpoints = {}
        self.current_env = os.environ.get('APOLLO_ENV', '')  # 先留空，load_config 中从配置文件补充
        self.timeout = 30
        self.max_retries = 2
        self.retry_delay = 1
        self.config_client = ConfigClient()
        self.load_config()
        self.load_auth()
    
    def _apply_remote_config(self, data: dict):
        """应用配置服务拉取的配置（地址、环境、endpoints、超时等）"""
        self.endpoints = data.get('endpoints', {})
        self.timeout = data.get('timeout', 30)
        self.max_retries = data.get('max_retries', 2)
        self.retry_delay = data.get('retry_delay', 1)

        # 环境与地址：环境变量优先，其次配置服务
        env_openapi_host = os.environ.get('APOLLO_OPENAPI_HOST')
        if env_openapi_host:
            self.openapi_base = env_openapi_host
            logger.info(f"环境变量 APOLLO_OPENAPI_HOST 覆盖: {self.openapi_base}")
        else:
            environments = data.get('environments', {})
            if not self.current_env:
                self.current_env = data.get('default_env', 'PRO')
                logger.info(f"未设置 APOLLO_ENV，使用配置服务默认环境: {self.current_env}")
            env_key = self.current_env
            if env_key in environments:
                env_config = environments[env_key]
                self.openapi_base = env_config.get('openapi_service', self.openapi_base)
                logger.info(f"[ConfigService] 加载 {env_key} 环境配置: OpenAPI={self.openapi_base}")

        # 构建完整 URL
        for endpoint in self.endpoints.values():
            if 'path' in endpoint:
                endpoint['full_url'] = f"{self.openapi_base}{endpoint['path']}"

        logger.info(f"Apollo 配置加载完成（来自配置服务）[{self.current_env}]: OpenAPI={self.openapi_base}")

    def load_config(self):
        """加载 Apollo 连接配置：优先配置服务拉取，失败回退本地 api_endpoints.json"""
        remote = self.config_client.get_configs(force=True)
        if remote:
            self._apply_remote_config(remote)
            return

        # ---- 回退：本地配置文件 ----
        logger.warning("[ConfigClient] 配置服务不可用，回退本地配置 api_endpoints.json")
        config_path = os.path.join(_config_dir, 'api_endpoints.json')
        if not os.path.exists(config_path):
            logger.warning("本地配置 api_endpoints.json 不存在，使用内置默认值")
            return

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

        # 4. 加载超时/重试参数
        self.timeout = data.get('timeout', 30)
        self.max_retries = data.get('max_retries', 2)
        self.retry_delay = data.get('retry_delay', 1)

        # 5. 加载 endpoints 模板
        self.endpoints = data.get('endpoints', {})

        # 6. 构建完整 URL（全部基于 OpenAPI 统一地址）
        for endpoint in self.endpoints.values():
            if 'path' in endpoint:
                endpoint['full_url'] = f"{self.openapi_base}{endpoint['path']}"

        logger.info(f"Apollo 配置加载完成（本地回退）[{self.current_env}]: OpenAPI={self.openapi_base}")

    def load_auth(self):
        """加载 Token：优先第三方接口拉取的（已解密），其次环境变量，最后本地 auth.json"""
        # 1. 第三方接口拉取（config_client 缓存中包含解密后的 openapi_token）
        remote = self.config_client._cache or {}
        remote_token = remote.get('openapi_token', '')
        if remote_token:
            self.openapi_token = remote_token
            logger.info("从第三方接口加载 Apollo OpenAPI Token（已解密）")
            return

        auth_path = os.path.join(_config_dir, 'auth.json')

        # 2. 环境变量
        env_token = os.environ.get('APOLLO_OPENAPI_TOKEN')
        if env_token:
            self.openapi_token = env_token
            logger.info("从环境变量加载 Apollo OpenAPI Token")
            return

        # 3. 本地 auth.json（回退）
        if os.path.exists(auth_path):
            try:
                with open(auth_path, 'r', encoding='utf-8') as f:
                    data = json.load(f)
                    self.openapi_token = data.get('openapi_token', '')
                    if self.openapi_token:
                        logger.info("从本地配置文件加载 Apollo OpenAPI Token（回退）")
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
            
            resp = self._session.get(url, params=params, headers=headers, timeout=self.config.timeout)
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
            "config_source": {
                "api_base": client.config.config_client.api_base,
                "session_id": client.config.config_client.session_id[:8] + "...(脱敏)",
                "using_remote": bool(client.config.config_client._cache),
                "refresh_sec": client.config.config_client.refresh_sec,
                "last_error": client.config.config_client.last_error or None
            },
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
