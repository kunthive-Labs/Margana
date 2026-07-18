package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kunthive-Labs/Margana/internal/guilds"
	"github.com/kunthive-Labs/Margana/internal/history"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/webhook"
	"github.com/kunthive-Labs/Margana/internal/wsclient"
)

// These tests stand up the reference relay over a real httptest listener and
// drive Marga's ACTUAL client packages (wsclient, webhook, history, guilds)
// against it, proving both sides agree on the wire contract.

const testAPIKey = "contract-test-key"

func newContractRelay(t *testing.T, retention time.Duration) (srv *httptest.Server, hub *Hub, store *Store) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	hub = NewHub(nil)
	backend := NewLocalBackend(store, hub, "general")
	server := NewServer(store, hub, backend, testAPIKey, retention, nil)
	srv = httptest.NewServer(server)
	t.Cleanup(func() {
		srv.Close()
		_ = store.Close()
	})
	return srv, hub, store
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws"
}

// connectSubscribed returns a wsclient connected and subscribed to channel,
// after confirming the hub has registered the subscription.
func connectSubscribed(t *testing.T, srv *httptest.Server, hub *Hub, username, channel string) *wsclient.Client {
	t.Helper()
	client := wsclient.New(wsURL(srv.URL), username, channel, testAPIKey)
	if err := client.Connect(); err != nil {
		t.Fatalf("ws connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Subscribe(channel); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	waitForSubscribers(t, hub, channel, 1)
	return client
}

func waitForSubscribers(t *testing.T, hub *Hub, channel string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.channels[channel])
		hub.mu.RUnlock()
		if n >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d subscriber(s) on %q", want, channel)
}

func TestContract_SendSubscribeReceive(t *testing.T) {
	srv, hub, _ := newContractRelay(t, 0)
	client := connectSubscribed(t, srv, hub, "alice", "general")

	sender := webhook.New("", srv.URL, testAPIKey, "alice", "", "")
	msgID, err := sender.Send("hello world", "general", "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msgID == "" {
		t.Fatal("Send returned an empty message id")
	}

	select {
	case m := <-client.Messages():
		if m.EventType != "message_create" {
			t.Errorf("event type = %q, want message_create", m.EventType)
		}
		if m.ID != msgID {
			t.Errorf("id = %q, want %q", m.ID, msgID)
		}
		if m.Username != "alice" || m.Content != "hello world" || m.Channel != "general" {
			t.Errorf("unexpected broadcast: %+v", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the broadcast message")
	}
}

func TestContract_History(t *testing.T) {
	srv, _, _ := newContractRelay(t, 0)
	sender := webhook.New("", srv.URL, testAPIKey, "alice", "", "")

	before := time.Now().UTC()
	id1, err := sender.Send("first", "general", "")
	if err != nil {
		t.Fatalf("send first: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	id2, err := sender.Send("second", "general", "")
	if err != nil {
		t.Fatalf("send second: %v", err)
	}

	fetcher := history.New(srv.URL, testAPIKey)

	msgs, err := fetcher.Fetch("general", 100, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("Fetch returned %d messages, want 2", len(msgs))
	}
	if msgs[0].ID != id1 || msgs[1].ID != id2 {
		t.Fatalf("history order = [%s %s], want [%s %s]", msgs[0].ID, msgs[1].ID, id1, id2)
	}
	if msgs[0].Content != "first" || msgs[1].Content != "second" {
		t.Fatalf("history content = [%q %q], want [first second]", msgs[0].Content, msgs[1].Content)
	}

	// since before both -> both; since the first message's timestamp -> only the second.
	if got, err := fetcher.FetchSinceMessages("general", before); err != nil || len(got) != 2 {
		t.Fatalf("FetchSince(before) = (%d msgs, %v), want (2, nil)", len(got), err)
	}
	sinceMid, err := fetcher.FetchSinceMessages("general", msgs[0].Timestamp)
	if err != nil {
		t.Fatalf("FetchSince(mid): %v", err)
	}
	if len(sinceMid) != 1 || sinceMid[0].ID != id2 {
		t.Fatalf("FetchSince(mid) = %d msgs, want exactly [%s]", len(sinceMid), id2)
	}
}

func TestContract_EditBroadcast(t *testing.T) {
	srv, hub, _ := newContractRelay(t, 0)
	client := connectSubscribed(t, srv, hub, "alice", "general")
	sender := webhook.New("", srv.URL, testAPIKey, "alice", "", "")

	msgID, err := sender.Send("original", "general", "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Drain the create so we can assert on the update.
	select {
	case <-client.Messages():
	case <-time.After(3 * time.Second):
		t.Fatal("no message_create broadcast")
	}

	if err := sender.Edit(msgID, "general", "edited"); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	select {
	case m := <-client.Messages():
		if m.EventType != "message_update" {
			t.Errorf("event type = %q, want message_update", m.EventType)
		}
		if m.ID != msgID {
			t.Errorf("id = %q, want %q", m.ID, msgID)
		}
		if m.Content != "edited" {
			t.Errorf("content = %q, want edited", m.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no message_update broadcast")
	}
}

func TestContract_Typing(t *testing.T) {
	srv, hub, _ := newContractRelay(t, 0)
	client := connectSubscribed(t, srv, hub, "alice", "general")

	// Typing is backend-driven; the hub API is what a real bridge calls.
	hub.BroadcastTyping("general", "bob")
	select {
	case te := <-client.TypingEvents():
		if te.Username != "bob" {
			t.Errorf("typing username = %q, want bob", te.Username)
		}
		if te.Channel != "general" {
			t.Errorf("typing channel = %q, want general", te.Channel)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no typing event")
	}
}

func TestContract_StatusUpdate(t *testing.T) {
	srv, _, _ := newContractRelay(t, 0)
	client := wsclient.New(wsURL(srv.URL), "alice", "general", testAPIKey)
	if err := client.Connect(); err != nil {
		t.Fatalf("ws connect: %v", err)
	}
	defer client.Close()
	// Let identify register the connection before it triggers a broadcast.
	time.Sleep(50 * time.Millisecond)

	if err := client.SendStatus("online"); err != nil {
		t.Fatalf("SendStatus: %v", err)
	}
	select {
	case p := <-client.Presences():
		if p.Username != "alice" {
			t.Errorf("presence username = %q, want alice", p.Username)
		}
		if p.Status != "online" {
			t.Errorf("presence status = %q, want online", p.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no status_update event")
	}
}

func TestContract_Channels(t *testing.T) {
	srv, _, _ := newContractRelay(t, 0)
	sender := webhook.New("", srv.URL, testAPIKey, "alice", "", "")
	if _, err := sender.Send("hi", "random", ""); err != nil {
		t.Fatalf("Send: %v", err)
	}

	names, err := history.New(srv.URL, testAPIKey).FetchChannels()
	if err != nil {
		t.Fatalf("FetchChannels: %v", err)
	}
	if !contains(names, "general") || !contains(names, "random") {
		t.Fatalf("channels = %v, want to contain general and random", names)
	}
}

func TestContract_Guilds(t *testing.T) {
	srv, _, _ := newContractRelay(t, 0)
	gc := guilds.NewClient(srv.URL, testAPIKey)

	gs, err := gc.FetchGuilds("")
	if err != nil {
		t.Fatalf("FetchGuilds: %v", err)
	}
	if len(gs) != 1 || gs[0].ID != "local" {
		t.Fatalf("guilds = %+v, want exactly one guild with id=local", gs)
	}

	none, err := gc.FetchGuilds("no-such-guild")
	if err != nil {
		t.Fatalf("FetchGuilds(query): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("filtered guilds = %+v, want empty", none)
	}
}

func TestContract_SendFile(t *testing.T) {
	srv, hub, _ := newContractRelay(t, 0)
	client := connectSubscribed(t, srv, hub, "alice", "general")

	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("file contents"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	sender := webhook.New("", srv.URL, testAPIKey, "alice", "", "")
	msgID, err := sender.SendFile(path, "general", "with caption")
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if msgID == "" {
		t.Fatal("SendFile returned an empty message id")
	}
	select {
	case m := <-client.Messages():
		if m.ID != msgID {
			t.Errorf("id = %q, want %q", m.ID, msgID)
		}
		if m.Content != "with caption" {
			t.Errorf("content = %q, want 'with caption'", m.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no broadcast for the uploaded file")
	}
}

func TestContract_DeleteMyData(t *testing.T) {
	srv, _, _ := newContractRelay(t, 0)
	sender := webhook.New("", srv.URL, testAPIKey, "alice", "", "")
	if _, err := sender.Send("one", "general", ""); err != nil {
		t.Fatalf("send one: %v", err)
	}
	if _, err := sender.Send("two", "general", ""); err != nil {
		t.Fatalf("send two: %v", err)
	}

	// The local backend keys authorship by username, so delete-my-data uses it.
	if deleted := postDeleteMyData(t, srv.URL, "alice"); deleted != 2 {
		t.Fatalf("delete-my-data reported %d, want 2", deleted)
	}

	msgs, err := history.New(srv.URL, testAPIKey).Fetch("general", 100, nil)
	if err != nil {
		t.Fatalf("Fetch after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("history after delete = %d messages, want 0", len(msgs))
	}
}

func TestContract_RetentionFilterOnRead(t *testing.T) {
	// A short retention window plus a store row older than it: the pruner has
	// not run, but the read path must still hide the stale row.
	srv, _, store := newContractRelay(t, time.Hour)
	if err := store.Insert(Message{
		ID: "stale", Channel: "general", UserID: "alice", Username: "alice",
		Content: "old", Timestamp: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("Insert stale: %v", err)
	}
	if err := store.Insert(Message{
		ID: "fresh", Channel: "general", UserID: "alice", Username: "alice",
		Content: "recent", Timestamp: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Insert fresh: %v", err)
	}

	msgs, err := history.New(srv.URL, testAPIKey).Fetch("general", 100, nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "fresh" {
		t.Fatalf("history with retention = %d messages %v, want only [fresh]", len(msgs), msgIDs(msgs))
	}
}

func TestContract_AuthRequired(t *testing.T) {
	srv, _, _ := newContractRelay(t, 0)

	// Missing key -> 401.
	resp, err := http.Get(srv.URL + "/api/channels")
	if err != nil {
		t.Fatalf("GET without key: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-key GET /api/channels = HTTP %d, want 401", resp.StatusCode)
	}

	// healthz is exempt from auth.
	h, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = h.Body.Close()
	if h.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz = HTTP %d, want 200", h.StatusCode)
	}

	// A client with the wrong key gets an error.
	if _, err := guilds.NewClient(srv.URL, "wrong-key").FetchGuilds(""); err == nil {
		t.Fatal("expected an error with the wrong API key")
	}
}

// --- helpers ---------------------------------------------------------------

func postDeleteMyData(t *testing.T, baseURL, userID string) int64 {
	t.Helper()
	body := strings.NewReader(`{"user_id":` + strconv.Quote(userID) + `}`)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/delete-my-data", body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete-my-data: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete-my-data returned HTTP %d", resp.StatusCode)
	}
	var out struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode delete-my-data response: %v", err)
	}
	return out.Deleted
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func msgIDs(msgs []model.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}
