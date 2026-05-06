package singleton

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"notify-me/internal/config"
)

// HookHandler processes a Claude Code hook request received over IPC.
// body is the raw JSON from the hook. Returns the response JSON.
type HookHandler func(ctx context.Context, body []byte) ([]byte, error)

// AcquireOrActivate tries to acquire the singleton file lock. If another
// instance holds it, it sends a UDS / loopback-TCP nudge so the existing
// instance raises its main window, and returns (true, nil) so the caller exits.
// Returns (false, nil) when this process becomes the lock holder.
func AcquireOrActivate(cfg *config.Config) (alreadyRunning bool, err error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return false, err
	}
	lockPath := filepath.Join(dir, ".lock")
	if err := acquire(lockPath); err == nil {
		return false, nil
	} else if !errIsHeld(err) {
		return false, err
	}
	// Lock is held by another process — try to nudge it.
	if err := nudge(dir); err != nil {
		return true, fmt.Errorf("another instance running but nudge failed: %w", err)
	}
	return true, nil
}

// ListenForActivation spins up an IPC endpoint (Unix socket on macOS/Linux,
// loopback TCP on Windows) that handles two message types:
//   - activation nudge → calls onActivate
//   - hook request (JSON with hook_event_name) → calls onHook
//
// Caller must invoke the returned closer at shutdown.
func ListenForActivation(onActivate func(), onHook HookHandler) (closer func(), err error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return listenIPC(filepath.Join(dir, ".sock"), onActivate, onHook)
}

// Release releases the file lock held by this process. Caller should invoke
// at shutdown.
func Release() error { return release() }

// HookIPC sends a hook request to the running daemon via IPC and returns
// the response. Used by the "notify-me hook" CLI subcommand.
func HookIPC(body []byte) ([]byte, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return hookIPC(filepath.Join(dir, ".sock"), body)
}

// HookIPCCancel is like HookIPC but closes the IPC connection when ctx is
// cancelled, allowing the caller to abandon the request (e.g. when terminal
// input arrives first).
func HookIPCCancel(ctx context.Context, body []byte) ([]byte, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return hookIPCCancel(ctx, filepath.Join(dir, ".sock"), body)
}

// nudge connects to the IPC endpoint and sends the activate command.
func nudge(dir string) error {
	return nudgeIPC(filepath.Join(dir, ".sock"))
}

func errIsHeld(err error) bool {
	return os.IsExist(err) || isLockedErr(err)
}
