package matrix

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"maunium.net/go/mautrix/id"
)

// fileSyncStore persists the /sync next_batch token and filter ID to a JSON
// file so restarts resume incrementally instead of doing a full initial sync.
// It implements mautrix.SyncStore.
type fileSyncStore struct {
	path string
	mu   sync.Mutex
	data syncData
}

type syncData struct {
	FilterIDs map[string]string `json:"filter_ids"`
	NextBatch map[string]string `json:"next_batch"`
}

func newFileSyncStore(path string) *fileSyncStore {
	s := &fileSyncStore{path: path}
	s.data.FilterIDs = map[string]string{}
	s.data.NextBatch = map[string]string{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &s.data)
		if s.data.FilterIDs == nil {
			s.data.FilterIDs = map[string]string{}
		}
		if s.data.NextBatch == nil {
			s.data.NextBatch = map[string]string{}
		}
	}
	return s
}

func (s *fileSyncStore) persist() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	raw, err := json.Marshal(s.data)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, raw, 0o600)
}

func (s *fileSyncStore) SaveFilterID(_ context.Context, userID id.UserID, filterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.FilterIDs[userID.String()] = filterID
	s.persist()
	return nil
}

func (s *fileSyncStore) LoadFilterID(_ context.Context, userID id.UserID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.FilterIDs[userID.String()], nil
}

func (s *fileSyncStore) SaveNextBatch(_ context.Context, userID id.UserID, nextBatch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.NextBatch[userID.String()] = nextBatch
	s.persist()
	return nil
}

func (s *fileSyncStore) LoadNextBatch(_ context.Context, userID id.UserID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.NextBatch[userID.String()], nil
}
