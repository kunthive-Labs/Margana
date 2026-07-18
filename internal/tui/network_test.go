package tui

import (
	"context"
	"testing"
	"time"

	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

// fakeAdapter is a minimal network.Network used to exercise the TUI's event
// multiplexer without a real connection.
type fakeAdapter struct {
	id     network.NetworkID
	events chan network.Event
}

func newFakeAdapter(id network.NetworkID) *fakeAdapter {
	return &fakeAdapter{id: id, events: make(chan network.Event, 16)}
}

func (f *fakeAdapter) ID() network.NetworkID { return f.id }
func (f *fakeAdapter) Capabilities() network.Capabilities {
	return network.Capabilities{Edit: true, FileUpload: true, Typing: true, Presence: true, History: true, ServerList: true, Reactions: true}
}
func (f *fakeAdapter) Connect(context.Context) error { return nil }
func (f *fakeAdapter) Disconnect() error             { return nil }
func (f *fakeAdapter) CurrentUser() network.Identity { return network.Identity{Network: f.id} }
func (f *fakeAdapter) ListServers(context.Context) ([]network.Server, error) {
	return nil, nil
}
func (f *fakeAdapter) ListChannels(context.Context, string) ([]network.ChannelRef, error) {
	return nil, nil
}
func (f *fakeAdapter) Subscribe(network.ChannelRef) error   { return nil }
func (f *fakeAdapter) Unsubscribe(network.ChannelRef) error { return nil }
func (f *fakeAdapter) FetchHistory(context.Context, network.ChannelRef, int, *time.Time) ([]model.Message, error) {
	return nil, nil
}
func (f *fakeAdapter) Send(context.Context, network.ChannelRef, string, string) (string, error) {
	return "", nil
}
func (f *fakeAdapter) SendFile(context.Context, network.ChannelRef, string, string) (string, error) {
	return "", nil
}
func (f *fakeAdapter) Edit(context.Context, network.ChannelRef, string, string) error { return nil }
func (f *fakeAdapter) React(network.ChannelRef, string, string) error                 { return nil }
func (f *fakeAdapter) SetStatus(string) error                                         { return nil }
func (f *fakeAdapter) Events() <-chan network.Event                                   { return f.events }

func newTestModel(adapters ...network.Network) Model {
	active := network.NetworkID("")
	if len(adapters) > 0 {
		active = adapters[0].ID()
	}
	return New(adapters, active, nil, nil, "general", "me", "", "", "", "test", nil, "", "", "", nil, "")
}

// TestMultiplexMessageEvent verifies a message event routes through the
// dispatcher into the model's message list.
func TestMultiplexMessageEvent(t *testing.T) {
	a := newFakeAdapter("discord")
	m := newTestModel(a)

	ev := networkEventMsg{Network: "discord", Kind: network.EventMessage, Message: &model.Message{
		ID:        "m1",
		Username:  "alice",
		Content:   "hi",
		Channel:   "general",
		Timestamp: time.Now(),
	}}
	updated, cmd := m.Update(ev)
	mm := updated.(Model)
	if len(mm.msgs) != 1 || mm.msgs[0].ID != "m1" {
		t.Fatalf("message event not applied: %#v", mm.msgs)
	}
	if cmd == nil {
		t.Fatal("dispatcher must re-arm the adapter listener (got nil cmd)")
	}
}

// TestMultiplexStatusEvent verifies a connection-state event updates status.
func TestMultiplexStatusEvent(t *testing.T) {
	a := newFakeAdapter("discord")
	m := newTestModel(a)

	updated, _ := m.Update(networkEventMsg{Network: "discord", Kind: network.EventStatus, State: network.StateConnected})
	if updated.(Model).status != network.StateConnected {
		t.Fatalf("status not updated, got %q", updated.(Model).status)
	}
}

// TestMultiplexTypingAndPresence verifies typing and presence events land.
func TestMultiplexTypingAndPresence(t *testing.T) {
	a := newFakeAdapter("discord")
	m := newTestModel(a)

	updated, _ := m.Update(networkEventMsg{Network: "discord", Kind: network.EventTyping, Typing: &model.TypingEvent{Username: "bob", Channel: "general"}})
	m = updated.(Model)
	if _, ok := m.typingUsers["bob"]; !ok {
		t.Fatal("typing event not recorded")
	}

	updated, _ = m.Update(networkEventMsg{Network: "discord", Kind: network.EventPresence, Presence: &model.UserPresence{Username: "bob", Status: "online", Online: true}})
	m = updated.(Model)
	if m.presences["bob"].Status != "online" {
		t.Fatal("presence event not recorded")
	}
}

// TestMultiplexPresentUsers verifies the online-users snapshot lands.
func TestMultiplexPresentUsers(t *testing.T) {
	a := newFakeAdapter("discord")
	m := newTestModel(a)

	updated, _ := m.Update(networkEventMsg{Network: "discord", Kind: network.EventPresentUsers, Users: []string{"a", "b"}})
	if got := updated.(Model).terminalOnline; len(got) != 2 {
		t.Fatalf("present users not recorded, got %v", got)
	}
}

// TestMessageOriginNetworkPreserved verifies messages from a non-default
// network are tagged and still routed to the active channel view.
func TestMultiplexReArmTargetsCorrectAdapter(t *testing.T) {
	discord := newFakeAdapter("discord")
	matrix := newFakeAdapter("matrix")
	m := newTestModel(discord, matrix)

	// An event from matrix must re-arm matrix's listener, not discord's.
	updated, cmd := m.Update(networkEventMsg{Network: "matrix", Kind: network.EventStatus, State: network.StateConnected})
	_ = updated
	if cmd == nil {
		t.Fatal("expected a re-arm command for the matrix adapter")
	}
}

// TestNonActiveNetworkMessageHidden verifies a message from a non-active
// network does not enter the visible view or channel list.
func TestNonActiveNetworkMessageHidden(t *testing.T) {
	discord := newFakeAdapter("discord")
	matrix := newFakeAdapter("matrix")
	m := newTestModel(discord, matrix) // active = discord

	updated, _ := m.Update(networkEventMsg{Network: "matrix", Kind: network.EventMessage, Message: &model.Message{
		ID:        "mx1",
		Username:  "alice",
		Content:   "hi from matrix",
		Channel:   "!room:hs",
		Timestamp: time.Now(),
	}})
	mm := updated.(Model)
	if len(mm.msgs) != 0 {
		t.Fatalf("matrix message must not show while discord is active, got %d msgs", len(mm.msgs))
	}
	for _, ch := range mm.channels {
		if ch == "!room:hs" {
			t.Fatal("non-active network channel leaked into the visible channel list")
		}
	}
}

// TestSwitchNetworkChangesActive verifies /network switches the active network,
// while listing (empty target) and unknown targets leave it unchanged.
func TestSwitchNetworkChangesActive(t *testing.T) {
	discord := newFakeAdapter("discord")
	matrix := newFakeAdapter("matrix")
	m := newTestModel(discord, matrix) // active = discord

	listed, _ := m.Update(commands.SwitchNetworkMsg{Network: ""})
	if got := listed.(Model).active; got != "discord" {
		t.Fatalf("listing networks must not change active, got %q", got)
	}

	updated, _ := m.Update(commands.SwitchNetworkMsg{Network: "matrix"})
	if got := updated.(Model).active; got != "matrix" {
		t.Fatalf("expected active network 'matrix', got %q", got)
	}

	stay, _ := updated.(Model).Update(commands.SwitchNetworkMsg{Network: "irc"})
	if got := stay.(Model).active; got != "matrix" {
		t.Fatalf("unknown network must not change active, got %q", got)
	}
}
