package storage

// schema defines the initial table and indexes. SQLite's CREATE IF NOT EXISTS
// handles fresh databases; migrations below handle existing ones.
const schema = `
CREATE TABLE IF NOT EXISTS notifications (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  endpoint      TEXT NOT NULL,
  title         TEXT NOT NULL,
  message       TEXT NOT NULL,
  source_ip     TEXT,
  source_header TEXT,
  session_id    TEXT,
  status        TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  resolved_at   INTEGER,
  duration_ms   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_created   ON notifications(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_status    ON notifications(status);
`

// postMigrationIndexes are indexes that depend on columns added by migrations.
// These run after migrations so the columns are guaranteed to exist.
const postMigrationIndexes = `
CREATE INDEX IF NOT EXISTS idx_session ON notifications(session_id);
CREATE INDEX IF NOT EXISTS idx_tool ON notifications(tool_name);
`

// migrations is an ordered list of ALTER statements applied after the base
// schema. Each is run with "ALTER TABLE …" wrapped in a no-op guard so that
// adding an already-existing column does not fail (SQLite has no IF NOT EXISTS
// for ALTER COLUMN, so we catch the duplicate-column error).
var migrations = []string{
	// v0.1.0 → v0.2.0: add session_id column.
	`ALTER TABLE notifications ADD COLUMN session_id TEXT`,
	// v0.2.0 → v0.3.0: add hook detail columns for dashboard.
	`ALTER TABLE notifications ADD COLUMN tool_name TEXT`,
	`ALTER TABLE notifications ADD COLUMN tool_input_summary TEXT`,
	`ALTER TABLE notifications ADD COLUMN hook_event TEXT`,
	`ALTER TABLE notifications ADD COLUMN transcript_path TEXT`,
	// v0.3.0 → v0.4.0: add resolved_by_rule_id for auto-approve tracking.
	`ALTER TABLE notifications ADD COLUMN resolved_by_rule_id INTEGER DEFAULT NULL`,
}

// policyRulesSchema creates the policy_rules table for the auto-approve engine.
const policyRulesSchema = `
CREATE TABLE IF NOT EXISTS policy_rules (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  type        TEXT NOT NULL,
  session_id  TEXT,
  tool_name   TEXT,
  pattern     TEXT,
  enabled     INTEGER NOT NULL DEFAULT 1,
  priority    INTEGER NOT NULL DEFAULT 0,
  source      TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
)
`

// policyRulesIndexes creates indexes on the policy_rules table.
const policyRulesIndexes = `
CREATE INDEX IF NOT EXISTS idx_policy_rules_type_session ON policy_rules(type, session_id);
CREATE INDEX IF NOT EXISTS idx_policy_rules_tool ON policy_rules(tool_name);
`
