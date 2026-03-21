# Local Supabase with Docker

This workspace runs a self-hosted Supabase stack locally with `docker compose`.

## Start

```powershell
docker compose up -d
```

## Stop

```powershell
docker compose down
```

## Reset local data

```powershell
.\reset.ps1
```

## URLs

- Studio / API gateway: `http://localhost:8000`
- Postgres session mode: `localhost:5432`
- Postgres transaction mode: `localhost:6543`

## Credentials and keys

Project-specific secrets were generated into `.env`.

- Studio username: `supabase`
- Studio password: see `DASHBOARD_PASSWORD` in `.env`
- Client API key: see `ANON_KEY` in `.env`
- Service role key: see `SERVICE_ROLE_KEY` in `.env`

## Quick checks

```powershell
docker compose ps
Invoke-WebRequest http://localhost:8000/auth/v1/health -Headers @{ apikey = '<ANON_KEY>'; Authorization = 'Bearer <ANON_KEY>' }
```

## Notes

- This setup is for local development, not production hardening.
- The official Supabase Docker template was adapted for this Windows workspace.
- Linux-specific `:Z` bind mount flags were removed for Docker Desktop compatibility.
