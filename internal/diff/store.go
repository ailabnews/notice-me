package diff

import "sync"

// DiffPayload holds the raw content needed to render and apply a diff for a
// notification. Stored in memory while the notification is active and removed
// on resolve/cancel.
type DiffPayload struct {
	ToolName  string     `json:"tool_name"`
	FilePath  string     `json:"file_path"`
	OldString string     `json:"old_string"`
	NewString string     `json:"new_string"`
	Hunks     []HunkMeta `json:"hunks"`
}

// HunkMeta describes one contiguous group of changes within a diff. Pre-computed
// at store time so the frontend and backend use identical hunk boundaries.
type HunkMeta struct {
	Index    int      `json:"index"`
	OldStart int      `json:"old_start"` // 1-based line number in old text
	OldCount int      `json:"old_count"`
	NewStart int      `json:"new_start"` // 1-based line number in new text
	NewCount int      `json:"new_count"`
	OldLines []string `json:"old_lines"`
	NewLines []string `json:"new_lines"`
}

// Store is a goroutine-safe in-memory store for DiffPayload keyed by
// notification ID. It is intentionally not persisted — diff data is only
// needed while a notification is active.
type Store struct {
	mu   sync.RWMutex
	data map[int64]*DiffPayload
}

func NewStore() *Store {
	return &Store{data: make(map[int64]*DiffPayload)}
}

func (s *Store) Set(id int64, p DiffPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = &p
}

func (s *Store) Get(id int64) (DiffPayload, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data[id]
	if !ok {
		return DiffPayload{}, false
	}
	return *p, true
}

func (s *Store) Delete(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
}
