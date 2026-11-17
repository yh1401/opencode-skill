"""架构优化分析工具实现

对应《架构优化智能体实现方案-V2》所需的三类数据源：

- EasyViews 平台接口：模块与应用归属（1.1）、服务链路拓扑（1.3）、
  节点列表（2.2）、物理机与云主机关系（2.3）、项目部署关系（2.4）、
  宿主机集中度（2.5）
- Prometheus：副本、CPU 限流、内存、HPA、Pod 负载、OOM 等运行指标（1.2）
- Pinpoint：见 tools.py（1.3 链路下钻复用）

阈值判定由规则判定层（arch_rules）执行，保证同一输入得到一致结论；
工具层负责取数、确定性关联计算与把判定结果组装为可追溯证据。
取数状态：SUCCESS / NO_DATA / DATA_INSUFFICIENT / FAIL。
"""
import asyncio
import logging
import math
import re
import time
import uuid
from collections import Counter, deque
from datetime import datetime
from typing import Optional

import httpx

import arch_rules
import config
import task_scope
from easyviews_client import get_easyviews_client
from prometheus_client import get_prometheus_client
import tools as pinpoint_tools

logger = logging.getLogger(__name__)


def _scope_guard(task_id: Optional[str], node: str) -> None:
    """跨会话任务作用域校验：task_id 已注册且 node 不在白名单时拒绝调用"""
    ok, reason = task_scope.get_registry().check(
        task_id, node, config.ARCH_TASK_TTL_MINUTES
    )
    if not ok:
        raise ValueError(f"SCOPE_DENIED: {reason}")


def _api(path: str) -> str:
    """拼接 EasyViews 内层真实接口地址"""
    return f"{config.EASYVIEWS_BASE_URL.rstrip('/')}{path}"


def _task_meta(task_id: Optional[str] = None) -> dict:
    """任务标识与采集时间（方案第七节：报告需记录任务标识与采集时间）

    调用方传入 task_id 时原样透传，便于与外部任务系统对齐；未传则生成。
    """
    now = time.time()
    return {
        "taskId": task_id or f"arch-{int(now * 1000)}-{uuid.uuid4().hex[:6]}",
        "collectTime": int(now * 1000),
        "collectTimeIso": datetime.now().astimezone().isoformat(timespec="seconds"),
    }


# EasyViews 各接口的数据更新时间字段名不统一，按候选键尽力提取
_UPDATE_TIME_KEYS = (
    "updateTime",
    "update_time",
    "lastUpdateTime",
    "dataUpdateTime",
    "dataTime",
    "collectTime",
    "timestamp",
    "time",
)


def _data_time(body: dict) -> Optional[str]:
    """提取数据更新时间（无法识别时返回 None，不做推测）"""
    if not isinstance(body, dict):
        return None
    for src in (body, body.get("data")):
        if not isinstance(src, dict):
            continue
        for k in _UPDATE_TIME_KEYS:
            v = src.get(k)
            if v not in (None, "", [], {}):
                return str(v)
    return None


# ---------------- 一、中心节点：模块与链路 ----------------


async def list_nodes(node_prefix: str = "云公司") -> dict:
    """获取节点列表（方案 2.2）

    node_prefix 用于筛选节点名，默认"云公司"（方案要求），传空串返回全部。
    """
    body = await get_easyviews_client().call(
        _api("/api/external/monitorData/getNode"),
        types="GET",
        req_param={"center": "true"},
    )
    data = body.get("data") or []
    if node_prefix:
        data = [n for n in data if str(n.get("node", "")).startswith(node_prefix)]
    missing_code = [n.get("node") for n in data if not n.get("ctyun_node_code3")]
    return {
        "status": "SUCCESS" if data else "NO_DATA",
        "total": len(data),
        "missingNodeCode": missing_code,
        "dataUpdateTime": _data_time(body),
        "note": "ctyun_node_code3 用于宿主机关系查询，node 用于项目部署查询，两者不可混用",
        "nodes": data,
    }


async def get_module_topology(
    node: str,
    product_name: Optional[str] = None,
    simplify: bool = True,
) -> dict:
    """获取模块与应用归属（方案 1.1）

    解析 data[].topologyData.nodes[]，保留模块标识、中文名称、对象类型与 projectName。
    deployNode 为后续链路拓扑接口（1.3）的 node 入参。
    """
    if not node:
        raise ValueError("node 不能为空，如 '云公司->贵州'")
    product_name = product_name or config.ARCH_PRODUCT_NAME
    body = await get_easyviews_client().call(
        _api("/api/autoOperation/topology/getTopologyByProductName"),
        types="GET",
        req_param={"center": "true", "node": node, "productName": product_name},
    )
    data = body.get("data") or []
    if not data:
        return {
            "status": "NO_DATA",
            "node": node,
            "product": product_name,
            "moduleCount": 0,
            "modules": [],
            "dataUpdateTime": _data_time(body),
            "errMsg": "接口未返回模块数据，请确认 node 与 productName 是否正确",
        }
    if not simplify:
        return body
    modules = []
    for item in data:
        for n in (item.get("topologyData") or {}).get("nodes") or []:
            modules.append(
                {
                    "moduleId": n.get("id"),
                    "moduleName": n.get("name"),
                    "moduleNameCn": n.get("nameCn"),
                    "objectType": n.get("objectType"),
                    "objSign": n.get("objSign"),
                    "oId": n.get("oId"),
                    "projectName": n.get("projectName") or [],
                    "domains": n.get("domains") or [],
                }
            )
    return {
        "status": "SUCCESS" if modules else "NO_DATA",
        "node": node,
        "product": product_name,
        "deployNode": data[0].get("deployNode"),
        "dataUpdateTime": _data_time(body),
        "moduleCount": len(modules),
        "modules": modules,
    }


async def _fetch_link_topology(
    node: str,
    projects: list[str],
    minute: int,
    caller_range: int,
    callee_range: int,
    filter_node: bool,
    node_type: str,
) -> dict:
    """单次调用上游链路接口（不施加总时限，供分批与单批共用）"""
    body = await get_easyviews_client().call(
        _api("/api/autoOperation/topology/getTopologyByNodeProject/"),
        types="POST",
        req_param={
            "center": "true",
            "calleeRange": callee_range,
            "callerRange": caller_range,
            "filterNode": filter_node,
            "minute": minute,
            "nodeType": node_type,
            "node": node,
            "projects": projects,
        },
    )
    data = body.get("data")
    update_time = _data_time(body)
    logger.info(
        "link topology batch done: projects=%d minute=%d nodes=%d edges=%d dataUpdateTime=%s",
        len(projects),
        minute,
        len(data.get("nodes") or []) if isinstance(data, dict) else 0,
        len(data.get("edges") or []) if isinstance(data, dict) else 0,
        update_time,
    )
    return {
        "status": "SUCCESS" if data else "NO_DATA",
        "minute": minute,
        "callerRange": caller_range,
        "calleeRange": callee_range,
        "dataUpdateTime": update_time,
        "data": data,
    }


_EDGE_COUNT_KEYS = ("totalCount", "errorCount", "slowCount")


def _merge_topology_payloads(payloads: list[dict]) -> tuple[dict, dict]:
    """合并多批次拓扑：节点按 id、调用边按 (source, target) 去重。

    单批次内上游不返回重复边（实测 466 节点 / 591 边无重复），但同一条边可能
    出现在多个批次（两端应用分处不同批次），因此跨批次按边去重；重复边的计数
    取各批次最大值而不是求和，避免同一次调用被重复计数。
    """
    nodes: dict[str, dict] = {}
    edges: dict[tuple[str, str], dict] = {}
    duplicate_edges = 0
    conflicting_edges = 0
    empty_batches = 0
    data_update_time: Optional[str] = None
    for item in payloads:
        data = item.get("data")
        if item.get("status") != "SUCCESS" or not isinstance(data, dict):
            empty_batches += 1
            continue
        if data_update_time is None:
            data_update_time = item.get("dataUpdateTime")
        for n in data.get("nodes") or []:
            if not isinstance(n, dict):
                continue
            key = n.get("id") or n.get("name")
            if not key:
                continue
            key = str(key)
            cur = nodes.get(key)
            if cur is None:
                nodes[key] = dict(n)
                continue
            for k, v in n.items():
                if cur.get(k) in (None, "", [], {}) and v not in (None, "", [], {}):
                    cur[k] = v
        for e in data.get("edges") or []:
            if not isinstance(e, dict):
                continue
            s, t = e.get("source"), e.get("target")
            if not s or not t:
                continue
            key = (str(s), str(t))
            cur = edges.get(key)
            if cur is None:
                edges[key] = dict(e)
                continue
            duplicate_edges += 1
            conflict = False
            for k in _EDGE_COUNT_KEYS:
                try:
                    if float(e.get(k) or 0) > float(cur.get(k) or 0):
                        cur[k] = e[k]
                        conflict = True
                except (TypeError, ValueError):
                    continue
            if conflict:
                conflicting_edges += 1
    merged = {"edges": list(edges.values()), "nodes": list(nodes.values())}
    stats = {
        "duplicateEdges": duplicate_edges,
        "conflictingEdges": conflicting_edges,
        "emptyBatches": empty_batches,
        "dataUpdateTime": data_update_time,
    }
    return merged, stats


async def _fetch_link_topology_batched(
    node: str,
    projects: list[str],
    minute: int,
    caller_range: int,
    callee_range: int,
    filter_node: bool,
    node_type: str,
    batch_size: int,
    concurrency: int,
) -> dict:
    """按批并发取数并合并；任一批次失败则整体记为数据不足（缺失批次会造成“无依赖”误判）"""
    chunks = [projects[i:i + batch_size] for i in range(0, len(projects), batch_size)]
    sem = asyncio.Semaphore(max(1, concurrency))
    batch_info = {
        "totalProjects": len(projects),
        "batchSize": batch_size,
        "batchCount": len(chunks),
        "concurrency": max(1, concurrency),
    }

    async def one(index: int, chunk: list[str]) -> dict:
        async with sem:
            try:
                item = await _fetch_link_topology(
                    node, chunk, minute, caller_range, callee_range, filter_node, node_type
                )
                item["batchIndex"] = index
                return item
            except Exception as e:  # noqa: BLE001
                logger.warning(
                    "link topology batch failed: index=%d projects=%d %s: %s",
                    index, len(chunk), type(e).__name__, str(e)[:200], exc_info=True,
                )
                return {
                    "status": "FAIL",
                    "batchIndex": index,
                    "errMsg": f"{type(e).__name__}: {str(e)[:200]}",
                    "minute": minute,
                    "callerRange": caller_range,
                    "calleeRange": callee_range,
                    "dataUpdateTime": None,
                    "data": None,
                }

    results = await asyncio.gather(*(one(i, c) for i, c in enumerate(chunks)))
    failures = [
        {"batchIndex": r.get("batchIndex"), "errMsg": r.get("errMsg")}
        for r in results
        if r.get("status") == "FAIL"
    ]
    if failures:
        return {
            "status": "DATA_INSUFFICIENT",
            "minute": minute,
            "callerRange": caller_range,
            "calleeRange": callee_range,
            "dataUpdateTime": None,
            "data": None,
            "errMsg": f"{len(failures)}/{len(chunks)} 个批次取数失败，未合并（缺失批次会被误判为无依赖），"
            "可降低 ARCH_LINK_BATCH_SIZE 或 ARCH_LINK_BATCH_CONCURRENCY 后重试",
            "degradedReason": "BATCH_FAILED",
            "batchInfo": {**batch_info, "failures": failures},
        }

    merged, stats = _merge_topology_payloads(results)
    status = "SUCCESS" if merged["edges"] or merged["nodes"] else "NO_DATA"
    return {
        "status": status,
        "minute": minute,
        "callerRange": caller_range,
        "calleeRange": callee_range,
        "dataUpdateTime": stats["dataUpdateTime"],
        "data": merged,
        "batchInfo": {**batch_info, **stats},
    }


async def get_link_topology(
    node: str,
    projects: list[str],
    minute: Optional[int] = None,
    caller_range: int = 1,
    callee_range: int = 1,
    filter_node: bool = False,
    node_type: str = "",
) -> dict:
    """获取应用链路拓扑（方案 1.3）

    node 使用 1.1 返回的 deployNode（可能含多个节点别名，按原样传入）；
    projects 为目标模块的 projectName 列表。
    minute 缺省取 config.ARCH_LINK_WINDOW_MINUTES（默认 15 分钟），
    5 分钟窗口在低峰期样本过少，容易把"无调用"误判成链路缺失。

    上游耗时随应用数 × 窗口增长：应用数超过 ARCH_LINK_BATCH_SIZE（默认 30）时
    自动按批并发取数再合并（节点按 id、边按 source+target 去重），
    整次调用仍受 ARCH_LINK_TIMEOUT 总时限约束，避免无响应被网关空闲超时切断。
    """
    if not node:
        raise ValueError("node 不能为空，应传 1.1 接口返回的 deployNode")
    if not projects:
        raise ValueError("projects 不能为空，应传模块的 projectName 列表")
    if minute is None:
        minute = config.ARCH_LINK_WINDOW_MINUTES
    batch_size = config.ARCH_LINK_BATCH_SIZE
    # 上游耗时 ≈ 应用数 × 窗口；以"默认窗口 × 批大小"为负载预算，窗口越大自动缩小批
    # （121 应用 × 15 分钟 → 30/批；× 30 分钟 → 15/批），避免只放大窗口就撞上总时限
    if batch_size > 0 and minute > 0:
        budget = batch_size * max(1, config.ARCH_LINK_WINDOW_MINUTES)
        batch_size = max(1, min(batch_size, budget // minute))
    if batch_size > 0 and len(projects) > batch_size:
        coro = _fetch_link_topology_batched(
            node, projects, minute, caller_range, callee_range, filter_node, node_type,
            batch_size, config.ARCH_LINK_BATCH_CONCURRENCY,
        )
        shape = f"batched({len(projects)}/{batch_size})"
    else:
        coro = _fetch_link_topology(
            node, projects, minute, caller_range, callee_range, filter_node, node_type
        )
        shape = f"single({len(projects)})"
    timeout = config.ARCH_LINK_TIMEOUT
    if timeout <= 0:
        return await coro
    try:
        result = await asyncio.wait_for(coro, timeout=timeout)
    except asyncio.CancelledError:
        logger.warning(
            "get_link_topology cancelled (客户端中断/会话关闭): node=%s projects=%d minute=%s shape=%s",
            node, len(projects), minute, shape,
        )
        raise
    except (asyncio.TimeoutError, httpx.TimeoutException) as e:
        logger.warning(
            "get_link_topology timeout (%s): node=%s projects=%d minute=%s shape=%s",
            type(e).__name__, node, len(projects), minute, shape,
        )
        return {
            "status": "DATA_INSUFFICIENT",
            "minute": minute,
            "callerRange": caller_range,
            "calleeRange": callee_range,
            "dataUpdateTime": None,
            "data": None,
            "errMsg": f"链路拓扑取数超过 {timeout:g} 秒未完成，已中止取数，记为数据不足；"
            "可降低 minute、调小 ARCH_LINK_BATCH_SIZE 或调大 ARCH_LINK_BATCH_CONCURRENCY 后重试",
            "degradedReason": "TIMEOUT",
            "timeoutSeconds": timeout,
        }
    data = result.get("data")
    logger.info(
        "get_link_topology ok: node=%s projects=%d minute=%s shape=%s status=%s edges=%d",
        node, len(projects), minute, shape, result.get("status"),
        len(data.get("edges") or []) if isinstance(data, dict) else 0,
    )
    return result



# ---------------- 二、服务链路与依赖分析（方案 1.3） ----------------

# 响应结构以联调结果为准，以下为多字段兼容候选
_NODE_NAME_KEYS = ("name", "nodeName", "applicationName", "appName", "id", "key", "node")
_EDGE_SOURCE_KEYS = (
    "source", "caller", "from", "sourceName", "callerName", "parent",
    "callerApplicationName", "sourceApplication",
)
_EDGE_TARGET_KEYS = (
    "target", "callee", "to", "targetName", "calleeName", "child",
    "calleeApplicationName", "targetApplication",
)
_CHILD_LIST_KEYS = (
    "calleeList", "callees", "children", "targets", "childList", "subNodes",
    "calleeNodes", "dependencies",
)
_COUNT_KEYS = (
    "totalCount", "callCount", "count", "requestCount", "transactionCount",
    "sampleCount", "total",
)
_ERROR_KEYS = ("errorCount", "failCount", "failedCount", "error", "exceptionCount")
_ELAPSED_KEYS = (
    "elapsed", "avgElapsed", "elapsedTime", "maxElapsed", "duration",
    "responseTime", "avgResponseTime", "avgElapsedTime",
)


def _pick(d: dict, keys) -> Optional[str]:
    """从字典中按候选键取第一个非空值"""
    for k in keys:
        v = d.get(k)
        if v not in (None, "", [], {}):
            return str(v)
    return None


def _pick_num(d: dict, keys) -> Optional[float]:
    """从字典中按候选键取第一个可转数值"""
    for k in keys:
        v = d.get(k)
        if v is None or isinstance(v, bool):
            continue
        try:
            return float(v)
        except (TypeError, ValueError):
            continue
    return None


def _edge_stats(raw: dict) -> dict:
    """提取调用边统计字段（调用量、错误数、耗时）"""
    total = _pick_num(raw, _COUNT_KEYS)
    err = _pick_num(raw, _ERROR_KEYS)
    elapsed = _pick_num(raw, _ELAPSED_KEYS)
    rate = None
    if total and total > 0 and err is not None:
        rate = round(err / total, 4)
    return {
        "totalCount": total,
        "errorCount": err,
        "errorRate": rate,
        "elapsed": elapsed,
    }


def _extract_graph(data) -> dict:
    """从链路拓扑响应中提取节点与调用边（尽力兼容多种结构）

    返回 {"nodes": {name: raw}, "edges": [{source,target,stats}], "parseMode": str}
    无法识别时 nodes/edges 为空，由上层标记为数据不足，不做推测。
    """
    nodes: dict = {}
    edges: list = []
    parse_mode = ""

    def add_edge(src, dst, raw):
        if not src or not dst:
            return
        src, dst = str(src), str(dst)
        nodes.setdefault(src, {})
        nodes.setdefault(dst, {})
        edges.append({"source": src, "target": dst, "stats": _edge_stats(raw)})

    if isinstance(data, list):
        for n in data:
            if not isinstance(n, dict):
                continue
            name = _pick(n, _NODE_NAME_KEYS)
            if not name:
                continue
            nodes.setdefault(name, n)
            for ck in _CHILD_LIST_KEYS:
                children = n.get(ck)
                if isinstance(children, list):
                    for c in children:
                        if isinstance(c, dict):
                            add_edge(name, _pick(c, _NODE_NAME_KEYS + _EDGE_TARGET_KEYS), c)
        parse_mode = "list-of-nodes" if edges or nodes else ""
    elif isinstance(data, dict):
        raw_nodes = (
            data.get("nodes") or data.get("nodeList") or data.get("nodeData") or []
        )
        raw_edges = (
            data.get("edges") or data.get("links") or data.get("linkList")
            or data.get("edgeList") or data.get("CallerCallee") or []
        )
        if isinstance(raw_nodes, dict):
            for k, v in raw_nodes.items():
                nodes[str(k)] = v if isinstance(v, dict) else {}
        elif isinstance(raw_nodes, list):
            for n in raw_nodes:
                if isinstance(n, dict):
                    name = _pick(n, _NODE_NAME_KEYS)
                    if name:
                        nodes[name] = n
        if isinstance(raw_edges, list):
            for e in raw_edges:
                if not isinstance(e, dict):
                    continue
                add_edge(_pick(e, _EDGE_SOURCE_KEYS), _pick(e, _EDGE_TARGET_KEYS), e)
        if raw_nodes or raw_edges:
            parse_mode = "nodes+edges"

    return {"nodes": nodes, "edges": edges, "parseMode": parse_mode}


def _dfs_finish_order(nodes: list, adj: dict) -> list:
    """迭代 DFS 求完成时间顺序（Kosaraju 第一遍），避免深递归"""
    visited = set()
    order = []
    for s in nodes:
        if s in visited:
            continue
        visited.add(s)
        stack = [(s, iter(adj.get(s, ())))]
        while stack:
            v, it = stack[-1]
            nxt = None
            for w in it:
                if w not in visited:
                    visited.add(w)
                    nxt = w
                    break
            if nxt:
                stack.append((nxt, iter(adj.get(nxt, ()))))
            else:
                stack.pop()
                order.append(v)
    return order


def _find_scc(nodes: list, adj: dict) -> list:
    """Kosaraju 求强连通分量（迭代实现），仅返回规模 > 1 的环"""
    order = _dfs_finish_order(nodes, adj)
    radj: dict = {n: [] for n in nodes}
    for u in nodes:
        for v in adj.get(u, ()):
            if v in radj:
                radj[v].append(u)
    visited = set()
    comps = []
    for s in reversed(order):
        if s in visited:
            continue
        visited.add(s)
        comp = []
        stack = [s]
        while stack:
            v = stack.pop()
            comp.append(v)
            for w in radj.get(v, ()):
                if w not in visited:
                    visited.add(w)
                    stack.append(w)
        if len(comp) > 1:
            comps.append(comp)
    return comps


def _longest_path(adj: dict, nodes: list) -> dict:
    """DAG 最长调用链（Kahn 拓扑序 + 动态规划），环上节点自动排除"""
    indeg = {n: 0 for n in nodes}
    for u in nodes:
        for v in adj.get(u, []):
            if v in indeg:
                indeg[v] += 1
    queue = [n for n in nodes if indeg[n] == 0]
    order = []
    while queue:
        u = queue.pop(0)
        order.append(u)
        for v in adj.get(u, []):
            if v in indeg:
                indeg[v] -= 1
                if indeg[v] == 0:
                    queue.append(v)

    dist = {n: 1 for n in order}
    prev: dict = {}
    for u in order:
        for v in adj.get(u, []):
            if v in dist and dist[u] + 1 > dist[v]:
                dist[v] = dist[u] + 1
                prev[v] = u
    if not dist:
        return {"length": 0, "path": [], "excludedCyclicNodes": len(nodes) - len(order)}
    end = max(dist, key=lambda n: dist[n])
    path = [end]
    while path[-1] in prev:
        path.append(prev[path[-1]])
    path.reverse()
    return {
        "length": dist[end],
        "path": path,
        "excludedCyclicNodes": len(nodes) - len(order),
    }


def _reachable(start: str, graph: dict) -> set:
    """从 start 出发的可达节点集合（不含自身，迭代实现）"""
    seen: set = set()
    stack = []
    for v in graph.get(start, ()):
        if v != start and v not in seen:
            seen.add(v)
            stack.append(v)
    while stack:
        v = stack.pop()
        for w in graph.get(v, ()):
            if w != start and w not in seen:
                seen.add(w)
                stack.append(w)
    return seen


_IMPACT_NODE_PREVIEW = 10


def _impact_scope(nodes: list, adj: dict, radj: dict) -> list[dict]:
    """计算每个节点的上下游影响范围（方案 1.3「上下游影响范围」）

    affectedCount 为反向可达节点数：该节点异常时，所有（直接或间接）调用它的
    服务都会受影响，即故障影响面；downstreamCount 为正向可达节点数，用于说明
    其自身依赖复杂度。结果按影响面从大到小排序。
    """
    total = max(1, len(nodes) - 1)
    out: list[dict] = []
    for n in nodes:
        ancestors = _reachable(n, radj)
        descendants = _reachable(n, adj)
        preview = sorted(ancestors)[:_IMPACT_NODE_PREVIEW]
        out.append(
            {
                "node": n,
                "affectedCount": len(ancestors),
                "affectedRatio": round(len(ancestors) / total, 4),
                "directCallers": len(radj.get(n, ())),
                "downstreamCount": len(descendants),
                "affectedNodes": preview,
                "affectedTruncated": len(ancestors) > len(preview),
            }
        )
    out.sort(key=lambda x: (-x["affectedCount"], -x["directCallers"], x["node"]))
    return out


# 路径计数饱和上限，避免超大 DAG 出现天文数字或 inf
_PATH_COUNT_CAP = 1e15


def _path_stats(adj: dict, nodes: list) -> dict:
    """统计经过每个节点的源→汇路径数（方案 1.3「关键依赖识别」）

    Kahn 拓扑排序后在 DAG 上做双向 DP：
    经过 v 的源→汇路径数 = 从源点到 v 的路径数 × 从 v 到汇点的路径数。
    被越多核心链路共享的节点越关键，用于优先保护。
    仅剔除真实环上节点（自环 + 非平凡强连通分量），环的下游节点重新作为源点参与计算。
    """
    cyclic: set = {n for n in nodes if n in adj.get(n, ())}
    for comp in _find_scc(nodes, adj):
        cyclic.update(comp)
    dag_nodes = [n for n in nodes if n not in cyclic]
    dag_set = set(dag_nodes)
    sub = {
        n: [v for v in adj.get(n, ()) if v in dag_set] for n in dag_nodes
    }

    indeg = {n: 0 for n in dag_nodes}
    for u in dag_nodes:
        for v in sub[u]:
            indeg[v] += 1
    queue = deque(n for n in dag_nodes if indeg[n] == 0)
    order: list[str] = []
    while queue:
        u = queue.popleft()
        order.append(u)
        for v in sub[u]:
            indeg[v] -= 1
            if indeg[v] == 0:
                queue.append(v)

    dag_in = {n: 0 for n in order}
    dag_out = {n: 0 for n in order}
    for u in order:
        for v in sub[u]:
            dag_in[v] += 1
            dag_out[u] += 1

    from_start = {n: (1.0 if dag_in[n] == 0 else 0.0) for n in order}
    for u in order:
        for v in sub[u]:
            from_start[v] = min(_PATH_COUNT_CAP, from_start[v] + from_start[u])

    to_end = {n: (1.0 if dag_out[n] == 0 else 0.0) for n in order}
    for u in reversed(order):
        for v in sub[u]:
            to_end[u] = min(_PATH_COUNT_CAP, to_end[u] + to_end[v])

    through = {
        n: min(_PATH_COUNT_CAP, from_start[n] * to_end[n]) for n in order
    }
    return {
        "pathsThrough": through,
        "excludedCyclicNodes": len(cyclic),
    }


def _link_timeout_result(
    node: str,
    dep_filter: dict,
    minute: Optional[int],
    task_id: Optional[str],
    timeout: float,
    reason: str,
) -> dict:
    """链路拓扑超时兜底结果：保持状态契约，把未完成部分记为数据不足"""
    return {
        "node": node,
        "projects": dep_filter["selected"],
        "depFilter": {
            k: dep_filter[k]
            for k in ("suffixOnly", "suffix", "selectedCount", "excludedCount", "excluded")
        },
        "minute": config.ARCH_LINK_WINDOW_MINUTES if minute is None else minute,
        **_task_meta(task_id),
        "status": "DATA_INSUFFICIENT",
        "errMsg": f"{reason}，记为数据不足，不得据此判定无风险；"
        "可缩小 projects 范围或稍后重试",
        "graph": {"nodeCount": 0, "edgeCount": 0},
        "findings": [],
        "dataLimitations": ["超时中止，未产出可用链路采样"],
        "degradedReason": "TIMEOUT",
        "timeoutSeconds": timeout,
    }


async def analyze_link_topology(
    node: str,
    projects: list[str],
    minute: Optional[int] = None,
    caller_range: int = 1,
    callee_range: int = 1,
    filter_node: bool = False,
    node_type: str = "",
    task_id: Optional[str] = None,
    dep_suffix_only: Optional[bool] = None,
    dep_suffix: Optional[str] = None,
    include_raw: bool = False,
) -> dict:
    """链路拓扑 + 基础图计算入口：对整次调用施加总时限（ARCH_LINK_TIMEOUT，默认 45s）

    MCP 走 Streamable HTTP，工具执行期间响应流为空，网关空闲超时（常见 60s）
    会先掐断连接，表现为“长时间无输出就断开”。这里在时限到达时取消取数，
    返回 数据不足 + degradedReason=TIMEOUT，保证一定有响应。
    """
    timeout = config.ARCH_LINK_TIMEOUT
    if timeout <= 0:
        return await _analyze_link_topology_impl(
            node=node, projects=projects, minute=minute,
            caller_range=caller_range, callee_range=callee_range,
            filter_node=filter_node, node_type=node_type, task_id=task_id,
            dep_suffix_only=dep_suffix_only, dep_suffix=dep_suffix,
            include_raw=include_raw,
        )
    try:
        return await asyncio.wait_for(
            _analyze_link_topology_impl(
                node=node, projects=projects, minute=minute,
                caller_range=caller_range, callee_range=callee_range,
                filter_node=filter_node, node_type=node_type, task_id=task_id,
                dep_suffix_only=dep_suffix_only, dep_suffix=dep_suffix,
                include_raw=include_raw,
            ),
            timeout=timeout,
        )
    except (asyncio.TimeoutError, httpx.TimeoutException) as e:
        dep_filter = filter_dep_projects(
            projects, suffix_only=dep_suffix_only, suffix=dep_suffix
        )
        logger.warning(
            "analyze_link_topology timeout (%s): node=%s projects=%d minute=%s",
            type(e).__name__, node, len(dep_filter["selected"]), minute,
        )
        reason = (
            f"链路拓扑取数超过 {timeout:g} 秒未完成，已中止取数"
            if isinstance(e, asyncio.TimeoutError)
            else "上游 EasyViews 链路接口响应超时，已中止取数"
        )
        return _link_timeout_result(
            node, dep_filter, minute, task_id, timeout, reason
        )


async def _analyze_link_topology_impl(
    node: str,
    projects: list[str],
    minute: Optional[int] = None,
    caller_range: int = 1,
    callee_range: int = 1,
    filter_node: bool = False,
    node_type: str = "",
    task_id: Optional[str] = None,
    dep_suffix_only: Optional[bool] = None,
    dep_suffix: Optional[str] = None,
    include_raw: bool = False,
) -> dict:
    """链路拓扑 + 基础图计算（方案 1.3）

    在 get_link_topology 原始数据之上完成确定性图计算：
    入度/出度（集中依赖、下游依赖过多）、强连通分量（循环依赖）、
    最长路径（长调用链）、异常调用边（高错误率 / 慢调用）、
    反向可达集（上下游影响范围）、源→汇路径计数（关键依赖识别）。

    阈值来自 config（ARCH_LINK_*），为默认建议值，方案尚未最终制定；
    链路阈值确认后改环境变量即可，无需改代码。
    默认按 ARCH_DEP_SUFFIX_ONLY 只分析 -dep 结尾项目，未入选项目在 depFilter 中列明。
    默认不返回原始图（include_raw=false），风险项按类型各保留前 ARCH_LINK_TOP_N 条，
    以控制返回体积（121 应用 × 15 分钟原始图约 500KB，客户端消费大结果易卡住）。
    """
    dep_filter = filter_dep_projects(projects, suffix_only=dep_suffix_only, suffix=dep_suffix)
    if minute is None:
        minute = config.ARCH_LINK_WINDOW_MINUTES
    selected_projects = dep_filter["selected"]
    base = {
        "node": node,
        "projects": selected_projects,
        "depFilter": {
            k: dep_filter[k]
            for k in ("suffixOnly", "suffix", "selectedCount", "excludedCount", "excluded")
        },
        "minute": minute,
        **_task_meta(task_id),
    }
    if not selected_projects:
        base.update(
            {
                "status": "NO_DATA",
                "errMsg": "后缀筛选后无可分析项目，请检查 projects 或关闭 dep_suffix_only",
                "graph": {"nodeCount": 0, "edgeCount": 0},
                "findings": [],
            }
        )
        return base

    raw = await get_link_topology(
        node=node,
        projects=selected_projects,
        minute=minute,
        caller_range=caller_range,
        callee_range=callee_range,
        filter_node=filter_node,
        node_type=node_type,
    )
    thresholds = {
        "highInDegree": config.ARCH_LINK_HIGH_INDEGREE,
        "highOutDegree": config.ARCH_LINK_HIGH_OUTDEGREE,
        "longPathLength": config.ARCH_LINK_LONG_PATH,
        "errorRate": config.ARCH_LINK_ERROR_RATE,
        "slowElapsedMs": config.ARCH_LINK_SLOW_MS,
        "impactMinNodes": config.ARCH_LINK_IMPACT_MIN_NODES,
        "criticalPaths": config.ARCH_LINK_CRITICAL_PATHS,
        "note": "链路长度、依赖数阈值方案未定义，当前为默认建议值，需确认后调整；"
        "impactMinNodes / criticalPaths 为 0 表示关闭对应判定",
    }
    base["thresholds"] = thresholds
    base["dataUpdateTime"] = raw.get("dataUpdateTime")

    if raw.get("status") != "SUCCESS":
        return {
            **base,
            "status": raw.get("status", "NO_DATA"),
            "errMsg": raw.get("errMsg") or "链路拓扑无数据，无法进行图计算",
            "graph": {"nodeCount": 0, "edgeCount": 0},
            "findings": [],
            **({"degradedReason": raw["degradedReason"]} if raw.get("degradedReason") else {}),
            **({"timeoutSeconds": raw["timeoutSeconds"]} if raw.get("timeoutSeconds") else {}),
        }

    parsed = _extract_graph(raw.get("data"))
    nodes = parsed["nodes"]
    edges = parsed["edges"]
    limitations = [
        "链路拓扑字段语义以联调结果为准；节点/边为多字段兼容解析，"
        f"解析方式：{parsed['parseMode'] or '未识别'}",
    ]

    if not nodes or not edges:
        return {
            **base,
            "status": "DATA_INSUFFICIENT",
            "errMsg": "未能从响应中识别出节点或调用边，请传 include_raw=true 核对原始 data 结构",
            "graph": {
                "nodeCount": len(nodes),
                "edgeCount": len(edges),
                "parseMode": parsed["parseMode"],
            },
            **({"rawData": raw.get("data")} if include_raw else {}),
            "findings": [],
            "dataLimitations": limitations,
        }

    node_list = sorted(nodes.keys())
    adj: dict = {n: [] for n in node_list}
    in_deg: Counter = Counter()
    out_deg: Counter = Counter()
    for e in edges:
        s, t = e["source"], e["target"]
        if t not in adj[s]:
            adj[s].append(t)
        out_deg[s] += 1
        in_deg[t] += 1
    for n in node_list:
        in_deg.setdefault(n, 0)
        out_deg.setdefault(n, 0)

    # 反向邻接表：用于计算"谁依赖我"（影响范围）
    radj: dict = {n: [] for n in node_list}
    for u in node_list:
        for v in adj[u]:
            radj[v].append(u)

    top_n = config.ARCH_LINK_TOP_N
    findings: list[dict] = []

    # 1. 集中依赖：入度过高
    for n, d in in_deg.most_common():
        if d <= thresholds["highInDegree"]:
            break
        findings.append(
            {
                "type": "HIGH_IN_DEGREE",
                "desc": "集中依赖：多个应用依赖同一被调方，故障影响面大",
                "target": n,
                "value": d,
                "threshold": thresholds["highInDegree"],
                "statusCode": "RISK",
            }
        )

    # 2. 下游依赖过多：出度过高
    for n, d in out_deg.most_common():
        if d <= thresholds["highOutDegree"]:
            break
        findings.append(
            {
                "type": "HIGH_OUT_DEGREE",
                "desc": "下游依赖过多：单应用依赖方过多，任一下游异常均可能影响自身",
                "target": n,
                "value": d,
                "threshold": thresholds["highOutDegree"],
                "statusCode": "RISK",
            }
        )

    # 3. 循环依赖：非平凡强连通分量
    cycles = _find_scc(node_list, adj)
    cycles.sort(key=len, reverse=True)
    for comp in cycles[:top_n]:
        findings.append(
            {
                "type": "CIRCULAR_DEPENDENCY",
                "desc": "循环依赖：调用关系成环，存在相互等待与级联超时风险",
                "nodes": sorted(comp),
                "value": len(comp),
                "statusCode": "RISK",
            }
        )

    # 4. 长调用链：DAG 最长路径
    longest: dict = {"length": 0, "path": [], "skipped": True}
    if len(node_list) > config.ARCH_LINK_MAX_NODES:
        limitations.append(
            f"节点数 {len(node_list)} 超过 {config.ARCH_LINK_MAX_NODES}，"
            f"已跳过最长路径计算"
        )
    else:
        longest = _longest_path(adj, node_list)
        longest["skipped"] = False
        if longest["excludedCyclicNodes"]:
            limitations.append(
                f"最长路径计算排除了 {longest['excludedCyclicNodes']} 个环上节点"
            )
        if longest["length"] > thresholds["longPathLength"]:
            findings.append(
                {
                    "type": "LONG_CALL_CHAIN",
                    "desc": "长调用链：链路层级过深，单次请求串联故障点过多",
                    "path": longest["path"],
                    "value": longest["length"],
                    "threshold": thresholds["longPathLength"],
                    "statusCode": "RISK",
                }
            )

    # 5. 异常调用边：高错误率 / 慢调用
    abnormal = []
    no_stat_edges = 0
    for e in edges:
        st = e["stats"]
        if st["errorRate"] is None and st["elapsed"] is None:
            no_stat_edges += 1
            continue
        reasons = []
        if st["errorRate"] is not None and st["errorRate"] > thresholds["errorRate"]:
            reasons.append(f"错误率 {st['errorRate']:.2%}")
        if st["elapsed"] is not None and st["elapsed"] > thresholds["slowElapsedMs"]:
            reasons.append(f"平均耗时 {st['elapsed']:.0f}ms")
        if reasons:
            abnormal.append(
                {
                    "source": e["source"],
                    "target": e["target"],
                    **st,
                    "reason": "、".join(reasons),
                }
            )
    if no_stat_edges:
        limitations.append(
            f"{no_stat_edges} 条调用边缺少调用量/错误数/耗时字段，未参与异常判定"
        )
    abnormal.sort(key=lambda x: (-(x["errorRate"] or 0), -(x["elapsed"] or 0)))
    for a in abnormal[:top_n]:
        findings.append(
            {
                "type": "ABNORMAL_EDGE",
                "desc": "异常调用边：错误率或耗时超阈值",
                "source": a["source"],
                "target": a["target"],
                "value": a["reason"],
                "errorRate": a["errorRate"],
                "elapsed": a["elapsed"],
                "statusCode": "RISK",
            }
        )

    # 6. 上下游影响范围：反向可达节点数（该节点异常时波及的上游服务）
    impact: list[dict] = []
    if len(node_list) > config.ARCH_LINK_MAX_NODES:
        limitations.append(
            f"节点数 {len(node_list)} 超过 {config.ARCH_LINK_MAX_NODES}，"
            f"已跳过影响范围计算"
        )
    else:
        impact = _impact_scope(node_list, adj, radj)
        if thresholds["impactMinNodes"] > 0:
            for item in impact:
                if item["affectedCount"] < thresholds["impactMinNodes"]:
                    break  # 已按影响面降序，后续节点都不会触发
                findings.append(
                    {
                        "type": "WIDE_IMPACT_SCOPE",
                        "desc": "影响范围偏大：该节点异常会波及多个上游服务",
                        "target": item["node"],
                        "value": item["affectedCount"],
                        "affectedRatio": item["affectedRatio"],
                        "directCallers": item["directCallers"],
                        "affectedNodes": item["affectedNodes"],
                        "threshold": thresholds["impactMinNodes"],
                        "statusCode": "RISK",
                    }
                )
        else:
            limitations.append(
                "影响范围判定已关闭（ARCH_LINK_IMPACT_MIN_NODES=0）"
            )

    # 7. 关键依赖：经过节点的源→汇路径数，被多条核心链路共享者优先保护
    path_stats: dict = {
        "pathsThrough": {},
        "excludedCyclicNodes": 0,
        "skipped": True,
    }
    critical: list[dict] = []
    if len(node_list) > config.ARCH_LINK_MAX_NODES:
        limitations.append(
            f"节点数 {len(node_list)} 超过 {config.ARCH_LINK_MAX_NODES}，"
            f"已跳过关键依赖计算"
        )
    else:
        path_stats = {**_path_stats(adj, node_list), "skipped": False}
        if path_stats["excludedCyclicNodes"]:
            limitations.append(
                f"关键依赖计算排除了 {path_stats['excludedCyclicNodes']} 个环上节点"
            )
        ranked = sorted(
            path_stats["pathsThrough"].items(), key=lambda kv: (-kv[1], kv[0])
        )
        critical = [
            {
                "node": n,
                "pathCount": int(round(c)),
                "inDegree": in_deg[n],
                "outDegree": out_deg[n],
            }
            for n, c in ranked[:top_n]
        ]
        if thresholds["criticalPaths"] > 0:
            for c in critical:
                if c["pathCount"] < thresholds["criticalPaths"]:
                    break  # 已按路径数降序
                findings.append(
                    {
                        "type": "CRITICAL_DEPENDENCY",
                        "desc": "关键依赖：位于多条核心调用链路上，应优先保护",
                        "target": c["node"],
                        "value": c["pathCount"],
                        "inDegree": c["inDegree"],
                        "outDegree": c["outDegree"],
                        "threshold": thresholds["criticalPaths"],
                        "statusCode": "RISK",
                    }
                )
        else:
            limitations.append(
                "关键依赖判定已关闭（ARCH_LINK_CRITICAL_PATHS=0）"
            )

    # 风险项按类型封顶：大模块（121 应用）下同类风险可达数百条，逐条输出会让
    # 工具结果膨胀到数百 KB，IDE 客户端消费大结果时易表现为“卡住”
    finding_counts: dict[str, int] = {}
    kept: list[dict] = []
    for f in findings:
        t = f["type"]
        finding_counts[t] = finding_counts.get(t, 0) + 1
        if finding_counts[t] <= top_n:
            kept.append(f)
    dropped = len(findings) - len(kept)
    if dropped:
        limitations.append(
            f"风险项按类型各保留前 {top_n} 条，同类合计 {len(findings)} 条，"
            f"其余 {dropped} 条未逐条列出（计数见 findingCounts）"
        )
    findings = kept

    return {
        **base,
        "status": "SUCCESS",
        "graph": {
            "nodeCount": len(node_list),
            "edgeCount": len(edges),
            "parseMode": parsed["parseMode"],
            "isolatedNodes": sum(1 for n in node_list if in_deg[n] == 0 and out_deg[n] == 0),
        },
        "degrees": {
            "in": dict(in_deg.most_common(top_n)),
            "out": dict(out_deg.most_common(top_n)),
        },
        "topInDegree": [
            {"node": n, "inDegree": d} for n, d in in_deg.most_common(top_n)
        ],
        "topOutDegree": [
            {"node": n, "outDegree": d} for n, d in out_deg.most_common(top_n)
        ],
        "cycles": [sorted(c) for c in cycles[:top_n]],
        "cycleCount": len(cycles),
        "longestPath": longest,
        "abnormalEdges": abnormal[:top_n],
        "abnormalEdgeCount": len(abnormal),
        "impactScope": impact[:top_n],
        "criticalDependencies": critical,
        "pathStats": {
            "skipped": path_stats["skipped"],
            "excludedCyclicNodes": path_stats["excludedCyclicNodes"],
            "unit": "sourceToSinkPathCount",
            "note": "路径数按 DAG 上的源→汇路径统计，环上节点不参与",
        },
        "findingCount": len(findings),
        "findingCounts": finding_counts,
        "findings": findings,
        "dataLimitations": limitations,
        **({"rawData": raw.get("data")} if include_raw else {}),
    }


# ---------------- 三、省节点：故障域分析 ----------------

async def get_host_structure(node_code: str, reverse: bool = True) -> dict:
    """获取物理机与云主机关系（方案 2.3）

    node_code 为节点的 ctyun_node_code3（如 'hi-haikou-9'），不是节点名称。
    reverse=True 时额外返回"云主机 IP → 物理宿主机 IP"反向索引（方案分析口径）。
    """
    if not node_code:
        raise ValueError(
            "node_code 不能为空，应传节点列表中的 ctyun_node_code3（如 'hi-haikou-9'），"
            "注意不要传节点名称；该字段为空时记录为映射数据不足"
        )
    body = await get_easyviews_client().call(
        _api("/api/external/monitor/getVirtualStructure"),
        types="GET",
        req_param={"center": "true", "node": node_code},
    )
    ip_data = (body.get("data") or {}).get("ipData") or {}
    if not ip_data:
        return {
            "status": "DATA_INSUFFICIENT",
            "nodeCode": node_code,
            "hostCount": 0,
            "dataUpdateTime": _data_time(body),
            "errMsg": "未返回宿主机与云主机映射，标记为映射数据不足",
        }
    result: dict = {
        "status": "SUCCESS",
        "nodeCode": node_code,
        "hostCount": len(ip_data),
        "dataUpdateTime": _data_time(body),
    }
    if reverse:
        vm_to_host: dict[str, list[str]] = {}
        for host_ip, vm_ips in ip_data.items():
            for vm_ip in vm_ips or []:
                vm_to_host.setdefault(vm_ip, []).append(host_ip)
        result["vmToHost"] = vm_to_host
        result["vmCount"] = len(vm_to_host)
        result["conflictVms"] = [ip for ip, hosts in vm_to_host.items() if len(hosts) > 1]
    result["ipData"] = ip_data
    return result


async def get_project_deploy(node: str) -> dict:
    """获取项目与云主机部署关系（方案 2.4）

    node 为节点名称（如 '云公司->海口9'），不是 ctyun_node_code3。
    """
    if not node:
        raise ValueError("node 不能为空，如 '云公司->海口9'（节点名称，非 ctyun_node_code3）")
    body = await get_easyviews_client().call(
        _api("/api/external/monitorData/getTopologyInfo"),
        types="GET",
        req_param={"center": "true", "node": node},
    )
    data = body.get("data") or {}
    project_data = data.get("projectData") or []
    project_ip_data = data.get("projectIpData") or {}
    return {
        "status": "SUCCESS" if project_ip_data else "NO_DATA",
        "node": node,
        "projectCount": len(project_data),
        "projectData": project_data,
        "projectIpData": project_ip_data,
        "ipProjectData": data.get("ipProjectData") or {},
        "dataUpdateTime": _data_time(body),
    }


_HOST_COMPACT_FIELDS = (
    "project",
    "instanceCount",
    "mappedInstanceCount",
    "hostCount",
    "maxHostIp",
    "maxHostInstanceCount",
    "maxHostRatio",
    "coverage",
    "scenario",
    "scenarioDesc",
)


def _compact_host_item(p: dict) -> dict:
    """宿主机集中度摘要项：去掉 hostRatio / hostDistribution / 明细 IP 列表"""
    return {k: p.get(k) for k in _HOST_COMPACT_FIELDS if p.get(k) is not None}


def _host_ratio_bucket(ratio: float) -> str:
    if ratio >= 0.9:
        return ">=0.9"
    if ratio >= 0.7:
        return "0.7-0.9"
    if ratio >= 0.5:
        return "0.5-0.7"
    return "<0.5"


def _host_count_bucket(count: int) -> str:
    if count <= 1:
        return "1"
    if count == 2:
        return "2"
    if count <= 4:
        return "3-4"
    return ">=5"


async def analyze_host_concentration(
    node: str,
    node_code: str,
    only_risk: bool = False,
    task_id: Optional[str] = None,
    detail: Optional[str] = None,
    partial_limit: Optional[int] = None,
    project_limit: Optional[int] = None,
    include_ips: bool = False,
) -> dict:
    """宿主机集中度分析（方案 2.5）

    关联"项目部署（2.4）"与"物理机-云主机映射（2.3）"，输出每个项目的：
    instanceCount（云主机实例数）、hostCount（宿主机数）、
    maxHostRatio（最大宿主机集中比例）、hostRatio（逐宿主机比例）、
    coverage（映射覆盖率）、scenario（方案 2.5 场景分类）。

    only_risk=False（默认）返回全量项目，保证单实例部署与部分集中项目可见；
    传 True 时仅返回集中风险与单实例项目。
    detail="summary"（默认）只回计数、集中/单实例精简项、partial 前 N 条与聚合分布，
    明细按需用 detail="full" 取回；阈值判定逻辑不受 detail 影响。
    """
    _scope_guard(task_id, node)
    deploy = await get_project_deploy(node)
    structure = await get_host_structure(node_code, reverse=True)
    meta = {
        "dataUpdateTime": deploy.get("dataUpdateTime"),
        "hostDataUpdateTime": structure.get("dataUpdateTime"),
        **_task_meta(task_id),
    }
    if structure.get("status") == "DATA_INSUFFICIENT":
        return {
            "status": "DATA_INSUFFICIENT",
            "node": node,
            "nodeCode": node_code,
            "errMsg": structure.get("errMsg"),
            "concentrated": [],
            "singleInstances": [],
            "projects": [],
            **meta,
        }
    vm_to_host: dict[str, list[str]] = structure.get("vmToHost") or {}
    conflict_vms = set(structure.get("conflictVms") or [])

    projects = []
    insufficient = 0
    for project, ips in (deploy.get("projectIpData") or {}).items():
        ips = ips or []
        if not ips:
            continue
        unmapped = [ip for ip in ips if not vm_to_host.get(ip)]
        conflict_ips = [ip for ip in ips if ip in conflict_vms]
        hosts = [h for ip in ips if vm_to_host.get(ip) for h in vm_to_host[ip]]
        host_counts = Counter(hosts)
        total = len(ips)
        mapped = total - len(unmapped)
        max_host, max_count = host_counts.most_common(1)[0] if host_counts else (None, 0)
        if unmapped:
            insufficient += 1
        host_ratio = {
            host: round(cnt / total, 4) for host, cnt in host_counts.most_common()
        }
        scenario, scenario_desc = _host_scenario(
            instance_count=total,
            host_count=len(host_counts),
            max_ratio=round(max_count / total, 4) if total else 0.0,
            unmapped_count=len(unmapped),
            conflict_count=len(conflict_ips),
        )
        projects.append(
            {
                "project": project,
                "instanceCount": total,
                "mappedInstanceCount": mapped,
                "hostCount": len(host_counts),
                "maxHostIp": max_host,
                "maxHostInstanceCount": max_count,
                "maxHostRatio": round(max_count / total, 4) if total else 0.0,
                "hostRatio": host_ratio,
                "coverage": round(mapped / total, 4) if total else 0.0,
                "hostDistribution": dict(host_counts),
                "unmappedIps": unmapped,
                "conflictIps": conflict_ips,
                "scenario": scenario,
                "scenarioDesc": scenario_desc,
            }
        )

    concentrated = [
        p
        for p in projects
        if p["instanceCount"] > 1
        and p["coverage"] > 0
        and p["hostCount"] == 1
        and p["maxHostRatio"] >= 1.0
    ]
    single_instances = [p for p in projects if p["scenario"] == "SINGLE_INSTANCE"]
    partial = [p for p in projects if p["scenario"] == "PARTIAL_CONCENTRATION"]
    unmapped_projects = [p for p in projects if p["unmappedIps"]]
    conflict_projects = [p for p in projects if p["conflictIps"]]

    concentrated.sort(key=lambda p: -p["instanceCount"])
    projects.sort(key=lambda p: (-p["maxHostRatio"], -p["instanceCount"]))

    if not projects:
        status = "NO_DATA"
    elif insufficient:
        status = "DATA_INSUFFICIENT"
    else:
        status = "SUCCESS"

    if only_risk:
        visible = [p for p in projects if p["scenario"] in ("ALL_IN_SAME_HOST", "SINGLE_INSTANCE")]
    else:
        visible = projects

    detail = (detail or config.ARCH_OUTPUT_DETAIL or "summary").lower()
    p_limit = max(1, partial_limit if partial_limit is not None else config.ARCH_PARTIAL_LIMIT)
    pr_limit = max(1, project_limit if project_limit is not None else config.ARCH_PROJECT_LIMIT)

    base = {
        "status": status,
        "node": node,
        "nodeCode": node_code,
        "concentrationRatioThreshold": config.ARCH_HOST_CONCENTRATION_RATIO,
        "projectCount": len(projects),
        "concentratedCount": len(concentrated),
        "singleInstanceCount": len(single_instances),
        "partialConcentrationCount": len(partial),
        "unmappedProjectCount": len(unmapped_projects),
        "conflictProjectCount": len(conflict_projects),
        "insufficientMappingCount": insufficient,
        "hostCount": structure.get("hostCount"),
        "errMsg": (
            "存在无法映射宿主机的云主机，仅输出已知分布，不据此作完整故障域结论"
            if insufficient
            else ""
        ),
        **meta,
    }

    if detail == "full":
        base.update(
            {
                "outputVersion": "full",
                "conflictVms": structure.get("conflictVms") or [],
                "concentrated": concentrated,
                "singleInstances": single_instances,
                "partialConcentration": partial,
                "unmappedProjects": [
                    {
                        "project": p["project"],
                        "unmappedIps": p["unmappedIps"],
                        "coverage": p["coverage"],
                    }
                    for p in unmapped_projects
                ],
                "conflictProjects": [
                    {"project": p["project"], "conflictIps": p["conflictIps"]}
                    for p in conflict_projects
                ],
                "projects": visible,
            }
        )
        return base

    # summary：只回计数 + 关键精简项，明细按需 detail=full
    partial.sort(key=lambda p: (-p["maxHostRatio"], -p["instanceCount"]))
    ratio_buckets = Counter(_host_ratio_bucket(p["maxHostRatio"]) for p in partial)
    host_buckets = Counter(_host_count_bucket(p["hostCount"]) for p in partial)
    shown = set()
    conc_out = [_compact_host_item(p) for p in concentrated[:pr_limit]]
    shown.update(p["project"] for p in concentrated[:pr_limit])
    single_out = [_compact_host_item(p) for p in single_instances[:pr_limit]]
    shown.update(p["project"] for p in single_instances[:pr_limit])
    partial_out = [
        {
            "project": p["project"],
            "instanceCount": p["instanceCount"],
            "hostCount": p["hostCount"],
            "maxHostRatio": p["maxHostRatio"],
        }
        for p in partial[:p_limit]
    ]
    shown.update(p["project"] for p in partial[:p_limit])
    rest = [p for p in visible if p["project"] not in shown]
    projects_out = [_compact_host_item(p) for p in rest[:pr_limit]]

    base.update(
        {
            "outputVersion": "compact-v1",
            "concentrated": conc_out,
            "singleInstances": single_out,
            "partialConcentration": partial_out,
            "partialSummary": {
                "ratioBuckets": dict(ratio_buckets),
                "hostCountBuckets": dict(host_buckets),
            },
            "projects": projects_out,
            "unmappedProjects": [
                {
                    "project": p["project"],
                    "coverage": p["coverage"],
                    **({"unmappedIps": p["unmappedIps"]} if include_ips else {}),
                }
                for p in unmapped_projects[:pr_limit]
            ],
            "conflictProjects": [
                {
                    "project": p["project"],
                    **({"conflictIps": p["conflictIps"]} if include_ips else {}),
                }
                for p in conflict_projects[:pr_limit]
            ],
            "conflictVms": (structure.get("conflictVms") or [])[:pr_limit],
            "truncated": {
                "concentrated": max(0, len(concentrated) - pr_limit),
                "singleInstances": max(0, len(single_instances) - pr_limit),
                "partialConcentration": max(0, len(partial) - p_limit),
                "projects": max(0, len(rest) - pr_limit),
                "unmappedProjects": max(0, len(unmapped_projects) - pr_limit),
                "conflictProjects": max(0, len(conflict_projects) - pr_limit),
            },
            "detailHint": (
                "summary 已省略逐宿主机分布与 IP 明细；需要逐项证据时对单节点传 detail=full，"
                "partial 全量清单用 partial_limit 放大"
            ),
        }
    )
    return base


def _host_scenario(
    instance_count: int,
    host_count: int,
    max_ratio: float,
    unmapped_count: int,
    conflict_count: int,
) -> tuple[str, str]:
    """按方案 2.5 表格分类集中度场景"""
    if conflict_count:
        return (
            "MAPPING_CONFLICT",
            "存在云主机映射到多个宿主机，需核查数据更新时间及迁移记录",
        )
    if unmapped_count and host_count == 0:
        return (
            "UNMAPPED",
            "云主机均无法映射宿主机，映射数据不足，不能判定故障域",
        )
    if instance_count <= 1:
        return (
            "SINGLE_INSTANCE",
            "仅部署 1 台云主机，为单实例部署；是否构成风险取决于业务冗余要求",
        )
    if unmapped_count:
        return (
            "PARTIAL_MAPPING",
            f"存在 {unmapped_count} 台云主机无法映射宿主机，"
            f"仅按已映射部分统计分布，可能低估集中度",
        )
    if host_count == 1:
        return (
            "ALL_IN_SAME_HOST",
            "多台云主机全部位于同一物理宿主机，存在共同故障点，建议分散部署",
        )
    if max_ratio >= config.ARCH_HOST_CONCENTRATION_RATIO:
        return (
            "PARTIAL_CONCENTRATION",
            f"分布于 {host_count} 台宿主机但单一宿主机承载占比达 {max_ratio:.0%}，"
            f"需评估单宿主机故障影响",
        )
    return (
        "SPREAD",
        f"分布于 {host_count} 台宿主机且分布相对均衡",
    )



# ---------------- 四、运行指标（Prometheus，方案 1.2） ----------------


def _normalize_exclude_namespace(
    exclude_namespace,
) -> Optional[list[str]]:
    """把 exclude_namespace 参数规范化为字符串列表

    入参支持：None（用 config 默认值）、空字符串""（禁用排除）、
    单字符串（逗号分隔或单值）、list[str]（直接用）。
    返回值：list[str]（可能为空）或 None（显式禁用）。
    """
    if exclude_namespace is None:
        return list(config.ARCH_EXCLUDE_NAMESPACES) or None
    if isinstance(exclude_namespace, str):
        if exclude_namespace == "" or exclude_namespace.lower() in (
            "none",
            "null",
            "off",
        ):
            return None
        parts = [n.strip() for n in exclude_namespace.split(",") if n.strip()]
        return parts or None
    if isinstance(exclude_namespace, (list, tuple)):
        parts = [str(n).strip() for n in exclude_namespace if str(n).strip()]
        return parts or None
    return None


def _promql_string(value: str) -> str:
    """转义要放进 PromQL 双引号字符串的文本

    只做字符串层转义（反斜杠与双引号）。绝不能用 re.escape：
    PromQL 字符串沿用 Go 转义规则，`\\-`、`\\.`、`\\+` 都是非法未知转义，
    后端会直接 400；正则元字符该由调用方按 RE2 语义手写。
    """
    return str(value).replace("\\", "\\\\").replace('"', '\\"')


_PROMQL_ESCAPES = set('abfnrtv"\\')
_PROMQL_ESCAPE_LETTERS = set("xuU")


def _invalid_escapes(promql: str) -> list[str]:
    """自检 PromQL 字符串内的非法转义，返回问题片段（空列表表示合法）

    生产事故：`deployment=~".+\\-dep"` 这类由 re.escape 生成的串会被后端判
    400，而错误信息经 JSON 回显成"双反斜杠"，排查时极易误判为接口/网关问题。
    在发请求前拦截可把此类问题直接暴露在工具返回里。
    """
    problems: list[str] = []
    i = 0
    n = len(promql)
    while i < n:
        if promql[i] != '"':
            i += 1
            continue
        i += 1
        buf: list[str] = []
        while i < n and promql[i] != '"':
            ch = promql[i]
            if ch != "\\":
                buf.append(ch)
                i += 1
                continue
            nxt = promql[i + 1] if i + 1 < n else ""
            buf.append("\\" + nxt)
            if not (
                nxt in _PROMQL_ESCAPES
                or nxt in _PROMQL_ESCAPE_LETTERS
                or nxt.isdigit()
            ):
                problems.append("".join(buf[-4:]))
            i += 2
        i += 1
    return problems


def _ns_exclude_selector(exclude_namespace) -> str:
    """把排除清单转成 PromQL 的负向 regex 选择器，返回空串表示不加排除"""
    if not exclude_namespace:
        return ""
    joined = "|".join(_promql_string(ns) for ns in exclude_namespace)
    return f'namespace!~"{joined}"'


def _gray_exclude_selector(label: str) -> str:
    """把灰度应用名模式转成负向 regex 选择器；未配置时返回空串

    灰度/临时发布副本（*-gray / *-canary / *-shadow）与正式应用共用同一套
    -dep 命名，若不剔除会把发布期临时副本计入冗余与容量判定，造成误报。
    配置项按"正则片段"理解，因此不做正则转义，只做 PromQL 字符串转义。
    """
    patterns = getattr(config, "ARCH_GRAY_PATTERNS", None) or []
    if not patterns:
        return ""
    joined = "|".join(_promql_string(p) for p in patterns)
    return f'{label}!~"(?i).*(?:{joined}).*"'


def _node_label(node: Optional[str]) -> tuple[str, str] | None:
    """节点 → 实际使用的标签与取值

    默认用 cmdb_node；config.ARCH_NODE_CLUSTER_MAP 中配置的节点改用 cluster
    （这些节点的业务指标不带 cmdb_node，如 云公司->广州4d → cluster="广州4d生产"）。
    """
    if not node:
        return None
    mapped = (getattr(config, "ARCH_NODE_CLUSTER_MAP", None) or {}).get(str(node))
    if mapped:
        return "cluster", str(mapped)
    return "cmdb_node", str(node)


def _label_selector(node: str, env: str, *extra: str) -> str:
    """构造 PromQL 标签选择器

    node / env 传空串时不加入对应标签：部分环境业务 Deployment 的
    kube_* 指标没有 cmdb_node 标签，需要用"应用名 + 命名空间"口径重查，
    此时必须避免生成 cmdb_node="" 这种无意义条件。
    node 命中 ARCH_NODE_CLUSTER_MAP 时改用 cluster 标签（如广州4d）。
    exclude_namespace 通过 extra 参数传入（位置在 node/env 之后），
    调用方一般不需要关心：_rule_promql / _batch_promqls 会自动处理。
    """
    parts = []
    scope = _node_label(node)
    if scope:
        label, value = scope
        parts.append(f'{label}="{_promql_string(value)}"')
    if env:
        parts.append(f'env="{_promql_string(env)}"')
    parts.extend(e for e in extra if e)
    return "{" + ", ".join(parts) + "}"


# ReplicaSet 管理的 Pod 名：<deployment>-<rs-hash>-<pod-suffix>
_POD_DEP_REGEX = "(.+)-[a-z0-9]+-[a-z0-9]+"


def _dep_selector(
    deployment: Optional[str],
    namespace: Optional[str],
    dep_only: bool,
    exclude_namespace=None,
) -> str:
    """Deployment 标签选择器

    优先级：显式指定 namespace 精确值时 exclude_namespace 不生效；
    namespace 为空时 exclude_namespace 生效（排除非业务 ns）。
    """
    parts = []
    if namespace:
        parts.append(f'namespace="{_promql_string(namespace)}"')
    else:
        ns_excl = _ns_exclude_selector(_normalize_exclude_namespace(exclude_namespace))
        if ns_excl:
            parts.append(ns_excl)
    if deployment:
        parts.append(f'deployment="{_promql_string(deployment)}"')
    elif dep_only:
        parts.append(f'deployment=~".+{_promql_string(config.ARCH_DEP_SUFFIX)}"')
        gray = _gray_exclude_selector("deployment")
        if gray:
            parts.append(gray)
    return ",".join(parts)


def _pod_selector_for(
    deployment: Optional[str], pod_selector: Optional[str], dep_only: bool
) -> str:
    """Pod 标签选择器；未指定应用时只取 -dep 应用的 Pod（并剔除灰度副本）"""
    if deployment:
        return (
            pod_selector
            if pod_selector is not None
            else f'pod=~"{_promql_string(deployment)}-.*"'
        )
    if not dep_only:
        return ""
    parts = [f'pod=~".+{_promql_string(config.ARCH_DEP_SUFFIX)}-.*"']
    gray = _gray_exclude_selector("pod")
    if gray:
        parts.append(gray)
    return ",".join(parts)


def _derive_deployment(expr: str) -> str:
    """从 Pod 名派生 deployment 标签

    cAdvisor 的 container_* 指标不带 deployment 标签（实测口径），
    不做派生会导致 sum by (deployment) 结果为空、规则恒为数据不足。
    """
    return (
        f'label_replace({expr}, "deployment", "$1", "pod", "{_POD_DEP_REGEX}")'
    )


def _memory_limit_aggregate(
    node: str, env: str, pod: str, exclude_namespace=None
) -> str:
    """内存 limit 聚合：主口径无数据时回退另一 KSM 口径（新旧指标名兼容）。

    两侧先各自聚合为 (namespace, deployment) 再 or：标签完全对齐，
    回退口径只在主口径缺口处补位，两套指标并存时不会重复计数。
    """
    metric = config.ARCH_METRIC_MEMORY_LIMIT
    res_memory = 'resource="memory"'
    ns_excl_sel = _ns_exclude_selector(_normalize_exclude_namespace(exclude_namespace))
    ns_extra = [ns_excl_sel] if ns_excl_sel else []
    if metric.endswith("_memory_bytes"):
        primary = f"{metric}{_label_selector(node, env, pod, *ns_extra)}"
        alt = (
            f"kube_pod_container_resource_limits"
            f"{_label_selector(node, env, pod, res_memory, *ns_extra)}"
        )
    else:
        primary = f"{metric}{_label_selector(node, env, pod, res_memory, *ns_extra)}"
        alt = (
            f"kube_pod_container_resource_limits_memory_bytes"
            f"{_label_selector(node, env, pod, *ns_extra)}"
        )
    return (
        f"(sum by (namespace, deployment) ({_derive_deployment(primary)})"
        f" or sum by (namespace, deployment) ({_derive_deployment(alt)}))"
    )



_POD_SCOPED_RULES = {
    "cpu_throttle_ratio",
    "memory_ratio",
    "pod_cpu_imbalance",
    "oom_killed",
    "restart_increase",
}
_POD_NAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")

# Pod 归属解析结果缓存：键 (node, deployment, namespace)，值 (写入时间, (selector, info))
# 同一 Deployment 的多次规则复核（oom_killed / pod_cpu_imbalance 等）共用一次解析，
# 避免每条规则都重扫 kube_pod_owner
_OWNER_CACHE: dict[tuple, tuple[float, tuple[str, dict]]] = {}
_OWNER_CACHE_MAX = 500


def _owner_cache_get(key: tuple) -> Optional[tuple[str, dict]]:
    row = _OWNER_CACHE.get(key)
    if not row:
        return None
    if time.time() - row[0] > config.ARCH_POD_OWNER_CACHE_TTL:
        _OWNER_CACHE.pop(key, None)
        return None
    return row[1]


def _owner_cache_put(key: tuple, value: tuple[str, dict]) -> tuple[str, dict]:
    if config.ARCH_POD_OWNER_CACHE_TTL <= 0:
        return value
    if len(_OWNER_CACHE) >= _OWNER_CACHE_MAX:
        _OWNER_CACHE.pop(next(iter(_OWNER_CACHE)))
    _OWNER_CACHE[key] = (time.time(), value)
    return value


async def _resolve_pod_selector(
    node: str,
    env: str,
    deployment: Optional[str],
    end_ts: Optional[float],
    namespace: Optional[str] = None,
) -> tuple[str, dict]:
    """解析 Deployment 的真实 Pod 列表，生成精确 pod 选择器（方案 1.2 Pod 归属）

    优先查询 kube_pod_owner（owner_kind=ReplicaSet，owner_name 按 Deployment 前缀匹配），
    命中后生成精确正则；查询失败、为空或超过上限时回退名称前缀匹配。
    两种模式的差异写入 podSelector，避免静默改变统计口径。

    性能约束（2026-11-04）：
    - cluster 口径节点（如 广州4d）先带 cluster 标签收口，避免全集群扫 kube_pod_owner；
      收口后无命中再回退一次全局查询，实际口径记录在 podSelector.scope。
    - 结果按 (node, deployment, namespace) 缓存 ARCH_POD_OWNER_CACHE_TTL 秒；
      指定 end_ts 的历史查询不走缓存，保证按指定时刻取数。
    """
    if not deployment:
        return "", {"mode": "not-used", "reason": "未指定 deployment"}
    prefix = f'pod=~"{deployment}-.*"'
    if not config.ARCH_POD_OWNER_LOOKUP:
        return prefix, {"mode": "prefix", "reason": "ARCH_POD_OWNER_LOOKUP=false"}

    use_cache = end_ts is None and config.ARCH_POD_OWNER_CACHE_TTL > 0
    key = (str(node), str(env or ""), deployment, namespace or "")
    if use_cache:
        cached = _owner_cache_get(key)
        if cached:
            selector, info = cached
            return selector, {**info, "cached": True}

    ns_filter = f', namespace="{namespace}"' if namespace else ""
    base_filter = (
        f'owner_kind="ReplicaSet", owner_name=~"{deployment}-.*"{ns_filter}'
    )
    # 只有 cluster 口径节点收口；cmdb_node 口径不加，避免该标签缺失时多一次空查询
    scope = _node_label(node)
    attempts: list[tuple[str, str]] = []
    if scope and scope[0] == "cluster":
        attempts.append(
            (f'{scope[0]}="{_promql_string(scope[1])}", {base_filter}', "scoped")
        )
    attempts.append((base_filter, "global"))

    scoped_missed = False
    fallback: dict = {
        "mode": "prefix-fallback",
        "reason": "kube_pod_owner 未返回 Pod",
    }
    for flt, mode in attempts:
        promql = f"group by (pod) (kube_pod_owner{{{flt}}})"
        try:
            body = await get_prometheus_client().query(promql, end_ts)
        except Exception as e:
            # 异常已由客户端内部重试过，不再发起第二次查询（避免超时叠加）
            fallback = {
                "mode": "prefix-fallback",
                "reason": f"kube_pod_owner 查询失败: {e}",
            }
            break
        result = (body.get("data") or {}).get("result") or []
        pods = sorted(
            {
                str((r.get("metric") or {}).get("pod") or "")
                for r in result
                if _POD_NAME_RE.match(str((r.get("metric") or {}).get("pod") or ""))
            }
        )
        if not pods:
            if mode == "scoped":
                scoped_missed = True
            continue
        if len(pods) > config.ARCH_POD_OWNER_MAX_PODS:
            fallback = {
                "mode": "prefix-fallback",
                "reason": f"Pod 数 {len(pods)} 超过上限 {config.ARCH_POD_OWNER_MAX_PODS}",
            }
            break
        info: dict = {"mode": "owner", "podCount": len(pods), "pods": pods[:50]}
        if len(pods) > 50:
            info["truncated"] = True
        if mode == "global":
            info["scope"] = "global"
            if scoped_missed:
                info["scopedFallback"] = "cluster 收口无命中，已回退全局查询"
        else:
            info["scope"] = f'{scope[0]}="{scope[1]}"'
        value = (f'pod=~"^({"|".join(pods)})$"', info)
        return _owner_cache_put(key, value) if use_cache else value

    if scoped_missed:
        fallback["scopedFallback"] = "cluster 收口无命中，全局查询亦未返回 Pod"
    value = (prefix, fallback)
    return _owner_cache_put(key, value) if use_cache else value


def _latency_promql(node: str, env: str, deployment: Optional[str], window: str) -> str:
    """按模板生成 P99 延迟 PromQL，替换 $NODE/$ENV/$DEPLOYMENT/$WINDOW 占位符"""
    q = config.ARCH_LATENCY_PROMQL
    for key, value in (
        ("$NODE", node),
        ("$ENV", env),
        ("$DEPLOYMENT", deployment or ""),
        ("$WINDOW", window),
    ):
        q = q.replace(key, value)
    return q


async def _latency_cross_check(
    node: str,
    env: str,
    deployment: Optional[str],
    window: str,
    end_ts: Optional[float],
    sample_points: int,
    step_seconds: int,
    include_samples: bool = False,
) -> dict:
    """CPU 限流与调用延迟交叉验证（方案 1.2.2）

    限流比例超标但延迟未超阈值时，可降低处理优先级；两者同时超标则确认业务影响。
    延迟 PromQL 未配置时不发起查询，明确返回 NOT_CONFIGURED，不得臆断影响。
    """
    if not config.ARCH_LATENCY_PROMQL:
        return {
            "status": "NOT_CONFIGURED",
            "verdict": "UNKNOWN",
            "priorityHint": "UNKNOWN",
            "thresholdMs": config.ARCH_LATENCY_P99_MS,
            "note": "未配置 ARCH_LATENCY_PROMQL，无法交叉验证调用延迟；"
            "不得据此判断限流已产生业务影响",
        }
    promql = _latency_promql(node, env, deployment, window)
    sampled = await _sample_promql(promql, end_ts, sample_points, step_seconds)
    values: list[float] = []
    if _is_aggr(sampled["samples"]):
        for s in sampled["samples"].get("series", []):
            for _, raw_val in s.get("values", []):
                try:
                    v = float(raw_val) if raw_val is not None else None
                except (TypeError, ValueError):
                    v = None
                if v is not None and not math.isnan(v) and not math.isinf(v):
                    values.append(v)
    else:
        for point in sampled["samples"]:
            for row in point.get("result") or []:
                value = _float_value(row)
                if value is not None:
                    values.append(value)
    base = {
        "promql": promql,
        "window": window,
        "thresholdMs": config.ARCH_LATENCY_P99_MS,
        "unit": "ms",
        "note": "PromQL 需返回毫秒（秒级指标请 *1000）",
    }
    if include_samples:
        base["samples"] = sampled["samples"]
        base["errorList"] = sampled["errorList"]
    if not values:
        status = sampled["status"]
        return {
            **base,
            "status": "NO_DATA" if status == "SUCCESS" else status,
            "verdict": "UNKNOWN",
            "priorityHint": "UNKNOWN",
            "note": base["note"] + "；未取到延迟数据，无法判断限流是否已造成业务影响",
        }
    max_value = max(values)
    avg_value = sum(values) / len(values)
    impacted = max_value > config.ARCH_LATENCY_P99_MS
    return {
        **base,
        "status": sampled["status"],
        "valueCount": len(values),
        "maxMs": round(max_value, 2),
        "avgMs": round(avg_value, 2),
        "verdict": "CONFIRMED_IMPACT" if impacted else "NO_LATENCY_IMPACT",
        "priorityHint": "HIGH" if impacted else "LOWER",
        "note": "限流比例超标且延迟超过阈值，业务影响已确认，建议优先处理"
        if impacted
        else "限流比例超标但延迟未超阈值，可降低处理优先级并持续观察",
    }


def _rule_promql(
    rule: str,
    node: str,
    env: str,
    window: str,
    deployment: Optional[str],
    pod_selector: Optional[str] = None,
    namespace: Optional[str] = None,
    dep_only: bool = True,
    exclude_namespace=None,
) -> str:
    """按方案规则生成 PromQL（阈值判定不在工具内做）

    - rate() 使用 ARCH_RATE_WINDOW（默认 5m），与"持续 15 分钟"的观察窗口解耦
    - cAdvisor 指标无 deployment 标签，统一通过 label_replace 从 Pod 名派生
    - 未指定 deployment 时按 -dep 后缀收口，避免全节点序列撑爆上下文
    - exclude_namespace：未显式指定时用 config.ARCH_EXCLUDE_NAMESPACES 默认清单；
      显式指定空字符串 / "none" 时禁用排除；显式指定 list 或逗号分隔字符串时用该清单
      （仅在 namespace 精确值未指定时生效，显式 namespace 优先级最高）
    """
    rate_window = config.ARCH_RATE_WINDOW
    # namespace 精确值已指定 → exclude_namespace 不生效
    # namespace 为空 → 生效（排除系统/灰度 ns）
    effective_ns_excl = None if namespace else exclude_namespace
    dep = _dep_selector(deployment, namespace, dep_only, effective_ns_excl)
    ns_excl_sel = _ns_exclude_selector(
        _normalize_exclude_namespace(effective_ns_excl)
    )
    pod_extra = [ns_excl_sel] if ns_excl_sel else []
    pod = _pod_selector_for(deployment, pod_selector, dep_only)
    if rule == "deployment_replicas":
        return f"kube_deployment_spec_replicas{_label_selector(node, env, dep)}"
    if rule == "deployment_available":
        return f"kube_deployment_status_replicas_available{_label_selector(node, env, dep)}"
    if rule == "cpu_throttle_ratio":
        sel = _label_selector(
            node, env, pod, 'container!=""', 'container!="POD"', *pod_extra
        )
        throttled = _derive_deployment(
            f"rate({config.ARCH_METRIC_CPU_THROTTLED}{sel}[{rate_window}])"
        )
        periods = _derive_deployment(
            f"rate({config.ARCH_METRIC_CPU_PERIODS}{sel}[{rate_window}])"
        )
        return (
            f"sum by (namespace, deployment) ({throttled})"
            f" / sum by (namespace, deployment) ({periods})"
        )
    if rule == "memory_ratio":
        sel_ws = _label_selector(
            node, env, pod, 'container!=""', 'container!="POD"', *pod_extra
        )
        working_set = _derive_deployment(f"container_memory_working_set_bytes{sel_ws}")
        return (
            f"sum by (namespace, deployment) ({working_set})"
            f" / {_memory_limit_aggregate(node, env, pod, effective_ns_excl)}"
        )
    if rule == "hpa_replicas":
        if namespace:
            ns_sel = f'namespace="{namespace}"'
        else:
            ns_sel = ns_excl_sel or ""
        sel = _label_selector(node, env, ns_sel) if ns_sel else _label_selector(node, env)
        return (
            f"kube_horizontalpodautoscaler_status_current_replicas{sel}"
            f" / kube_horizontalpodautoscaler_spec_max_replicas{sel}"
        )
    if rule == "pod_cpu_imbalance":
        sel = _label_selector(
            node, env, pod, 'container!=""', 'container!="POD"', *pod_extra
        )
        # 返回每 Pod CPU 用量；同规格分组与最大值/平均值比值由工具层计算
        return (
            f"sum by (namespace, deployment, pod) "
            f"({_derive_deployment(f'rate(container_cpu_usage_seconds_total{sel}[{rate_window}])')})"
        )
    if rule == "oom_killed":
        sel = _label_selector(node, env, pod, 'reason="OOMKilled"', *pod_extra)
        # 口径：kube_pod_container_status_last_terminated_reason 是持久型 gauge，
        # 最近一次终止原因会一直保留（直到下一次终止被覆盖），因此即时值即可
        # 覆盖观察窗口之外的 OOM；不再用 max_over_time(...[7d:15m]) 子查询
        # （7d×15m 子查询每个采样点要评估 672 步，实测是巡检耗时主因）。
        # 若容器在 OOM 后又以其他原因重启，由 container_oom_events_total /
        # restarts 的 7d 增量补证（见 events / restart 辅助查询）。
        return _derive_deployment(
            f"kube_pod_container_status_last_terminated_reason{sel}"
        )
    if rule == "restart_increase":
        sel = _label_selector(node, env, pod, *pod_extra)
        return _derive_deployment(
            f"increase(kube_pod_container_status_restarts_total{sel}"
            f"[{config.ARCH_RULE_OOM_RESTART_WINDOW}]) > 0"
        )
    raise ValueError(
        f"未知规则: {rule}，可选值: deployment_replicas, deployment_available, "
        f"cpu_throttle_ratio, memory_ratio, hpa_replicas, pod_cpu_imbalance, "
        f"oom_killed, restart_increase"
    )


def _is_aggr(samples) -> bool:
    """判断 samples 是按 series 聚合的新格式还是按时间点展开的旧格式"""
    return isinstance(samples, dict) and "series" in samples


def _aggr_samples(samples: list[dict]) -> dict:
    """把按时间点展开的原始采样转成按 series 聚合的紧凑格式

    新格式：
        {"series": [{"metric": {...}, "values": [[ts, "val"], ...]}],
         "timePoints": [ts1, ts2, ...]}
    同一个 series 的 metric labels 只出现 1 次，值按时间序列存储，
    避免 389 deployment × 15 时间点 × 每个点重复 labels 的体积膨胀。
    """
    grouped: dict[tuple, dict] = {}
    for point in samples:
        ts = point.get("ts")
        for r in point.get("result") or []:
            labels = r.get("metric") or {}
            key = tuple(sorted(labels.items()))
            bucket = grouped.setdefault(key, {"metric": labels, "values": []})
            bucket["values"].append([ts, (r.get("value") or [None, None])[1]])
    time_points = sorted(
        {p.get("ts") for p in samples if p.get("ts") is not None}
    )
    return {"series": list(grouped.values()), "timePoints": time_points}


def _has_data(samples) -> bool:
    """聚合格式和展开格式统一的"是否有有效数据"检查"""
    if _is_aggr(samples):
        return any(s.get("values") for s in samples.get("series", []))
    return any((p.get("result") or []) for p in samples or [])


def _http_status(e: Exception) -> Optional[int]:
    """尽力提取异常里的 HTTP 状态码，用于区分"请求不合法(4xx)"与"服务端问题" """
    resp = getattr(e, "response", None)
    code = getattr(resp, "status_code", None)
    if isinstance(code, int):
        return code
    m = re.search(r"status=(\d{3})|'(\d{3}) ", str(e))
    if m:
        return int(m.group(1) or m.group(2))
    return None


def _error_kind(e: Exception) -> str:
    """给采样异常分类，便于上层区分"超时 / 4xx / 查询错误"，避免无意义重试"""
    text = str(e)
    name = type(e).__name__
    if "timeout" in text.lower() or "Timeout" in name:
        return "TIMEOUT"
    if name == "HTTPStatusError" or "status=4" in text or "status=5" in text:
        return "HTTP_ERROR"
    if "Prometheus 查询失败" in text:
        return "QUERY_ERROR"
    if "非 JSON" in text:
        return "BAD_RESPONSE"
    return "ERROR"


async def _sample_promql(
    promql: str,
    end_ts: Optional[float],
    sample_points: int,
    step_seconds: int,
    cache: Optional[dict] = None,
) -> dict:
    """对一个 PromQL 做多点历史采样（一次 query_range 取回全部点）

    旧实现按时间点串行调用 instant query（N 次往返）：8 条规则 × 多条 PromQL
    时可达数百次请求，实测单节点巡检耗时 20 分钟级。改为一次 query_range，
    请求数 N→1，时间点语义不变（每个 step 一个采样点）。

    兼容性：query_range 失败时退化为对 end_ts 的即时查询，并在状态里标
    DATA_INSUFFICIENT + degradedReason，避免把"接口不支持"误判成"无数据"。

    cache：同一次批量分析内共享的结果缓存，键为 (promql, end_ts, 点数, 步长)。
    多条规则会用到同一个 PromQL（如 deployment_replicas 与 deployment_available
    都需要 kube_deployment_spec_replicas），命中缓存即不再重复请求；
    同时保证同一次分析内各规则读到同一时刻的快照。
    """
    key = (promql, end_ts, int(sample_points), int(step_seconds))
    if cache is not None and key in cache:
        return cache[key]

    # 自检：PromQL 字符串内非法转义（如 re.escape 造成的 \-dep）会被后端 400，
    # 且错误信息经 JSON 回显后极易被误判成网关问题，这里直接拦下并说明原因
    bad_escapes = _invalid_escapes(promql)
    if bad_escapes:
        out = {
            "status": "FAIL",
            "promql": promql,
            "samples": {"series": [], "timePoints": []},
            "errorList": [
                {
                    "ts": int(end_ts or time.time()),
                    "kind": "INVALID_PROMQL",
                    "error": "PromQL 字符串含非法转义 "
                    f"{bad_escapes[:3]}：PromQL 字符串沿用 Go 转义规则，"
                    "只能用 \\\" \\\\ \\n \\t \\x \\u 等；请去掉多余的 re.escape",
                }
            ],
            "errorCount": 1,
            "errorKinds": {"INVALID_PROMQL": 1},
            "successPointCount": 0,
            "emptyPointCount": max(1, int(sample_points)),
            "failPointCount": 1,
        }
        if cache is not None:
            cache[key] = out
        return out

    client = get_prometheus_client()
    now = end_ts if end_ts is not None else time.time()
    points = max(1, int(sample_points))
    step = max(1, int(step_seconds))
    errors: list[dict] = []
    raw_points: list[dict] = []
    degraded_reason: Optional[str] = None
    responded = False  # 是否至少有一次请求成功返回（用于区分 FAIL 与 NO_DATA）

    if points == 1:
        try:
            body = await client.query(promql, int(now))
            responded = True
            raw_points = [
                {
                    "ts": int(now),
                    "result": (body.get("data") or {}).get("result") or [],
                }
            ]
        except Exception as e:
            logger.warning("metric sample failed: %s", e)
            errors.append({"ts": int(now), "error": str(e), "kind": _error_kind(e)})
    else:
        start = int(now - (points - 1) * step)
        end = int(now)
        try:
            body = await client.query_range(promql, start, end, f"{step}s")
            responded = True
            data = body.get("data") or {}
            result = data.get("result") or []
            if (data.get("resultType") or "") == "matrix":
                by_ts: dict[int, list] = {}
                for series in result:
                    labels = series.get("metric") or {}
                    for ts, val in series.get("values") or []:
                        by_ts.setdefault(int(float(ts)), []).append(
                            {"metric": labels, "value": [ts, val]}
                        )
                raw_points = [
                    {"ts": ts, "result": res} for ts, res in sorted(by_ts.items())
                ]
            else:
                raw_points = [{"ts": end, "result": result}]
        except Exception as e:
            status_code = _http_status(e)
            kind = _error_kind(e)
            errors.append({"ts": end, "error": str(e), "kind": kind})
            if status_code is not None and 400 <= status_code < 500:
                # 4xx 是请求本身不合法（转义、语法、参数），重发同一 PromQL 只会再错
                degraded_reason = (
                    f"query_range 返回 {status_code}（请求不合法），"
                    "未做降级重试，请检查 promql 与标签选择器"
                )
            elif kind == "TIMEOUT":
                # 超时说明查询过重，再发一次即时查询只会把耗时翻倍
                degraded_reason = (
                    "query_range 超时，未做降级重试（该查询已过重，"
                    "重发即时查询只会叠加等待）"
                )
            else:
                logger.warning("query_range failed, fallback to instant: %s", e)
                degraded_reason = "query_range 不可用，已降级为单点即时查询"
                try:
                    body = await client.query(promql, end)
                    responded = True
                    raw_points = [
                        {
                            "ts": end,
                            "result": (body.get("data") or {}).get("result") or [],
                        }
                    ]
                except Exception as e2:
                    logger.warning("instant fallback failed: %s", e2)
                    errors.append(
                        {"ts": end, "error": str(e2), "kind": _error_kind(e2)}
                    )

    non_empty = [p for p in raw_points if p["result"]]
    if non_empty and len(raw_points) < points:
        status = "DATA_INSUFFICIENT"
    elif non_empty:
        status = "SUCCESS"
    elif responded:
        # 请求成功但没有任何序列：采集未接入/选择器无命中，不是取数失败
        status = "NO_DATA"
    else:
        status = "FAIL"
    samples = _aggr_samples(raw_points)
    error_kinds = Counter(e.get("kind") for e in errors if e.get("kind"))
    out = {
        "status": status,
        "promql": promql,
        "samples": samples,
        "errorList": errors[:5],
        "errorCount": len(errors),
        "errorKinds": dict(error_kinds),
        "successPointCount": len(non_empty),
        "emptyPointCount": max(0, points - len(raw_points)),
        "failPointCount": len(errors),
    }
    if degraded_reason:
        out["degradedReason"] = degraded_reason
    if cache is not None:
        cache[key] = out
    return out


def _float_value(result: dict) -> Optional[float]:
    """解析 Prometheus 采样值为浮点，NaN / Inf 返回 None"""
    try:
        v = float((result.get("value") or [None, None])[1])
    except (TypeError, ValueError):
        return None
    if math.isnan(v) or math.isinf(v):
        return None
    return v


async def _sample_spec(promqls: dict, end_ts: Optional[float], step_seconds: int) -> tuple[dict, str]:
    """采样 Pod CPU 规格（limit 优先，缺失回退 request），返回 (采样结果, 来源)"""
    for key, source in (("spec", "limits"), ("spec_requests", "requests")):
        query = promqls.get(key)
        if not query:
            continue
        sampled = await _sample_promql(query, end_ts, 1, step_seconds)
        if _has_data(sampled["samples"]):
            return sampled, source
    return {"samples": {"series": []}}, "none"


def _group_imbalance(cpu_samples, spec_samples) -> list[dict]:
    """按 deployment + CPU 规格分组，计算每点"最大 / 平均"比值

    兼容 cpu_samples 的两种格式：新聚合格式 dict 或旧展开格式 list。
    spec_samples 亦同。
    返回旧展开格式 list[dict]（时间点为外层），供 arch_rule_check 做后续 include_samples 证据保留。
    缺少规格的 Pod 单独归入 cpuSpec="unknown"，由规则层判为数据不足，避免不同规格混算。
    """
    # --- 先统一转成 {ns_pod: spec_val} ---
    spec_by_pod: dict[tuple[str, str], float] = {}

    def _fill_spec_from_series(series_list):
        for s in series_list:
            labels = s.get("metric") or {}
            pod = labels.get("pod")
            if not pod:
                continue
            key = (labels.get("namespace") or "", pod)
            if key in spec_by_pod:
                continue
            for _, raw_val in s.get("values") or []:
                val, _ = arch_rules._parse_float(raw_val)
                if val is not None and val > 0:
                    spec_by_pod[key] = val
                    break

    def _fill_spec_from_points(points_list):
        for point in points_list or []:
            for r in point.get("result") or []:
                labels = r.get("metric") or {}
                pod = labels.get("pod")
                if not pod:
                    continue
                key = (labels.get("namespace") or "", pod)
                if key in spec_by_pod:
                    continue
                val = _float_value(r)
                if val is not None and val > 0:
                    spec_by_pod[key] = val

    if _is_aggr(spec_samples):
        _fill_spec_from_series(spec_samples.get("series") or [])
    else:
        _fill_spec_from_points(spec_samples)

    # --- 收集每个时间点的 (ns, dep, spec_key) -> [cpu_values] ---
    # 统一先展开成 {ts: {(ns, dep, spec_key): [val]}}
    groups_by_ts: dict[int, dict[tuple[str, str, str], list[float]]] = {}

    def _collect_from_series(series_list):
        for s in series_list:
            labels = s.get("metric") or {}
            dep = labels.get("deployment") or ""
            pod = labels.get("pod") or ""
            ns = labels.get("namespace") or ""
            if not dep:
                continue
            spec = spec_by_pod.get((ns, pod))
            spec_key = f"{spec:g}" if spec is not None else "unknown"
            for ts, raw_val in s.get("values") or []:
                v, _ = arch_rules._parse_float(raw_val)
                if v is None:
                    continue
                groups_by_ts.setdefault(int(ts), {}).setdefault(
                    (ns, dep, spec_key), []
                ).append(v)

    def _collect_from_points(points_list):
        for point in points_list or []:
            ts = int(point.get("ts"))
            bucket = groups_by_ts.setdefault(ts, {})
            for r in point.get("result") or []:
                labels = r.get("metric") or {}
                dep = labels.get("deployment") or ""
                pod = labels.get("pod") or ""
                ns = labels.get("namespace") or ""
                if not dep:
                    continue
                val = _float_value(r)
                if val is None:
                    continue
                spec = spec_by_pod.get((ns, pod))
                spec_key = f"{spec:g}" if spec is not None else "unknown"
                bucket.setdefault((ns, dep, spec_key), []).append(val)

    if _is_aggr(cpu_samples):
        _collect_from_series(cpu_samples.get("series") or [])
    else:
        _collect_from_points(cpu_samples)

    # --- 转成旧展开格式 list[dict] ---
    out: list[dict] = []
    for ts in sorted(groups_by_ts):
        result = []
        for (ns, dep, spec_key), values in groups_by_ts[ts].items():
            avg = sum(values) / len(values)
            if avg <= 0:
                continue
            metric = {"deployment": dep, "cpuSpec": spec_key}
            if ns:
                metric["namespace"] = ns
            if spec_key == "unknown":
                metric["specMissing"] = "true"
            elif avg < config.ARCH_RULE_IMBALANCE_MIN_AVG_CORES:
                metric["lowAvg"] = "true"
            result.append(
                {"metric": metric, "value": [ts, str(round(max(values) / avg, 6))]}
            )
        out.append({"ts": ts, "result": result})
    return out


_NS_MATCHER_RE = re.compile(r"namespace\s*(?:=~|!~|=|!=)")


def _has_namespace_matcher(query: str) -> bool:
    """query 中是否已存在 namespace 标签匹配器"""
    return bool(_NS_MATCHER_RE.search(query))


def _inject_ns_exclusion(query: str, ns_sel: str) -> tuple[str, bool]:
    """把 `namespace!~"..."` 拼进第一个标签选择器，返回 (新 query, 是否注入)

    用切片插入而不是 re.sub 替换：ns_sel 里可能含 `\\"` 这类反斜杠，
    正则替换模板会把反斜杠当转义处理，既可能报 bad escape，也可能吞字符。

    覆盖两种选择器形态：
    - 非空选择器 `metric{job="api"}` → 插在 `{` 之后并补逗号
    - 空选择器 `metric{}` → 插在 `{}` 内，不补逗号（补了会生成尾逗号 → 400）
    """
    if _has_namespace_matcher(query):
        return query, False
    idx = query.find("{")
    if idx < 0:
        return query, False
    rest = query[idx + 1:]
    stripped = rest.lstrip()
    if stripped.startswith("}"):
        offset = len(rest) - len(stripped)
        pos = idx + 1 + offset
        return query[:pos] + ns_sel + query[pos:], True
    return query[: idx + 1] + ns_sel + ", " + query[idx + 1:], True


def _compact_query_result(data: dict, limit: int) -> dict:
    """压缩查询结果：只返回前 limit 条序列，避免数万条序列撑爆上下文

    matrix 保留完整时间点（通常 15~60 点）；vector 只保留瞬时值。
    调用方可通过 seriesCount / truncated 判断是否被截断并按需收窄选择器。
    """
    result_type = data.get("resultType")
    result = data.get("result") or []
    total = len(result)
    if result_type == "matrix":
        items = [
            {"metric": r.get("metric") or {}, "values": r.get("values") or []}
            for r in result[:limit]
        ]
    elif result_type == "vector":
        items = [
            {
                "metric": r.get("metric") or {},
                "value": (r.get("value") or [None, None])[1],
            }
            for r in result[:limit]
        ]
    else:
        items = result[:limit]
    return {
        "resultType": result_type,
        "seriesCount": total,
        "returned": len(items),
        "truncated": total > limit,
        "result": items,
    }


async def prometheus_query(
    query: str,
    ts: Optional[float] = None,
    start_ts: Optional[float] = None,
    end_ts: Optional[float] = None,
    step: str = "1m",
    exclude_namespace=None,
    limit: int = 50,
) -> dict:
    """执行 PromQL 查询

    - 仅传 query / ts：即时查询（推荐，可用于历史时间点采样）
    - 传 start_ts + end_ts：范围查询

    注意：架构优化的"持续 N 分钟"判定应使用 arch_metric_query 的多点采样，
    不要用范围查询（数据源侧口径要求）。

    exclude_namespace：把排除清单自动拼进 query 的第一个 PromQL 标签选择器里
    （形如 `{namespace!~"(kube-system|default|...)"}`）。
    - query 里已有 namespace 匹配器时不重复注入（精确值/自定义排除优先）
    - query 无任何 `{...}` 选择器时也不注入，返回 excludeNamespaceSkippedReason
    - 显式传空字符串 / "none" 时禁用自动排除

    limit：最多返回多少条序列（默认 50），超出部分丢弃并置 truncated=true；
    结果按 {metric, 值} 压缩，不再只返回 resultType 而丢掉数值。
    """
    if not query or not query.strip():
        raise ValueError("query 不能为空")
    limit = max(1, int(limit))

    applied = False
    applied_list: list[str] = []
    skipped_reason: Optional[str] = None
    normalized = _normalize_exclude_namespace(exclude_namespace)
    if normalized:
        if _has_namespace_matcher(query):
            skipped_reason = "query 已含 namespace 匹配器，未重复注入"
        else:
            ns_sel = _ns_exclude_selector(normalized)
            query, applied = _inject_ns_exclusion(query, ns_sel)
            if applied:
                applied_list = normalized
            else:
                skipped_reason = (
                    f"query 无标签选择器，exclude_namespace 未生效，请手动加 {ns_sel}"
                )
        if skipped_reason:
            logger.warning("prometheus_query: %s", skipped_reason)

    client = get_prometheus_client()
    out = {
        "mode": "range" if start_ts is not None and end_ts is not None else "instant",
        "query": query,
        "limit": limit,
        "excludeNamespaceApplied": applied,
        "excludeNamespaceList": applied_list,
    }
    if skipped_reason:
        out["excludeNamespaceSkippedReason"] = skipped_reason
    # 自检：非法转义（如 \-dep）不发请求，直接把原因写在返回里
    bad_escapes = _invalid_escapes(query)
    if bad_escapes:
        return {
            **out,
            "status": "FAIL",
            "resultType": None,
            "seriesCount": 0,
            "returned": 0,
            "truncated": False,
            "result": [],
            "error": "PromQL 字符串含非法转义 "
            f"{bad_escapes[:3]}：PromQL 只接受 Go 风格转义（\\\" \\\\ \\n \\t \\x \\u）",
        }
    if start_ts is not None and end_ts is not None:
        body = await client.query_range(query, start_ts, end_ts, step)
    else:
        body = await client.query(query, ts)
    data = body.get("data") or {}
    out["status"] = "SUCCESS" if data.get("result") else "NO_DATA"
    out.update(_compact_query_result(data, limit))
    return out


def _summarize_samples(samples: dict, top_n: int = 20) -> dict:
    """把聚合格式 samples 压缩成极简摘要，避免 389 deployment × 15 点撑爆上下文

    返回：
        seriesCount:       覆盖 series 总数
        timePoints:        采样时间点（Unix 秒）
        coverage:          有效值覆盖度（每个 series 有效点 / 总点数 的平均）
        stat:              全量统计量（min/max/avg/volatile 数量）
        riskHints:         波动最大 Top N series 预览（含 deployment/namespace/avg/span）
    """
    series_list = samples.get("series") or []
    time_points = samples.get("timePoints") or []
    total_series = len(series_list)

    if not series_list:
        return {
            "seriesCount": 0,
            "timePoints": time_points,
            "coverage": 0.0,
            "stat": {"min": None, "max": None, "avg": None, "volatileSeries": 0},
            "riskHints": [],
        }

    all_vals: list[float] = []
    coverages: list[float] = []
    span_ranked: list[tuple[float, str, str, float]] = []  # (span, deployment, namespace, avg)
    total_points = len(time_points) or 1

    for s in series_list:
        labels = s.get("metric") or {}
        dep = labels.get("deployment", "")
        ns = labels.get("namespace", "")
        raw_values = s.get("values") or []
        valid = [float(v) for _, v in raw_values if v is not None and v != "NaN"]
        if valid:
            all_vals.extend(valid)
            coverages.append(len(valid) / total_points)
            span = max(valid) - min(valid)
            avg = sum(valid) / len(valid)
            span_ranked.append((span, dep, ns, avg))
        else:
            coverages.append(0.0)

    if not all_vals:
        return {
            "seriesCount": total_series,
            "timePoints": time_points,
            "coverage": sum(coverages) / max(1, len(coverages)),
            "stat": {"min": None, "max": None, "avg": None, "volatileSeries": 0},
            "riskHints": [],
        }

    sorted_spans = sorted(span_ranked, key=lambda x: -x[0])
    hints = [
        {"deployment": dep, "namespace": ns, "avg": round(avg, 4), "span": round(span, 4)}
        for span, dep, ns, avg in sorted_spans[:top_n]
    ]

    return {
        "seriesCount": total_series,
        "timePoints": time_points,
        "coverage": round(sum(coverages) / len(coverages), 4),
        "stat": {
            "min": round(min(all_vals), 4),
            "max": round(max(all_vals), 4),
            "avg": round(sum(all_vals) / len(all_vals), 4),
            "volatileSeries": sum(1 for span, _, _, _ in span_ranked if span > 0.1),
        },
        "riskHints": hints,
    }


async def arch_metric_query(
    rule: str,
    node: str,
    env: Optional[str] = None,
    deployment: Optional[str] = None,
    window: str = "15m",
    end_ts: Optional[float] = None,
    sample_points: int = 15,
    step_seconds: int = 60,
    namespace: Optional[str] = None,
    dep_only: bool = True,
    task_id: Optional[str] = None,
    return_raw_samples: bool = False,
    exclude_namespace=None,
) -> dict:
    """按方案 1.2 规则取指标：以多个历史时间点的即时查询模拟时间窗口采样

    ⚠️ **优先级指引**：架构风险分析应优先使用 arch_rule_check（单规则服务端判定）
    或 analyze_node_runtime（全节点批量判定），直接返回风险清单；
    本工具为"低级取数接口"，仅在需要自行做非标准分析时使用。

    参数 return_raw_samples 控制返回体量：
        False (默认)  samples 为压缩摘要 {seriesCount, coverage, stat, riskHints}
                      → 389 deployment × 15 点 ≈ 3000 tokens，适合 agent 多轮调用
        True          samples 为完整按 series 聚合格式 {series:[{metric,values}], timePoints}
                      → 389 deployment × 15 点 ≈ 90k tokens / 条，7 条累计 ~630k tokens

    rule 可选：
        deployment_replicas   期望副本数
        deployment_available  可用副本数
        cpu_throttle_ratio    CPU 限流周期比例
        memory_ratio          内存工作集/limit 比例
        hpa_replicas          HPA 当前/最大副本比例（>=1 表示达到上限）
        pod_cpu_imbalance    每 Pod CPU 用量（原始值，同规格比值由 arch_rule_check 计算）
        oom_killed            最近终止原因为 OOMKilled 的容器
        restart_increase      窗口内重启次数增长

    采样口径：从 end_ts 起向前取 sample_points 个点（步长 step_seconds），
    默认 15 点 × 60s = 15 分钟窗口，与方案"持续 15 分钟"规则一致。
    单次瞬时值不足以证明持续触发，因此固定使用多点采样。
    """
    if sample_points <= 0:
        raise ValueError("sample_points 必须大于 0")
    if step_seconds <= 0:
        raise ValueError("step_seconds 必须大于 0")
    env = env or config.ARCH_ENV
    if node:
        _scope_guard(task_id, node)
    pod_selector, pod_info = "", {"mode": "not-used"}
    if deployment and rule in _POD_SCOPED_RULES:
        pod_selector, pod_info = await _resolve_pod_selector(
            node, env, deployment, end_ts, namespace
        )
    promql = _rule_promql(
        rule,
        node,
        env,
        window,
        deployment,
        pod_selector or None,
        namespace,
        dep_only,
        exclude_namespace,
    )
    sampled = await _sample_promql(promql, end_ts, sample_points, step_seconds)
    scope_fallback = None
    if (
        not _has_data(sampled["samples"])
        and (deployment or namespace)
        # hpa_replicas 无 deployment/namespace 精确约束时不做兜底，避免跨节点误取
        and not (rule == "hpa_replicas" and not namespace)
    ):
        # 单应用场景的标签兜底：部分集群 deployment 级指标缺 cmdb_node/env，
        # 已有明确 deployment/namespace 时可直接去掉节点标签重查
        alt_promql = _rule_promql(
            rule, "", "", window, deployment, pod_selector or None,
            namespace, dep_only, exclude_namespace,
        )
        alt_sampled = await _sample_promql(
            alt_promql, end_ts, sample_points, step_seconds
        )
        if _has_data(alt_sampled["samples"]):
            scope_fallback = {
                "applied": True,
                "mode": "deployment_names",
                "reason": "cmdb_node/env 口径无数据，去掉节点标签后按应用名重查成功",
            }
            promql, sampled = alt_promql, alt_sampled
        else:
            scope_fallback = {
                "applied": False,
                "mode": "deployment_names",
                "reason": "cmdb_node/env 口径与应用名口径均无数据",
            }
    raw_samples = sampled["samples"]
    if return_raw_samples:
        samples_field = raw_samples
    else:
        samples_field = _summarize_samples(raw_samples)
    return {
        "status": sampled["status"],
        "rule": rule,
        "node": node,
        "env": env,
        "deployment": deployment,
        "namespace": namespace,
        "excludeNamespaceApplied": _normalize_exclude_namespace(exclude_namespace) is not None and not namespace,
        "excludeNamespaceList": _normalize_exclude_namespace(exclude_namespace) or [],
        "window": window,
        "rateWindow": config.ARCH_RATE_WINDOW,
        "depOnly": dep_only,
        "promql": promql,
        "podSelector": pod_info,
        "scopeFallback": scope_fallback,
        "samplePoints": sample_points,
        "stepSeconds": step_seconds,
        "successPointCount": sampled["successPointCount"],
        "emptyPointCount": sampled["emptyPointCount"],
        "failPointCount": sampled["failPointCount"],
        "errorList": sampled["errorList"],
        "samples": samples_field,
        **_task_meta(task_id),
    }


def _rule_promqls(
    rule: str,
    node: str,
    env: str,
    window: str,
    deployment: Optional[str],
    pod_selector: Optional[str] = None,
    namespace: Optional[str] = None,
    dep_only: bool = True,
    exclude_namespace=None,
) -> dict:
    """返回规则所需的主 PromQL 与辅助 PromQL

    - deployment_available 需要期望副本数配合计算差值
    - oom_killed 需要重启增长作为配套证据（方案 1.2.4）
    - memory_ratio 在主查询为空时探测 limit 是否存在，用于区分数据不足与不适用
    - exclude_namespace 透传，排除非业务命名空间（namespace 精确值指定时不生效）
    """
    # namespace 精确值已指定 → exclude_namespace 不生效
    effective_ns_excl = None if namespace else exclude_namespace
    out: dict = {
        "main": _rule_promql(
            rule,
            node,
            env,
            window,
            deployment,
            pod_selector,
            namespace,
            dep_only,
            effective_ns_excl,
        )
    }
    dep = _dep_selector(deployment, namespace, dep_only, effective_ns_excl)
    pod = _pod_selector_for(deployment, pod_selector, dep_only)
    ns_excl_sel = _ns_exclude_selector(_normalize_exclude_namespace(effective_ns_excl))
    ns_extra = [ns_excl_sel] if ns_excl_sel else []
    if rule == "deployment_available":
        out["spec"] = f"kube_deployment_spec_replicas{_label_selector(node, env, dep)}"
    elif rule == "oom_killed":
        sel_ns = _label_selector(node, env, pod, *ns_extra)
        out["restart"] = (
            f"increase(kube_pod_container_status_restarts_total"
            f"{sel_ns}[{config.ARCH_RULE_OOM_RESTART_WINDOW}])"
        )
        # cAdvisor OOM 事件计数增量：与 last_terminated_reason 互为补充证据
        out["oom_events"] = (
            f"increase(container_oom_events_total{sel_ns}"
            f"[{config.ARCH_RULE_OOM_RESTART_WINDOW}])"
        )
    elif rule == "pod_cpu_imbalance":
        # 同规格过滤：按 Pod CPU limit 分组，缺失时回退 CPU request
        res_cpu = 'resource="cpu"'
        out["spec"] = (
            f"sum by (namespace, pod) (kube_pod_container_resource_limits"
            f"{_label_selector(node, env, pod, res_cpu, *ns_extra)})"
        )
        out["spec_requests"] = (
            f"sum by (namespace, pod) (kube_pod_container_resource_requests"
            f"{_label_selector(node, env, pod, res_cpu, *ns_extra)})"
        )
    elif rule == "memory_ratio":
        out["limit_probe"] = _memory_limit_aggregate(node, env, pod, effective_ns_excl)
    return out


async def list_arch_rules() -> dict:
    """列出全部架构规则、当前阈值与规则版本（方案 4.1.4 可追溯要求）"""
    defs = arch_rules.rule_definitions()
    rules = [
        {
            "rule": name,
            "desc": d["desc"],
            "category": d["category"],
            "threshold": d["threshold"],
            "operator": d["operator"],
            "unit": d["unit"],
            "duration": d["duration"],
            "scope": d["scope"],
            "notes": d["notes"],
        }
        for name, d in defs.items()
    ]
    return {
        "status": "SUCCESS",
        "ruleVersion": config.ARCH_RULE_VERSION,
        "env": config.ARCH_ENV,
        "productName": config.ARCH_PRODUCT_NAME,
        "durationMinutes": config.ARCH_RULE_DURATION_MINUTES,
        "minCoverage": config.ARCH_RULE_MIN_COVERAGE,
        "oomRestartWindow": config.ARCH_RULE_OOM_RESTART_WINDOW,
        "depSuffixOnly": config.ARCH_DEP_SUFFIX_ONLY,
        "depSuffix": config.ARCH_DEP_SUFFIX,
        "podOwnerLookup": config.ARCH_POD_OWNER_LOOKUP,
        "latencyPromqlConfigured": bool(config.ARCH_LATENCY_PROMQL),
        "latencyP99ThresholdMs": config.ARCH_LATENCY_P99_MS,
        "ruleCount": len(rules),
        "rules": rules,
        "statusVocab": arch_rules.STATUS_CODE,
    }


async def _hpa_load_check(
    node: str,
    env: str,
    deployment: Optional[str],
    namespace: Optional[str],
    end_ts: Optional[float],
    points: int,
    step_seconds: int,
    task_id: Optional[str],
) -> dict:
    """HPA 达到副本上限后的负载组合校验（方案 1.2.2）

    方案要求"HPA 达到最大副本且资源或业务负载仍持续偏高"才构成风险，
    因此对同一 Deployment 追加 CPU 限流与内存占比判定，二者任一持续偏高即确认。
    """
    if not deployment:
        return {
            "applied": False,
            "status": "NOT_EVALUATED",
            "priorityHint": "UNKNOWN",
            "note": "节点级查询无法定位 HPA 对应工作负载，需按 deployment 复核 CPU/内存是否持续偏高",
        }
    checks: dict = {}
    for check_rule in ("cpu_throttle_ratio", "memory_ratio"):
        res = await arch_rule_check(
            rule=check_rule,
            node=node,
            env=env,
            deployment=deployment,
            namespace=namespace,
            end_ts=end_ts,
            sample_points=points,
            step_seconds=step_seconds,
            task_id=task_id,
            cross_check_latency=False,
        )
        checks[check_rule] = {
            "status": res["status"],
            "statusCode": res["statusCode"],
            "summary": res["summary"],
        }
    sustained = any(v["statusCode"] == "RISK" for v in checks.values())
    return {
        "applied": True,
        "status": "CONFIRMED_LOAD_HIGH" if sustained else "LOAD_NOT_HIGH",
        "sustainedHigh": sustained,
        "checks": checks,
        "priorityHint": "HIGH" if sustained else "OBSERVE",
        "note": "HPA 已达副本上限且 CPU/内存持续偏高，确认扩容能力不足"
        if sustained
        else "HPA 已达副本上限但 CPU/内存未持续偏高，优先级可降低并继续观察",
    }


async def _arch_rule_check_impl(
    rule: str,
    node: str,
    env: Optional[str] = None,
    deployment: Optional[str] = None,
    end_ts: Optional[float] = None,
    sample_points: Optional[int] = None,
    step_seconds: int = 60,
    task_id: Optional[str] = None,
    include_samples: bool = False,
    namespace: Optional[str] = None,
    dep_only: bool = True,
    cross_check_latency: bool = True,
    exclude_namespace=None,
) -> dict:
    """取数并执行规则判定，返回带证据的判定结论（方案 4.2 规则引擎职责）

    与 arch_metric_query 的区别：本函数**包含阈值判定**，输出
    已发现风险 / 未触发规则 / 数据不足 / 不适用 四种状态，
    并携带规则版本、阈值、实测值、持续时间与采集时间，可直接用于报告。

    include_samples=True 时额外返回 evidence（原始采样序列、辅助序列与失败点），
    对应方案 4.1.5「保留原始证据引用」；默认关闭以避免响应体过大。
    """
    defs = arch_rules.rule_definitions()
    if rule not in defs:
        raise ValueError(
            f"未知规则: {rule}，可选值: {', '.join(defs.keys())}"
        )
    if not node:
        raise ValueError("node 不能为空，如 '云公司->贵州'")
    _scope_guard(task_id, node)
    if step_seconds <= 0:
        raise ValueError("step_seconds 必须大于 0")

    env = env or config.ARCH_ENV
    duration_min = config.ARCH_RULE_DURATION_MINUTES
    window = f"{duration_min}m"
    points = sample_points if sample_points is not None else duration_min
    if points <= 0:
        raise ValueError("sample_points 必须大于 0")

    pod_selector, pod_info = "", {"mode": "not-used"}
    if deployment and rule in _POD_SCOPED_RULES:
        pod_selector, pod_info = await _resolve_pod_selector(
            node, env, deployment, end_ts, namespace
        )
    promqls = _rule_promqls(
        rule,
        node,
        env,
        window,
        deployment,
        pod_selector or None,
        namespace,
        dep_only,
        exclude_namespace,
    )
    used_promqls: dict = {"main": promqls["main"]}
    main = await _sample_promql(promqls["main"], end_ts, points, step_seconds)
    # 保留判定前的原始采样，供 include_samples 回溯（imbalance 会被改写成分组比值）
    raw_main_samples = main["samples"]

    scope_fallback = None
    if (
        not _has_data(main["samples"])
        and (deployment or namespace)
        # hpa_replicas 无 namespace 精确约束时不做兜底，避免跨节点误取
        and not (rule == "hpa_replicas" and not namespace)
    ):
        alt_promqls = _rule_promqls(
            rule, "", "", window, deployment, pod_selector or None,
            namespace, dep_only, exclude_namespace,
        )
        alt_main = await _sample_promql(
            alt_promqls["main"], end_ts, points, step_seconds
        )
        if _has_data(alt_main["samples"]):
            scope_fallback = {
                "applied": True,
                "mode": "deployment_names",
                "reason": "cmdb_node/env 口径无数据，去掉节点标签后按应用名重查成功",
            }
            promqls, main = alt_promqls, alt_main
            used_promqls = {"main": promqls["main"]}
            raw_main_samples = main["samples"]
        else:
            scope_fallback = {
                "applied": False,
                "mode": "deployment_names",
                "reason": "cmdb_node/env 口径与应用名口径均无数据",
            }

    aux: dict = {}
    extra: dict = {}
    evidence_aux: dict = {}
    if rule == "pod_cpu_imbalance":
        # 同规格过滤：先按 CPU limit 分组，缺失时回退 CPU request
        spec, spec_source = await _sample_spec(promqls, end_ts, step_seconds)
        used_promqls["spec"] = promqls.get("spec")
        if spec_source == "requests":
            used_promqls["spec_requests"] = promqls.get("spec_requests")
        evidence_aux["spec_series"] = spec["samples"]
        evidence_aux["specSource"] = spec_source
        main = {**main, "samples": _group_imbalance(main["samples"], spec["samples"])}
        extra["specSource"] = spec_source
        if spec_source == "none":
            extra["dataLimitations"] = [
                "未获取到 Pod CPU limit/request，无法同规格比较，相关目标记为数据不足"
            ]
    elif "spec" in promqls:
        spec = await _sample_promql(promqls["spec"], end_ts, points, step_seconds)
        aux["spec_series"] = spec["samples"]
        used_promqls["spec"] = promqls["spec"]
    # OOM 辅助证据只判 any(>0)，7d increase 取单点即时值即可，
    # 避免 15 个采样点各评估一次 7 天窗口（单次复核耗时主因）
    aux_points = 1 if config.ARCH_RULE_OOM_AUX_INSTANT else points
    if "restart" in promqls:
        restart = await _sample_promql(
            promqls["restart"], end_ts, aux_points, step_seconds
        )
        aux["restart_series"] = restart["samples"]
        used_promqls["restart"] = promqls["restart"]
    if "oom_events" in promqls:
        events = await _sample_promql(
            promqls["oom_events"], end_ts, aux_points, step_seconds
        )
        aux["oom_events_series"] = events["samples"]
        used_promqls["oom_events"] = promqls["oom_events"]
    if "limit_probe" in promqls and not _has_data(main["samples"]):
        probe = await _sample_promql(
            promqls["limit_probe"], end_ts, 1, step_seconds
        )
        aux["limit_exists"] = _has_data(probe["samples"])
        used_promqls["limit_probe"] = promqls["limit_probe"]

    context = {
        "node": node,
        "env": env,
        "deployment": deployment,
        "namespace": namespace,
        "window": window,
    }
    result = arch_rules.evaluate(
        rule, main["samples"], points, step_seconds, context, aux
    )
    result.update(extra)
    result["collectionStatus"] = main["status"]
    result["promqls"] = used_promqls
    result["scopeFallback"] = scope_fallback
    result["dataQuality"]["failPointCount"] = main["failPointCount"]
    result["dataQuality"]["successPointCount"] = main["successPointCount"]
    result.update(_task_meta(task_id))

    if include_samples:
        # 方案 4.1.5：保留原始证据引用，供报告逐条回溯
        result["evidence"] = {
            "main": {
                "promql": promqls["main"],
                "collectionStatus": main["status"],
                "samples": raw_main_samples,
                "successPointCount": main["successPointCount"],
                "emptyPointCount": main["emptyPointCount"],
                "failPointCount": main["failPointCount"],
                "errorList": main["errorList"],
                "note": "samples 为按 series 聚合的紧凑格式："
                        "series[].metric 为 labels（仅出现 1 次），"
                        "series[].values 为 [[ts, 'val'], ...] 时间序列；"
                        "timePoints 为全部采样时间点（Unix 秒）。"
                        "体积比旧展开格式缩减 ~15x，避免 389 deployment × 15 点撑爆上下文",
            },
            "aux": {**aux, **evidence_aux},
        }
        if rule == "pod_cpu_imbalance":
            result["evidence"]["derived"] = {
                "desc": "按同规格分组的每点最大/平均 CPU 比值（实际判定依据）",
                "samples": main["samples"],
            }

    if main["status"] == "FAIL":
        result["status"] = arch_rules.STATUS_INSUFFICIENT
        result["statusCode"] = arch_rules.STATUS_CODE[arch_rules.STATUS_INSUFFICIENT]
        result["summary"] = "指标采样全部失败，记为数据不足，不得据此判定无风险"

    result["podSelector"] = pod_info
    if rule == "hpa_replicas":
        # 方案 1.2.2：达到副本上限需结合资源/业务负载持续偏高，才构成扩容不足
        if result.get("statusCode") == "RISK":
            load = await _hpa_load_check(
                node, env, deployment, namespace, end_ts, points, step_seconds, task_id
            )
            if load.get("applied"):
                result["priorityHint"] = load.get("priorityHint")
            result["loadCrossCheck"] = load
        else:
            result["loadCrossCheck"] = {
                "applied": False,
                "priorityHint": "UNKNOWN",
                "note": "本次 HPA 未达到副本上限或规则不适用，不做负载组合校验",
            }
    if rule == "cpu_throttle_ratio" and cross_check_latency:
        # 方案 1.2.2：限流比例需与调用延迟交叉验证后再定优先级
        cross = await _latency_cross_check(
            node,
            env,
            deployment,
            window,
            end_ts,
            points,
            step_seconds,
            include_samples=include_samples,
        )
        if cross["status"] == "NOT_CONFIGURED":
            cross["applied"] = False
            cross["priorityHint"] = "UNKNOWN"
        elif result.get("statusCode") != "RISK":
            cross["applied"] = False
            cross["priorityHint"] = "UNKNOWN"
            max_ms = cross.get("maxMs")
            if max_ms is not None:
                cross["note"] = f"本次限流未触发；延迟 P99 观测值 {max_ms} ms 仅作参考"
            else:
                cross["note"] += "；本次限流未触发"
        else:
            cross["applied"] = True
        result["latencyCrossCheck"] = cross
    return result


def _rule_timeout_result(
    rule: str,
    node: str,
    env: Optional[str],
    deployment: Optional[str],
    namespace: Optional[str],
    task_id: Optional[str],
    timeout: float,
) -> dict:
    """超时兜底结果：保持四态契约，把未完成部分记为数据不足"""
    rule_def = arch_rules.rule_definitions().get(rule) or {
        "desc": rule,
        "category": "",
        "threshold": None,
        "operator": "",
        "unit": "",
        "duration": None,
        "scope": "",
        "notes": "",
    }
    return {
        "ruleVersion": config.ARCH_RULE_VERSION,
        "rule": rule,
        "ruleDesc": rule_def["desc"],
        "category": rule_def["category"],
        "status": arch_rules.STATUS_INSUFFICIENT,
        "statusCode": arch_rules.STATUS_CODE[arch_rules.STATUS_INSUFFICIENT],
        "summary": f"规则复核超过 {timeout:g} 秒未完成，已中止取数，"
        "记为数据不足，不得据此判定无风险",
        "threshold": {
            "value": rule_def["threshold"],
            "operator": rule_def["operator"],
            "unit": rule_def["unit"],
            "duration": rule_def["duration"],
        },
        "context": {
            "node": node,
            "env": env or config.ARCH_ENV,
            "deployment": deployment,
            "namespace": namespace,
            "window": f"{config.ARCH_RULE_DURATION_MINUTES}m",
        },
        "items": [],
        "dataQuality": {
            "samplePoints": None,
            "timeoutSeconds": timeout,
            "note": "超时中止，未产出可用采样",
        },
        "scope": rule_def["scope"],
        "notes": rule_def["notes"],
        "collectionStatus": "FAIL",
        "degradedReason": "TIMEOUT",
        "timeoutSeconds": timeout,
        "podSelector": {"mode": "unknown", "reason": "超时中止，Pod 归属未完成"},
        **_task_meta(task_id),
    }


async def arch_rule_check(
    rule: str,
    node: str,
    env: Optional[str] = None,
    deployment: Optional[str] = None,
    end_ts: Optional[float] = None,
    sample_points: Optional[int] = None,
    step_seconds: int = 60,
    task_id: Optional[str] = None,
    include_samples: bool = False,
    namespace: Optional[str] = None,
    dep_only: bool = True,
    cross_check_latency: bool = True,
    exclude_namespace=None,
) -> dict:
    """规则复核入口：对单次复核施加总时限（ARCH_RULE_TIMEOUT，默认 45s）

    MCP 走 Streamable HTTP，工具执行期间响应流是空的，网关空闲超时（常见 60s）
    会先掐断连接，表现为"工具侧无响应就断开"。这里在总时限到达时取消未完成的
    子查询并返回 INSUFFICIENT + degradedReason=TIMEOUT，保证一定有响应。
    """
    timeout = config.ARCH_RULE_TIMEOUT
    if timeout <= 0:
        return await _arch_rule_check_impl(
            rule, node, env, deployment, end_ts, sample_points, step_seconds,
            task_id, include_samples, namespace, dep_only, cross_check_latency,
            exclude_namespace,
        )
    try:
        return await asyncio.wait_for(
            _arch_rule_check_impl(
                rule, node, env, deployment, end_ts, sample_points,
                step_seconds, task_id, include_samples, namespace, dep_only,
                cross_check_latency, exclude_namespace,
            ),
            timeout=timeout,
        )
    except asyncio.TimeoutError:
        logger.warning(
            "arch_rule_check timeout (%.0fs): rule=%s node=%s deployment=%s",
            timeout, rule, node, deployment,
        )
        return _rule_timeout_result(
            rule, node, env, deployment, namespace, task_id, timeout
        )


# ---------------- 五、Pinpoint ↔ EasyViews 应用名映射（方案 1.3 前置） ----------------

_NUM_SUFFIX_RE = re.compile(r"-\d{3,}$")
_NON_ALNUM_RE = re.compile(r"[^a-z0-9]")


def _strip_dep_suffix(name: str, suffix: Optional[str] = None) -> str:
    """去除可配置的 -dep 类后缀（支持重复后缀，如 xxx-dep-dep）"""
    suffix = config.ARCH_DEP_SUFFIX if suffix is None else suffix
    if not suffix:
        return name
    pattern = re.compile(re.escape(suffix) + "$", re.IGNORECASE)
    out = name
    while pattern.search(out):
        stripped = pattern.sub("", out)
        if stripped == out:
            break
        out = stripped
    return out


def _normalize_app_name(name: str, dep_suffix: Optional[str] = None) -> str:
    """应用名归一化：去 -dep 后缀、去时间戳式数字尾缀、去所有非字母数字

    例：infodevice-consumer-pro-005925-dep -> infodeviceconsumerpro
    dep_suffix 默认取 ARCH_DEP_SUFFIX，可通过参数覆盖。
    """
    s = _strip_dep_suffix(str(name or "").lower(), dep_suffix)
    s = _NUM_SUFFIX_RE.sub("", s)
    return _NON_ALNUM_RE.sub("", s)


def filter_dep_projects(
    projects: list[str],
    suffix_only: Optional[bool] = None,
    suffix: Optional[str] = None,
) -> dict:
    """按 -dep 命名规则筛选第一阶段分析范围（方案 1.2）

    方案要求第一阶段仅对名称以 -dep 结尾的应用执行容器检查，其余记录为未覆盖。
    本函数不做静默丢弃：同时返回 selected 与 excluded 清单，便于报告中说明范围。
    suffix_only 默认取 ARCH_DEP_SUFFIX_ONLY，suffix 默认取 ARCH_DEP_SUFFIX。
    """
    suffix_only = config.ARCH_DEP_SUFFIX_ONLY if suffix_only is None else suffix_only
    suffix = config.ARCH_DEP_SUFFIX if suffix is None else suffix
    uniq = list(dict.fromkeys(str(p).strip() for p in projects or [] if str(p).strip()))
    pattern = re.compile(re.escape(suffix) + "$", re.IGNORECASE) if suffix else None
    if not suffix_only:
        selected, excluded = uniq, []
    elif pattern:
        selected = [p for p in uniq if pattern.search(p)]
        excluded = [p for p in uniq if not pattern.search(p)]
    else:
        selected, excluded = [], uniq
    return {
        "status": "SUCCESS" if selected else "NO_DATA",
        "suffixOnly": bool(suffix_only),
        "suffix": suffix,
        "total": len(uniq),
        "selectedCount": len(selected),
        "excludedCount": len(excluded),
        "selected": selected,
        "excluded": excluded,
        "note": (
            "第一阶段仅覆盖 -dep 结尾应用，excluded 中的应用本阶段未检查，"
            "不得据此判定无风险"
            if suffix_only
            else "未启用后缀筛选，全部应用纳入本阶段分析"
        ),
    }


def _score_match(project_norm: str, app_norm: str, min_len: int) -> tuple[float, str]:
    """计算名称匹配置信分，返回 (score, matchType)"""
    if not project_norm or not app_norm:
        return 0.0, ""
    if project_norm == app_norm:
        return 1.0, "exact"
    if len(project_norm) >= min_len and project_norm in app_norm:
        return 0.85, "project_in_pinpoint"
    if len(app_norm) >= min_len and app_norm in project_norm:
        return 0.75, "pinpoint_in_project"
    return 0.0, ""


def _extract_applications(body) -> list[dict]:
    """兼容 Pinpoint 应用列表的多种响应结构"""
    if isinstance(body, list):
        apps = body
    elif isinstance(body, dict):
        apps = body.get("applications") or body.get("data") or []
    else:
        apps = []
    if isinstance(apps, dict):
        apps = apps.get("applications") or []
    return [a for a in apps if isinstance(a, dict)]


async def build_app_mapping(
    node: str,
    product_name: Optional[str] = None,
    projects: Optional[list[str]] = None,
    min_score: Optional[float] = None,
) -> dict:
    """构建 EasyViews projectName ↔ Pinpoint applicationName 映射（方案 1.3 前置）

    方案 1.3 要求链路分析结果关联回模块归属，但两个平台的应用命名体系不同：
    - EasyViews：ehome-api-prod、infodevice-consumer-pro-005925-dep
    - Pinpoint：GuiZhou_infodevice-report_Product_infodevice-consumer-pro-_C

    本工具按归一化名称做精确/子串匹配，输出置信分与匹配方式供人工抽查。
    projects 为空时自动从 get_module_topology 提取该节点全部 projectName。
    """
    from tools import list_applications as _list_applications

    if not node:
        raise ValueError("node 不能为空，如 '云公司->贵州'")
    product_name = product_name or config.ARCH_PRODUCT_NAME
    min_score = config.ARCH_MAPPING_MIN_SCORE if min_score is None else min_score
    min_len = config.ARCH_MAPPING_MIN_LEN

    module_of: dict[str, list[str]] = {}
    if projects:
        project_list = list(dict.fromkeys(projects))
        topology_note = "projects 由调用方显式传入，未查询模块拓扑"
    else:
        topology = await get_module_topology(node=node, product_name=product_name)
        if topology.get("status") != "SUCCESS":
            return {
                "status": topology.get("status", "NO_DATA"),
                "node": node,
                "product": product_name,
                "errMsg": topology.get("errMsg", "未获取到模块拓扑，无法提取 projectName"),
                "mapping": {},
            }
        for m in topology.get("modules") or []:
            module_label = m.get("moduleNameCn") or m.get("moduleName") or m.get("moduleId")
            for p in m.get("projectName") or []:
                module_of.setdefault(p, [])
                if module_label not in module_of[p]:
                    module_of[p].append(module_label)
        project_list = list(module_of.keys())
        topology_note = (
            f"项目清单来自模块拓扑，deployNode={topology.get('deployNode')}"
        )

    apps_body = await _list_applications()
    applications = _extract_applications(apps_body)
    if not applications:
        return {
            "status": "NO_DATA",
            "node": node,
            "errMsg": "未获取到 Pinpoint 应用列表，无法建立映射",
            "projectCount": len(project_list),
            "mapping": {},
        }

    app_norm_cache = [
        (
            app,
            _normalize_app_name(app.get("applicationName") or ""),
        )
        for app in applications
    ]

    mapping: dict[str, list[dict]] = {}
    unmatched: list[dict] = []
    conflicts: list[dict] = []
    reverse: dict[str, list[str]] = {}

    for pname in project_list:
        pnorm = _normalize_app_name(pname)
        if len(pnorm) < min_len:
            unmatched.append(
                {
                    "project": pname,
                    "reason": f"归一化名称过短（{pnorm}，{len(pnorm)} 字符 < {min_len}），"
                    f"为避免误配已跳过，需人工指定映射",
                }
            )
            continue
        candidates = []
        for app, anorm in app_norm_cache:
            score, match_type = _score_match(pnorm, anorm, min_len)
            if score >= min_score:
                candidates.append(
                    {
                        "applicationName": app.get("applicationName"),
                        "serviceType": app.get("serviceType"),
                        "serviceTypeCode": app.get("serviceTypeCode") or app.get("code"),
                        "score": score,
                        "matchType": match_type,
                    }
                )
        if not candidates:
            unmatched.append(
                {
                    "project": pname,
                    "normalized": pnorm,
                    "reason": "Pinpoint 应用列表中未找到置信分达标的候选",
                }
            )
            continue
        candidates.sort(key=lambda c: (-c["score"], c["applicationName"] or ""))
        top_score = candidates[0]["score"]
        best = [c for c in candidates if c["score"] == top_score]
        mapping[pname] = {
            "normalized": pnorm,
            "modules": module_of.get(pname, []),
            "matched": best if len(best) == 1 else [],
            "candidates": candidates,
            "ambiguous": len(best) > 1,
        }
        if len(best) > 1:
            conflicts.append(
                {
                    "project": pname,
                    "reason": f"{len(best)} 个 Pinpoint 应用同获 {top_score} 置信分，无法唯一确定",
                    "candidates": [c["applicationName"] for c in best],
                }
            )
        else:
            reverse.setdefault(best[0]["applicationName"], []).append(pname)

    unique_matched = sum(1 for v in mapping.values() if v["matched"])
    return {
        "status": "SUCCESS" if mapping else "NO_DATA",
        "node": node,
        "product": product_name,
        "projectCount": len(project_list),
        "pinpointAppCount": len(applications),
        "uniqueMatchedCount": unique_matched,
        "ambiguousCount": len(conflicts),
        "unmatchedCount": len(unmatched),
        "evidence": {
            "minScore": min_score,
            "minLen": min_len,
            "matchTypes": {
                "exact": "归一化后完全相同（1.0）",
                "project_in_pinpoint": "projectName 是 Pinpoint 应用名子串（0.85）",
                "pinpoint_in_project": "Pinpoint 应用名是 projectName 子串（0.75）",
            },
            "topologyNote": topology_note,
        },
        "mapping": mapping,
        "reverseIndex": reverse,
        "unmatched": unmatched,
        "conflicts": conflicts,
        "notes": "映射基于名称归一化模糊匹配，含置信分与匹配方式，"
        "结论使用前应抽查确认；置信分不达标项需人工补充映射后重新分析。",
    }


# ---------------- 六、批量编排与报告汇总（方案 1.2 / 4.1.1 / 第五章） ----------------

_STATUS_ORDER = {"RISK": 0, "INSUFFICIENT": 1, "NOT_APPLICABLE": 2, "OK": 3}


def _module_label(module: dict) -> str:
    """模块展示名：中文名优先"""
    return (
        module.get("moduleNameCn")
        or module.get("moduleName")
        or module.get("moduleId")
        or "未命名模块"
    )


async def resolve_analysis_scope(
    node: str,
    product_name: Optional[str] = None,
    dep_suffix_only: Optional[bool] = None,
    dep_suffix: Optional[str] = None,
    task_id: Optional[str] = None,
) -> dict:
    """确定本次分析范围：节点 → 模块 → -dep 应用清单（方案 1.2 / 4.1.1）

    只做确定性整理，不做风险判定：
    - 输出模块清单、每模块纳入/排除的应用数、应用的多模块归属
    - 应用跨模块重复时保留全部归属（方案 1.2 明确要求）
    - 本工具结果可直接作为报告首页"分析范围"
    """
    if not node:
        raise ValueError("node 不能为空，如 '云公司->贵州'")
    _scope_guard(task_id, node)
    product_name = product_name or config.ARCH_PRODUCT_NAME
    dep_suffix_only = (
        config.ARCH_DEP_SUFFIX_ONLY if dep_suffix_only is None else dep_suffix_only
    )
    dep_suffix = config.ARCH_DEP_SUFFIX if dep_suffix is None else dep_suffix
    meta = _task_meta(task_id)

    topology = await get_module_topology(node=node, product_name=product_name)
    if topology.get("status") != "SUCCESS":
        return {
            "status": topology.get("status", "NO_DATA"),
            "node": node,
            "product": product_name,
            "errMsg": topology.get("errMsg", "未获取到模块拓扑，无法确定分析范围"),
            "ruleVersion": config.ARCH_RULE_VERSION,
            **meta,
        }

    modules: list[dict] = []
    app_modules: dict[str, list[str]] = {}
    all_selected: list[str] = []
    all_excluded: list[str] = []
    for m in topology.get("modules") or []:
        label = _module_label(m)
        projects = list(dict.fromkeys(m.get("projectName") or []))
        filtered = filter_dep_projects(projects, dep_suffix_only, dep_suffix)
        selected = filtered["selected"]
        excluded = filtered["excluded"]
        for app in selected:
            app_modules.setdefault(app, [])
            if label not in app_modules[app]:
                app_modules[app].append(label)
        all_selected.extend(selected)
        all_excluded.extend(excluded)
        modules.append(
            {
                "module": label,
                "moduleId": m.get("moduleId"),
                "moduleName": m.get("moduleName"),
                "objectType": m.get("objectType"),
                "projectCount": len(projects),
                "appCount": len(selected),
                "apps": selected,
                "excludedCount": len(excluded),
                "excluded": excluded,
            }
        )

    selected_unique = list(dict.fromkeys(all_selected))
    excluded_unique = list(dict.fromkeys(all_excluded))
    duplicates = [
        {"app": app, "modules": labels}
        for app, labels in sorted(app_modules.items())
        if len(labels) > 1
    ]
    return {
        "status": "SUCCESS" if selected_unique else "NO_DATA",
        "node": node,
        "deployNode": topology.get("deployNode"),
        "product": product_name,
        "ruleVersion": config.ARCH_RULE_VERSION,
        "depSuffixOnly": bool(dep_suffix_only),
        "depSuffix": dep_suffix,
        "moduleCount": len(modules),
        "appCount": len(selected_unique),
        "excludedCount": len(excluded_unique),
        "duplicateAppCount": len(duplicates),
        "modules": modules,
        "appModules": app_modules,
        "duplicateApps": duplicates,
        "excludedPreview": excluded_unique[:50],
        "excludedTruncated": len(excluded_unique) > 50,
        "dataUpdateTime": topology.get("dataUpdateTime"),
        "note": "appModules 为应用→模块的多对多归属；未入选应用（excluded）本阶段未检查，"
        "不得据此判定无风险",
        **meta,
    }


def _batch_promqls(
    rule: str,
    node: str,
    env: str,
    dep_only: bool = True,
    exclude_namespace=None,
    deployments: Optional[list[str]] = None,
) -> dict:
    """批量分析用 PromQL：一次查询覆盖节点内全部 -dep 应用

    与单应用模式的区别：
    - deployment 维度由 label_replace 从 Pod 名派生，结果按 (namespace, deployment) 聚合
    - pod_cpu_imbalance 在 PromQL 内直接算最大/平均比值，并打 batchMode 标记
    - exclude_namespace 透传到 _dep_selector / _label_selector，排除非业务命名空间

    deployments 非空时改用"应用名直查"口径（不依赖 cmdb_node/env 标签）：
    部分生产集群的业务 Deployment 在 kube-state-metrics 中不带节点标签，
    无法按节点标签筛选，只能用已知应用名构造 deployment / pod 选择器。
    """
    rate_window = config.ARCH_RATE_WINDOW
    if deployments:
        # 应用名只做字符串转义、不做正则转义（PromQL 的 Go 字符串不接受 `\-`），
        # 未转义的 `.` 可能把同命名空间的名字近似应用一并匹配进来；越界序列
        # 由 `_batch_identity_audit` 在判定前按应用清单剔除，不依赖正则精度。
        names = "|".join(
            _promql_string(str(d)) for d in dict.fromkeys(deployments) if str(d)
        )
        dep = f'deployment=~"^({names})$"'
        pod = f'pod=~"^({names})-.*"'
    else:
        dep = _dep_selector(None, None, dep_only, exclude_namespace)
        pod = _pod_selector_for(None, None, dep_only)
    ns_excl_sel = _ns_exclude_selector(_normalize_exclude_namespace(exclude_namespace))
    ns_extra = [ns_excl_sel] if ns_excl_sel else []
    sel_pod = _label_selector(
        node, env, pod, 'container!=""', 'container!="POD"', *ns_extra
    )
    dep_sel = _label_selector(node, env, dep)
    if rule == "deployment_replicas":
        return {"main": f"kube_deployment_spec_replicas{dep_sel}"}
    if rule == "deployment_available":
        return {
            "main": f"kube_deployment_status_replicas_available{dep_sel}",
            "spec": f"kube_deployment_spec_replicas{dep_sel}",
        }
    if rule == "cpu_throttle_ratio":
        throttled = _derive_deployment(
            f"rate({config.ARCH_METRIC_CPU_THROTTLED}{sel_pod}[{rate_window}])"
        )
        periods = _derive_deployment(
            f"rate({config.ARCH_METRIC_CPU_PERIODS}{sel_pod}[{rate_window}])"
        )
        return {
            "main": f"sum by (namespace, deployment) ({throttled})"
            f" / sum by (namespace, deployment) ({periods})"
        }
    if rule == "memory_ratio":
        working_set = _derive_deployment(
            f"container_memory_working_set_bytes{sel_pod}"
        )
        return {
            "main": f"sum by (namespace, deployment) ({working_set})"
            f" / {_memory_limit_aggregate(node, env, pod, exclude_namespace)}",
            "limit_probe": _memory_limit_aggregate(
                node, env, pod, exclude_namespace
            ),
        }
    if rule == "hpa_replicas":
        return {
            "main": _rule_promql(
                rule, node, env, "", None, None, None, dep_only, exclude_namespace
            )
        }
    if rule == "pod_cpu_imbalance":
        per_pod = (
            f"sum by (namespace, deployment, pod) "
            f"({_derive_deployment(f'rate(container_cpu_usage_seconds_total{sel_pod}[{rate_window}])')})"
        )
        ratio = (
            f"max by (namespace, deployment) ({per_pod})"
            f" / avg by (namespace, deployment) ({per_pod})"
        )
        return {
            "main": f'label_replace({ratio}, "batchMode", "true", "deployment", ".+")'
        }
    if rule == "oom_killed":
        reason = 'reason="OOMKilled"'
        sel_pod_ns = _label_selector(node, env, pod, *ns_extra)
        reason_sel = _label_selector(node, env, pod, reason, *ns_extra)
        restart_metric = (
            f"increase(kube_pod_container_status_restarts_total{sel_pod_ns}"
            f"[{config.ARCH_RULE_OOM_RESTART_WINDOW}])"
        )
        event_metric = (
            f"increase(container_oom_events_total{sel_pod_ns}"
            f"[{config.ARCH_RULE_OOM_RESTART_WINDOW}])"
        )
        return {
            # 即时值即可（gauge 持久保存最近一次终止原因），理由见 _rule_promql
            "main": _derive_deployment(
                f"kube_pod_container_status_last_terminated_reason{reason_sel}"
            ),
            "restart": _derive_deployment(restart_metric),
            "oom_events": _derive_deployment(event_metric),
        }
    if rule == "restart_increase":
        sel_pod_ns = _label_selector(node, env, pod, *ns_extra)
        return {
            "main": _derive_deployment(
                f"increase(kube_pod_container_status_restarts_total{sel_pod_ns}"
                f"[{config.ARCH_RULE_OOM_RESTART_WINDOW}]) > 0"
            )
        }
    raise ValueError(f"未知规则: {rule}")


def _batch_identity_audit(
    samples,
    allowed_apps: Optional[set],
    pod_level: bool,
) -> tuple[dict, dict]:
    """批量结果身份审计：越界序列剔除 + 同一身份多序列检测

    生产问题：同一命名空间内两个 Deployment 的 KSM 序列除 deployment 标签外
    完全相同（同集群、同 kube-state-metrics 实例）。批量路径若用应用名拼正则
    匹配（未转义 `.` 等元字符）或丢失集群约束，两条序列会同时进入结果，按
    时间点配对时后一条覆盖前一条，表现为"应用 A 被应用 B 的数值串配"。
    单应用复核用 `deployment="A"` 精确过滤，因此结论正确。

    这里在判定前做两道防线：
    1. 序列的 deployment 不在应用清单内 → 直接剔除并计数；
    2. 同一 (namespace, deployment[, pod]) 出现多组不同标签序列 → 无法唯一
       归属，整组剔除并上抛，让上层记"数据不足"，绝不取其中一条。
    """
    if not _is_aggr(samples):
        return samples, {"outOfScopeSeries": 0, "ambiguousIdentities": []}

    kept_series: list[dict] = []
    out_of_scope = 0
    for series in samples.get("series") or []:
        labels = series.get("metric") or {}
        app = labels.get("deployment")
        if allowed_apps and app and app not in allowed_apps:
            out_of_scope += 1
            continue
        kept_series.append(series)

    def _identity(labels: dict) -> tuple[str, str, str, str]:
        return (
            labels.get("namespace") or "",
            labels.get("deployment") or "",
            (labels.get("pod") or "") if pod_level else "",
            (labels.get("container") or "") if pod_level else "",
        )

    series_by_identity: dict[tuple[str, str, str, str], list[dict]] = {}
    for series in kept_series:
        labels = series.get("metric") or {}
        if not labels.get("deployment"):
            continue
        series_by_identity.setdefault(_identity(labels), []).append(series)

    # 只有"同一身份、同一时间点出现不同数值"才算串配；同值重复抓取只保留一条，
    # 避免把不同 job 的同源序列误判为歧义
    ambiguous: list[str] = []
    deduped: list[dict] = []
    for ident, series_list in series_by_identity.items():
        values_by_ts: dict = {}
        conflict = False
        for series in series_list:
            for ts, value in series.get("values") or []:
                if ts in values_by_ts and values_by_ts[ts] != value:
                    conflict = True
                    break
                values_by_ts.setdefault(ts, value)
            if conflict:
                break
        if conflict:
            ambiguous.append("/".join(part for part in ident if part))
        elif series_list:
            deduped.append(series_list[0])

    no_dep = [
        series for series in kept_series if not (series.get("metric") or {}).get("deployment")
    ]
    return (
        {"series": deduped + no_dep, "timePoints": samples.get("timePoints") or []},
        {"outOfScopeSeries": out_of_scope, "ambiguousIdentities": sorted(ambiguous)[:20]},
    )


async def _run_batch_rule(
    rule: str,
    node: str,
    env: str,
    points: int,
    step_seconds: int,
    end_ts: Optional[float],
    dep_only: bool,
    exclude_namespace=None,
    deployments: Optional[list[str]] = None,
    cache: Optional[dict] = None,
) -> dict:
    """执行单条规则的节点级批量取数与判定

    取数口径两级：
    1. 默认按 cmdb_node/env 标签筛选（节点标签完整时最准确）
    2. 主查询无数据且已知应用清单时，自动改用应用名选择器重查
       （规避部分集群 kube_* 指标缺 cmdb_node 造成的"假数据不足"）
    实际生效口径写入 scopeFallback，便于报告标注数据来源。

    cache：同一次批量分析的共享查询缓存，规则间相同 PromQL 只请求一次。
    """
    promqls = _batch_promqls(
        rule, node, env, dep_only, exclude_namespace, deployments
    )
    main = await _sample_promql(
        promqls["main"], end_ts, points, step_seconds, cache
    )
    scope_fallback = None
    # hpa_replicas 的 PromQL 不带 deployment 选择器，去掉节点标签会跨节点误取，故不兜底
    if deployments and rule != "hpa_replicas" and not _has_data(main["samples"]):
        alt_promqls = _batch_promqls(
            rule, "", "", dep_only, exclude_namespace, deployments
        )
        alt_main = await _sample_promql(
            alt_promqls["main"], end_ts, points, step_seconds, cache
        )
        if _has_data(alt_main["samples"]):
            scope_fallback = {
                "applied": True,
                "mode": "deployment_names",
                "reason": "cmdb_node/env 标签口径无数据，改用应用名选择器重查成功",
                "appCount": len(deployments),
            }
            promqls, main = alt_promqls, alt_main
        else:
            scope_fallback = {
                "applied": False,
                "mode": "deployment_names",
                "reason": "cmdb_node/env 口径无数据，应用名口径同样无数据",
                "appCount": len(deployments),
            }
            if alt_main.get("errorKinds"):
                scope_fallback["errorKinds"] = alt_main["errorKinds"]
    aux: dict = {}
    if "spec" in promqls:
        spec = await _sample_promql(
            promqls["spec"], end_ts, points, step_seconds, cache
        )
        aux["spec_series"] = spec["samples"]
    aux_points = 1 if config.ARCH_RULE_OOM_AUX_INSTANT else points
    if "restart" in promqls:
        restart = await _sample_promql(
            promqls["restart"], end_ts, aux_points, step_seconds, cache
        )
        aux["restart_series"] = restart["samples"]
    if "oom_events" in promqls:
        events = await _sample_promql(
            promqls["oom_events"], end_ts, aux_points, step_seconds, cache
        )
        aux["oom_events_series"] = events["samples"]
    if "limit_probe" in promqls and not _has_data(main["samples"]):
        probe = await _sample_promql(
            promqls["limit_probe"], end_ts, 1, step_seconds, cache
        )
        aux["limit_exists"] = _has_data(probe["samples"])
        promqls["limit_probe_used"] = True

    # 身份审计：判定前剔除越界序列，阻断无法唯一归属的同名序列
    scope_apps = set(deployments) if deployments else None
    pod_level = rule in _POD_SCOPED_RULES
    main["samples"], audit = _batch_identity_audit(
        main["samples"], scope_apps, pod_level
    )
    aux_audits = []
    for key in ("spec_series", "restart_series", "oom_events_series"):
        if key in aux:
            aux[key], aux_audit = _batch_identity_audit(
                aux[key], scope_apps, key != "spec_series" and pod_level
            )
            aux_audits.append(aux_audit)
    identity_audit = {
        "outOfScopeSeries": audit["outOfScopeSeries"]
        + sum(a["outOfScopeSeries"] for a in aux_audits),
        "ambiguousIdentities": list(
            dict.fromkeys(
                audit["ambiguousIdentities"]
                + [i for a in aux_audits for i in a["ambiguousIdentities"]]
            )
        ),
    }
    result = arch_rules.evaluate(
        rule,
        main["samples"],
        points,
        step_seconds,
        {
            "node": node,
            "env": env,
            "deployment": None,
            "namespace": None,
            "window": f"{config.ARCH_RULE_DURATION_MINUTES}m",
            "mode": "batch",
        },
        aux,
    )
    result["collectionStatus"] = main["status"]
    result["promqls"] = promqls
    result["batchMode"] = True
    result["identityAudit"] = identity_audit
    # 主查询命中的时间序列数：报告里的"Pod/序列量"直接引用该值，避免再发一次计数查询
    samples = main.get("samples") or {}
    if isinstance(samples, dict):
        series_count = len(samples.get("series") or [])
    else:
        series_count = len(
            {
                tuple(sorted((r.get("metric") or {}).items()))
                for p in samples
                for r in (p.get("result") or [])
            }
        )
    result["seriesCount"] = series_count
    result["failPointCount"] = main["failPointCount"]
    # 错误详情必须上抛：否则智能体只看到"数据不足"，只能去翻落盘文件猜原因
    if main.get("errorKinds"):
        result["errorKinds"] = main["errorKinds"]
    if main.get("errorList"):
        result["errorList"] = main["errorList"][:3]
    if main.get("degradedReason"):
        result["degradedReason"] = main["degradedReason"]
    if scope_fallback:
        result["scopeFallback"] = scope_fallback
    return result


async def analyze_node_runtime(
    node: str,
    module: Optional[str] = None,
    deployments: Optional[list[str]] = None,
    rules: Optional[list[str]] = None,
    env: Optional[str] = None,
    end_ts: Optional[float] = None,
    sample_points: Optional[int] = None,
    step_seconds: int = 60,
    dep_only: bool = True,
    top_n: Optional[int] = None,
    task_id: Optional[str] = None,
    exclude_namespace=None,
) -> dict:
    """节点级运行架构批量分析（方案 1.2 全流程）

    一次调用完成"确定范围 → 8 条规则批量判定 → 关联模块 → 汇总风险"，
    避免按应用逐个调用 arch_rule_check 造成的 N×M 次查询与上下文膨胀。

    范围优先级：deployments > module > 节点全部 -dep 应用。
    每条规则只发起 sample_points 次节点级查询，规则间受 ARCH_BATCH_CONCURRENCY 限制。
    """
    if not node:
        raise ValueError("node 不能为空，如 '云公司->贵州'")
    _scope_guard(task_id, node)
    env = env or config.ARCH_ENV
    definitions = arch_rules.rule_definitions()
    rule_list = list(dict.fromkeys(rules or definitions.keys()))
    unknown = [r for r in rule_list if r not in definitions]
    if unknown:
        raise ValueError(f"未知规则: {', '.join(unknown)}")
    points = config.ARCH_RULE_DURATION_MINUTES if sample_points is None else sample_points
    if points <= 0:
        raise ValueError("sample_points 必须大于 0")
    if step_seconds <= 0:
        raise ValueError("step_seconds 必须大于 0")
    top_n = config.ARCH_BATCH_TOP_N if top_n is None else max(1, top_n)
    task_meta = _task_meta(task_id)

    scope = await resolve_analysis_scope(node=node, task_id=task_meta["taskId"])
    scope_ok = scope.get("status") == "SUCCESS"
    modules = scope.get("modules") or []
    app_modules: dict[str, list[str]] = scope.get("appModules") or {}
    scope_mode = "node"
    scope_label = "节点全部 -dep 应用"
    scope_apps: Optional[list[str]] = None
    selected_module_labels: Optional[set] = None

    if deployments:
        scope_apps = list(dict.fromkeys(str(d) for d in deployments if str(d).strip()))
        scope_mode = "explicit"
        scope_label = "调用方显式指定的应用"
    elif module:
        matched = [
            m
            for m in modules
            if module
            in {m.get("module"), _module_label(m), m.get("moduleId"), m.get("moduleName")}
        ]
        if not matched:
            return {
                "status": "NO_DATA",
                "node": node,
                "errMsg": f"模块 {module} 不存在于节点 {node} 的模块拓扑中",
                "ruleVersion": config.ARCH_RULE_VERSION,
                **task_meta,
            }
        scope_apps = list(
            dict.fromkeys(p for m in matched for p in m.get("apps") or [])
        )
        scope_mode = "module"
        scope_label = module
        selected_module_labels = {
            m.get("module") or _module_label(m) for m in matched
        }
    elif scope_ok and app_modules:
        scope_apps = list(app_modules.keys())

    if dep_only and scope_apps:
        suffix = config.ARCH_DEP_SUFFIX.lower()
        scope_apps = [a for a in scope_apps if a.lower().endswith(suffix)]
    scope_set = set(scope_apps) if scope_apps is not None else None

    limitations: list[str] = []
    if not scope_ok:
        limitations.append(
            "模块拓扑不可用，应用归属无法关联；风险结果仅按 Prometheus 中的 -dep 序列统计"
        )
    if not config.ARCH_LATENCY_PROMQL:
        limitations.append(
            "未配置 ARCH_LATENCY_PROMQL，CPU 限流结果缺少调用延迟交叉验证，不能据此确认业务影响"
        )
    if not scope_apps:
        limitations.append(
            "未获得应用清单（模块拓扑/项目部署接口不可用），无法执行应用名口径兜底；"
            "部署级规则在缺 cmdb_node 标签的环境下会记为数据不足，请显式传 deployments"
        )

    semaphore = asyncio.Semaphore(max(1, config.ARCH_BATCH_CONCURRENCY))
    # 规则间共享查询缓存：同一 PromQL（如 spec_replicas）只请求一次
    query_cache: dict = {}

    async def run_one(rule_name: str):
        async with semaphore:
            try:
                return rule_name, await _run_batch_rule(
                    rule_name, node, env, points, step_seconds, end_ts, dep_only,
                    exclude_namespace, scope_apps, query_cache
                ), None
            except Exception as e:  # 单规则失败不阻断其它规则
                logger.warning("batch rule %s failed: %s", rule_name, e)
                return rule_name, None, str(e)

    gathered = await asyncio.gather(*(run_one(r) for r in rule_list))

    rule_reports: list[dict] = []
    findings: list[dict] = []
    observed_apps: set[str] = set()
    observed_namespaces: set[str] = set()
    offline_apps: list[str] = []
    min_valid_point_ratio: Optional[float] = None
    totals = Counter()
    category_totals: dict[str, Counter] = {}
    fallback_rules: list[str] = []
    fallback_miss_rules: list[str] = []

    for rule_name, result, error in gathered:
        definition = definitions[rule_name]
        if error or result is None:
            rule_reports.append(
                {
                    "rule": rule_name,
                    "desc": definition["desc"],
                    "category": definition["category"],
                    "status": "数据不足",
                    "statusCode": "INSUFFICIENT",
                    "summary": "该规则取数失败，未产出判定结果",
                    "threshold": definition["threshold"],
                    "unit": definition["unit"],
                    "duration": definition["duration"],
                    "targetCount": 0,
                    "riskCount": 0,
                    "insufficientCount": 0,
                    "naCount": 0,
                    "okCount": 0,
                    "outOfScopeCount": 0,
                    "items": [],
                    "error": error,
                }
            )
            limitations.append(f"{rule_name} 取数失败：{error}")
            continue

        kept: list[dict] = []
        dropped = 0
        fallback = result.get("scopeFallback") or {}
        if fallback.get("applied"):
            fallback_rules.append(rule_name)
        elif fallback:
            fallback_miss_rules.append(rule_name)
        for item in result.get("items") or []:
            target = item.get("target") or {}
            app = target.get("deployment")
            if app:
                observed_apps.add(app)
            if scope_set is not None and app and app not in scope_set:
                dropped += 1
                continue
            kept.append(item)
            ns = target.get("namespace")
            if ns:
                observed_namespaces.add(ns)
            if item.get("offline") and app:
                offline_apps.append(app)
            total_points = item.get("totalPoints") or 0
            valid_points = item.get("validPoints")
            if total_points and isinstance(valid_points, int):
                ratio = valid_points / total_points
                if min_valid_point_ratio is None or ratio < min_valid_point_ratio:
                    min_valid_point_ratio = ratio

        # 批量只做筛查：副本类规则的 RISK 项用单应用接口（deployment+namespace
        # 精确过滤）回验一次。两个同名部署若除 deployment 外标签完全相同，批量
        # 选择器可能同时命中；精确过滤能给出唯一答案，回验不通过的直接降级。
        if (
            config.ARCH_BATCH_RECHECK_RISKS
            and rule_name in ("deployment_replicas", "deployment_available")
        ):
            suspects = [
                i
                for i in kept
                if i.get("statusCode") == "RISK"
                and (i.get("target") or {}).get("deployment")
                and (i.get("target") or {}).get("namespace")
            ][: max(0, config.ARCH_BATCH_RECHECK_MAX)]
            if suspects:
                recheck_sem = asyncio.Semaphore(
                    max(1, config.ARCH_BATCH_RECHECK_CONCURRENCY)
                )

                async def _recheck(item):
                    target = item.get("target") or {}
                    async with recheck_sem:
                        try:
                            single = await _arch_rule_check_impl(
                                rule_name,
                                node,
                                env,
                                target.get("deployment"),
                                end_ts,
                                points,
                                step_seconds,
                                task_id,
                                namespace=target.get("namespace"),
                                dep_only=dep_only,
                                exclude_namespace=exclude_namespace,
                            )
                        except Exception as exc:
                            return item, None, str(exc)
                    return item, single, None

                rechecked = await asyncio.gather(
                    *(_recheck(i) for i in suspects)
                )
                corrected = 0
                for item, single, err in rechecked:
                    if err or not single:
                        item["exactRecheck"] = {"applied": False, "error": err}
                        continue
                    single_items = single.get("items") or []
                    app = (item.get("target") or {}).get("deployment")
                    single_item = next(
                        (
                            x
                            for x in single_items
                            if (x.get("target") or {}).get("deployment") == app
                        ),
                        single_items[0] if single_items else {},
                    )
                    item["exactRecheck"] = {
                        "applied": True,
                        "statusCode": single.get("statusCode"),
                        "observedValue": single_item.get("observedValue"),
                        "summary": single.get("summary"),
                    }
                    if single.get("statusCode") != "RISK":
                        item["batchStatus"] = "RISK"
                        item["statusCode"] = single.get("statusCode")
                        item["status"] = single.get("status")
                        item["summary"] = single.get("summary")
                        item["observedValue"] = single_item.get("observedValue")
                        corrected += 1
                if corrected:
                    limitations.append(
                        f"{rule_name}：{corrected} 个批量风险项经单应用精确复核后降级"
                    )

        counts = Counter(item.get("statusCode") for item in kept)
        offline_count = sum(1 for item in kept if item.get("offline"))
        totals.update(counts)
        category = definition["category"]
        category_totals.setdefault(category, Counter()).update(counts)
        ordered = sorted(
            kept, key=lambda i: _STATUS_ORDER.get(i.get("statusCode"), 9)
        )
        # 只回传需要解释的明细：OK / NOT_APPLICABLE 的数量已在 okCount / naCount，
        # 明细回传会让 200+ 应用 × 8 条规则的返回膨胀到 20 万 token 量级
        actionable = [
            i
            for i in ordered
            if i.get("statusCode") in ("RISK", "INSUFFICIENT")
        ]
        rule_reports.append(
            {
                "rule": rule_name,
                "desc": definition["desc"],
                "category": category,
                "status": result.get("status"),
                "statusCode": result.get("statusCode"),
                "summary": result.get("summary"),
                "threshold": definition["threshold"],
                "unit": definition["unit"],
                "duration": definition["duration"],
                "targetCount": len(kept),
                "riskCount": counts.get("RISK", 0),
                "insufficientCount": counts.get("INSUFFICIENT", 0),
                "naCount": counts.get("NOT_APPLICABLE", 0),
                "okCount": counts.get("OK", 0),
                "offlineCount": offline_count,
                "outOfScopeCount": dropped,
                "items": actionable[:top_n],
                "itemsTruncated": len(actionable) > top_n,
                "dataQuality": result.get("dataQuality"),
                "collectionStatus": result.get("collectionStatus"),
                "seriesCount": result.get("seriesCount"),
                "identityAudit": result.get("identityAudit"),
                "errorKinds": result.get("errorKinds"),
                "errorSample": ((result.get("errorList") or [{}])[0]).get("error"),
                "degradedReason": result.get("degradedReason"),
            }
        )
        if result.get("errorKinds"):
            limitations.append(
                f"{rule_name}：取数异常 {result['errorKinds']}"
                + (
                    f"（{result['degradedReason']}）"
                    if result.get("degradedReason")
                    else ""
                )
            )
        audit = result.get("identityAudit") or {}
        if audit.get("ambiguousIdentities"):
            limitations.append(
                f"{rule_name}：{len(audit['ambiguousIdentities'])} 个身份存在多条序列"
                f"（{', '.join(audit['ambiguousIdentities'][:3])}），"
                "已剔除并按数据不足处理，需单应用精确复核"
            )
        if counts.get("INSUFFICIENT", 0):
            limitations.append(
                f"{rule_name}：{counts['INSUFFICIENT']} 个目标数据不足，未纳入风险结论"
            )
        if offline_count:
            limitations.append(
                f"{rule_name}：{offline_count} 个应用期望副本为 0"
                "（已下线/暂停），未纳入冗余判定"
            )
        if rule_name == "pod_cpu_imbalance" and counts:
            limitations.append(
                "pod_cpu_imbalance 批量模式按 deployment 聚合计算最大/平均，"
                "未做同规格分组；风险项需用单应用 arch_rule_check 复核同规格口径"
            )
        if rule_name == "hpa_replicas" and counts.get("NOT_APPLICABLE", 0):
            limitations.append(
                "HPA 指标无数据，按不适用处理；如需判定扩容能力，须先接入 kube-state-metrics HPA 指标"
            )
        for item in kept:
            if item.get("statusCode") != "RISK":
                continue
            target = item.get("target") or {}
            app = target.get("deployment")
            finding = {
                "riskType": definition["desc"],
                "rule": rule_name,
                "category": category,
                "status": item.get("status"),
                "statusCode": item.get("statusCode"),
                "node": node,
                "app": app,
                "namespace": target.get("namespace"),
                "modules": app_modules.get(app, []) if app else [],
                "threshold": item.get("threshold"),
                "observedValue": item.get("observedValue"),
                "maxValue": item.get("maxValue"),
                "minValue": item.get("minValue"),
                "validPoints": item.get("validPoints"),
                "totalPoints": item.get("totalPoints"),
                "reason": item.get("reason"),
                "ruleVersion": config.ARCH_RULE_VERSION,
                "taskId": task_meta["taskId"],
                "collectTime": (result.get("dataQuality") or {}).get("collectTime"),
            }
            # target 与 rules[].items 重复，只保留 Pod 级规则需要的下钻标签
            extra = {
                k: target[k]
                for k in ("pod", "container", "hpa", "cpuSpec")
                if target.get(k)
            }
            if extra:
                finding["targetDetail"] = extra
            findings.append(finding)

    if fallback_rules:
        limitations.append(
            "以下规则按应用名口径取数（cmdb_node 标签缺失）："
            + "、".join(fallback_rules)
        )
    if fallback_miss_rules:
        limitations.append(
            "以下规则两种口径均无数据，需先确认指标采集链路："
            + "、".join(fallback_miss_rules)
        )

    category_order = {"可用性": 0, "稳定性": 1, "容量": 2, "负载": 3}
    findings.sort(
        key=lambda f: (
            category_order.get(f.get("category"), 9),
            f.get("rule") or "",
            f.get("app") or "",
        )
    )
    findings_total = len(findings)
    # 先按全量风险统计模块风险数，再截断返回体积，避免 moduleSummary 少报
    risk_by_app = Counter(f.get("app") for f in findings if f.get("app"))
    findings_cap = max(1, config.ARCH_FINDINGS_MAX)
    findings = findings[:findings_cap]

    # 取数自检汇总：报告里需要引用的"离线应用清单 / 采样覆盖率 / 序列数"直接取这里，
    # 不再让模型重新发查询验证（实测复核环节占单次巡检约 20% 耗时）
    unique_offline_apps = list(dict.fromkeys(offline_apps))
    verification = {
        "offlineAppCount": len(unique_offline_apps),
        "offlineApps": unique_offline_apps[:20],
        "offlineAppsTruncated": len(unique_offline_apps) > 20,
        "namespaceCount": len(observed_namespaces),
        "minValidPointRatio": (
            round(min_valid_point_ratio, 4)
            if min_valid_point_ratio is not None
            else None
        ),
        "riskAppCount": len(risk_by_app),
        "outOfScopeSeries": sum(
            (r.get("identityAudit") or {}).get("outOfScopeSeries", 0)
            for r in rule_reports
        ),
        "ambiguousIdentityCount": sum(
            len((r.get("identityAudit") or {}).get("ambiguousIdentities") or [])
            for r in rule_reports
        ),
        "exactRecheckCount": sum(
            1
            for r in rule_reports
            for i in (r.get("items") or [])
            if (i.get("exactRecheck") or {}).get("applied")
        ),
        "note": "本区块为工具侧取数自检结果，可直接用于报告的数据覆盖说明；"
        "对应缺口无需再次查询",
    }

    module_summary: list[dict] = []
    for m in modules:
        label = m.get("module") or _module_label(m)
        if selected_module_labels is not None and label not in selected_module_labels:
            continue
        apps = set(m.get("apps") or [])
        module_summary.append(
            {
                "module": label,
                "moduleId": m.get("moduleId"),
                "appCount": len(apps),
                "riskCount": sum(risk_by_app.get(a, 0) for a in apps),
                "riskPreview": [a for a in sorted(apps) if risk_by_app.get(a)][:10],
                "appsWithMetricData": len(apps & observed_apps),
            }
        )
    module_summary.sort(key=lambda m: (-m["riskCount"], m["module"]))

    if totals.get("RISK"):
        overall = "RISK"
    elif not totals:
        overall = "NO_DATA"
    elif totals.get("INSUFFICIENT"):
        overall = "DATA_INSUFFICIENT"
    elif all(k == "NOT_APPLICABLE" for k in totals):
        overall = "NOT_APPLICABLE"
    else:
        overall = "SUCCESS"

    coverage = None
    if scope_set:
        coverage = round(len(scope_set & observed_apps) / len(scope_set), 4)

    return {
        "status": overall,
        "node": node,
        "env": env,
        "ruleVersion": config.ARCH_RULE_VERSION,
        "scope": {
            "mode": scope_mode,
            "label": scope_label,
            "appCount": len(scope_apps) if scope_apps is not None else None,
            "moduleCount": len(modules),
            "deployNode": scope.get("deployNode"),
            "topologyStatus": scope.get("status"),
            "duplicateApps": scope.get("duplicateApps") or [],
        },
        "window": {
            "durationMinutes": config.ARCH_RULE_DURATION_MINUTES,
            "samplePoints": points,
            "stepSeconds": step_seconds,
            "rateWindow": config.ARCH_RATE_WINDOW,
        },
        "depOnly": dep_only,
        "scopeFallback": {
            "mode": "deployment_names" if fallback_rules else "cmdb_node",
            "rulesUsingDeploymentNames": fallback_rules,
            "rulesWithoutData": fallback_miss_rules,
            "appCount": len(scope_apps) if scope_apps is not None else 0,
            "note": "cmdb_node/env 口径无数据时自动改用应用名选择器重查；"
            "该口径不依赖节点标签，但要求应用清单（EASYVIEWS 拓扑或显式传入）准确",
        },
        "ruleCount": len(rule_list),
        "rules": rule_reports,
        "summary": {
            "riskRuleCount": sum(1 for r in rule_reports if r.get("riskCount")),
            "riskItemCount": findings_total,
            "targetCount": sum(r.get("targetCount", 0) for r in rule_reports),
            "okItemCount": totals.get("OK", 0),
            "insufficientItemCount": totals.get("INSUFFICIENT", 0),
            "naItemCount": totals.get("NOT_APPLICABLE", 0),
            "appsInScope": len(scope_apps) if scope_apps is not None else None,
            "appsWithMetricData": len(observed_apps),
            "appCoverage": coverage,
            "byCategory": {
                category: {
                    "risk": counts.get("RISK", 0),
                    "insufficient": counts.get("INSUFFICIENT", 0),
                    "ok": counts.get("OK", 0),
                    "na": counts.get("NOT_APPLICABLE", 0),
                }
                for category, counts in category_totals.items()
            },
        },
        "findings": findings,
        "findingsTruncated": findings_total > len(findings),
        "verification": verification,
        "moduleSummary": module_summary,
        "dataLimitations": list(dict.fromkeys(limitations)),
        "notes": "副本类 RISK 项已由工具用单应用精确接口回验（见 items[].exactRecheck），"
        "不通过的已自动降级，模型无需再复核；仅当需要解释单个应用细节时，"
        "再用 arch_rule_check_tool（传 namespace）查 1 次，不要对全量应用重复取数",
        **task_meta,
    }


_PROBE_METRIC_FAMILIES: dict[str, tuple[str, ...]] = {
    "deployment": (
        "kube_deployment_spec_replicas",
        "kube_deployment_status_replicas_available",
        "kube_horizontalpodautoscaler_status_current_replicas",
    ),
    "container": (
        "container_memory_working_set_bytes",
        "container_cpu_cfs_throttled_periods_total",
        "kube_pod_container_status_last_terminated_reason",
        "kube_pod_container_status_restarts_total",
    ),
}
_PROBE_SCOPE_LABELS = ("cmdb_node", "cluster", "env", "namespace")


async def _probe_metric(
    client,
    metric: str,
    family: str,
    node: str,
    env: str,
    deployment: Optional[str],
) -> dict:
    """探测单个指标族的标签口径（只查标签名与 count，不返回序列内容）

    统计口径只算业务 `-dep` 应用：系统组件（calico/harbor/monitoring 等）
    通常带 cmdb_node，若一并计入会掩盖"业务应用缺标签"的问题。
    """
    item: dict = {"metric": metric, "family": family}
    labels: list[str] = []
    labels_source = "labels_api"
    labels_error = ""
    try:
        labels = await client.label_names(metric)
    except Exception as e:
        # VictoriaMetrics 对带 match[] 的 /api/v1/labels 返回 422，
        # 降级为取一条样本序列读标签名，体量同样恒定
        labels_error = str(e)[:200]
        labels_source = "series_sample"
        try:
            sample = await client.series_sample(metric, limit=1)
            labels = sorted(str(k) for k in sample.keys())
        except Exception as e2:
            return {
                **item,
                "status": "FAIL",
                "labelsSource": "unavailable",
                "error": f"labels: {labels_error} | series: {str(e2)[:200]}",
            }
    if not labels:
        # 两个接口都正常但无标签：指标未采集（可观测性缺口），不是探测失败
        return {
            **item,
            "status": "NO_DATA",
            "labelsSource": labels_source,
            "note": "指标无任何序列，请确认 exporters 是否采集该指标",
        }
    item["status"] = "SUCCESS"
    item["labelsSource"] = labels_source
    if labels_error:
        item["labelsApiError"] = labels_error
    item["labels"] = [l for l in _PROBE_SCOPE_LABELS if l in labels]
    if family == "deployment":
        scope_expr = 'deployment=~".+-dep"'
        count_expr = "count(%s)"
    else:
        scope_expr = 'pod=~".+-dep-.+"'
        count_expr = "count(count by (pod)(%s))"
    hits: dict = {}
    node_label, node_value = _node_label(node) or ("cmdb_node", node)
    if node_label == "cluster":
        # 已配置 cluster 口径的节点：cluster 用映射值（有效口径），
        # cmdb_node 仍按节点名探测一次，仅用于确认该标签确实缺失
        node_targets = [("cluster", node_value), ("cmdb_node", node)]
    else:
        node_targets = [("cmdb_node", node), ("cluster", node)]
    for label, value in node_targets:
        if label not in labels:
            hits[label] = None
            continue
        query = count_expr % f'{metric}{{{label}="{_promql_string(value)}", {scope_expr}}}'
        try:
            body = await client.query(query)
        except Exception:
            hits[label] = None
            continue
        result = (body.get("data") or {}).get("result") or []
        hits[label] = int(float(result[0]["value"][1])) if result else 0
    # env 必须与节点标签组合统计：单查 env 得到的是全集群命中，
    # 无法发现"本节点数据不带 env"或 env 取值变更
    if node_label in labels and "env" in labels:
        query = count_expr % (
            f'{metric}{{{node_label}="{_promql_string(node_value)}", '
            f'env="{_promql_string(env)}", {scope_expr}}}'
        )
        try:
            body = await client.query(query)
            result = (body.get("data") or {}).get("result") or []
            hits["env"] = int(float(result[0]["value"][1])) if result else 0
        except Exception:
            hits["env"] = None
    else:
        hits["env"] = None
    item["scopedSeries"] = hits
    if deployment and "deployment" in labels:
        try:
            body = await client.query(
                f'count({metric}{{deployment="{_promql_string(deployment)}"}})'
            )
            result = (body.get("data") or {}).get("result") or []
            item["appScopedSeries"] = int(float(result[0]["value"][1])) if result else 0
        except Exception:
            item["appScopedSeries"] = None
    return item


def _patrol_node_digest(node: str, res: dict, rules: Optional[list[str]]) -> dict:
    """把单节点批量结果压成 ≤1.5KB 摘要（不回 items / moduleSummary）"""
    rule_rows = []
    for r in res.get("rules") or []:
        if rules and r.get("rule") not in rules:
            continue
        rule_rows.append(
            {
                "rule": r.get("rule"),
                "status": r.get("status"),
                "risk": r.get("riskCount", 0),
                "insufficient": r.get("insufficientCount", 0),
                "ok": r.get("okCount", 0),
                "na": r.get("naCount", 0),
                "offline": r.get("offlineCount", 0),
                "errorKinds": r.get("errorKinds"),
            }
        )
    risks = [
        {
            "rule": f.get("rule"),
            "app": f.get("app"),
            "namespace": f.get("namespace"),
            "observedValue": f.get("observedValue"),
            "threshold": f.get("threshold"),
            "reason": f.get("reason"),
        }
        for f in (res.get("findings") or [])[: config.ARCH_PATROL_TOP_N]
    ]
    return {
        "node": node,
        "status": res.get("status"),
        "scope": {
            "mode": (res.get("scope") or {}).get("mode"),
            "appCount": (res.get("scope") or {}).get("appCount"),
        },
        "summary": {
            k: (res.get("summary") or {}).get(k)
            for k in (
                "riskRuleCount",
                "riskItemCount",
                "targetCount",
                "insufficientItemCount",
                "appsWithMetricData",
                "appCoverage",
            )
        },
        "rules": rule_rows,
        "topRisks": risks,
        "scopeFallback": {
            "mode": (res.get("scopeFallback") or {}).get("mode"),
            "rulesUsingDeploymentNames": (
                res.get("scopeFallback") or {}
            ).get("rulesUsingDeploymentNames"),
            "rulesWithoutData": (res.get("scopeFallback") or {}).get("rulesWithoutData"),
        },
        "limitations": (res.get("dataLimitations") or [])[:3],
        "errorKinds": res.get("errorKinds"),
    }


async def arch_patrol_run(
    task_id: str,
    nodes: list[str],
    rules: Optional[list[str]] = None,
    concurrency: Optional[int] = None,
    cursor: Optional[str] = None,
    detail: Optional[str] = None,
) -> dict:
    """一次调用完成多节点巡检（23 节点 1 轮对话）

    - 节点级并发受 ARCH_NODE_CONCURRENCY 限制，单节点内规则并发沿用 ARCH_BATCH_CONCURRENCY
    - 每节点只回摘要（计数 + topN 风险），不回 items / moduleSummary
    - 自动把 nodes 注册进 task_scope，供子会话 Worker 以同一 task_id 调用其他工具
    - cursor 为已完成节点数，支持从上次中断处续跑
    """
    if not task_id:
        raise ValueError("task_id 不能为空（多节点巡检需固定任务标识）")
    nodes = [n.strip() for n in (nodes or []) if n and n.strip()]
    if not nodes:
        raise ValueError("nodes 不能为空，如 ['云公司->贵州', '云公司->广州4d']")
    if len(nodes) > config.ARCH_PATROL_MAX_NODES:
        raise ValueError(
            f"nodes 超过上限 {config.ARCH_PATROL_MAX_NODES}，请拆分为多次调用"
        )
    started = time.time()
    registry = task_scope.get_registry()
    if registry.resolve(task_id, config.ARCH_TASK_TTL_MINUTES) is None:
        registry.register(
            task_id,
            nodes,
            ttl_minutes=config.ARCH_TASK_TTL_MINUTES,
            owner="arch_patrol_run",
        )
    start = int(cursor) if cursor and str(cursor).isdigit() else 0
    target = nodes[start:]
    if not target:
        return {
            "status": "SUCCESS",
            "taskId": task_id,
            "nodeCount": len(nodes),
            "processedCount": len(nodes),
            "nodeSummaries": [],
            "nextCursor": None,
            "note": "cursor 已到末尾，无需续跑",
            **_task_meta(task_id),
        }
    limit = max(1, concurrency or config.ARCH_NODE_CONCURRENCY)
    sem = asyncio.Semaphore(limit)

    async def one(node: str) -> dict:
        async with sem:
            try:
                res = await asyncio.wait_for(
                    analyze_node_runtime(
                        node=node,
                        rules=rules,
                        task_id=task_id,
                        top_n=config.ARCH_PATROL_TOP_N,
                    ),
                    timeout=config.ARCH_PATROL_NODE_TIMEOUT,
                )
            except asyncio.TimeoutError:
                return {
                    "node": node,
                    "status": "FAIL",
                    "errorKinds": "TIMEOUT",
                    "errMsg": f"节点处理超时（>{config.ARCH_PATROL_NODE_TIMEOUT}s）",
                }
            except Exception as e:
                return {
                    "node": node,
                    "status": "FAIL",
                    "errorKinds": type(e).__name__,
                    "errMsg": str(e)[:200],
                }
            return _patrol_node_digest(node, res, rules)

    results = await asyncio.gather(*(one(n) for n in target), return_exceptions=True)
    summaries: list[dict] = []
    for node, item in zip(target, results):
        if isinstance(item, Exception):
            summaries.append(
                {
                    "node": node,
                    "status": "FAIL",
                    "errorKinds": type(item).__name__,
                    "errMsg": str(item)[:200],
                }
            )
        else:
            summaries.append(item)

    matrix: dict[str, dict] = {}
    all_risks: list[dict] = []
    for s in summaries:
        for r in s.get("rules") or []:
            row = matrix.setdefault(
                r["rule"],
                {"risk": 0, "insufficient": 0, "ok": 0, "na": 0, "offline": 0},
            )
            for k in ("risk", "insufficient", "ok", "na", "offline"):
                row[k] += r.get(k) or 0
        for f in s.get("topRisks") or []:
            all_risks.append({"node": s["node"], **f})

    failed = [s["node"] for s in summaries if s.get("status") == "FAIL"]
    if all(s.get("status") == "FAIL" for s in summaries):
        overall = "FAIL"
    elif failed:
        overall = "PARTIAL"
    else:
        overall = "SUCCESS"

    return {
        "status": overall,
        "taskId": task_id,
        "nodeCount": len(nodes),
        "processedCount": start + len(target),
        "failedNodes": failed,
        "nodeSummaries": summaries,
        "riskMatrix": matrix,
        "topRisks": all_risks[: config.ARCH_PATROL_RISK_MAX],
        "nextCursor": None,
        "concurrency": limit,
        "elapsedMs": int((time.time() - started) * 1000),
        "note": "摘要模式；单节点明细用 analyze_node_runtime_tool(detail) 或 arch_rule_check 下钻",
        **_task_meta(task_id),
    }


async def arch_label_probe(
    node: str,
    env: Optional[str] = None,
    deployment: Optional[str] = None,
    task_id: Optional[str] = None,
) -> dict:
    """探测指标族的标签口径，一次调用定位"数据管道断点"

    背景：部分生产集群的业务 Deployment 在 kube-state-metrics 中没有
    cmdb_node 标签，而容器级指标（cAdvisor）带有该标签，导致规则引擎
    按节点标签取数时出现"业务应用全量数据不足"的假象。

    本工具对 deployment 级与 container 级指标分别检查：
        - 指标存在哪些标签（只返回标签名，体量恒定）
        - 用 cmdb_node / cluster / env 过滤时是否有 series（count 结果）
        - 指定 deployment 时该应用是否有 series
    并给出 severity 一致的 recommendedMode，避免智能体反复试查、写脚本解析落盘 JSON。

    参数：
        node: 节点标识，如 '云公司->广州4d'；命中 ARCH_NODE_CLUSTER_MAP 的节点
              （如 云公司->广州4d → cluster="广州4d生产"）按 cluster 取数，不再附加 cmdb_node
        env: env 标签值，默认 ARCH_ENV（生产）
        deployment: 可选，额外验证单个应用是否有独立序列

    返回 metrics[]（每个指标族的标签与命中数）、clusterValues（低基数取值）、
    verdict（一致性结论与建议口径）。一次调用即可决定后续取数方式。

    标签来源兼容：优先 /api/v1/labels；VictoriaMetrics 对带 match[] 的该接口
    返回 422 时，自动降级为 /api/v1/series?match[]=<metric>&limit=1 取样本标签，
    并在每个 metric 的 labelsSource 标注来源（labels_api / series_sample）；
    指标无任何序列时该 metric 为 NO_DATA，两条路径都失败才是 FAIL。
    """
    if not node:
        raise ValueError("node 不能为空，如 '云公司->广州4d'")
    _scope_guard(task_id, node)
    env = env or config.ARCH_ENV
    node_label, node_value = _node_label(node) or ("cmdb_node", node)
    client = get_prometheus_client()
    matrix: list[dict] = []
    for family, metrics in _PROBE_METRIC_FAMILIES.items():
        for metric in metrics:
            item = await _probe_metric(client, metric, family, node, env, deployment)
            matrix.append(item)

    try:
        cluster_values = (await client.label_values("cluster"))[:50]
    except Exception:
        cluster_values = []

    def _stats(family: str) -> tuple[int, int, int, int, int]:
        items = [i for i in matrix if i.get("family") == family]

        def hit(label: str) -> int:
            return sum(
                1 for i in items if (i.get("scopedSeries") or {}).get(label)
            )

        return (
            len(items),
            hit(node_label),
            hit("cmdb_node"),
            hit("cluster"),
            hit("env"),
        )

    dep_total, dep_hit, dep_cmdb, dep_cluster, dep_env = _stats("deployment")
    ctr_total, ctr_hit, ctr_cmdb, ctr_cluster, ctr_env = _stats("container")
    # env 过滤全 0 但节点标签有数据 → 标签取值已变更，按节点口径取数必然全空
    env_dead = (
        dep_total + ctr_total > 0
        and dep_env == 0
        and ctr_env == 0
        and (dep_hit or ctr_hit)
    )
    consistent = (
        dep_hit == dep_total and ctr_hit == ctr_total and not env_dead
    )
    if env_dead:
        suggested = "env_mismatch"
        hint = (
            "cmdb_node/cluster 能查到数据，但 env 过滤命中为 0：env 标签取值可能已变更。"
            "先用 count by (env) (kube_deployment_spec_replicas{deployment=~\".+-dep\"}) "
            "确认实际取值，再显式传 env 参数；未确认前不要继续跑批量规则"
        )
    elif consistent:
        suggested = node_label
        if node_label == "cluster":
            hint = (
                f'该节点已配置按 cluster="{node_value}" 取数（ARCH_NODE_CLUSTER_MAP），'
                "规则查询会直接带该标签，无需再手工指定 cluster"
            )
        else:
            hint = "各指标族均带 cmdb_node 标签，直接按 node 查询即可"
    elif dep_hit == 0 and ctr_hit == 0:
        suggested = "deployment_names"
        hint = (
            f"节点标签（{node_label}={node_value}）在两类指标上均未命中："
            "请显式传 deployments 应用清单，"
            "analyze_node_runtime_tool 会自动改用应用名口径兜底；"
            "若应用名口径同样为空，需检查指标采集链路"
        )
    elif dep_hit == 0 and ctr_hit > 0:
        suggested = "deployment_names"
        hint = (
            f"deployment 级指标缺 {node_label} 标签、容器级指标有该标签："
            "节点级批量分析请显式传 deployments 应用清单"
            "（analyze_node_runtime_tool 会自动改用应用名口径兜底），"
            "容器级规则保持按节点标签查询"
        )
    elif dep_hit > 0 and ctr_hit == 0:
        suggested = "cluster"
        hint = (
            f"容器级指标缺 {node_label} 标签：请用 clusterValues 中与节点对应的集群名，"
            "通过 prometheus_query_tool 手工指定 cluster 选择器"
        )
    else:
        suggested = "cluster"
        hint = (
            f"节点标签 {node_label}={node_value} 只命中部分指标族："
            "先确认 cluster 标签取值与节点映射，或按应用名直查；"
            "两者都为空需检查指标采集链路"
        )

    probe_status = "SUCCESS"
    if not any(i.get("status") == "SUCCESS" for i in matrix):
        probe_status = (
            "NO_DATA" if any(i.get("status") == "NO_DATA" for i in matrix) else "FAIL"
        )

    return {
        "status": probe_status,
        "node": node,
        "env": env,
        "deployment": deployment,
        "metrics": matrix,
        "clusterValues": cluster_values,
        "verdict": {
            "nodeLabel": node_label,
            "nodeLabelValue": node_value,
            "deploymentLevelCmdbNode": f"{dep_cmdb}/{dep_total}",
            "deploymentLevelCluster": f"{dep_cluster}/{dep_total}",
            "deploymentLevelEnv": f"{dep_env}/{dep_total}",
            "containerLevelCmdbNode": f"{ctr_cmdb}/{ctr_total}",
            "containerLevelCluster": f"{ctr_cluster}/{ctr_total}",
            "containerLevelEnv": f"{ctr_env}/{ctr_total}",
            "envMatched": not env_dead,
            "consistent": consistent,
            "suggestedMode": suggested,
            "hint": hint,
        },
        **_task_meta(None),
    }


# ---------------- 七、Pinpoint 链路分析（方案 1.3） ----------------

_PINPOINT_COUNT_KEYS = ("totalCount", "count", "requestCount", "sampleCount")
_PINPOINT_ERROR_KEYS = ("errorCount", "failCount")
_PINPOINT_SLOW_KEYS = ("slowCount",)


def _pinpoint_edge_stats(raw: dict) -> dict:
    """提取 Pinpoint 调用边统计"""
    total = _pick_num(raw, _PINPOINT_COUNT_KEYS)
    err = _pick_num(raw, _PINPOINT_ERROR_KEYS)
    slow = _pick_num(raw, _PINPOINT_SLOW_KEYS)
    response = raw.get("responseStatistics") or raw.get("responseSummary") or {}
    elapsed = None
    if isinstance(response, dict):
        elapsed = _pick_num(response, ("Avg", "avg", "avgResponseTime", "avgElapsed"))
    if elapsed is None:
        elapsed = _pick_num(raw, _ELAPSED_KEYS)
    return {
        "totalCount": total,
        "errorCount": err,
        "errorRate": round(err / total, 4) if total and err is not None else None,
        "slowCount": slow,
        "slowRate": round(slow / total, 4) if total and slow is not None else None,
        "elapsed": elapsed,
    }


def _parse_pinpoint_servermap(body) -> dict:
    """解析 Pinpoint serverMap 响应为统一图结构

    兼容 applicationMapData.nodeDataArray / linkDataArray 以及 nodes / links，
    link 的 from/to 可能引用节点 key，需先映射为应用名。
    """
    data = body
    if isinstance(body, dict):
        data = body.get("applicationMapData") or body.get("data") or body
    if not isinstance(data, dict):
        return {"nodes": {}, "edges": []}
    raw_nodes = data.get("nodeDataArray") or data.get("nodes") or []
    raw_links = data.get("linkDataArray") or data.get("links") or []
    nodes: dict = {}
    key_to_name: dict = {}
    for n in raw_nodes if isinstance(raw_nodes, list) else []:
        if not isinstance(n, dict):
            continue
        name = n.get("applicationName") or n.get("name") or n.get("key")
        if not name:
            continue
        key = n.get("key") or name
        key_to_name[str(key)] = str(name)
        nodes[str(name)] = n
    edges: list[dict] = []
    for link in raw_links if isinstance(raw_links, list) else []:
        if not isinstance(link, dict):
            continue
        source = link.get("from") or link.get("source") or link.get("caller")
        target = link.get("to") or link.get("target") or link.get("callee")
        source = key_to_name.get(str(source), source)
        target = key_to_name.get(str(target), target)
        if not source or not target:
            continue
        nodes.setdefault(str(source), {})
        nodes.setdefault(str(target), {})
        edges.append(
            {
                "source": str(source),
                "target": str(target),
                "stats": _pinpoint_edge_stats(link),
            }
        )
    return {"nodes": nodes, "edges": edges}


def _graph_risk_findings(
    node_list: list[str],
    edges: list[dict],
    top_n: int,
    app_modules: dict[str, list[str]],
) -> tuple[list[dict], dict, list[str]]:
    """基于调用图计算链路风险（Pinpoint 与 EasyViews 共用口径）"""
    adjacency: dict[str, list[str]] = {n: [] for n in node_list}
    reverse: dict[str, list[str]] = {n: [] for n in node_list}
    in_degree: Counter = Counter()
    out_degree: Counter = Counter()
    for edge in edges:
        src, dst = edge["source"], edge["target"]
        if src not in adjacency or dst not in adjacency:
            continue
        if dst not in adjacency[src]:
            adjacency[src].append(dst)
            reverse[dst].append(src)
            out_degree[src] += 1
            in_degree[dst] += 1

    findings: list[dict] = []
    limitations: list[str] = []

    def module_of(app: str) -> list[str]:
        return app_modules.get(app, [])

    for edge in edges:
        stats = edge.get("stats") or {}
        reasons = []
        if (
            stats.get("errorRate") is not None
            and stats["errorRate"] > config.ARCH_LINK_ERROR_RATE
        ):
            reasons.append(
                f"错误率 {stats['errorRate']:.2%} 超过 {config.ARCH_LINK_ERROR_RATE:.0%}"
            )
        if (
            stats.get("slowRate") is not None
            and stats["slowRate"] > config.ARCH_LINK_SLOW_RATE
        ):
            reasons.append(
                f"慢调用占比 {stats['slowRate']:.2%} 超过 {config.ARCH_LINK_SLOW_RATE:.0%}"
            )
        if (
            stats.get("elapsed") is not None
            and stats["elapsed"] > config.ARCH_LINK_SLOW_MS
        ):
            reasons.append(
                f"平均耗时 {stats['elapsed']:.0f}ms 超过 {config.ARCH_LINK_SLOW_MS:.0f}ms"
            )
        if reasons:
            findings.append(
                {
                    "riskType": "异常调用边",
                    "category": "链路",
                    "status": "已发现风险",
                    "statusCode": "RISK",
                    "app": edge["target"],
                    "source": edge["source"],
                    "modules": module_of(edge["target"]),
                    "observedValue": stats.get("errorRate"),
                    "threshold": config.ARCH_LINK_ERROR_RATE,
                    "reason": "；".join(reasons),
                    "suggestion": "核查下游服务容量、超时与重试策略，并结合错误日志定位",
                }
            )

    for app, count in in_degree.most_common(top_n):
        if count >= config.ARCH_LINK_HIGH_INDEGREE:
            findings.append(
                {
                    "riskType": "集中依赖",
                    "category": "链路",
                    "status": "已发现风险",
                    "statusCode": "RISK",
                    "app": app,
                    "modules": module_of(app),
                    "observedValue": count,
                    "threshold": config.ARCH_LINK_HIGH_INDEGREE,
                    "reason": f"被 {count} 个应用直接调用，单点故障影响面大",
                    "suggestion": "评估该应用冗余与限流降级能力，必要时拆分或增加多副本/多可用区部署",
                }
            )
    for app, count in out_degree.most_common(top_n):
        if count >= config.ARCH_LINK_HIGH_OUTDEGREE:
            findings.append(
                {
                    "riskType": "下游依赖过多",
                    "category": "链路",
                    "status": "已发现风险",
                    "statusCode": "RISK",
                    "app": app,
                    "modules": module_of(app),
                    "observedValue": count,
                    "threshold": config.ARCH_LINK_HIGH_OUTDEGREE,
                    "reason": f"直接依赖 {count} 个下游应用，任一异常都可能放大影响",
                    "suggestion": "梳理依赖必要性，增加熔断/降级与超时隔离",
                }
            )

    cycles = _find_scc(node_list, adjacency)
    for comp in cycles[:top_n]:
        findings.append(
            {
                "riskType": "循环依赖",
                "category": "链路",
                "status": "已发现风险",
                "statusCode": "RISK",
                "apps": comp,
                "observedValue": len(comp),
                "threshold": 2,
                "reason": f"{len(comp)} 个应用构成调用环，故障会相互放大",
                "suggestion": "拆分共享逻辑或引入异步解耦，消除调用环",
            }
        )

    longest = {"length": 0, "path": []}
    critical: list[dict] = []
    if len(node_list) > config.ARCH_LINK_MAX_NODES:
        limitations.append(
            f"应用数 {len(node_list)} 超过 {config.ARCH_LINK_MAX_NODES}，"
            "跳过最长调用链与关键依赖计算"
        )
    else:
        longest = _longest_path(adjacency, node_list)
        if longest["length"] >= config.ARCH_LINK_LONG_PATH:
            findings.append(
                {
                    "riskType": "长调用链",
                    "category": "链路",
                    "status": "已发现风险",
                    "statusCode": "RISK",
                    "apps": longest["path"],
                    "observedValue": longest["length"],
                    "threshold": config.ARCH_LINK_LONG_PATH,
                    "reason": f"最长调用链 {longest['length']} 层："
                    + " → ".join(longest["path"]),
                    "suggestion": "评估链路超时与故障传播，合并或异步化非核心环节",
                }
            )
        path_stats = _path_stats(adjacency, node_list)
        for app, count in sorted(
            path_stats["pathsThrough"].items(), key=lambda kv: -kv[1]
        )[:top_n]:
            if count >= config.ARCH_LINK_CRITICAL_PATHS:
                critical.append(
                    {
                        "app": app,
                        "pathsThrough": count,
                        "modules": module_of(app),
                    }
                )

    impact = _impact_scope(node_list, adjacency, reverse)[:top_n]
    for item in impact:
        if item["affectedCount"] >= config.ARCH_LINK_IMPACT_MIN_NODES:
            findings.append(
                {
                    "riskType": "上下游影响范围大",
                    "category": "链路",
                    "status": "已发现风险",
                    "statusCode": "RISK",
                    "app": item["node"],
                    "modules": module_of(item["node"]),
                    "observedValue": item["affectedCount"],
                    "threshold": config.ARCH_LINK_IMPACT_MIN_NODES,
                    "reason": f"{item['affectedCount']} 个上游应用会受其异常影响",
                    "suggestion": "优先保障该应用可用性，完善监控告警与降级预案",
                }
            )

    graph = {
        "nodeCount": len(node_list),
        "edgeCount": len(edges),
        "topInDegree": [
            {"app": a, "count": c} for a, c in in_degree.most_common(top_n)
        ],
        "topOutDegree": [
            {"app": a, "count": c} for a, c in out_degree.most_common(top_n)
        ],
        "cycleCount": len(cycles),
        "longestPath": longest,
        "criticalDependencies": critical,
        "impactScope": impact,
    }
    return findings, graph, limitations


async def analyze_pinpoint_dependencies(
    node: str,
    projects: Optional[list[str]] = None,
    product_name: Optional[str] = None,
    min_score: Optional[float] = None,
    caller_range: int = 2,
    callee_range: int = 2,
    top_n: Optional[int] = None,
    task_id: Optional[str] = None,
) -> dict:
    """基于 Pinpoint 构建服务依赖图并做链路风险判定（方案 1.3）

    流程：EasyViews↔Pinpoint 应用映射 → 批量 serverMap → 合并调用边 →
    入/出度、循环依赖、最长链路、影响范围、关键依赖、异常调用边。
    EasyViews 链路结论与本工具结论应交叉核对，差异写入 dataLimitations。
    """
    if not node:
        raise ValueError("node 不能为空，如 '云公司->贵州'")
    _scope_guard(task_id, node)
    top_n = config.ARCH_LINK_TOP_N if top_n is None else max(1, top_n)
    meta = _task_meta(task_id)
    mapping = await build_app_mapping(
        node=node,
        product_name=product_name,
        projects=projects,
        min_score=min_score,
    )
    if mapping.get("status") != "SUCCESS":
        return {
            "status": mapping.get("status", "DATA_INSUFFICIENT"),
            "node": node,
            "errMsg": mapping.get("errMsg")
            or "未能建立 EasyViews↔Pinpoint 应用映射，无法开展 Pinpoint 链路分析",
            "mappingStatus": mapping.get("status"),
            **meta,
        }

    app_projects: dict[str, list[str]] = {}
    app_modules: dict[str, list[str]] = {}
    for project, info in (mapping.get("mapping") or {}).items():
        for candidate in info.get("matched") or []:
            name = candidate.get("applicationName")
            if not name:
                continue
            app_projects.setdefault(name, [])
            if project not in app_projects[name]:
                app_projects[name].append(project)
            for module in info.get("modules") or []:
                app_modules.setdefault(name, [])
                if module not in app_modules[name]:
                    app_modules[name].append(module)
    applications = sorted(app_projects.keys())
    if not applications:
        return {
            "status": "DATA_INSUFFICIENT",
            "node": node,
            "errMsg": "映射结果中没有唯一匹配的 Pinpoint 应用，无法构建链路图",
            **meta,
        }

    batch = await pinpoint_tools.get_server_maps_batch(
        applications=applications,
        caller_range=caller_range,
        callee_range=callee_range,
    )
    nodes: dict = {}
    merged: dict = {}
    for item in batch.get("succeeded") or []:
        parsed = _parse_pinpoint_servermap(item.get("data"))
        for name in parsed["nodes"]:
            nodes.setdefault(name, {})
        for edge in parsed["edges"]:
            key = (edge["source"], edge["target"])
            current = merged.get(key)
            current_total = (current or {}).get("stats", {}).get("totalCount") or 0
            new_total = edge.get("stats", {}).get("totalCount") or 0
            if current is None or new_total > current_total:
                merged[key] = edge

    node_list = sorted(nodes.keys())
    edge_list = list(merged.values())
    findings, graph, limitations = _graph_risk_findings(
        node_list, edge_list, top_n, app_modules
    )
    failed = batch.get("failed") or []
    if failed:
        limitations.append(
            f"{len(failed)} 个应用 serverMap 查询失败，依赖图不完整，结论需复核"
        )
    if len(applications) > 1:
        limitations.append(
            "多个 focal 应用的 serverMap 已合并去重；同一调用边取调用量最大记录，"
            "避免重复计数"
        )

    return {
        "status": "RISK" if findings else ("SUCCESS" if edge_list else "DATA_INSUFFICIENT"),
        "dataSource": "pinpoint",
        "node": node,
        "ruleVersion": config.ARCH_RULE_VERSION,
        "mappingStatus": mapping.get("status"),
        "applicationCount": len(applications),
        "applications": applications[:top_n],
        "serverMapSuccessCount": batch.get("successCount", 0),
        "serverMapFailedCount": len(failed),
        "failedApplications": [
            {"application": f.get("application"), "error": f.get("error")}
            for f in failed[:20]
        ],
        "nodeCount": graph["nodeCount"],
        "edgeCount": graph["edgeCount"],
        "thresholds": {
            "highInDegree": config.ARCH_LINK_HIGH_INDEGREE,
            "highOutDegree": config.ARCH_LINK_HIGH_OUTDEGREE,
            "longestPath": config.ARCH_LINK_LONG_PATH,
            "errorRate": config.ARCH_LINK_ERROR_RATE,
            "slowRate": config.ARCH_LINK_SLOW_RATE,
            "slowMs": config.ARCH_LINK_SLOW_MS,
            "impactMinNodes": config.ARCH_LINK_IMPACT_MIN_NODES,
            "criticalPaths": config.ARCH_LINK_CRITICAL_PATHS,
        },
        "findings": [{**f, "taskId": meta["taskId"]} for f in findings[: top_n * 4]],
        "findingsTruncated": len(findings) > top_n * 4,
        "graph": graph,
        "dataLimitations": list(dict.fromkeys(limitations)),
        "notes": "该结论来自 Pinpoint 调用关系；与 EasyViews 链路拓扑结论不一致时，"
        "以两边共同确认的依赖为准并记录差异",
        **meta,
    }


# ---------------- 八、报告汇总（方案第五章） ----------------

_RULE_SUGGESTIONS = {
    "deployment_replicas": "补足副本至冗余要求，并确认反亲和与多可用区分布",
    "deployment_available": "核查发布、扩缩容与节点故障记录，确认可用副本恢复",
    "cpu_throttle_ratio": "调整 CPU limit/request 或扩副本，结合 P99 延迟验证业务影响",
    "memory_ratio": "核查内存 limit 与工作集趋势，必要时上调 limit 或优化内存占用",
    "hpa_replicas": "评估上调 HPA 最大副本或优化单副本处理能力",
    "pod_cpu_imbalance": "检查流量分发、Pod 就绪状态与规格一致性，必要时调整调度与负载均衡",
    "oom_killed": "核查内存 limit、堆配置与泄漏，结合重启记录定位",
    "restart_increase": "排查重启原因（OOM、探针失败、节点异常）",
}

_HOST_SUGGESTIONS = {
    "ALL_IN_SAME_HOST": "按项目冗余要求将实例分散到不同物理宿主机，并复核调度约束与反亲和配置",
    "PARTIAL_CONCENTRATION": "评估单宿主机故障影响，必要时调整调度策略分散实例",
    "SINGLE_INSTANCE": "确认业务是否允许单实例部署；若无冗余要求，记录为待确认",
    "MAPPING_CONFLICT": "核查云主机映射冲突与迁移记录，确认宿主机归属后再判定",
    "PARTIAL_MAPPING": "补齐无法映射的云主机宿主机关系后复核集中度",
    "UNMAPPED": "补齐宿主机映射数据后复核，当前不能判定故障域",
}


async def summarize_architecture_findings(
    center: Optional[dict] = None,
    province: Optional[dict] = None,
    task_id: Optional[str] = None,
) -> dict:
    """汇总中心节点与省节点分析结果，生成方案第五章结构（不做新的取数）

    center 传入 analyze_node_runtime 的返回，province 传入 analyze_host_concentration
    的返回；本工具只做确定性归一化、优先级建议与数据限制汇总。
    """
    meta = _task_meta(task_id)
    entries: list[dict] = []
    limitations: list[str] = []
    scope: dict = {}

    if isinstance(center, dict) and center:
        scope["center"] = center.get("scope")
        for finding in center.get("findings") or []:
            category = finding.get("category")
            entries.append(
                {
                    "object": {
                        "node": finding.get("node"),
                        "module": "、".join(finding.get("modules") or []) or None,
                        "app": finding.get("app"),
                        "namespace": finding.get("namespace"),
                    },
                    "riskType": finding.get("riskType"),
                    "category": category,
                    "status": finding.get("status"),
                    "statusCode": finding.get("statusCode"),
                    "basis": {
                        "rule": finding.get("rule"),
                        "threshold": finding.get("threshold"),
                        "observedValue": finding.get("observedValue"),
                        "validPoints": finding.get("validPoints"),
                        "totalPoints": finding.get("totalPoints"),
                    },
                    "evidence": {
                        "reason": finding.get("reason"),
                        "ruleVersion": finding.get("ruleVersion"),
                        "collectTime": finding.get("collectTime"),
                        "target": finding.get("target"),
                    },
                    "impactScope": "同节点内依赖该应用的模块与调用方（需结合链路分析确认）",
                    "suggestion": _RULE_SUGGESTIONS.get(
                        finding.get("rule"), "结合业务影响复核"
                    ),
                    "priorityHint": "HIGH"
                    if category in ("可用性", "稳定性")
                    else "MEDIUM",
                    "taskId": finding.get("taskId") or meta["taskId"],
                    "dataLimitations": [],
                }
            )
        for rule in center.get("rules") or []:
            if rule.get("insufficientCount"):
                limitations.append(
                    f"{rule.get('rule')}：{rule['insufficientCount']} 个目标数据不足，"
                    "未纳入风险结论"
                )
            if rule.get("outOfScopeCount"):
                limitations.append(
                    f"{rule.get('rule')}：{rule['outOfScopeCount']} 个目标不在本次分析范围，已排除"
                )
        limitations.extend(center.get("dataLimitations") or [])

    if isinstance(province, dict) and province:
        scope["province"] = {
            "node": province.get("node"),
            "nodeCode": province.get("nodeCode"),
            "projectCount": province.get("projectCount"),
        }
        for project in province.get("projects") or []:
            scenario = project.get("scenario")
            if scenario == "SPREAD":
                continue
            if scenario in ("ALL_IN_SAME_HOST", "PARTIAL_CONCENTRATION"):
                status, status_code = "已发现风险", "RISK"
            elif scenario == "SINGLE_INSTANCE":
                status, status_code = "待业务确认", "PENDING"
            else:
                status, status_code = "数据不足", "INSUFFICIENT"
            entries.append(
                {
                    "object": {
                        "node": province.get("node"),
                        "project": project.get("project"),
                    },
                    "riskType": "物理宿主机故障域",
                    "category": "宿主机集中度",
                    "status": status,
                    "statusCode": status_code,
                    "basis": {
                        "scenario": scenario,
                        "instanceCount": project.get("instanceCount"),
                        "hostCount": project.get("hostCount"),
                        "maxHostRatio": project.get("maxHostRatio"),
                        "coverage": project.get("coverage"),
                        "concentrationThreshold": province.get(
                            "concentrationRatioThreshold"
                        ),
                    },
                    "evidence": {
                        "scenarioDesc": project.get("scenarioDesc"),
                        "hostDistribution": project.get("hostDistribution"),
                        "unmappedIps": project.get("unmappedIps"),
                        "conflictIps": project.get("conflictIps"),
                        "collectTime": province.get("collectTime"),
                    },
                    "impactScope": "该项目全部云主机实例所在物理宿主机",
                    "suggestion": _HOST_SUGGESTIONS.get(scenario, "结合冗余要求复核"),
                    "priorityHint": "HIGH"
                    if scenario == "ALL_IN_SAME_HOST"
                    else ("MEDIUM" if status_code == "RISK" else "LOW"),
                    "taskId": meta["taskId"],
                    "dataLimitations": (
                        ["存在无法映射宿主机的云主机，集中度可能被低估"]
                        if project.get("unmappedIps")
                        else []
                    ),
                }
            )
        limitations.extend(
            [
                "存在云主机无法映射宿主机，集中度按已映射部分统计"
                for _ in [0]
                if province.get("unmappedProjectCount")
            ]
        )
        limitations.extend(
            [
                "存在云主机映射到多个宿主机，相关项目标记为数据不足"
                for _ in [0]
                if province.get("conflictProjectCount")
            ]
        )

    priority_order = {"HIGH": 0, "MEDIUM": 1, "LOW": 2}
    entries.sort(
        key=lambda e: (
            priority_order.get(e.get("priorityHint"), 9),
            e.get("category") or "",
            str((e.get("object") or {}).get("app") or (e.get("object") or {}).get("project") or ""),
        )
    )
    risk_entries = [e for e in entries if e.get("statusCode") == "RISK"]
    counts = Counter(e.get("statusCode") for e in entries)
    overview = {
        "riskCount": len(risk_entries),
        "insufficientCount": counts.get("INSUFFICIENT", 0),
        "pendingCount": counts.get("PENDING", 0),
        "totalCount": len(entries),
        "byCategory": dict(Counter(e.get("category") for e in risk_entries)),
        "byPriority": dict(Counter(e.get("priorityHint") for e in risk_entries)),
    }

    if not entries:
        status = "NO_DATA"
    elif counts.get("INSUFFICIENT"):
        status = "DATA_INSUFFICIENT"
    else:
        status = "SUCCESS"

    return {
        "status": status,
        "ruleVersion": center.get("ruleVersion")
        if isinstance(center, dict)
        else config.ARCH_RULE_VERSION,
        "scope": scope,
        "overview": overview,
        "entries": entries,
        "dataLimitations": list(dict.fromkeys(limitations)),
        "notes": "报告仅包含确定性判定结果与可追溯证据；priorityHint 为规则级建议，"
        "最终优先级需结合业务重要性、影响范围与数据限制确认",
        **meta,
    }
