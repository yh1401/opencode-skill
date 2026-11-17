"""Prometheus HTTP API 客户端

仅封装方案所需的两个查询接口：
- GET /api/v1/query        即时查询（instant query）
- GET /api/v1/query_range  范围查询（range query，"持续 N 分钟"规则必须使用）

时间参数统一使用 Unix 秒（浮点）。
方案 4.1.2：网络异常与 5xx 做有限重试，保留失败状态；4xx 与查询语法错误不重试。
"""
import asyncio
import logging

import httpx

import config

logger = logging.getLogger(__name__)


class PrometheusClient:
    def __init__(
        self,
        base_url: str,
        ssl_verify: bool = True,
        timeout: float = 30.0,
        retry: int = 2,
        retry_backoff: float = 1.0,
    ):
        self.base_url = base_url.rstrip("/")
        self.retry = max(0, retry)
        self.retry_backoff = max(0.0, retry_backoff)
        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            timeout=timeout,
            verify=ssl_verify,
        )

    async def _get(self, path: str, params: dict) -> dict:
        """发起查询，网络异常与 5xx 按指数退避重试

        4xx、非 JSON 与 Prometheus 业务错误（status=error）不重试，直接抛出，
        由上层采样逻辑记为失败点，保证"失败状态可见"而非静默吞掉。
        """
        last_error: Exception | None = None
        for attempt in range(self.retry + 1):
            try:
                resp = await self._client.get(path, params=params)
            except httpx.TimeoutException as e:
                # 超时说明该查询本身已过重，重试只会把等待时间叠加成倍（30s→93s），
                # 直接抛出由上层记为 TIMEOUT 失败点
                last_error = e
                logger.warning(
                    "prometheus query timeout, no retry (attempt %d/%d): %s",
                    attempt + 1,
                    self.retry + 1,
                    e,
                )
                break
            except httpx.TransportError as e:
                last_error = e
            else:
                if resp.status_code >= 500:
                    last_error = RuntimeError(
                        f"Prometheus 服务端错误: status={resp.status_code}, "
                        f"path={path}, query={params.get('query')!r}"
                    )
                else:
                    return self._parse(resp, path, params)
            if attempt >= self.retry:
                break
            delay = self.retry_backoff * (2**attempt)
            logger.warning(
                "prometheus query failed (attempt %d/%d), retry in %.1fs: %s",
                attempt + 1,
                self.retry + 1,
                delay,
                last_error,
            )
            if delay:
                await asyncio.sleep(delay)
        raise last_error  # type: ignore[misc]

    @staticmethod
    def _parse(resp: httpx.Response, path: str, params: dict) -> dict:
        """校验 HTTP 状态、JSON 与 Prometheus 业务状态，返回响应体"""
        resp.raise_for_status()
        try:
            body = resp.json()
        except ValueError:
            raise RuntimeError(
                f"Prometheus 返回非 JSON: status={resp.status_code}, path={path}, "
                f"body_preview={resp.text[:300]!r}"
            )
        if isinstance(body, dict) and body.get("status") == "error":
            raise RuntimeError(
                f"Prometheus 查询失败: {body.get('errorType')}: {body.get('error')}, "
                f"query={params.get('query')!r}"
            )
        return body

    async def query(self, query: str, ts: float | None = None) -> dict:
        """即时查询：返回当前（或指定时刻）指标瞬时值"""
        params: dict = {"query": query}
        if ts is not None:
            params["time"] = ts
        return await self._get("/api/v1/query", params)

    async def query_range(
        self, query: str, start: float, end: float, step: str = "1m"
    ) -> dict:
        """范围查询：返回时间序列，用于"持续 N 分钟"类规则判定"""
        if end <= start:
            raise ValueError(f"end({end}) 必须大于 start({start})")
        return await self._get(
            "/api/v1/query_range",
            {"query": query, "start": start, "end": end, "step": step},
        )

    async def label_names(self, match: str | None = None) -> list[str]:
        """查询标签名集合（/api/v1/labels），用于判断指标族是否带某标签

        只返回标签名、不返回序列内容，体量恒定，适合做口径探测；
        传入 match（如 "kube_deployment_spec_replicas"）可限定指标族。
        """
        params: dict = {}
        if match:
            params["match[]"] = match
        body = await self._get("/api/v1/labels", params)
        return [str(x) for x in (body.get("data") or [])]

    async def label_values(self, label: str, match: str | None = None) -> list[str]:
        """查询某标签的全部取值（/api/v1/label/<label>/values）

        低基数标签（cluster / env）取值有限，可直接返回；
        高基数标签（deployment / pod）不要用本接口，应改用 count() 判定存在性。
        """
        params: dict = {}
        if match:
            params["match[]"] = match
        body = await self._get(f"/api/v1/label/{label}/values", params)
        return [str(x) for x in (body.get("data") or [])]

    async def series_sample(self, match: str, limit: int = 1) -> dict:
        """取一条序列的标签集合（/api/v1/series?match[]=...&limit=1）

        用于 /api/v1/labels 不可用时（VictoriaMetrics 对带 match[] 的 labels
        请求返回 422）降级读取标签名：只取 1 条序列，体量与 labels 接口相当。
        无匹配序列时返回空字典。
        """
        body = await self._get("/api/v1/series", {"match[]": match, "limit": limit})
        data = body.get("data") or []
        return data[0] if data else {}

    async def close(self) -> None:
        await self._client.aclose()


_client: PrometheusClient | None = None


def get_prometheus_client() -> PrometheusClient:
    """模块级单例客户端"""
    global _client
    if _client is None:
        _client = PrometheusClient(
            base_url=config.PROMETHEUS_BASE_URL,
            ssl_verify=config.PROMETHEUS_SSL_VERIFY,
            timeout=config.PROMETHEUS_TIMEOUT,
            retry=config.PROMETHEUS_RETRY,
            retry_backoff=config.PROMETHEUS_RETRY_BACKOFF,
        )
    return _client
