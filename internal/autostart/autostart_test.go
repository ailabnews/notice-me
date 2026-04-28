//go:build !windows

package autostart

import (
	"runtime"
	"testing"
)

func TestSetIsEnabledRoundtrip(t *testing.T) {
	if runtime.GOOS == "darwin" {
		// Override HOME so the plist lands in temp.
		t.Setenv("HOME", t.TempDir())
	}
	enabled, err := IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled initial: %v", err)
	}
	if enabled {
		t.Fatalf("expected initial=false, got true")
	}
	if err := Set(true, "/path/to/notify-me"); err != nil {
		t.Fatalf("Set(true): %v", err)
	}
	enabled, _ = IsEnabled()
	if runtime.GOOS == "darwin" && !enabled {
		t.Fatalf("expected darwin enabled after Set(true)")
	}
	// Linux returns false unconditionally — that's fine.
	if err := Set(false, "/path/to/notify-me"); err != nil {
		t.Fatalf("Set(false): %v", err)
	}
	enabled, _ = IsEnabled()
	if enabled {
		t.Fatalf("expected disabled after Set(false)")
	}
	// On darwin, also verify the plist file was removed.
	verifyPlistRemoved(t)
}
