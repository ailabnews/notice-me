//go:build windows

package claudeconfig

import (
	"os/exec"
	"syscall"
)

// hideWindow sets the CREATE_NO_WINDOW flag on a Windows exec.Cmd so that
// no console window flashes when the command runs.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags = 0x08000000 // CREATE_NO_WINDOW
}
