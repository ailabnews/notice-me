//go:build !darwin && !windows

package sound

// play is a no-op on platforms we don't ship to (e.g. Linux dev).
func play() {}
