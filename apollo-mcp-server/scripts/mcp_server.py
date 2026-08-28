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
import re
import argparse
import time
import threading
from typing import Optional, Dict, Any, List, Tuple
from datetime import datetime
from concurrent.futures import ThreadPoolExecutor, as_completed

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
# 文件日志开关：默认开启（本地/开发）；线上部署设置 LOG_TO_FILE=false 关闭文件日志，
# 仅保留控制台输出（Docker 容器 stdout 由 json-file 驱动自动轮转，无需手动清理）
_log_to_file = os.environ.get('LOG_TO_FILE', 'true').lower() == 'true'

logging.basicConfig(
    level=getattr(logging, _log_level, logging.INFO),
    format=_log_format,
    datefmt="%Y-%m-%d %H:%M:%S"
)
logger = logging.getLogger("apollo-mcp")

# 同时输出到控制台和文件（LOG_TO_FILE=false 时仅控制台，不写日志文件）
if _log_to_file:
    _log_dir = os.environ.get('LOG_DIR', os.path.join(_root_dir, 'logs'))
    os.makedirs(_log_dir, exist_ok=True)
    _log_file = os.path.join(_log_dir, 'apollo-mcp.log')
    _file_handler = logging.FileHandler(_log_file)
    _file_handler.setFormatter(logging.Formatter(_log_format))
    logger.addHandler(_file_handler)


def _fmt(obj, max_len=2000):
    """将请求/响应对象转为单行 JSON 字符串用于日志，超长截断，便于阅读"""
    try:
        s = json.dumps(obj, ensure_ascii=False)
    except Exception:
        s = str(obj)
    if len(s) > max_len:
        return s[:max_len] + f"...(已截断, 总长{len(s)})"
    return s


# ============================================================================
# 敏感配置脱敏（与 skill 层规则保持一致，MCP 工具返回前先行脱敏，双层保险）
# 规则来源：.trae/skills/apollo-config-query/SKILL.md「敏感信息脱敏」
# ============================================================================
_SENSITIVE_KEYWORDS = (
    # 密码类
    "password", "passwd", "pwd", "密码",
    # 密钥类
    "secret", "secret_key", "私钥", "密钥", "credential", "凭据",
    # Token 类
    "token", "api_key", "apikey", "access_key", "accesskey", "session_key", "sign_key",
    # 加密类
    "aes_key", "aeskey", "encrypt", "加密", "证书", "cert", "iv",
    # 证书完整词：子串匹配不会误伤 certification/certify/concert（第 11 位 e≠i）
    "certificate",
)

_MASKED_VALUE = "******"

# 关键词归一化（去分隔符 . _ - 并小写）后预计算，匹配时对 key 同样归一化，
# 使 aes.key / aes_key / aes-key 都能命中 aeskey 关键词，避免点号分隔的敏感 key 漏判
_SEP_RE = re.compile(r'[._\-]')
_SENSITIVE_KEYWORDS_NORM = tuple(_SEP_RE.sub('', kw).lower() for kw in _SENSITIVE_KEYWORDS)
# 过短易误伤的关键词，改为"独立段匹配"（key 按分隔符切分后某段恰好等于该词才命中）：
#   - iv：子串会误伤 active/driver/private/archive/delivery（均含 iv 子串）
#   - cert：子串会误伤 certification/certify/concert（均含 cert 子串）
_SEGMENT_KEYWORDS = {"iv", "cert"}
# 子串关键词的否定子串：key 归一化后含否定子串时，该关键词不命中：
#   - token：tokenizer（分词器）含 token 子串但非敏感，遇到 tokenize 跳过；
#     保留 token 子串匹配以覆盖 authtoken/accesstoken 等连写（漏判风险 > 误伤）
_KEYWORD_EXCLUDE = {"token": ("tokenize",)}


def _is_sensitive_key(key: str) -> bool:
    """配置 Key（忽略大小写、忽略分隔符 . _ -）包含任一敏感关键词 → 视为敏感字段。

    匹配策略：
    - 大部分关键词用"归一化子串包含"（aes.key/aes_key/aes-key 都命中 aeskey）；
    - iv / cert 过短易误伤（active/driver/certification/concert 等），改为"独立段匹配"——
      仅当按分隔符切分后存在某段恰好等于该词（如 aes.iv / ssl.cert）才视为敏感；
    - token 保留子串匹配以覆盖连写（authtoken），但遇 tokenize 否定跳过（避免 tokenizer 误伤）；
    - certificate 用完整词子串匹配，覆盖 server.certificate，不误伤 certification。
    """
    s = str(key)
    norm = _SEP_RE.sub('', s).lower()
    for kw in _SENSITIVE_KEYWORDS_NORM:
        if kw in _SEGMENT_KEYWORDS:
            continue  # iv/cert 走独立段匹配，避免子串误伤
        if kw in norm:
            # 否定检查（如 token 遇 tokenize 跳过）
            excludes = _KEYWORD_EXCLUDE.get(kw)
            if excludes and any(ex in norm for ex in excludes):
                continue
            return True
    # iv/cert 独立段匹配：覆盖 aes.iv / ssl.cert / server.cert 等
    segments = _SEP_RE.split(s.lower())
    return any(seg in _SEGMENT_KEYWORDS for seg in segments)


def _mask_value(value) -> str:
    """敏感值脱敏：保留前2后2以供观察辨识，中间用 **** 替代；短值边界处理。
    - 空值 → ****** （标识为敏感字段但无值）
    - 1~2 位 → 全掩码（不暴露任何明文，因太短暴露比例过大）
    - 3~4 位 → 前1 + *(中间) + 后1
    - ≥5 位 → 前2 + **** + 后2（中间固定4星，不暴露原长度）"""
    s = "" if value is None else str(value)
    n = len(s)
    if n == 0:
        return _MASKED_VALUE
    if n <= 2:
        return "*" * n
    if n <= 4:
        return s[0] + "*" * (n - 2) + s[-1]
    return s[:2] + "****" + s[-2:]


def _mask_sensitive_configs(configurations: dict) -> tuple:
    """对 configurations 字典的敏感字段值脱敏（前2后2），返回 (脱敏后字典, 脱敏字段数)"""
    masked = {}
    count = 0
    for k, v in (configurations or {}).items():
        if _is_sensitive_key(k):
            masked[k] = _mask_value(v)
            count += 1
        else:
            masked[k] = v
    return masked, count


# ============================================================================
# 日志开关：HTTP 动态控制，随时开启/关闭（完全静默），无需重启
# ============================================================================
_LOG_ENABLED = True

# uvicorn 框架日志（启动信息/访问日志/错误日志）
_UVICORN_LOGGERS = ("uvicorn", "uvicorn.error", "uvicorn.access")


def _apply_log_enabled(enabled: bool):
    """应用日志开关：关闭时禁用 apollo-mcp 与 uvicorn 全部日志，完全静默"""
    global _LOG_ENABLED
    _LOG_ENABLED = enabled
    if enabled:
        logger.disabled = False
        for name in _UVICORN_LOGGERS:
            _uv_logger = logging.getLogger(name)
            _uv_logger.disabled = False
            _uv_logger.setLevel(logging.INFO)
        logger.info("[LOG] 日志开关已开启")
    else:
        logger.info("[LOG] 日志开关已关闭")
        logger.disabled = True
        for name in _UVICORN_LOGGERS:
            logging.getLogger(name).disabled = True


class ApolloRequestError(Exception):
    """代理接口请求失败（重试耗尽或异常）。携带 is_timeout 标记，便于业务层区分超时与其他错误，
    返回不同提示让 agent 平台作出判断而非盲目重试。"""
    def __init__(self, original, is_timeout: bool = False, timeout=None, tag: str = ""):
        self.original = original
        self.is_timeout = is_timeout
        self.timeout = timeout
        self.tag = tag
        super().__init__(f"{tag}请求失败: {type(original).__name__}: {original}")


def _build_proxy_error(e: Exception, tool_label: str) -> dict:
    """根据异常构建代理接口错误返回，区分超时(504)与其他(503)。
    返回含 errorType/retryable/suggestion，让 agent 平台能判断无需盲目重试。"""
    if isinstance(e, ApolloRequestError):
        if e.is_timeout:
            return {
                "code": 504,
                "message": f"{tool_label}超时：Apollo 代理接口在 {e.timeout} 秒内未响应",
                "data": {},
                "errorType": "timeout",
                "retryable": False,
                "suggestion": f"{tool_label}已快速失败，无需重试。Apollo 代理接口可能暂时响应慢或网络不通，"
                              f"建议告知用户稍后再试，或换一套 Apollo 查询"
            }
        return {
            "code": 503,
            "message": f"{tool_label}失败，EasyOps 代理接口不可用: {e.original}",
            "data": {},
            "errorType": "unavailable",
            "retryable": False,
            "suggestion": f"Apollo 代理接口暂不可用，建议告知用户稍后重试"
        }
    # 其他异常（如遍历可用套全部失败的 RuntimeError）
    return {
        "code": 503,
        "message": f"{tool_label}失败: {e}",
        "data": {},
        "errorType": "unavailable",
        "retryable": False,
        "suggestion": "服务暂不可用，建议告知用户稍后重试"
    }


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
        # 按工具区分超时（环境变量兜底；ApolloConfig 拉取配置服务后会回写覆盖）
        # apollo_app_list / apollo_config_query：6s 超时 + 0 重试，快速失败避免 agent 卡死
        self.apps_timeout = int(os.environ.get('APOLLO_APPS_TIMEOUT', '6'))
        # namespace 配置查询（apollo_config_query）单次超时（秒）
        self.timeout = int(os.environ.get('APOLLO_REQUEST_TIMEOUT', '6'))
        # apollo_host_list 第三方接口，8s + 1 次重试
        self.host_timeout = int(os.environ.get('APOLLO_HOST_LIST_TIMEOUT', '8'))  # Host 列表单次超时（秒）
        self.max_retries = int(os.environ.get('APOLLO_MAX_RETRIES', '1'))     # host_list 单请求失败重试次数（apps/config 内部固定 0）
        self.retry_delay = float(os.environ.get('APOLLO_RETRY_DELAY', '1'))   # 重试间隔（秒）
        self._traversal_budget = int(os.environ.get('APOLLO_TRAVERSAL_BUDGET', '20'))  # 遍历可用套总预算（秒）
        # ---- Host 探活（探测各套 Apollo 是否可用，标记后遍历时优先可用套，避免无效超时等待）----
        self._host_probe_cache: dict = {}           # {host_id: {available, app_count, error, checked_at}}
        self._host_probe_ttl = float(os.environ.get('APOLLO_HOST_PROBE_TTL', '60'))   # 探活结果缓存秒数（60s 内复用）
        self._host_probe_enabled = os.environ.get('APOLLO_HOST_PROBE_ENABLED', 'true').lower() != 'false'
        # ---- 全局搜索缓存（减少重复拉取同一 app 的配置）----
        # app 列表缓存：{host_id: (data, ts)}，5 分钟复用
        self._apps_cache: dict = {}
        self._apps_cache_ttl = float(os.environ.get('APOLLO_APPS_CACHE_TTL', '300'))
        # config 结果缓存：{(host_id, app_id, env, ns): (data, ts)}，2 分钟复用
        self._config_cache: dict = {}
        self._config_cache_ttl = float(os.environ.get('APOLLO_CONFIG_CACHE_TTL', '120'))
        # 全局搜索并行度 & 总预算
        self._global_search_workers = int(os.environ.get('APOLLO_GLOBAL_SEARCH_WORKERS', '8'))
        self._global_search_budget = int(os.environ.get('APOLLO_GLOBAL_SEARCH_BUDGET', '60'))
        self._global_search_max_ns = int(os.environ.get('APOLLO_GLOBAL_SEARCH_MAX_NS', '5'))  # 每个 app 扫几个 namespace
        # 搜索过程结果去重
        self._search_cache_lock = threading.Lock()

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
        params = {"paginator": False, "pageIndex": 1, "pageSize": 100}
        # 鉴权：Cookie sessionId（既是认证凭证，也是 token 解密密钥）
        logger.info(f"[ConfigClient] 请求 Apollo Host 列表: url={url}, params={_fmt(params)}, "
                    f"sessionId={self.session_id[:8]}...(脱敏)")
        resp = self._send_request(url, params, self.host_timeout, "Host列表")
        duration = 0  # 耗时已在 _send_request 内打印
        if resp.status_code >= 400:
            logger.error(f"[ConfigClient] 响应异常: status={resp.status_code}, body={_fmt(resp.text)}")
        resp.raise_for_status()
        data = resp.json()

        if data.get('code') != 'success':
            logger.error(f"[ConfigClient] 响应业务失败: body={_fmt(data)}")
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

    def _probe_host_available(self, host_id, name: str) -> dict:
        """探活单套 Apollo：调用 fetch_proxy_apps（轻量、6s 0 重试快速失败），判定是否可用。

        返回 {available, app_count, error}：available=True 表示该套代理接口可达且能正常返回应用列表。
        探活目的：标记后遍历时优先走可用套，跳过超时/空响应套，省去无效等待。
        """
        try:
            result = self.fetch_proxy_apps(host_id)
            if str(result.get('code')) in ('200', 'success'):
                data = result.get('data') or []
                apps = (data.get('items') or []) if isinstance(data, dict) else data
                count = len(apps) if isinstance(apps, list) else 0
                logger.info(f"[ConfigClient] 探活套 {host_id}({name}): 可用, appCount={count}")
                return {"available": True, "app_count": count, "error": ""}
            logger.warning(f"[ConfigClient] 探活套 {host_id}({name}): 不可用(业务错误), message={result.get('message', '')}")
            return {"available": False, "app_count": 0, "error": f"业务错误: {result.get('message', '未知')}"}
        except Exception as e:
            logger.warning(f"[ConfigClient] 探活套 {host_id}({name}): 不可用(异常), {type(e).__name__}: {e}")
            return {"available": False, "app_count": 0, "error": f"{type(e).__name__}: {e}"}

    def _probe_hosts_concurrent(self, host_list: list) -> dict:
        """并发探活所有 Apollo 套，返回 {host_id: probe_result}。

        利用 _host_probe_ttl 缓存：60s 内已探活过的套直接复用结果，不发请求；
        未缓存/过期的套用线程池并发探活（N 套并发≈单套耗时，而非串行 N×6s）。
        关闭探活（APOLLO_HOST_PROBE_ENABLED=false）时返回空 dict，遍历回退原全量逻辑。
        """
        if not self._host_probe_enabled:
            return {}
        now = time.time()
        result: dict = {}
        to_probe: list = []
        for item in host_list:
            hid = item.get('id')
            if hid is None:
                continue
            cached = self._host_probe_cache.get(hid)
            if cached and (now - cached.get('checked_at', 0)) < self._host_probe_ttl:
                result[hid] = cached  # 命中缓存，复用
            else:
                to_probe.append(item)

        if to_probe:
            from concurrent.futures import ThreadPoolExecutor, as_completed
            workers = min(8, len(to_probe))
            logger.info(f"[ConfigClient] 并发探活 {len(to_probe)} 套 Apollo（workers={workers}），"
                        f"命中缓存 {len(result)} 套")
            with ThreadPoolExecutor(max_workers=workers) as executor:
                futures = {
                    executor.submit(self._probe_host_available, item.get('id'), item.get('name', '')): item.get('id')
                    for item in to_probe
                }
                for future in as_completed(futures):
                    hid = futures[future]
                    try:
                        probe = future.result()
                    except Exception as e:
                        probe = {"available": False, "app_count": 0, "error": f"{type(e).__name__}: {e}"}
                    probe['checked_at'] = now
                    self._host_probe_cache[hid] = probe  # 更新缓存
                    result[hid] = probe
        elif result:
            logger.info(f"[ConfigClient] 探活全部命中缓存（{len(result)} 套），跳过请求")
        return result

    def fetch_host_list(self) -> list:
        """实时调用第三方接口查询所有可用的 Apollo 服务实例列表，并并发探活标记可用性。

        返回的 apolloHostId（即第三方记录 id）作为查询链路的入口：
        哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 配置项(key)

        探活：对每套并发调用应用列表接口探活，结果带 available/appCount 标记并缓存（默认 60s），
        供后续遍历优先走可用套、跳过不可用套，省去无效超时等待。
        """
        host_list = self._fetch_host_list_raw()
        default_id = self._select_target_host(host_list).get('id')
        self._default_host_id = default_id  # 顺带缓存默认套，避免 query_hosts 额外请求
        logger.info(f"[ConfigClient] 实时查询到 {len(host_list)} 条 Apollo Host 记录，开始探活标记可用性")

        # 并发探活（命中缓存则跳过请求），结果用于 host 字段标记 + 后续遍历排序
        probe_map = self._probe_hosts_concurrent(host_list)

        hosts = []
        for item in host_list:
            hid = item.get('id')
            probe = probe_map.get(hid, {})
            hosts.append({
                'apolloHostId': hid,
                'id': hid,
                'name': item.get('name', ''),
                'host': item.get('host', ''),
                'has_token': bool(self.decrypt_token(item.get('token', ''), self.session_id)),
                'secondProductId': item.get('secondProductId', []),
                'user': item.get('user', ''),
                'is_default': hid == default_id,
                # 探活标记：available=true 表示该套代理接口可达、可正常查询
                'available': probe.get('available', None),
                'appCount': probe.get('app_count', None),
                'availableDetail': probe.get('error', '') if not probe.get('available') else '',
            })
        avail_count = sum(1 for h in hosts if h.get('available'))
        logger.info(f"[ConfigClient] 探活完成: 可用 {avail_count}/{len(hosts)} 套")
        return hosts

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

    def _proxy_get(self, url: str, params: dict, timeout: int = None,
                   max_retries: int = None, tag: str = "代理接口") -> requests.Response:
        """调用 EasyOps 代理接口 GET，统一打印完整出入参数日志（URL/参数/状态码/耗时/响应体）。
        timeout/max_retries 可按工具覆盖（如 apps 工具传 5s/0 重试快速失败）。"""
        actual_timeout = self.timeout if timeout is None else timeout
        logger.info(f"[ConfigClient] {tag}请求: url={url}, params={_fmt(params)}, "
                    f"timeout={actual_timeout}s, sessionId={self.session_id[:8]}...(脱敏)")
        resp = self._send_request(url, params, actual_timeout, tag, max_retries=max_retries)
        try:
            body = _fmt(resp.json())
        except Exception:
            body = _fmt(resp.text)
        logger.info(f"[ConfigClient] {tag}响应: status={resp.status_code}, url={resp.url}, body={body}")
        if resp.status_code >= 400:
            logger.error(f"[ConfigClient] {tag}返回异常状态码: status={resp.status_code}, body={_fmt(resp.text)}")
        resp.raise_for_status()
        return resp

    def _send_request(self, url: str, params: dict, timeout: int, tag: str,
                      max_retries: int = None) -> "requests.Response":
        """统一发请求 + 短重试。超时/连接异常时按 max_retries 重试（间隔 retry_delay 秒），
        正常请求毫秒级返回不受影响；重试耗尽抛 ApolloRequestError（含 is_timeout 标记），
        便于业务层区分超时返回 504，让 agent 平台判断而非盲目重试。"""
        retries = self.max_retries if max_retries is None else max_retries
        last_exc = None
        total_attempts = retries + 1
        for attempt in range(total_attempts):
            try:
                start = time.time()
                resp = requests.get(
                    url,
                    params=params,
                    headers={"Content-Type": "application/json"},
                    cookies={"sessionId": self.session_id},
                    timeout=timeout,
                    verify=False
                )
                duration = round((time.time() - start) * 1000)
                logger.info(f"[ConfigClient] {tag}请求: attempt={attempt + 1}/{total_attempts}, "
                            f"status={resp.status_code}, 耗时={duration}ms, url={resp.url}")
                return resp
            except Exception as e:
                last_exc = e
                logger.warning(f"[ConfigClient] {tag}请求异常: attempt={attempt + 1}/{total_attempts}, "
                                f"error={type(e).__name__}: {e}")
                if attempt < retries:
                    time.sleep(self.retry_delay)
        # 判断是否超时类异常（requests.exceptions.Timeout 子类或异常信息含 timeout/timed out）
        is_timeout = False
        try:
            is_timeout = isinstance(last_exc, requests.exceptions.Timeout)
        except Exception:
            pass
        if not is_timeout:
            ename = type(last_exc).__name__.lower()
            is_timeout = 'timeout' in ename or 'timed out' in str(last_exc).lower()
        raise ApolloRequestError(last_exc, is_timeout=is_timeout, timeout=timeout, tag=tag)

    def fetch_proxy_apps(self, apollo_host_id) -> dict:
        """通过 EasyOps 代理接口获取指定 Apollo 的应用列表（获取所有 app）

        GET {api_base}/thirdApi/apollo/apps?apolloHostId={id}
        鉴权：Cookie sessionId（EasyOps 统一保存 Apollo 地址与 Token，调用方无需传 Token）
        """
        url = f"{self.api_base}/thirdApi/apollo/apps"
        # 应用列表数据简单应快速返回：5s 超时 + 0 重试，超时即快速失败
        resp = self._proxy_get(url, {"apolloHostId": apollo_host_id},
                               timeout=self.apps_timeout, max_retries=0, tag="应用列表")
        return resp.json()

    def fetch_proxy_apps_first_available(self) -> tuple:
        """不带 apolloHostId 时遍历可用套：默认套优先 + 探活可用套优先，返回第一个能成功获取应用列表的套。

        用于"先出应用列表、用户选择后再查配置"的交互引导：
        默认套（如 15）不可用时自动切换到其它可用套，避免列表查询整体失败。

        探活优化：遍历前并发探活各套（命中缓存则跳过请求），按 available=true 优先排序，
        不可用套（超时/空）排最后，受总预算限制基本不会被试到，省去无效超时等待。

        返回: (proxy_result, used_host_id, host_name)
        - proxy_result 顶层附加 usedHostId / hostName 字段，标注实际使用的套
        - 全部套都失败时抛 RuntimeError（由业务层统一转 503）
        """
        host_list = self._fetch_host_list_raw()
        default_id = self._select_target_host(host_list).get('id')
        probe_map = self._probe_hosts_concurrent(host_list)  # 探活（命中缓存则不发请求）
        # 排序：探活可用优先（最重要，避免在不可用套上等待）→ 默认套优先 → 原顺序
        ordered = sorted(
            host_list,
            key=lambda item: (
                0 if probe_map.get(item.get('id'), {}).get('available') else 1,
                0 if item.get('id') == default_id else 1,
            )
        )
        skipped_unavail = sum(1 for it in ordered if probe_map.get(it.get('id'), {}).get('available') is False)
        if skipped_unavail:
            logger.info(f"[ConfigClient] 遍历应用列表：探活标记 {skipped_unavail} 套不可用，已排到最后")
        errors = []
        budget_start = time.time()
        budget_sec = self._traversal_budget   # 遍历总预算（秒），超此提前终止
        for item in ordered:
            if time.time() - budget_start > budget_sec:
                logger.warning(f"[ConfigClient] 遍历应用列表超总预算 {budget_sec}s，提前终止（已试 {len(errors)} 套）")
                errors.append(f"超遍历预算{budget_sec}s终止")
                break
            hid = item.get('id')
            name = item.get('name', '')
            try:
                result = self.fetch_proxy_apps(hid)
                if str(result.get('code')) in ('200', 'success'):
                    data = result.get('data') or []
                    apps = (data.get('items') or []) if isinstance(data, dict) else data
                    logger.info(f"[ConfigClient] 遍历套找到可用应用列表: apolloHostId={hid}, name={name}, "
                                f"appCount={len(apps) if isinstance(apps, list) else '未知'}")
                    result['usedHostId'] = hid
                    result['hostName'] = name
                    return result, hid, name
                errors.append(f"套{hid}({name}) 业务错误: {result.get('message', '未知')}")
            except Exception as e:
                errors.append(f"套{hid}({name}) 异常: {e}")
        detail = '; '.join(errors) if errors else '无可用套'
        raise RuntimeError(f"遍历所有 Apollo 套均无法获取应用列表: {detail}")

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
        resp = self._proxy_get(url, params, timeout=self.timeout, max_retries=0, tag="配置查询")
        return resp.json()

    def fetch_proxy_namespace_first_match(self, env: str, app_id: str,
                                          cluster_name: str = "default",
                                          namespace_name: str = "application") -> tuple:
        """不带 apolloHostId 时遍历可用套，按 appId 定位所在套：默认套优先 + 探活可用套优先，
        返回第一个能成功查询到该应用配置的套（"只传 appId 也能自动定位"）。

        探活优化：遍历前并发探活各套（命中缓存则跳过请求），按 available=true 优先排序，
        不可用套排最后，省去在不可用套上的无效超时等待。

        返回: (proxy_result, used_host_id, host_name)
        - proxy_result 顶层附加 usedHostId / hostName 字段，标注实际使用的套
        - 全部套都失败时抛 RuntimeError（由业务层统一转 503）
        """
        host_list = self._fetch_host_list_raw()
        default_id = self._select_target_host(host_list).get('id')
        probe_map = self._probe_hosts_concurrent(host_list)  # 探活（命中缓存则不发请求）
        # 排序：探活可用优先（最重要，避免在不可用套上等待）→ 默认套优先 → 原顺序
        ordered = sorted(
            host_list,
            key=lambda item: (
                0 if probe_map.get(item.get('id'), {}).get('available') else 1,
                0 if item.get('id') == default_id else 1,
            )
        )
        skipped_unavail = sum(1 for it in ordered if probe_map.get(it.get('id'), {}).get('available') is False)
        if skipped_unavail:
            logger.info(f"[ConfigClient] 遍历应用配置：探活标记 {skipped_unavail} 套不可用，已排到最后")
        errors = []
        budget_start = time.time()
        budget_sec = self._traversal_budget   # 遍历总预算（秒），超此提前终止
        for item in ordered:
            if time.time() - budget_start > budget_sec:
                logger.warning(f"[ConfigClient] 遍历应用配置超总预算 {budget_sec}s，提前终止（已试 {len(errors)} 套）")
                errors.append(f"超遍历预算{budget_sec}s终止")
                break
            hid = item.get('id')
            name = item.get('name', '')
            try:
                result = self.fetch_proxy_namespace(hid, env, app_id, cluster_name, namespace_name)
                if str(result.get('code')) in ('200', 'success'):
                    logger.info(f"[ConfigClient] 遍历套定位到应用配置: apolloHostId={hid}, name={name}, "
                                f"appId={app_id}, env={env}, namespace={namespace_name}")
                    result['usedHostId'] = hid
                    result['hostName'] = name
                    return result, hid, name
                errors.append(f"套{hid}({name}) 业务错误: {result.get('message', '未知')}")
            except Exception as e:
                errors.append(f"套{hid}({name}) 异常: {e}")
        detail = '; '.join(errors) if errors else '无可用套'
        raise RuntimeError(f"遍历所有 Apollo 套均未找到应用 {app_id} 的配置: {detail}")

    # ========================================================================
    # 缓存层（全局搜索时避免重复拉取同一 host 的 app 列表、同一 app 的 config）
    # ========================================================================

    def _cached_fetch_apps(self, host_id) -> Optional[List[dict]]:
        """带缓存的应用列表查询：缓存命中直接返回，未命中请求后存入缓存。"""
        now = time.time()
        with self._search_cache_lock:
            cached = self._apps_cache.get(host_id)
            if cached and (now - cached[1]) < self._apps_cache_ttl:
                logger.debug(f"[ConfigClient] apps_cache 命中: host_id={host_id}, apps={len(cached[0])}")
                return cached[0]
        # 未命中：请求
        try:
            result = self.fetch_proxy_apps(host_id)
            data = result.get('data') or []
            apps = (data.get('items') or []) if isinstance(data, dict) else data
            if str(result.get('code')) in ('200', 'success') and isinstance(apps, list):
                with self._search_cache_lock:
                    self._apps_cache[host_id] = (apps, time.time())
                return apps
        except Exception as e:
            logger.debug(f"[ConfigClient] 全局搜索 apps 查询失败: host_id={host_id}, error={e}")
        return None

    def _cached_fetch_namespace(self, host_id, env, app_id, ns_name) -> Optional[Dict]:
        """带缓存的 Namespace 配置查询：缓存命中直接返回，未命中请求后存入缓存。"""
        cache_key = (host_id, app_id, env, ns_name)
        now = time.time()
        with self._search_cache_lock:
            cached = self._config_cache.get(cache_key)
            if cached and (now - cached[1]) < self._config_cache_ttl:
                logger.debug(f"[ConfigClient] config_cache 命中: {cache_key}")
                return cached[0]
        try:
            result = self.fetch_proxy_namespace(host_id, env, app_id, 'default', ns_name)
            if str(result.get('code')) in ('200', 'success'):
                data = result.get('data') or {}
                configurations = data.get('configurations') or {}
                with self._search_cache_lock:
                    self._config_cache[cache_key] = (configurations, time.time())
                return configurations
        except Exception as e:
            logger.debug(f"[ConfigClient] 全局搜索 config 查询失败: {cache_key}, error={e}")
        return None

    def _clear_search_cache(self):
        """清空全局搜索缓存（可手动触发）"""
        with self._search_cache_lock:
            self._apps_cache.clear()
            self._config_cache.clear()
            self._host_probe_cache.clear()
        logger.info("[ConfigClient] 搜索缓存已清空")

    # ========================================================================
    # 全局搜索核心（并发遍历所有可用 host 的所有 app，按 keyword 匹配）
    # ========================================================================

    def fetch_global_search(self, keyword: str, search_field: str = "both",
                            env: str = "PRO", max_results: int = 100,
                            namespace_name: str = "application") -> dict:
        """全局配置搜索：并发遍历所有可用 Apollo 套的所有应用，按 keyword 匹配 key 或 value。

        解决的场景："哪些应用的配置用到了某个地址/关键词"——这是日常排障/比对的高频需求，
        原来需要逐个应用调用 apollo_config_query（700 应用 × 6s = 小时级），
        现在由 MCP 内部用 ThreadPoolExecutor 并行完成，单次调用即可返回结果。

        策略：
        1. 并发遍历可用 host（按探活结果排序，跳过不可用套）
        2. 每个 host 并发遍历所有 app 的指定 namespace 配置
        3. 对每个配置项按 keyword 匹配 key 或 value（search_field 控制）
        4. 总预算 self._global_search_budget 秒，超时返回已查到的部分结果 + 未完成标记
        5. 结果带 host_id/host_name/app_id/namespace 标注，脱敏后返回

        返回结构：
        {
          "code": 200,
          "message": "success",
          "data": {
            "keyword": "...", "searchField": "...", "env": "...", "namespace": "...",
            "summary": {"totalScanned": N, "matchedCount": M, "elapsedMs": T},
            "matches": [{"apolloHostId":..,"hostName":..,"appId":..,"appName":..,"namespaceName":..,"key":..,"value":..}],
            "partial": true/false,   // 是否超预算提前终止
            "unfinishedApps": [...]   // 超预算时未完成的应用（仅 sample，最多 20 条）
          }
        }
        """
        start = time.time()
        kw = (keyword or '').strip().lower()
        if not kw:
            raise ValueError("keyword 不能为空")

        # 1. 取 host 列表 + 探活排序
        host_list = self._fetch_host_list_raw()
        default_id = self._select_target_host(host_list).get('id')
        probe_map = self._probe_hosts_concurrent(host_list)
        ordered_hosts = sorted(
            host_list,
            key=lambda item: (
                0 if probe_map.get(item.get('id'), {}).get('available') is not False else 1,
                0 if item.get('id') == default_id else 1,
            )
        )
        # 只保留探活可用或未探活的套（跳过明确不可用的）
        active_hosts = [h for h in ordered_hosts if probe_map.get(h.get('id'), {}).get('available') is not False]
        if not active_hosts:
            active_hosts = ordered_hosts  # 全部不可用时退化为尝试所有

        budget_sec = self._global_search_budget
        budget_start = time.time()
        matches: List[dict] = []
        unfinished_apps: List[str] = []
        app_id_set: set = set()  # 去重同一 appId
        scanned_count = 0
        max_ns = self._global_search_max_ns

        # 2. 并发遍历：每个 host 内遍历 apps，每个 app 查 namespace 配置
        def _scan_host(host_info) -> Tuple[List[dict], List[str], int]:
            """扫描单个 host 下所有应用，返回 (matches, unfinished_apps, scanned_count)"""
            hid = host_info.get('id')
            hname = host_info.get('name', '')
            local_matches: List[dict] = []
            local_unfinished: List[str] = []
            local_scanned = 0

            # 预算检查
            if time.time() - budget_start > budget_sec:
                local_unfinished.append(f"host_{hid}_{hname}")
                return local_matches, local_unfinished, local_scanned

            apps = self._cached_fetch_apps(hid)
            if not apps:
                return local_matches, local_unfinished, local_scanned

            for app in apps:
                # 再次检查预算
                if time.time() - budget_start > budget_sec:
                    local_unfinished.append(f"{hid}/{app.get('appId')}")
                    break

                app_id = app.get('appId') or app.get('appid', '')
                app_name = app.get('name', '') or app_id
                if not app_id or app_id in app_id_set:
                    continue
                app_id_set.add(app_id)

                # 扫描 namespace（默认 application，外加常见的几个）
                ns_list = [namespace_name]
                if namespace_name == 'application':
                    # 同时扫 datasource/redis 等常见 namespace（最多 max_ns 个）
                    extras = ['datasource', 'redis', 'common', 'logging']
                    for ns in extras:
                        if len(ns_list) >= max_ns:
                            break
                        if ns not in ns_list:
                            ns_list.append(ns)

                for ns in ns_list:
                    if time.time() - budget_start > budget_sec:
                        break
                    configs = self._cached_fetch_namespace(hid, env, app_id, ns)
                    if not configs:
                        continue
                    local_scanned += 1
                    for k, v in configs.items():
                        k_lower = str(k).lower()
                        v_lower = str(v).lower()
                        hit = False
                        if search_field in ('key', 'both') and kw in k_lower:
                            hit = True
                        if not hit and search_field in ('value', 'both') and kw in v_lower:
                            hit = True
                        if hit:
                            local_matches.append({
                                'apolloHostId': hid,
                                'hostName': hname,
                                'appId': app_id,
                                'appName': app_name,
                                'namespaceName': ns,
                                'key': k,
                                'value': v,
                            })
            return local_matches, local_unfinished, local_scanned

        # 3. 并发执行各 host 扫描（max_workers 线程池）
        actual_workers = min(self._global_search_workers, max(len(active_hosts), 1))
        logger.info(f"[ConfigClient] 开始全局搜索: keyword='{kw}', field={search_field}, env={env}, "
                    f"hosts={len(active_hosts)}, workers={actual_workers}, budget={budget_sec}s")

        with ThreadPoolExecutor(max_workers=actual_workers) as pool:
            futures = {pool.submit(_scan_host, h): h for h in active_hosts}
            for fut in as_completed(futures):
                try:
                    m, uf, sc = fut.result(timeout=budget_sec)
                    matches.extend(m)
                    unfinished_apps.extend(uf)
                    scanned_count += sc
                except Exception as e:
                    logger.debug(f"[ConfigClient] 全局搜索 host 扫描异常: {e}")

        elapsed_ms = round((time.time() - start) * 1000)
        partial = (time.time() - budget_start) > budget_sec or len(unfinished_apps) > 0

        # 应用脱敏（与单配置查询一致，直接调用模块顶层函数）
        masked_count = 0
        for m in matches:
            if _is_sensitive_key(m['key']):
                m['value'] = _mask_value(m['value'])
                m['masked'] = True
                masked_count += 1

        # 截断结果（按 max_results 限制）
        truncated = False
        if len(matches) > max_results:
            matches = matches[:max_results]
            truncated = True

        # 采样 unfinished_apps 避免过长
        if len(unfinished_apps) > 20:
            unfinished_apps = unfinished_apps[:20]
            unfinished_apps.append(f"... 另有 {len(unfinished_apps) - 20} 项未完成")

        logger.info(f"[ConfigClient] 全局搜索完成: keyword='{kw}', matches={len(matches)}, "
                    f"scanned_apps={scanned_count}, elapsed={elapsed_ms}ms, partial={partial}")

        result_data = {
            "keyword": keyword,
            "searchField": search_field,
            "env": env,
            "namespace": namespace_name,
            "summary": {
                "hostCount": len(active_hosts),
                "appCount": len(app_id_set),
                "scannedApps": scanned_count,
                "matchedCount": len(matches),
                "maskedCount": masked_count,
                "elapsedMs": elapsed_ms,
            },
            "matches": matches,
            "partial": partial,
            "truncated": truncated,
        }
        if unfinished_apps:
            result_data["unfinishedApps"] = unfinished_apps

        return {"code": 200, "message": "success", "data": result_data}

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
        self.timeout = 8
        self.max_retries = 1
        self.retry_delay = 1
        self.config_client = ConfigClient()
        # 异步后台加载配置：避免 EasyOps 不可达时阻塞服务启动 17s 导致 MCP 客户端连接超时
        # 配置仅用于展示（openapi_base/token/endpoints），查询走代理接口不依赖此配置
        self._config_loaded = False
        self._config_lock = threading.Lock()
        self._bg_thread = threading.Thread(target=self._background_load, daemon=True)
        self._bg_thread.start()

    def _background_load(self):
        """后台加载配置 + 鉴权信息（不阻塞服务启动）"""
        try:
            self.load_config()
            self.load_auth()
        except Exception as e:
            logger.warning(f"[ApolloConfig] 后台加载配置异常（不影响查询）: {e}")
        finally:
            with self._config_lock:
                self._config_loaded = True
    
    def _apply_remote_config(self, data: dict):
        """应用配置服务拉取的配置（地址、环境、endpoints、超时等）"""
        self.endpoints = data.get('endpoints', {})
        # 超时/重试：优先用配置服务下发的值，默认值与 ConfigClient 一致（8s/1次/1s）
        self.timeout = data.get('timeout', 8)
        self.max_retries = data.get('max_retries', 1)
        self.retry_delay = data.get('retry_delay', 1)
        # 回写到 ConfigClient，让配置服务的超时/重试真正生效（取代之前的"僵尸配置"）
        self.config_client.timeout = self.timeout
        self.config_client.max_retries = self.max_retries
        self.config_client.retry_delay = self.retry_delay
        budget = data.get('traversal_budget')
        if budget:
            self.config_client._traversal_budget = int(budget)

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

        # 配置服务不可用：代理查询不受影响，使用内置默认值（ConfigClient 已在 __init__ 读取环境变量）
        logger.warning("[ConfigClient] 第三方接口拉取配置失败，使用内置默认值（代理查询不受影响）")
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

        # ---- 通过 EasyOps 代理接口查询 ----
        start = time.time()
        try:
            self._request_count += 1
            if apolloHostId is None:
                proxy_result, used_host_id, host_name = self.config.config_client.fetch_proxy_namespace_first_match(
                    env, appId, clusterName, namespaceName)
                logger.info(f"查询配置: apolloHostId 未指定，遍历可用套定位，命中套={used_host_id}({host_name}), "
                            f"env={env}, appId={appId}, cluster={clusterName}, namespace={namespaceName}, key={key or '(全部)'}")
            else:
                proxy_result = self.config.config_client.fetch_proxy_namespace(
                    apolloHostId, env, appId, clusterName, namespaceName)
                logger.info(f"查询配置: apolloHostId={apolloHostId}, env={env}, appId={appId}, "
                            f"cluster={clusterName}, namespace={namespaceName}, key={key or '(全部)'}")
        except Exception as e:
            duration = round((time.time() - start) * 1000)
            logger.error(f"[apollo-config-query] 代理接口调用失败: {type(e).__name__}, 耗时={duration}ms, error={e}")
            return _build_proxy_error(e, "配置查询")
        duration = round((time.time() - start) * 1000)
        logger.info(f"[apollo-config-query] 代理接口返回: 耗时={duration}ms, result={_fmt(proxy_result)}")

        if proxy_result is not None and str(proxy_result.get('code')) in ('200', 'success'):
            self._success_count += 1
            result = self._normalize_config_result(proxy_result, appId, env, clusterName, namespaceName, key)
            data = result.get('data') or {}
            logger.info(f"[apollo-config-query] 查询成功: appId={appId}, env={env}, namespace={namespaceName}, "
                        f"configCount={len(data.get('configurations', {}))}, result={_fmt(result)}")
            return result

        # 代理返回业务错误（如 404 未授权/无此环境），原样返回
        err_code = proxy_result.get('code') if proxy_result else '空'
        err_msg = proxy_result.get('message') if proxy_result else '代理接口返回为空'
        logger.warning(f"[apollo-config-query] 代理返回业务错误: code={err_code}, message={err_msg}")
        return proxy_result or {"code": 500, "message": "代理接口返回为空", "data": {}}

    def _normalize_config_result(self, result: dict, appId: str, env: str,
                                 clusterName: str, namespaceName: str, key: str = None) -> dict:
        """统一处理：OpenAPI items 数组 → configurations 字典 + key 模糊过滤 + 敏感字段脱敏（保持输出格式稳定）"""
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
            # 敏感字段脱敏（硬性规则：MCP 返回前先行脱敏，与 skill 层规则一致）
            configurations = data.get('configurations', {})
            masked_configs, masked_count = _mask_sensitive_configs(configurations)
            data['configurations'] = masked_configs
            data['maskedCount'] = masked_count
            if masked_count > 0:
                data['maskedFieldsTip'] = f"（已对 {masked_count} 个敏感字段脱敏，如需查看明文请联系管理员确认）"
            logger.info(f"[apollo-config-query] 脱敏完成: 共 {len(masked_configs)} 项，敏感字段 {masked_count} 个已掩码")
            result['data'] = data
        return result
    
    def query_hosts(self) -> dict:
        """
        实时查询所有可用的 Apollo 服务实例列表（每次调用第三方接口，不做缓存）
        供 skill 引导用户选择在哪套 Apollo（apolloHostId）上查询
        """
        try:
            start = time.time()
            hosts = self.config.config_client.fetch_host_list()
            duration = round((time.time() - start) * 1000)
            logger.info(f"[apollo-host-list] 查询成功: 耗时={duration}ms, hostCount={len(hosts)}, "
                        f"hostIds={[h.get('apolloHostId') for h in hosts]}")
        except Exception as e:
            duration = round((time.time() - start) * 1000)
            logger.error(f"[apollo-host-list] 查询失败: {type(e).__name__}, 耗时={duration}ms, error={e}")
            err = _build_proxy_error(e, "Apollo 服务实例列表查询")
            err["data"] = {"default_host": self.config.openapi_base, "hosts": [], "count": 0}
            return err
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

        apolloHostId 缺省时：遍历所有可用套，取第一个能成功返回应用列表的套
        （结果顶层带 usedHostId / hostName 标注实际使用的套），供"先出列表再选应用"的交互引导使用。
        """
        if USE_MOCK:
            return self._get_mock_response('apollo-app-list')

        start = time.time()
        try:
            self._request_count += 1
            if apolloHostId is None:
                proxy_result, used_host_id, host_name = self.config.config_client.fetch_proxy_apps_first_available()
                logger.info(f"查询应用列表: apolloHostId 未指定，遍历可用套，命中套={used_host_id}({host_name})")
            else:
                proxy_result = self.config.config_client.fetch_proxy_apps(apolloHostId)
                logger.info(f"查询应用列表: apolloHostId={apolloHostId}")
        except Exception as e:
            duration = round((time.time() - start) * 1000)
            logger.error(f"[apollo-app-list] 代理接口调用失败: {type(e).__name__}, 耗时={duration}ms, error={e}")
            return _build_proxy_error(e, "应用列表查询")
        duration = round((time.time() - start) * 1000)
        logger.info(f"[apollo-app-list] 代理接口返回: 耗时={duration}ms, result={_fmt(proxy_result)}")

        if proxy_result is not None and str(proxy_result.get('code')) in ('200', 'success'):
            self._success_count += 1
            data = proxy_result.get('data') or []
            apps = (data.get('items') or []) if isinstance(data, dict) else data
            used = proxy_result.get('usedHostId') or apolloHostId
            logger.info(f"[apollo-app-list] 查询成功: apolloHostId={used}, appCount={len(apps) if isinstance(apps, list) else '未知'}")
            return proxy_result

        err_code = proxy_result.get('code') if proxy_result else '空'
        err_msg = proxy_result.get('message') if proxy_result else '代理接口返回为空'
        logger.warning(f"[apollo-app-list] 代理返回业务错误: code={err_code}, message={err_msg}")
        return proxy_result or {"code": 500, "message": "代理接口返回为空", "data": {}}

    def query_global_search(self, keyword: str, search_field: str = "both",
                            env: str = "PRO", max_results: int = 100,
                            namespace_name: str = "application") -> dict:
        """全局配置搜索（解决"哪些应用的配置用到了某个地址/关键词"的高频排障需求）。

        自动并发遍历所有可用 Apollo 套的所有应用（线程池并行），按 keyword 匹配 key 或 value。
        单次调用返回结果——无需 agent 逐个应用调 apollo_config_query（700+ 应用原来小时级，现在 30-60 秒）。

        参数:
            keyword: 搜索关键词（必填），如 "vcpinner.cn:13302"、"password"、"timeout"
            search_field: "key" / "value" / "both"，默认 both
            env: 环境，默认 PRO
            max_results: 最多返回多少条匹配（默认 100）
            namespace_name: 搜索哪个 namespace 的配置（默认 application；值为 application 时自动扩展扫常见 namespace）

        返回结构：
            code=200，data.matches 每条含 host_id/host_name/app_id/app_name/namespace/key/value，
            summary 有扫描统计，partial=true 表示超预算提前终止（结果可能不全）。
        """
        if USE_MOCK:
            # Mock 模式：在已有的 mock app 里模拟搜索匹配
            if not keyword or not keyword.strip():
                raise ValueError("keyword 不能为空")
            return self._mock_global_search(keyword, search_field, max_results)

        start = time.time()
        try:
            self._request_count += 1
            result = self.config.config_client.fetch_global_search(
                keyword=keyword,
                search_field=search_field,
                env=env,
                max_results=max_results,
                namespace_name=namespace_name,
            )
            self._success_count += 1
            duration = round((time.time() - start) * 1000)
            data = result.get('data') or {}
            summary = data.get('summary', {})
            logger.info(f"[apollo-global-search] 全局搜索完成: keyword='{keyword}', matches={len(data.get('matches', []))}, "
                        f"partial={data.get('partial')}, elapsed={duration}ms, result={_fmt(result)}")
            return result
        except Exception as e:
            duration = round((time.time() - start) * 1000)
            logger.error(f"[apollo-global-search] 全局搜索失败: {type(e).__name__}, 耗时={duration}ms, error={e}")
            return _build_proxy_error(e, "全局配置搜索")

    def _mock_global_search(self, keyword: str, search_field: str, max_results: int) -> dict:
        """Mock 模式全局搜索：在 mock_responses.json 的所有 app 配置里模拟匹配"""
        mock_data = self._load_mock_data()
        configs_data = mock_data.get('_configs', {})
        apps_data = mock_data.get('_apps', [])
        kw = (keyword or '').strip().lower()
        matches = []
        scanned = 0
        for app_id, ns_map in configs_data.items():
            app_info = next((a for a in apps_data if a.get('appId') == app_id), {})
            app_name = app_info.get('name', '') or app_id
            for ns_name, configs in ns_map.items():
                scanned += 1
                for k, v in configs.items():
                    k_lower = str(k).lower()
                    v_lower = str(v).lower()
                    hit = False
                    if search_field in ('key', 'both') and kw in k_lower:
                        hit = True
                    if not hit and search_field in ('value', 'both') and kw in v_lower:
                        hit = True
                    if hit:
                        matches.append({
                            'apolloHostId': -1,  # mock
                            'hostName': 'Mock-Apollo',
                            'appId': app_id,
                            'appName': app_name,
                            'namespaceName': ns_name,
                            'key': k,
                            'value': v,
                        })
        # 脱敏
        for m in matches:
            if _is_sensitive_key(m['key']):
                m['value'] = _mask_value(m['value'])
                m['masked'] = True
        truncated = False
        if len(matches) > max_results:
            matches = matches[:max_results]
            truncated = True
        print(f"[MOCK] [apollo-global-search] keyword='{keyword}', matches={len(matches)}, scanned={scanned}")
        return {
            "code": 200,
            "message": "success",
            "data": {
                "keyword": keyword,
                "searchField": search_field,
                "env": "PRO",
                "namespace": "application",
                "summary": {
                    "hostCount": 1,
                    "appCount": len(configs_data),
                    "scannedApps": scanned,
                    "matchedCount": len(matches),
                    "maskedCount": sum(1 for m in matches if m.get('masked')),
                    "elapsedMs": 50,
                },
                "matches": matches,
                "partial": False,
                "truncated": truncated,
            }
        }


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
        "description": "查询 Apollo 配置中心的配置项列表。查询链路：哪套Apollo(apolloHostId) → 环境(env) → 应用(appId) → 集群(clusterName) → Namespace(namespaceName) → 具体配置(key)。apolloHostId 缺省时自动遍历所有可用套定位应用所在套（结果带 usedHostId/hostName 标注实际使用的套），env 缺省为 PRO。支持按 Key 关键词过滤。当用户未提供 appId 时，先调用 apollo_app_list 获取可选应用列表（其中 usedHostId 为实际可用套），让用户选择 appId 后再用本工具查询。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "apolloHostId": {"type": "integer", "description": "Apollo 服务 ID，来自 apollo_host_list 返回的 apolloHostId，用于指定查询哪一套 Apollo；不传时自动遍历所有可用套定位应用所在套"},
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
        "description": "获取 Apollo 服务中的应用列表（AppId、应用名称、所属部门）。apolloHostId 缺省时自动遍历所有可用套，取第一个能成功返回应用列表的套（结果顶层带 usedHostId/hostName 标注实际使用的套）。当用户想查配置但未提供 appId 时，先调用本工具获取可选应用列表，让用户选择后再用选中的 appId 调用 apollo_config_query。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "apolloHostId": {"type": "integer", "description": "Apollo 服务 ID，来自 apollo_host_list 返回的 apolloHostId，用于指定查询哪一套 Apollo；不传时使用默认 Apollo"}
            }
        }
    },
    {
        "name": "apollo_config_global_search",
        "description": "【全局配置搜索】并发遍历所有可用 Apollo 套的所有应用，按关键词匹配配置的 Key 或 Value。解决日常排障高频需求：'哪些应用的配置用到了某个地址/关键词'、'比对所有应用是否都有某个配置项'。输入 keyword（必填），自动扫描所有 host × 所有 app × 配置，单次调用返回结果（预计 30-60 秒，比逐个应用调 apollo_config_query 快几十倍）。结果每条标注 host_id/app_id/namespace/key/value，超时返回部分结果+未完成标记。敏感字段已脱敏。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "keyword": {"type": "string", "description": "搜索关键词（必填），如 'vcpinner.cn:13302'、'password'、'timeout'、'redis.password'"},
                "searchField": {"type": "string", "description": "匹配字段：'key' 只匹配配置项 Key，'value' 只匹配值，'both' 都匹配。默认 'both'"},
                "env": {"type": "string", "description": "环境，默认 'PRO'。如需搜索开发环境配置传 'DEV'"},
                "maxResults": {"type": "integer", "description": "最多返回多少条匹配结果（默认 100，避免结果过多）"},
                "namespaceName": {"type": "string", "description": "搜索哪个 namespace 的配置（默认 'application'；设为 'application' 时自动扩展扫描 datasource/redis/common/logging 等常见 namespace）"}
            },
            "required": ["keyword"]
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
        logger.info(f"[MCP] 执行工具: tool={tool_name}, arguments={_fmt(arguments)}")
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
        elif tool_name == "apollo_config_global_search":
            keyword = arguments.get("keyword")
            if not keyword:
                raise ValueError("keyword 是必填参数，请指定搜索关键词")
            return client.query_global_search(
                keyword=keyword,
                search_field=arguments.get("searchField", "both"),
                env=arguments.get("env", "PRO"),
                max_results=int(arguments.get("maxResults", 100)),
                namespace_name=arguments.get("namespaceName", "application"),
            )
        else:
            raise ValueError(f"Unknown tool: {tool_name}")
    
    def handle_message(self, message: dict) -> Tuple[dict, bool]:
        jsonrpc = message.get("jsonrpc")
        request_id = message.get("id")
        method = message.get("method")
        params = message.get("params", {})
        
        if jsonrpc != "2.0":
            return self._build_response(request_id, error=self._build_error(-32600, "Invalid Request")), False

        logger.info(f"[MCP] 收到请求: id={request_id}, method={method}, params={_fmt(params)}")

        try:
            if method == "initialize":
                logger.info("[MCP] 处理 initialize 握手")
                # 标准 MCP 握手：返回协议版本、服务端能力与标识
                client_version = params.get("protocolVersion", "2024-11-05")
                response = self._build_response(request_id, {
                    "protocolVersion": client_version,
                    "capabilities": {
                        "tools": {"listChanged": False}
                    },
                    "serverInfo": {
                        "name": "apollo-config-query-mcp",
                        "version": "3.2.0"
                    }
                })
                return response, False

            elif method == "notifications/initialized":
                # 通知类消息无 id，无需响应
                logger.info("[MCP] 处理 notifications/initialized")
                return {}, False
            
            elif method == "tools/list":
                logger.info("[MCP] 处理 tools/list")
                response = self._build_response(request_id, {"tools": MCP_TOOLS})
                return response, False
            
            elif method == "tools/call":
                tool_name = params.get("name")
                arguments = params.get("arguments", {})
                logger.info(f"[MCP] 收到 tools/call: tool={tool_name}, arguments={_fmt(arguments)}")
                
                if not tool_name:
                    return self._build_response(request_id, error=self._build_error(-32602, "tool name is required")), False
                
                try:
                    start_time = time.time()
                    result = self._execute_tool(tool_name, arguments)
                    duration = time.time() - start_time
                    logger.info(f"[MCP] 工具执行完成: tool={tool_name}, 耗时={round(duration*1000)}ms, result={_fmt(result)}")
                    
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
                    logger.error(f"[MCP] 工具参数校验失败: tool={tool_name}, error={e}")
                    return self._build_response(request_id, error=self._build_error(-32602, str(e))), False
                except Exception as e:
                    logger.error(f"[MCP] 工具执行异常: tool={tool_name}, error={e}")
                    return self._build_response(request_id, error=self._build_error(-32603, f"Internal error: {str(e)}")), False
            
            else:
                return self._build_response(request_id, error=self._build_error(-32601, f"Method not found: {method}")), False
        
        except Exception as e:
            logger.error(f"[MCP] 消息处理异常: method={method}, error={e}")
            return self._build_response(request_id, error=self._build_error(-32603, f"Internal error: {str(e)}")), False


mcp_handler = MCPProtocolHandler()

# ============================================================================
# HTTP 服务层
# ============================================================================

if HAS_FASTAPI:
    app = FastAPI(
        title="apollo-config-query-mcp",
        description="Apollo 配置查询 MCP 服务 - 为 Apollo 配置中心提供标准化工具调用接口",
        version="3.1.0"
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
            "version": "3.2.0",
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
            "version": "3.2.0",
            "environment": client.config.current_env,
            "mock_enabled": USE_MOCK,
            "log_enabled": _LOG_ENABLED,
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
    
    @app.post("/log/off", tags=["日志管理"])
    def log_off():
        """关闭日志（立即生效，无需重启）：完全静默，不再记录任何日志"""
        _apply_log_enabled(False)
        return {"code": 200, "message": "日志已关闭", "log_enabled": False}
    
    @app.post("/log/on", tags=["日志管理"])
    def log_on():
        """开启日志（立即生效，无需重启）"""
        _apply_log_enabled(True)
        return {"code": 200, "message": "日志已开启", "log_enabled": True}
    
    @app.get("/log/status", tags=["日志管理"])
    def log_status():
        """查询日志开关当前状态"""
        return {"code": 200, "log_enabled": _LOG_ENABLED}
    
    @app.post("/mcp", tags=["MCP 协议"])
    async def mcp_endpoint(request: dict):
        logger.info(f"[HTTP] /mcp 收到请求: {_fmt(request)}")
        response, _ = mcp_handler.handle_message(request)
        logger.info(f"[HTTP] /mcp 返回: {_fmt(response)}")
        return JSONResponse(content=response)
    
    @app.get("/tools", tags=["平台兼容"])
    def platform_tools_endpoint():
        logger.info("[HTTP] GET /tools 返回工具列表")
        return JSONResponse(content={"tools": MCP_TOOLS})
    
    @app.get("/tools/list", tags=["平台兼容"])
    def platform_tools_list_endpoint():
        logger.info("[HTTP] GET /tools/list 返回工具列表")
        return JSONResponse(content={"tools": MCP_TOOLS})
    
    @app.post("/tools/call", tags=["平台兼容"])
    def platform_tools_call(request: dict):
        params = request.get("params", {})
        tool_name = params.get("name")
        arguments = params.get("arguments", {})
        logger.info(f"[HTTP] POST /tools/call 收到请求: tool={tool_name}, arguments={_fmt(arguments)}")
        try:
            result = mcp_handler._execute_tool(tool_name, arguments)
            logger.info(f"[HTTP] POST /tools/call 返回: tool={tool_name}, result={_fmt(result)}")
            return JSONResponse(content={
                "jsonrpc": "2.0",
                "id": request.get("id", 1),
                "result": {
                    "content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]
                }
            })
        except Exception as e:
            logger.error(f"[HTTP] POST /tools/call 执行异常: tool={tool_name}, error={e}")
            return JSONResponse(content={
                "jsonrpc": "2.0",
                "id": request.get("id", 1),
                "error": {"code": -32603, "message": str(e)}
            })
    
    @app.post("/api/config", tags=["兼容层 - 配置查询"])
    def http_config_query(params: ApolloConfigQueryParams):
        logger.info(f"[HTTP] POST /api/config 收到请求: {_fmt(params.model_dump())}")
        result = client.query_config(
            appId=params.appId,
            env=params.env,
            clusterName=params.clusterName,
            namespaceName=params.namespaceName,
            key=params.key,
            apolloHostId=params.apolloHostId
        )
        logger.info(f"[HTTP] POST /api/config 返回: {_fmt(result)}")
        return JSONResponse(content=result)
    
    @app.get("/api/apps", tags=["兼容层 - 应用列表"])
    def http_apps_list():
        logger.info("[HTTP] GET /api/apps 收到请求")
        result = client.query_apps()
        logger.info(f"[HTTP] GET /api/apps 返回: {_fmt(result)}")
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
