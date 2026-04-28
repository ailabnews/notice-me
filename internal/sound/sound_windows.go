//go:build windows

package sound

import "syscall"

func play() {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBeep")
	proc.Call(uintptr(0x40)) // MB_ICONINFORMATION
}
