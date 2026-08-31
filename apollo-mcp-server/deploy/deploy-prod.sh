#!/bin/bash
# ================================================
# Apollo MCP Server - 生产/测试环境一键部署脚本
# 使用方法: chmod +x deploy-prod.sh && ./deploy-prod.sh
# 支持 Docker Compose V1(docker-compose) / V2(docker compose)
# ================================================
set -e

# 切换到脚本所在目录（deploy/），保证 .env 和 docker-compose.yml 定位正确
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# 1. 检测 Compose 命令（V1/V2 兼容）
if docker compose version >/dev/null 2>&1; then
    DC="docker compose"
    echo "[Compose V2] $(docker compose version | head -1)"
elif command -v docker-compose >/dev/null 2>&1; then
    DC="docker-compose"
    echo "[Compose V1] $(docker-compose --version)"
else
    echo "错误: 未检测到 Docker Compose"
    echo "安装方式: yum install -y docker-compose-plugin  (或 apt install -y docker-compose-v2)"
    exit 1
fi

# 2. 配置 .env（首次部署自动生成；已存在时做安全 merge——保留现有值，补入 .env.example 新增变量）
#    这样 .env.example 新增的配置项（如 APOLLO_GLOBAL_SEARCH_*）会自动补到服务器 .env，
#    但服务器上已改过的值（如 APOLLO_REQUEST_TIMEOUT=8）不会被覆盖
if [ ! -f .env ]; then
    echo "[2/5] 初始化 .env（首次部署）"
    if [ -f ../.env.example ]; then
        cp ../.env.example .env
    else
        cat > .env << 'EOF'
MCP_USE_MOCK=false
APOLLO_ENV=PRO
# 推荐方式：第三方「Apollo Host 信息查询接口」（默认值已内置在代码中，无需配置）
# APOLLO_HOST_API_BASE=https://easyops.tech.ctseelink.cn
# APOLLO_HOST_SESSION_ID=e5e27a7d1805758400287ae86741f889
LOG_LEVEL=INFO
LOG_TO_FILE=false
EOF
    fi
    chmod 600 .env
    echo "✅ .env 已生成"
else
    echo "[2/5] 检查 .env 更新（merge .env.example 新增变量）"
    if [ -f ../.env.example ]; then
        ENV_BAK=".env.bak.$(date +%Y%m%d%H%M%S)"
        cp .env "$ENV_BAK"
        # 遍历 .env.example，提取 KEY=value 格式的变量（包括注释掉的默认值 # KEY=value），
        # 如果 .env 里没有就追加——新增变量自动补进服务器 .env，已有值保持不变
        ADDED=0
        while IFS= read -r line; do
            # 只处理含 "=" 且等号左边是合法 KEY 格式的行
            # 合法 KEY: 以字母/数字/下划线开头，只含字母/数字/下划线/点号
            if echo "$line" | grep -qE '^[[:space:]]*#?[[:space:]]*[A-Za-z_][A-Za-z0-9_.]*='; then
                # 提取 key（去掉 # 注释符和空格）
                key=$(echo "$line" | sed 's/^[[:space:]]*#*[[:space:]]*//' | cut -d'=' -f1 | sed 's/[[:space:]]*$//')
                [ -z "$key" ] && continue
                # 检查 .env 是否已有这个 key
                if ! grep -Eq "^[[:space:]]*${key}=" .env 2>/dev/null; then
                    if echo "$line" | grep -qE '^[[:space:]]*#'; then
                        echo "# [NEW] ${key} 默认值已追加（如需启用去掉注释#即可）" >> .env
                        echo "$line" >> .env
                    else
                        echo "$line" >> .env
                    fi
                    ADDED=$((ADDED + 1))
                    echo "  + 新增: $key"
                fi
            fi
        done < ../.env.example

        if [ "$ADDED" -gt 0 ]; then
            echo "✅ merge 完成：新增 $ADDED 个变量（已备份到 $ENV_BAK）"
            chmod 600 .env
        else
            echo "ℹ️  .env 已是最新，无需更新"
            rm -f "$ENV_BAK"
        fi
    fi
fi

# 校验：第三方接口 sessionId 未配置时给出提示（不再阻断，代码内置默认第三方接口）
ENV_SESSION_ID=$(grep -E '^\s*APOLLO_HOST_SESSION_ID=' .env 2>/dev/null | head -1 | cut -d'=' -f2- | tr -d '"' | tr -d "'" | xargs)
if [ -z "$ENV_SESSION_ID" ]; then
    echo "ℹ️  未配置第三方接口 sessionId，使用代码内置默认第三方接口（https://easyops.tech.ctseelink.cn）"
fi

# 3. 构建与启动（禁用 BuildKit 避免 rpc EOF；先停旧容器保证干净）
echo "[3/5] 构建并启动服务"
DOCKER_BUILDKIT=0 $DC down 2>/dev/null || true
DOCKER_BUILDKIT=0 $DC up -d --build

# 4. 等待启动并查看日志
echo "[4/5] 等待服务启动"
sleep 3
echo "--- 启动日志 ---"
$DC logs --tail=50 apollo-mcp || true

# 5. 健康检查（轮询等待：容器启动需同步拉取第三方接口，可能耗时 10s+）
echo "[5/5] 健康检查"
HEALTH="000"
for i in $(seq 1 24); do
    HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 3 http://localhost:8062/health || echo "000")
    if [ "$HEALTH" = "200" ]; then
        break
    fi
    echo "  等待服务就绪... (${i}/24) HTTP=$HEALTH"
    sleep 5
done

if [ "$HEALTH" = "200" ]; then
    IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    echo ""
    echo "✅ 部署成功！"
    echo ""
    echo "  服务地址: http://${IP:-<服务器IP>}:8062"
    echo "  MCP 端点: http://${IP:-<服务器IP>}:8062/mcp"
    echo "  健康检查: http://localhost:8062/health"
    echo ""
    echo "下一步:"
    echo "  1. 查看日志: $DC logs -f apollo-mcp"
    echo "  2. 注册 MCP: 在 StarAgent 平台填写服务地址"
    echo "  3. 绑定 Skill: 将 apollo-config-query 技能绑定到此 MCP"
else
    echo "❌ 健康检查失败 (HTTP $HEALTH)"
    echo "查看日志排查:"
    echo "  $DC logs --tail=100 apollo-mcp"
    echo "  或进入容器: $DC exec apollo-mcp sh"
    echo "  或先验证第三方接口连通性:"
    echo "  curl -m 10 -b \"sessionId=e5e27a7d1805758400287ae86741f889\" https://easyops.tech.ctseelink.cn/thirdApi/getApolloHostInfo"
    exit 1
fi
