//go:build !darwin && !windows

package autostart

import "testing"

// On non-darwin, non-windows platforms the autostart implementation is a no-op,
// so there is nothing on disk to verify.
func verifyPlistRemoved(t *testing.T) { t.Helper() }
