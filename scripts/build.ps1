# scripts/build.ps1 — build the Windows artefact
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

Push-Location frontend
  npm install --no-audit --no-fund
  npm run build
Pop-Location

# Restore .gitkeep deleted by vite emptyOutDir
if (-not (Test-Path frontend/dist/.gitkeep)) {
  New-Item -Path frontend/dist/.gitkeep -ItemType File | Out-Null
}

# Build with CGO enabled (WebView2 requires it).
$env:CGO_ENABLED = '1'

if (-not (Test-Path 'build\bin')) {
  New-Item -Path 'build\bin' -ItemType Directory | Out-Null
}

go build -ldflags="-H windowsgui" -o build\bin\notify-me.exe .
Write-Host "Windows build at: build\bin\notify-me.exe"
