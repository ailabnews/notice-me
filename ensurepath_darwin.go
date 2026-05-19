//go:build darwin

package main

import (
	"fmt"
	"os"
)

// EnsureInPath creates a symlink in a standard bin directory (macOS).
func (a *App) EnsureInPath() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前路径失败: %w", err)
	}

	targetDir := "/usr/local/bin"
	if _, err := os.Stat(targetDir); err != nil {
		targetDir = "/opt/homebrew/bin"
	}

	link := targetDir + "/notify-me"
	os.Remove(link)
	if err := os.Symlink(exe, link); err != nil {
		return fmt.Errorf("创建符号链接失败，请手动执行:\nsudo ln -sf %s %s", exe, link)
	}
	return nil
}
