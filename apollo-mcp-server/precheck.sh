#!/bin/bash
# ================================================
# Apollo MCP Server - 生产环境预检脚本
# 使用方法: chmod +x precheck.sh && ./precheck.sh
# 作用: 部署前全面检查服务器环境是否满足要求
# ================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

PASS=0
WARN=0
FAIL=0

echo -e "${CYAN}╔══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║     Apollo MCP Server - 环境预检               ║${NC}"
echo -e "${CYAN}╚══════════════════════════════════════════════════╝${NC}"
echo ""

# ================================================
# 1. 系统信息
# ================================================
echo -e "${YELLOW}[1/7] 系统信息${NC}"
OS_INFO=$(cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d'"' -f2)
if [ -z "$OS_INFO" ]; then
    OS_PRODUCT=$(sw_vers -productName 2>/dev/null)
    OS_VERSION=$(sw_vers -productVersion 2>/dev/null)
    [ -n "$OS_PRODUCT" ] && OS_INFO="${OS_PRODUCT} ${OS_VERSION}"
fi
[ -z "$OS_INFO" ] && OS_INFO=$(uname -s)
echo "  OS:       ${OS_INFO}"
echo "  内核:     $(uname -r)"
echo "  主机名:   $(hostname)"
# 兼容 Linux (hostname -I) 和 macOS (ipconfig)
SERVER_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$SERVER_IP" ] && SERVER_IP=$(ipconfig getifaddr en0 2>/dev/null)
[ -z "$SERVER_IP" ] && SERVER_IP="未知"
echo "  服务器IP: ${SERVER_IP}"
echo ""

# ================================================
# 2. Docker 环境检查
# ================================================
echo -e "${YELLOW}[2/7] Docker 环境检查${NC}"

# Docker 是否安装
if command -v docker >/dev/null 2>&1; then
    echo -e "  ${GREEN}✅${NC} Docker 已安装: $(docker --version)"
    PASS=$((PASS+1))
else
    echo -e "  ${RED}❌${NC} Docker 未安装"
    echo -e "      安装方法: curl -fsSL https://get.docker.com | sh"
    FAIL=$((FAIL+1))
fi

# Docker 守护进程是否运行
if command -v docker >/dev/null 2>&1; then
    if docker info >/dev/null 2>&1; then
        echo -e "  ${GREEN}✅${NC} Docker 守护进程运行中"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} Docker 守护进程未运行"
        echo -e "      启动方法: sudo systemctl start docker"
        FAIL=$((FAIL+1))
    fi
fi

# Docker Compose
if docker compose version >/dev/null 2>&1; then
    echo -e "  ${GREEN}✅${NC} Docker Compose: $(docker compose version --short)"
    PASS=$((PASS+1))
elif command -v docker-compose >/dev/null 2>&1; then
    echo -e "  ${YELLOW}⚠️${NC}  旧版 docker-compose: $(docker-compose --version)"
    echo -e "      建议升级到 Docker Compose V2"
    WARN=$((WARN+1))
else
    echo -e "  ${RED}❌${NC} Docker Compose 未安装"
    echo -e "      安装方法: sudo apt-get install docker-compose-plugin"
    FAIL=$((FAIL+1))
fi

# Docker 镜像加速器
REGISTRY=$(docker info 2>/dev/null | grep "Registry Mirrors" -A1 | tail -1 | xargs)
if [ -n "$REGISTRY" ]; then
    echo -e "  ${GREEN}✅${NC} 镜像加速器: $REGISTRY"
else
    echo -e "  ${YELLOW}⚠️${NC}  未配置镜像加速器（拉取镜像可能较慢）"
    WARN=$((WARN+1))
fi

echo ""

# ================================================
# 3. 端口检查
# ================================================
echo -e "${YELLOW}[3/7] 端口检查${NC}"

check_port() {
    local port=$1
    local name=$2
    if command -v ss >/dev/null 2>&1; then
        if ss -tlnp | grep -q ":${port} " ; then
            echo -e "  ${RED}❌${NC} 端口 ${port} (${name}) 已被占用"
            ss -tlnp | grep ":${port} "
            FAIL=$((FAIL+1))
        else
            echo -e "  ${GREEN}✅${NC} 端口 ${port} (${name}) 可用"
            PASS=$((PASS+1))
        fi
    elif command -v netstat >/dev/null 2>&1; then
        if netstat -tlnp 2>/dev/null | grep -q ":${port} " ; then
            echo -e "  ${RED}❌${NC} 端口 ${port} (${name}) 已被占用"
            FAIL=$((FAIL+1))
        else
            echo -e "  ${GREEN}✅${NC} 端口 ${port} (${name}) 可用"
            PASS=$((PASS+1))
        fi
    else
        echo -e "  ${YELLOW}⚠️${NC}  无法检查端口（ss/netstat 未安装）"
        WARN=$((WARN+1))
    fi
}

check_port 8062 "MCP Server"
echo ""

# ================================================
# 4. 网络连通性检查
# ================================================
echo -e "${YELLOW}[4/7] 网络连通性检查${NC}"

# Apollo ConfigService
APOLLO_CONFIG_HOST="apollo-config.tech.ctseelink.cn"
APOLLO_CONFIG_PORT=8080
APOLLO_OPENAPI_PORT=8070

# DNS 解析
if command -v nslookup >/dev/null 2>&1 || command -v dig >/dev/null 2>&1; then
    if nslookup ${APOLLO_CONFIG_HOST} >/dev/null 2>&1 || dig ${APOLLO_CONFIG_HOST} >/dev/null 2>&1; then
        echo -e "  ${GREEN}✅${NC} DNS 解析正常: ${APOLLO_CONFIG_HOST}"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} DNS 解析失败: ${APOLLO_CONFIG_HOST}"
        echo -e "      请检查 /etc/hosts 或内网 DNS 配置"
        FAIL=$((FAIL+1))
    fi
else
    echo -e "  ${YELLOW}⚠️${NC}  nslookup/dig 未安装，跳过 DNS 检查"
    WARN=$((WARN+1))
fi

# ConfigService 连通性 (8080)
if command -v curl >/dev/null 2>&1; then
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "http://${APOLLO_CONFIG_HOST}:${APOLLO_CONFIG_PORT}" 2>/dev/null)
    if [ "$HTTP_CODE" != "000" ] && [ -n "$HTTP_CODE" ]; then
        echo -e "  ${GREEN}✅${NC} Apollo ConfigService (${APOLLO_CONFIG_HOST}:${APOLLO_CONFIG_PORT}) 可达 (HTTP ${HTTP_CODE})"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} Apollo ConfigService (${APOLLO_CONFIG_HOST}:${APOLLO_CONFIG_PORT}) 不可达"
        echo -e "      请检查网络/防火墙规则"
        FAIL=$((FAIL+1))
    fi

    # OpenAPI 连通性 (8070)
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "http://${APOLLO_CONFIG_HOST}:${APOLLO_OPENAPI_PORT}" 2>/dev/null)
    if [ "$HTTP_CODE" != "000" ] && [ -n "$HTTP_CODE" ]; then
        echo -e "  ${GREEN}✅${NC} Apollo OpenAPI (${APOLLO_CONFIG_HOST}:${APOLLO_OPENAPI_PORT}) 可达 (HTTP ${HTTP_CODE})"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} Apollo OpenAPI (${APOLLO_CONFIG_HOST}:${APOLLO_OPENAPI_PORT}) 不可达"
        echo -e "      请检查网络/防火墙规则"
        FAIL=$((FAIL+1))
    fi
else
    echo -e "  ${RED}❌${NC} curl 未安装，无法检查网络连通性"
    FAIL=$((FAIL+1))
fi

# 外网连通性（拉取 Docker 镜像用）
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "https://registry-1.docker.io" 2>/dev/null)
if [ "$HTTP_CODE" != "000" ] && [ -n "$HTTP_CODE" ]; then
    echo -e "  ${GREEN}✅${NC} Docker Hub 可达 (拉取 python:3.11-slim)"
    PASS=$((PASS+1))
else
    echo -e "  ${YELLOW}⚠️${NC}  Docker Hub 不可达，需配置镜像加速器"
    WARN=$((WARN+1))
fi

echo ""

# ================================================
# 5. 磁盘空间检查
# ================================================
echo -e "${YELLOW}[5/7] 磁盘空间检查${NC}"

DISK_INFO=$(df -h / 2>/dev/null | tail -1)
DISK_AVAIL=$(echo "$DISK_INFO" | awk '{print $4}')
DISK_USE_PCT=$(echo "$DISK_INFO" | awk '{print $5}' | tr -d '%')

if [ -n "$DISK_USE_PCT" ]; then
    echo "  根分区: 总量 $(echo $DISK_INFO | awk '{print $2}'), 已用 ${DISK_USE_PCT}%, 可用 ${DISK_AVAIL}"
    if [ "$DISK_USE_PCT" -lt 80 ]; then
        echo -e "  ${GREEN}✅${NC} 磁盘空间充足"
        PASS=$((PASS+1))
    elif [ "$DISK_USE_PCT" -lt 90 ]; then
        echo -e "  ${YELLOW}⚠️${NC}  磁盘空间偏低 (${DISK_USE_PCT}%)"
        WARN=$((WARN+1))
    else
        echo -e "  ${RED}❌${NC} 磁盘空间不足 (${DISK_USE_PCT}%)"
        echo -e "      Docker 镜像+日志至少需要 1GB 可用空间"
        FAIL=$((FAIL+1))
    fi
fi

echo ""

# ================================================
# 6. 系统资源检查
# ================================================
echo -e "${YELLOW}[6/7] 系统资源检查${NC}"

# 内存（兼容 Linux free 和 macOS vm_stat）
MEM_AVAIL=""
if command -v free >/dev/null 2>&1; then
    MEM_INFO=$(free -m 2>/dev/null | grep Mem)
    if [ -n "$MEM_INFO" ]; then
        MEM_TOTAL=$(echo "$MEM_INFO" | awk '{print $2}')
        MEM_AVAIL=$(echo "$MEM_INFO" | awk '{print $7}')
        echo "  内存: 总量 ${MEM_TOTAL}MB, 可用 ${MEM_AVAIL}MB"
    fi
else
    # macOS: 用 sysctl 获取内存
    MEM_TOTAL=$(sysctl -n hw.memsize 2>/dev/null)
    if [ -n "$MEM_TOTAL" ]; then
        MEM_TOTAL=$((MEM_TOTAL / 1024 / 1024))
        # macOS 可用内存用 vm_stat 粗略估算
        PAGE_SIZE=$(sysctl -n hw.pagesize 2>/dev/null || echo 4096)
        FREE_PAGES=$(vm_stat 2>/dev/null | grep "free:" | awk '{print $3}' | tr -d '.')
        INACTIVE_PAGES=$(vm_stat 2>/dev/null | grep "inactive:" | awk '{print $3}' | tr -d '.')
        if [ -n "$FREE_PAGES" ] && [ -n "$INACTIVE_PAGES" ]; then
            MEM_AVAIL=$(( (FREE_PAGES + INACTIVE_PAGES) * PAGE_SIZE / 1024 / 1024 ))
        fi
        echo "  内存: 总量 ${MEM_TOTAL}MB, 可用约 ${MEM_AVAIL}MB"
    fi
fi
if [ -n "$MEM_AVAIL" ]; then
    if [ "$MEM_AVAIL" -gt 256 ]; then
        echo -e "  ${GREEN}✅${NC} 内存充足 (需要 >256MB)"
        PASS=$((PASS+1))
    else
        echo -e "  ${YELLOW}⚠️${NC}  内存偏低，可能影响服务稳定性"
        WARN=$((WARN+1))
    fi
fi

# CPU（兼容 Linux nproc 和 macOS sysctl）
CPU_CORES=$(nproc 2>/dev/null)
[ -z "$CPU_CORES" ] && CPU_CORES=$(sysctl -n hw.logicalcpu 2>/dev/null)
[ -z "$CPU_CORES" ] && CPU_CORES=$(grep -c ^processor /proc/cpuinfo 2>/dev/null)
if [ -n "$CPU_CORES" ]; then
    echo "  CPU: ${CPU_CORES} 核"
    if [ "$CPU_CORES" -ge 2 ]; then
        echo -e "  ${GREEN}✅${NC} CPU 核数充足"
        PASS=$((PASS+1))
    else
        echo -e "  ${YELLOW}⚠️${NC}  仅 1 核 CPU，高并发场景可能性能不足"
        WARN=$((WARN+1))
    fi
fi

echo ""

# ================================================
# 7. 常用工具检查
# ================================================
echo -e "${YELLOW}[7/7] 常用工具检查${NC}"

for tool in curl git wget; do
    if command -v $tool >/dev/null 2>&1; then
        echo -e "  ${GREEN}✅${NC} ${tool}: $(command -v $tool)"
        PASS=$((PASS+1))
    else
        echo -e "  ${YELLOW}⚠️${NC}  ${tool} 未安装"
        WARN=$((WARN+1))
    fi
done

echo ""

# ================================================
# 汇总
# ================================================
echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
echo -e "  ${GREEN}通过: ${PASS}${NC}  ${YELLOW}警告: ${WARN}${NC}  ${RED}失败: ${FAIL}${NC}"
echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
echo ""

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}❌ 环境检查未通过，请修复上述失败项后再部署${NC}"
    echo ""
    echo "常见修复方法:"
    echo "  1. 安装 Docker:     curl -fsSL https://get.docker.com | sh"
    echo "  2. 启动 Docker:     sudo systemctl start docker"
    echo "  3. 安装 Compose:    sudo apt-get install docker-compose-plugin"
    echo "  4. 开放端口:         sudo firewall-cmd --add-port=8062/tcp --permanent"
    echo "  5. 配置内网 DNS:     检查 /etc/resolv.conf 或 /etc/hosts"
    exit 1
elif [ "$WARN" -gt 0 ]; then
    echo -e "${YELLOW}⚠️  环境检查通过（有 ${WARN} 个警告）${NC}"
    echo -e "  警告项不影响部署，但建议关注"
    echo ""
    echo -e "${GREEN}可以执行部署: ./deploy-prod.sh${NC}"
    exit 0
else
    echo -e "${GREEN}✅ 环境检查全部通过！${NC}"
    echo ""
    echo -e "可以执行部署: ${CYAN}./deploy-prod.sh${NC}"
    exit 0
fi
