# scripts/build.ps1 — build the Windows artefact
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

if (-not (Get-Command wails3 -ErrorAction SilentlyContinue)) {
  Write-Host "Installing wails3 CLI..."
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
}

Push-Location frontend
  npm install --no-audit --no-fund
  npm run build
Pop-Location

# Restore .gitkeep deleted by vite emptyOutDir
if (-not (Test-Path frontend/dist/.gitkeep)) {
  New-Item -Path frontend/dist/.gitkeep -ItemType File | Out-Null
}

wails3 build -platform windows/amd64
Write-Host "Windows build at: build\bin\notify-me.exe"
