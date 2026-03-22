set positional-arguments

default:
  @just --list

# Create a project with cross-platform script dispatch.
new-project project kong_http_port='28081' postgres_port='5432' pooler_transaction_port='6543':
  @just _new-project-{{os_family()}} "{{project}}" "{{kong_http_port}}" "{{postgres_port}}" "{{pooler_transaction_port}}"

_new-project-windows project kong_http_port postgres_port pooler_transaction_port:
  pwsh -NoProfile -File ./scripts/new-project.ps1 -Project "{{project}}" -KongHttpPort {{kong_http_port}} -PostgresPort {{postgres_port}} -PoolerTransactionPort {{pooler_transaction_port}}

_new-project-unix project kong_http_port postgres_port pooler_transaction_port:
  bash ./scripts/new-project.sh --project "{{project}}" --kong-http-port "{{kong_http_port}}" --postgres-port "{{postgres_port}}" --pooler-transaction-port "{{pooler_transaction_port}}"

# Reset one project with cross-platform script dispatch.
reset project='project-a':
  @just _reset-{{os_family()}} "{{project}}"

_reset-windows project:
  pwsh -NoProfile -File ./reset.ps1 -Project "{{project}}"

_reset-unix project:
  bash ./scripts/reset.sh --project "{{project}}"

# Run one project stack.
up project:
  docker compose --env-file projects/{{project}}/.env -p supabase-{{project}} up -d

# Stop one project stack.
down project:
  docker compose --env-file projects/{{project}}/.env -p supabase-{{project}} down

# Show one project stack status.
ps project:
  docker compose --env-file projects/{{project}}/.env -p supabase-{{project}} ps
