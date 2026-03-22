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
