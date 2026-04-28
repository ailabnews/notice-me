//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func showStartupErrorOS(msg string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	title, _ := syscall.UTF16PtrFromString("notify-me")
	text, _ := syscall.UTF16PtrFromString(msg)
	// MB_OK | MB_ICONERROR == 0x10
	proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
