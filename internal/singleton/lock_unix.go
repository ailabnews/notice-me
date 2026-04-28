//go:build !windows

package singleton

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
)

var lockFile *os.File

func acquire(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("lock held: %w", err)
		}
		return err
	}
	lockFile = f
	return nil
}

func release() error {
	if lockFile == nil {
		return nil
	}
	_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	err := lockFile.Close()
	lockFile = nil
	return err
}

func isLockedErr(err error) bool {
	return err != nil && errors.Is(err, syscall.EWOULDBLOCK)
}

func listenIPC(path string, onActivate func()) (func(), error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
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
	return func() { _ = ln.Close(); _ = os.Remove(path) }, nil
}

func nudgeIPC(path string) error {
	c, err := net.Dial("unix", path)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Write([]byte("activate"))
	return err
}

// flockNB is a test helper for verifying second-acquire blocking.
func flockNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
