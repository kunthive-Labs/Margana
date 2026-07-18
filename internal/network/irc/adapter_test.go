package irc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/lrstanley/girc"

	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/network"
)

// compile-time assertion (also asserted in adapter.go) — kept here so the test
// package fails loudly if the interface drifts.
var _ network.Network = (*Adapter)(nil)

func newTestAdapter() *Adapter {
	return &Adapter{
		id:       "irc",
		server:   "irc.example.test",
		port:     6667,
		nick:     "marga",
		identity: network.Identity{Network: "irc", UserID: "marga", Username: "marga"},
		events:   make(chan network.Event, 64),
		subs:     make(map[string]bool),
		members:  make(map[string]map[string]bool),
	}
}

func TestNewFromConfig(t *testing.T) {
	a := New(config.NetworkConfig{ID: "libera", Type: "irc", Enabled: true, Server: "irc.libera.chat", TLS: true, Nick: "marga"})
	if a.ID() != network.NetworkID("libera") {
		t.Errorf("ID: got %q, want per-config id", a.ID())
	}
	if a.port != 6697 {
		t.Errorf("default TLS port: got %d, want 6697", a.port)
	}
	// A second IRC network must get a distinct ID so they can coexist.
	b := New(config.NetworkConfig{ID: "oftc", Type: "irc", Enabled: true, Server: "irc.oftc.net"})
	if b.ID() == a.ID() {
		t.Errorf("two IRC networks share an ID %q", a.ID())
	}
	if b.port != 6667 {
		t.Errorf("default non-TLS port: got %d, want 6667", b.port)
	}
}

func TestCapabilitiesAllFalse(t *testing.T) {
	got := newTestAdapter().Capabilities()
	want := network.Capabilities{} // IRC supports none of the optional features
	if got != want {
		t.Errorf("Capabilities: got %+v, want all-false %+v", got, want)
	}
}

func TestOnMessageChannelPrivmsg(t *testing.T) {
	a := newTestAdapter()
	e := girc.Event{
		Command: girc.PRIVMSG,
		Source:  &girc.Source{Name: "bob"},
		Params:  []string{"#general", "hello world"},
	}

	a.onMessage(nil, e)

	ev := <-a.Events()
	if ev.Kind != network.EventMessage || ev.Message == nil {
		t.Fatalf("expected a message event, got %+v", ev)
	}
	m := ev.Message
	if m.Network != string(a.ID()) {
		t.Errorf("network not stamped: got %q, want %q", m.Network, a.ID())
	}
	if m.Channel != "#general" {
		t.Errorf("channel: got %q, want #general", m.Channel)
	}
	if m.Username != "bob" {
		t.Errorf("username: got %q, want bob", m.Username)
	}
	if m.Content != "hello world" {
		t.Errorf("content: got %q, want %q", m.Content, "hello world")
	}
	if m.EventType != "message_create" {
		t.Errorf("event type: got %q, want message_create", m.EventType)
	}
	if m.ID == "" {
		t.Error("expected a synthetic message id")
	}
}

func TestOnMessageUsesMsgidTag(t *testing.T) {
	a := newTestAdapter()
	e := girc.Event{
		Command: girc.PRIVMSG,
		Source:  &girc.Source{Name: "bob"},
		Params:  []string{"#general", "tagged"},
		Tags:    girc.Tags{"msgid": "srv-42"},
	}

	a.onMessage(nil, e)

	m := (<-a.Events()).Message
	if m.ID != "srv-42" {
		t.Errorf("message id: got %q, want the IRCv3 msgid srv-42", m.ID)
	}
}

func TestOnMessagePrivateMessageChannelIsSender(t *testing.T) {
	a := newTestAdapter()
	// A PM targets our own nick; the "channel" should be the sender's nick.
	e := girc.Event{
		Command: girc.PRIVMSG,
		Source:  &girc.Source{Name: "carol"},
		Params:  []string{"marga", "psst"},
	}

	a.onMessage(nil, e)

	m := (<-a.Events()).Message
	if m.Channel != "carol" {
		t.Errorf("PM channel: got %q, want the sender nick carol", m.Channel)
	}
}

func TestOnNamesEmitsPresentUsers(t *testing.T) {
	a := newTestAdapter()
	// RPL_NAMREPLY: <me> <symbol> <channel> :<nick list> (with membership prefixes).
	e := girc.Event{
		Command: girc.RPL_NAMREPLY,
		Params:  []string{"marga", "=", "#general", "alice bob @carol +dave"},
	}

	a.onNames(nil, e)

	ev := <-a.Events()
	if ev.Kind != network.EventPresentUsers {
		t.Fatalf("expected a present-users event, got kind %d", ev.Kind)
	}
	got := make(map[string]bool)
	for _, u := range ev.Users {
		got[u] = true
	}
	for _, want := range []string{"alice", "bob", "carol", "dave"} {
		if !got[want] {
			t.Errorf("expected %q in present users; got %v", want, ev.Users)
		}
	}
}

func TestJoinPartQuitTrackPresence(t *testing.T) {
	a := newTestAdapter()

	drain := func() []string {
		select {
		case ev := <-a.Events():
			if ev.Kind != network.EventPresentUsers {
				t.Fatalf("expected present-users event, got kind %d", ev.Kind)
			}
			return ev.Users
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for a present-users event")
			return nil
		}
	}

	a.onJoin(nil, girc.Event{Command: girc.JOIN, Source: &girc.Source{Name: "alice"}, Params: []string{"#general"}})
	if u := drain(); len(u) != 1 || u[0] != "alice" {
		t.Fatalf("after alice join: got %v", u)
	}
	a.onJoin(nil, girc.Event{Command: girc.JOIN, Source: &girc.Source{Name: "bob"}, Params: []string{"#general"}})
	if u := drain(); len(u) != 2 {
		t.Fatalf("after bob join: got %v", u)
	}
	a.onPart(nil, girc.Event{Command: girc.PART, Source: &girc.Source{Name: "alice"}, Params: []string{"#general"}})
	if u := drain(); len(u) != 1 || u[0] != "bob" {
		t.Fatalf("after alice part: got %v", u)
	}
	a.onQuit(nil, girc.Event{Command: girc.QUIT, Source: &girc.Source{Name: "bob"}})
	if u := drain(); len(u) != 0 {
		t.Fatalf("after bob quit: got %v", u)
	}
}

func TestSubscribeRecordsChannel(t *testing.T) {
	a := newTestAdapter()
	if err := a.Subscribe(network.ChannelRef{Network: a.ID(), ID: "#general", Name: "#general"}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Not connected, so nothing is joined yet, but the intent is recorded and
	// surfaced by ListChannels.
	refs, err := a.ListChannels(context.Background(), "")
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "#general" || refs[0].Name != "#general" {
		t.Fatalf("ListChannels: got %+v, want one #general ref", refs)
	}
}

func TestUnsupportedAndOfflineOperations(t *testing.T) {
	a := newTestAdapter()
	if _, err := a.SendFile(context.Background(), network.ChannelRef{ID: "#c"}, "/tmp/x", ""); err == nil {
		t.Error("SendFile: expected an unsupported error")
	}
	if err := a.Edit(context.Background(), network.ChannelRef{ID: "#c"}, "1", "x"); err == nil {
		t.Error("Edit: expected an unsupported error")
	}
	if _, err := a.Send(context.Background(), network.ChannelRef{ID: "#c"}, "hi", ""); err == nil {
		t.Error("Send: expected a not-connected error")
	}
	if servers, err := a.ListServers(context.Background()); err != nil || servers != nil {
		t.Errorf("ListServers: got (%v, %v), want (nil, nil)", servers, err)
	}
	if msgs, err := a.FetchHistory(context.Background(), network.ChannelRef{ID: "#c"}, 10, nil); err != nil || msgs != nil {
		t.Errorf("FetchHistory: got (%v, %v), want (nil, nil)", msgs, err)
	}
}

func TestConnectDisconnectClosesEvents(t *testing.T) {
	a := newTestAdapter()

	// Drive the lifecycle over a fake connection (net.Pipe) so no real server is
	// ever dialed. The client end is drained so girc's synchronous registration
	// writes never block.
	clientEnd, serverEnd := net.Pipe()
	defer clientEnd.Close()
	defer serverEnd.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := clientEnd.Read(buf); err != nil {
				return
			}
		}
	}()
	a.dial = func() error { return a.client.MockConnect(serverEnd) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let the mock session establish

	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	// Events() must be closed once the run loop stops.
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-a.Events():
			if !ok {
				return // closed — success
			}
		case <-timeout:
			t.Fatal("Events() was not closed after Disconnect")
		}
	}
}
