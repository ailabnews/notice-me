//go:build windows

package singleton

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/windows"
)

var lockHandle windows.Handle

func acquire(path string) error {
	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, // no sharing — second instance will fail to open
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	lockHandle = h
	return nil
}

func release() error {
	if lockHandle == 0 {
		return nil
	}
	err := windows.CloseHandle(lockHandle)
	lockHandle = 0
	return err
}

func isLockedErr(err error) bool {
	if err == nil {
		return false
	}
	if os.IsExist(err) {
		return true
	}
	// ERROR_SHARING_VIOLATION = 32
	var errno windows.Errno
	if errors.As(err, &errno) {
		return uint32(errno) == 32
	}
	return false
}

// On Windows, use a TCP loopback port instead of UDS for IPC.
const ipcPort = 8861

func listenIPC(_ string, onActivate func()) (func(), error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ipcPort))
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 32)
			_, _ = c.Read(buf)
			onActivate()
			_ = c.Close()
		}
	}()
	return func() { _ = ln.Close() }, nil
}

func nudgeIPC(_ string) error {
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", ipcPort))
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Write([]byte("activate"))
	return err
}
