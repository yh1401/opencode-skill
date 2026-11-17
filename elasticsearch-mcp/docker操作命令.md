# Docker 容器与镜像操作手册

> 适用环境：Windows + Docker Desktop（PowerShell 终端）
> 场景：es-mcp 容器/镜像的删除与重建

## 一、查看

```powershell
# 查看运行中的容器
docker ps

# 查看全部容器（含已停止）
docker ps -a

# 查看镜像列表
docker images

# 查看容器环境变量（重建时确认配置不遗漏）
docker inspect es-mcp --format '{{json .Config.Env}}'

# 查看容器网络模式与端口映射
docker inspect es-mcp --format '{{.HostConfig.NetworkMode}} {{.HostConfig.PortBindings}}'

# 查看容器重启策略
docker inspect es-mcp --format '{{.HostConfig.RestartPolicy.Name}}'
```

## 二、停止并删除容器

```powershell
# 正常停止（默认等待优雅退出，最长 10 秒）
docker stop es-mcp

# 容器不响应时：限制等待时间，超时自动强杀
docker stop -t 1 es-mcp

# 强制杀掉运行中的容器
docker kill es-mcp

# 删除容器（须先停止）
docker rm es-mcp

# 强制删除：停止 + 删除一步到位（即使运行中）
docker rm -f es-mcp
```

## 三、删除镜像

```powershell
# 删除指定镜像
docker rmi es-mcp-linux

# 镜像有多个 tag 时需带 tag 删除
docker rmi es-mcp-linux:latest

# 清理悬空镜像（无 tag 的废弃中间层）
docker image prune
```

## 四、常见报错及处理

| 报错 | 原因 | 处理 |
| --- | --- | --- |
| `cannot connect to the Docker daemon` | Docker Desktop 未启动 | 启动 Docker Desktop，托盘图标变绿后重试 |
| `image is being used by a running container` | 镜像被运行中容器占用 | 先 `docker rm -f` 占用容器，再 `docker rmi` |
| `container ... is running` | 容器未停止 | 用 `docker rm -f <容器名>` 强制删除 |
| `image is referenced in multiple repositories` | 镜像有多个 tag | 带 tag 删除：`docker rmi <name>:<tag>` |

> 注意区分命令对象：
> - `docker rmi` 删**镜像**，`docker rm` 删**容器**
> - `docker stop` 停容器，`docker kill` 强杀容器

## 五、重新部署 es-mcp 容器

```powershell
docker stop es-mcp
docker rm es-mcp

docker run -d `
  --name es-mcp `
  --network host `
  -e ES_URL=http://117.91.17.82:9201 `
  -e ES_USERNAME=xingye_test `
  -e ES_PASSWORD=YOUR_ES_PASSWORD `
  es-mcp-linux
```

说明：
- 环境变量、网络模式以 `docker inspect` 查到的原配置为准，勿遗漏
- `--network host` 为既定方案（解决容器访问 ES 的网络隔离问题）
- 若原容器带 `--restart always`，新容器按需加上；不需要自启动则省略

## 六、验证

```powershell
# 确认容器与镜像状态
docker ps -a
docker images

# 查看容器日志
docker logs -f es-mcp

# 健康检查（streamable-HTTP 模式）
curl http://localhost:8080/ping
```
