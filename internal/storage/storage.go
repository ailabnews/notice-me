package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"notify-me/internal/policy"

	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

type Record struct {
	ID               int64  `json:"id"`
	Endpoint         string `json:"endpoint"`
	Title            string `json:"title"`
	Message          string `json:"message"`
	SourceIP         string `json:"source_ip"`
	SourceHeader     string `json:"source_header"`
	SessionID        string `json:"session_id"`
	ToolName         string `json:"tool_name"`
	ToolInputSummary string `json:"tool_input_summary"`
	HookEvent        string `json:"hook_event"`
	TranscriptPath   string `json:"transcript_path"`
	Status           string `json:"status"`
	CreatedAt        int64  `json:"created_at"`
	ResolvedAt       int64  `json:"resolved_at"`
	DurationMs       int64  `json:"duration_ms"`
	ResolvedByRuleID int64  `json:"-"`
}

type ListFilter struct {
	Status string
	Search string
	Limit  int
	Offset int
}

func Open(path string) (*Storage, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	// Apply incremental migrations — ignore "duplicate column" errors since
	// the column may already exist from the base schema on fresh databases.
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			if !isDupColumn(err) {
				return nil, err
			}
		}
	}
	// Indexes that reference migrated columns go here.
	if _, err := db.Exec(postMigrationIndexes); err != nil {
		return nil, err
	}
	// Policy rules table for auto-approve engine.
	if _, err := db.Exec(policyRulesSchema); err != nil {
		return nil, err
	}
	if _, err := db.Exec(policyRulesIndexes); err != nil {
		return nil, err
	}
	return &Storage{db: db}, nil
}

func (s *Storage) Close() error { return s.db.Close() }

func (s *Storage) Insert(ctx context.Context, r Record) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO notifications(endpoint,title,message,source_ip,source_header,session_id,
            tool_name,tool_input_summary,hook_event,transcript_path,status,created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Endpoint, r.Title, r.Message, r.SourceIP, r.SourceHeader, r.SessionID,
		r.ToolName, r.ToolInputSummary, r.HookEvent, r.TranscriptPath, r.Status, r.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Storage) UpdateStatus(ctx context.Context, id int64, status string, resolvedAt int64, ruleID int64) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE notifications SET status=?, resolved_at=?,
        resolved_by_rule_id=CASE WHEN ? > 0 THEN ? ELSE resolved_by_rule_id END,
        duration_ms = CASE WHEN created_at > 0 THEN ?-created_at ELSE 0 END
        WHERE id=?`,
		status, resolvedAt, ruleID, ruleID, resolvedAt, id)
	return err
}

func (s *Storage) Get(ctx context.Context, id int64) (Record, error) {
	var r Record
	var srcIP, srcHdr, sessID, toolName, toolSummary, hookEvent, transPath sql.NullString
	var resolved, dur, ruleID sql.NullInt64
	row := s.db.QueryRowContext(ctx, `SELECT id,endpoint,title,message,source_ip,source_header,session_id,
        tool_name,tool_input_summary,hook_event,transcript_path,status,created_at,resolved_at,duration_ms,resolved_by_rule_id
        FROM notifications WHERE id=?`, id)
	if err := row.Scan(&r.ID, &r.Endpoint, &r.Title, &r.Message, &srcIP, &srcHdr, &sessID,
		&toolName, &toolSummary, &hookEvent, &transPath,
		&r.Status, &r.CreatedAt, &resolved, &dur, &ruleID); err != nil {
		return r, err
	}
	r.SourceIP = srcIP.String
	r.SourceHeader = srcHdr.String
	r.SessionID = sessID.String
	r.ToolName = toolName.String
	r.ToolInputSummary = toolSummary.String
	r.HookEvent = hookEvent.String
	r.TranscriptPath = transPath.String
	r.ResolvedAt = resolved.Int64
	r.DurationMs = dur.Int64
	r.ResolvedByRuleID = ruleID.Int64
	return r, nil
}

func (s *Storage) List(ctx context.Context, f ListFilter) ([]Record, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	var conds []string
	var args []any
	if f.Status != "" {
		conds = append(conds, "status=?")
		args = append(args, f.Status)
	}
	if f.Search != "" {
		conds = append(conds, "(title LIKE ? OR message LIKE ?)")
		pattern := "%" + f.Search + "%"
		args = append(args, pattern, pattern)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM notifications %s", where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
        SELECT id,endpoint,title,message,source_ip,source_header,session_id,
            tool_name,tool_input_summary,hook_event,transcript_path,
            status,created_at,resolved_at,duration_ms,resolved_by_rule_id
        FROM notifications %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, where), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var srcIP, srcHdr, sessID, toolName, toolSummary, hookEvent, transPath sql.NullString
		var resolved, dur, ruleID sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Endpoint, &r.Title, &r.Message, &srcIP, &srcHdr, &sessID,
			&toolName, &toolSummary, &hookEvent, &transPath,
			&r.Status, &r.CreatedAt, &resolved, &dur, &ruleID); err != nil {
			return nil, 0, err
		}
		r.SourceIP = srcIP.String
		r.SourceHeader = srcHdr.String
		r.SessionID = sessID.String
		r.ToolName = toolName.String
		r.ToolInputSummary = toolSummary.String
		r.HookEvent = hookEvent.String
		r.TranscriptPath = transPath.String
		r.ResolvedAt = resolved.Int64
		r.DurationMs = dur.Int64
		r.ResolvedByRuleID = ruleID.Int64
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (s *Storage) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE id=?`, id)
	return err
}

func (s *Storage) DeleteAll(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notifications`)
	return err
}

// isDupColumn returns true for SQLite "duplicate column name" errors, which
// occur when an ALTER TABLE ADD COLUMN is replayed on a database that already
// has the column (e.g. fresh DB created with the full schema).
func isDupColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

// ─── Dashboard query types ───

type ToolCount struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

type DecisionStats struct {
	Approved  int `json:"approved"`
	Denied    int `json:"denied"`
	Timeout   int `json:"timeout"`
	Cancelled int `json:"cancelled"`
	Other     int `json:"other"`
}

type SessionInfo struct {
	SessionID string `json:"session_id"`
	LastSeen  int64  `json:"last_seen"`
	Count     int    `json:"count"`
}

// ToolUsageCount returns per-tool call counts ordered by frequency.
func (s *Storage) ToolUsageCount(ctx context.Context) ([]ToolCount, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT COALESCE(tool_name,'unknown') AS tool, COUNT(*) AS cnt
        FROM notifications WHERE tool_name IS NOT NULL AND tool_name != ''
        GROUP BY tool ORDER BY cnt DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolCount
	for rows.Next() {
		var tc ToolCount
		if err := rows.Scan(&tc.Tool, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// DecisionStats returns counts grouped by status.
func (s *Storage) DecisionStats(ctx context.Context) (*DecisionStats, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT status, COUNT(*) FROM notifications GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ds := &DecisionStats{}
	for rows.Next() {
		var status string
		var cnt int
		if err := rows.Scan(&status, &cnt); err != nil {
			return nil, err
		}
		switch status {
		case "approved", "auto_approved", "允许", "acknowledged", "知道了":
			ds.Approved += cnt
		case "denied", "拒绝":
			ds.Denied += cnt
		case "timeout":
			ds.Timeout += cnt
		case "cancelled":
			ds.Cancelled += cnt
		default:
			ds.Other += cnt
		}
	}
	return ds, rows.Err()
}

// AvgResponseTime returns average duration_ms for resolved notifications.
func (s *Storage) AvgResponseTime(ctx context.Context) (float64, error) {
	var avg sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
        SELECT AVG(duration_ms) FROM notifications
        WHERE duration_ms > 0 AND duration_ms < 300000`).Scan(&avg)
	if err != nil {
		return 0, err
	}
	return avg.Float64, nil
}

// SessionIDs returns distinct sessions ordered by most recent activity.
func (s *Storage) SessionIDs(ctx context.Context) ([]SessionInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT session_id, MAX(created_at) AS last_seen, COUNT(*) AS cnt
        FROM notifications WHERE session_id IS NOT NULL AND session_id != ''
        GROUP BY session_id ORDER BY last_seen DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.SessionID, &si.LastSeen, &si.Count); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// RecentBySession returns the latest N records for a specific session.
func (s *Storage) RecentBySession(ctx context.Context, sessionID string, limit int) ([]Record, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id,endpoint,title,message,source_ip,source_header,session_id,
            tool_name,tool_input_summary,hook_event,transcript_path,
            status,created_at,resolved_at,duration_ms,resolved_by_rule_id
        FROM notifications WHERE session_id=?
        ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var srcIP, srcHdr, sessID, toolName, toolSummary, hookEvent, transPath sql.NullString
		var resolved, dur, ruleID sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Endpoint, &r.Title, &r.Message, &srcIP, &srcHdr, &sessID,
			&toolName, &toolSummary, &hookEvent, &transPath,
			&r.Status, &r.CreatedAt, &resolved, &dur, &ruleID); err != nil {
			return nil, err
		}
		r.SourceIP = srcIP.String
		r.SourceHeader = srcHdr.String
		r.SessionID = sessID.String
		r.ToolName = toolName.String
		r.ToolInputSummary = toolSummary.String
		r.HookEvent = hookEvent.String
		r.TranscriptPath = transPath.String
		r.ResolvedAt = resolved.Int64
		r.DurationMs = dur.Int64
		r.ResolvedByRuleID = ruleID.Int64
		out = append(out, r)
	}
	return out, rows.Err()
}

// TotalCount returns the total number of notifications.
func (s *Storage) TotalCount(ctx context.Context) (int, error) {
	var cnt int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications`).Scan(&cnt)
	return cnt, err
}

// ─── Policy rule CRUD ───

// ListPolicyRules returns all policy rules ordered by priority descending.
func (s *Storage) ListPolicyRules(ctx context.Context) ([]policy.PolicyRule, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, type, session_id, tool_name, pattern, enabled, priority, source, created_at, updated_at
        FROM policy_rules ORDER BY priority DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []policy.PolicyRule
	for rows.Next() {
		var r policy.PolicyRule
		var sessID, toolName, pattern sql.NullString
		if err := rows.Scan(&r.ID, &r.Type, &sessID, &toolName, &pattern,
			&r.Enabled, &r.Priority, &r.Source, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.SessionID = sessID.String
		r.ToolName = toolName.String
		r.Pattern = pattern.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertPolicyRule inserts a new policy rule and returns its ID.
func (s *Storage) InsertPolicyRule(ctx context.Context, r policy.PolicyRule) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO policy_rules(type, session_id, tool_name, pattern, enabled, priority, source, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Type, r.SessionID, r.ToolName, r.Pattern, r.Enabled, r.Priority, r.Source, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdatePolicyRule updates an existing policy rule by ID.
func (s *Storage) UpdatePolicyRule(ctx context.Context, r policy.PolicyRule) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE policy_rules SET type=?, session_id=?, tool_name=?, pattern=?, enabled=?, priority=?, source=?, updated_at=?
        WHERE id=?`,
		r.Type, r.SessionID, r.ToolName, r.Pattern, r.Enabled, r.Priority, r.Source, r.UpdatedAt, r.ID)
	return err
}

// DeletePolicyRule deletes a policy rule by ID.
func (s *Storage) DeletePolicyRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM policy_rules WHERE id=?`, id)
	return err
}

// CleanupInactiveSessionRules deletes session rules whose session_id has not
// been seen in the last 24 hours.
func (s *Storage) CleanupInactiveSessionRules(ctx context.Context) error {
	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
	_, err := s.db.ExecContext(ctx, `
        DELETE FROM policy_rules
        WHERE type = ? AND session_id NOT IN (
            SELECT DISTINCT session_id FROM notifications
            WHERE session_id IS NOT NULL AND session_id != '' AND created_at > ?
        )`, policy.RuleTypeSession, cutoff)
	return err
}
