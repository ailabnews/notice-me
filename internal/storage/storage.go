package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

type Record struct {
	ID           int64  `json:"id"`
	Endpoint     string `json:"endpoint"`
	Title        string `json:"title"`
	Message      string `json:"message"`
	SourceIP     string `json:"source_ip"`
	SourceHeader string `json:"source_header"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	ResolvedAt   int64  `json:"resolved_at"`
	DurationMs   int64  `json:"duration_ms"`
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
	return &Storage{db: db}, nil
}

func (s *Storage) Close() error { return s.db.Close() }

func (s *Storage) Insert(ctx context.Context, r Record) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO notifications(endpoint,title,message,source_ip,source_header,status,created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Endpoint, r.Title, r.Message, r.SourceIP, r.SourceHeader, r.Status, r.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Storage) UpdateStatus(ctx context.Context, id int64, status string, resolvedAt int64) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE notifications SET status=?, resolved_at=?,
        duration_ms = CASE WHEN created_at > 0 THEN ?-created_at ELSE 0 END
        WHERE id=?`,
		status, resolvedAt, resolvedAt, id)
	return err
}

func (s *Storage) Get(ctx context.Context, id int64) (Record, error) {
	var r Record
	var srcIP, srcHdr sql.NullString
	var resolved, dur sql.NullInt64
	row := s.db.QueryRowContext(ctx, `SELECT id,endpoint,title,message,source_ip,source_header,status,created_at,resolved_at,duration_ms FROM notifications WHERE id=?`, id)
	if err := row.Scan(&r.ID, &r.Endpoint, &r.Title, &r.Message, &srcIP, &srcHdr, &r.Status, &r.CreatedAt, &resolved, &dur); err != nil {
		return r, err
	}
	r.SourceIP = srcIP.String
	r.SourceHeader = srcHdr.String
	r.ResolvedAt = resolved.Int64
	r.DurationMs = dur.Int64
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
        SELECT id,endpoint,title,message,source_ip,source_header,status,created_at,resolved_at,duration_ms
        FROM notifications %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, where), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var srcIP, srcHdr sql.NullString
		var resolved, dur sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Endpoint, &r.Title, &r.Message, &srcIP, &srcHdr, &r.Status, &r.CreatedAt, &resolved, &dur); err != nil {
			return nil, 0, err
		}
		r.SourceIP = srcIP.String
		r.SourceHeader = srcHdr.String
		r.ResolvedAt = resolved.Int64
		r.DurationMs = dur.Int64
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
