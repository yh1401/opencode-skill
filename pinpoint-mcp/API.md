# Pinpoint Web API 连通性验证

验证 Pinpoint Web 接口（`/api/applications` 等）认证是否通过，返回 **200**（正常）还是 **302**（未认证/会话失效，重定向到登录页）。

> 当前环境（两个域名，认证体系不同）：
>
> - `https://trace-api.tech.ctseelink.cn:52080` — **Basic Auth 体系**，Basic 认证返回 200 ✅（MCP 采用此域名）
> - `https://trace.tech.ctseelink.cn:52080` — **SSO Cookie 体系**，Basic 认证返回 302（需 Cookie + X-Auth-Session）
>   默认 Basic 凭据：`YOUR_PINPOINT_BASIC_AUTH`（即 `pinpoint-web:YOUR_PINPOINT_PASSWORD` 的 Base64）

## 1. 只看状态码

```bash
curl -s -o /dev/null -w "%{http_code}\n" https://trace.tech.ctseelink.cn:52080/api/applications
```

## 2. 带 Basic 认证验证

```bash
# 方式一：-u 自动生成 Authorization 头
curl -s -o /dev/null -w "%{http_code}\n" \
  -u "pinpoint-web:YOUR_PINPOINT_PASSWORD" \
  https://trace.tech.ctseelink.cn:52080/api/applications

# 方式二：直接用固定 Base64 凭据
curl -s -o /dev/null -w "%{http_code}\n" \
  -H "Authorization: Basic YOUR_PINPOINT_BASIC_AUTH" \
  https://trace-api.tech.ctseelink.cn:52080/api/applications
```

## 3. 查看完整响应头（302 时定位跳转目标）

```bash
curl -s -D - -o /dev/null \
  -H "Authorization: Basic YOUR_PINPOINT_BASIC_AUTH" \
  https://trace-api.tech.ctseelink.cn:52080/api/applications
```

输出示例：

```
HTTP/1.1 302 Moved Temporarily
Server: nginx
Location: https://trace.tech.ctseelink.cn:52080/userLogin   ← 认证失效，重定向到 SSO 登录页
```

## 4. 对照组：确认网关是否处理 Basic 头

```bash
# 不带任何认证头
curl -s -o /dev/null -w "%{http_code}\n" https://trace.tech.ctseelink.cn:52080/api/applications

# 只带 Basic 头（方式二命令）
```

> 若两者结果相同（均为 302）→ 网关**忽略 Basic**，该环境必须改用 Cookie 认证。

## 5. Cookie + X-Auth-Session 组合验证（当前环境实际所需）

```bash
curl -s -D - -o /dev/null \
  -H "Cookie: <浏览器登录后的完整Cookie串>" \
  -H "X-Auth-Session: <网关下发的令牌>" \
  https://trace.tech.ctseelink.cn:52080/api/applications
```

## 6. 结果判定

| 状态码                        | 含义                             | 处理                                                             |
| ----------------------------- | -------------------------------- | ---------------------------------------------------------------- |
| `200`                         | 认证通过，正常返回数据           | 通过                                                             |
| `302`（Location: /userLogin） | 会话失效/未认证，被 SSO 网关拦截 | 需配置 `PINPOINT_COOKIE` + `PINPOINT_X_AUTH_SESSION`；Basic 无效 |
| `302`（Location: /login）     | Spring Security formLogin 拦截   | 账号密码或 Cookie 问题                                           |
| `401`                         | Basic 凭据错误                   | 检查凭据是否正确                                                 |
| `403`                         | 权限不足                         | 检查账号 inspector 权限                                          |

## 7. 结论（当前环境）

- **`trace-api` 域名走 Basic Auth 体系**：Basic 认证返回 200 ✅。MCP 应使用该域名，配置 `WEB_BASE_URL=https://trace-api.tech.ctseelink.cn:52080` + `PINPOINT_SSL_VERIFY=false`，认证用默认 Basic 凭据即可，**无需 Cookie / X-Auth-Session**。
- **`trace` 域名走 SSO Cookie 体系**：Basic 返回 302 到 `/userLogin`，若使用该域名则必须配置 `PINPOINT_COOKIE` + `PINPOINT_X_AUTH_SESSION`。
