package singleton

import (
	"fmt"
	"os"
	"path/filepath"

	"notify-me/internal/config"
)

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
// loopback TCP on Windows) that calls onActivate when nudged.
// Caller must invoke the returned closer at shutdown.
func ListenForActivation(onActivate func()) (closer func(), err error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return listenIPC(filepath.Join(dir, ".sock"), onActivate)
}

// Release releases the file lock held by this process. Caller should invoke
// at shutdown.
func Release() error { return release() }

// nudge connects to the IPC endpoint and sends the activate command.
func nudge(dir string) error {
	return nudgeIPC(filepath.Join(dir, ".sock"))
}

func errIsHeld(err error) bool {
	return os.IsExist(err) || isLockedErr(err)
}
