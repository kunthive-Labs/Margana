package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/db"
	"github.com/kunthive-Labs/Margana/internal/history"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

func msg(id string, ts time.Time) model.Message {
	return model.Message{
		ID:        id,
		Username:  "user",
		Content:   "content-" + id,
		Channel:   "general",
		Timestamp: ts,
	}
}

func TestInsertSortedAppendsToEnd(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}

	result := insertSorted(msgs, msg("3", base.Add(2*time.Minute)))
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[2].ID != "3" {
		t.Errorf("expected last message ID '3', got %q", result[2].ID)
	}
}

func TestInsertSortedInsertsInMiddle(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs := []model.Message{msg("1", base), msg("3", base.Add(2*time.Minute))}

	result := insertSorted(msgs, msg("2", base.Add(time.Minute)))
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[1].ID != "2" {
		t.Errorf("expected middle message ID '2', got %q", result[1].ID)
	}
}

func TestInsertSortedDeduplicatesByID(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}

	result := insertSorted(msgs, msg("1", base))
	if len(result) != 2 {
		t.Errorf("expected 2 messages after duplicate insert, got %d", len(result))
	}
}

func TestInsertSortedAtBeginning(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs := []model.Message{msg("2", base.Add(time.Minute)), msg("3", base.Add(2*time.Minute))}

	result := insertSorted(msgs, msg("1", base))
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("expected first message ID '1', got %q", result[0].ID)
	}
}

func TestInsertSortedEmptySlice(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var msgs []model.Message

	result := insertSorted(msgs, msg("1", base))
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("expected message ID '1', got %q", result[0].ID)
	}
}

func TestMergeMessagesNoDuplicates(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	existing := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}
	incoming := []model.Message{msg("3", base.Add(2*time.Minute)), msg("4", base.Add(3*time.Minute))}

	result := mergeMessages(existing, incoming)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	for i := 0; i < len(result)-1; i++ {
		if !result[i].Timestamp.Before(result[i+1].Timestamp) {
			t.Errorf("messages not sorted chronologically at index %d: %v >= %v", i, result[i].Timestamp, result[i+1].Timestamp)
		}
	}
}

func TestMergeMessagesWithDuplicates(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	existing := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}
	incoming := []model.Message{msg("2", base.Add(time.Minute)), msg("3", base.Add(2*time.Minute))}

	result := mergeMessages(existing, incoming)
	if len(result) != 3 {
		t.Errorf("expected 3 messages after dedup, got %d", len(result))
	}
}

func TestMergeMessagesAllDuplicates(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	existing := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}
	incoming := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}

	result := mergeMessages(existing, incoming)
	if len(result) != 2 {
		t.Errorf("expected 2 messages when all incoming are duplicates, got %d", len(result))
	}
}

func TestMergeMessagesEmptyExisting(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var existing []model.Message
	incoming := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}

	result := mergeMessages(existing, incoming)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
}

func TestMergeMessagesEmptyIncoming(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	existing := []model.Message{msg("1", base), msg("2", base.Add(time.Minute))}
	var incoming []model.Message

	result := mergeMessages(existing, incoming)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
}

func TestMergeMessagesBothEmpty(t *testing.T) {
	result := mergeMessages(nil, nil)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestMergeMessagesSortedChronologically(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	existing := []model.Message{msg("3", base.Add(2*time.Minute)), msg("5", base.Add(4*time.Minute))}
	incoming := []model.Message{msg("1", base), msg("4", base.Add(3*time.Minute)), msg("2", base.Add(time.Minute))}

	result := mergeMessages(existing, incoming)
	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result))
	}

	expectedOrder := []string{"1", "2", "3", "4", "5"}
	for i, m := range result {
		if m.ID != expectedOrder[i] {
			t.Errorf("expected message %d to have ID %q, got %q", i, expectedOrder[i], m.ID)
		}
	}
}

func TestMergeMessagesWithRealtimeOverlap(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	existing := []model.Message{
		msg("rt-1", base.Add(90*time.Minute)),
		msg("rt-2", base.Add(95*time.Minute)),
	}

	incoming := []model.Message{
		msg("h-1", base),
		msg("h-2", base.Add(30*time.Minute)),
		msg("h-3", base.Add(60*time.Minute)),
		msg("rt-1", base.Add(90*time.Minute)),
	}

	result := mergeMessages(existing, incoming)
	if len(result) != 5 {
		t.Fatalf("expected 5 messages after overlap dedup, got %d", len(result))
	}

	for i := 0; i < len(result)-1; i++ {
		if !result[i].Timestamp.Before(result[i+1].Timestamp) {
			t.Errorf("messages not sorted at index %d", i)
		}
	}
}

func TestMergeMessagesIncomingOlderThanExisting(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	existing := []model.Message{msg("new", base.Add(10*time.Hour))}
	incoming := []model.Message{msg("old", base)}

	result := mergeMessages(existing, incoming)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].ID != "old" {
		t.Errorf("expected oldest message first, got %q", result[0].ID)
	}
	if result[1].ID != "new" {
		t.Errorf("expected newest message last, got %q", result[1].ID)
	}
}

func TestMergeMessagesReplacesStaleLocalEchoAfterRestart(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	existing := []model.Message{{
		ID:        "echo-123",
		Username:  "terminal-user",
		Content:   "hello from terminal",
		Channel:   "general",
		Timestamp: base,
	}}
	incoming := []model.Message{{
		ID:        "discord-123",
		Username:  "discord-user",
		Content:   "hello from terminal",
		Channel:   "general",
		Timestamp: base.Add(2 * time.Second),
	}}

	result := mergeMessages(existing, incoming)
	if len(result) != 1 {
		t.Fatalf("expected stale echo to be replaced, got %d messages", len(result))
	}
	if result[0].ID != "discord-123" {
		t.Fatalf("expected real server ID after merge, got %q", result[0].ID)
	}
}

func TestMergeMessagesKeepsRepeatedMessagesOutsideEchoWindow(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	existing := []model.Message{{
		ID:        "echo-123",
		Username:  "me",
		Content:   "repeatable",
		Channel:   "general",
		Timestamp: base,
	}}
	incoming := []model.Message{{
		ID:        "discord-123",
		Username:  "me",
		Content:   "repeatable",
		Channel:   "general",
		Timestamp: base.Add(30 * time.Minute),
	}}

	result := mergeMessages(existing, incoming)
	if len(result) != 2 {
		t.Fatalf("expected distinct repeated messages to remain, got %d", len(result))
	}
}

func TestMergeMessagesKeepsDifferentRealMessagesWithSameContent(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	existing := []model.Message{{
		ID:        "discord-1",
		Username:  "me",
		Content:   "same content",
		Channel:   "general",
		Timestamp: base,
	}}
	incoming := []model.Message{{
		ID:        "discord-2",
		Username:  "me",
		Content:   "same content",
		Channel:   "general",
		Timestamp: base.Add(time.Second),
	}}

	result := mergeMessages(existing, incoming)
	if len(result) != 2 {
		t.Fatalf("expected same-content real messages to remain distinct, got %d", len(result))
	}
}

func TestSentWebsocketMessageReplacesLocalEcho(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m := New(nil, "discord", nil, nil, "general", "terminal-user", "", "discord-user", "", "test-guild", nil, "", "", "", nil, "")
	m.sentHashes[contentHash("terminal-user", "general", "hello")] = time.Now()
	m.msgs = []model.Message{{
		ID:        "echo-123",
		Username:  "terminal-user",
		Content:   "hello",
		Channel:   "general",
		Timestamp: base,
	}}

	updatedModel, _ := m.Update(networkEventMsg{Network: "discord", Kind: network.EventMessage, Message: &model.Message{
		ID:        "discord-123",
		Username:  "discord-user",
		Content:   "hello",
		Channel:   "general",
		Timestamp: base.Add(time.Second),
	}})
	updated := updatedModel.(Model)
	if len(updated.msgs) != 1 {
		t.Fatalf("expected websocket self-message to replace echo, got %d messages", len(updated.msgs))
	}
	if updated.msgs[0].ID != "discord-123" {
		t.Fatalf("expected real server ID after websocket reconcile, got %q", updated.msgs[0].ID)
	}
}

func openTUITestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.New(filepath.Join(t.TempDir(), "marga.db"))
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func assertStoredMessageIDs(t *testing.T, store *db.Store, expected []string) {
	t.Helper()
	msgs, err := store.GetMessages("general", 10, nil)
	if err != nil {
		t.Fatalf("getting stored messages: %v", err)
	}
	if len(msgs) != len(expected) {
		t.Fatalf("expected %d stored messages, got %d: %#v", len(expected), len(msgs), msgs)
	}
	for i, id := range expected {
		if msgs[i].ID != id {
			t.Fatalf("expected stored ID %q at index %d, got %q", id, i, msgs[i].ID)
		}
	}
}

func TestViewFitsWindowWithSidebarsAndMultilineInput(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.width = 120
	m.height = 30
	m.channels = []string{"general", "backend", "frontend", "ops", "random"}
	m.terminalOnline = []string{"me", "alice", "bob", "charlie"}
	m.presences["alice"] = model.UserPresence{Username: "alice", Status: "reviewing the release notes", Online: true}
	m.notifications = []model.Notification{
		{
			Channel:   "backend",
			Username:  "alice",
			Content:   "@me can you look at the deploy failure before standup?",
			Timestamp: base,
			MsgID:     "mention-1",
		},
	}

	for i := 0; i < 24; i++ {
		m.msgs = append(m.msgs, model.Message{
			ID:        fmt.Sprintf("msg-%02d", i),
			Username:  "alice",
			Content:   "this is a message with enough text to exercise wrapping inside the chat panel",
			Channel:   "general",
			Timestamp: base.Add(time.Duration(i) * time.Minute),
		})
	}
	m.replyTo = &m.msgs[len(m.msgs)-1]
	m.input.SetValue("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine")

	assertViewFits(t, m.View(), m.width, m.height)
}

func TestViewFitsSmallWindow(t *testing.T) {
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.width = 40
	m.height = 12
	m.input.SetValue("one\ntwo\nthree\nfour\nfive\nsix\nseven")

	assertViewFits(t, m.View(), m.width, m.height)
}

func TestStartEditLastPrefillsInput(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.msgs = []model.Message{
		{ID: "1", Username: "alice", Content: "a", Channel: "general", Timestamp: base},
		{ID: "2", Username: "me", Content: "first", Channel: "general", Timestamp: base.Add(time.Minute), Editable: true},
		{ID: "3", Username: "me", Content: "second", Channel: "general", Timestamp: base.Add(2 * time.Minute), Editable: true},
	}

	updated, _ := m.startEdit("last")
	got := updated.(Model)
	if got.editingMessage == nil {
		t.Fatal("expected editingMessage to be set")
	}
	if got.editingMessage.ID != "3" {
		t.Fatalf("expected message ID 3, got %q", got.editingMessage.ID)
	}
	if got.input.Value() != "second" {
		t.Fatalf("expected input to be prefilled, got %q", got.input.Value())
	}
}

func TestStartEditPickerSelectsOwnMessages(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.msgs = []model.Message{
		{ID: "1", Username: "alice", Content: "a", Channel: "general", Timestamp: base},
		{ID: "2", Username: "me", Content: "first", Channel: "general", Timestamp: base.Add(time.Minute), Editable: true},
		{ID: "3", Username: "bob", Content: "b", Channel: "general", Timestamp: base.Add(2 * time.Minute)},
	}

	updated, _ := m.startEdit("")
	got := updated.(Model)
	if !got.editSelectMode {
		t.Fatal("expected edit select mode to be enabled")
	}
	if got.editSelectIdx != 1 {
		t.Fatalf("expected selected index 1, got %d", got.editSelectIdx)
	}
}

func assertViewFits(t *testing.T, view string, width, height int) {
	t.Helper()

	if got := lipgloss.Height(view); got != height {
		t.Fatalf("expected view height %d, got %d\n%s", height, got, view)
	}
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d exceeds width %d: got %d\n%s", i+1, width, got, line)
		}
	}
}

func TestChannelsResultMsgReplacesStaleSelectedChannel(t *testing.T) {
	store, err := db.New(filepath.Join(t.TempDir(), "marga.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer store.Close()

	if err := store.InsertChannel("deleted"); err != nil {
		t.Fatalf("InsertChannel deleted: %v", err)
	}
	if err := store.InsertChannel("general"); err != nil {
		t.Fatalf("InsertChannel general: %v", err)
	}

	m := New(nil, "", store, nil, "deleted", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	updated, cmd := m.Update(history.ChannelsResultMsg{Channels: []string{"general", "dev"}})
	got := updated.(Model)

	if got.channel != "dev" {
		t.Fatalf("expected fallback to first synced channel, got %q", got.channel)
	}
	if len(got.channels) != 2 || got.channels[0] != "dev" || got.channels[1] != "general" {
		t.Fatalf("expected synced channels only, got %#v", got.channels)
	}
	if cmd == nil {
		t.Fatal("expected follow-up commands after switching channel")
	}

	channels, err := store.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 2 || channels[0] != "dev" || channels[1] != "general" {
		t.Fatalf("expected store to contain synced channels only, got %#v", channels)
	}
}

// Bug 1 regression: insertSorted must not insert a duplicate when the incoming
// message has an earlier timestamp than messages that appear after the
// existing entry in the list.
func TestInsertSortedNoDuplicateWhenTimestampEarlierThanLaterMessages(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	// m.msgs after upgradeEchoID: realID sits at T+2, another message at T+3.
	msgs := []model.Message{
		{ID: "other", Username: "alice", Content: "hi", Channel: "general", Timestamp: base.Add(time.Second)},
		{ID: "real-id", Username: "me", Content: "hello", Channel: "general", Timestamp: base.Add(2 * time.Second)},
		{ID: "later", Username: "alice", Content: "bye", Channel: "general", Timestamp: base.Add(3 * time.Second)},
	}
	// Relay broadcast arrives with second-truncated timestamp: T+0 (earlier than "other").
	incoming := model.Message{ID: "real-id", Username: "me", Content: "hello", Channel: "general", Timestamp: base}

	result := insertSorted(msgs, incoming)
	if len(result) != 3 {
		t.Fatalf("insertSorted duplicated message: got %d entries (want 3)", len(result))
	}
	count := 0
	for _, m := range result {
		if m.ID == "real-id" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 entry with ID 'real-id', got %d", count)
	}
}

// Bug 2 regression: file echoes have no sentHash; wsMsg from self must still
// reconcile (replace) the file-echo rather than inserting a duplicate.
func TestFileEchoReconciledBySelfMessagePath(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m := New(nil, "discord", nil, nil, "general", "terminal-user", "discord-id-123", "discord-user", "Discord User", "test-guild", nil, "", "", "", nil, "")

	// Simulate what sendFileWithEcho does: echo added, no sentHash registered.
	m.msgs = []model.Message{{
		ID:        "file-echo-999",
		Username:  "terminal-user",
		Content:   "here is a file",
		Channel:   "general",
		Timestamp: base,
	}}
	// sentHashes is intentionally empty (no hash for file echoes).

	// Relay broadcasts back with real ID and same username.
	updatedModel, _ := m.Update(networkEventMsg{Network: "discord", Kind: network.EventMessage, Message: &model.Message{
		ID:        "discord-file-456",
		Username:  "terminal-user",
		UserID:    "discord-id-123",
		Content:   "here is a file",
		Channel:   "general",
		Timestamp: base.Add(time.Second),
	}})
	updated := updatedModel.(Model)
	if len(updated.msgs) != 1 {
		t.Fatalf("file echo not reconciled: got %d messages (want 1)", len(updated.msgs))
	}
	if updated.msgs[0].ID != "discord-file-456" {
		t.Fatalf("expected real server ID after file echo reconcile, got %q", updated.msgs[0].ID)
	}
}

// Bug 3 regression: deduplicateSentMessage must not skip an alias when it
// matches the incoming username only case-insensitively (alias stored with
// exact case that differs from incoming).
func TestDeduplicateSentMessageCaseMismatch(t *testing.T) {
	m := New(nil, "", nil, nil, "general", "Alice", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.sentHashes[contentHash("Alice", "general", "hello")] = time.Now()

	// Relay broadcasts with lowercase username — EqualFold would skip "Alice".
	got := m.deduplicateSentMessage("alice", "general", "hello")
	if !got {
		t.Fatal("deduplicateSentMessage returned false for case-mismatched username (want true)")
	}
	if len(m.sentHashes) != 0 {
		t.Fatal("expected sentHashes entry to be removed after dedup")
	}
}

// Bug 1+3 combined: upgradeEchoID already ran, wsMsg arrives with
// case-different username — must not duplicate.
func TestNoDuplicateWhenUpgradedEchoAndCaseMismatch(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m := New(nil, "discord", nil, nil, "general", "Alice", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.sentHashes[contentHash("Alice", "general", "hello")] = time.Now()

	// upgradeEchoID already ran: echo-XXX → real-id.
	m.msgs = []model.Message{
		{ID: "other", Username: "bob", Content: "hi", Channel: "general", Timestamp: base.Add(time.Second)},
		{ID: "real-id", Username: "Alice", Content: "hello", Channel: "general", Timestamp: base.Add(2 * time.Second)},
	}

	updatedModel, _ := m.Update(networkEventMsg{Network: "discord", Kind: network.EventMessage, Message: &model.Message{
		ID:        "real-id",
		Username:  "alice",
		Content:   "hello",
		Channel:   "general",
		Timestamp: base, // earlier than "other" — triggers old insertSorted bug
	}})
	updated := updatedModel.(Model)
	if len(updated.msgs) != 2 {
		t.Fatalf("got %d messages after wsMsg (want 2)", len(updated.msgs))
	}
	count := 0
	for _, msg := range updated.msgs {
		if msg.ID == "real-id" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 entry with ID 'real-id', got %d", count)
	}
}
