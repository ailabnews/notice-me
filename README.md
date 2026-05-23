# notify-me

<p align="center"><strong><a href="./docs/README.md">使用教程</a></strong></p>

Cross-platform desktop confirmation tool. HTTP in, popup out.

Receives HTTP POSTs on `localhost:1886`, displays an always-on-top popup, and synchronously returns the user's decision (`approved` / `denied` / `acknowledged` / `timeout`) to the caller. Designed to make ClaudeCode hook confirmations not require staring at a terminal.

## Build

Windows + macOS are the supported targets. Build via Wails v3:

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest

# macOS (run on a Mac)
wails3 build -platform darwin/arm64
# Windows (run on a Win box, or cross-compile)
wails3 build -platform windows/amd64
```

macOS unsigned: first launch requires right-click → Open to bypass Gatekeeper.

## API

Three endpoints (default prefix `/api`, configurable):

| Path | Mode | Default OK / Cancel | Default title |
| --- | --- | --- | --- |
| `POST /api/confirm` | two-button | 确定 / 取消 | ClaudeCode 通知 |
| `POST /api/danger` | two-button | 允许 / 拒绝 | ⚠️ 危险操作确认 |
| `POST /api/info` | single-button | 知道了 / (none) | 通知 |

### Three request forms

**Plain text:**
```bash
curl -d "Continue?" http://127.0.0.1:1886/api/confirm
```

**Header / Query overrides:**
```bash
curl -d "rm -rf /tmp/foo" \
     -H "X-Title: 危险命令" \
     -H "X-Timeout: 60" \
     -H "X-Ok: 允许" \
     -H "X-Cancel: 拒绝" \
     http://127.0.0.1:1886/api/confirm
```

**JSON:**
```bash
curl -X POST -H "Content-Type: application/json" -d '{
  "title":"Confirm","message":"rm -rf /tmp","timeout":60
}' http://127.0.0.1:1886/api/confirm
```

### Response (HTTP 200, body)

| Body | Meaning |
| --- | --- |
| `approved` | user clicked OK |
| `denied` | user clicked Cancel or closed popup |
| `acknowledged` | single-button click |
| `timeout` | no action within `timeout` seconds (default 180) |
| `paused` | tray menu has paused intake (HTTP 503) |
| `queue full` | too many pending notifications (HTTP 503) |

Field priority for overrides: **JSON body > Header > Query > endpoint default > global default**.

## ClaudeCode hook

### Method 1: HTTP hooks (recommended)

Claude Code natively supports HTTP hooks — configure it to POST directly to notify-me's `/api/claude/hook` endpoint. No shell scripts needed.

Copy `examples/claude-settings.json` into your `~/.claude/settings.json` (global) or `.claude/settings.json` (per-project). The endpoint handles these hook events:

| Event | Behavior | Popup |
| --- | --- | --- |
| `PreToolUse` | **Blocking** — waits for user decision, returns `allow`/`deny` | Confirm/Danger popup |
| `PermissionRequest` | **Blocking** — waits for user decision, returns `allow`/`deny` | Confirm popup |
| `PermissionDenied` | **Instant** — returns `retry: true` so Claude can retry | None |
| `Notification` (idle_prompt) | **Fire-and-forget** — shows notification, returns 200 immediately | Info popup |
| `Stop` | **Fire-and-forget** — shows "Claude 已完成" | Info popup |
| `StopFailure` | **Fire-and-forget** — shows error message | Info popup |

Dangerous commands (`rm -rf`, `git push --force`, `git reset --hard`, etc.) are automatically detected and shown with the danger popup style (red warning).

### Method 2: Shell command hooks (legacy)

See `examples/claude-hook-confirm.sh` (bash) / `examples/claude-hook-confirm.ps1` (PowerShell).

## Configuration

Lives at:
- macOS / Windows / Linux: `~/.notice-me/config.json`

Editable via the Settings tab in the main window. Most fields hot-reload; `host` / `port` changes require a restart.

## License

Proprietary / private use.
