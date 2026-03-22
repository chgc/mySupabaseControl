#!/usr/bin/env bash
set -euo pipefail

PROJECT=""
KONG_HTTP_PORT=28081
POSTGRES_PORT=5432
POOLER_TRANSACTION_PORT=6543

print_usage() {
  cat <<'EOF'
Usage:
  ./scripts/new-project.sh --project <name> [--kong-http-port <port>] [--postgres-port <port>] [--pooler-transaction-port <port>]

Options:
  --project, -p                  Project name (required)
  --kong-http-port               Kong HTTP port (default: 28081)
  --postgres-port                Postgres port (default: 5432)
  --pooler-transaction-port      Supavisor transaction port (default: 6543)
  --help, -h                     Show this help
EOF
}

is_valid_port() {
  local value="$1"
  [[ "$value" =~ ^[0-9]+$ ]] && (( value >= 1 && value <= 65535 ))
}

slugify_project() {
  local name="$1"
  local slug

  slug="$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]+/-/g; s/^-+//; s/-+$//')"

  if [[ -z "$slug" ]]; then
    echo "Project name '$name' becomes empty after normalization. Use letters or numbers." >&2
    exit 1
  fi

  if [[ ! "$slug" =~ ^[a-z0-9] ]]; then
    slug="p-$slug"
  fi

  printf '%s' "$slug"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project|-p)
      PROJECT="${2:-}"
      shift 2
      ;;
    --kong-http-port)
      KONG_HTTP_PORT="${2:-}"
      shift 2
      ;;
    --postgres-port)
      POSTGRES_PORT="${2:-}"
      shift 2
      ;;
    --pooler-transaction-port)
      POOLER_TRANSACTION_PORT="${2:-}"
      shift 2
      ;;
    --help|-h)
      print_usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      print_usage
      exit 1
      ;;
  esac
done

if [[ -z "$PROJECT" ]]; then
  echo "Missing required argument: --project" >&2
  print_usage
  exit 1
fi

if ! is_valid_port "$KONG_HTTP_PORT"; then
  echo "Invalid --kong-http-port: $KONG_HTTP_PORT (expected 1-65535)" >&2
  exit 1
fi

if ! is_valid_port "$POSTGRES_PORT"; then
  echo "Invalid --postgres-port: $POSTGRES_PORT (expected 1-65535)" >&2
  exit 1
fi

if ! is_valid_port "$POOLER_TRANSACTION_PORT"; then
  echo "Invalid --pooler-transaction-port: $POOLER_TRANSACTION_PORT (expected 1-65535)" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_ENV="$ROOT/.env"

if [[ ! -f "$BASE_ENV" ]]; then
  echo "Missing base .env file: $BASE_ENV" >&2
  exit 1
fi

PROJECT_SLUG="$(slugify_project "$PROJECT")"
PROJECT_DIR="$ROOT/projects/$PROJECT_SLUG"
PROJECT_ENV="$PROJECT_DIR/.env"

if [[ -f "$PROJECT_ENV" ]]; then
  echo "Project env already exists: $PROJECT_ENV" >&2
  exit 1
fi

mkdir -p "$PROJECT_DIR"
cp "$BASE_ENV" "$PROJECT_ENV"

cat >> "$PROJECT_ENV" <<EOF

# Multi-project overrides
PROJECT_SLUG=$PROJECT_SLUG
PROJECT_DATA_DIR=./projects/$PROJECT_SLUG/volumes
KONG_HTTP_PORT=$KONG_HTTP_PORT
POSTGRES_PORT=$POSTGRES_PORT
POOLER_PROXY_PORT_TRANSACTION=$POOLER_TRANSACTION_PORT
SUPABASE_PUBLIC_URL=http://localhost:$KONG_HTTP_PORT
API_EXTERNAL_URL=http://localhost:$KONG_HTTP_PORT
STUDIO_DEFAULT_PROJECT=Local Supabase ($PROJECT_SLUG)
EOF

mkdir -p \
  "$PROJECT_DIR/volumes/db/data" \
  "$PROJECT_DIR/volumes/storage" \
  "$PROJECT_DIR/volumes/functions" \
  "$PROJECT_DIR/volumes/snippets"

echo "Created project '$PROJECT_SLUG'"
if [[ "$PROJECT_SLUG" != "$PROJECT" ]]; then
  echo "Input project name '$PROJECT' normalized to '$PROJECT_SLUG' for cross-platform compatibility."
fi
echo "Env file: $PROJECT_ENV"
echo "Start command: docker compose --env-file projects/$PROJECT_SLUG/.env -p supabase-$PROJECT_SLUG up -d"
