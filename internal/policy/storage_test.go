package policy_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"notify-me/internal/policy"
	"notify-me/internal/storage"
)

func setupTestDB(t *testing.T) *storage.Storage {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestListPolicyRulesEmpty(t *testing.T) {
	db := setupTestDB(t)
	rules, err := db.ListPolicyRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

func TestInsertAndListPolicyRules(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	id, err := db.InsertPolicyRule(ctx, policy.PolicyRule{
		Type: policy.RuleTypeGlobal, ToolName: "Bash", Pattern: "npm *",
		Enabled: true, Priority: 100, Source: policy.SourceManual,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}

	rules, err := db.ListPolicyRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	r := rules[0]
	if r.ToolName != "Bash" {
		t.Errorf("ToolName = %q", r.ToolName)
	}
	if r.Pattern != "npm *" {
		t.Errorf("Pattern = %q", r.Pattern)
	}
	if !r.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestUpdatePolicyRule(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	id, _ := db.InsertPolicyRule(ctx, policy.PolicyRule{
		Type: policy.RuleTypeGlobal, ToolName: "Bash", Pattern: "*",
		Enabled: true, Priority: 50, Source: policy.SourceManual,
		CreatedAt: now, UpdatedAt: now,
	})

	err := db.UpdatePolicyRule(ctx, policy.PolicyRule{
		ID: id, Type: policy.RuleTypeGlobal, ToolName: "Bash", Pattern: "git *",
		Enabled: false, Priority: 90, UpdatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}

	rules, _ := db.ListPolicyRules(ctx)
	if len(rules) != 1 {
		t.Fatal("expected 1 rule")
	}
	if rules[0].Pattern != "git *" {
		t.Errorf("Pattern = %q", rules[0].Pattern)
	}
	if rules[0].Enabled {
		t.Error("should be disabled")
	}
}

func TestDeletePolicyRule(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	id, _ := db.InsertPolicyRule(ctx, policy.PolicyRule{
		Type: policy.RuleTypeGlobal, ToolName: "Bash", Pattern: "*",
		Enabled: true, Priority: 50, Source: policy.SourceManual,
		CreatedAt: now, UpdatedAt: now,
	})

	if err := db.DeletePolicyRule(ctx, id); err != nil {
		t.Fatal(err)
	}
	rules, _ := db.ListPolicyRules(ctx)
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestCleanupInactiveSessionRules(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// Insert a session rule for an inactive session.
	db.InsertPolicyRule(ctx, policy.PolicyRule{
		Type: policy.RuleTypeSession, SessionID: "old-session",
		ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 0,
		Source: policy.SourcePopup, CreatedAt: now, UpdatedAt: now,
	})

	// No notifications for "old-session" -> should be cleaned up.
	if err := db.CleanupInactiveSessionRules(ctx); err != nil {
		t.Fatal(err)
	}

	rules, _ := db.ListPolicyRules(ctx)
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after cleanup, got %d", len(rules))
	}
}

func TestCleanupKeepsActiveSessionRules(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// Insert a session rule.
	db.InsertPolicyRule(ctx, policy.PolicyRule{
		Type: policy.RuleTypeSession, SessionID: "active-session",
		ToolName: "Bash", Pattern: "*", Enabled: true, Priority: 0,
		Source: policy.SourcePopup, CreatedAt: now, UpdatedAt: now,
	})

	// Insert a notification for that session (makes it "active").
	db.Insert(ctx, storage.Record{
		Endpoint: "test", Title: "t", Message: "m",
		SessionID: "active-session", Status: "approved", CreatedAt: now,
	})

	if err := db.CleanupInactiveSessionRules(ctx); err != nil {
		t.Fatal(err)
	}

	rules, _ := db.ListPolicyRules(ctx)
	if len(rules) != 1 {
		t.Errorf("expected 1 rule (active session), got %d", len(rules))
	}
}
