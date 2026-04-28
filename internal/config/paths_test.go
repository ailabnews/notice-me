package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigDir(t *testing.T) {
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if filepath.Base(dir) != "notify-me" {
		t.Fatalf("expected suffix notify-me, got %s", dir)
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		want := filepath.Join(home, "Library", "Application Support", "notify-me")
		if dir != want {
			t.Fatalf("darwin: want %s got %s", want, dir)
		}
	}
}

func TestConfigDirCreatesParent(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dir not created: %v", err)
	}
}
