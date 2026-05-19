//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnsureInPath adds the directory containing the notify-me binary to the
// user's PATH environment variable via `setx` (Windows).
func (a *App) EnsureInPath() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前路径失败: %w", err)
	}
	binDir := filepath.Dir(exe)

	// Check if already in current process PATH.
	current := os.Getenv("PATH")
	for _, p := range filepath.SplitList(current) {
		if strings.EqualFold(p, binDir) {
			return nil
		}
	}

	// Build new PATH: keep existing user PATH, append binDir.
	// Use `powershell` to read the persistent user PATH (not just the
	// inherited process PATH, which may differ).
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`[Environment]::GetEnvironmentVariable('Path','User')`).Output()
	if err != nil {
		return fmt.Errorf("读取用户 PATH 失败: %w", err)
	}
	userPath := strings.TrimSpace(string(out))

	newPath := userPath
	if newPath != "" && !strings.HasSuffix(newPath, ";") {
		newPath += ";"
	}
	newPath += binDir

	// Use `setx` to persist the user PATH permanently.
	if err := exec.Command("setx", "PATH", newPath).Run(); err != nil {
		return fmt.Errorf("写入用户 PATH 失败: %w", err)
	}

	// Also update the current process so the change takes effect immediately.
	os.Setenv("PATH", current+";"+binDir)
	return nil
}
