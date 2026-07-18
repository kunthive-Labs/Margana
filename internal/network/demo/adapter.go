// Package demo provides a scripted, credential-free network.Network adapter
// used by `marga --demo` (or MARGA_DEMO=1). It emits a canned stream of rooms,
// messages, presence, and typing so the TUI can be exercised — for recordings
// (docs/demo.tape / the demo CI job) and manual UI review — without a live
// Discord relay or Matrix homeserver.
package demo

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

var _ network.Network = (*Adapter)(nil)

// step is one scripted event: wait `after`, then post `content` from `user` in
// `channel`. A "typing" step (content == "") shows a typing indicator instead.
type step struct {
	after   time.Duration
	channel string
	user    string
	content string
	typing  bool
}

// Adapter is a scripted, offline network. It never dials anything.
type Adapter struct {
	id       network.NetworkID
	server   string
	me       string
	rooms    []string
	users    []string
	presence map[string]string
	script   []step

	events chan network.Event
	cancel context.CancelFunc
	once   sync.Once
	seq    atomic.Int64
}

// New builds a scripted demo adapter.
func New(id network.NetworkID, server, me string, rooms, users []string, presence map[string]string, script []step) *Adapter {
	return &Adapter{
		id:       id,
		server:   server,
		me:       me,
		rooms:    rooms,
		users:    users,
		presence: presence,
		script:   script,
		events:   make(chan network.Event, 64),
	}
}

func (a *Adapter) ID() network.NetworkID { return a.id }

func (a *Adapter) Capabilities() network.Capabilities {
	return network.Capabilities{
		Edit: true, FileUpload: true, Typing: true, Presence: true,
		History: true, ServerList: true, Reactions: false,
		Encryption: a.id == "matrix",
	}
}

func (a *Adapter) Connect(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)
	go a.run(ctx)
	return nil
}

func (a *Adapter) run(ctx context.Context) {
	defer a.once.Do(func() { close(a.events) })

	a.emit(ctx, network.Event{Network: a.id, Kind: network.EventStatus, State: network.StateConnected})
	a.emit(ctx, network.Event{Network: a.id, Kind: network.EventPresentUsers, Users: append([]string{a.me}, a.users...)})
	for user, status := range a.presence {
		a.emit(ctx, network.Event{Network: a.id, Kind: network.EventPresence, Presence: &model.UserPresence{
			Username: user, Status: status, Online: true, LastSeen: time.Now(), UpdatedAt: time.Now(),
		}})
	}

	// Loop the script so a long recording never goes silent; IDs stay unique.
	for {
		for _, s := range a.script {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.after):
			}
			if s.typing {
				a.emit(ctx, network.Event{Network: a.id, Kind: network.EventTyping, Typing: &model.TypingEvent{
					Type: "typing", Username: s.user, Channel: s.channel,
				}})
				continue
			}
			a.emit(ctx, network.Event{Network: a.id, Kind: network.EventMessage, Message: &model.Message{
				Network: string(a.id), ID: a.nextID(), Username: s.user, Content: s.content,
				Channel: s.channel, Timestamp: time.Now(), EventType: "message_create",
			}})
		}
	}
}

func (a *Adapter) nextID() string {
	return fmt.Sprintf("demo-%s-%d", a.id, a.seq.Add(1))
}

func (a *Adapter) emit(ctx context.Context, ev network.Event) {
	select {
	case a.events <- ev:
	case <-ctx.Done():
	}
}

func (a *Adapter) Disconnect() error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func (a *Adapter) CurrentUser() network.Identity {
	return network.Identity{Network: a.id, UserID: "@" + a.me, Username: a.me}
}

func (a *Adapter) ListServers(ctx context.Context) ([]network.Server, error) {
	return []network.Server{{ID: a.server, Name: a.server}}, nil
}

func (a *Adapter) ListChannels(ctx context.Context, serverID string) ([]network.ChannelRef, error) {
	refs := make([]network.ChannelRef, 0, len(a.rooms))
	for _, r := range a.rooms {
		refs = append(refs, network.ChannelRef{Network: a.id, ServerID: a.server, ID: r, Name: r})
	}
	return refs, nil
}

func (a *Adapter) Subscribe(network.ChannelRef) error   { return nil }
func (a *Adapter) Unsubscribe(network.ChannelRef) error { return nil }

// FetchHistory returns no backlog — the live script fills the view (and briefly
// shows the empty state, which is intentional for the demo).
func (a *Adapter) FetchHistory(ctx context.Context, ref network.ChannelRef, limit int, before *time.Time) ([]model.Message, error) {
	return nil, nil
}

// Send echoes nothing new: the TUI renders its own local echo, and we just hand
// back a canonical id as a real network would.
func (a *Adapter) Send(ctx context.Context, ref network.ChannelRef, content, replyToID string) (string, error) {
	return a.nextID(), nil
}

func (a *Adapter) SendFile(ctx context.Context, ref network.ChannelRef, path, content string) (string, error) {
	return a.nextID(), nil
}

func (a *Adapter) Edit(ctx context.Context, ref network.ChannelRef, messageID, content string) error {
	return nil
}

// React is unsupported by the scripted demo (Capabilities().Reactions == false).
func (a *Adapter) React(ref network.ChannelRef, messageID, emoji string) error {
	return fmt.Errorf("demo: reactions not supported")
}

func (a *Adapter) SetStatus(status string) error { return nil }

func (a *Adapter) Events() <-chan network.Event { return a.events }

// Adapters returns the default demo networks: a Matrix-flavored network (with an
// E2EE room) and a Discord-flavored network, so the recording can show live
// chat and a network switch (Ctrl+T).
func Adapters() []network.Network {
	matrix := New(
		"matrix", "matrix.org", "you",
		[]string{"general", "rust-nerds", "e2ee-room"},
		[]string{"alice", "bob", "priya"},
		map[string]string{"alice": "shipping the relay refactor", "priya": "reviewing PRs"},
		[]step{
			{after: 900 * time.Millisecond, channel: "general", user: "alice", content: "morning! marga's multi-network view is slick 🐚"},
			{after: 1200 * time.Millisecond, channel: "general", user: "bob", typing: true},
			{after: 900 * time.Millisecond, channel: "general", user: "bob", content: "agreed — one TUI for Discord *and* Matrix"},
			{after: 1600 * time.Millisecond, channel: "e2ee-room", user: "priya", content: "this room is end-to-end encrypted 🔒"},
			{after: 1800 * time.Millisecond, channel: "general", user: "alice", content: "and it works over ssh/tmux without a browser"},
		},
	)
	discord := New(
		"discord", "Kunthive Labs", "you",
		[]string{"general", "dev", "showcase"},
		[]string{"nova", "kai"},
		map[string]string{"nova": "building in public"},
		[]step{
			{after: 1400 * time.Millisecond, channel: "dev", user: "nova", content: "pushed the notifications patch 🔔"},
			{after: 1700 * time.Millisecond, channel: "dev", user: "kai", content: "desktop + bell on mention — nice"},
			{after: 2000 * time.Millisecond, channel: "general", user: "nova", content: "switch networks with Ctrl+T ⚡"},
		},
	)
	return []network.Network{matrix, discord}
}
