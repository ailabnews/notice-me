package logger

import (
	"bytes"
	"strings"
	"testing"

	"notify-me/internal/config"
)

func TestLoggerWritesInfoByDefault(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{Log: config.LogConfig{Level: "info"}}
	log := newWith(cfg, &buf)
	log.Info().Msg("hello")
	log.Debug().Msg("muted")
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Fatalf("info missing: %s", out)
	}
	if strings.Contains(out, "muted") {
		t.Fatalf("debug should be muted: %s", out)
	}
}

func TestLoggerDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := &config.Config{Log: config.LogConfig{Level: "debug"}}
	log := newWith(cfg, &buf)
	log.Debug().Msg("verbose")
	if !strings.Contains(buf.String(), "verbose") {
		t.Fatalf("debug missing: %s", buf.String())
	}
}

func TestLoggerLevelMatrix(t *testing.T) {
	cases := []struct {
		level   string
		emit    string // method invoked
		wantHit bool
	}{
		{"debug", "debug", true},
		{"info", "debug", false},
		{"info", "info", true},
		{"warn", "info", false},
		{"warn", "warn", true},
		{"error", "warn", false},
		{"error", "error", true},
		{"unknown", "info", true},  // unknown falls back to info
		{"unknown", "debug", false},
		{"", "info", true}, // empty falls back to info
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		cfg := &config.Config{Log: config.LogConfig{Level: tc.level}}
		log := newWith(cfg, &buf)
		switch tc.emit {
		case "debug":
			log.Debug().Msg("x")
		case "info":
			log.Info().Msg("x")
		case "warn":
			log.Warn().Msg("x")
		case "error":
			log.Error().Msg("x")
		}
		got := strings.Contains(buf.String(), `"x"`) || strings.Contains(buf.String(), `"message":"x"`)
		if got != tc.wantHit {
			t.Errorf("level=%q emit=%q want=%v got=%v out=%s", tc.level, tc.emit, tc.wantHit, got, buf.String())
		}
	}
}
