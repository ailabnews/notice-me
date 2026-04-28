package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadOrInitWritesDefaults(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, err := LoadOrInit()
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}
	if cfg.Server.Port != 886 {
		t.Fatalf("default port: got %d", cfg.Server.Port)
	}
	if len(cfg.Endpoints) != 3 {
		t.Fatalf("default endpoints: got %d", len(cfg.Endpoints))
	}
	if cfg.Behavior.DefaultTimeoutSeconds != 180 {
		t.Fatalf("default timeout: got %d", cfg.Behavior.DefaultTimeoutSeconds)
	}
	p, _ := FilePath("config.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("config.json not written: %v", err)
	}
}

func TestLoadOrInitRecoversFromCorruption(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOTIFY_ME_CONFIG_HOME", home)
	p, _ := FilePath("config.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadOrInit()
	if err == nil {
		t.Fatal("expected non-nil error to flag corruption (defaults still returned)")
	}
	if cfg == nil || cfg.Server.Port == 0 {
		t.Fatal("expected defaults despite corruption")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "config.json.broken-*"))
	if len(matches) == 0 {
		t.Fatal("broken config not preserved")
	}
}

func TestSaveRoundtrip(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, _ := LoadOrInit()
	cfg.Server.Port = 9999
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	p, _ := FilePath("config.json")
	raw, _ := os.ReadFile(p)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if int(got["server"].(map[string]any)["port"].(float64)) != 9999 {
		t.Fatal("port not persisted")
	}
	if _, ok := got["mu"]; ok {
		t.Fatal("persisted JSON must not contain mu field")
	}
}

func TestSnapshotApplyConcurrent(t *testing.T) {
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, _ := LoadOrInit()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = cfg.Snapshot() }()
		go func() { defer wg.Done(); cfg.Apply(cfg.Snapshot()) }()
	}
	wg.Wait()
}
