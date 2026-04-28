package sound

// Play plays a short notification sound. Best-effort — errors swallowed.
// Implementation lives in sound_<os>.go.
func Play() { play() }
