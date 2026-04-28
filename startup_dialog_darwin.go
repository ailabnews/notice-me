//go:build darwin

package main

import "os/exec"

func showStartupErrorOS(msg string) {
	script := `display dialog "` + escapeAS(msg) + `" buttons {"OK"} with icon stop`
	_ = exec.Command("osascript", "-e", script).Run()
}

// escapeAS escapes double quotes for embedding inside an AppleScript string
// literal. Backslash itself is not special to `display dialog` text, so quote
// escaping is sufficient.
func escapeAS(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return string(out)
}
