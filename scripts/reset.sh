#!/usr/bin/env bash
set -euo pipefail

PROJECT="project-a"

print_usage() {
  cat <<'EOF'
Usage:
  ./reset.sh [--project <name>]

Options:
  --project, -p     Project name (default: project-a)
  --help, -h        Show this help
EOF
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

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_SLUG="$(slugify_project "$PROJECT")"

REQUESTED_ENV_FILE="$ROOT/projects/$PROJECT/.env"
SLUG_ENV_FILE="$ROOT/projects/$PROJECT_SLUG/.env"

if [[ -f "$REQUESTED_ENV_FILE" ]]; then
  ENV_FILE="$REQUESTED_ENV_FILE"
  EFFECTIVE_PROJECT="$PROJECT"
elif [[ -f "$SLUG_ENV_FILE" ]]; then
  ENV_FILE="$SLUG_ENV_FILE"
  EFFECTIVE_PROJECT="$PROJECT_SLUG"
else
  echo "Missing env file. Checked: $REQUESTED_ENV_FILE and $SLUG_ENV_FILE" >&2
  exit 1
fi

COMPOSE_PROJECT="supabase-$(slugify_project "$EFFECTIVE_PROJECT")"

docker compose --env-file "$ENV_FILE" -p "$COMPOSE_PROJECT" down -v --remove-orphans

PROJECT_DATA_DIR="$(grep -E '^PROJECT_DATA_DIR=' "$ENV_FILE" | head -n 1 | cut -d'=' -f2-)"
if [[ -z "$PROJECT_DATA_DIR" ]]; then
  PROJECT_DATA_DIR="./projects/$EFFECTIVE_PROJECT/volumes"
fi

if [[ "$PROJECT_DATA_DIR" = /* ]]; then
  PROJECT_DATA_PATH="$PROJECT_DATA_DIR"
else
  PROJECT_DATA_PATH="$ROOT/${PROJECT_DATA_DIR#./}"
fi

PATHS=(
  "$PROJECT_DATA_PATH/db/data"
  "$PROJECT_DATA_PATH/storage"
)

for path in "${PATHS[@]}"; do
  rm -rf "$path"
  mkdir -p "$path"
done

echo "Reset complete for '$EFFECTIVE_PROJECT'"
echo "Env file: $ENV_FILE"
echo "Data path: $PROJECT_DATA_PATH"
