package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"notify-me/internal/config"
	"notify-me/internal/dispatcher"
	"notify-me/internal/policy"
	"notify-me/internal/storage"
)

// policyStoreAdapter wraps *storage.Storage to implement policy.PolicyStore.
// The storage method takes context but the policy interface does not;
// we use context.Background() as the adapter's default.
type policyStoreAdapter struct {
	db *storage.Storage
}

func (a *policyStoreAdapter) ListPolicyRules() ([]policy.PolicyRule, error) {
	return a.db.ListPolicyRules(context.Background())
}

// newPolicyTestServer creates a Server with real SQLite storage and a real
// policy engine, wired up for integration testing.
func newPolicyTestServer(t *testing.T, onActive func(*dispatcher.Notification)) (*Server, *dispatcher.Dispatcher, *storage.Storage, *policy.Engine, context.CancelFunc) {
	t.Helper()
	t.Setenv("NOTIFY_ME_CONFIG_HOME", t.TempDir())
	cfg, err := config.LoadOrInit()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(config.Config{
		Server:    config.ServerConfig{Host: "127.0.0.1", Port: 0, EndpointPrefix: "/api", MaxQueueSize: 4},
		Endpoints: []config.EndpointConfig{},
		Behavior:  config.BehaviorConfig{DefaultTimeoutSeconds: 5, TimeoutAction: "timeout", StopHookEnabled: true},
	})

	// Real SQLite storage.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Real policy engine backed by the storage.
	polEngine := policy.NewEngine(&policyStoreAdapter{db: db})

	var d *dispatcher.Dispatcher
	d = dispatcher.New(dispatcher.Options{
		QueueSize: 4,
		OnActive:  onActive,
	})
	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx)

	s := New(cfg, d, db, polEngine, zerolog.Nop())

	// Wire OnSessionAuth the same way app.go does.
	s.OnSessionAuth = func(sessionID, toolName, pattern string) {
		now := time.Now().UnixMilli()
		db.InsertPolicyRule(ctx, policy.PolicyRule{
			Type:      policy.RuleTypeSession,
			SessionID: sessionID,
			ToolName:  toolName,
			Pattern:   pattern,
			Enabled:   true,
			Source:    policy.SourcePopup,
			CreatedAt: now,
			UpdatedAt: now,
		})
		polEngine.Reload()
	}

	return s, d, db, polEngine, cancel
}

func TestPolicyAutoApprove_GlobalRule(t *testing.T) {
	// Set up a global rule that auto-approves "git *" commands via Bash.
	s, _, db, polEngine, cancel := newPolicyTestServer(t, func(n *dispatcher.Notification) {
		// This should NOT be called if auto-approve works.
		t.Error("OnActive should not be called for auto-approved requests")
		go n.Resolve(dispatcher.Result{Decision: n.OkText})
	})
	defer cancel()

	now := time.Now().UnixMilli()
	db.InsertPolicyRule(context.Background(), policy.PolicyRule{
		Type: policy.RuleTypeGlobal, ToolName: "Bash", Pattern: "git *",
		Enabled: true, Priority: 100, Source: policy.SourceManual,
		CreatedAt: now, UpdatedAt: now,
	})
	polEngine.Reload()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git commit -m fix"}}`
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
		t.Fatalf("expected auto-allow, got %+v", out)
	}
	if out.HookSpecificOutput.PermissionDecisionRsn != "notify-me: auto-approved by policy rule" {
		t.Fatalf("expected auto-approved reason, got %q", out.HookSpecificOutput.PermissionDecisionRsn)
	}
}

func TestPolicyNoMatch_ShowsPopup(t *testing.T) {
	// Set up a global rule for "git *" only; request uses "npm test" which should NOT match.
	popupActivated := make(chan string, 1)
	s, _, db, polEngine, cancel := newPolicyTestServer(t, func(n *dispatcher.Notification) {
		popupActivated <- n.Title
		go n.Resolve(dispatcher.Result{Decision: n.OkText})
	})
	defer cancel()

	now := time.Now().UnixMilli()
	db.InsertPolicyRule(context.Background(), policy.PolicyRule{
		Type: policy.RuleTypeGlobal, ToolName: "Bash", Pattern: "git *",
		Enabled: true, Priority: 100, Source: policy.SourceManual,
		CreatedAt: now, UpdatedAt: now,
	})
	polEngine.Reload()

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
	// Should get "allow" because the popup resolved with OkText ("允许"), which maps to "approved" -> "allow".
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("expected allow (from popup), got %+v", out)
	}
	// The reason should be user-approved, not auto-approved.
	if !strings.Contains(out.HookSpecificOutput.PermissionDecisionRsn, "notify-me") {
		t.Fatalf("expected user-approved reason, got %q", out.HookSpecificOutput.PermissionDecisionRsn)
	}

	// Verify popup was actually shown (not auto-approved).
	select {
	case title := <-popupActivated:
		if !strings.Contains(title, "确认执行") {
			t.Fatalf("popup title = %q, expected to contain '确认执行'", title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("popup was not activated within 2s")
	}
}

func TestPolicyAutoSession_CreatesSessionRule(t *testing.T) {
	// Simulate: first request goes to popup, user picks "auto_session",
	// which creates a session rule. Second request with the same tool should auto-approve.
	var resolveDecision string
	s, _, _, _, cancel := newPolicyTestServer(t, func(n *dispatcher.Notification) {
		go n.Resolve(dispatcher.Result{Decision: resolveDecision})
	})
	defer cancel()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// First request: popup resolves with "auto_session".
	// The resolveHandler maps "auto_session" -> calls OnSessionAuth -> then resolves as "approved".
	resolveDecision = "auto_session"

	// We need to send the request through the _resolve endpoint to trigger auto_session.
	// But first, let's send a PreToolUse request that will block on the popup.
	// The popup will be resolved via the resolve endpoint with decision=auto_session.

	reqDone := make(chan struct{})
	var firstResp *http.Response
	go func() {
		defer close(reqDone)
		body := `{"hook_event_name":"PreToolUse","session_id":"sess-auto","tool_name":"Bash","tool_input":{"command":"docker build ."}}`
		resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
		if err != nil {
			t.Error(err)
			return
		}
		firstResp = resp
	}()

	// Wait a moment for the popup to be activated and then resolve via _resolve endpoint.
	time.Sleep(200 * time.Millisecond)

	// Resolve the notification with auto_session decision via the internal endpoint.
	// The notification ID should be 1 (first insert).
	resolveURL := ts.URL + "/api/_resolve?id=1&decision=auto_session"
	resp, err := http.Post(resolveURL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("resolve status %d", resp.StatusCode)
	}

	// Wait for the first request to complete.
	<-reqDone
	if firstResp != nil {
		firstResp.Body.Close()
	}

	// Now the session rule should exist. Second request with same session should auto-approve.
	body2 := `{"hook_event_name":"PreToolUse","session_id":"sess-auto","tool_name":"Bash","tool_input":{"command":"docker build ."}}`
	resp2, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 200 {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("second request status %d body %q", resp2.StatusCode, b)
	}

	var out2 claudeHookOutput
	if err := json.NewDecoder(resp2.Body).Decode(&out2); err != nil {
		t.Fatal(err)
	}
	if out2.HookSpecificOutput == nil || out2.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("expected auto-allow on second request, got %+v", out2)
	}
	if out2.HookSpecificOutput.PermissionDecisionRsn != "notify-me: auto-approved by policy rule" {
		t.Fatalf("expected auto-approved reason on second request, got %q", out2.HookSpecificOutput.PermissionDecisionRsn)
	}
}

func TestPolicyAutoApprove_PermissionRequest(t *testing.T) {
	// Verify that PermissionRequest events also check the policy engine.
	s, _, db, polEngine, cancel := newPolicyTestServer(t, func(n *dispatcher.Notification) {
		t.Error("OnActive should not be called for auto-approved requests")
		go n.Resolve(dispatcher.Result{Decision: n.OkText})
	})
	defer cancel()

	now := time.Now().UnixMilli()
	db.InsertPolicyRule(context.Background(), policy.PolicyRule{
		Type: policy.RuleTypeGlobal, ToolName: "Bash", Pattern: "*",
		Enabled: true, Priority: 100, Source: policy.SourceManual,
		CreatedAt: now, UpdatedAt: now,
	})
	polEngine.Reload()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	body := `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`
	resp, err := http.Post(ts.URL+"/api/claude/hook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out claudeHookOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.Decision == nil {
		t.Fatalf("expected decision, got %+v", out)
	}
	if out.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Fatalf("expected allow, got %q", out.HookSpecificOutput.Decision.Behavior)
	}
}
