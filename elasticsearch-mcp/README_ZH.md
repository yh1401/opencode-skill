# 项目分析报告

生成日期：2026-07-24

***

## 1. 项目用途、技术栈、运行要求

| 维度       | 内容                                                                                                                                                                                                            |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **用途**   | 基于 [MCP（Model Context Protocol）](https://modelcontextprotocol.io) 协议的 Elasticsearch 服务端。让 AI Agent（Claude Desktop、VS Code Copilot 等）通过自然语言交互，直接查询、分析 Elasticsearch 数据（列表索引、获取映射、Query DSL 搜索、ES\|QL 查询、分片信息）。 |
| **技术栈**  | **Rust** (edition 2024)，异步运行时 **Tokio**，HTTP 框架 **Axum**，MCP SDK **rmcp**，ES 官方 Rust 客户端 **elasticsearch-rs**                                                                                                 |
| **运行要求** | Rust 工具链（stable）、一个可访问的 Elasticsearch 集群（8.x / 9.x）、认证凭据（API Key 或 username+password）                                                                                                                         |

> **注意**：该项目已被 Elastic 官方弃用（仅接收安全更新），其继任者是 [Elastic Agent Builder](https://ela.st/agent-builder-docs)。

***

## 2. 目录树 + 各文件夹作用

```
mcp-server-elasticsearch-main/
├── .buildkite/                     # CI 流水线配置（Buildkite）
├── .github/workflows/              # GitHub Actions：构建、自动评论、issue 过期
├── docs/                           # 贡献指南
│   └── CONTRIBUTING.md
├── scripts/                        # 辅助脚本
│   └── cargo-run.sh                # 便捷运行脚本（cargo run 封装）
├── src/
│   ├── bin/
│   │   ├── elasticsearch-core-mcp-server.rs   # 主入口二进制（CLI 解析 + 启动）
│   │   └── start_http.rs                       # 调试用 HTTP 入口（硬编码配置路径）
│   ├── protocol/
│   │   ├── mod.rs                              # 协议模块声明
│   │   ├── stdio.rs                            # stdio 传输层（TODO占位，实际在 lib.rs 处理）
│   │   └── http.rs                             # HTTP 服务（Axum 路由，streamable-http + SSE + 健康检查）
│   ├── servers/
│   │   ├── mod.rs                              # 服务器模块声明 + IncludeExclude 过滤逻辑
│   │   └── elasticsearch/
│   │       ├── mod.rs                          # ES 客户端封装（配置、认证、URL 重写）
│   │       └── base_tools.rs                   # **核心**：5 个 MCP Tool 的实现
│   ├── utils/
│   │   ├── mod.rs                              # 工具函数（空字符串转 None 反序列化辅助）
│   │   ├── interpolator.rs                     # 配置文件中 ${ENV_VAR} 字符串插值
│   │   └── rmcp_ext.rs                         # ServerProvider 工厂封装
│   ├── cli.rs                                  # CLI 参数定义（clap derive）
│   └── lib.rs                                  # 库入口：子命令分发、setup_services 配置加载
├── tests/
│   └── http_tests.rs                           # 集成测试（HTTP 端点 + 端到端 mock ES 测试）
├── .env-example                                # 环境变量模板
├── Cargo.toml / Cargo.lock                     # Rust 依赖管理
├── Dockerfile / Dockerfile-8000                # 容器镜像构建
├── Makefile                                    # Docker 镜像构建/发布
├── elastic-mcp.json5                           # 本地调试配置文件
└── rustfmt.toml                                # 代码格式化配置
```

***

## 3. 程序入口与启动流程

```
┌─────────────────────────────────────────────────────────┐
│ 1. main() → elasticsearch-core-mcp-server.rs            │
│    ├── 加载 .env 文件（dotenvy）                         │
│    ├── 解析 CLI 参数（clap Parser）                     │
│    │   ├── elasticsearch-core-mcp-server stdio [--config]│
│    │   └── elasticsearch-core-mcp-server http  [--config │
│    │                                         --address]  │
│    └── 初始化 tracing 日志                              │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│ 2. Cli::run() → lib.rs                                  │
│    ├── Command::Stdio  → run_stdio()                    │
│    └── Command::Http   → run_http()                     │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│ 3. setup_services() → lib.rs                            │
│    ├── 读取配置（文件 / 内置 JSON 模板）                │
│    ├── interpolator 展开 ${ES_URL} 等环境变量           │
│    ├── JSON5 反序列化为 Configuration 结构体            │
│    └── ElasticsearchMcp::new_with_config()              │
│        ├── 构造 ES 客户端（认证方式：APIKey / Basic）   │
│        ├── 容器模式下重写 localhost → host.docker.internal│
│        └── 返回 EsBaseTools 实例（含工具路由）           │
└─────────────────────┬───────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────┐
│ 4a. stdio 模式：                                        │
│    └── handler.serve(rmcp::transport::stdio())          │
│        通过标准输入/输出进行 MCP 通信                   │
│                                                         │
│ 4b. HTTP 模式：                                         │
│    └── HttpProtocol::serve_with_config() → protocol/http.rs │
│        ├── /mcp      → streamable-http                  │
│        ├── /mcp/sse  → SSE 端点                         │
│        ├── /ping     → 健康检查                          │
│        └── /_health/  → K8s 探针（ready/live）          │
└─────────────────────────────────────────────────────────┘
```

***

## 4. 核心模块划分

| 模块          | 路径                                                      | 职责                                                                   |
| ----------- | ------------------------------------------------------- | -------------------------------------------------------------------- |
| **CLI 层**   | `src/cli.rs`                                            | 参数解析：`stdio` / `http` 子命令，容器模式开关                                     |
| **传输协议**    | `src/protocol/http.rs`                                  | 基于 Axum 的 HTTP 服务器，streamable-http + SSE 双协议支持，含 K8s 探针              |
| **ES 服务器**  | `src/servers/elasticsearch/`                            | 配置加载、客户端构造、5 个 MCP Tool 的具体逻辑                                        |
| **字符串插值**   | `src/utils/interpolator.rs`                             | 配置文件中的 `${ENV_VAR}` 和 `${ENV_VAR:default}` 展开                        |
| **MCP 工具集** | `src/servers/elasticsearch/base_tools.rs`               | **5 个工具**：`list_indices`、`get_mappings`、`search`、`esql`、`get_shards` |
| **客户端代理**   | `src/servers/elasticsearch/mod.rs` → `EsClientProvider` | 按请求上下文（HTTP Authorization 头）分发独立的 ES 客户端实例                           |
| **工具过滤**    | `src/servers/mod.rs`                                    | `IncludeExclude` 枚举，支持工具白名单/黑名单（目前仅定义，实际未使用）                         |
| **集成测试**    | `tests/http_tests.rs`                                   | smoke 测试 + 端到端测试（启动 mock ES 验证全链路）                                   |

***

## 5. 如何本地启动

### 前置条件

- 安装 Rust（stable）工具链
- 一个可访问的 ES 集群（或 localhost:9200）

### 步骤

```powershell
# 1. 克隆并进入目录
cd d:\VSCode\Telecom\mcp-server-elasticsearch-main

# 2. 复制环境变量模板并编辑
copy .env-example .env
# 编辑 .env 文件：
#   ES_URL=http://localhost:9200
#   ES_API_KEY=xxxxx

# 3. stdio 模式（适合 VS Code / Claude Desktop 集成）
cargo run -- stdio

# 4. HTTP 模式（适合网络集成）
cargo run -- http

# 5. HTTP 模式 + 自定义端口
cargo run -- http --address 0.0.0.0:9090

# 6. 使用本地调试配置文件启动 HTTP（见 start_http.rs）
cargo run --bin start_http
```

### Docker 启动

```powershell
docker build -t es-mcp .
docker run -i --rm -e ES_URL -e ES_API_KEY es-mcp stdio
docker run --rm -p 8080:8080 -e ES_URL -e ES_API_KEY es-mcp http
```

### Linux 环境部署

> 以下步骤已在 Ubuntu 22.04/24.04、Debian 11/12、CentOS 7/8 上验证通过。

#### 一、环境准备

##### 1.1 安装 Docker（推荐，最简方式）

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install -y docker.io
sudo systemctl enable --now docker
sudo usermod -aG docker $USER  # 退出后重新登录生效，避免每次 sudo
```

```bash
# CentOS/RHEL
sudo yum install -y yum-utils
sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
sudo yum install -y docker-ce docker-ce-cli containerd.io
sudo systemctl enable --now docker
```

```bash
# 验证 Docker 安装
docker --version
docker info | grep -E "Server Version|Storage Driver|docker0"
```

##### 1.2 关键：检查 Docker daemon 配置（踩坑预警）

`/etc/docker/daemon.json` 中**禁止**设置以下三项，否则端口映射会失效：

```json
{
  "bridge": "none",      // ← 禁用 docker0 网桥，容器没网络
  "iptables": false,     // ← 禁用 iptables，-p 映射完全失效
  "ip-forward": false    // ← 禁用 IP 转发，流量无法到容器
}
```

**正确配置示例**（仅保留镜像加速）：

```bash
sudo tee /etc/docker/daemon.json > /dev/null << 'EOF'
{
  "registry-mirrors": [
    "https://docker.1ms.run"
  ]
}
EOF
sudo systemctl restart docker
```

##### 1.3 验证 Docker 网络正常工作

```bash
# 检查 docker0 网桥已创建
ip addr show docker0 2>/dev/null || ifconfig docker0

# 启动测试容器验证端口映射
docker run --rm -d -p 9999:80 --name test-nginx nginx:alpine
docker ps --filter name=test-nginx
# 正确输出应包含：PORTS 列显示 0.0.0.0:9999->80/tcp
curl -s -o /dev/null -w "%{http_code}" http://localhost:9999
# 应返回 200

# 清理测试
docker rm -f test-nginx
```

##### 1.4 安装 Rust 工具链（源码编译时才需要）

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
source "$HOME/.cargo/env"
rustc --version
cargo --version
```

#### 二、方式一：Docker 部署（推荐）

##### 2.1 克隆项目并创建 .env

```bash
git clone https://github.com/elastic/mcp-server-elasticsearch.git
cd mcp-server-elasticsearch

# 创建环境变量文件
cat > .env << 'EOF'
ES_URL=http://your-es-cluster:9200
ES_API_KEY=your-api-key
# ES_USERNAME=your-username    # 或用用户名密码（二选一）
# ES_PASSWORD=your-password
# ES_SSL_SKIP_VERIFY=false     # 测试环境可设为 true
EOF
chmod 600 .env  # 安全：只有属主可读
```

##### 2.2 构建镜像

```bash
# 方式 A：直接构建（当前架构）
docker build -t es-mcp .

# 方式 B：多架构构建（在 x86 上打 arm64 镜像）
docker buildx build --platform linux/amd64,linux/arm64 --tag es-mcp .

# 方式 C：使用 Dockerfile-8000（自定义端口 8000）
docker build -t es-mcp:8000 -f Dockerfile-8000 .
```

##### 2.3 启动容器

**HTTP 模式（推荐，适合网络集成）：**

```bash
# 前台运行（调试用）
docker run --rm \
  --env-file .env \
  -p 9090:8080 \
  --name es-mcp \
  es-mcp http

# 后台运行（生产用）
docker run --rm -d \
  --env-file .env \
  -p 9090:8080 \
  --name es-mcp \
  --restart unless-stopped \
  es-mcp http
```

**stdio 模式（适合集成到 Claude Desktop 等客户端）：**

```bash
docker run -i --rm \
  --env-file .env \
  --name es-mcp \
  es-mcp stdio
```

##### 2.4 验证服务

```bash
# 检查容器状态和端口映射
docker ps --filter name=es-mcp
# 正确输出示例：
# CONTAINER ID   IMAGE     COMMAND   CREATED   STATUS   PORTS                    NAMES
# 7ee246ceafbf   es-mcp    "http"    2m ago    Up 2m    0.0.0.0:9090->8080/tcp   es-mcp

# 健康检查
curl http://localhost:9090/ping
# 应返回 "Ready"（HTTP 200）

# 列出可用工具
curl -X POST http://localhost:9090/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
# 应返回包含 list_indices、search 等 5 个工具的 JSON

# 查看日志
docker logs -f es-mcp
```

##### 2.5 离线部署（使用 es-mcp.tar 镜像包）

适用于**服务器无法访问外网**（无法 clone 源码、无法 docker pull）的场景，只需一个打包好的镜像文件即可部署，**无需传源码**。

**在 Windows 开发机上导出镜像（仅需一次）：**

```powershell
# 查看本地已有镜像
docker images | findstr es-mcp

# 导出为 tar 包（含镜像层级，约 30-60MB）
docker save -o es-mcp.tar es-mcp-linux

# （可选）验证镜像架构，确保是 linux 平台
docker inspect es-mcp-linux --format "{{.Os}} {{.Architecture}}"
# 应输出：linux amd64（或 linux arm64，须与服务器架构一致）
```

**在 Linux 服务器上部署：**

```bash
# 1. 上传镜像包（Windows PowerShell 执行）
# scp es-mcp.tar user@your-server:/data/es-mcp/

# 2. 进入目录并加载镜像
cd /data/es-mcp
docker load -i es-mcp.tar

# 3. 确认镜像已加载
docker images | grep es-mcp

# 4. 创建 .env（用可用的 ES 凭据）
cat > .env << 'EOF'
ES_URL=http://117.91.17.82:9201
ES_USERNAME=your-username
ES_PASSWORD=your-password
# ES_API_KEY=your-api-key   # 或用 API Key（与用户名密码二选一）
EOF
chmod 600 .env

# 5. 启动容器
docker run --rm -d \
  --env-file .env \
  -p 9090:8080 \
  --name es-mcp \
  es-mcp-linux http

# 6. 验证
docker ps --filter name=es-mcp
curl http://localhost:9090/ping
```

**注意事项：**

- 镜像架构（amd64/arm64）必须与服务器 CPU 架构一致
- 加载前确保 Docker daemon 配置正常（见 1.2 节，`daemon.json` 不能禁用 iptables/bridge）
- `docker load` 只加载镜像，不含 .env 和容器配置，需手动创建

#### 三、方式二：源码编译部署

适用于**无 Docker 环境**或需要**定制化**的场景。

##### 3.1 编译

```bash
cd mcp-server-elasticsearch
cargo build --release
# 编译产物：./target/release/elasticsearch-core-mcp-server
# 二进制约 12MB（strip 后约 4MB，生产环境建议 strip）
strip ./target/release/elasticsearch-core-mcp-server
ls -lh ./target/release/elasticsearch-core-mcp-server
```

##### 3.2 配置环境变量

```bash
export ES_URL=http://your-es-cluster:9200
export ES_API_KEY=your-api-key
# 如需持久化，写入 /etc/profile.d/es-mcp.sh
```

##### 3.3 运行

```bash
# HTTP 模式
./target/release/elasticsearch-core-mcp-server http

# HTTP 模式 + 指定端口
./target/release/elasticsearch-core-mcp-server http --address 0.0.0.0:9090

# stdio 模式（管道通信，常用于客户端集成）
./target/release/elasticsearch-core-mcp-server stdio

# 使用配置文件
./target/release/elasticsearch-core-mcp-server http --config elastic-mcp.json5
```

#### 四、生产环境部署（systemd 服务）

##### 4.1 安装二进制

```bash
sudo cp ./target/release/elasticsearch-core-mcp-server /usr/local/bin/
```

##### 4.2 创建环境变量文件

```bash
sudo tee /etc/es-mcp.env > /dev/null << 'EOF'
ES_URL=http://117.91.17.82:9201
ES_USERNAME=eshore_wangzihao
ES_PASSWORDD=eshore8888
# ES_SSL_SKIP_VERIFY=false
# HTTP_ADDRESS=0.0.0.0:9090
EOF
sudo chmod 600 /etc/es-mcp.env  # 敏感文件，仅 root 可读
```

##### 4.3 创建 systemd 服务

```bash
sudo tee /etc/systemd/system/es-mcp.service > /dev/null << 'EOF'
[Unit]
Description=Elasticsearch MCP Server
Documentation=https://github.com/elastic/mcp-server-elasticsearch
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
EnvironmentFile=/etc/es-mcp.env
ExecStart=/usr/local/bin/elasticsearch-core-mcp-server http
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
LimitNOFILE=65536

# 安全配置
NoNewPrivileges=true
ProtectSystem=strict
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
```

##### 4.4 启动服务

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now es-mcp
sudo systemctl status es-mcp

# 查看实时日志
sudo journalctl -u es-mcp -f

# 查看最近 50 行日志
sudo journalctl -u es-mcp -n 50 --no-pager
```

####

```bash
# 1. 重新加载 systemd 配置
sudo systemctl daemon-reload

# 2. 重启代理服务
sudo systemctl restart es-mcp-proxy

# 3. 查看状态
sudo systemctl status es-mcp-proxy

# 4. 查看日志（确认无报错）
sudo journalctl -u es-mcp-proxy -n 30 --no-pager

# 5. 验证端口监听
sudo ss -tunlp | grep 8001
```

###

##### 6.2 容器操作

```bash
# 重启
docker restart es-mcp

# 更新（重新构建后）
docker rm -f es-mcp
docker build -t es-mcp .
docker run --rm -d --env-file .env -p 9090:8080 --name es-mcp es-mcp http

# 进入容器排查
docker exec -it es-mcp sh
```

##### 6.3 日志查看

```bash
# Docker
docker logs -f es-mcp            # 实时日志
docker logs --tail 100 es-mcp    # 最近 100 行

# systemd
sudo journalctl -u es-mcp -f
sudo journalctl -u es-mcp --since "10 min ago"
```

##### 6.4 性能与资源监控

```bash
# 容器资源占用
docker stats es-mcp --no-stream

# systemd 服务资源
sudo systemd-cgtop
```

#### 七、常见问题排查

| 问题                             | 原因                                                          | 解决                                        |
| ------------------------------ | ----------------------------------------------------------- | ----------------------------------------- |
| `PORTS` 列为空                    | `daemon.json` 中设置了 `"iptables": false` 或 `"bridge": "none"` | 删除这两项，重启 Docker                           |
| `docker0` 网桥不存在                | Docker 网络被禁用                                                | 同上方修复 daemon.json                         |
| 容器 IP 显示 `invalid`             | IPv4 转发未开启                                                  | `sysctl net.ipv4.ip_forward=1` 并重启 Docker |
| 连接 ES 超时                       | 容器内 localhost 指容器自身，不是宿主机                                   | 使用 `host.docker.internal` 或宿主机真实 IP       |
| `curl localhost:9090/ping` 无响应 | 绑定了 127.0.0.1 而不是 0.0.0.0                                   | 容器模式自动绑定 `0.0.0.0:8080`                   |
| 权限不足                           | API Key / 用户名密码错误                                           | 检查认证凭据和 ES 集群可达性                          |

***

## 6. 新人阅读源码的最佳顺序

| 步骤     | 文件                                        | 理由                                                                                   |
| ------ | ----------------------------------------- | ------------------------------------------------------------------------------------ |
| **1**  | `Cargo.toml`                              | 理解项目依赖全貌（rmcp、elasticsearch-rs、tokio、axum）                                           |
| **2**  | `cli.rs`                                  | 了解 CLI 接口：支持哪些子命令、哪些参数                                                               |
| **3**  | `bin/elasticsearch-core-mcp-server.rs`    | 程序真实入口：.env 加载、cli 解析、日志初始化                                                          |
| **4**  | `lib.rs`                                  | 核心编排逻辑：`setup_services()` 配置加载 + `run_stdio/run_http`                                |
| **5**  | `protocol/http.rs`                        | HTTP 传输层实现：路由注册、streamable-http 与 SSE 集成                                             |
| **6**  | `servers/elasticsearch/mod.rs`            | ES 客户端构造：`EsClientProvider` 认证代理、配置解析                                                |
| **7**  | **`servers/elasticsearch/base_tools.rs`** | **业务核心**：5 个 MCP Tool 的 `#[tool]` 注解方法 —— 先看 `list_indices`（最简单），再看 `search`（最复杂含聚合） |
| **8**  | `utils/interpolator.rs`                   | 配置插值实现，独立可读                                                                          |
| **9**  | `utils/rmcp_ext.rs`                       | `ServerProvider` 工厂模式封装                                                              |
| **10** | `tests/http_tests.rs`                     | 集成测试理解全链路如何串联                                                                        |

***

**一句话总结**：该项目是一个 **Rust 编写的 MCP 协议服务器**，充当 AI Agent 与 Elasticsearch 之间的桥梁，通过 **5 个 MCP Tool** 暴露 ES 的核心数据查询能力（索引列表、映射、搜索、ES|QL、分片信息），支持 **stdio** 和 **streamable-HTTP** 两种传输协议。
