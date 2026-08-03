#!/bin/bash
# ──────────────────────────────────────────────────────────────────────
# Skill 打包脚本 v3.0 - 一键打包所有 Skill + 增量打包 + 自动日期存档 + 详细统计
# ──────────────────────────────────────────────────────────────────────
# 用法:
#   bash packages/build.sh                   打包所有 skill（增量模式）
#   bash packages/build.sh ops-data-query    只打包指定 skill
#   bash packages/build.sh log-analyzer-skill log-analyzer-flash  打包多个
#   bash packages/build.sh --force           强制重新打包所有 skill
#   bash packages/build.sh --help            显示帮助
#   bash packages/build.sh --list            列出所有可打包的 skill
# ──────────────────────────────────────────────────────────────────────

set -e

# ── 颜色定义 ──
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m'

# ── 配置参数 ──
VERSION="3.0"
DATE_TAG=$(date +%Y-%m-%d)
TIME_TAG=$(date +%H%M%S)
PACKAGES_DIR="packages"
SKIP_DIRS=("__pycache__" ".git" "node_modules" ".DS_Store")
SKIP_FILES=("*.pyc" "*.pyo" "*.log" "*.tmp" "*.swp" "*~")

# ── 需要打包的 skill 列表 ──
ALL_SKILLS=(
  "log-analyzer"
  "log-analyzer-flash"
  "log-analyzer-skill"
  "ops-data-query"
)

# ── 帮助信息 ──
show_help() {
  echo -e "${CYAN}Skill 打包脚本 v${VERSION}${NC}"
  echo "─────────────────────────────────────────────────────────────"
  echo "用法:"
  echo "  bash packages/build.sh [选项] [skill名称...]"
  echo ""
  echo "选项:"
  echo "  --help              显示帮助信息"
  echo "  --list              列出所有可打包的 skill"
  echo "  --clean             清理所有打包产物（保留脚本）"
  echo "  --verbose           显示详细打包过程"
  echo "  --force             强制重新打包所有 skill（忽略增量检查）"
  echo ""
  echo "示例:"
  echo "  bash packages/build.sh                   # 打包所有 skill（增量模式）"
  echo "  bash packages/build.sh ops-data-query    # 只打包指定 skill"
  echo "  bash packages/build.sh --force           # 强制重新打包所有"
  echo ""
  echo -e "${YELLOW}可打包的 skill 列表:${NC}"
  for skill in "${ALL_SKILLS[@]}"; do
    if [ -d "$skill" ]; then
      echo -e "  ${GREEN}✓${NC} $skill"
    else
      echo -e "  ${RED}✗${NC} $skill (目录不存在)"
    fi
  done
  exit 0
}

# ── 列出所有 skill ──
list_skills() {
  echo -e "${CYAN}可打包的 Skill 列表${NC}"
  echo "─────────────────────────────────────────────────────────────"
  for skill in "${ALL_SKILLS[@]}"; do
    if [ -d "$skill" ]; then
      SIZE=$(du -sh "$skill" 2>/dev/null | cut -f1)
      SKILL_FILES=$(find "$skill" -type f 2>/dev/null | wc -l)
      echo -e "  ${GREEN}✓${NC} $skill"
      echo -e "     大小: ${BLUE}${SIZE}${NC} | 文件数: ${BLUE}${SKILL_FILES}${NC}"
    else
      echo -e "  ${RED}✗${NC} $skill (目录不存在)"
    fi
  done
  exit 0
}

# ── 清理打包产物（保留脚本）──
clean_packages() {
  echo -e "${YELLOW}清理打包产物...${NC}"
  for skill in "${ALL_SKILLS[@]}"; do
    SKILL_DIR="$PACKAGES_DIR/$skill"
    if [ -d "$SKILL_DIR" ]; then
      rm -rf "$SKILL_DIR"
      echo -e "  ${GREEN}✓${NC} 已清理: ${SKILL_DIR}"
    fi
  done
  echo -e "${GREEN}✅ 清理完成${NC}"
  exit 0
}

# ── 计算目录哈希值（排除指定文件/目录）──
calculate_hash() {
  local skill_dir="$1"
  local skip_dirs="$2"
  local skip_files="$3"

  local find_cmd="find \"$skill_dir\" -type f"
  
  for dir in ${skip_dirs[@]}; do
    find_cmd="$find_cmd ! -path \"*/$dir/*\""
  done
  
  for file in ${skip_files[@]}; do
    find_cmd="$find_cmd ! -name \"$file\""
  done

  local hash=$(eval "$find_cmd" | sort | xargs md5sum 2>/dev/null | md5sum | awk '{print $1}')
  echo "$hash"
}

# ── 主程序 ──
main() {
  cd "$(dirname "$0")/.." || exit 1

  # ── 解析命令行参数 ──
  VERBOSE=false
  FORCE=false
  SKILLS_TO_BUILD=()

  for arg in "$@"; do
    case "$arg" in
      --help) show_help ;;
      --list) list_skills ;;
      --clean) clean_packages ;;
      --verbose) VERBOSE=true ;;
      --force) FORCE=true ;;
      *) SKILLS_TO_BUILD+=("$arg") ;;
    esac
  done

  # ── 确定要打包的 skill ──
  if [ ${#SKILLS_TO_BUILD[@]} -eq 0 ]; then
    SKILLS_TO_BUILD=("${ALL_SKILLS[@]}")
  fi

  # ── 初始化统计变量 ──
  SUCCESS_COUNT=0
  SKIP_COUNT=0
  NO_CHANGE_COUNT=0
  FAIL_COUNT=0
  TOTAL_SIZE=0

  # ── 生成忽略参数 ──
  EXCLUDE_PATTERNS=""
  for dir in "${SKIP_DIRS[@]}"; do
    EXCLUDE_PATTERNS="$EXCLUDE_PATTERNS -x */${dir}/* -x ${dir}"
  done
  for file in "${SKIP_FILES[@]}"; do
    EXCLUDE_PATTERNS="$EXCLUDE_PATTERNS -x ${file}"
  done

  # ── 输出标题 ──
  echo -e "${PURPLE}==========================================${NC}"
  echo -e "${CYAN}  Skill 打包脚本 v${VERSION}${NC}"
  echo -e "${CYAN}  日期: ${DATE_TAG} ${TIME_TAG}${NC}"
  echo -e "${CYAN}  目标: ${#SKILLS_TO_BUILD[@]} 个 skill${NC}"
  echo -e "${CYAN}  模式: ${FORCE:+强制打包}${FORCE:-增量打包}"
  echo -e "${PURPLE}==========================================${NC}"

  # ── 开始打包 ──
  for skill in "${SKILLS_TO_BUILD[@]}"; do
    echo ""
    echo -e "${BLUE}▶ 处理: ${skill}${NC}"

    # 检查目录是否存在
    if [ ! -d "$skill" ]; then
      echo -e "  ${YELLOW}⚠ 跳过: 目录不存在${NC}"
      SKIP_COUNT=$((SKIP_COUNT + 1))
      continue
    fi

    # 检查是否为有效的 skill（包含 SKILL.md）
    if [ ! -f "$skill/SKILL.md" ]; then
      echo -e "  ${YELLOW}⚠ 跳过: 不是有效的 skill (缺少 SKILL.md)${NC}"
      SKIP_COUNT=$((SKIP_COUNT + 1))
      continue
    fi

    # 准备输出目录
    SKILL_DIR="$PACKAGES_DIR/$skill"
    ZIP_NAME="${skill}.zip"
    ZIP_PATH="${SKILL_DIR}/${ZIP_NAME}"
    HASH_FILE="${SKILL_DIR}/.last_hash"
    mkdir -p "$SKILL_DIR"

    # ── 增量检查（非强制模式）──
    if [ "$FORCE" = false ]; then
      current_hash=$(calculate_hash "$skill" "${SKIP_DIRS[*]}" "${SKIP_FILES[*]}")
      
      if [ -f "$HASH_FILE" ]; then
        last_hash=$(cat "$HASH_FILE")
        if [ "$current_hash" = "$last_hash" ]; then
          SIZE=$(du -h "$ZIP_PATH" 2>/dev/null | cut -f1)
          echo -e "  ${GREEN}✓ 跳过: 代码无变化${NC}${SIZE:+ (当前包: ${SIZE})}"
          NO_CHANGE_COUNT=$((NO_CHANGE_COUNT + 1))
          if [ -f "$ZIP_PATH" ]; then
            SIZE_BYTES=$(stat -f%z "$ZIP_PATH" 2>/dev/null || stat -c%s "$ZIP_PATH" 2>/dev/null || echo 0)
            TOTAL_SIZE=$((TOTAL_SIZE + SIZE_BYTES))
          fi
          continue
        fi
      fi
    fi

    # 归档旧包
    if [ -f "$ZIP_PATH" ]; then
      ARCHIVE_NAME="${skill}.${DATE_TAG}-${TIME_TAG}.zip"
      mv "$ZIP_PATH" "${SKILL_DIR}/${ARCHIVE_NAME}"
      echo -e "  ${YELLOW}📦 旧包已存档: ${ARCHIVE_NAME}${NC}"
    fi

    # 打包（显示进度）
    if $VERBOSE; then
      echo -e "  ${CYAN}📤 正在压缩...${NC}"
      zip -r "$ZIP_PATH" "$skill/" $EXCLUDE_PATTERNS
    else
      zip -r "$ZIP_PATH" "$skill/" $EXCLUDE_PATTERNS > /dev/null
    fi

    # 检查打包结果
    if [ $? -eq 0 ]; then
      # 更新哈希文件
      current_hash=$(calculate_hash "$skill" "${SKIP_DIRS[*]}" "${SKIP_FILES[*]}")
      echo "$current_hash" > "$HASH_FILE"

      SIZE=$(du -h "$ZIP_PATH" | cut -f1)
      SIZE_BYTES=$(stat -f%z "$ZIP_PATH" 2>/dev/null || stat -c%s "$ZIP_PATH" 2>/dev/null || echo 0)
      TOTAL_SIZE=$((TOTAL_SIZE + SIZE_BYTES))
      echo -e "  ${GREEN}✅ 新包已生成: ${ZIP_NAME} (${SIZE})${NC}"
      SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    else
      echo -e "  ${RED}❌ 打包失败${NC}"
      FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
  done

  # ── 输出统计 ──
  echo ""
  echo -e "${PURPLE}==========================================${NC}"
  echo -e "${CYAN}  打包完成!${NC}"
  echo -e "${PURPLE}==========================================${NC}"
  echo ""
  echo -e "${GREEN}✓ 成功: ${SUCCESS_COUNT}${NC}"
  echo -e "${BLUE}◎ 无变化: ${NO_CHANGE_COUNT}${NC}"
  echo -e "${YELLOW}⚠ 跳过: ${SKIP_COUNT}${NC}"
  echo -e "${RED}✗ 失败: ${FAIL_COUNT}${NC}"

  if [ $TOTAL_SIZE -gt 0 ]; then
    TOTAL_HUMAN=$(python3 -c "import sys; s=int(sys.argv[1]); u=['B','KB','MB','GB']; i=0; while s>=1024 and i<3: s/=1024; i+=1; print(f'{s:.1f}{u[i]}')" "$TOTAL_SIZE" 2>/dev/null || echo "${TOTAL_SIZE}B")
    echo -e "${BLUE}📦 总大小: ${TOTAL_HUMAN}${NC}"
  fi

  echo -e "${BLUE}📁 输出目录: $(cd "$PACKAGES_DIR" && pwd)${NC}"
  echo ""

  # 显示打包产物列表
  if [ -d "$PACKAGES_DIR" ]; then
    echo -e "${CYAN}📋 打包产物:${NC}"
    find "$PACKAGES_DIR" -name "*.zip" | sort | while read -r zipfile; do
      SIZE=$(du -h "$zipfile" | cut -f1)
      echo -e "  ${GREEN}✓${NC} ${zipfile} (${SIZE})"
    done
  fi

  echo ""
  echo -e "${YELLOW}💡 提示: 使用 --clean 清理旧包，使用 --force 强制重新打包${NC}"
}

# ── 执行主程序 ──
main "$@"