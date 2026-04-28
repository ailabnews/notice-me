package logger

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"

	"notify-me/internal/config"
)

// New returns a zerolog logger writing to <ConfigDir>/logs/notify-me.log with
// rotation, plus stderr.
func New(cfg *config.Config) zerolog.Logger {
	dir, err := config.ConfigDir()
	if err != nil {
		return zerolog.New(os.Stderr).With().Timestamp().Logger()
	}
	logPath := filepath.Join(dir, "logs", "notify-me.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return zerolog.New(os.Stderr).With().Timestamp().Logger()
	}
	file := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
	}
	multi := io.MultiWriter(os.Stderr, file)
	return newWith(cfg, multi)
}

func newWith(cfg *config.Config, w io.Writer) zerolog.Logger {
	lvl := zerolog.InfoLevel
	switch strings.ToLower(cfg.Log.Level) {
	case "debug":
		lvl = zerolog.DebugLevel
	case "warn":
		lvl = zerolog.WarnLevel
	case "error":
		lvl = zerolog.ErrorLevel
	}
	return zerolog.New(w).Level(lvl).With().Timestamp().Logger()
}
