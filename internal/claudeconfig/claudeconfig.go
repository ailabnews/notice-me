// Package claudeconfig reads and writes Claude Code's ~/.claude/settings.json
// to manage hook configuration for notify-me.
package claudeconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SettingsPath returns the path to the user's global Claude Code settings.json.
func SettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// HookStatus describes the current hook configuration state.
type HookStatus struct {
	Configured    bool     `json:"configured"`
	Mode          string   `json:"mode"` // "http", "stdio", or ""
	SettingsPath  string   `json:"settings_path"`
	HooksFound    []string `json:"hooks_found"`
	Installed     bool     `json:"installed"`
	BinaryPath    string   `json:"binary_path"`
	ClaudeVersion string   `json:"claude_version"`
	Error         string   `json:"error,omitempty"`
}

// GetStatus checks whether notify-me hooks are configured and whether
// Claude Code is installed on the system.
func GetStatus() (*HookStatus, error) {
	p, err := SettingsPath()
	if err != nil {
		return nil, err
	}
	status := &HookStatus{SettingsPath: p}

	// Detect Claude Code binary.
	// macOS .app bundles inherit a minimal PATH, so we also search via
	// the user's login shell and common installation directories.
	if path := findClaude(); path != "" {
		status.Installed = true
		status.BinaryPath = path
		status.ClaudeVersion = detectClaudeVersion(path)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil // not configured
		}
		return nil, err
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		return status, nil
	}

	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		return status, nil
	}

	// Scan hook entries for notify-me URLs or commands.
	for event, arr := range hooks {
		var matchers []struct {
			Hooks []struct {
				Type    string `json:"type"`
				URL     string `json:"url"`
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(arr, &matchers); err != nil {
			continue
		}
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if h.Type == "http" && (containsNM(h.URL)) {
					status.Configured = true
					status.Mode = "http"
					status.HooksFound = append(status.HooksFound, event)
				} else if h.Type == "command" && containsNM(h.Command) {
					status.Configured = true
					status.Mode = "stdio"
					status.HooksFound = append(status.HooksFound, event)
				}
			}
		}
	}
	return status, nil
}

// Configure writes notify-me hook entries into Claude Code's settings.json.
// It replaces the entire "hooks" section with the program's default configuration.
// All other top-level settings keys are preserved unchanged.
// mode is "http" or "stdio". enableStopHook controls whether Stop and
// StopFailure events are included.
func Configure(mode, httpURL string, enableStopHook bool) error {
	p, err := SettingsPath()
	if err != nil {
		return err
	}

	// Read existing settings (or start fresh).
	settings := make(map[string]any)
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &settings)
	}

	// Build the hook entry for the chosen mode.
	makeHook := func() map[string]any {
		entry := map[string]any{}
		if mode == "http" {
			entry["type"] = "http"
			entry["url"] = httpURL
		} else {
			entry["type"] = "command"
			entry["command"] = "notify-me hook"
		}
		return entry
	}

	// Build the complete hooks configuration, replacing any existing hooks.
	hooks := map[string]any{
		"PreToolUse": []any{
			map[string]any{"matcher": "Bash", "hooks": []any{makeHook()}},
			map[string]any{"matcher": "Edit|Write", "hooks": []any{makeHook()}},
		},
		"PermissionRequest": []any{
			map[string]any{"hooks": []any{makeHook()}},
		},
		"Notification": []any{
			map[string]any{"matcher": "idle_prompt", "hooks": []any{makeHook()}},
		},
	}

	if enableStopHook {
		hooks["Stop"] = []any{
			map[string]any{"hooks": []any{makeHook()}},
		}
		hooks["StopFailure"] = []any{
			map[string]any{"hooks": []any{makeHook()}},
		}
	}

	// Replace hooks entirely; other settings keys stay unchanged.
	settings["hooks"] = hooks
	return writeSettings(p, settings)
}

// Remove strips notify-me hook entries from Claude Code's settings.json.
func Remove() error {
	p, err := SettingsPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parse settings: %w", err)
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		return nil
	}

	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		return nil
	}

	// Remove matchers that contain notify-me hooks.
	for event, arr := range hooks {
		var matchers []json.RawMessage
		_ = json.Unmarshal(arr, &matchers)
		var filtered []json.RawMessage
		for _, m := range matchers {
			if !matcherContainsNM(m) {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			delete(hooks, event)
		} else {
			filtRaw, _ := json.Marshal(filtered)
			hooks[event] = filtRaw
		}
	}

	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		hooksRaw, _ := json.Marshal(hooks)
		settings["hooks"] = hooksRaw
	}

	// Re-marshal the whole settings.
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(p, append(out, '\n'))
}

// ─── helpers ───

// findClaude locates the Claude Code binary. It tries:
//  1. exec.LookPath (works when launched from a terminal)
//  2. The user's login shell (handles macOS .app bundles)
//  3. Common absolute paths as a last resort
func findClaude() string {
	// Fast path: standard PATH lookup.
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}

	// macOS .app bundles get a sterile PATH. Ask the user's login shell.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, shell, "-l", "-c", "which claude").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" && !strings.HasPrefix(p, "which:") {
			return p
		}
	}

	// Fallback: common installation directories.
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".npm/bin/claude"),
			filepath.Join(home, ".local/bin/claude"),
		)
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

func containsNM(s string) bool {
	return len(s) > 0 && (contains(s, "127.0.0.1:1886") || contains(s, "notify-me") || contains(s, "notify_me"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && (startsWith(s, sub) || contains(s[1:], sub)))
}

func startsWith(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := range prefix {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}

func matcherContainsNM(m json.RawMessage) bool {
	var parsed struct {
		Hooks []struct {
			URL     string `json:"url"`
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if json.Unmarshal(m, &parsed) != nil {
		return false
	}
	for _, h := range parsed.Hooks {
		if containsNM(h.URL) || containsNM(h.Command) {
			return true
		}
	}
	return false
}

func writeSettings(p string, settings map[string]any) error {
	// Extract hooks so they are written at the end of the JSON output.
	hooksVal := settings["hooks"]
	delete(settings, "hooks")

	mainOut, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	if hooksVal == nil {
		return atomicWrite(p, append(mainOut, '\n'))
	}

	// Marshal hooks with extra indentation so content aligns under the key.
	hooksOut, err := json.MarshalIndent(hooksVal, "  ", "  ")
	if err != nil {
		return err
	}
	// Strip the leading 2-space prefix so the opening brace sits after "hooks": .
	hooksStr := strings.TrimPrefix(string(hooksOut), "  ")

	mainStr := string(mainOut)
	var result string
	if mainStr == "{}" {
		result = "{\n  \"hooks\": " + hooksStr + "\n}\n"
	} else {
		// Remove trailing "\n}" and append hooks + closing brace.
		result = mainStr[:len(mainStr)-2] + ",\n  \"hooks\": " + hooksStr + "\n}\n"
	}
	return atomicWrite(p, []byte(result))
}

func atomicWrite(p string, data []byte) error {
	dir := filepath.Dir(p)
	_ = os.MkdirAll(dir, 0o755)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// detectClaudeVersion runs `claude --version` with a short timeout and
// returns the trimmed output. Returns empty string on any error.
func detectClaudeVersion(binPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
