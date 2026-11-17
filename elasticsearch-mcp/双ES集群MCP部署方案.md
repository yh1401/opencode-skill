# 双 ES 集群 MCP 部署方案

> 目标：将领导给的两个 ES 地址部署为 MCP 服务，运行在 Linux 服务器上。
>
> 两个地址：
>
> - `gz4-ana-elkapi.tech.ctseelink.cn` → 192.168.243.165
> - `sz3-ana-elkapi.ctseelink.cn` → 192.168.243.165

***

## 一、关键判断：单集群 vs 双集群

本项目 MCP 服务的 ES 配置**一次只支持一个集群地址**（非数组），见 `src/servers/elasticsearch/mod.rs` 中的 `ElasticsearchMcpConfig`：

```rust
pub struct ElasticsearchMcpConfig {
    pub url: String,           // 只能填一个地址
    pub api_key: Option<String>,
    pub username: Option<String>,
    pub password: Option<String>,
    ...
}
```

默认配置模板（`src/lib.rs`）也只有一个 `elasticsearch` 对象：

```json
{
    "elasticsearch": {
        "url": "${ES_URL}",
        "api_key": "${ES_API_KEY:}",
        "username": "${ES_USERNAME:}",
        "password": "${ES_PASSWORD:}",
        "ssl_skip_verify": "${ES_SSL_SKIP_VERIFY:false}"
    }
}
```

两个域名指向**同一个 IP（192.168.243.165）**，两种可能：

| 可能                            | 表现                      | 部署方式                          |
| ----------------------------- | ----------------------- | ----------------------------- |
| A. 同一集群的两个入口（VIP/双活）          | 两个域名的 `cluster_uuid` 相同 | **只跑 1 个实例**，ES\_URL 二选一      |
| B. 两个不同集群（按 Host/SNI 路由到不同后端） | `cluster_uuid` 不同       | **跑 2 个实例**，端口不同（9090 / 9091） |

***

## 二、开工前必须确认的 4 件事

| # | 确认项        | 说明                                                 |
| - | ---------- | -------------------------------------------------- |
| 1 | **协议与端口**  | 域名没带端口，是 `https://...:9200` 还是走网关的 `443/80`？需逐个端口试 |
| 2 | **认证凭据**   | API Key 还是 username/password。地址本身不带凭据，没有凭据 MCP 连不上 |
| 3 | **是否同一集群** | 上面的 cluster\_uuid 判断                               |
| 4 | **证书**     | 自签证书会导致连接失败，需要 `ES_SSL_SKIP_VERIFY=true`           |

***

## 三、部署步骤

### Step 0：连通性验证（在目标 Linux 服务器上）

```bash
# DNS 解析检查
getent hosts gz4-ana-elkapi.tech.ctseelink.cn
getent hosts sz3-ana-elkapi.ctseelink.cn

# 若服务器 DNS 解析不出，需在 /etc/hosts 里补：
# 192.168.243.165  gz4-ana-elkapi.tech.ctseelink.cn
# 192.168.243.165  sz3-ana-elkapi.ctseelink.cn

# ES 连通性验证
curl -sk -u <user>:<pass> https://<域名>:<端口>/_cat/health
```

### Step 1：准备镜像（Windows 开发机）

镜像是**通用**的——ES 地址在运行时由环境变量注入，**换地址不用重新 build**。

```powershell
# 查看本地已有镜像
docker images | findstr es-mcp

# 若没有则构建
docker build -t es-mcp-linux .

# 导出为 tar 包（服务器离线时用）
docker save -o es-mcp.tar es-mcp-linux

# （可选）验证镜像架构
docker inspect es-mcp-linux --format "{{.Os}} {{.Architecture}}"
# 应输出：linux amd64
```

### Step 2：上传到服务器

```powershell
# Windows PowerShell
scp es-mcp.tar root@<服务器IP>:/data/es-mcp/
```

```bash
# Linux 服务器上
cd /data/es-mcp
docker load -i es-mcp.tar
docker images | grep es-mcp
```

### Step 3：创建环境变量文件

#### 方案 B（双集群）——创建两份 .env

```bash
mkdir -p /data/es-mcp/gz4 /data/es-mcp/sz3

# gz4 集群
cat > /data/es-mcp/gz4/.env << 'EOF'
ES_URL=https://gz4-ana-elkapi.tech.ctseelink.cn:443
ES_USERNAME=sre_mcp
ES_PASSWORD=YOUR_ES_PASSWORD
ES_SSL_SKIP_VERIFY=true
HTTP_ADDRESS=0.0.0.0:9090
EOF

# sz3 集群
cat > /data/es-mcp/sz3/.env << 'EOF'
ES_URL=https://sz3-ana-elkapi.ctseelink.cn:443
ES_USERNAME=sre_mcp
ES_PASSWORD=YOUR_ES_PASSWORD
ES_SSL_SKIP_VERIFY=true
HTTP_ADDRESS=0.0.0.0:9091
EOF

chmod 600 /home/fortress/es-mcp/*/.env
```


### Step 4：启动容器

沿用既定的 `--network host` 方案。**注意：host 网络下两个实例必须用不同端口**，靠 `HTTP_ADDRESS` 区分（见 `src/cli.rs` 中 `HttpCommand.address` 字段，支持 `HTTP_ADDRESS` 环境变量）。

#### 方案 B（双集群）

```bash
# gz4 实例 → 9090
docker run -d --restart unless-stopped --network host \
  --name es-mcp-gz4 --env-file /home/fortress/es-mcp/gz4/.env \
  es-mcp-linux http

# sz3 实例 → 9091
docker run -d --restart unless-stopped --network host \
  --name es-mcp-sz3 --env-file /home/fortress/es-mcp/sz3/.env \
  es-mcp-linux http
```


### Step 5：验证

```bash
# 容器状态
docker ps --filter name=es-mcp

# 健康检查
curl http://localhost:9090/ping          # 返回 Ready
curl http://localhost:9091/ping          # 方案 B 才需要

# 列出可用工具（应返回 list_indices / get_mappings / search / esql / get_shards）
curl -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 查看日志
docker logs -f es-mcp-gz4
docker logs -f es-mcp-sz3
```

### Step 6：客户端接入

MCP 客户端配置指向 streamable-http 端点：

| 集群  | MCP 端点                    |
| --- | ------------------------- |
| gz4 | `http://<服务器IP>:9090/mcp` |
| sz3 | `http://<服务器IP>:9091/mcp` |

健康检查路径（见 `src/protocol/http.rs` 路由注册）：

| 路径               | 用途                     |
| ---------------- | ---------------------- |
| `/ping`          | 简易健康检查                 |
| `/_health/ready` | K8s 就绪探针               |
| `/_health/live`  | K8s 存活探针               |
| `/mcp`           | streamable-http MCP 端点 |
| `/mcp/sse`       | SSE MCP 端点             |

***

## 四、HTTP 服务路由总览

基于 `src/protocol/http.rs`，HTTP 模式注册的路由：

```
/              → 首页（简单欢迎信息）
/ping          → 健康检查，返回 "Ready"
/mcp           → streamable-http MCP 端点（主）
/mcp/sse       → SSE MCP 端点（备）
/_health/ready → 就绪探针
/_health/live  → 存活探针
```

***

## 五、常见问题排查

| 问题                             | 原因                                                          | 解决                                        |
| ------------------------------ | ----------------------------------------------------------- | ----------------------------------------- |
| `PORTS` 列为空                    | `daemon.json` 中设置了 `"iptables": false` 或 `"bridge": "none"` | 删除这两项，重启 Docker                           |
| `docker0` 网桥不存在                | Docker 网络被禁用                                                | 同上修复 `daemon.json`                        |
| 容器 IP 显示 `invalid`             | IPv4 转发未开启                                                  | `sysctl net.ipv4.ip_forward=1` 并重启 Docker |
| 连接 ES 超时                       | 容器内 localhost 指容器自身，不是宿主机                                   | 使用 `--network host` 或宿主机真实 IP             |
| `curl localhost:9090/ping` 无响应 | 绑定了 127.0.0.1 而不是 0.0.0.0                                   | 设置 `HTTP_ADDRESS=0.0.0.0:9090`            |
| 权限不足                           | API Key / 用户名密码错误                                           | 检查认证凭据和 ES 集群可达性                          |
| SSL 握手失败                       | 自签证书                                                        | 设置 `ES_SSL_SKIP_VERIFY=true`              |
| 两个实例端口冲突                       | 都用默认 8080                                                   | 用 `HTTP_ADDRESS` 分别指定不同端口                 |

***

## 六、容器运维命令

```bash
# 重启
docker restart es-mcp-gz4
docker restart es-mcp-sz3

# 更新（重新构建镜像后）
docker rm -f es-mcp-gz4
docker build -t es-mcp-linux .
docker run -d --restart unless-stopped --network host \
  --name es-mcp-gz4 --env-file /home/fortress/es-mcp/gz4/.env \
  es-mcp-linux http

docker run -d --restart unless-stopped --network host \
  --name es-mcp-sz3 --env-file /home/fortress/es-mcp/sz3/.env \
  es-mcp-linux http

# 进入容器排查
docker exec -it es-mcp-gz4 sh

# 查看实时日志
docker logs -f es-mcp-gz4
docker logs --tail 100 es-mcp-gz4

# 资源占用
docker stats es-mcp-gz4 --no-stream
```

***

## 七、架构限制说明

| 限制                          | 说明                                                                                                                                   |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **单实例只支持单集群**               | 配置结构 `ElasticsearchMcpConfig` 只有一个 `url` 字段，想同时查两个集群只能跑两个实例                                                                          |
| **认证方式二选一**                 | API Key (`ES_API_KEY`) 或 Basic Auth (`ES_USERNAME` + `ES_PASSWORD`)，见 `mod.rs` 中 `Credentials::EncodedApiKey` / `Credentials::Basic` |
| **HTTP Authorization 头被忽略** | `EsClientProvider::get()` 中明确注释：HTTP 客户端转发的 Authorization 头不会用于 ES 认证，凭据始终来自服务端配置                                                    |
| **容器模式自动重写 localhost**      | `CONTAINER_MODE=true` 时自动将 `localhost` 替换为 `host.docker.internal`（见 `rewrite_localhost` 函数）                                          |

