package relay

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestInsertAndFetch(t *testing.T) {
	store := newTestStore(t)
	base := time.Now().UTC().Add(-time.Hour)

	for _, m := range []Message{
		{ID: "m1", Channel: "general", UserID: "alice", Username: "alice", Content: "first", Timestamp: base},
		{ID: "m2", Channel: "general", UserID: "alice", Username: "alice", Content: "second", Timestamp: base.Add(time.Minute)},
		{ID: "m3", Channel: "general", UserID: "alice", Username: "alice", Content: "third", Timestamp: base.Add(2 * time.Minute)},
		// A different channel must not leak into general.
		{ID: "x1", Channel: "other", UserID: "bob", Username: "bob", Content: "elsewhere", Timestamp: base},
	} {
		if err := store.Insert(m); err != nil {
			t.Fatalf("Insert %s: %v", m.ID, err)
		}
	}

	got, err := store.FetchBefore("general", time.Time{}, 10)
	if err != nil {
		t.Fatalf("FetchBefore: %v", err)
	}
	if want := []string{"m1", "m2", "m3"}; !equalIDs(got, want) {
		t.Fatalf("FetchBefore = %v, want %v (oldest first)", ids(got), want)
	}

	// before cursor excludes newer messages and the cursor itself.
	cursor, err := store.FetchBefore("general", base.Add(2*time.Minute), 10)
	if err != nil {
		t.Fatalf("FetchBefore(cursor): %v", err)
	}
	if want := []string{"m1", "m2"}; !equalIDs(cursor, want) {
		t.Fatalf("FetchBefore(cursor) = %v, want %v", ids(cursor), want)
	}

	// limit keeps the most recent N, still returned oldest-first.
	limited, err := store.FetchBefore("general", time.Time{}, 2)
	if err != nil {
		t.Fatalf("FetchBefore(limit): %v", err)
	}
	if want := []string{"m2", "m3"}; !equalIDs(limited, want) {
		t.Fatalf("FetchBefore(limit 2) = %v, want %v", ids(limited), want)
	}

	// since is strict (excludes the boundary message).
	since, err := store.FetchSince("general", base)
	if err != nil {
		t.Fatalf("FetchSince: %v", err)
	}
	if want := []string{"m2", "m3"}; !equalIDs(since, want) {
		t.Fatalf("FetchSince(base) = %v, want %v", ids(since), want)
	}

	// Round-tripped timestamps survive at nanosecond precision.
	if !got[2].Timestamp.Equal(base.Add(2 * time.Minute)) {
		t.Errorf("timestamp round-trip = %v, want %v", got[2].Timestamp, base.Add(2*time.Minute))
	}
}

func TestPruneOlderThan(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	for _, m := range []Message{
		{ID: "old", Channel: "general", UserID: "alice", Username: "alice", Content: "old", Timestamp: now.Add(-48 * time.Hour)},
		{ID: "edge", Channel: "general", UserID: "alice", Username: "alice", Content: "edge", Timestamp: now.Add(-12 * time.Hour)},
		{ID: "new", Channel: "general", UserID: "alice", Username: "alice", Content: "new", Timestamp: now.Add(-time.Hour)},
	} {
		if err := store.Insert(m); err != nil {
			t.Fatalf("Insert %s: %v", m.ID, err)
		}
	}

	// 0 (and negative) means keep forever.
	if n, err := store.PruneOlderThan(0); err != nil || n != 0 {
		t.Fatalf("PruneOlderThan(0) = (%d, %v), want (0, nil)", n, err)
	}

	n, err := store.PruneOlderThan(24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneOlderThan: %v", err)
	}
	if n != 1 {
		t.Fatalf("PruneOlderThan(24h) removed %d, want 1 (only the 48h-old row)", n)
	}

	got, err := store.FetchBefore("general", time.Time{}, 10)
	if err != nil {
		t.Fatalf("FetchBefore: %v", err)
	}
	if want := []string{"edge", "new"}; !equalIDs(got, want) {
		t.Fatalf("after prune = %v, want %v", ids(got), want)
	}
}

func TestDeleteByUser(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	for _, m := range []Message{
		{ID: "a1", Channel: "general", UserID: "alice", Username: "alice", Content: "a1", Timestamp: now},
		{ID: "a2", Channel: "dev", UserID: "alice", Username: "alice", Content: "a2", Timestamp: now},
		{ID: "b1", Channel: "general", UserID: "bob", Username: "bob", Content: "b1", Timestamp: now},
	} {
		if err := store.Insert(m); err != nil {
			t.Fatalf("Insert %s: %v", m.ID, err)
		}
	}

	n, err := store.DeleteByUser("alice")
	if err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteByUser(alice) removed %d, want 2", n)
	}

	// alice is gone from every channel; bob is untouched.
	if got, _ := store.FetchBefore("general", time.Time{}, 10); !equalIDs(got, []string{"b1"}) {
		t.Fatalf("general after delete = %v, want [b1]", ids(got))
	}
	if got, _ := store.FetchBefore("dev", time.Time{}, 10); len(got) != 0 {
		t.Fatalf("dev after delete = %v, want []", ids(got))
	}
}

func ids(msgs []Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func equalIDs(msgs []Message, want []string) bool {
	if len(msgs) != len(want) {
		return false
	}
	for i, m := range msgs {
		if m.ID != want[i] {
			return false
		}
	}
	return true
}
