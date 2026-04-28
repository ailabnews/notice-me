package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

type Config struct {
	Server    ServerConfig     `json:"server"`
	Endpoints []EndpointConfig `json:"endpoints"`
	UI        UIConfig         `json:"ui"`
	Behavior  BehaviorConfig   `json:"behavior"`
	History   HistoryConfig    `json:"history"`
	Log       LogConfig        `json:"log"`

	mu *sync.RWMutex `json:"-"`
}

type ServerConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	EndpointPrefix string `json:"endpoint_prefix"`
	AuthToken      string `json:"auth_token"`
	MaxQueueSize   int    `json:"max_queue_size"`
}

type EndpointConfig struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	OkText     string `json:"ok_text"`
	CancelText string `json:"cancel_text"`
	Mode       string `json:"mode"` // "two-button" | "single-button"
}

type UIConfig struct {
	PopupPosition string    `json:"popup_position"`
	PopupSize     PopupSize `json:"popup_size"`
	Theme         string    `json:"theme"`
}

type PopupSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type BehaviorConfig struct {
	DefaultTimeoutSeconds int    `json:"default_timeout_seconds"`
	TimeoutAction         string `json:"timeout_action"`
	SoundEnabled          bool   `json:"sound_enabled"`
	Autostart             bool   `json:"autostart"`
	MinimizeToTrayOnClose bool   `json:"minimize_to_tray_on_close"`
}

type HistoryConfig struct {
	MaxRecords    int `json:"max_records"`
	RetentionDays int `json:"retention_days"`
}

type LogConfig struct {
	Level      string `json:"level"`
	MaxSizeMB  int    `json:"max_size_mb"`
	MaxBackups int    `json:"max_backups"`
}

// LoadOrInit reads config.json from the per-user dir. If missing, writes
// defaults. If present but corrupt, renames the broken file aside, returns
// defaults plus a non-nil error so the caller can flag the user.
func LoadOrInit() (*Config, error) {
	p, err := FilePath("config.json")
	if err != nil {
		return defaults(), err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		cfg := defaults()
		return cfg, cfg.Save()
	}
	if err != nil {
		return defaults(), err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		broken := fmt.Sprintf("%s.broken-%d", p, time.Now().Unix())
		_ = os.Rename(p, broken)
		d := defaults()
		_ = d.Save()
		return d, fmt.Errorf("config corrupted, defaults loaded; original moved to %s", broken)
	}
	cfg.mu = &sync.RWMutex{}
	return &cfg, nil
}

func (c *Config) Save() error {
	p, err := FilePath("config.json")
	if err != nil {
		return err
	}
	c.mu.RLock()
	raw, err := json.MarshalIndent(c, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Snapshot returns a deep-ish copy safe to share with frontend (no mutex).
func (c *Config) Snapshot() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := Config{
		Server:    c.Server,
		Endpoints: append([]EndpointConfig(nil), c.Endpoints...),
		UI:        c.UI,
		Behavior:  c.Behavior,
		History:   c.History,
		Log:       c.Log,
	}
	return cp
}

// Apply replaces the live config under the lock.
func (c *Config) Apply(next Config) {
	c.mu.Lock()
	c.Server = next.Server
	c.Endpoints = append([]EndpointConfig(nil), next.Endpoints...)
	c.UI = next.UI
	c.Behavior = next.Behavior
	c.History = next.History
	c.Log = next.Log
	c.mu.Unlock()
}
