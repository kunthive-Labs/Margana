package relay

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Guild is a server/guild descriptor for GET /api/guilds.
type Guild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Channel is a channel descriptor for the channel-listing endpoints. Type is a
// string ("text") to match what internal/history decodes and filters on.
type Channel struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// Backend is the pluggable delivery target behind the relay's REST + WebSocket
// surface. It is the single seam between the network-neutral relay machinery
// (store, hub, HTTP server) and a concrete chat network.
type Backend interface {
	// Publish delivers an outbound message and returns the stored form, with a
	// server-assigned ID and timestamp filled in.
	Publish(m Message) (Message, error)
	// ListChannels returns the channels the backend exposes.
	ListChannels() ([]Channel, error)
	// ListGuilds returns the servers/guilds the backend exposes, filtered by a
	// case-insensitive substring query (an empty query returns all).
	ListGuilds(query string) ([]Guild, error)
}

// LocalBackend is a self-contained echo relay: messages POSTed to it are
// persisted to the store and broadcast straight back to the channel's WebSocket
// subscribers. That makes `docker compose up` a working loopback chat with
// history — no Discord account, bot token, or gateway connection required.
type LocalBackend struct {
	store *Store
	hub   *Hub

	mu        sync.Mutex
	channels  map[string]struct{}
	guildName string
}

// NewLocalBackend wires the echo backend to its store and hub. defaultChannel,
// when non-empty, seeds the channel list so clients can list a channel before
// any message has been sent.
func NewLocalBackend(store *Store, hub *Hub, defaultChannel string) *LocalBackend {
	b := &LocalBackend{
		store:     store,
		hub:       hub,
		channels:  make(map[string]struct{}),
		guildName: "local",
	}
	if defaultChannel != "" {
		b.channels[defaultChannel] = struct{}{}
	}
	return b
}

// Publish persists the message and broadcasts a message_create to the channel's
// subscribers.
func (b *LocalBackend) Publish(m Message) (Message, error) {
	if m.ID == "" {
		m.ID = newID()
	}
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now().UTC()
	}
	// The neutral send contract carries a username, not a stable user id, so we
	// key authorship by username. That is what lets DELETE /api/users/{id}/...
	// and POST /api/delete-my-data work with the username the client sends.
	if m.UserID == "" {
		m.UserID = m.Username
	}

	if err := b.store.Insert(m); err != nil {
		return Message{}, err
	}
	b.mu.Lock()
	if m.Channel != "" {
		b.channels[m.Channel] = struct{}{}
	}
	b.mu.Unlock()

	b.hub.BroadcastMessage(m, "message_create")
	return m, nil
}

// ListChannels returns the seeded channel plus every channel a message has been
// posted to, sorted by name.
func (b *LocalBackend) ListChannels() ([]Channel, error) {
	b.mu.Lock()
	names := make([]string, 0, len(b.channels))
	for name := range b.channels {
		names = append(names, name)
	}
	b.mu.Unlock()

	sort.Strings(names)
	chans := make([]Channel, 0, len(names))
	for _, name := range names {
		chans = append(chans, Channel{Name: name, Type: "text"})
	}
	return chans, nil
}

// ListGuilds returns the single synthetic "local" guild, subject to the query
// filter.
func (b *LocalBackend) ListGuilds(query string) ([]Guild, error) {
	g := Guild{ID: "local", Name: b.guildName}
	if query != "" && !strings.Contains(strings.ToLower(g.Name), strings.ToLower(query)) {
		return []Guild{}, nil
	}
	return []Guild{g}, nil
}

// --- Discord backend boundary ---------------------------------------------
//
// The real Discord backend — the gateway bot that holds the WebSocket to
// Discord and translates its events into Marga's neutral wire types — lives in
// the separate kunthive-Labs/marga-discord-relay repository, by design: it
// needs a bot token and a live gateway connection that this self-host reference
// deliberately does not carry. This stub keeps the Backend seam explicit so
// that repository (or your own bridge) can drop in without touching the store,
// hub, or HTTP server.

var errDiscordNotBuilt = errors.New("discord backend is not built in the reference relay; run the external kunthive-Labs/marga-discord-relay for a live Discord bridge")

type discordBackend struct{}

// NewDiscordStub returns a Backend that reports the Discord bridge is not part
// of this reference relay. Selecting it (RELAY_BACKEND=discord) documents the
// boundary rather than silently echoing.
func NewDiscordStub() Backend { return discordBackend{} }

func (discordBackend) Publish(Message) (Message, error)   { return Message{}, errDiscordNotBuilt }
func (discordBackend) ListChannels() ([]Channel, error)   { return nil, errDiscordNotBuilt }
func (discordBackend) ListGuilds(string) ([]Guild, error) { return nil, errDiscordNotBuilt }

// newID returns a random opaque message id. Clients treat ids as opaque strings
// (used for replies and edits), so a random hex id stands in for a Discord
// snowflake in the reference relay.
func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Collisions are astronomically unlikely for a single-process relay.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}
