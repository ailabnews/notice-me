//go:build darwin

package sound

import "os/exec"

func play() {
	_ = exec.Command("afplay", "-v", "0.5", "/System/Library/Sounds/Glass.aiff").Start()
}
