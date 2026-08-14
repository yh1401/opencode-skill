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
    try:
        import urllib3
        urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)
    except Exception:
        pass
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

    完整链路：skill → MCP → EasyOps 代理接口（/thirdApi/apollo/apps、/thirdApi/apollo/namespace）→ 返回配置

    设计目标：
        - 配置/应用查询统一走 EasyOps 代理接口，MCP 无需持有 Apollo Token
        - Token / 服务地址由第三方接口集中管理，MCP 通过接口获取（不直连 Apollo OpenAPI）
        - 启动时拉取一次并缓存，之后定时刷新；代理接口调用不通时直接返回错误提示

    环境变量：
        APOLLO_HOST_API_BASE      第三方接口地址，默认 https://easyops.tech.ctseelink.cn
        APOLLO_HOST_SESSION_ID    第三方鉴权 sessionId（32位），同时是 token 解密密钥
        APOLLO_HOST_NAME          可选，按服务名称模糊过滤第三方返回的 host（不填取第一条）
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
        self._default_host_id: Optional[int] = None # 默认 Apollo Host 的 id（作为 apolloHostId 兜底）
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

    def _fetch_host_list_raw(self) -> list:
        """调用第三方接口获取原始 Apollo Host 列表"""
        url = f"{self.api_base}/thirdApi/getApolloHostInfo"
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
        return host_list

    def _select_target_host(self, host_list: list) -> dict:
        """选择默认套：APOLLO_HOST_NAME 模糊过滤，否则取第一条"""
        name_filter = os.environ.get('APOLLO_HOST_NAME', '')
        if name_filter:
            for item in host_list:
                if name_filter.lower() in item.get('name', '').lower():
                    return item
        return host_list[0]

    def _fetch(self) -> dict:
        """调用第三方接口获取 Apollo Host 信息，组装成与本地模板同构的配置结构"""
        host_list = self._fetch_host_list_raw()
        target = self._select_target_host(host_list)
        self._default_host_id = target.get('id')

        host = target.get('host', '')
        raw_token = self.decrypt_token(target.get('token', ''), self.session_id)

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

    def fetch_host_list(self) -> list:
        """实时调用第三方接口查询所有可用的 Apollo 环境列表（供 apollo_host_list 每次调用时查询，不做缓存）。

        返回的 apolloHostId（即第三方记录 id）作为查询链路的入口：
        哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)
        """
        host_list = self._fetch_host_list_raw()
        default_id = self._select_target_host(host_list).get('id')
        self._default_host_id = default_id  # 顺带缓存默认套，避免 query_hosts 额外请求
        logger.info(f"[ConfigClient] 实时查询到 {len(host_list)} 条 Apollo Host 记录")

        return [
            {
                'apolloHostId': item.get('id'),
                'id': item.get('id'),
                'name': item.get('name', ''),
                'host': item.get('host', ''),
                'has_token': bool(self.decrypt_token(item.get('token', ''), self.session_id)),
                'secondProductId': item.get('secondProductId', []),
                'user': item.get('user', ''),
                'is_default': item.get('id') == default_id,
            }
            for item in host_list
        ]

    def get_default_host_id(self) -> Optional[int]:
        """获取默认 Apollo Host 的 id（作为 apolloHostId 兜底，配置查询/应用列表缺省时使用）"""
        if self._default_host_id is not None:
            return self._default_host_id
        try:
            host_list = self._fetch_host_list_raw()
            self._default_host_id = self._select_target_host(host_list).get('id')
        except Exception as e:
            logger.warning(f"[ConfigClient] 获取默认 Apollo Host ID 失败: {e}")
        return self._default_host_id

    def fetch_proxy_apps(self, apollo_host_id) -> dict:
        """通过 EasyOps 代理接口获取指定 Apollo 的应用列表（获取所有 app）

        GET {api_base}/thirdApi/apollo/apps?apolloHostId={id}
        鉴权：Cookie sessionId（EasyOps 统一保存 Apollo 地址与 Token，调用方无需传 Token）
        """
        url = f"{self.api_base}/thirdApi/apollo/apps"
        logger.info(f"[ConfigClient] 代理接口查询应用列表: apolloHostId={apollo_host_id}")
        resp = requests.get(
            url,
            params={"apolloHostId": apollo_host_id},
            headers={"Content-Type": "application/json"},
            cookies={"sessionId": self.session_id},
            timeout=30,
            verify=False
        )
        resp.raise_for_status()
        return resp.json()

    def fetch_proxy_namespace(self, apollo_host_id, env: str, app_id: str,
                              cluster_name: str = "default", namespace_name: str = "application") -> dict:
        """通过 EasyOps 代理接口获取指定 Apollo 单个 Namespace 配置

        GET {api_base}/thirdApi/apollo/namespace?apolloHostId=&env=&appId=&clusterName=&namespaceName=
        鉴权：Cookie sessionId（无需 Apollo Token）
        """
        url = f"{self.api_base}/thirdApi/apollo/namespace"
        params = {
            "apolloHostId": apollo_host_id,
            "env": env,
            "appId": app_id,
            "clusterName": cluster_name,
            "namespaceName": namespace_name,
        }
        logger.info(f"[ConfigClient] 代理接口查询配置: apolloHostId={apollo_host_id}, env={env}, appId={app_id}, "
                    f"cluster={cluster_name}, namespace={namespace_name}")
        resp = requests.get(
            url,
            params=params,
            headers={"Content-Type": "application/json"},
            cookies={"sessionId": self.session_id},
            timeout=30,
            verify=False
        )
        resp.raise_for_status()
        return resp.json()

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
        self.openapi_base = ""  # 默认套地址（来自第三方接口，仅用于信息展示）
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

        # 环境与地址：来自第三方接口（默认套地址仅用于信息展示，查询走代理接口，不直连）
        environments = data.get('environments', {})
        if not self.current_env:
            self.current_env = data.get('default_env', 'PRO')
            logger.info(f"未设置 APOLLO_ENV，使用配置服务默认环境: {self.current_env}")
        env_key = self.current_env
        if env_key in environments:
            env_config = environments[env_key]
            self.openapi_base = env_config.get('openapi_service', self.openapi_base)
            logger.info(f"[ConfigService] 加载 {env_key} 环境配置: OpenAPI={self.openapi_base}")

        # 构建完整 URL（仅用于健康检查/信息展示，无直连调用）
        for endpoint in self.endpoints.values():
            if 'path' in endpoint:
                endpoint['full_url'] = f"{self.openapi_base}{endpoint['path']}"

        logger.info(f"Apollo 配置加载完成（来自配置服务）[{self.current_env}]: OpenAPI={self.openapi_base}")

    def load_config(self):
        """加载 Apollo 连接配置：优先配置服务拉取；失败仅影响默认套地址展示，查询走代理接口不受影响"""
        remote = self.config_client.get_configs(force=True)
        if remote:
            self._apply_remote_config(remote)
            return

        # 配置服务不可用：代理查询不受影响，直接使用内置默认值
        logger.warning("[ConfigClient] 第三方接口拉取配置失败，使用内置默认值（代理查询不受影响）")
        self.timeout = 30
        self.max_retries = 2
        self.retry_delay = 1
        self.endpoints = {}

    def load_auth(self):
        """加载 Token 信息（仅用于展示）。配置/应用查询走 EasyOps 代理接口，无需本地 Token。"""
        remote = self.config_client._cache or {}
        remote_token = remote.get('openapi_token', '')
        if remote_token:
            self.openapi_token = remote_token
            logger.info("从第三方接口加载默认 Apollo 套 Token（仅展示，查询走代理接口）")
        else:
            logger.info("未从第三方接口获取到 Token（查询走代理接口，不受影响）")


class ApolloAPIClient:
    def __init__(self):
        self.config = ApolloConfig()
        self._request_count = 0
        self._success_count = 0
    
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
    
    def query_config(self, appId: str, env: str = "PRO", clusterName: str = "default",
                     namespaceName: str = "application", key: str = None,
                     apolloHostId: int = None) -> dict:
        """
        查询 Apollo 配置项。

        查询链路：哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)。
        通过 EasyOps 代理接口查询（按 apolloHostId 指定任意一套 Apollo，无需 Token）；调用不通时直接返回错误提示。
        """
        if USE_MOCK:
            return self._get_mock_response('apollo-config-query', appId=appId, env=env,
                                           clusterName=clusterName, namespaceName=namespaceName, key=key)

        if apolloHostId is None:
            apolloHostId = self.config.config_client.get_default_host_id()

        logger.info(f"查询配置: apolloHostId={apolloHostId}, env={env}, appId={appId}, "
                    f"cluster={clusterName}, namespace={namespaceName}, key={key or '(全部)'}")

        # ---- 通过 EasyOps 代理接口查询 ----
        try:
            self._request_count += 1
            proxy_result = self.config.config_client.fetch_proxy_namespace(
                apolloHostId, env, appId, clusterName, namespaceName)
        except Exception as e:
            logger.error(f"[apollo-config-query] 代理接口调用失败: {e}")
            return {"code": 503, "message": f"查询失败，EasyOps 代理接口不可用: {e}", "data": {}}

        if proxy_result is not None and str(proxy_result.get('code')) in ('200', 'success'):
            self._success_count += 1
            return self._normalize_config_result(proxy_result, appId, env, clusterName, namespaceName, key)

        # 代理返回业务错误（如 404 未授权/无此环境），原样返回
        return proxy_result or {"code": 500, "message": "代理接口返回为空", "data": {}}

    def _normalize_config_result(self, result: dict, appId: str, env: str,
                                 clusterName: str, namespaceName: str, key: str = None) -> dict:
        """统一处理：OpenAPI items 数组 → configurations 字典 + key 模糊过滤（保持输出格式稳定）"""
        if str(result.get('code')) in ('200', 'success'):
            data = result.get('data', {})
            if isinstance(data, dict) and 'items' in data:
                data['configurations'] = {
                    item['key']: item['value']
                    for item in data['items'] if isinstance(item, dict) and 'key' in item
                }
            if key:
                configurations = data.get('configurations', {})
                total_count = len(configurations)
                filtered = {k: v for k, v in configurations.items() if key.lower() in k.lower()}
                data['configurations'] = filtered
                data['_filtered_by'] = key
                data['_total_count'] = total_count
                data['_filtered_count'] = len(filtered)
            result['data'] = data
        return result
    
    def query_hosts(self) -> dict:
        """
        实时查询所有可用的 Apollo 环境列表（每次调用第三方接口，不做缓存）
        供 skill 引导用户选择在哪套 Apollo（apolloHostId）上查询
        """
        try:
            hosts = self.config.config_client.fetch_host_list()
        except Exception as e:
            logger.error(f"[ApolloAPIClient] 查询 Apollo 环境列表失败: {e}")
            return {"code": 503, "message": f"查询 Apollo 环境列表失败: {e}",
                    "data": {"default_host": self.config.openapi_base, "hosts": [], "count": 0}}
        return {
            "code": 200,
            "message": "success",
            "data": {
                "default_host": self.config.openapi_base,
                "default_apolloHostId": self.config.config_client.get_default_host_id(),
                "count": len(hosts),
                "hosts": hosts
            }
        }

    def query_apps(self, apolloHostId: int = None) -> dict:
        """
        查询 Apollo 应用列表（链路：哪套Apollo(apolloHostId) → 应用列表）。
        通过 EasyOps 代理接口查询（按 apolloHostId 指定任意一套 Apollo，无需 Token）；调用不通时直接返回错误提示。
        """
        if USE_MOCK:
            return self._get_mock_response('apollo-app-list')

        if apolloHostId is None:
            apolloHostId = self.config.config_client.get_default_host_id()
        logger.info(f"查询应用列表: apolloHostId={apolloHostId}")

        # ---- 通过 EasyOps 代理接口查询 ----
        try:
            self._request_count += 1
            proxy_result = self.config.config_client.fetch_proxy_apps(apolloHostId)
        except Exception as e:
            logger.error(f"[apollo-app-list] 代理接口调用失败: {e}")
            return {"code": 503, "message": f"查询失败，EasyOps 代理接口不可用: {e}", "data": {}}

        if proxy_result is not None and str(proxy_result.get('code')) in ('200', 'success'):
            self._success_count += 1
            return proxy_result

        return proxy_result or {"code": 500, "message": "代理接口返回为空", "data": {}}


client = ApolloAPIClient()


def _to_int(value):
    """将 JSON-RPC 传入的 apolloHostId 安全转换为 int（兼容字符串形式），无法解析时返回 None"""
    if value is None or value == "":
        return None
    try:
        return int(value)
    except (TypeError, ValueError):
        return None

# ============================================================================
# 标准 MCP 工具定义
# ============================================================================

MCP_TOOLS = [
    {
        "name": "apollo_config_query",
        "description": "查询 Apollo 配置中心的配置项列表。查询链路：哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 具体配置(key)。apolloHostId 缺省时使用默认 Apollo，env 缺省为 PRO。支持按 Key 关键词过滤。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "apolloHostId": {"type": "integer", "description": "Apollo 服务 ID，来自 apollo_host_list 返回的 apolloHostId，用于指定查询哪一套 Apollo；不传时使用默认 Apollo"},
                "appId": {"type": "string", "description": "应用ID，如 'rule-engine'。必填参数"},
                "env": {"type": "string", "description": "环境，如 PRO(生产)、DEV(开发)、SIT(集成测试)、FAT(测试)、UAT(预发)，以目标 Apollo 实际环境名为准"},
                "clusterName": {"type": "string", "description": "集群名称，默认 'default'"},
                "namespaceName": {"type": "string", "description": "Namespace 名称，默认 'application'"},
                "key": {"type": "string", "description": "配置项 Key 关键词（模糊匹配），用于过滤配置项"}
            },
            "required": ["appId"]
        }
    },
    {
        "name": "apollo_host_list",
        "description": "实时查询所有可用的 Apollo 服务列表（apolloHostId、服务名称、地址、Token 是否有、所属产品线、是否默认），每次调用都会实时获取，用于确定查询哪一套 Apollo（查询链路第一步）。",
        "inputSchema": {
            "type": "object",
            "properties": {}
        }
    },
    {
        "name": "apollo_app_list",
        "description": "获取指定 Apollo 服务中的所有应用列表（AppId、应用名称、所属部门）。apolloHostId 缺省时使用默认 Apollo。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "apolloHostId": {"type": "integer", "description": "Apollo 服务 ID，来自 apollo_host_list 返回的 apolloHostId，用于指定查询哪一套 Apollo；不传时使用默认 Apollo"}
            }
        }
    }
]

MCP_CAPABILITIES = {
    "tools/list": {},
    "tools/call": {}
}


class MCPProtocolHandler:
    """标准 MCP 协议处理器（Streamable HTTP，JSON-RPC 2.0）"""

    def __init__(self):
        pass

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
                key=arguments.get("key"),
                apolloHostId=_to_int(arguments.get("apolloHostId"))
            )
        elif tool_name == "apollo_host_list":
            return client.query_hosts()
        elif tool_name == "apollo_app_list":
            return client.query_apps(apolloHostId=_to_int(arguments.get("apolloHostId")))
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
                # 标准 MCP 握手：返回协议版本、服务端能力与标识
                client_version = params.get("protocolVersion", "2024-11-05")
                response = self._build_response(request_id, {
                    "protocolVersion": client_version,
                    "capabilities": {
                        "tools": {"listChanged": False}
                    },
                    "serverInfo": {
                        "name": "apollo-config-query-mcp",
                        "version": "3.0.0"
                    }
                })
                return response, False

            elif method == "notifications/initialized":
                # 通知类消息无 id，无需响应
                print(f"[MCP] notifications/initialized")
                return {}, False
            
            elif method == "tools/list":
                print(f"[MCP] tools/list")
                response = self._build_response(request_id, {"tools": MCP_TOOLS})
                return response, False
            
            elif method == "tools/call":
                tool_name = params.get("name")
                arguments = params.get("arguments", {})
                print(f"[MCP] tools/call: {tool_name}")
                
                if not tool_name:
                    return self._build_response(request_id, error=self._build_error(-32602, "tool name is required")), False
                
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
                    return response, False
                except ValueError as e:
                    print(f"[MCP] tool [{tool_name}] validation error: {str(e)}")
                    return self._build_response(request_id, error=self._build_error(-32602, str(e))), False
                except Exception as e:
                    print(f"[MCP] tool [{tool_name}] error: {str(e)}")
                    return self._build_response(request_id, error=self._build_error(-32603, f"Internal error: {str(e)}")), False
            
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
        version="3.0.0"
    )
    
    class ApolloConfigQueryParams(BaseModel):
        appId: str
        apolloHostId: Optional[int] = None
        env: Optional[str] = "PRO"
        clusterName: Optional[str] = "default"
        namespaceName: Optional[str] = "application"
        key: Optional[str] = None
    
    @app.get("/", tags=["基础"])
    def root():
        return {
            "service": "apollo-config-query-mcp",
            "version": "3.0.0",
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
            "version": "3.0.0",
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
        response, _ = mcp_handler.handle_message(request)
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
            key=params.key,
            apolloHostId=params.apolloHostId
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
