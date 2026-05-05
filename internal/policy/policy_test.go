package policy

import (
	"fmt"
	"sync"
	"testing"
)

// mockStore implements PolicyStore for testing.
type mockStore struct {
	mu    sync.Mutex
	rules []PolicyRule
	err   error
}

func (m *mockStore) ListPolicyRules() ([]PolicyRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PolicyRule{}, m.rules...), m.err
}

func TestMatchGlobalRule(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules([]PolicyRule{
		{ToolName: "Bash", Pattern: "git *", Enabled: true, Priority: 10},
	}, nil)

	ok, rule := e.Match("Bash", "sess1", "git commit -m fix")
	if !ok {
		t.Fatal("expected match")
	}
	if rule.Pattern != "git *" {
		t.Fatalf("unexpected pattern: %s", rule.Pattern)
	}
}

func TestMatchSessionRule(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules(nil, map[string][]PolicyRule{
		"sess1": {
			{ToolName: "Bash", Pattern: "docker *", Enabled: true, Priority: 5},
		},
	})

	ok, _ := e.Match("Bash", "sess1", "docker build .")
	if !ok {
		t.Fatal("expected session rule to match")
	}

	ok, _ = e.Match("Bash", "sess2", "docker build .")
	if ok {
		t.Fatal("session rule should not match different session")
	}
}

func TestNoMatchWrongTool(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules([]PolicyRule{
		{ToolName: "Bash", Pattern: "git *", Enabled: true, Priority: 10},
	}, nil)

	ok, _ := e.Match("Edit", "sess1", "git *")
	if ok {
		t.Fatal("should not match different tool")
	}
}

func TestDisabledRuleSkipped(t *testing.T) {
	// Disabled filtering happens in Reload, not SetRules (raw setter).
	store := &mockStore{
		rules: []PolicyRule{
			{Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "git *", Enabled: false, Priority: 10},
		},
	}
	e := NewEngine(store)

	ok, _ := e.Match("Bash", "sess1", "git commit -m fix")
	if ok {
		t.Fatal("disabled rule should not match")
	}
}

func TestCommandPrefixMatch(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules([]PolicyRule{
		{ToolName: "Bash", Pattern: "git *", Enabled: true, Priority: 10},
	}, nil)

	// Should match: starts with "git "
	ok, _ := e.Match("Bash", "sess1", "git commit -m fix")
	if !ok {
		t.Fatal("expected 'git commit' to match 'git *'")
	}

	// Should match: starts with "git "
	ok, _ = e.Match("Bash", "sess1", "git push")
	if !ok {
		t.Fatal("expected 'git push' to match 'git *'")
	}

	// Should NOT match: different command
	ok, _ = e.Match("Bash", "sess1", "npm test")
	if ok {
		t.Fatal("'npm test' should not match 'git *'")
	}

	// Should NOT match: "git" alone (no space after prefix)
	ok, _ = e.Match("Bash", "sess1", "git")
	if ok {
		t.Fatal("'git' alone should not match 'git *' (no trailing space)")
	}
}

func TestFilePathMatch(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules([]PolicyRule{
		{ToolName: "Edit", Pattern: "/src/*.go", Enabled: true, Priority: 10},
	}, nil)

	ok, _ := e.Match("Edit", "sess1", "/src/main.go")
	if !ok {
		t.Fatal("expected /src/main.go to match /src/*.go")
	}

	// path.Match does not cross / with *
	ok, _ = e.Match("Edit", "sess1", "/src/sub/main.go")
	if ok {
		t.Fatal("/src/sub/main.go should not match /src/*.go (no / crossing)")
	}

	ok, _ = e.Match("Edit", "sess1", "/lib/main.go")
	if ok {
		t.Fatal("/lib/main.go should not match /src/*.go")
	}
}

func TestWildcardMatchAll(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules([]PolicyRule{
		{ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 1},
	}, nil)

	ok, _ := e.Match("Bash", "sess1", "anything at all")
	if !ok {
		t.Fatal("wildcard pattern '*' should match everything")
	}
}

func TestWildcardToolName(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules([]PolicyRule{
		{ToolName: "*", Pattern: "*", Enabled: true, Priority: 1},
	}, nil)

	tools := []string{"Bash", "Edit", "Write", "Read", "Glob", "Grep"}
	for _, tool := range tools {
		ok, _ := e.Match(tool, "sess1", "whatever")
		if !ok {
			t.Fatalf("wildcard tool '*' should match tool %q", tool)
		}
	}
}

func TestPriorityOrder(t *testing.T) {
	// Priority sorting happens in Reload, not SetRules (raw setter).
	store := &mockStore{
		rules: []PolicyRule{
			{Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 1},
			{Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "git *", Enabled: true, Priority: 10},
		},
	}
	e := NewEngine(store)

	ok, rule := e.Match("Bash", "sess1", "git commit")
	if !ok {
		t.Fatal("expected match")
	}
	if rule.Priority != 10 {
		t.Fatalf("expected highest priority rule (10), got %d", rule.Priority)
	}
}

func TestExtractContent(t *testing.T) {
	tests := []struct {
		toolName string
		command  string
		filePath string
		want     string
	}{
		{"Bash", "git commit -m fix", "/some/file.go", "git commit -m fix"},
		{"Edit", "", "/src/main.go", "/src/main.go"},
		{"Write", "content", "/out.txt", "/out.txt"},
		{"Read", "", "/foo.go", "Read"},
		{"Glob", "", "", "Glob"},
	}
	for _, tt := range tests {
		got := ExtractContent(tt.toolName, tt.command, tt.filePath)
		if got != tt.want {
			t.Errorf("ExtractContent(%q, %q, %q) = %q, want %q",
				tt.toolName, tt.command, tt.filePath, got, tt.want)
		}
	}
}

func TestDerivePattern(t *testing.T) {
	tests := []struct {
		toolName string
		command  string
		filePath string
		want     string
	}{
		{"Bash", "git commit -m fix", "", "git *"},
		{"Bash", "npm", "", "npm *"},
		{"Bash", "docker build .", "", "docker *"},
		{"Edit", "", "/src/main.go", "/src/*"},
		{"Write", "", "/app/views/index.html", "/app/views/*"},
		{"Read", "", "/foo.go", "*"},
		{"Glob", "", "", "*"},
	}
	for _, tt := range tests {
		got := DerivePattern(tt.toolName, tt.command, tt.filePath)
		if got != tt.want {
			t.Errorf("DerivePattern(%q, %q, %q) = %q, want %q",
				tt.toolName, tt.command, tt.filePath, got, tt.want)
		}
	}
}

func TestReloadFromStore(t *testing.T) {
	store := &mockStore{
		rules: []PolicyRule{
			{Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "git *", Enabled: true, Priority: 5},
			{Type: RuleTypeSession, SessionID: "s1", ToolName: "Edit", Pattern: "/tmp/*", Enabled: true, Priority: 3},
			{Type: RuleTypeGlobal, ToolName: "Bash", Pattern: "rm *", Enabled: false, Priority: 10},
		},
	}

	e := NewEngine(store)

	// Should match the enabled global rule
	ok, _ := e.Match("Bash", "any", "git push")
	if !ok {
		t.Fatal("expected match on enabled global rule after reload")
	}

	// Should match session rule
	ok, _ = e.Match("Edit", "s1", "/tmp/foo.go")
	if !ok {
		t.Fatal("expected match on session rule after reload")
	}

	// Disabled rule should not match
	ok, _ = e.Match("Bash", "any", "rm -rf /")
	if ok {
		t.Fatal("disabled rule should not match")
	}
}

func TestReloadStoreError(t *testing.T) {
	store := &mockStore{err: fmt.Errorf("db error")}
	e := NewEngine(store)

	// Engine should still function (no rules), no panic
	ok, _ := e.Match("Bash", "any", "git push")
	if ok {
		t.Fatal("should not match with no rules")
	}
}

func TestReloadNilStore(t *testing.T) {
	e := NewEngine(nil)
	// Should not panic
	e.Reload()

	e.SetRules([]PolicyRule{
		{ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 1},
	}, nil)

	ok, _ := e.Match("Bash", "any", "anything")
	if !ok {
		t.Fatal("should match after SetRules")
	}
}

func TestMatchNoRules(t *testing.T) {
	e := NewEngine(nil)
	ok, _ := e.Match("Bash", "any", "git push")
	if ok {
		t.Fatal("should not match with no rules")
	}
}

func TestGlobalBeforeSession(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules(
		[]PolicyRule{
			{ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 5},
		},
		map[string][]PolicyRule{
			"sess1": {
				{ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 10},
			},
		},
	)

	ok, rule := e.Match("Bash", "sess1", "anything")
	if !ok {
		t.Fatal("expected match")
	}
	// Global rules are checked first, so global (priority 5) should win
	if rule.Priority != 5 {
		t.Fatalf("expected global rule (priority 5) to be matched first, got priority %d", rule.Priority)
	}
}

func TestConcurrentMatch(t *testing.T) {
	e := NewEngine(nil)
	e.SetRules([]PolicyRule{
		{ToolName: "Bash", Pattern: "git *", Enabled: true, Priority: 10},
	}, nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _ := e.Match("Bash", "sess1", "git commit")
			if !ok {
				t.Error("concurrent match should succeed")
			}
		}()
	}
	wg.Wait()
}
