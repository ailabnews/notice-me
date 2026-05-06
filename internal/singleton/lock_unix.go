//go:build !windows

package singleton

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
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

func listenIPC(path string, onActivate func(), onHook HookHandler) (func(), error) {
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
			go handleIPCConn(c, onActivate, onHook)
		}
	}()
	return func() { _ = ln.Close(); _ = os.Remove(path) }, nil
}

// handleIPCConn reads one message from an IPC connection and dispatches it.
// - Valid JSON with "hook_event_name" → hook request (request-response)
// - Anything else → activation nudge (fire-and-forget)
func handleIPCConn(c net.Conn, onActivate func(), onHook HookHandler) {
	defer c.Close()
	// Set a read deadline so we don't block forever on a misbehaving client.
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Minute))

	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 256<<10), 256<<10)
	if !scanner.Scan() {
		// No data — treat as nudge.
		onActivate()
		return
	}
	data := scanner.Bytes()

	// Detect hook request: valid JSON containing "hook_event_name".
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) == nil {
		if _, ok := probe["hook_event_name"]; ok && onHook != nil {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Monitor for client disconnect. When the client process exits
			// (e.g. user cancels in Claude Code terminal), the OS closes the
			// socket and this read returns an error, cancelling the context.
			go func() {
				buf := make([]byte, 1)
				_ = c.SetReadDeadline(time.Time{}) // clear initial deadline
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

	// Nudge or unrecognized — just activate.
	onActivate()
}

func nudgeIPC(path string) error {
	c, err := net.Dial("unix", path)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Write([]byte("activate\n"))
	return err
}

// hookIPCCancel is like hookIPC but closes the connection when ctx is done,
// unblocking the scanner read.
func hookIPCCancel(ctx context.Context, path string, body []byte) ([]byte, error) {
	c, err := net.DialTimeout("unix", path, 3*time.Second)
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

// hookIPC connects to the daemon's IPC socket, sends the hook request body,
// and returns the response.
func hookIPC(path string, body []byte) ([]byte, error) {
	c, err := net.DialTimeout("unix", path, 3*time.Second)
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

	// Read response. Blocking events can take a long time (user interaction).
	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 256<<10), 256<<10)
	if !scanner.Scan() {
		return nil, fmt.Errorf("no response from notify-me daemon")
	}
	return scanner.Bytes(), nil
}

// flockNB is a test helper for verifying second-acquire blocking.
func flockNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
