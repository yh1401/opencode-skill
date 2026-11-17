"""EasyViews 平台代理接口客户端

平台所有数据接口统一通过 POST {BASE}/api/third/thirdAuth/getTykjProvince 代理调用，
请求体结构：

    {
        "url": "<内层真实接口地址>",
        "types": "GET" | "POST",
        "provinceDetail": {"province": ..., "appId": ..., "appSecret": ...},
        "reqParam": {...}
    }

响应统一为 {"code": 0, "data": ..., "msg": "操作成功"}，
code 非 0 视为业务失败，直接抛出便于上层识别数据不足。
"""
import asyncio
import logging
import time

import httpx

import config

logger = logging.getLogger(__name__)


class EasyViewsClient:
    """EasyViews 代理接口客户端（无需 Cookie/登录，凭据在请求体中）"""

    def __init__(
        self,
        base_url: str,
        proxy_path: str,
        province: str,
        app_id: str,
        app_secret: str,
        ssl_verify: bool = True,
        timeout: float = 60.0,
        retry: int = 2,
        retry_backoff: float = 1.0,
        retry_timeout: bool = False,
    ):
        self.base_url = base_url.rstrip("/")
        self.proxy_path = proxy_path
        self.province = province
        self.app_id = app_id
        self.app_secret = app_secret
        self.retry = max(0, retry)
        self.retry_backoff = max(0.0, retry_backoff)
        self.retry_timeout = retry_timeout
        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            timeout=timeout,
            verify=ssl_verify,
        )

    async def call(self, url: str, types: str = "GET", req_param: dict | None = None) -> dict:
        """调用代理接口，返回原始响应体（已校验业务 code）

        网络异常与 5xx 按 EASYVIEWS_RETRY 做指数退避重试；
        4xx、非 JSON 与业务 code 非 0 不重试，直接抛出便于上层标记数据不足。
        """
        payload = {
            "url": url,
            "types": types,
            "provinceDetail": {
                "province": self.province,
                "appId": self.app_id,
                "appSecret": self.app_secret,
            },
            "reqParam": req_param or {},
        }
        logger.info("easyviews proxy call: %s (%s)", url, types)
        last_error: Exception | None = None
        started = time.perf_counter()
        for attempt in range(self.retry + 1):
            try:
                resp = await self._client.post(self.proxy_path, json=payload)
            except httpx.TimeoutException as e:
                # 超时说明上游查询过重或卡住，重试只会把等待叠加成倍，直接上报
                if not self.retry_timeout:
                    logger.warning(
                        "easyviews proxy call timeout after %.2fs: %s (%s)",
                        time.perf_counter() - started, url, types,
                    )
                    raise
                last_error = e
            except httpx.TransportError as e:
                last_error = e
            else:
                if resp.status_code >= 500:
                    last_error = RuntimeError(
                        f"EasyViews 服务端错误: status={resp.status_code}, url={url}"
                    )
                else:
                    logger.info(
                        "easyviews proxy ok: %s (%s) elapsed=%.2fs bytes=%d",
                        url, types, time.perf_counter() - started, len(resp.content),
                    )
                    return self._parse(resp, url, req_param)
            if attempt >= self.retry:
                break
            delay = self.retry_backoff * (2**attempt)
            logger.warning(
                "easyviews proxy call failed (attempt %d/%d), retry in %.1fs: %s",
                attempt + 1,
                self.retry + 1,
                delay,
                last_error,
            )
            if delay:
                await asyncio.sleep(delay)
        raise last_error  # type: ignore[misc]

    @staticmethod
    def _parse(resp: httpx.Response, url: str, req_param: dict | None) -> dict:
        """校验 HTTP 状态与业务 code，返回响应体"""
        resp.raise_for_status()
        try:
            body = resp.json()
        except ValueError:
            raise RuntimeError(
                f"EasyViews 返回非 JSON: status={resp.status_code}, url={url}, "
                f"body_preview={resp.text[:300]!r}"
            )
        if isinstance(body, dict) and body.get("code") not in (0, None):
            raise RuntimeError(
                f"EasyViews 业务错误: code={body.get('code')}, msg={body.get('msg')}, "
                f"url={url}, reqParam={req_param}"
            )
        return body

    async def close(self) -> None:
        await self._client.aclose()


_client: EasyViewsClient | None = None


def get_easyviews_client() -> EasyViewsClient:
    """模块级单例客户端"""
    global _client
    if _client is None:
        if not (config.EASYVIEWS_APP_ID and config.EASYVIEWS_APP_SECRET):
            raise RuntimeError(
                "缺少 EasyViews 凭据：出于安全考虑不再提供默认值，"
                "请通过环境变量设置 EASYVIEWS_APP_ID / EASYVIEWS_APP_SECRET"
            )
        _client = EasyViewsClient(
            base_url=config.EASYVIEWS_BASE_URL,
            proxy_path=config.EASYVIEWS_PROXY_PATH,
            province=config.EASYVIEWS_PROVINCE,
            app_id=config.EASYVIEWS_APP_ID,
            app_secret=config.EASYVIEWS_APP_SECRET,
            ssl_verify=config.EASYVIEWS_SSL_VERIFY,
            timeout=config.EASYVIEWS_TIMEOUT,
            retry=config.EASYVIEWS_RETRY,
            retry_backoff=config.EASYVIEWS_RETRY_BACKOFF,
            retry_timeout=config.EASYVIEWS_RETRY_TIMEOUT,
        )
    return _client
