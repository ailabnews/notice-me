//go:build !darwin && !windows

package main

import (
	"fmt"
	"os"
)

// showStartupErrorOS prints to stderr on platforms without a supported native
// dialog (most importantly Linux dev hosts).
func showStartupErrorOS(msg string) {
	fmt.Fprintln(os.Stderr, "[notify-me] startup error:", msg)
}
