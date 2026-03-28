#!/usr/bin/env bash
# setup-control-plane.sh
# 一鍵準備 sbctl Control Plane 前置環境：
#   1. 檢查 Docker / Go 是否存在
#   2. 啟動 Control Plane 專用 PostgreSQL container
#   3. 套用 migration（建立 projects / project_configs / project_overrides 表）
#   4. 建置 sbctl binary
#   5. 輸出環境變數設定說明
#
# 使用方式：
#   bash scripts/setup-control-plane.sh
#   bash scripts/setup-control-plane.sh --db-port 5433 --db-password mysecret
set -euo pipefail

# ── 預設值 ────────────────────────────────────────────────────────────────────

CONTAINER_NAME="sbctl-db"
DB_PORT=5433
DB_NAME="sbctl"
DB_USER="postgres"
DB_PASSWORD="sbctl_secret"
PROJECTS_DIR="$(pwd)/projects"
BINARY_DIR="$(pwd)"

BOLD="\033[1m"
GREEN="\033[0;32m"
YELLOW="\033[0;33m"
RED="\033[0;31m"
RESET="\033[0m"

# ── 說明 ──────────────────────────────────────────────────────────────────────

print_usage() {
  cat <<'EOF'
Usage:
  bash scripts/setup-control-plane.sh [options]

Options:
  --db-port <port>       Host port for Control Plane PostgreSQL (default: 5433)
  --db-password <pass>   PostgreSQL password (default: sbctl_secret)
  --db-name <name>       Database name (default: sbctl)
  --projects-dir <path>  Directory to store project files (default: ./projects)
  --no-build             Skip building the sbctl binary
  --reset-db             Drop and recreate the container (WARNING: deletes all data)
  --help, -h             Show this help
EOF
}

# ── 引數解析 ──────────────────────────────────────────────────────────────────

NO_BUILD=false
RESET_DB=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --db-port)        DB_PORT="${2:-}";       shift 2 ;;
    --db-password)    DB_PASSWORD="${2:-}";   shift 2 ;;
    --db-name)        DB_NAME="${2:-}";       shift 2 ;;
    --projects-dir)   PROJECTS_DIR="${2:-}";  shift 2 ;;
    --no-build)       NO_BUILD=true;          shift   ;;
    --reset-db)       RESET_DB=true;          shift   ;;
    --help|-h)        print_usage; exit 0              ;;
    *)
      echo -e "${RED}Unknown option: $1${RESET}" >&2
      print_usage >&2
      exit 1
      ;;
  esac
done

DB_URL="postgresql://${DB_USER}:${DB_PASSWORD}@localhost:${DB_PORT}/${DB_NAME}"

# ── 工具函式 ──────────────────────────────────────────────────────────────────

log()     { echo -e "${BOLD}==> $*${RESET}"; }
success() { echo -e "${GREEN}✔  $*${RESET}"; }
warn()    { echo -e "${YELLOW}⚠  $*${RESET}"; }
die()     { echo -e "${RED}✘  $*${RESET}" >&2; exit 1; }

require_cmd() {
  command -v "$1" &>/dev/null || die "Required command not found: $1. Please install it first."
}

wait_for_postgres() {
  local dsn="$1"
  local retries=30
  local i=0
  log "等待 PostgreSQL 就緒..."
  while ! docker exec "${CONTAINER_NAME}" pg_isready -U "${DB_USER}" -q 2>/dev/null; do
    i=$((i + 1))
    if [[ $i -ge $retries ]]; then
      die "PostgreSQL 超時未就緒（${retries} 秒）。請執行 docker logs ${CONTAINER_NAME} 確認狀態。"
    fi
    sleep 1
  done
  success "PostgreSQL 已就緒"
}

# ── 步驟 1：檢查必要工具 ──────────────────────────────────────────────────────

log "步驟 1/4：檢查必要工具"

require_cmd docker

if ! $NO_BUILD; then
  require_cmd go
fi

# 確認 Docker daemon 在跑
if ! docker info &>/dev/null; then
  die "Docker daemon 未啟動。請先開啟 Docker Desktop 或啟動 Docker daemon。"
fi

success "必要工具確認完成"

# ── 步驟 2：啟動 Control Plane PostgreSQL ─────────────────────────────────────

log "步驟 2/4：準備 Control Plane PostgreSQL（container: ${CONTAINER_NAME}，port: ${DB_PORT}）"

# 如果指定 --reset-db，先移除舊 container
if $RESET_DB; then
  warn "--reset-db 指定：移除舊 container（${CONTAINER_NAME}）..."
  docker rm -f "${CONTAINER_NAME}" 2>/dev/null || true
fi

if docker inspect "${CONTAINER_NAME}" &>/dev/null; then
  STATUS="$(docker inspect -f '{{.State.Status}}' "${CONTAINER_NAME}")"
  if [[ "$STATUS" == "running" ]]; then
    success "Container ${CONTAINER_NAME} 已在執行中，跳過建立"
  else
    warn "Container ${CONTAINER_NAME} 存在但狀態為 ${STATUS}，正在啟動..."
    docker start "${CONTAINER_NAME}"
  fi
else
  log "建立新 container ${CONTAINER_NAME}..."
  docker run -d \
    --name "${CONTAINER_NAME}" \
    -p "${DB_PORT}:5432" \
    -e POSTGRES_USER="${DB_USER}" \
    -e POSTGRES_PASSWORD="${DB_PASSWORD}" \
    -e POSTGRES_DB="${DB_NAME}" \
    --restart unless-stopped \
    postgres:17-alpine \
    postgres -c "log_min_messages=WARNING"
  success "Container 建立完成"
fi

wait_for_postgres "${DB_URL}"

# ── 步驟 3：套用 Migration ────────────────────────────────────────────────────

log "步驟 3/4：套用 Migration"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATION_DIR="${SCRIPT_DIR}/../control-plane/migrations"

if [[ ! -d "$MIGRATION_DIR" ]]; then
  die "Migration 目錄不存在：${MIGRATION_DIR}"
fi

# 依序套用所有 migration 檔案（按字母順序）
for MIGRATION_FILE in "${MIGRATION_DIR}"/*.sql; do
  [[ -f "$MIGRATION_FILE" ]] || continue
  MIGRATION_NAME="$(basename "$MIGRATION_FILE")"
  docker exec -i "${CONTAINER_NAME}" \
    psql -U "${DB_USER}" -d "${DB_NAME}" -v ON_ERROR_STOP=1 \
    < "${MIGRATION_FILE}" \
    && success "Migration 套用成功：${MIGRATION_NAME}" \
    || {
      warn "Migration ${MIGRATION_NAME} 可能已套用（IF NOT EXISTS / ADD COLUMN IF NOT EXISTS 是正常的），繼續..."
    }
done

# ── 步驟 4：建置 sbctl Binary ─────────────────────────────────────────────────

if ! $NO_BUILD; then
  log "步驟 4/5：建置 sbctl binary"
  (
    cd "${SCRIPT_DIR}/../control-plane"
    go build -ldflags "-X main.version=$(git describe --tags --always 2>/dev/null || echo dev)" \
      -o "${BINARY_DIR}/sbctl" \
      ./cmd/sbctl/
  )
  success "sbctl binary 建置完成：${BINARY_DIR}/sbctl"
else
  log "步驟 4/5：略過 build（--no-build）"
fi

# ── 步驟 5：寫出 .sbctl.env ───────────────────────────────────────────────────

ENV_FILE="${BINARY_DIR}/.sbctl.env"
log "步驟 5/5：寫出環境變數至 ${ENV_FILE}"

cat > "${ENV_FILE}" <<EOF
# sbctl Control Plane 環境設定
# 由 scripts/setup-control-plane.sh 自動產生
# 此檔案會在 sbctl 啟動時自動載入（shell 環境變數優先）
SBCTL_DB_URL=${DB_URL}
SBCTL_PROJECTS_DIR=${PROJECTS_DIR}
EOF

success ".sbctl.env 寫出完成"

# ── 完成：輸出使用說明 ────────────────────────────────────────────────────────

echo ""
echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════════╗${RESET}"
echo -e "${GREEN}${BOLD}║           Control Plane 環境準備完成！                   ║${RESET}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════╝${RESET}"
echo ""
echo -e "環境變數已寫入 ${BOLD}${ENV_FILE}${RESET}"
echo -e "sbctl 啟動時會自動載入，${BOLD}無需手動 export${RESET}。"
echo ""
echo "快速測試："
echo ""
echo -e "  ${BOLD}./sbctl project list${RESET}                                     # 應顯示空表格"
echo -e "  ${BOLD}./sbctl project create my-project --display-name \"My\"${RESET}   # 建立專案"
echo ""
echo "若需覆蓋 .sbctl.env 設定，可直接 export 環境變數（shell 優先）："
echo ""
echo -e "  ${BOLD}export SBCTL_DB_URL=\"<自訂 DSN>\"${RESET}"
echo ""
echo "停止 Control Plane DB："
echo -e "  ${BOLD}docker stop ${CONTAINER_NAME}${RESET}"
echo ""
echo "重新啟動 Control Plane DB："
echo -e "  ${BOLD}docker start ${CONTAINER_NAME}${RESET}"
echo ""
