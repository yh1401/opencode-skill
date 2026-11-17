# 运行时镜像：二进制与时区数据由 scripts/docker_up.sh 在宿主机预先生成到 backend/build/，
# 基础镜像使用本机已有的 alpine，构建过程不拉取任何远程镜像（离线可构建）。
# 镜像内同时包含平台后端 aiops-server 与 Mock MCP 上游 mockmcp，compose 用 command 区分。
FROM alpine:latest
COPY build/zoneinfo /usr/share/zoneinfo
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY build/aiops-server /app/aiops-server
COPY build/mockmcp /app/mockmcp
COPY mockdata /app/mockdata
EXPOSE 8080 8888 8090 8061 9201
CMD ["/app/aiops-server"]
