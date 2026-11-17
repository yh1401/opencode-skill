"""Pinpoint 查询工具定义（基于真实 Controller 参数）"""
import asyncio
import logging
import time
from typing import Optional

import config
from pinpoint_client import get_client

logger = logging.getLogger(__name__)


def _default_range(minutes: int | None = None) -> tuple[int, int]:
    """默认时间范围（毫秒时间戳）：最近 N 分钟"""
    minutes = minutes or config.DEFAULT_RANGE_MINUTES
    now = int(time.time() * 1000)
    return now - minutes * 60 * 1000, now


async def _resolve_service_type(client, application_name: str) -> int | None:
    """从 /api/applications 中查找指定应用的 serviceTypeCode。

    Pinpoint 后端的 serverMap/heatmap 接口要求 serviceTypeCode 或 serviceTypeName，
    若用户未指定，自动查询应用列表补全。
    兼容响应结构：{"applications": [...]} / {"data": [...]} / 直接返回数组 [...]。
    """
    result = await client.get_json("/api/applications", params={"serviceName": "DEFAULT"})
    if isinstance(result, dict):
        applications = result.get("applications") or result.get("data") or []
    elif isinstance(result, list):
        applications = result
    else:
        applications = []
    for app in applications:
        if app.get("applicationName") == application_name:
            # 兼容两种字段名：标准 Pinpoint 用 serviceTypeCode，网关返回用 code
            code = app.get("serviceTypeCode") or app.get("code")
            if code is not None:
                return code
            break
    return None


async def list_applications(
    service_name: str = "DEFAULT",
    clear_cache: bool = False,
) -> dict:
    """列出 Pinpoint 监控的全部应用列表。

    对应接口：GET /api/applications（MainController）
    """
    client = get_client()
    params = {"serviceName": service_name}
    if clear_cache:
        params["clearCache"] = "true"
    return await client.get_json("/api/applications", params=params)


async def get_server_map(
    application_name: str,
    from_ts: Optional[int] = None,
    to_ts: Optional[int] = None,
    service_type_code: Optional[int] = None,
    service_type_name: Optional[str] = None,
    caller_range: int = 1,
    callee_range: int = 1,
    bidirectional: bool = True,
    was_only: bool = False,
    use_statistics_agent_state: bool = False,
) -> dict:
    """查询应用的 ServerMap 服务拓扑图数据。

    对应接口：GET /api/servermap/serverMap（MapController#getServerMapData）
    参数 from/to 为毫秒时间戳；不传时默认最近 5 分钟。
    """
    if not application_name:
        raise ValueError("application_name 不能为空")
    f, t = _default_range() if from_ts is None else (from_ts, to_ts)
    client = get_client()
    params = {
        "applicationName": application_name,
        "from": f,
        "to": t,
        "callerRange": caller_range,
        "calleeRange": callee_range,
        "bidirectional": str(bidirectional).lower(),
        "wasOnly": str(was_only).lower(),
        "useStatisticsAgentState": str(use_statistics_agent_state).lower(),
    }
    # 若未指定 serviceType，自动从应用列表查找
    if service_type_code is None and service_type_name is None:
        resolved_code = await _resolve_service_type(client, application_name)
        if resolved_code is not None:
            params["serviceTypeCode"] = resolved_code
        else:
            raise ValueError(
                f"未找到应用 '{application_name}' 的 serviceTypeCode，"
                f"请通过 list_applications_tool 确认应用名是否正确，"
                f"或手动指定 service_type_code / service_type_name"
            )
    else:
        if service_type_code is not None:
            params["serviceTypeCode"] = service_type_code
        if service_type_name is not None:
            params["serviceTypeName"] = service_type_name
    return await client.get_json("/api/servermap/serverMap", params=params)


async def drag_heatmap(
    application: str,
    x1: int,
    x2: int,
    y1: int,
    y2: int,
    agent_id: Optional[str] = None,
    dot_status: Optional[bool] = None,
    limit: int = 50,
    service_type_code: Optional[int] = None,
    service_type_name: Optional[str] = None,
) -> dict:
    """按散点图拖拽区域（时间 x 轴、响应时间 y 轴）查询事务列表。

    对应接口：GET /api/heatmap/drag（HeatMapController#dragScatterArea）
    注意：该接口有 @PreAuthorize 权限校验，需当前账号有 inspector 权限。
    """
    if not application:
        raise ValueError("application 不能为空")
    client = get_client()
    params = {
        "application": application,
        "x1": x1,
        "x2": x2,
        "y1": y1,
        "y2": y2,
        "limit": limit,
    }
    if agent_id is not None:
        params["agentId"] = agent_id
    if dot_status is not None:
        params["dotStatus"] = str(dot_status).lower()
    # 若未指定 serviceType，自动从应用列表查找
    if service_type_code is None and service_type_name is None:
        resolved_code = await _resolve_service_type(client, application)
        if resolved_code is not None:
            params["serviceTypeCode"] = resolved_code
    else:
        if service_type_code is not None:
            params["serviceTypeCode"] = service_type_code
        if service_type_name is not None:
            params["serviceTypeName"] = service_type_name
    return await client.get_json("/api/heatmap/drag", params=params)


async def get_transaction_trace(
    trace_id: str,
    focus_timestamp: int = 0,
    agent_id: Optional[str] = None,
    span_id: int = -1,
    link_trace_id: Optional[str] = None,
    link_span_id: int = -1,
) -> dict:
    """查询单个事务的完整调用树（CallStack）。

    对应接口：GET /api/transaction/trace（TransactionController#getTrace）
    trace_id 必填，格式形如 agentId^transactionId，如 'agent^1234^5678^1'。
    """
    if not trace_id:
        raise ValueError("trace_id 不能为空")
    if "^" not in trace_id:
        raise ValueError(
            f"trace_id 格式不正确: {trace_id!r}，应为 agentId^startTime^seq^elapsed "
            f"（如 'agent01^1720000000000^1^123'），可从 drag_heatmap_tool 返回结果中获取"
        )
    client = get_client()
    params = {
        "traceId": trace_id,
        "focusTimestamp": focus_timestamp,
        "spanId": span_id,
        "linkSpanId": link_span_id,
    }
    if agent_id is not None:
        params["agentId"] = agent_id
    if link_trace_id is not None:
        params["linkTraceId"] = link_trace_id
    return await client.get_json("/api/transaction/trace", params=params)


async def _load_service_type_map(client) -> dict:
    """一次性拉取应用列表，构建 applicationName -> serviceTypeCode 映射"""
    result = await client.get_json("/api/applications", params={"serviceName": "DEFAULT"})
    if isinstance(result, dict):
        applications = result.get("applications") or result.get("data") or []
    elif isinstance(result, list):
        applications = result
    else:
        applications = []
    mapping = {}
    for app in applications:
        name = app.get("applicationName")
        if not name:
            continue
        code = app.get("serviceTypeCode") or app.get("code")
        if code is not None:
            mapping[name] = code
    return mapping


async def get_server_maps_batch(
    applications: list[str],
    from_ts: Optional[int] = None,
    to_ts: Optional[int] = None,
    caller_range: int = 2,
    callee_range: int = 2,
    bidirectional: bool = True,
    was_only: bool = False,
    use_statistics_agent_state: bool = False,
    concurrency: Optional[int] = None,
) -> dict:
    """批量查询多个应用的 ServerMap（架构优化 1.3 链路分析用）。

    与逐个调用 get_server_map 的区别：
    - 应用列表只拉取一次，serviceTypeCode 复用同一份映射
    - 受并发上限保护，避免一次性打满 Pinpoint 后端
    - 单个应用失败不中断整体，失败项进入 failed 供报告声明数据限制

    返回 {"succeeded": [{application, data}], "failed": [{application, error}],
          "successCount", "failedCount", "from", "to"}
    """
    apps = list(dict.fromkeys(a for a in applications if a))
    if not apps:
        raise ValueError("applications 不能为空")
    now = int(time.time() * 1000)
    to = to_ts or now
    start = from_ts or (to - config.DEFAULT_RANGE_MINUTES * 60 * 1000)
    limit = max(1, concurrency or config.ARCH_BATCH_CONCURRENCY)

    client = get_client()
    type_map: dict = {}
    try:
        type_map = await _load_service_type_map(client)
    except Exception as e:  # 映射失败不阻断，缺失 serviceType 的应用会进入 failed
        logger.warning("load serviceType map failed: %s", e)

    semaphore = asyncio.Semaphore(limit)

    async def query_one(name: str):
        async with semaphore:
            params = {
                "applicationName": name,
                "from": start,
                "to": to,
                "callerRange": caller_range,
                "calleeRange": callee_range,
                "bidirectional": str(bidirectional).lower(),
                "wasOnly": str(was_only).lower(),
                "useStatisticsAgentState": str(use_statistics_agent_state).lower(),
            }
            if name in type_map:
                params["serviceTypeCode"] = type_map[name]
            try:
                data = await client.get_json("/api/servermap/serverMap", params=params)
                return {"application": name, "data": data}, None
            except Exception as e:
                return None, {"application": name, "error": str(e)}

    results = await asyncio.gather(*(query_one(a) for a in apps))
    succeeded = [r for r, _ in results if r is not None]
    failed = [err for _, err in results if err is not None]
    if failed:
        logger.warning("serverMap batch: %d/%d failed", len(failed), len(apps))
    return {
        "succeeded": succeeded,
        "failed": failed,
        "successCount": len(succeeded),
        "failedCount": len(failed),
        "applicationCount": len(apps),
        "from": start,
        "to": to,
        "concurrency": limit,
    }
