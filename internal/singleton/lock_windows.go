//go:build windows

package singleton

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

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

func listenIPC(_ string, onActivate func(), onHook HookHandler) (func(), error) {
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
			go handleIPCConn(c, onActivate, onHook)
		}
	}()
	return func() { _ = ln.Close() }, nil
}

func handleIPCConn(c net.Conn, onActivate func(), onHook HookHandler) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Minute))

	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 256<<10), 256<<10)
	if !scanner.Scan() {
		onActivate()
		return
	}
	data := scanner.Bytes()

	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) == nil {
		if _, ok := probe["hook_event_name"]; ok && onHook != nil {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Monitor for client disconnect.
			go func() {
				buf := make([]byte, 1)
				_ = c.SetReadDeadline(time.Time{})
				_, err := c.Read(buf)
				if err != nil {
					cancel()
				}
			}()

			resp, err := onHook(ctx, data)
			if err != nil {
				resp = []byte(`{"error":"` + err.Error() + `"}`)
			}
			_, _ = c.Write(resp)
			_, _ = c.Write([]byte("\n"))
			return
		}
	}

	onActivate()
}

func nudgeIPC(_ string) error {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ipcPort), 3*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Write([]byte("activate\n"))
	return err
}

func hookIPCCancel(ctx context.Context, _ string, body []byte) ([]byte, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", ipcPort)
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("notify-me daemon not running: %w", err)
	}

	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-closed:
		}
	}()

	if _, err := c.Write(body); err != nil {
		return nil, fmt.Errorf("ipc write: %w", err)
	}
	if _, err := c.Write([]byte("\n")); err != nil {
		return nil, fmt.Errorf("ipc write newline: %w", err)
	}

	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 256<<10), 256<<10)
	if !scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("no response from notify-me daemon")
	}
	return scanner.Bytes(), nil
}

func hookIPC(_ string, body []byte) ([]byte, error) {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ipcPort), 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("notify-me daemon not running: %w", err)
	}
	defer c.Close()

	if _, err := c.Write(body); err != nil {
		return nil, fmt.Errorf("ipc write: %w", err)
	}
	if _, err := c.Write([]byte("\n")); err != nil {
		return nil, fmt.Errorf("ipc write newline: %w", err)
	}

	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 256<<10), 256<<10)
	if !scanner.Scan() {
		return nil, fmt.Errorf("no response from notify-me daemon")
	}
	return scanner.Bytes(), nil
}
