[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [ValidateNotNullOrEmpty()]
  [string]$Project,

  [Parameter(Mandatory = $false)]
  [ValidateRange(1, 65535)]
  [int]$KongHttpPort = 28081,

  [Parameter(Mandatory = $false)]
  [ValidateRange(1, 65535)]
  [int]$PostgresPort = 5432,

  [Parameter(Mandatory = $false)]
  [ValidateRange(1, 65535)]
  [int]$PoolerTransactionPort = 6543
)

$ErrorActionPreference = 'Stop'

function Get-ProjectSlug {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name
  )

  $slug = $Name.Trim().ToLowerInvariant()
  $slug = [System.Text.RegularExpressions.Regex]::Replace($slug, '[^a-z0-9_-]+', '-')
  $slug = $slug.Trim('-')

  if ([string]::IsNullOrWhiteSpace($slug)) {
    throw "Project name '$Name' becomes empty after normalization. Use letters or numbers."
  }

  if (-not [char]::IsLetterOrDigit($slug[0])) {
    $slug = "p-$slug"
  }

  return $slug
}

$projectSlug = Get-ProjectSlug -Name $Project
$root = Split-Path -Parent $PSScriptRoot

$baseEnv = Join-Path $root '.env'
if (-not (Test-Path $baseEnv)) {
  throw "Missing base .env file: $baseEnv"
}

$projectDir = Join-Path (Join-Path $root 'projects') $projectSlug
$projectEnv = Join-Path $projectDir '.env'

if (Test-Path $projectEnv) {
  throw "Project env already exists: $projectEnv"
}

New-Item -ItemType Directory -Force -Path $projectDir | Out-Null
Copy-Item $baseEnv $projectEnv

$overrides = @(
  '',
  '# Multi-project overrides',
  "PROJECT_SLUG=$projectSlug",
  "PROJECT_DATA_DIR=./projects/$projectSlug/volumes",
  "KONG_HTTP_PORT=$KongHttpPort",
  "POSTGRES_PORT=$PostgresPort",
  "POOLER_PROXY_PORT_TRANSACTION=$PoolerTransactionPort",
  "SUPABASE_PUBLIC_URL=http://localhost:$KongHttpPort",
  "API_EXTERNAL_URL=http://localhost:$KongHttpPort",
  "STUDIO_DEFAULT_PROJECT=Local Supabase ($projectSlug)"
)

Add-Content -Path $projectEnv -Value $overrides

$dataDirs = @(
  (Join-Path $projectDir 'volumes/db/data'),
  (Join-Path $projectDir 'volumes/storage'),
  (Join-Path $projectDir 'volumes/functions'),
  (Join-Path $projectDir 'volumes/snippets')
)

foreach ($dir in $dataDirs) {
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
}

Write-Output "Created project '$projectSlug'"
if ($projectSlug -ne $Project) {
  Write-Output "Input project name '$Project' normalized to '$projectSlug' for cross-platform compatibility."
}
Write-Output "Env file: $projectEnv"
Write-Output "Start command: docker compose --env-file projects/$projectSlug/.env -p supabase-$projectSlug up -d"
