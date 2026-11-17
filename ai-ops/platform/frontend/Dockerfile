# 运行时镜像：dist/ 由 scripts/docker_up.sh 执行 npm run build 生成，
# 基础镜像使用本机已有的 nginx:alpine，构建过程不拉取任何远程镜像。
FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY dist /usr/share/nginx/html
EXPOSE 80
