package storage

const schema = `
CREATE TABLE IF NOT EXISTS notifications (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  endpoint      TEXT NOT NULL,
  title         TEXT NOT NULL,
  message       TEXT NOT NULL,
  source_ip     TEXT,
  source_header TEXT,
  status        TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  resolved_at   INTEGER,
  duration_ms   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_created ON notifications(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_status  ON notifications(status);
`
