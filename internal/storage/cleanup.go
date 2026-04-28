package storage

import (
	"context"
	"time"
)

// Prune deletes records older than retentionDays, then trims excess so the
// total row count is <= maxRecords.
func (s *Storage) Prune(ctx context.Context, retentionDays int, maxRecords int) error {
	cutoff := time.Now().UnixMilli() - int64(retentionDays)*86_400_000
	if _, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE created_at < ?`, cutoff); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
        DELETE FROM notifications WHERE id IN (
            SELECT id FROM notifications ORDER BY created_at DESC LIMIT -1 OFFSET ?
        )`, maxRecords)
	return err
}

// RunCleanup ticks every interval until ctx cancels.
// Prune errors are intentionally swallowed; the boot integration (Task 23)
// will wrap this with a logger so failures surface in the rotating log file.
func (s *Storage) RunCleanup(ctx context.Context, interval time.Duration, retentionDays, maxRecords int) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.Prune(ctx, retentionDays, maxRecords)
		}
	}
}
