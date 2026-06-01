package matrix

import (
	"context"
	"path/filepath"
	"testing"

	"maunium.net/go/mautrix/id"
)

func TestFileSyncStorePersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sync.json")
	ctx := context.Background()
	uid := id.UserID("@me:example.org")

	s := newFileSyncStore(path)
	if err := s.SaveNextBatch(ctx, uid, "batch-1"); err != nil {
		t.Fatalf("SaveNextBatch: %v", err)
	}
	if err := s.SaveFilterID(ctx, uid, "filter-1"); err != nil {
		t.Fatalf("SaveFilterID: %v", err)
	}

	// A fresh store reading the same file resumes incrementally.
	reloaded := newFileSyncStore(path)
	if got, _ := reloaded.LoadNextBatch(ctx, uid); got != "batch-1" {
		t.Errorf("next batch after reload = %q, want batch-1", got)
	}
	if got, _ := reloaded.LoadFilterID(ctx, uid); got != "filter-1" {
		t.Errorf("filter id after reload = %q, want filter-1", got)
	}
}

func TestFileSyncStoreMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	s := newFileSyncStore(path)
	if got, _ := s.LoadNextBatch(context.Background(), id.UserID("@me:hs")); got != "" {
		t.Errorf("expected empty next batch for new store, got %q", got)
	}
}

func TestFileSyncStoreSeparatesUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.json")
	ctx := context.Background()
	s := newFileSyncStore(path)

	_ = s.SaveNextBatch(ctx, id.UserID("@a:hs"), "a-batch")
	_ = s.SaveNextBatch(ctx, id.UserID("@b:hs"), "b-batch")

	if got, _ := s.LoadNextBatch(ctx, id.UserID("@a:hs")); got != "a-batch" {
		t.Errorf("user a batch = %q", got)
	}
	if got, _ := s.LoadNextBatch(ctx, id.UserID("@b:hs")); got != "b-batch" {
		t.Errorf("user b batch = %q", got)
	}
}
