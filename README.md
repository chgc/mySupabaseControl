# Local Supabase with Docker (Multi-Projects)

This workspace runs self-hosted Supabase locally and now supports running multiple isolated projects at the same time.

## Cross-platform commands via just

This repo uses `justfile` as the main command interface.

- `just` auto-detects OS and dispatches to the right script.
- Windows calls PowerShell scripts.
- macOS/Linux calls Bash scripts.
- On macOS/Linux, you do **not** need to install PowerShell.

Install `just`:

```bash
brew install just
```

```powershell
winget install --id Casey.Just --exact
```

## Project layout

- `docker-compose.yml` is the shared base stack.
- `projects/<project>/.env` stores each project's runtime settings.
- `projects/<project>/volumes` stores each project's local data.

Create a new project from the base `.env`:

```bash
just new-project project-a 28081 5432 6543
just new-project project-b 38081 15432 16543
```

Included examples:

- `projects/project-a/.env`
- `projects/project-b/.env`

## Start a project

```bash
just up project-a
```

Start another project in parallel:

```bash
just up project-b
```

## Stop a project

```bash
just down project-a
```

## Reset one project only

```bash
just reset project-a
```

See all commands:

```bash
just --list
```

This command:

- Stops only `supabase-project-a`
- Removes only that project's containers and volumes
- Recreates only that project's `db/data` and `storage` folders

## URLs and ports

Read each project's `.env`:

- `KONG_HTTP_PORT` controls API gateway URL (`http://localhost:<KONG_HTTP_PORT>`)
- `POSTGRES_PORT` controls direct Postgres port
- `POOLER_PROXY_PORT_TRANSACTION` controls pooler transaction port

Example defaults:

- project-a: `28081`, `5432`, `6543`
- project-b: `38081`, `15432`, `16543`

## Credentials and keys

Each project has its own `.env` file.

- Studio username: `supabase`
- Studio password: `DASHBOARD_PASSWORD`
- Client API key: `ANON_KEY`
- Service role key: `SERVICE_ROLE_KEY`

## Quick checks

```powershell
just ps project-a
Invoke-WebRequest http://localhost:28081/auth/v1/health -Headers @{ apikey = '<ANON_KEY>'; Authorization = 'Bearer <ANON_KEY>' }
```

## Notes

- This setup is for local development, not production hardening.
- Fixed `container_name` entries were removed so multiple projects can run without name collisions.
- Writable bind mounts are now project-scoped via `PROJECT_DATA_DIR`.

---

## sbctl — Control Plane CLI

`sbctl` 是 Control Plane 的命令列工具，可程式化地管理 Supabase 專案的完整生命週期，並支援 MCP Server 模式供 AI agent 使用。

### 前置需求

- Docker（已安裝且 daemon 在執行中）
- Go 1.22+（用來建置 binary）

### 一鍵安裝

```bash
just cp-setup
```

這個指令會自動：
1. 啟動 Control Plane 專用 PostgreSQL container（port 5433）
2. 套用 DB migration（建立 `projects` / `project_configs` / `project_overrides` 表）
3. 建置 `./sbctl` binary

完成後依照輸出設定環境變數：

```bash
export SBCTL_DB_URL="postgresql://postgres:sbctl_secret@localhost:5433/sbctl"
export SBCTL_PROJECTS_DIR="./projects"
```

### 使用方式

```bash
# 列出所有專案
./sbctl project list

# 建立並啟動新專案（需 Docker）
./sbctl project create my-project --display-name "My Project"

# 查看專案狀態
./sbctl project get my-project

# 停止 / 啟動 / 重置 / 刪除
./sbctl project stop my-project
./sbctl project start my-project
./sbctl project reset my-project
./sbctl project delete my-project

# JSON 輸出
./sbctl -o json project list

# 啟動 MCP Server（供 AI agent 使用，stdio transport）
./sbctl mcp serve
```

### 進階安裝選項

```bash
# 自訂 DB port 與密碼
just cp-setup --db-port 5434 --db-password mypassword

# 只重建 binary（不重建 DB）
just cp-setup --no-build

# 重置 DB（清除所有資料並重建）
just cp-setup --reset-db
```
