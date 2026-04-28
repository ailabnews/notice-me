package autostart

const appName = "notify-me"

// Set enables or disables autostart for the current user.
func Set(enabled bool, exePath string) error { return setOS(enabled, exePath) }

// IsEnabled reports the current state.
func IsEnabled() (bool, error) { return isEnabledOS() }
