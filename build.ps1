param(
  [string]$Version = '0.4.0'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Output = Join-Path $Root 'build\WhaleDesktop.exe'

Push-Location $Root
try {
  go test ./...
  $env:GOPROXY = if ($env:GOPROXY) { $env:GOPROXY } else { 'https://goproxy.cn,direct' }
  go run github.com/tc-hib/go-winres@v0.3.3 simply `
    --arch amd64 `
    --out rsrc.syso `
    --no-suffix `
    --manifest gui `
    --icon assets/icon.ico `
    --file-description "DeepSeek Balance Whale Widget" `
    --product-name "WhaleDesktop" `
    --product-version $Version `
    --file-version $Version `
    --copyright "Copyright (c) 2026 MeteorNOX"
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Output) | Out-Null
  go build -tags 'desktop,production' -trimpath `
    -ldflags '-s -w -H=windowsgui' `
    -o $Output .
  Write-Host "Built: $Output (v$Version)"
} finally {
  Pop-Location
}
