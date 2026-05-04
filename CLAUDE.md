# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`notify-me` — cross-platform desktop confirmation tool. Listens on `127.0.0.1:1886`, accepts HTTP POSTs (e.g. from a ClaudeCode hook), shows an always-on-top popup, and **synchronously** returns the user's decision (`approved` / `denied` / `acknowledged` / `timeout` / `cancelled`) in the HTTP response body. Module path: `notify-me`.

Stack: Go 1.25 + Wails v3 alpha.78 (CGO/WebView GUI) + Vue 3 + Pinia + Vite (multi-entry) + modernc.org/sqlite (pure-Go) + zerolog + lumberjack.

Supported targets: **Windows + macOS only.** Linux is not a target — `scripts/build.sh` rejects it and `GOOS=linux go build ./...` does not work because Wails has no Linux build tag in this codebase.

## Build & run

```bash
# Frontend (Vite multi-entry: index.html + popup.html → frontend/dist/)
cd frontend && npm install && npm run build && cd ..
# vite emptyOutDir wipes frontend/dist/.gitkeep — restore it (build.sh does this)
touch frontend/dist/.gitkeep

# Full app build (must run on the target OS — Wails uses Cocoa/WebView2 via CGO):
scripts/build.sh           # macOS only (run on a Mac)
scripts/build.ps1          # Windows only (run on a Win box)

# Cross-compile from Linux is NOT supported. Use a real Mac/Win or GitHub Actions.
# Windows-from-Linux can be attempted via wails3 but is not wired into build scripts.
```

`go install github.com/wailsapp/wails/v3/cmd/wails3@latest` is required; build scripts auto-install if missing. If `proxy.golang.org` is unreachable, set `GOPROXY=https://goproxy.cn,direct`.

## Test

```bash
go test ./... -race                          # all packages, race detector on
go test ./internal/dispatcher -race -run TestCancelActiveReleasesWorker
go test ./internal/server -race -v
go vet ./...
GOOS=windows GOARCH=amd64 go build ./...     # verify Windows still compiles
```

Tests rely on `NOTIFY_ME_CONFIG_HOME` to redirect the per-user config dir into `t.TempDir()`. Platform-specific code uses build tags (`//go:build darwin`, `!windows`, `!darwin && !windows`); test helpers mirror those tags (e.g. `autostart_darwin_test.go` + `autostart_other_test.go`) so the test binary builds on every supported platform.

## Architecture

### Boot sequence (main.go → app.go)

1. `config.LoadOrInit()` — reads `<ConfigDir>/config.json`; on parse failure, renames the broken file aside, writes defaults, returns `(*Config, err)` so the app can show a banner.
2. `singleton.AcquireOrActivate(cfg)` — file lock at `<ConfigDir>/.lock`. If held by another process, send a UDS (Unix) / TCP-loopback (Windows) **nudge** to `<ConfigDir>/.sock` so the existing instance raises its window, then exit.
3. `application.New(...)` (Wails) → `app.Boot(wailsApp)`:
   - `window.NewManager` + `MountMain()` (hidden until tray Show).
   - `singleton.ListenForActivation(...)` → IPC server that calls `win.ShowMain` on nudge.
   - `storage.Open(<ConfigDir>/notifications.db)` (SQLite WAL); on failure → native dialog + `w.Quit()`.
   - `dispatcher.New(...)` + `go disp.Run(ctx)` — single goroutine serial queue.
   - `server.New(...)` + `server.Start()` — HTTP listener.
   - `tray.Mount(...)` — **last**, so handlers close over fully-initialized subsystems.

### The HTTP → dispatcher → popup → response loop

- HTTP handler (`internal/server/handler.go` + `server.go::endpointHandler`) builds a `*Notification` via `parseRequest` (priority **JSON body > Header > Query > endpoint default > global default**), inserts a `pending` row, calls `disp.Enqueue(n)`, then **blocks** on `select { <-n.ResultCh; <-r.Context().Done() }`.
- Dispatcher worker (`internal/dispatcher/dispatcher.go::run`) pops `n` off the channel, calls `OnActive` (which emits the popup via `window.OpenPopup`), then `select`s on `<-ctx.Done() / <-timer.C / <-n.Done`.
- Frontend popup calls `App.Resolve(id, decision)` → `disp.Resolve(id, ...)` → `n.Resolve(r)` (sync.Once-guarded; closes `Done`, sends to `ResultCh`).
- HTTP handler unblocks on `ResultCh`, writes `UpdateStatus`, returns `200 <decision>` body.

**Critical race fix to preserve:** the dispatcher worker waits on `n.Done` (signal-only, closed by `Resolve`), **not** `n.ResultCh`. Earlier versions had both worker and HTTP handler reading `ResultCh`; if HTTP won, the worker stayed parked until timer (default 180s), blocking the queue. See `dispatcher_test.go::TestCancelActiveReleasesWorker`.

**Client-disconnect path:** if `r.Context().Done()` fires first, handler calls `disp.Cancel(n.ID)` then `<-n.ResultCh` to drain (guaranteed non-blocking because `Resolve` is once-guarded and the channel is buffered cap 1). DB is updated with `context.Background()` since the request context is dead.

### Config

`internal/config/config.go::Config` carries a `*sync.RWMutex` (pointer — eager-init in `defaults()` and `LoadOrInit`; **never a value field** because `Snapshot()` returns by value and `go vet` flags the copy). `Apply` deep-copies the `Endpoints` slice. `Save` is atomic (`tmp + rename`). The `mu` field is JSON-tagged `"-"`. Most fields hot-reload via `Snapshot()`; **`server.host`/`port`/`endpoint_prefix`/`auth_token` and endpoint definitions are frozen at `server.Handler()` build time** — changing them requires a server restart.

### JSON content-type detection

`parseRequest` uses `mime.ParseMediaType(...)` and matches on exact `application/json` so that variants like `application/json-patch+json` are **not** treated as JSON (regression test: `TestParseRejectsJSONPatchVariant`). Body is hard-capped at 64 KB via `http.MaxBytesReader` + `io.LimitReader`.

### Storage

`internal/storage/storage.go` — single `notifications` table, opened with `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`. `Prune` deletes by retention-days then trims oldest until row count ≤ `max_records`. `RunCleanup(ctx, hour, ...)` ticks hourly (errors swallowed; logger wrap-up is the integration responsibility, see `cleanup.go` comment).

### Wails v3 alpha.78 specifics (easy to get wrong)

- Window creation: `app.Window.NewWithOptions(application.WebviewWindowOptions{...})` — manager pattern, not `app.NewWindow`.
- Events come from `github.com/wailsapp/wails/v3/pkg/events` — use `events.Common.WindowClosing`, `events.Common.WindowRuntimeReady`.
- **Use `RegisterHook`** (synchronous, supports `e.Cancel()`) to intercept window close and hide-to-tray. `OnWindowEvent` runs in a goroutine and is too late to cancel.
- Background colour: `application.NewRGB(255,255,255)` returns a value, not a pointer. Field is `BackgroundColour`.
- Frontend events: `app.Event.Emit(name, payload)` — no `WailsEvent` struct.

### Frontend layout

`frontend/vite.config.ts` defines two entries → two HTML files → two SPA bundles in `frontend/dist/`:
- `index.html` + `src/main.ts` + `src/App.vue` → main window (history view, settings view; Pinia stores in `src/stores/`).
- `popup.html` + `src/popup.ts` + `src/PopupApp.vue` → notification popup.

Both bundles are embedded via `//go:embed all:frontend/dist` in `main.go`. **`vite build` deletes `frontend/dist/.gitkeep`** because of `emptyOutDir: true`; the build scripts re-touch it.

### Platform branching

Files using `_<os>.go` + build tags. Pattern: `foo.go` declares the cross-platform interface, `foo_darwin.go` / `foo_windows.go` / `foo_other.go` provide implementations. Examples:

- `internal/sound/` — `afplay` (macOS) / `MessageBeep` (Windows) / no-op (other).
- `internal/autostart/` — LaunchAgent plist (macOS) / `HKCU\...\Run` registry (Windows) / no-op.
- `internal/singleton/lock_unix.go` (`flock`) vs `lock_windows.go` (`CreateFile` exclusive).
- `startup_dialog_*.go` at the repo root — pre-Wails native error dialog.

When adding platform-specific code, mirror this layout and add a corresponding `*_test.go` per tag if tests reference platform-only symbols (otherwise the test binary will fail to build on other OSes — bit us before with `plistName`).

## Conventions

- Go: standard `gofmt` + `go vet`; tests must pass under `-race`.
- Errors at startup that prevent serving (db open, server start) → call `showStartupError(...)` (native dialog) **then** `w.Quit()`. Don't crash.
- Background goroutines must respect the `app.ctx` from `Boot()`. Cleanup callbacks go in `app.cleanup []func()` and run in `Shutdown()`.
- Single-instance behavior is mandatory; never bypass `singleton.AcquireOrActivate` in `main`.
- The dispatcher's `OnActive` callback **must not block** — emit a Wails event / open a window and return.

## Reference

- Spec: `docs/superpowers/specs/2026-04-27-notify-me-design.md`
- Plan: `docs/superpowers/plans/2026-04-27-notify-me.md` (23-task implementation plan; all tasks completed; tagged v0.1.0)
- API + request examples: `README.md`
- Hook examples: `examples/claude-hook-confirm.{sh,ps1}`
