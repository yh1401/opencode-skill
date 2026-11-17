#!/usr/bin/env python3
"""
架构优化智能体 MCP Server - arch-opt-mcp-server

从业务拓扑接口 / Prometheus / Pinpoint / 云主机宿主机关系接口采集数据，
由规则引擎做确定性判定（结果四态：已发现风险 / 未触发规则 / 数据不足 / 不适用），
供上层 Skill 薄指令层组装 Prompt 后由大模型生成架构优化报告。

遵循标准 MCP 协议规范：
- SSE 传输模式：GET /sse + POST /messages
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
import re
import sys
import argparse
import time
from typing import Optional, Dict, Any, List, Tuple
from datetime import datetime, timedelta

_script_dir = os.path.dirname(os.path.abspath(__file__))
_root_dir = os.path.dirname(_script_dir)
_config_dir = os.path.join(_root_dir, 'config')
_mock_dir = os.path.join(_root_dir, 'mock')

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


# ============================================================================
# 结果四态定义
# ============================================================================

STATUS_RISK = "risk"          # 🟥 已发现风险
STATUS_OK = "ok"              # 🟩 未触发规则
STATUS_INSUFFICIENT = "insufficient"  # 🟨 数据不足
STATUS_NA = "not_applicable"  # ⚪ 不适用


# ============================================================================
# 配置加载
# ============================================================================

class ArchConfig:
    """加载 datasources.json / thresholds.json，支持环境变量覆盖"""

    def __init__(self):
        self.proxy_url = "https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince"
        self.province_detail = {"province": "大模型", "appId": "", "appSecret": ""}
        self.prometheus_base = "http://183.56.210.90:8082"
        self.prometheus_query_path = "/api/v1/query"
        self.prometheus_range_path = "/api/v1/query_range"
        self.business_endpoints = {}
        self.default_product = "天翼看家"
        self.center_nodes = {"贵州": "云公司->贵州", "广州4c": "云公司->广州4c"}
        self.thresholds = {}
        self.thresholds_version = "unknown"
        self.load()

    def load(self):
        ds_path = os.path.join(_config_dir, 'datasources.json')
        if os.path.exists(ds_path):
            with open(ds_path, 'r', encoding='utf-8') as f:
                data = json.load(f)
            proxy = data.get('proxy', {})
            self.proxy_url = proxy.get('url', self.proxy_url)
            self.province_detail = proxy.get('provinceDetail', self.province_detail)
            prom = data.get('prometheus', {})
            self.prometheus_base = prom.get('base_url', self.prometheus_base)
            self.prometheus_query_path = prom.get('query_path', self.prometheus_query_path)
            self.prometheus_range_path = prom.get('query_range_path', self.prometheus_range_path)
            self.business_endpoints = data.get('business_endpoints', {})
            self.default_product = data.get('default_product', self.default_product)
            self.center_nodes = data.get('center_nodes', self.center_nodes)
            logger.log_info(f"加载数据源配置: prometheus={self.prometheus_base}, "
                            f"endpoints={list(self.business_endpoints.keys())}")
        else:
            logger.log_warning(f"数据源配置文件不存在: {ds_path}")

        # 环境变量覆盖（敏感凭据优先放 .env）
        self.proxy_url = os.environ.get('ARCH_PROXY_URL', self.proxy_url)
        self.prometheus_base = os.environ.get('ARCH_PROMETHEUS_URL', self.prometheus_base)
        self.province_detail = dict(self.province_detail)
        self.province_detail['appId'] = os.environ.get('ARCH_APP_ID', self.province_detail.get('appId', ''))
        self.province_detail['appSecret'] = os.environ.get('ARCH_APP_SECRET', self.province_detail.get('appSecret', ''))
        self.province_detail['province'] = os.environ.get('ARCH_PROVINCE', self.province_detail.get('province', '大模型'))

        th_path = os.path.join(_config_dir, 'thresholds.json')
        if os.path.exists(th_path):
            with open(th_path, 'r', encoding='utf-8') as f:
                self.thresholds = json.load(f)
            self.thresholds_version = self.thresholds.get('version', 'unknown')
            logger.log_info(f"加载阈值配置: version={self.thresholds_version}")
        else:
            logger.log_warning(f"阈值配置文件不存在: {th_path}")

    # 阈值便捷读取
    def th(self, group: str, key: str, default=None):
        return self.thresholds.get(group, {}).get(key, default)


config = ArchConfig()


# ============================================================================
# 数据采集客户端
# ============================================================================

class ArchDataClient:
    """统一封装：拓扑代理 / Prometheus 请求，含超时、重试、Mock 降级"""

    def __init__(self):
        self._session = None
        self._request_count = 0
        self._success_count = 0
        self._mock_count = 0
        self._mock_data = {}
        self._mock_prom = {}
        self._load_mock()

    def _load_mock(self):
        biz_path = os.path.join(_mock_dir, 'mock_responses.json')
        prom_path = os.path.join(_mock_dir, 'prometheus_mock.json')
        try:
            if os.path.exists(biz_path):
                with open(biz_path, 'r', encoding='utf-8') as f:
                    self._mock_data = json.load(f)
            if os.path.exists(prom_path):
                with open(prom_path, 'r', encoding='utf-8') as f:
                    self._mock_prom = json.load(f)
        except Exception as e:
            logger.log_error(f"加载 Mock 数据失败: {e}")

    def _ensure_session(self):
        if HAS_REQUESTS and self._session is None:
            self._session = requests.Session()
            proxy_env = os.environ.get('HTTP_PROXY') or os.environ.get('http_proxy')
            if proxy_env:
                self._session.proxies = {'http': proxy_env, 'https': proxy_env}
                logger.log_info(f"HTTP 会话已配置代理: {proxy_env}")

    @property
    def _timeout(self) -> int:
        return int(os.environ.get('ARCH_REQUEST_TIMEOUT', '8'))

    @property
    def _max_retries(self) -> int:
        return int(os.environ.get('ARCH_MAX_RETRIES', '1'))

    @property
    def _retry_delay(self) -> float:
        return float(os.environ.get('ARCH_RETRY_DELAY', '1'))

    def _get_mock_business(self, endpoint_key: str) -> dict:
        self._mock_count += 1
        logger.log_mock_usage(endpoint_key, "business api fallback")
        if endpoint_key in self._mock_data:
            return self._mock_data[endpoint_key]
        return {"code": 500, "msg": f"无 Mock 数据: {endpoint_key}", "data": None}

    def _get_mock_prom(self, metric_key: str) -> dict:
        self._mock_count += 1
        logger.log_mock_usage(metric_key, "prometheus fallback")
        empty = {"status": "success", "data": {"resultType": "vector", "result": []}}
        return self._mock_prom.get(metric_key, empty)

    def call_business(self, endpoint_key: str, req_param: dict) -> dict:
        """经统一拓扑代理调用业务接口"""
        self._request_count += 1
        ep = config.business_endpoints.get(endpoint_key)
        if not ep:
            if USE_MOCK:
                return self._get_mock_business(endpoint_key)
            logger.log_error(f"[BIZ] 未找到业务端点: {endpoint_key}")
            return {"code": 500, "msg": f"未找到业务端点: {endpoint_key}", "data": None}

        payload = {
            "url": ep.get('url'),
            "types": ep.get('types', 'GET'),
            "provinceDetail": config.province_detail,
            "reqParam": req_param,
        }

        if not HAS_REQUESTS:
            if USE_MOCK:
                return self._get_mock_business(endpoint_key)
            return {"code": 500, "msg": "requests 模块未安装", "data": None}

        logger.log_api_request(endpoint_key, "POST", config.proxy_url, req_param)

        for attempt in range(self._max_retries + 1):
            start = time.time()
            try:
                self._ensure_session()
                resp = self._session.post(config.proxy_url, json=payload, timeout=self._timeout)
                duration = time.time() - start
                resp.raise_for_status()
                result = resp.json()
                self._success_count += 1
                logger.log_api_response(endpoint_key, resp.status_code, duration, True)
                return result
            except requests.exceptions.Timeout:
                logger.log_warning(f"[BIZ] {endpoint_key} 超时 attempt={attempt + 1}/{self._max_retries + 1}")
                if attempt < self._max_retries:
                    time.sleep(self._retry_delay)
                    continue
                if USE_MOCK:
                    return self._get_mock_business(endpoint_key)
                return {"code": 504, "msg": "接口请求超时", "data": None}
            except requests.exceptions.ConnectionError:
                logger.log_warning(f"[BIZ] {endpoint_key} 连接失败 attempt={attempt + 1}/{self._max_retries + 1}")
                if attempt < self._max_retries:
                    time.sleep(self._retry_delay)
                    continue
                if USE_MOCK:
                    return self._get_mock_business(endpoint_key)
                return {"code": 503, "msg": "接口连接失败", "data": None}
            except Exception as e:
                logger.log_error(f"[BIZ] {endpoint_key} 请求异常: {e}")
                if USE_MOCK:
                    return self._get_mock_business(endpoint_key)
                return {"code": 500, "msg": f"接口请求异常: {e}", "data": None}

    def prom_query(self, promql: str, mock_key: Optional[str] = None) -> dict:
        """Prometheus 即时/窗口查询（promql 已含 [15m] 等窗口表达式）"""
        self._request_count += 1
        url = f"{config.prometheus_base}{config.prometheus_query_path}"
        params = {"query": promql}

        if not HAS_REQUESTS:
            if USE_MOCK and mock_key:
                return self._get_mock_prom(mock_key)
            return {"status": "error", "data": {"resultType": "vector", "result": []}}

        for attempt in range(self._max_retries + 1):
            start = time.time()
            try:
                self._ensure_session()
                resp = self._session.get(url, params=params, timeout=self._timeout)
                duration = time.time() - start
                resp.raise_for_status()
                result = resp.json()
                self._success_count += 1
                logger.log_api_response(f"prom:{mock_key or 'query'}", resp.status_code, duration, True)
                return result
            except Exception as e:
                logger.log_warning(f"[PROM] 查询失败 attempt={attempt + 1}/{self._max_retries + 1}: {e}")
                if attempt < self._max_retries:
                    time.sleep(self._retry_delay)
                    continue
                if USE_MOCK and mock_key:
                    return self._get_mock_prom(mock_key)
                return {"status": "error", "data": {"resultType": "vector", "result": []}}

    def stats(self) -> dict:
        return {
            "requests": self._request_count,
            "success": self._success_count,
            "mock_fallback": self._mock_count,
        }


client = ArchDataClient()


# ============================================================================
# 公共工具函数
# ============================================================================

def _now_iso() -> str:
    return datetime.now().strftime('%Y-%m-%d %H:%M:%S')


def _meta(node: str = None, window: str = None, extra: dict = None) -> dict:
    """统一结果元信息：任务标识、采集时间、分析窗口、规则版本"""
    m = {
        "collected_at": _now_iso(),
        "rule_version": config.thresholds_version,
    }
    if node:
        m["node"] = node
    if window:
        m["window"] = window
    if extra:
        m.update(extra)
    return m


def _is_dep(name: str) -> bool:
    """第一阶段仅对 -dep 结尾应用执行容器检查"""
    return isinstance(name, str) and name.endswith('-dep')


def _prom_vector(result: dict) -> List[dict]:
    """从 Prometheus 响应中提取 vector 结果"""
    try:
        if result.get('status') != 'success':
            return []
        return result.get('data', {}).get('result', []) or []
    except Exception:
        return []


def _val(item: dict) -> Optional[float]:
    """提取 vector 某条记录的数值"""
    try:
        return float(item.get('value', [None, None])[1])
    except (TypeError, ValueError, IndexError):
        return None


def _result(status: str, conclusion: str, **kwargs) -> dict:
    base = {"status": status, "conclusion": conclusion}
    base.update(kwargs)
    return base


# ============================================================================
# 工具实现：中心节点
# ============================================================================

def tool_module_topology(node: str, product_name: str = None, only_dep: bool = True) -> dict:
    """获取模块与应用归属（拓扑接口 getTopologyByProductName）"""
    product_name = product_name or config.default_product
    resp = client.call_business('getTopologyByProductName', {
        "center": "true",
        "node": node,
        "productName": product_name,
    })

    if resp.get('code') != 0 or not resp.get('data'):
        return _result(STATUS_INSUFFICIENT, "模块拓扑数据获取失败，无法建立模块与应用归属",
                       **_meta(node), coverage={"expected": 1, "actual": 0},
                       raw={"code": resp.get('code'), "msg": resp.get('msg')},
                       error="业务接口未返回有效数据")

    modules = []
    deploy_node = None
    for item in resp.get('data', []):
        deploy_node = deploy_node or item.get('deployNode')
        topo = item.get('topologyData', {}) or {}
        for n in topo.get('nodes', []) or []:
            projects = n.get('projectName', []) or []
            covered = [p for p in projects if _is_dep(p)] if only_dep else projects
            uncovered = [p for p in projects if not _is_dep(p)] if only_dep else []
            modules.append({
                "node": item.get('name', node),
                "module": n.get('nameCn') or n.get('name'),
                "module_id": n.get('name'),
                "object_type": n.get('objectType'),
                "project_count": len(projects),
                "projects": projects,
                "covered_projects": covered,
                "uncovered_projects": uncovered,
                "coverage_status": STATUS_OK if covered else STATUS_NA,
            })

    if not modules:
        return _result(STATUS_INSUFFICIENT, "接口返回成功但未解析到模块节点",
                       **_meta(node), raw=resp)

    total_projects = sum(len(m['projects']) for m in modules)
    covered_projects = sum(len(m['covered_projects']) for m in modules)
    return _result(STATUS_OK, f"获取到 {len(modules)} 个模块，共 {total_projects} 个应用",
                   **_meta(node),
                   deploy_node=deploy_node,
                   modules=modules,
                   summary={
                       "module_count": len(modules),
                       "project_count": total_projects,
                       "covered_dep_count": covered_projects,
                       "uncovered_count": total_projects - covered_projects,
                   },
                   note="第一阶段仅对 -dep 结尾应用执行容器检查，其余标注 ⚪ 不适用")


def _deployments_for(node: str, deployments: List[str], product_name: str = None) -> Tuple[Optional[List[str]], Optional[str]]:
    """若未显式传入 deployments，则从模块拓扑自动派生 -dep 应用列表"""
    if deployments:
        return deployments, None
    topo = tool_module_topology(node, product_name)
    if topo.get('status') == STATUS_INSUFFICIENT:
        return None, "未能从模块拓扑派生应用列表，请显式传入 deployments"
    derived = []
    for m in topo.get('modules', []):
        derived.extend(m.get('covered_projects', []))
    derived = sorted(set(derived))
    if not derived:
        return None, "模块拓扑中未找到 -dep 结尾的应用"
    return derived, None


# ReplicaSet 生成的 Pod 名：<deployment>-<rs-hash>-<pod-suffix>
_POD_DEP_RE = r'(.+)-[a-z0-9]+-[a-z0-9]+'
_POD_DEP_PATTERN = re.compile(r'^(.+)-[a-z0-9]+-[a-z0-9]+$')
_DEP_POD_REGEX = r'.+-dep-.*'


def _dep_from_pod(pod: str) -> Optional[str]:
    """从 Pod 名反推 deployment（仅适用 ReplicaSet 命名）"""
    if not pod:
        return None
    m = _POD_DEP_PATTERN.match(pod)
    return m.group(1) if m else None


def _group_by_dep_ns(vec: List[dict]) -> Dict[Tuple[str, str], List[dict]]:
    """按 (namespace, deployment) 分组；deployment 缺失时从 pod 名派生"""
    grouped: Dict[Tuple[str, str], List[dict]] = {}
    for item in vec:
        metric = item.get('metric', {}) or {}
        dep = metric.get('deployment') or _dep_from_pod(metric.get('pod'))
        if not dep:
            continue
        ns = metric.get('namespace', '') or ''
        grouped.setdefault((ns, dep), []).append(item)
    return grouped


def _index_by_dep(grouped: Dict[Tuple[str, str], List[dict]]) -> Dict[str, Dict[str, List[dict]]]:
    """把 (namespace, deployment) 分组索引为 deployment -> namespace -> items"""
    idx: Dict[str, Dict[str, List[dict]]] = {}
    for (ns, dep), items in grouped.items():
        idx.setdefault(dep, {}).setdefault(ns, []).extend(items)
    return idx


def _max_val(items: List[dict]) -> Optional[float]:
    vals = [v for v in (_val(i) for i in items) if v is not None]
    return max(vals) if vals else None


def _label_replace(expr: str) -> str:
    """从 pod 名派生 deployment 标签"""
    return f'label_replace({expr},"deployment","$1","pod","{_POD_DEP_RE}")'


def _pod_filter(node: str) -> str:
    return (f'cmdb_node="{node}",pod=~"{_DEP_POD_REGEX}",'
            f'container!="",container!="POD"')


def tool_replica(node: str, deployments: List[str] = None, product_name: str = None) -> dict:
    """D1 副本数量与可用性"""
    deps, err = _deployments_for(node, deployments, product_name)
    if err:
        return _result(STATUS_INSUFFICIENT, err, **_meta(node), deployments=[])

    spec_vec = _prom_vector(client.prom_query(
        f'kube_deployment_spec_replicas{{cmdb_node="{node}"}}', 'kube_deployment_spec_replicas'))
    avail_vec = _prom_vector(client.prom_query(
        f'kube_deployment_status_replicas_available{{cmdb_node="{node}"}}', 'kube_deployment_status_replicas_available'))

    spec_idx = _index_by_dep(_group_by_dep_ns(spec_vec))
    avail_idx = _index_by_dep(_group_by_dep_ns(avail_vec))

    min_replicas = config.th('replica', 'min_replicas', 2)
    check_equal = config.th('replica', 'check_available_equal', True)
    note = config.th('replica', 'min_replicas_note', '')

    items = []
    counts = {STATUS_RISK: 0, STATUS_OK: 0, STATUS_INSUFFICIENT: 0, STATUS_NA: 0}
    for dep in deps:
        if not _is_dep(dep):
            counts[STATUS_NA] += 1
            items.append(_result(STATUS_NA, "非 -dep 应用，本阶段未覆盖", deployment=dep))
            continue
        by_ns = spec_idx.get(dep)
        if not by_ns:
            counts[STATUS_INSUFFICIENT] += 1
            items.append(_result(STATUS_INSUFFICIENT, "未查询到期望副本指标",
                                 deployment=dep, evidence={"promql": "kube_deployment_spec_replicas"}))
            continue

        avail_by_ns = avail_idx.get(dep, {})
        for ns, spec_items in sorted(by_ns.items()):
            spec = int(_max_val(spec_items) or 0)
            avail_items = avail_by_ns.get(ns) or next(iter(avail_by_ns.values()), [])
            avail = int(_max_val(avail_items)) if avail_items else None

            problems = []
            if spec < min_replicas:
                problems.append(f"副本不足：期望 {spec} < 冗余要求 {min_replicas}")
            if check_equal and avail is not None and avail < spec:
                problems.append(f"副本不可用：可用 {avail} < 期望 {spec}")

            if problems:
                counts[STATUS_RISK] += 1
                items.append(_result(STATUS_RISK, "；".join(problems), deployment=dep, namespace=ns or None,
                                     spec_replicas=spec, available_replicas=avail,
                                     evidence={"spec": spec, "available": avail, "min_replicas": min_replicas}))
            else:
                counts[STATUS_OK] += 1
                items.append(_result(STATUS_OK, "副本数量与可用性正常", deployment=dep, namespace=ns or None,
                                     spec_replicas=spec, available_replicas=avail))

    return _result(STATUS_RISK if counts[STATUS_RISK] else STATUS_OK,
                   f"检查 {len(deps)} 个应用：命中风险 {counts[STATUS_RISK]} 个",
                   **_meta(node), items=items, counts=counts,
                   threshold={"min_replicas": min_replicas, "note": note})


def tool_capacity(node: str, deployments: List[str] = None, product_name: str = None) -> dict:
    """D2 容量瓶颈：CPU 限流 / 内存容量 / HPA 扩容"""
    deps, err = _deployments_for(node, deployments, product_name)
    if err:
        return _result(STATUS_INSUFFICIENT, err, **_meta(node))

    dur = config.th('capacity', 'duration_minutes', 15)
    cpu_th = config.th('capacity', 'cpu_throttle_ratio', 0.20)
    mem_th = config.th('capacity', 'memory_usage_ratio', 0.85)
    window = f"{dur}m"
    counters = {STATUS_RISK: 0, STATUS_OK: 0, STATUS_INSUFFICIENT: 0, STATUS_NA: 0}

    cpu_items = []
    mem_items = []
    hpa_items = []

    # container_* 无 deployment 标签：全节点一次聚合，再从 pod 名派生 deployment
    sel = _pod_filter(node)
    cpu_promql = (
        f'sum by (namespace,deployment)({_label_replace(f"rate(container_cpu_cfs_throttled_seconds_total{{{sel}}}[{window}])")})'
        f' / sum by (namespace,deployment)({_label_replace(f"rate(container_cpu_cfs_periods_total{{{sel}}}[{window}])")})'
    )
    mem_limit_metric = (f'kube_pod_container_resource_limits_memory_bytes'
                        f'{{cmdb_node="{node}",pod=~"{_DEP_POD_REGEX}"}}')
    mem_promql = (
        f'sum by (namespace,deployment)({_label_replace(f"container_memory_working_set_bytes{{{sel}}}")})'
        f' / sum by (namespace,deployment)({_label_replace(mem_limit_metric)})'
    )
    hpa_promql = (
        f'(kube_horizontalpodautoscaler_status_current_replicas{{cmdb_node="{node}"}}'
        f' == on(namespace,horizontalpodautoscaler)'
        f' kube_horizontalpodautoscaler_spec_max_replicas{{cmdb_node="{node}"}})'
        f' * on(namespace,horizontalpodautoscaler) group_left(scaletargetref_name)'
        f' kube_horizontalpodautoscaler_info{{cmdb_node="{node}",scaletargetref_kind="Deployment"}}'
    )

    cpu_idx = _index_by_dep(_group_by_dep_ns(
        _prom_vector(client.prom_query(cpu_promql, 'container_cpu_throttle_ratio'))))
    mem_idx = _index_by_dep(_group_by_dep_ns(
        _prom_vector(client.prom_query(mem_promql, 'container_memory_usage_ratio'))))
    hpa_map: Dict[Tuple[str, str], float] = {}
    for item in _prom_vector(client.prom_query(hpa_promql, 'hpa_max_reached')):
        metric = item.get('metric', {}) or {}
        target = metric.get('scaletargetref_name')
        val = _val(item)
        if target and val is not None:
            hpa_map[(metric.get('namespace', '') or '', target)] = val

    for dep in deps:
        if not _is_dep(dep):
            counters[STATUS_NA] += 1
            continue

        # CPU 限流：限流周期比例
        cpu_by_ns = cpu_idx.get(dep, {})
        if not cpu_by_ns:
            counters[STATUS_INSUFFICIENT] += 1
            cpu_items.append(_result(STATUS_INSUFFICIENT, "未查询到 CPU 限流指标（需时间窗口证据）",
                                     deployment=dep, evidence={"promql": cpu_promql}))
        for ns, items in sorted(cpu_by_ns.items()):
            ratio = _max_val(items) or 0
            if ratio > cpu_th:
                counters[STATUS_RISK] += 1
                cpu_items.append(_result(STATUS_RISK, f"CPU 限流比例 {ratio:.1%} > {cpu_th:.0%}（持续 {dur}min）",
                                         deployment=dep, namespace=ns or None, ratio=ratio,
                                         evidence={"promql": cpu_promql, "observed": ratio,
                                                   "threshold": cpu_th, "duration": window}))
            else:
                counters[STATUS_OK] += 1
                cpu_items.append(_result(STATUS_OK, "CPU 限流正常", deployment=dep, namespace=ns or None,
                                         ratio=ratio))

        # 内存容量：工作集 / limit
        mem_by_ns = mem_idx.get(dep, {})
        if not mem_by_ns:
            counters[STATUS_INSUFFICIENT] += 1
            mem_items.append(_result(STATUS_INSUFFICIENT, "未查询到内存容量指标或 limit 缺失",
                                     deployment=dep, evidence={"promql": mem_promql}))
        for ns, items in sorted(mem_by_ns.items()):
            ratio = _max_val(items) or 0
            if ratio == 0:
                counters[STATUS_INSUFFICIENT] += 1
                mem_items.append(_result(STATUS_INSUFFICIENT, "内存 limit 可能缺失或为 0，口径待确认",
                                         deployment=dep, namespace=ns or None, ratio=ratio,
                                         evidence={"promql": mem_promql}))
            elif ratio > mem_th:
                counters[STATUS_RISK] += 1
                mem_items.append(_result(STATUS_RISK, f"内存使用率 {ratio:.1%} > {mem_th:.0%}（持续 {dur}min）",
                                         deployment=dep, namespace=ns or None, ratio=ratio,
                                         evidence={"promql": mem_promql, "observed": ratio,
                                                   "threshold": mem_th, "duration": window}))
            else:
                counters[STATUS_OK] += 1
                mem_items.append(_result(STATUS_OK, "内存容量正常", deployment=dep, namespace=ns or None,
                                         ratio=ratio))

        # HPA 扩容：无 HPA 指标统一 ⚪
        hpa_hits = [(ns, cur) for (ns, target), cur in hpa_map.items() if target == dep]
        if not hpa_hits:
            hpa_items.append(_result(STATUS_NA, "无 HPA 或未达到最大副本", deployment=dep))
        for ns, cur in hpa_hits:
            counters[STATUS_RISK] += 1
            hpa_items.append(_result(STATUS_RISK, f"HPA 已达最大副本 {cur:.0f}，需结合负载判断是否持续偏高",
                                     deployment=dep, namespace=ns or None, current=cur,
                                     evidence={"promql": hpa_promql,
                                               "threshold": config.th('capacity', 'hpa_max_reached_check')}))

    return _result(STATUS_RISK if counters[STATUS_RISK] else STATUS_OK,
                   f"容量检查：命中风险 {counters[STATUS_RISK]} 个",
                   **_meta(node, window=f"{dur}min"),
                   cpu=cpu_items, memory=mem_items, hpa=hpa_items, counts=counters,
                   threshold={"cpu_throttle_ratio": cpu_th, "memory_usage_ratio": mem_th,
                              "duration_minutes": dur})


def tool_load_balance(node: str, deployments: List[str] = None, product_name: str = None) -> dict:
    """D3 Pod 负载不均衡"""
    deps, err = _deployments_for(node, deployments, product_name)
    if err:
        return _result(STATUS_INSUFFICIENT, err, **_meta(node))

    ratio_th = config.th('load_balance', 'max_avg_ratio', 2.0)
    dur = config.th('load_balance', 'duration_minutes', 15)
    window = f"{dur}m"
    items = []
    counters = {STATUS_RISK: 0, STATUS_OK: 0, STATUS_INSUFFICIENT: 0, STATUS_NA: 0}

    sel = _pod_filter(node)
    pod_cpu = f'sum by (namespace,deployment,pod)({_label_replace(f"rate(container_cpu_usage_seconds_total{{{sel}}}[{window}])")})'
    promql = (f'max by (namespace,deployment)({pod_cpu})'
              f' / avg by (namespace,deployment)({pod_cpu})')
    pod_count_metric = f'kube_pod_info{{cmdb_node="{node}",pod=~"{_DEP_POD_REGEX}"}}'
    pod_count_promql = f'count by (namespace,deployment)({_label_replace(pod_count_metric)})'

    ratio_idx = _index_by_dep(_group_by_dep_ns(
        _prom_vector(client.prom_query(promql, 'pod_load_balance_ratio'))))
    pod_idx = _index_by_dep(_group_by_dep_ns(
        _prom_vector(client.prom_query(pod_count_promql, 'kube_pod_info'))))

    for dep in deps:
        if not _is_dep(dep):
            counters[STATUS_NA] += 1
            continue
        by_ns = ratio_idx.get(dep, {})
        if not by_ns:
            counters[STATUS_INSUFFICIENT] += 1
            items.append(_result(STATUS_INSUFFICIENT, "未查询到 Pod 负载指标（需时间窗口证据）",
                                 deployment=dep, evidence={"promql": promql}))
            continue
        pods_by_ns = pod_idx.get(dep, {})
        for ns, vec in sorted(by_ns.items()):
            ratio = _max_val(vec) or 0
            pods = int(_max_val(pods_by_ns.get(ns, [])) or 0)
            if pods == 1:
                counters[STATUS_NA] += 1
                items.append(_result(STATUS_NA, "仅 1 个 Pod，负载不均衡不适用",
                                     deployment=dep, namespace=ns or None, pod_count=pods))
            elif ratio > ratio_th:
                counters[STATUS_RISK] += 1
                items.append(_result(STATUS_RISK, f"最大/平均负载 {ratio:.2f} > {ratio_th}（持续 {dur}min）",
                                     deployment=dep, namespace=ns or None, ratio=ratio, pod_count=pods,
                                     evidence={"promql": promql, "observed": ratio, "threshold": ratio_th,
                                               "duration": window}))
            else:
                counters[STATUS_OK] += 1
                items.append(_result(STATUS_OK, "Pod 负载均衡正常", deployment=dep, namespace=ns or None,
                                     ratio=ratio, pod_count=pods))

    return _result(STATUS_RISK if counters[STATUS_RISK] else STATUS_OK,
                   f"负载均衡检查：命中风险 {counters[STATUS_RISK]} 个",
                   **_meta(node, window=f"{dur}min"), items=items, counts=counters,
                   threshold={"max_avg_ratio": ratio_th, "duration_minutes": dur})


def tool_oom(node: str, deployments: List[str] = None, product_name: str = None, days: int = None) -> dict:
    """D4 OOM 风险：近期 OOMKilled + 重启证据"""
    deps, err = _deployments_for(node, deployments, product_name)
    if err:
        return _result(STATUS_INSUFFICIENT, err, **_meta(node))

    lookback_days = days or config.th('oom', 'lookback_days', 7)
    lookback_note = config.th('oom', 'lookback_note', '')
    items = []
    counters = {STATUS_RISK: 0, STATUS_OK: 0, STATUS_INSUFFICIENT: 0, STATUS_NA: 0}

    oom_metric = (f'kube_pod_container_status_last_terminated_reason'
                  f'{{cmdb_node="{node}",pod=~"{_DEP_POD_REGEX}",reason="OOMKilled"}} == 1')
    oom_promql = _label_replace(f'max_over_time(({oom_metric})[{lookback_days}d:15m])')
    restart_metric = f'kube_pod_container_status_restarts_total{{cmdb_node="{node}",pod=~"{_DEP_POD_REGEX}"}}'
    restart_promql = f'max by (namespace,deployment)({_label_replace(restart_metric)})'

    oom_idx = _index_by_dep(_group_by_dep_ns(
        _prom_vector(client.prom_query(oom_promql, 'oom_killed'))))
    restart_idx = _index_by_dep(_group_by_dep_ns(
        _prom_vector(client.prom_query(restart_promql, 'container_restarts'))))

    for dep in deps:
        if not _is_dep(dep):
            counters[STATUS_NA] += 1
            continue
        oom_by_ns = oom_idx.get(dep, {})
        restart_by_ns = restart_idx.get(dep, {})

        if not oom_by_ns and not restart_by_ns:
            counters[STATUS_INSUFFICIENT] += 1
            items.append(_result(STATUS_INSUFFICIENT, "未查询到 OOM/重启指标，无法判定近期 OOM",
                                 deployment=dep, evidence={"promql": [oom_promql, restart_promql]}))
            continue

        if oom_by_ns:
            for ns in sorted(set(oom_by_ns) | set(restart_by_ns)):
                restarts = _max_val(restart_by_ns.get(ns, []))
                counters[STATUS_RISK] += 1
                items.append(_result(STATUS_RISK,
                                     f"近期出现 OOMKilled（近 {lookback_days} 天）"
                                     + (f"，重启次数 {restarts:.0f}" if restarts is not None else "，重启证据待确认"),
                                     deployment=dep, namespace=ns or None,
                                     evidence={"promql": oom_promql, "restarts": restarts,
                                               "lookback_days": lookback_days}))
        else:
            for ns, vec in sorted(restart_by_ns.items()):
                counters[STATUS_OK] += 1
                items.append(_result(STATUS_OK, "未发现近期 OOM", deployment=dep, namespace=ns or None,
                                     evidence={"restart_count": _max_val(vec)}))

    return _result(STATUS_RISK if counters[STATUS_RISK] else STATUS_OK,
                   f"OOM 检查：命中风险 {counters[STATUS_RISK]} 个",
                   **_meta(node, window=f"近 {lookback_days} 天"), items=items, counts=counters,
                   threshold={"lookback_days": lookback_days, "note": lookback_note})


def tool_link_dependency(deploy_node: str, projects: List[str] = None, minute: int = 5) -> dict:
    """D5 链路依赖（Pinpoint 拓扑接口 getTopologyByNodeProject）"""
    if not deploy_node:
        return _result(STATUS_INSUFFICIENT, "缺少 deploy_node，请先调用 mcp_module_topology 获取",
                       **_meta())
    resp = client.call_business('getTopologyByNodeProject', {
        "center": "true",
        "calleeRange": 1,
        "callerRange": 1,
        "filterNode": False,
        "minute": minute,
        "nodeType": "",
        "node": deploy_node,
        "projects": projects or [],
    })

    if resp.get('code') != 0 or not resp.get('data'):
        return _result(STATUS_INSUFFICIENT, "链路拓扑数据获取失败",
                       **_meta(window=f"{minute}min"),
                       raw={"code": resp.get('code'), "msg": resp.get('msg')})

    data = resp.get('data') or {}
    nodes = data.get('nodes', []) or []
    edges = data.get('edges', []) or []

    if not nodes and not edges:
        return _result(STATUS_INSUFFICIENT, "接口成功但未返回调用关系",
                       **_meta(window=f"{minute}min"), raw=data)

    thresholds_defined = (config.th('link_dependency', 'error_rate_threshold') is not None
                          or config.th('link_dependency', 'slow_call_threshold_ms') is not None)

    # 基础统计（不做阈值判定，供大模型分析）
    in_degree: Dict[str, int] = {}
    for e in edges:
        in_degree[e.get('to')] = in_degree.get(e.get('to'), 0) + 1
    concentrated = [{"node": k, "upstream_count": v} for k, v in in_degree.items() if v >= 2]

    return _result(STATUS_OK if not thresholds_defined else STATUS_OK,
                   "已获取链路拓扑；链路长度/依赖数/错误率阈值待联调制定",
                   **_meta(window=f"{minute}min"),
                   nodes=nodes, edges=edges,
                   analysis={"concentrated_dependencies": concentrated,
                             "node_count": len(nodes), "edge_count": len(edges)},
                   threshold_status="阈值待制定" if not thresholds_defined else "已配置",
                   note="拓扑连边仅代表观测到的调用关系，风险判断需结合调用量、耗时、错误率与业务重要性")


# ============================================================================
# 工具实现：省节点故障域
# ============================================================================

def tool_node_list(filter_prefix: str = "云公司") -> dict:
    """获取节点列表，筛选名称以指定前缀开头的节点"""
    resp = client.call_business('getNode', {"center": "true"})
    if resp.get('code') != 0 or not resp.get('data'):
        return _result(STATUS_INSUFFICIENT, "节点列表获取失败", **_meta())

    nodes = []
    for n in resp.get('data', []) or []:
        name = n.get('node', '')
        if filter_prefix and not name.startswith(filter_prefix):
            continue
        nodes.append({
            "node": name,
            "node_id": n.get('node_id'),
            "ctyun_node_code3": n.get('ctyun_node_code3') or "",
            "province_name": n.get('province_name'),
            "flag": n.get('flag'),
            "has_code3": bool(n.get('ctyun_node_code3')),
        })

    missing_code3 = [n['node'] for n in nodes if not n['has_code3']]
    return _result(STATUS_OK, f"筛选到 {len(nodes)} 个节点（前缀 {filter_prefix}）",
                   **_meta(), items=nodes,
                   summary={"total": len(nodes), "missing_code3": len(missing_code3)},
                   missing_code3=missing_code3,
                   note="ctyun_node_code3 为空的节点无法进行宿主机映射，需记录为数据不足")


def tool_virtual_structure(ctyun_node_code3: str) -> dict:
    """物理机 ↔ 云主机关系（getVirtualStructure）"""
    if not ctyun_node_code3:
        return _result(STATUS_INSUFFICIENT, "缺少 ctyun_node_code3，无法查询宿主机关系", **_meta())
    resp = client.call_business('getVirtualStructure', {
        "center": "true",
        "node": ctyun_node_code3,
    })
    if resp.get('code') != 0 or not resp.get('data'):
        return _result(STATUS_INSUFFICIENT, "宿主机关系获取失败", **_meta(node=ctyun_node_code3))

    ip_data = (resp.get('data') or {}).get('ipData', {}) or {}
    reverse: Dict[str, str] = {}
    conflicts = []
    for host_ip, vm_ips in ip_data.items():
        for vm in (vm_ips or []):
            if vm in reverse and reverse[vm] != host_ip:
                conflicts.append({"vm_ip": vm, "hosts": [reverse[vm], host_ip]})
            else:
                reverse[vm] = host_ip

    return _result(STATUS_OK, f"获取 {len(ip_data)} 台宿主机，{len(reverse)} 台云主机映射",
                   **_meta(node=ctyun_node_code3), ip_data=ip_data,
                   vm_to_host=reverse, conflicts=conflicts,
                   note="映射冲突需核查数据更新时间及迁移记录")


def tool_topology_info(node: str) -> dict:
    """项目 ↔ 云主机部署关系（getTopologyInfo）"""
    resp = client.call_business('getTopologyInfo', {"center": "true", "node": node})
    if resp.get('code') != 0 or not resp.get('data'):
        return _result(STATUS_INSUFFICIENT, "项目部署关系获取失败", **_meta(node=node))

    data = resp.get('data') or {}
    return _result(STATUS_OK, "获取项目与云主机部署关系",
                   **_meta(node=node),
                   project_data=data.get('projectData', []),
                   project_ip_data=data.get('projectIpData', {}),
                   ip_project_data=data.get('ipProjectData', {}))


def tool_fault_domain(node: str, min_instances: int = None) -> dict:
    """D6 省节点故障域：汇总「项目 → 云主机 → 物理宿主机」映射并判定集中度"""
    min_inst = min_instances if min_instances is not None else config.th('fault_domain', 'min_instance_to_check', 1)
    max_concentration = config.th('fault_domain', 'max_host_concentration', 0.5)

    # 前置：需先由 node_list 获得 ctyun_node_code3
    node_resp = tool_node_list("云公司")
    code3 = None
    if node_resp.get('status') == STATUS_OK:
        for n in node_resp.get('items', []):
            if n['node'] == node:
                code3 = n.get('ctyun_node_code3')
                break

    vs = tool_virtual_structure(code3) if code3 else None
    ti = tool_topology_info(node)

    if ti.get('status') == STATUS_INSUFFICIENT:
        return _result(STATUS_INSUFFICIENT, "项目部署关系缺失，无法判定故障域",
                       **_meta(node=node), raw=ti)

    project_ip = ti.get('project_ip_data', {}) or {}
    vm_to_host = (vs or {}).get('vm_to_host', {}) if vs else {}

    if not vm_to_host:
        # 无法映射宿主机（如 ctyun_node_code3 为空）
        projects = list(project_ip.keys())
        return _result(STATUS_INSUFFICIENT,
                       "宿主机映射数据不足（节点 ctyun_node_code3 可能为空），仅能输出已知部署分布",
                       **_meta(node=node),
                       items=[{"project": p, "instances": len(project_ip.get(p, [])),
                               "host_count": None, "max_concentration": None,
                               "status": STATUS_INSUFFICIENT} for p in projects],
                       coverage={"mapped": 0, "total": len(projects)},
                       note="需补齐 ctyun_node_code3 或确认替代查询方式")

    items = []
    counters = {STATUS_RISK: 0, STATUS_OK: 0, STATUS_INSUFFICIENT: 0, STATUS_NA: 0}
    for project, vm_ips in project_ip.items():
        vm_ips = vm_ips or []
        total = len(vm_ips)
        if total < min_inst:
            counters[STATUS_NA] += 1
            items.append(_result(STATUS_NA, "实例数低于检查下限", project=project, instances=total))
            continue

        host_count: Dict[str, int] = {}
        unmapped = []
        for ip in vm_ips:
            host = vm_to_host.get(ip)
            if host:
                host_count[host] = host_count.get(host, 0) + 1
            else:
                unmapped.append(ip)

        mapped = total - len(unmapped)
        if mapped == 0:
            counters[STATUS_INSUFFICIENT] += 1
            items.append(_result(STATUS_INSUFFICIENT, "该项目云主机均无法映射宿主机",
                                 project=project, instances=total, unmapped=unmapped))
            continue

        max_host = max(host_count.values()) if host_count else 0
        concentration = max_host / total if total else 0
        host_num = len(host_count)

        if len(unmapped) > 0:
            counters[STATUS_INSUFFICIENT] += 1
            items.append(_result(STATUS_INSUFFICIENT,
                                 f"部分云主机未映射宿主机（已映射 {mapped}/{total}），集中度仅供参考",
                                 project=project, instances=total, host_count=host_num,
                                 max_concentration=round(concentration, 3),
                                 host_distribution=host_count, unmapped=unmapped,
                                 coverage=round(mapped / total, 3)))
        elif host_num == 1:
            counters[STATUS_RISK] += 1
            items.append(_result(STATUS_RISK,
                                 f"{total} 个实例全部集中于同一宿主机 {list(host_count.keys())[0]}",
                                 project=project, instances=total, host_count=1,
                                 max_concentration=1.0, host_distribution=host_count))
        elif concentration > max_concentration:
            counters[STATUS_RISK] += 1
            items.append(_result(STATUS_RISK,
                                 f"最大宿主机集中比例 {concentration:.1%}（{host_num} 台宿主机）",
                                 project=project, instances=total, host_count=host_num,
                                 max_concentration=round(concentration, 3),
                                 host_distribution=host_count))
        else:
            counters[STATUS_OK] += 1
            items.append(_result(STATUS_OK, "实例分散于多个宿主机",
                                 project=project, instances=total, host_count=host_num,
                                 max_concentration=round(concentration, 3),
                                 host_distribution=host_count))

    return _result(STATUS_RISK if counters[STATUS_RISK] else STATUS_OK,
                   f"故障域分析：命中风险 {counters[STATUS_RISK]} 个项目",
                   **_meta(node=node), items=items, counts=counters,
                   node_code3=code3,
                   threshold={"min_instance_to_check": min_inst,
                              "max_host_concentration": max_concentration})


# ============================================================================
# MCP 工具注册
# ============================================================================

MCP_TOOLS = [
    {
        "name": "mcp_module_topology",
        "description": "获取中心节点（贵州/广州4c）的业务模块与应用归属，解析 projectName 并筛选 -dep 结尾应用。返回 deploy_node 供链路分析使用。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "node": {"type": "string", "description": "节点标识，如 云公司->贵州 / 云公司->广州4c"},
                "product_name": {"type": "string", "description": "产品名称，默认 天翼看家"},
                "only_dep": {"type": "boolean", "description": "是否仅保留 -dep 结尾应用（默认 true）"}
            },
            "required": ["node"]
        }
    },
    {
        "name": "mcp_replica",
        "description": "D1 副本数量与可用性：期望副本是否满足冗余要求、可用副本是否低于期望。结果四态。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "node": {"type": "string", "description": "节点标识"},
                "deployments": {"type": "array", "items": {"type": "string"}, "description": "应用/deployment 列表，缺省时自动从模块拓扑派生 -dep 应用"},
                "product_name": {"type": "string", "description": "产品名称"}
            },
            "required": ["node"]
        }
    },
    {
        "name": "mcp_capacity",
        "description": "D2 容量瓶颈：按 (namespace, deployment) 判定，CPU 限流(>20%持续15min)、内存(工作集/limit>85%)、HPA 达最大副本。结果四态。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "node": {"type": "string", "description": "节点标识"},
                "deployments": {"type": "array", "items": {"type": "string"}, "description": "应用列表"},
                "product_name": {"type": "string", "description": "产品名称"}
            },
            "required": ["node"]
        }
    },
    {
        "name": "mcp_load_balance",
        "description": "D3 Pod 负载不均衡：按 (namespace, deployment) 取最大/平均 CPU 负载，>2 持续 15min 判风险；单 Pod 应用不适用。结果四态。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "node": {"type": "string", "description": "节点标识"},
                "deployments": {"type": "array", "items": {"type": "string"}, "description": "应用列表"},
                "product_name": {"type": "string", "description": "产品名称"}
            },
            "required": ["node"]
        }
    },
    {
        "name": "mcp_oom",
        "description": "D4 稳定性 OOM：近 7 天（按阈值配置）OOMKilled 窗口回看，辅以重启次数证据。结果四态。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "node": {"type": "string", "description": "节点标识"},
                "deployments": {"type": "array", "items": {"type": "string"}, "description": "应用列表"},
                "product_name": {"type": "string", "description": "产品名称"},
                "days": {"type": "integer", "description": "近期窗口天数，默认取阈值配置"}
            },
            "required": ["node"]
        }
    },
    {
        "name": "mcp_link_dependency",
        "description": "D5 链路依赖：经 Pinpoint 拓扑接口获取服务调用关系（节点+边+统计字段）。链路阈值待联调制定。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "deploy_node": {"type": "string", "description": "mcp_module_topology 返回的 deploy_node（可能含多节点/别名）"},
                "projects": {"type": "array", "items": {"type": "string"}, "description": "目标模块的 projectName 列表"},
                "minute": {"type": "integer", "description": "统计窗口（分钟），默认 5"}
            },
            "required": ["deploy_node"]
        }
    },
    {
        "name": "mcp_node_list",
        "description": "省节点分析第一步：获取节点列表并筛选名称以「云公司」开头的节点（含 ctyun_node_code3）。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "filter_prefix": {"type": "string", "description": "节点名前缀，默认 云公司"}
            },
            "required": []
        }
    },
    {
        "name": "mcp_virtual_structure",
        "description": "省节点分析第二步：获取物理宿主机 ↔ 云主机关系，反向索引为「云主机IP → 宿主机IP」。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "ctyun_node_code3": {"type": "string", "description": "节点的 ctyun_node_code3"}
            },
            "required": ["ctyun_node_code3"]
        }
    },
    {
        "name": "mcp_topology_info",
        "description": "省节点分析第三步：获取项目 ↔ 云主机部署关系（projectIpData 为主）。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "node": {"type": "string", "description": "节点标识，如 云公司->海口9"}
            },
            "required": ["node"]
        }
    },
    {
        "name": "mcp_fault_domain",
        "description": "D6 省节点故障域：汇总「项目 → 云主机 → 物理宿主机」映射，输出宿主机数与最大宿主机集中比例，超过 max_host_concentration 判风险。结果四态。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "node": {"type": "string", "description": "云公司相关省市节点标识"},
                "min_instances": {"type": "integer", "description": "触发检查的最小实例数"}
            },
            "required": ["node"]
        }
    },
]

DEFAULT_ENABLED_TOOLS = [t['name'] for t in MCP_TOOLS]
_env_enabled = os.environ.get('MCP_ENABLED_TOOLS', '')
ENABLED_TOOLS = [t.strip() for t in _env_enabled.split(',') if t.strip()] if _env_enabled else list(DEFAULT_ENABLED_TOOLS)
logger.log_info(f"已启用工具白名单: {ENABLED_TOOLS}")


def _get_available_tools() -> List[dict]:
    return [tool for tool in MCP_TOOLS if tool['name'] in ENABLED_TOOLS]


def _is_tool_enabled(tool_name: str) -> bool:
    return tool_name in ENABLED_TOOLS


MCP_CAPABILITIES = {
    "tools/list": {},
    "tools/call": {}
}


# ============================================================================
# 工具调度
# ============================================================================

def _dispatch_tool(tool_name: str, args: dict) -> dict:
    if not _is_tool_enabled(tool_name):
        raise ValueError(f"工具 {tool_name} 未启用，当前仅支持: {', '.join(ENABLED_TOOLS)}")

    def clean(kwargs):
        return {k: v for k, v in kwargs.items() if v is not None}

    if tool_name == "mcp_module_topology":
        return tool_module_topology(**clean({
            "node": args.get("node"),
            "product_name": args.get("product_name"),
            "only_dep": args.get("only_dep"),
        }))
    elif tool_name == "mcp_replica":
        return tool_replica(**clean({
            "node": args.get("node"),
            "deployments": args.get("deployments"),
            "product_name": args.get("product_name"),
        }))
    elif tool_name == "mcp_capacity":
        return tool_capacity(**clean({
            "node": args.get("node"),
            "deployments": args.get("deployments"),
            "product_name": args.get("product_name"),
        }))
    elif tool_name == "mcp_load_balance":
        return tool_load_balance(**clean({
            "node": args.get("node"),
            "deployments": args.get("deployments"),
            "product_name": args.get("product_name"),
        }))
    elif tool_name == "mcp_oom":
        return tool_oom(**clean({
            "node": args.get("node"),
            "deployments": args.get("deployments"),
            "product_name": args.get("product_name"),
            "days": args.get("days"),
        }))
    elif tool_name == "mcp_link_dependency":
        return tool_link_dependency(**clean({
            "deploy_node": args.get("deploy_node"),
            "projects": args.get("projects"),
            "minute": args.get("minute"),
        }))
    elif tool_name == "mcp_node_list":
        return tool_node_list(**clean({"filter_prefix": args.get("filter_prefix")}))
    elif tool_name == "mcp_virtual_structure":
        return tool_virtual_structure(**clean({"ctyun_node_code3": args.get("ctyun_node_code3")}))
    elif tool_name == "mcp_topology_info":
        return tool_topology_info(**clean({"node": args.get("node")}))
    elif tool_name == "mcp_fault_domain":
        return tool_fault_domain(**clean({
            "node": args.get("node"),
            "min_instances": args.get("min_instances"),
        }))
    else:
        raise ValueError(f"Unknown tool: {tool_name}")


# ============================================================================
# MCP 协议处理器
# ============================================================================

class MCPProtocolHandler:
    def __init__(self):
        self.sse_clients = []

    def register_sse_client(self, client_obj):
        self.sse_clients.append(client_obj)
        logger.log_info(f"SSE 客户端已注册，当前连接数: {len(self.sse_clients)}")

    def unregister_sse_client(self, client_obj):
        if client_obj in self.sse_clients:
            self.sse_clients.remove(client_obj)

    def broadcast(self, message: dict):
        msg_str = json.dumps(message, ensure_ascii=False)
        for c in self.sse_clients:
            try:
                c.put(f"data: {msg_str}\n\n")
            except Exception as e:
                logger.log_error(f"广播消息失败: {e}")
                self.unregister_sse_client(c)

    def _build_response(self, request_id, result=None, error=None) -> dict:
        resp = {"jsonrpc": "2.0", "id": request_id}
        if error is not None:
            resp["error"] = error
        else:
            resp["result"] = result
        return resp

    def _build_error(self, code: int, message: str, data=None) -> dict:
        err = {"code": code, "message": message}
        if data is not None:
            err["data"] = data
        return err

    def handle_message(self, message: dict):
        jsonrpc = message.get("jsonrpc")
        request_id = message.get("id")
        method = message.get("method")
        params = message.get("params") or {}

        if jsonrpc != "2.0":
            return self._build_response(request_id, error=self._build_error(-32600, "Invalid Request")), False

        # JSON-RPC 通知无 id、无需响应
        if isinstance(method, str) and method.startswith("notifications/"):
            logger.log_mcp_request(method, request_id)
            return None, False

        try:
            if method == "initialize":
                logger.log_mcp_request(method, request_id)
                response = self._build_response(request_id, {
                    "protocolVersion": "2024-11-05",
                    "name": "arch-opt-mcp-server",
                    "version": config.thresholds_version,
                    "serverInfo": {"name": "arch-opt-mcp-server", "version": config.thresholds_version},
                    "capabilities": MCP_CAPABILITIES
                })
                logger.log_mcp_response(method, request_id, True)
                return response, True

            elif method == "ping":
                logger.log_mcp_request(method, request_id)
                return self._build_response(request_id, {}), True

            elif method == "tools/list":
                logger.log_mcp_request(method, request_id)
                response = self._build_response(request_id, {"tools": _get_available_tools()})
                logger.log_mcp_response(method, request_id, True)
                return response, True

            elif method == "tools/call":
                tool_name = params.get("name")
                arguments = params.get("arguments", {})
                logger.log_mcp_request(method, request_id, {"name": tool_name})
                if not tool_name:
                    return self._build_response(
                        request_id,
                        error=self._build_error(-32602, "Invalid params: tool name is required")), True
                try:
                    start = time.time()
                    result = _dispatch_tool(tool_name, arguments)
                    duration = time.time() - start
                    logger.log_tool_call(tool_name, arguments, result, duration, success=True)
                    logger.log_mcp_response(method, request_id, True)
                    return self._build_response(request_id, {
                        "content": [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}],
                        "isError": False
                    }), True
                except ValueError as e:
                    logger.log_mcp_response(method, request_id, False, str(e))
                    return self._build_response(request_id, error=self._build_error(-32602, str(e))), True
                except Exception as e:
                    logger.log_tool_call(tool_name, arguments, {}, 0, success=False)
                    logger.log_mcp_response(method, request_id, False, str(e))
                    return self._build_response(
                        request_id, error=self._build_error(-32603, f"Internal error: {str(e)}")), True
            else:
                return self._build_response(
                    request_id, error=self._build_error(-32601, f"Method not found: {method}")), False
        except Exception as e:
            logger.log_error(f"MCP 消息处理异常: {e}")
            return self._build_response(
                request_id, error=self._build_error(-32603, f"Internal error: {str(e)}")), False


mcp_handler = MCPProtocolHandler()


# ============================================================================
# HTTP 服务层（FastAPI：标准 MCP 协议 + 兼容层）
# ============================================================================

if HAS_FASTAPI:

    class ToolCallRequest(BaseModel):
        name: str
        arguments: Dict[str, Any] = {}

    class ToolCallBody(BaseModel):
        name: Optional[str] = None
        tool: Optional[str] = None
        arguments: Dict[str, Any] = {}

    app = FastAPI(
        title="arch-opt-mcp-server",
        description="架构优化智能体 MCP 服务器：副本/容量/负载/OOM/链路/故障域",
        version=config.thresholds_version,
    )

    @app.get("/", tags=["基础"])
    def index():
        return {
            "service": "arch-opt-mcp-server",
            "version": config.thresholds_version,
            "tools": [t['name'] for t in _get_available_tools()],
        }

    @app.get("/health", tags=["基础"])
    def health():
        return {"status": "ok", "time": _now_iso(), "mock": USE_MOCK}

    @app.get("/sse", tags=["MCP 协议"])
    async def sse():
        import asyncio

        async def event_generator():
            queue = asyncio.Queue()

            class ClientWrapper:
                def put(self, data):
                    queue.put_nowait(data)

            cw = ClientWrapper()
            mcp_handler.register_sse_client(cw)
            try:
                yield f"event: endpoint\ndata: /messages\n\n"
                while True:
                    try:
                        msg = await asyncio.wait_for(queue.get(), timeout=15)
                        yield msg
                    except asyncio.TimeoutError:
                        yield ": keep-alive\n\n"
            finally:
                mcp_handler.unregister_sse_client(cw)

        return StreamingResponse(event_generator(), media_type="text/event-stream")

    @app.post("/messages", tags=["MCP 协议"])
    async def messages(message: dict):
        response, should_broadcast = mcp_handler.handle_message(message)
        if response is None:
            return JSONResponse(content={}, status_code=202)
        if should_broadcast:
            mcp_handler.broadcast(response)
        return JSONResponse(content=response)

    @app.post("/mcp", tags=["MCP 协议"])
    async def mcp_endpoint(message: dict):
        response, _ = mcp_handler.handle_message(message)
        if response is None:
            return JSONResponse(content={}, status_code=202)
        return JSONResponse(content=response)

    @app.get("/tools", tags=["平台兼容"])
    def list_tools_get():
        return {"tools": _get_available_tools()}

    @app.post("/tools/list", tags=["平台兼容"])
    def list_tools_post():
        return {"tools": _get_available_tools()}

    @app.get("/tools/list", tags=["平台兼容"])
    def list_tools_get2():
        return {"tools": _get_available_tools()}

    @app.post("/tools/call", tags=["平台兼容"])
    def call_tool(body: ToolCallBody):
        tool_name = body.name or body.tool
        if not tool_name:
            raise HTTPException(status_code=400, detail="缺少工具名 name")
        try:
            result = _dispatch_tool(tool_name, body.arguments or {})
            return JSONResponse(content=result)
        except ValueError as e:
            raise HTTPException(status_code=400, detail=str(e))
        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    @app.post("/api/mcp_module_topology", tags=["兼容层"])
    def api_module_topology(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_module_topology", req))

    @app.post("/api/mcp_replica", tags=["兼容层"])
    def api_replica(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_replica", req))

    @app.post("/api/mcp_capacity", tags=["兼容层"])
    def api_capacity(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_capacity", req))

    @app.post("/api/mcp_load_balance", tags=["兼容层"])
    def api_load_balance(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_load_balance", req))

    @app.post("/api/mcp_oom", tags=["兼容层"])
    def api_oom(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_oom", req))

    @app.post("/api/mcp_link_dependency", tags=["兼容层"])
    def api_link_dependency(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_link_dependency", req))

    @app.post("/api/mcp_node_list", tags=["兼容层"])
    def api_node_list(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_node_list", req))

    @app.post("/api/mcp_virtual_structure", tags=["兼容层"])
    def api_virtual_structure(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_virtual_structure", req))

    @app.post("/api/mcp_topology_info", tags=["兼容层"])
    def api_topology_info(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_topology_info", req))

    @app.post("/api/mcp_fault_domain", tags=["兼容层"])
    def api_fault_domain(req: dict):
        return JSONResponse(content=_dispatch_tool("mcp_fault_domain", req))

    @app.get("/api/config", tags=["基础"])
    def api_config():
        return {
            "rule_version": config.thresholds_version,
            "thresholds": config.thresholds,
            "prometheus": config.prometheus_base,
        }


def run_http(host: str, port: int):
    if not HAS_FASTAPI:
        logger.log_error("未安装 fastapi 模块，请先安装: pip install fastapi uvicorn")
        sys.exit(1)

    logger.log_info("🚀 启动架构优化 MCP HTTP 服务器...")
    logger.log_info(f"📍 监听地址: http://{host}:{port}")
    logger.log_info(f"📖 API 文档: http://{host}:{port}/docs")
    logger.log_info(f"🔌 MCP SSE: http://{host}:{port}/sse")
    logger.log_info(f"✅ 注册工具: {[tool['name'] for tool in _get_available_tools()]}")
    logger.log_info(f"🧪 Mock 模式: {USE_MOCK}")

    uvicorn.run(app, host=host, port=port, log_level="info")


def main():
    parser = argparse.ArgumentParser(description="arch-opt-mcp-server - 架构优化智能体 MCP 服务器")
    parser.add_argument("--transport", default="http", choices=["http"], help="传输模式")
    parser.add_argument("--host", default="0.0.0.0", help="监听地址")
    parser.add_argument("--port", type=int, default=8062, help="监听端口")
    args = parser.parse_args()

    if args.transport == "http":
        run_http(args.host, args.port)


if __name__ == "__main__":
    main()
