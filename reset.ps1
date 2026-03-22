[CmdletBinding()]
param(
  [Parameter(Mandatory = $false)]
  [string]$Project = 'project-a'
)

$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot

$envFile = Join-Path $PSScriptRoot "projects/$Project/.env"
if (-not (Test-Path $envFile)) {
  throw "Missing env file: $envFile"
}

$composeProject = "supabase-$Project"

docker compose --env-file $envFile -p $composeProject down -v --remove-orphans

$projectDataDir = Select-String -Path $envFile -Pattern '^PROJECT_DATA_DIR=(.*)$' |
  ForEach-Object { $_.Matches[0].Groups[1].Value } |
  Select-Object -First 1

if (-not $projectDataDir) {
  $projectDataDir = "./projects/$Project/volumes"
}

$projectDataPath = Join-Path $PSScriptRoot $projectDataDir.TrimStart('./').Replace('/', [IO.Path]::DirectorySeparatorChar)

$paths = @(
  (Join-Path $projectDataPath 'db/data'),
  (Join-Path $projectDataPath 'storage')
)

foreach ($path in $paths) {
  if (Test-Path $path) {
    Remove-Item -Recurse -Force $path
  }
  New-Item -ItemType Directory -Force -Path $path | Out-Null
}
