#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v wails3 >/dev/null 2>&1; then
  echo "Installing wails3 CLI..."
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
fi

# Frontend build
(cd frontend && npm install --no-audit --no-fund && npm run build)

# Restore .gitkeep deleted by vite emptyOutDir.
touch frontend/dist/.gitkeep

case "$(uname -s)" in
  Darwin*)
    wails3 build -platform darwin/arm64
    echo "macOS build at: build/bin/notify-me.app"
    ;;
  Linux*)
    echo "Linux is not a supported target. To build Windows from Linux, run:"
    echo "  GOOS=windows GOARCH=amd64 wails3 build -platform windows/amd64"
    exit 1
    ;;
  *)
    echo "Unknown OS — use scripts/build.ps1 for Windows."
    exit 1
    ;;
esac
