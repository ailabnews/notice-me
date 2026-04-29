#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v wails3 >/dev/null 2>&1; then
  echo "Installing wails3 CLI..."
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
fi

wails3 package
echo "macOS .app bundle at: bin/notify-me.app"
