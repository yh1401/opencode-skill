```
# 1. 上传代码到服务器（scp 或 git pull）
scp -r mcp-server-elasticsearch-main user@172.26.6.232:/home/user/

# 2. 服务器上构建镜像（Dockerfile-8000，国内网络友好）
cd /home/user/mcp-server-elasticsearch-main
docker build -f Dockerfile-8000 -t mcp-server-es:latest .

# 3. 替换两个容器（沿用原参数 + 新增超时变量）
docker rm -f es-mcp-gz4
docker run -d --restart unless-stopped --network host  \
   --name es-mcp-gz4 --env-file /home/fortress/es-mcp/gz4/.env \
  mcp-server-es:latest

docker rm -f es-mcp-sz3
docker run -d --restart unless-stopped --network host  \
   --name es-mcp-sz3 --env-file /home/fortress/es-mcp/sz3/.env \
  mcp-server-es:latest

# 4. 确认启动日志无报错
docker logs  es-mcp-gz4
docker logs  es-mcp-sz3
```