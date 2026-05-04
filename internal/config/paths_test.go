package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDir(t *testing.T) {
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if filepath.Base(dir) != ".notice-me" {
		t.Fatalf("expected suffix .notice-me, got %s", dir)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".notice-me")
	if dir != want {
		t.Fatalf("want %s got %s", want, dir)
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
