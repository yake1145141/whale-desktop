param(
  [string]$Version = '0.4.1',
  [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$BuildExe = Join-Path $Root 'build\WhaleDesktop.exe'
$ReleaseRoot = Join-Path $Root 'dist-release'
$PortableDir = Join-Path $ReleaseRoot "portable\WhaleDesktop-v$Version-x64"
$PortableZip = Join-Path $ReleaseRoot "WhaleDesktop-Portable-v$Version-x64.zip"
$InstallerDir = Join-Path $ReleaseRoot 'installer'
$InstallerScript = Join-Path $Root 'installer\WhaleDesktop.iss'
$IconFile = Join-Path $Root 'assets\icon.ico'

if (-not $SkipBuild) {
  & (Join-Path $Root 'build.ps1') -Version $Version
  if ($LASTEXITCODE -ne 0) { throw "Build failed with exit code $LASTEXITCODE" }
}
if (-not (Test-Path -LiteralPath $BuildExe)) {
  throw "Missing build output: $BuildExe"
}

New-Item -ItemType Directory -Force -Path $PortableDir, $InstallerDir | Out-Null
Copy-Item -LiteralPath $BuildExe -Destination $PortableDir -Force
Copy-Item -LiteralPath (Join-Path $Root 'README.md') -Destination $PortableDir -Force
Copy-Item -LiteralPath (Join-Path $Root 'LICENSE') -Destination $PortableDir -Force

$portableReadme = @"
WhaleDesktop v$Version · Windows x64 绿色版

直接双击 WhaleDesktop.exe 即可运行，不需要安装。
程序会把配置、台词、皮肤和账本保存到：
%APPDATA%\dsh-whale-desktop\

首次运行会初始化默认皮肤。退出请使用托盘菜单或挂件菜单。
系统需要 Microsoft Edge WebView2 Runtime；Windows 10/11 通常已预装。

项目主页：
https://github.com/yake1145141/whale-desktop
"@
Set-Content -LiteralPath (Join-Path $PortableDir '使用说明.txt') `
  -Value $portableReadme -Encoding utf8

Compress-Archive -Path (Join-Path $PortableDir '*') `
  -DestinationPath $PortableZip -CompressionLevel Optimal -Force

$IsccCandidates = @(
  (Get-Command iscc.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -ErrorAction SilentlyContinue),
  (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'),
  'C:\Program Files (x86)\Inno Setup 6\ISCC.exe',
  'C:\Program Files\Inno Setup 6\ISCC.exe'
) | Where-Object { $_ -and (Test-Path -LiteralPath $_) }
$Iscc = $IsccCandidates | Select-Object -First 1
if (-not $Iscc) {
  throw 'Inno Setup 6 is required. Install it with: winget install JRSoftware.InnoSetup'
}

& $Iscc `
  "/DAppVersion=$Version" `
  "/DSourceDir=$PortableDir" `
  "/DOutputDir=$InstallerDir" `
  "/DIconFile=$IconFile" `
  $InstallerScript
if ($LASTEXITCODE -ne 0) {
  throw "Inno Setup failed with exit code $LASTEXITCODE"
}

$Installer = Join-Path $InstallerDir "WhaleDesktop-Setup-v$Version-x64.exe"
if (-not (Test-Path -LiteralPath $Installer)) {
  throw "Installer was not generated: $Installer"
}

Write-Host "Portable: $PortableZip"
Write-Host "Installer: $Installer"
