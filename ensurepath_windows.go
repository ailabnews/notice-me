//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	// Use `powershell` to read the persistent user PATH.
	psCmd := exec.Command("powershell", "-NoProfile", "-Command",
		`[Environment]::GetEnvironmentVariable('Path','User')`)
	hideWinCmd(psCmd)
	out, err := psCmd.Output()
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
	setxCmd := exec.Command("setx", "PATH", newPath)
	hideWinCmd(setxCmd)
	if err := setxCmd.Run(); err != nil {
		return fmt.Errorf("写入用户 PATH 失败: %w", err)
	}

	// Also update the current process so the change takes effect immediately.
	os.Setenv("PATH", current+";"+binDir)
	return nil
}

// hideWinCmd prevents a console window from flashing when a command runs.
func hideWinCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags = 0x08000000 // CREATE_NO_WINDOW
}
