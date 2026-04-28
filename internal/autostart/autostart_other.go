//go:build !darwin && !windows

package autostart

// Linux dev box: no autostart wiring — calls succeed silently.
func setOS(enabled bool, exePath string) error { return nil }
func isEnabledOS() (bool, error)               { return false, nil }
