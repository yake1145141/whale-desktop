@echo off
setlocal
set "EXE=%~dp0build\WhaleDesktop.exe"
if not exist "%EXE%" (
  echo [x] WhaleDesktop.exe not found. Run: powershell -ExecutionPolicy Bypass -File build.ps1
  exit /b 1
)
if /i "%~1"=="--debug" (
  set "WHALE_DEBUG=1"
  start "" "%EXE%" %*
) else (
  start "" /b "%EXE%" %*
)
exit /b 0
