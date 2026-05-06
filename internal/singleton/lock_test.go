//go:build !windows

package singleton

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireReleaseRoundtrip(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")
	if err := acquire(lockPath); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Second acquire after release should succeed.
	if err := acquire(lockPath); err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	_ = release()
}

func TestSecondAcquireBlocks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")
	if err := acquire(lockPath); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()
	// Try acquire from a different file descriptor -- in a real second process
	// this would be a fresh fd; we simulate with os.OpenFile + Flock directly.
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = flockNB(f)
	if err == nil {
		t.Fatal("expected EWOULDBLOCK on second flock")
	}
}

func TestIPCActivate(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, ".sock")
	var fired atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	closer, err := listenIPC(sock, func() {
		fired.Add(1)
		wg.Done()
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if err := nudgeIPC(sock); err != nil {
		t.Fatal(err)
	}
	waitOrTimeout(t, &wg, 200*time.Millisecond)
	if fired.Load() != 1 {
		t.Fatalf("fired=%d", fired.Load())
	}
}

func waitOrTimeout(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("timeout")
	}
}
