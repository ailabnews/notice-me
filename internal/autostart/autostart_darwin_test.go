//go:build darwin

package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

func verifyPlistRemoved(t *testing.T) {
	t.Helper()
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, "Library", "LaunchAgents", plistName)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("plist not removed: %v", err)
	}
}
