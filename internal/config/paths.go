package config

import (
	"os"
	"path/filepath"
)

const appDirName = ".notice-me"

// ConfigDir returns the per-user config dir (~/.notice-me), creating it if
// missing. Honours NOTIFY_ME_CONFIG_HOME for tests.
func ConfigDir() (string, error) {
	if override := os.Getenv("NOTIFY_ME_CONFIG_HOME"); override != "" {
		dir := filepath.Join(override, appDirName)
		return dir, os.MkdirAll(dir, 0o755)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, appDirName)
	return dir, os.MkdirAll(dir, 0o755)
}

// FilePath returns <ConfigDir>/<name>.
func FilePath(name string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}
