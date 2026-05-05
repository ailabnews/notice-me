# Audit Policy Feature Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "审核" tab with global policy engine and session auto-approve rules, plus a "会话授权" button in the notification popup.

**Architecture:** New `internal/policy` package provides an in-memory rule matching engine with SQLite-backed storage. The policy engine sits in front of the dispatcher — matching requests bypass the popup entirely. Session rules are created from the popup via an extended `_resolve` endpoint. Frontend uses Wails service bindings for CRUD.

**Tech Stack:** Go 1.25 + Wails v3 alpha.78 + Vue 3 + Pinia + SQLite + TDesign Vue Next

**Spec:** `docs/superpowers/specs/2026-05-05-audit-policy-design.md`

**Deferred from spec:** `**` recursive glob expansion for file path matching. MVP uses `path.Match` only (`*` does not cross `/`). Can be added later if needed.

**Type ownership:** `PolicyRule` is defined in `internal/policy/types.go`. The `storage` package imports `policy` and its methods accept/return `policy.PolicyRule`. No circular dependency since `policy.PolicyStore` interface takes no storage types — the adapter in `app.go` bridges them.

---

## File Structure

### New files
- `internal/policy/types.go` — PolicyRule struct, constants
- `internal/policy/policy.go` — Engine (matching, cache, pattern matching)
- `internal/policy/storage.go` — PolicyStore interface + storage methods on `*storage.Storage`
- `frontend/src/views/AuditView.vue` — Audit tab view
- `frontend/src/stores/audit.ts` — Pinia store for policy rules
- `internal/policy/policy_test.go` — Engine unit tests
- `internal/policy/storage_test.go` — Storage integration tests
- `internal/server/claude_hook_policy_test.go` — Auto-approve integration tests

### Modified files
- `internal/storage/schema.go` — Add `policy_rules` table + `notifications.resolved_by_rule_id` migration
- `internal/storage/storage.go` — Add `ResolvedByRuleID` to Record, expand `UpdateStatus`, add policy query methods, update `DecisionStats`, update all `Scan` calls
- `internal/dispatcher/notification.go` — Add `SessionID`, `ToolContent` fields
- `internal/dispatcher/dispatcher.go` — Add `GetInFlight` method
- `internal/server/server.go` — Inject policy engine, update `HistoryStore` interface, update `New`
- `internal/server/claude_hook.go` — Add `extractContent`, policy matching before popup, `auto_session` in resolve
- `internal/window/window.go` — Pass `has_session_auth` param to popup
- `app.go` — Add policy Wails methods, wire engine in Boot
- `frontend/src/App.vue` — Add audit tab
- `frontend/src/PopupApp.vue` — Add session auth button
- `frontend/src/style.css` — Add popup session-auth button styles + audit view styles

---

### Task 1: Policy types and storage schema

**Files:**
- Create: `internal/policy/types.go`
- Modify: `internal/storage/schema.go`
- Modify: `internal/storage/storage.go`

- [ ] **Step 1: Write the test for PolicyRule struct and constants**

```go
// internal/policy/types_test.go
package policy

import "testing"

func TestRuleTypeConstants(t *testing.T) {
	if RuleTypeGlobal != "global" {
		t.Errorf("RuleTypeGlobal = %q, want %q", RuleTypeGlobal, "global")
	}
	if RuleTypeSession != "session" {
		t.Errorf("RuleTypeSession = %q, want %q", RuleTypeSession, "session")
	}
	if SourceManual != "manual" {
		t.Errorf("SourceManual = %q, want %q", SourceManual, "manual")
	}
	if SourcePopup != "popup" {
		t.Errorf("SourcePopup = %q, want %q", SourcePopup, "popup")
	}
}

func TestPolicyRuleFields(t *testing.T) {
	r := PolicyRule{
		Type:      RuleTypeGlobal,
		ToolName:  "Bash",
		Pattern:   "npm *",
		Enabled:   true,
		Priority:  100,
		Source:    SourceManual,
	}
	if r.Type != "global" {
		t.Errorf("Type = %q", r.Type)
	}
	if !r.Enabled {
		t.Error("Enabled should be true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/policy/ -run TestRuleType -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Create `internal/policy/types.go`**

```go
package policy

// Rule type constants.
const (
	RuleTypeGlobal  = "global"
	RuleTypeSession = "session"
	SourceManual    = "manual"
	SourcePopup     = "popup"
)

// PolicyRule represents a single auto-approve rule.
type PolicyRule struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`       // "global" | "session"
	SessionID string `json:"session_id"` // non-empty for type="session"
	ToolName  string `json:"tool_name"`  // tool name or "*"
	Pattern   string `json:"pattern"`    // glob pattern, default "*"
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`
	Source    string `json:"source"` // "manual" | "popup"
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/policy/ -v`
Expected: PASS

- [ ] **Step 5: Add schema migration in `internal/storage/schema.go`**

Add to `migrations` slice:
```go
// v0.3.0 → v0.4.0: policy engine + audit trail.
`ALTER TABLE notifications ADD COLUMN resolved_by_rule_id INTEGER DEFAULT NULL`,
```

Add new table creation after migrations in `Open`:
```go
// policy_rules table for auto-approve engine.
if _, err := db.Exec(policyRulesSchema); err != nil {
    return nil, err
}
if _, err := db.Exec(policyRulesIndexes); err != nil {
    return nil, err
}
```

New constants:
```go
const policyRulesSchema = `
CREATE TABLE IF NOT EXISTS policy_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL,
    session_id  TEXT NOT NULL DEFAULT '',
    tool_name   TEXT NOT NULL,
    pattern     TEXT NOT NULL DEFAULT '*',
    enabled     INTEGER NOT NULL DEFAULT 1,
    priority    INTEGER NOT NULL DEFAULT 0,
    source      TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
)`

const policyRulesIndexes = `
CREATE INDEX IF NOT EXISTS idx_policy_rules_type ON policy_rules(type, enabled, priority DESC);
CREATE INDEX IF NOT EXISTS idx_policy_rules_session ON policy_rules(session_id);
`
```

- [ ] **Step 6: Update `internal/storage/storage.go` — Record struct, UpdateStatus, Scan paths, DecisionStats, add policy methods**

Add `ResolvedByRuleID` to `Record`:
```go
ResolvedByRuleID int64  `json:"-"` // 0 = no rule matched
```

Expand `UpdateStatus` signature:
```go
func (s *Storage) UpdateStatus(ctx context.Context, id int64, status string, resolvedAt int64, ruleID int64) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE notifications SET status=?, resolved_at=?,
        duration_ms = CASE WHEN created_at > 0 THEN ?-created_at ELSE 0 END,
        resolved_by_rule_id = CASE WHEN ? > 0 THEN ? ELSE resolved_by_rule_id END
        WHERE id=?`,
		status, resolvedAt, resolvedAt, ruleID, ruleID, id)
	return err
}
```

Update all `Scan` calls to include `resolved_by_rule_id` — add `var ruleID sql.NullInt64` and scan it as the last column. Set `r.ResolvedByRuleID = ruleID.Int64`.

Update SELECT column lists in `Get`, `List`, `RecentBySession` to include `resolved_by_rule_id`.

Update `DecisionStats` switch to include `"auto_approved"`:
```go
case "approved", "允许", "acknowledged", "知道了", "auto_approved":
    ds.Approved += cnt
```

Add policy storage methods (import `"notify-me/internal/policy"`, use `policy.PolicyRule`):
```go
func (s *Storage) ListPolicyRules(ctx context.Context) ([]policy.PolicyRule, error)
func (s *Storage) InsertPolicyRule(ctx context.Context, r policy.PolicyRule) (int64, error)
func (s *Storage) UpdatePolicyRule(ctx context.Context, r policy.PolicyRule) error
func (s *Storage) DeletePolicyRule(ctx context.Context, id int64) error
func (s *Storage) CleanupInactiveSessionRules(ctx context.Context) error
```

**Important:** Must fix ALL callers of `UpdateStatus` — add `0` as ruleID arg: `s.db.UpdateStatus(ctx, id, status, ts, 0)` throughout `server.go`, `claude_hook.go`, AND all test helpers (`fakeStorage` in `internal/server/handler_test.go`, `claude_hook_test.go`, and any other test files implementing `HistoryStore`). Also update `newTestServer` calls to pass a nil policy engine as the new parameter.

- [ ] **Step 7: Run all tests**

Run: `go test ./... -race`
Expected: PASS (existing tests updated with new UpdateStatus signature)

- [ ] **Step 8: Commit**

```bash
git add internal/policy/types.go internal/policy/types_test.go internal/storage/schema.go internal/storage/storage.go
git commit -m "feat: add policy_rules table, types, and storage methods"
```

---

### Task 2: Policy matching engine

**Files:**
- Create: `internal/policy/policy.go`
- Create: `internal/policy/policy_test.go`

- [ ] **Step 1: Write failing tests for the engine**

```go
// internal/policy/policy_test.go
package policy

import "testing"

func TestMatchGlobalRule(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 1, Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "npm *", Enabled: true, Priority: 100},
		{ID: 2, Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "go *", Enabled: true, Priority: 90},
	}
	matched, rule := e.Match("Bash", "sess-1", "npm test")
	if !matched {
		t.Fatal("expected match")
	}
	if rule.ID != 1 {
		t.Errorf("rule.ID = %d, want 1", rule.ID)
	}
}

func TestMatchSessionRule(t *testing.T) {
	e := NewEngine(nil)
	e.sessionRules = map[string][]PolicyRule{
		"sess-1": {
			{ID: 10, Type: RuleTypeSession, SessionID: "sess-1", ToolName: "Edit", Pattern: "/Users/x/proj/*", Enabled: true},
		},
	}
	matched, _ := e.Match("Edit", "sess-1", "/Users/x/proj/main.go")
	if !matched {
		t.Fatal("expected match")
	}
}

func TestNoMatchWrongTool(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 1, Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 100},
	}
	matched, _ := e.Match("Edit", "sess-1", "something")
	if matched {
		t.Fatal("should not match different tool")
	}
}

func TestDisabledRuleSkipped(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 1, Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "*", Enabled: false, Priority: 100},
	}
	matched, _ := e.Match("Bash", "sess-1", "anything")
	if matched {
		t.Fatal("disabled rules should not match")
	}
}

func TestCommandPrefixMatch(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 1, ToolName: "Bash", Pattern: "git *", Enabled: true, Priority: 100},
	}
	matched, _ := e.Match("Bash", "s", "git commit -m fix")
	if !matched {
		t.Fatal("git * should match git commit ...")
	}
	matched, _ = e.Match("Bash", "s", "npm test")
	if matched {
		t.Fatal("git * should not match npm test")
	}
}

func TestFilePathMatch(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 1, ToolName: "Edit", Pattern: "/Users/x/proj/src/*", Enabled: true, Priority: 100},
	}
	matched, _ := e.Match("Edit", "s", "/Users/x/proj/src/main.go")
	if !matched {
		t.Fatal("should match file in directory")
	}
	matched, _ = e.Match("Edit", "s", "/Users/x/proj/src/sub/util.go")
	if matched {
		t.Fatal("* should not cross / in path.Match")
	}
}

func TestWildcardMatchAll(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 1, ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 100},
	}
	matched, _ := e.Match("Bash", "s", "anything at all")
	if !matched {
		t.Fatal("* should match all")
	}
}

func TestWildcardToolName(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 1, ToolName: "*", Pattern: "*", Enabled: true, Priority: 100},
	}
	matched, _ := e.Match("Edit", "s", "whatever")
	if !matched {
		t.Fatal("* tool should match any tool")
	}
}

func TestPriorityOrder(t *testing.T) {
	e := NewEngine(nil)
	e.globalRules = []PolicyRule{
		{ID: 2, ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 50},
		{ID: 1, ToolName: "Bash", Pattern: "npm *", Enabled: true, Priority: 100},
	}
	matched, rule := e.Match("Bash", "s", "npm test")
	if !matched {
		t.Fatal("expected match")
	}
	if rule.ID != 1 {
		t.Errorf("should match higher priority rule, got %d", rule.ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/policy/ -run TestMatch -v`
Expected: FAIL — `NewEngine` undefined

- [ ] **Step 3: Implement `internal/policy/policy.go`**

```go
package policy

import (
	"path"
	"sort"
	"strings"
	"sync"
)

// PolicyStore is the interface for loading/saving rules.
type PolicyStore interface {
	ListPolicyRules() ([]PolicyRule, error)
}

// Engine performs in-memory rule matching for auto-approve decisions.
type Engine struct {
	store PolicyStore

	mu           sync.RWMutex
	globalRules  []PolicyRule // sorted by priority DESC
	sessionRules map[string][]PolicyRule
}

// NewEngine creates an engine, loading rules from the store.
func NewEngine(store PolicyStore) *Engine {
	e := &Engine{store: store, sessionRules: map[string][]PolicyRule{}}
	if store != nil {
		e.Reload()
	}
	return e
}

// Reload fetches all rules from the store and rebuilds the in-memory cache.
func (e *Engine) Reload() {
	if e.store == nil {
		return
	}
	rules, err := e.store.ListPolicyRules()
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.globalRules = nil
	e.sessionRules = map[string][]PolicyRule{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch r.Type {
		case RuleTypeGlobal:
			e.globalRules = append(e.globalRules, r)
		case RuleTypeSession:
			e.sessionRules[r.SessionID] = append(e.sessionRules[r.SessionID], r)
		}
	}
	sort.Slice(e.globalRules, func(i, j int) bool {
		return e.globalRules[i].Priority > e.globalRules[j].Priority
	})
}

// Match checks if a tool invocation matches any auto-approve rule.
// Returns (true, rule) if matched, (false, nil) otherwise.
func (e *Engine) Match(toolName, sessionID, content string) (bool, *PolicyRule) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 1. Global rules (priority sorted)
	for i := range e.globalRules {
		r := &e.globalRules[i]
		if matchTool(r.ToolName, toolName) && matchPattern(r.ToolName, r.Pattern, content) {
			return true, r
		}
	}
	// 2. Session rules
	sessRules := e.sessionRules[sessionID]
	for i := range sessRules {
		r := &sessRules[i]
		if matchTool(r.ToolName, toolName) && matchPattern(r.ToolName, r.Pattern, content) {
			return true, r
		}
	}
	return false, nil
}

// matchTool returns true if the rule's tool name matches the request tool.
func matchTool(ruleTool, reqTool string) bool {
	return ruleTool == "*" || ruleTool == reqTool
}

// matchPattern dispatches to the correct matching strategy based on tool type.
// File tools (Edit, Write) use path.Match; command tools use prefix matching.
func matchPattern(toolName, pattern, content string) bool {
	if pattern == "*" {
		return true
	}
	switch toolName {
	case "Edit", "Write":
		matched, _ := path.Match(pattern, content)
		return matched
	default:
		// Command matching: prefix with space separator
		if strings.HasSuffix(pattern, " *") {
			prefix := pattern[:len(pattern)-2]
			return strings.HasPrefix(content, prefix+" ")
		}
		return content == pattern
	}
}

// ExtractContent derives the matchable content string from tool input.
func ExtractContent(toolName string, command, filePath string) string {
	switch toolName {
	case "Bash":
		return command
	case "Edit", "Write":
		return filePath
	default:
		return toolName
	}
}

// DerivePattern auto-generates a pattern from tool input for popup "session auth".
func DerivePattern(toolName string, command, filePath string) string {
	switch toolName {
	case "Bash":
		// Extract binary name: first word of the command
		cmd := strings.TrimSpace(command)
		if idx := strings.IndexByte(cmd, ' '); idx > 0 {
			return cmd[:idx] + " *"
		}
		return cmd + " *"
	case "Edit", "Write":
		// Directory + /*
		dir := path.Dir(filePath)
		return dir + "/*"
	default:
		return "*"
	}
}

// SetRules replaces the in-memory rules (for testing).
func (e *Engine) SetRules(global []PolicyRule, session map[string][]PolicyRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.globalRules = global
	e.sessionRules = session
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/policy/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/policy/policy.go internal/policy/policy_test.go
git commit -m "feat: policy matching engine with in-memory cache"
```

---

### Task 3: Dispatcher GetInFlight + Notification fields

**Files:**
- Modify: `internal/dispatcher/notification.go`
- Modify: `internal/dispatcher/dispatcher.go`

- [ ] **Step 1: Add `SessionID` and `ToolContent` to Notification struct in `internal/dispatcher/notification.go`**

After the `HasDiff` field, add:
```go
// Policy engine fields.
SessionID   string // session ID for session-scoped auto-approve
ToolContent string // extracted content for pattern matching (command or file_path)
```

- [ ] **Step 2: Add `GetInFlight` method to `internal/dispatcher/dispatcher.go`**

```go
// GetInFlight returns the in-flight notification by ID, or nil if not found.
func (d *Dispatcher) GetInFlight(id int64) *Notification {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inFlight[id]
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/dispatcher/ -race -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/dispatcher/notification.go internal/dispatcher/dispatcher.go
git commit -m "feat: add SessionID/ToolContent to Notification, GetInFlight to Dispatcher"
```

---

### Task 4: Wire policy engine into server + claude_hook

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/claude_hook.go`

- [ ] **Step 1: Update `internal/server/server.go` — inject policy engine, update interface**

Add import: `"notify-me/internal/policy"`

Update `HistoryStore` interface to include new `UpdateStatus` signature:
```go
type HistoryStore interface {
	Insert(ctx context.Context, r storage.Record) (int64, error)
	UpdateStatus(ctx context.Context, id int64, status string, resolvedAt int64, ruleID int64) error
	List(ctx context.Context, f storage.ListFilter) ([]storage.Record, int, error)
	Delete(ctx context.Context, id int64) error
	DeleteAll(ctx context.Context) error
}
```

Add to `Server` struct:
```go
policy *policy.Engine
```

Update `New`:
```go
func New(cfg *config.Config, d *dispatcher.Dispatcher, db HistoryStore, pol *policy.Engine, log zerolog.Logger) *Server {
	return &Server{cfg: cfg, disp: d, db: db, policy: pol, log: log, DiffStore: diff.NewStore()}
}
```

Fix all `UpdateStatus` calls in `server.go` to pass `0` as ruleID.

- [ ] **Step 2: Update `internal/server/claude_hook.go` — add policy matching + auto_session handling**

Add import: `"notify-me/internal/policy"`

Add `extractContent` helper:
```go
func extractContent(toolName string, ti claudeToolInput) string {
	return policy.ExtractContent(toolName, ti.Command, ti.FilePath)
}
```

Update `processPreToolUse` — add policy check before popup:
```go
func (s *Server) processPreToolUse(ctx context.Context, req *claudeHookRequest) claudeHookOutput {
	toolInput := parseToolInput(req.ToolInput)
	content := extractContent(req.ToolName, toolInput)

	// Policy engine: auto-approve if matched.
	if s.policy != nil {
		if matched, rule := s.policy.Match(req.ToolName, req.SessionID, content); matched {
			s.autoApprove(ctx, req, content, rule.ID)
			return claudeHookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:         "PreToolUse",
					PermissionDecision:    "allow",
					PermissionDecisionRsn: "notify-me: auto-approved by policy rule",
				},
			}
		}
	}

	title, message, okText, cancelText, mode := buildPreToolUsePopup(req, toolInput)
	// ... existing diff + blockingPopup code ...
```

Inside `blockingPopup`, after setting `n.SourceHdr`, add:
```go
n.SessionID = sessionID  // already a parameter
n.ToolContent = ""       // callers set this before enqueue if needed
```

In `processPreToolUse` and `processPermissionRequest`, after creating the notification but before `blockingPopup`, pass `content` as an additional parameter to `blockingPopup`, or set `n.ToolContent = content` inside those functions. The simplest approach: add `toolContent string` parameter to `blockingPopup`, and set `n.ToolContent = toolContent` inside.
```

Similarly update `processPermissionRequest` with the same pattern.

Add `autoApprove` helper:
```go
func (s *Server) autoApprove(ctx context.Context, req *claudeHookRequest, content string, ruleID int64) {
	now := time.Now().UnixMilli()
	id, err := s.db.Insert(ctx, storage.Record{
		Endpoint:         "claude/hook",
		Title:            "确认执行: " + req.ToolName,
		Message:          formatToolMessage(req.ToolName, parseToolInput(req.ToolInput)),
		SourceIP:         "127.0.0.1",
		SourceHeader:     "claude-hook",
		SessionID:        req.SessionID,
		ToolName:         req.ToolName,
		ToolInputSummary: truncate(formatToolMessage(req.ToolName, parseToolInput(req.ToolInput)), 200),
		HookEvent:        req.HookEventName,
		TranscriptPath:   req.TranscriptPath,
		Status:           "auto_approved",
		CreatedAt:        now,
	})
	if err != nil {
		return
	}
	_ = s.db.UpdateStatus(ctx, id, "auto_approved", now, ruleID)
}
```

Update `resolveHandler` to handle `auto_session`:
```go
func (s *Server) resolveHandler(w http.ResponseWriter, r *http.Request) {
	// ... existing id/decision parsing ...
	if decision == "auto_session" {
		s.handleAutoSession(id)
		decision = "approved"
	}
	s.disp.Resolve(id, dispatcher.Result{Decision: decision})
	// ... rest unchanged ...
}

func (s *Server) handleAutoSession(id int64) {
	n := s.disp.GetInFlight(id)
	if n == nil {
		return
	}
	var pattern string
	switch n.ToolName {
	case "Bash":
		pattern = policy.DerivePattern("Bash", n.ToolContent, "")
	case "Edit", "Write":
		pattern = policy.DerivePattern(n.ToolName, "", n.ToolContent)
	default:
		pattern = "*"
	}
	if s.OnSessionAuth != nil {
		s.OnSessionAuth(n.SessionID, n.ToolName, pattern)
	}
}
```

Add `OnSessionAuth` callback to `Server` struct:
```go
OnSessionAuth func(sessionID, toolName, pattern string)
```

Update `blockingPopup` to set `SessionID` and `ToolContent` on the notification (pass them through from callers).

Fix all `UpdateStatus` calls in `claude_hook.go` to include ruleID `0`.

- [ ] **Step 3: Update `app.go` — create policy engine, wire callbacks**

Add import: `"notify-me/internal/policy"`

In `Boot`, after `a.db` init:
```go
// Policy engine.
a.policy = policy.NewEngine(&policyStoreAdapter{db: a.db})
```

Update `server.New` call:
```go
a.server = server.New(a.cfg, a.disp, a.db, a.policy, a.log)
```

Add `OnSessionAuth` callback:
```go
a.server.OnSessionAuth = func(sessionID, toolName, pattern string) {
	now := time.Now().UnixMilli()
	a.db.InsertPolicyRule(a.ctx, policy.PolicyRule{
		Type:      policy.RuleTypeSession,
		SessionID: sessionID,
		ToolName:  toolName,
		Pattern:   pattern,
		Enabled:   true,
		Priority:  0,
		Source:    policy.SourcePopup,
		CreatedAt: now,
		UpdatedAt: now,
	})
	a.policy.Reload()
}
```

Add policy Wails methods:
```go
func (a *App) GetPolicyRules() string {
	if a.db == nil { return "[]" }
	rules, err := a.db.ListPolicyRules(a.ctx)
	if err != nil { return "[]" }
	b, _ := json.Marshal(rules)
	return string(b)
}

func (a *App) AddPolicyRule(ruleJSON string) error {
	var r policy.PolicyRule
	if err := json.Unmarshal([]byte(ruleJSON), &r); err != nil { return err }
	r.CreatedAt = time.Now().UnixMilli()
	r.UpdatedAt = r.CreatedAt
	_, err := a.db.InsertPolicyRule(a.ctx, r)
	if err != nil { return err }
	a.policy.Reload()
	return nil
}

func (a *App) UpdatePolicyRule(ruleJSON string) error {
	var r policy.PolicyRule
	if err := json.Unmarshal([]byte(ruleJSON), &r); err != nil { return err }
	r.UpdatedAt = time.Now().UnixMilli()
	err := a.db.UpdatePolicyRule(a.ctx, r)
	if err != nil { return err }
	a.policy.Reload()
	return nil
}

func (a *App) DeletePolicyRule(id int64) error {
	err := a.db.DeletePolicyRule(a.ctx, id)
	if err != nil { return err }
	a.policy.Reload()
	return nil
}

func (a *App) CleanupSessionRules() error {
	err := a.db.CleanupInactiveSessionRules(a.ctx)
	if err != nil { return err }
	a.policy.Reload()
	return nil
}
```

Add `policyStoreAdapter` that implements `policy.PolicyStore`:
```go
type policyStoreAdapter struct {
	db *storage.Storage
}

func (p *policyStoreAdapter) ListPolicyRules() ([]policy.PolicyRule, error) {
	return p.db.ListPolicyRules(context.Background())
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./... -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/claude_hook.go app.go
git commit -m "feat: wire policy engine into server auto-approve and resolve"
```

---

### Task 5: Frontend — Audit tab view + store

**Files:**
- Create: `frontend/src/stores/audit.ts`
- Create: `frontend/src/views/AuditView.vue`
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/style.css`

- [ ] **Step 1: Create `frontend/src/stores/audit.ts`**

```typescript
import { defineStore } from 'pinia'
import { Call } from '@wailsio/runtime'

export interface PolicyRule {
  id: number
  type: 'global' | 'session'
  session_id: string
  tool_name: string
  pattern: string
  enabled: boolean
  priority: number
  source: 'manual' | 'popup'
  created_at: number
  updated_at: number
}

export const useAudit = defineStore('audit', {
  state: () => ({
    rules: [] as PolicyRule[],
    loading: false,
    error: '',
  }),
  getters: {
    globalRules: (s) => s.rules.filter(r => r.type === 'global'),
    sessionRules: (s) => s.rules.filter(r => r.type === 'session'),
  },
  actions: {
    async load() {
      this.loading = true
      this.error = ''
      try {
        const raw = await Call.ByName('main.App.GetPolicyRules')
        this.rules = JSON.parse(raw as string || '[]')
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      } finally {
        this.loading = false
      }
    },
    async add(rule: Omit<PolicyRule, 'id' | 'created_at' | 'updated_at'>) {
      this.error = ''
      try {
        await Call.ByName('main.App.AddPolicyRule', JSON.stringify(rule))
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
    async update(rule: PolicyRule) {
      this.error = ''
      try {
        await Call.ByName('main.App.UpdatePolicyRule', JSON.stringify(rule))
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
    async remove(id: number) {
      this.error = ''
      try {
        await Call.ByName('main.App.DeletePolicyRule', id)
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
    async cleanup() {
      this.error = ''
      try {
        await Call.ByName('main.App.CleanupSessionRules')
        await this.load()
      } catch (e: any) {
        this.error = e?.message ?? String(e)
      }
    },
  },
})
```

- [ ] **Step 2: Create `frontend/src/views/AuditView.vue`**

A Vue component with two tables (global rules top, session rules bottom), add/edit/delete dialogs using TDesign components (matching existing pattern in HomeView/HistoryView). Key sections:

- Top table: columns = [启用 switch, 工具名, 命令模式, 优先级, 操作(edit/delete)]
- Bottom table: columns = [会话ID, 工具名, 命令/路径模式, 来源, 添加时间, 操作(delete)]
- "清理已结束会话" button
- "+ 新增规则" dialog with fields: tool_name (input), pattern (input), priority (number)
- Edit dialog for global rules

Uses `useAudit` store. Loads on mount. Uses card-based layout consistent with HomeView.

- [ ] **Step 3: Update `frontend/src/App.vue` — add audit tab**

```vue
<button :class="{ active: tab === 'audit' }" @click="tab = 'audit'">审核</button>
```

Import `AuditView`. Add to tab type union and computed:
```typescript
case 'audit': return AuditView
```

- [ ] **Step 4: Add audit view styles to `frontend/src/style.css`**

Add styles for the audit tables, rule editor, and session list following existing card/table patterns.

- [ ] **Step 5: Build frontend and verify**

Run: `cd frontend && npm run build && cd ..`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add frontend/src/stores/audit.ts frontend/src/views/AuditView.vue frontend/src/App.vue frontend/src/style.css
git commit -m "feat: audit tab with policy engine and session rules UI"
```

---

### Task 6: Popup — session auth button

**Files:**
- Modify: `frontend/src/PopupApp.vue`
- Modify: `frontend/src/style.css`
- Modify: `internal/window/window.go`

- [ ] **Step 1: Update `internal/window/window.go` — pass `has_session_auth` param**

In `OpenPopup`, after building query params, check if the payload indicates a session-auth-eligible event:
```go
// Session auth button: show for PreToolUse and PermissionRequest events only.
hookEvent, _ := payload["hook_event"].(string)
if hookEvent == "PreToolUse" || hookEvent == "PermissionRequest" {
    q.Set("has_session_auth", "true")
}
```
This requires the `blockingPopup` callers in `claude_hook.go` to pass `hook_event` in the payload map (via `onActive` in `app.go`). Currently `onActive` builds the payload from notification fields — add `n.HookEvent` to the payload.

- [ ] **Step 2: Update `frontend/src/PopupApp.vue` — add session auth button**

Add new ref:
```typescript
const hasSessionAuth = ref(false)
```

Set in `onMounted`:
```typescript
hasSessionAuth.value = p.get('has_session_auth') === 'true'
```

Add resolve function:
```typescript
async function sessionAuth() {
  if (!resolveBase || !id.value) return
  try {
    await fetch(`${resolveBase}?id=${id.value}&decision=auto_session`, { method: 'POST' })
  } catch {
    // Go side will close the popup
  }
}
```

Add button in template, after the ok button:
```vue
<button v-if="hasSessionAuth" class="session-auth" @click="sessionAuth">会话授权</button>
```

- [ ] **Step 3: Add popup session-auth button style to `frontend/src/style.css`**

```css
.popup-actions .session-auth {
  flex: 0 0 auto;
  padding: 10px 16px;
  background: #3b82f6;
  color: #fff;
  border-color: #3b82f6;
  border-radius: 8px;
  border: 1px solid #3b82f6;
  cursor: pointer;
  font-size: 14px;
  font-weight: 500;
  transition: opacity .15s;
}
.popup-actions .session-auth:active { opacity: .75; }
```

- [ ] **Step 4: Build frontend**

Run: `cd frontend && npm run build && cd ..`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add frontend/src/PopupApp.vue frontend/src/style.css internal/window/window.go
git commit -m "feat: add session auth button to popup"
```

---

### Task 7: Integration tests

**Files:**
- Create: `internal/server/claude_hook_policy_test.go`
- Create: `internal/policy/storage_test.go`

- [ ] **Step 1: Write storage integration tests in `internal/policy/storage_test.go`**

Test InsertPolicyRule, ListPolicyRules, UpdatePolicyRule, DeletePolicyRule, CleanupInactiveSessionRules using `t.TempDir()` + `NOTIFY_ME_CONFIG_HOME`.

- [ ] **Step 2: Write auto-approve integration tests in `internal/server/claude_hook_policy_test.go`**

Test:
1. Request matching a global rule → auto-approved, no popup
2. Request not matching any rule → popup shown
3. `auto_session` resolve → creates session rule, subsequent request auto-approved
4. Disabled rule → not matched

- [ ] **Step 3: Run all tests**

Run: `go test ./... -race -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/claude_hook_policy_test.go internal/policy/storage_test.go
git commit -m "test: policy engine storage and auto-approve integration tests"
```

---

### Task 8: Final build verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -race`
Expected: PASS

- [ ] **Step 2: Build frontend**

Run: `cd frontend && npm run build && cd ..`
Expected: Build succeeds

- [ ] **Step 3: Verify Windows cross-compile**

Run: `GOOS=windows GOARCH=amd64 go build ./...`
Expected: PASS

- [ ] **Step 4: Run vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: complete audit policy feature — global + session auto-approve rules"
```
