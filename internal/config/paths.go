package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "notify-me"

// ConfigDir returns the per-user config dir for notify-me, creating it if
// missing. Honours NOTIFY_ME_CONFIG_HOME for tests.
func ConfigDir() (string, error) {
	if override := os.Getenv("NOTIFY_ME_CONFIG_HOME"); override != "" {
		dir := filepath.Join(override, appDirName)
		return dir, os.MkdirAll(dir, 0o755)
	}
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
	default:
		cfg, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		base = cfg
	}
	dir := filepath.Join(base, appDirName)
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
