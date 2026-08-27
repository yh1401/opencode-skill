#!/bin/bash
# ================================================
# cmdb-mcp-server 一键更新脚本（保留 .env 配置）
# 自动完成: 解压新包 → 保留 deploy/.env → 构建 → 健康检查
#
# 用法（服务器上）:
#   1) cd /data/mcp/cmdb-mcp            # 进入项目根目录
#   2) rz -bey                          # 上传安装包
#   3) ./deploy/update-prod.sh          # 一键更新（自动取最新的 .tar.gz；也可指定包名）
# ================================================
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$( cd "$SCRIPT_DIR/.." && pwd )"
cd "$PROJECT_DIR"

# 1. 定位安装包（默认取最新的 .tar.gz）
PKG="${1:-$(ls -t ./*.tar.gz 2>/dev/null | head -1)}"
if [ -z "$PKG" ]; then
    echo "❌ 未找到安装包，请先 rz -bey 上传"
    exit 1
fi
if [ ! -f "$PKG" ]; then
    echo "❌ 安装包不存在: $PKG"
    exit 1
fi
echo "[1/3] 解压安装包: $PKG"

# 2. 备份并清空旧文件（只删包内管理的目录，避免误删部署后生成的目录）
ENV_BAK=/tmp/cmdb-mcp-env.bak
[ -f deploy/.env ] && cp deploy/.env "$ENV_BAK"
rm -rf scripts deploy docs config mock requirements.txt .env.example Dockerfile docker-compose.yml
tar xzf "$PKG"

# 3. 恢复配置（无备份则从模板生成）
if [ -f "$ENV_BAK" ]; then
    cp "$ENV_BAK" deploy/.env
    echo "  已保留原有 deploy/.env 配置"
else
    cp .env.example deploy/.env
    echo "  首次部署，已从 .env.example 生成 deploy/.env"
fi

# 4. 构建启动 + 健康检查
echo "[2/3] 执行部署脚本"
cd deploy
chmod +x deploy-prod.sh
./deploy-prod.sh

echo "[3/3] 更新完成"
