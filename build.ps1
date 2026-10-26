$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Output = Join-Path $Root 'build\WhaleDesktop.exe'

Push-Location $Root
try {
  go test ./...
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Output) | Out-Null
  go build -tags 'desktop,production' -trimpath `
    -ldflags '-s -w -H=windowsgui' `
    -o $Output .
  Write-Host "Built: $Output"
} finally {
  Pop-Location
}
