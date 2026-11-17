"""Pinpoint Web API 异步客户端：处理登录认证与会话复用"""
import asyncio
import logging
import re

import httpx

import config

logger = logging.getLogger(__name__)

# 认证失效的判定状态码（302 为网关重定向到登录页）
_RETRY_STATUS_CODES = {302, 401, 403}


def _parse_login_form(html: str, default_action: str) -> tuple[str, dict]:
    """从登录页 HTML 中解析表单 action 与字段名（仅处理普通表单，无验证码场景）"""
    m = re.search(r'<form[^>]*action=["\']([^"\']*)["\']', html, re.I)
    action = m.group(1) if m else default_action
    fields = {}
    for m in re.finditer(r'<input[^>]*name=["\']([^"\']+)["\']', html, re.I):
        fields[m.group(1)] = ""
    return action, fields


def _merge_set_cookie(resp: httpx.Response, existing: str = "") -> str:
    """从响应头解析全部 Set-Cookie，与已有 Cookie 头合并，返回新的 Cookie 头值"""
    merged: dict[str, str] = {}
    if existing:
        for item in existing.split(";"):
            if "=" in item:
                k, v = item.strip().split("=", 1)
                merged[k] = v
    for sc in resp.headers.get_list("set-cookie"):
        first = sc.split(";", 1)[0]
        if "=" in first:
            k, v = first.split("=", 1)
            merged[k.strip()] = v.strip()
    return "; ".join(f"{k}={v}" for k, v in merged.items())


class PinpointClient:
    """封装对 Pinpoint Web 的 HTTP 调用。

    认证支持多种模式（可组合）：
    - Cookie 认证：构造时传 cookie（浏览器登录后的完整 Cookie 串），
      全局携带 Cookie 头，用于企业 SSO / 定制网关环境（nginx 校验会话）。
      配置后跳过 formLogin。
    - Basic 认证：构造时传 basic_auth（Base64 的 "user:pass"），
      全局携带 Authorization: Basic xxx 头，用于通过 nginx/网关的 Basic Auth。
    - 自动登录（auto_login=True）：请求遇 302/401/403 时，
      自动访问 LOGIN_URL 解析登录表单并提交账号密码，
      会话 Cookie 由 httpx jar 自动保存续期，X-Auth-Session 从响应头同步。
      （注意：开启自动登录时建议不要配置静态 PINPOINT_COOKIE，否则静态头会覆盖 jar）
    - formLogin：POST /login 获取 pinpointJwt Cookie（httpx 自动维护）。
    """

    def __init__(self, base_url: str, username: str, password: str,
                 basic_auth: str = "", cookie: str = "", ssl_verify: bool = True,
                 x_auth_session: str = "", auto_login: bool = False):
        self.base_url = base_url.rstrip("/")
        self.username = username
        self.password = password
        self.basic_auth = basic_auth
        self.cookie = cookie
        self.auto_login = auto_login
        headers = {"Authorization": f"Basic {basic_auth}"} if basic_auth else None
        if cookie:
            headers = {**(headers or {}), "Cookie": cookie}
        if x_auth_session:
            headers = {**(headers or {}), "X-Auth-Session": x_auth_session}
        # 认证失效重试时的并发保护（MCP 可能并发调用工具）
        self._login_lock = asyncio.Lock()
        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            headers=headers,
            timeout=30.0,
            # 默认不跟随重定向：由 _request 显式判定 302 并触发自动登录
            follow_redirects=False,
            verify=ssl_verify,
        )

    async def login(self) -> None:
        """建立认证会话（一次性）。

        - Cookie 模式：已配置完整 Cookie 串，跳过 formLogin。
        - 组合模式（Basic + formLogin）：Basic 头随请求全局携带，
          /login 亦携带；应用层仍需 Cookie 时两者缺一不可。
        - 纯 formLogin 模式：仅 Cookie。
        - formLogin 失败且已配置 Basic 时降级为仅 Basic（不抛错）。
        """
        if self.cookie:
            logger.info("cookie auth configured, skip formLogin")
            return
        try:
            resp = await self._client.post(config.LOGIN_URL, data={
                "username": self.username,
                "password": self.password,
            })
        except httpx.HTTPError as e:
            if self.basic_auth:
                logger.warning("formLogin failed (%s); fallback to Basic auth only", e)
                return
            raise
        if resp.status_code >= 400:
            if self.basic_auth:
                logger.warning("formLogin failed (status=%s); fallback to Basic auth only",
                               resp.status_code)
                return
            resp.raise_for_status()
        # 解析 Set-Cookie 更新全局 Cookie 头
        new_cookie = _merge_set_cookie(resp, existing=self._client.headers.get("Cookie", ""))
        if new_cookie:
            self._client.headers["Cookie"] = new_cookie
            logger.info("login success, updated Cookie header")
        else:
            logger.info("login success, cookies: %s", list(self._client.cookies.jar))

    async def _auto_login(self) -> None:
        """基于 LOGIN_URL 的自动登录：解析表单 → 提交账号密码 → 续期会话。

        成功后：会话 Cookie 由 httpx jar 自动维护（后续请求自动携带），
        X-Auth-Session 从响应头同步到全局请求头。
        失败（验证码/动态 token/表单不匹配）抛出明确异常。
        """
        async with self._login_lock:
            logger.info("auto login via %s", config.LOGIN_URL)
            # 1. 获取登录页（不跟随重定向）
            page = await self._client.get(config.LOGIN_URL, follow_redirects=False)
            if page.status_code == 302:
                raise RuntimeError(
                    f"auto login failed: {config.LOGIN_URL} -> {page.headers.get('location')}, "
                    f"确认 Basic 凭据是否有效")
            # 2. 解析表单 action 与字段
            action, form_data = _parse_login_form(page.text, default_action=config.LOGIN_URL)
            user_field = config.PINPOINT_LOGIN_USERNAME_FIELD
            pass_field = config.PINPOINT_LOGIN_PASSWORD_FIELD
            if user_field in form_data:
                form_data[user_field] = self.username
            if pass_field in form_data:
                form_data[pass_field] = self.password
            logger.info("auto login: submit form action=%s fields=%s", action, list(form_data))
            # 3. 提交登录表单
            resp = await self._client.post(action, data=form_data, follow_redirects=False)
            logger.info(
                "auto login submit: status=%s, location=%s, new_cookies=%s",
                resp.status_code,
                resp.headers.get("location"),
                [c.name for c in resp.cookies.jar],
            )
            # 解析 Set-Cookie 更新全局 Cookie 头（httpx jar 在此场景不可靠，手动维护）
            new_cookie = _merge_set_cookie(resp, existing=self._client.headers.get("Cookie", ""))
            if new_cookie:
                self._client.headers["Cookie"] = new_cookie
                logger.info("auto login: updated Cookie header")
            # 4. 同步 X-Auth-Session（网关下发的新令牌）
            xauth = resp.headers.get("x-auth-session") or page.headers.get("x-auth-session")
            if xauth:
                self._client.headers["X-Auth-Session"] = xauth
                logger.info("auto login: updated X-Auth-Session")
            # 5. 校验登录是否真正续期成功（否则及时报错，避免静默失效）
            if not new_cookie and not xauth:
                raise RuntimeError(
                    f"auto login failed: 未获取到任何会话凭据 (login_page_status={page.status_code}, "
                    f"submit_status={resp.status_code}, location={resp.headers.get('location')})，"
                    f"请检查 LOGIN_URL({config.LOGIN_URL}) 是否正确、"
                    f"登录页是否含验证码/动态 token，必要时改用 PINPOINT_COOKIE 手动配置")
            if resp.status_code in _RETRY_STATUS_CODES and not new_cookie:
                raise RuntimeError(
                    f"auto login failed: status={resp.status_code}, "
                    f"location={resp.headers.get('location')}，"
                    f"登录页可能含验证码/动态 token，需改用 PINPOINT_COOKIE 手动配置")

    async def get_json(self, path: str, params: dict | None = None) -> dict:
        """GET 请求并返回 JSON；会话失效时自动重新认证重试一次"""
        return await self._request("GET", path, params=params)

    async def _request(self, method: str, path: str, **kwargs) -> dict:
        resp = await self._client.request(method, path, **kwargs)
        if resp.status_code in _RETRY_STATUS_CODES:
            if self.auto_login:
                logger.warning("request failed (status=%s), auto re-login and retry", resp.status_code)
                await self._auto_login()
            else:
                logger.warning("request failed (status=%s), re-auth and retry", resp.status_code)
                await self.login()
            resp = await self._client.request(method, path, **kwargs)
            if resp.status_code in _RETRY_STATUS_CODES:
                raise RuntimeError(
                    f"认证重试后仍失败: status={resp.status_code}, url={resp.url}, "
                    f"location={resp.headers.get('location')}，请检查账号/验证码/Cookie")
        resp.raise_for_status()
        try:
            return resp.json()
        except ValueError:
            # 网关可能返回登录页/拦截页 HTML，暴露细节便于定位
            raise RuntimeError(
                f"非 JSON 响应: status={resp.status_code}, url={resp.url}, "
                f"content_type={resp.headers.get('content-type')}, "
                f"body_preview={resp.text[:300]!r}"
            )

    async def close(self) -> None:
        await self._client.aclose()


_client: PinpointClient | None = None


def get_client() -> PinpointClient:
    """模块级单例客户端"""
    global _client
    if _client is None:
        if not (
            config.PINPOINT_COOKIE
            or config.PINPOINT_BASIC_AUTH
            or (config.PINPOINT_USER and config.PINPOINT_PASS)
        ):
            raise RuntimeError(
                "缺少 Pinpoint 认证配置：出于安全考虑不再提供默认凭据，"
                "请通过环境变量设置 PINPOINT_COOKIE / PINPOINT_BASIC_AUTH "
                "或 PINPOINT_USER + PINPOINT_PASS"
            )
        _client = PinpointClient(
            base_url=config.WEB_BASE_URL,
            username=config.PINPOINT_USER,
            password=config.PINPOINT_PASS,
            basic_auth=config.PINPOINT_BASIC_AUTH,
            cookie=config.PINPOINT_COOKIE,
            ssl_verify=config.PINPOINT_SSL_VERIFY,
            x_auth_session=config.PINPOINT_X_AUTH_SESSION,
            auto_login=config.PINPOINT_AUTO_LOGIN,
        )
    return _client
