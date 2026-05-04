#!/usr/bin/env bash
set -euo pipefail

# Wraps a tool invocation in a notify-me confirmation. Exit 0 = allow, exit 2 = deny (block).
CMD="${CLAUDE_TOOL_INPUT:-(no tool input env)}"

# -m must be larger than the server's default 180s timeout — give it 200s.
RESULT=$(curl -s -m 200 -d "$CMD" http://127.0.0.1:1886/api/confirm || echo "denied")

case "$RESULT" in
  approved)  exit 0 ;;
  *)         echo "用户拒绝执行: $RESULT" >&2 ; exit 2 ;;
esac
