package main

// showStartupError surfaces a fatal pre-Wails error to the user via a native
// OS dialog. The actual implementation is selected at compile time via the
// per-OS files (startup_dialog_darwin.go, startup_dialog_windows.go,
// startup_dialog_other.go). We need this to be reachable before wailsApp.Run()
// initialises any UI of its own.
func showStartupError(msg string) { showStartupErrorOS(msg) }
