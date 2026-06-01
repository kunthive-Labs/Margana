// Package network defines the protocol-neutral adapter interface that lets
// Marga speak to multiple chat networks (Discord-via-relay, Matrix, IRC, ...)
// behind one TUI. Each concrete network lives in a subpackage and implements
// Network; the TUI multiplexes their Event streams.
package network

import "github.com/kunthive-Labs/Margana/internal/model"

// NetworkID is the stable handle for one configured connection ("discord",
// "matrix"). It namespaces channels, keyring entries, and per-network identity.
type NetworkID string

// Capabilities lets the TUI gray out unsupported actions instead of erroring.
type Capabilities struct {
	Edit       bool
	FileUpload bool
	Typing     bool
	Presence   bool
	History    bool
	ServerList bool // false => flat channel list, no "servers"
	Reactions  bool
	Encryption bool // adapter transparently handles end-to-end encrypted rooms
}

// Server is a top-level grouping of channels (Discord guild, Matrix space,
// Slack workspace). Serverless networks (IRC, flat Matrix) report none.
type Server struct {
	ID   string
	Name string
}

// ChannelRef is the fully-qualified channel identity across networks.
type ChannelRef struct {
	Network  NetworkID
	ServerID string // "" for serverless / flat networks
	ID       string // network-native channel/room id
	Name     string // human display name
}

// Key is a stable identifier suitable for map keys and DB scoping.
func (c ChannelRef) Key() string {
	return string(c.Network) + "\x1f" + c.ServerID + "\x1f" + c.ID
}

// Identity is the authenticated user on a given network.
type Identity struct {
	Network     NetworkID
	UserID      string // network-native id (Discord snowflake, Matrix MXID)
	Username    string // handle used for sending / mention matching
	DisplayName string
	AvatarURL   string
}

// ConnState mirrors the connection lifecycle the TUI status bar renders.
type ConnState string

const (
	StateConnected    ConnState = "connected"
	StateDisconnected ConnState = "disconnected"
	StateReconnecting ConnState = "reconnecting"
)

// EventKind tags the Event union.
type EventKind int

const (
	EventMessage      EventKind = iota // create OR update; disambiguate via Message.EventType
	EventTyping                        // a user is typing
	EventPresence                      // a user's presence/status changed
	EventStatus                        // this adapter's connection state changed
	EventChannelList                   // server/channel topology refreshed
	EventPresentUsers                  // "who is online" snapshot
)

// Event is the single envelope the TUI consumes from every adapter. Network is
// always set so the multiplexer can fan-in N adapters without losing origin.
type Event struct {
	Network  NetworkID
	Kind     EventKind
	Message  *model.Message
	Typing   *model.TypingEvent
	Presence *model.UserPresence
	State    ConnState
	Err      error
	Channels []ChannelRef
	Users    []string
}
