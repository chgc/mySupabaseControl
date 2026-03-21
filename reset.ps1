$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot

docker compose down -v --remove-orphans

$paths = @(
  '.\volumes\db\data',
  '.\volumes\storage'
)

foreach ($path in $paths) {
  if (Test-Path $path) {
    Remove-Item -Recurse -Force $path
  }
  New-Item -ItemType Directory -Force -Path $path | Out-Null
}
