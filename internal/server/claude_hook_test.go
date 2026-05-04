package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"notify-me/internal/config"
	"notify-me/internal/dispatcher"
)

// newTestServer creates a Server + dispatcher wired up for testing.
func newTestServer(t *testing.T, onActive func(*dispatcher.Notification)) (*Server, *dispatcher.Dispatcher, context.CancelFunc) {
	t.Helper()
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, err := config.LoadOrInit()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 5, TimeoutAction: "timeout"},
	})

	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{
		QueueSize: 4,
		OnActive:  onActive,
	})
	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx)

	s := New(cfg, d, &fakeStorage{}, zerolog.Nop())
	return s, d, cancel
}

func TestClaudeHook_PreToolUse_Allow(t *testing.T) {
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			// Popup sends the OK button label as the decision (real behavior).
			n.Resolve(dispatcher.Result{Decision: n.OkText})
		}()
	})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"npm test"}}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body %q", resp.StatusCode, b)
	}

	var out claudeHookOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("expected allow, got %+v", out)
	}
}

func TestClaudeHook_PreToolUse_Deny(t *testing.T) {
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			// Popup sends the Cancel button label as the decision (real behavior).
			n.Resolve(dispatcher.Result{Decision: n.CancelText})
		}()
	})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /tmp"}}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out claudeHookOutput
	json.NewDecoder(resp.Body).Decode(&out)
	if out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("expected deny, got %+v", out)
	}
}

func TestClaudeHook_PermissionRequest_Allow(t *testing.T) {
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {
		go func() {
			n.Resolve(dispatcher.Result{Decision: n.OkText})
		}()
	})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out claudeHookOutput
	json.NewDecoder(resp.Body).Decode(&out)
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Fatalf("expected allow, got %+v", out)
	}
}

func TestClaudeHook_PermissionDenied_Retry(t *testing.T) {
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"PermissionDenied","tool_name":"Bash","reason":"denied by classifier"}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out claudeHookOutput
	json.NewDecoder(resp.Body).Decode(&out)
	if out.HookSpecificOutput == nil || !out.HookSpecificOutput.Retry {
		t.Fatalf("expected retry=true, got %+v", out)
	}
}

func TestClaudeHook_Notification_FireAndForget(t *testing.T) {
	activated := make(chan string, 1)
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {
		activated <- n.Title
		// Auto-resolve so the dispatcher doesn't block.
		go n.Resolve(dispatcher.Result{Decision: "acknowledged"})
	})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"Notification","notification_type":"idle_prompt","message":"Claude is waiting"}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should return 200 immediately, not wait for popup dismissal.
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the popup was actually enqueued.
	select {
	case title := <-activated:
		if title != "Claude Code" {
			t.Fatalf("popup title = %q, want 'Claude Code'", title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("popup not activated within 2s")
	}
}

func TestClaudeHook_Stop_FireAndForget(t *testing.T) {
	activated := make(chan string, 1)
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {
		activated <- n.Title
		go n.Resolve(dispatcher.Result{Decision: "acknowledged"})
	})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"Stop","last_assistant_message":"Done!"}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	select {
	case title := <-activated:
		if !strings.Contains(title, "完成") {
			t.Fatalf("popup title = %q, want something with '完成'", title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("popup not activated")
	}
}

func TestClaudeHook_StopFailure_FireAndForget(t *testing.T) {
	activated := make(chan string, 1)
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {
		activated <- n.Title
		go n.Resolve(dispatcher.Result{Decision: "acknowledged"})
	})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"StopFailure","error":"rate_limit","error_details":"429 Too Many Requests"}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestClaudeHook_InvalidJSON(t *testing.T) {
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader("{bad"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestClaudeHook_MethodNotAllowed(t *testing.T) {
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/claude/hook")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestClaudeHook_UnknownEvent(t *testing.T) {
	s, _, cancel := newTestServer(t, func(n *dispatcher.Notification) {})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"SomeFutureEvent","session_id":"abc"}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for unknown event, got %d", resp.StatusCode)
	}
}

func TestClaudeHook_Paused(t *testing.T) {
	s, d, cancel := newTestServer(t, func(n *dispatcher.Notification) {})
	defer cancel()
	d.Pause()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out claudeHookOutput
	json.NewDecoder(resp.Body).Decode(&out)
	if out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("expected deny when paused, got %+v", out)
	}
}

func TestClaudeHook_NoAuthRequired(t *testing.T) {
	// Verify the claude/hook endpoint bypasses auth even when token is set.
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, _ := config.LoadOrInit()
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", AuthToken: "secret", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 5},
	})
	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{
		QueueSize: 4,
		OnActive:  func(n *dispatcher.Notification) { go n.Resolve(dispatcher.Result{Decision: n.OkText}) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	s := New(cfg, d, &fakeStorage{}, zerolog.Nop())
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// No auth header — should still work.
	body := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 (no auth required), got %d body %q", resp.StatusCode, b)
	}
}

func TestFormatToolMessageMd(t *testing.T) {
	cases := []struct {
		tool string
		ti   claudeToolInput
		want string
	}{
		{"Bash", claudeToolInput{Command: "npm test", Description: "Run tests"}, "Run tests"},
		{"Bash", claudeToolInput{Command: "npm test"}, "npm test"},
		{"Write", claudeToolInput{FilePath: "/src/main.go", Content: "package main"}, "/src/main.go"},
		{"Edit", claudeToolInput{FilePath: "/src/main.go", OldString: "foo"}, "/src/main.go"},
		{"Read", claudeToolInput{FilePath: "/src/main.go"}, "/src/main.go"},
		{"Grep", claudeToolInput{Pattern: "TODO"}, "TODO"},
	}
	for _, tc := range cases {
		got := formatToolMessageMd(tc.tool, tc.ti)
		if !strings.Contains(got, tc.want) {
			t.Errorf("formatToolMessageMd(%s) = %q, want containing %q", tc.tool, got, tc.want)
		}
	}
}

func TestIsDangerousTool(t *testing.T) {
	cases := []struct {
		tool string
		cmd  string
		want bool
	}{
		{"Bash", "rm -rf /tmp/foo", true},
		{"Bash", "git push --force origin main", true},
		{"Bash", "npm test", false},
		{"Bash", "ls -la", false},
		{"Write", "", false},
		{"Edit", "", false},
	}
	for _, tc := range cases {
		got := isDangerousTool(tc.tool, claudeToolInput{Command: tc.cmd})
		if got != tc.want {
			t.Errorf("isDangerousTool(%q, %q) = %v, want %v", tc.tool, tc.cmd, got, tc.want)
		}
	}
}

func TestClaudeHook_BuildPreToolUsePopup_DangerousCommand(t *testing.T) {
	req := &claudeHookRequest{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"rm -rf /tmp"}`)}
	ti := parseToolInput(req.ToolInput)
	title, _, _, _, mode := buildPreToolUsePopup(req, ti)
	if !strings.Contains(title, "危险") {
		t.Fatalf("expected danger title for rm -rf, got %q", title)
	}
	if mode != dispatcher.ModeTwoButton {
		t.Fatalf("expected two-button mode, got %v", mode)
	}
}
