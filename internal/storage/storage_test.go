package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestStorage(t *testing.T) *Storage {
	t.Helper()
	db := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(db)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestInsertAndGet(t *testing.T) {
	s := openTestStorage(t)
	ctx := context.Background()
	id, err := s.Insert(ctx, Record{
		Endpoint:  "confirm",
		Title:     "T",
		Message:   "M",
		SourceIP:  "127.0.0.1",
		Status:    "pending",
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	rec, err := s.Get(ctx, id)
	if err != nil || rec.Title != "T" {
		t.Fatalf("Get: %v %v", err, rec)
	}
}

func TestUpdateStatus(t *testing.T) {
	s := openTestStorage(t)
	ctx := context.Background()
	id, _ := s.Insert(ctx, Record{Endpoint: "x", Title: "t", Message: "m", Status: "pending", CreatedAt: time.Now().UnixMilli()})
	if err := s.UpdateStatus(ctx, id, "approved", time.Now().UnixMilli(), 0); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	rec, _ := s.Get(ctx, id)
	if rec.Status != "approved" || rec.ResolvedAt == 0 {
		t.Fatalf("status not persisted: %+v", rec)
	}
}

func TestListPagination(t *testing.T) {
	s := openTestStorage(t)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		_, _ = s.Insert(ctx, Record{Endpoint: "x", Title: "t", Message: "m", Status: "approved", CreatedAt: int64(1000 + i)})
	}
	page, total, err := s.List(ctx, ListFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if total != 25 || len(page) != 10 {
		t.Fatalf("page: %d total: %d", len(page), total)
	}
	if page[0].CreatedAt < page[1].CreatedAt {
		t.Fatal("expected DESC order by created_at")
	}
}

func TestPruneByAge(t *testing.T) {
	s := openTestStorage(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	old := now - int64(40*24*60*60*1000)
	_, _ = s.Insert(ctx, Record{Endpoint: "x", Title: "old", Message: "m", Status: "approved", CreatedAt: old})
	_, _ = s.Insert(ctx, Record{Endpoint: "x", Title: "new", Message: "m", Status: "approved", CreatedAt: now})
	if err := s.Prune(ctx, 30, 1000); err != nil {
		t.Fatal(err)
	}
	_, total, _ := s.List(ctx, ListFilter{Limit: 10})
	if total != 1 {
		t.Fatalf("expected 1 row after prune, got %d", total)
	}
}

func TestPruneByCount(t *testing.T) {
	s := openTestStorage(t)
	ctx := context.Background()
	base := time.Now().UnixMilli()
	for i := 0; i < 5; i++ {
		_, _ = s.Insert(ctx, Record{Endpoint: "x", Title: "t", Message: "m", Status: "approved", CreatedAt: base + int64(i)})
	}
	if err := s.Prune(ctx, 365, 3); err != nil {
		t.Fatal(err)
	}
	_, total, _ := s.List(ctx, ListFilter{Limit: 10})
	if total != 3 {
		t.Fatalf("want 3, got %d", total)
	}
}

func TestListStatusFilter(t *testing.T) {
	s := openTestStorage(t)
	ctx := context.Background()
	_, _ = s.Insert(ctx, Record{Endpoint: "x", Title: "a", Message: "m", Status: "approved", CreatedAt: 1})
	_, _ = s.Insert(ctx, Record{Endpoint: "x", Title: "b", Message: "m", Status: "denied", CreatedAt: 2})
	_, _ = s.Insert(ctx, Record{Endpoint: "x", Title: "c", Message: "m", Status: "approved", CreatedAt: 3})
	page, total, err := s.List(ctx, ListFilter{Status: "approved", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(page) != 2 {
		t.Fatalf("want 2/2 got %d/%d", total, len(page))
	}
}
