package network

import (
	"context"
	"time"

	"github.com/kunthive-Labs/Margana/internal/model"
)

// Network is one chat network Marga can talk to. Implementations may connect
// directly (Matrix, IRC) or through the relay (Discord, Slack); the TUI does
// not care which. All event delivery happens on Events().
type Network interface {
	ID() NetworkID
	Capabilities() Capabilities

	// Connect starts the read+reconnect loop bound to ctx and returns once the
	// initial connection attempt has been kicked off. Events flow on Events()
	// until ctx is cancelled or Disconnect is called.
	Connect(ctx context.Context) error
	Disconnect() error

	// CurrentUser is the authenticated identity. Valid after Connect.
	CurrentUser() Identity

	// Topology.
	ListServers(ctx context.Context) ([]Server, error)
	ListChannels(ctx context.Context, serverID string) ([]ChannelRef, error)

	// Subscription. ref.Network must equal ID().
	Subscribe(ref ChannelRef) error
	Unsubscribe(ref ChannelRef) error

	// FetchHistory returns a page of messages (newest-first); before==nil means
	// the latest page.
	FetchHistory(ctx context.Context, ref ChannelRef, limit int, before *time.Time) ([]model.Message, error)

	// Outbound. Send/SendFile return the canonical message ID the network assigned.
	Send(ctx context.Context, ref ChannelRef, content, replyToID string) (string, error)
	SendFile(ctx context.Context, ref ChannelRef, path, content string) (string, error)
	Edit(ctx context.Context, ref ChannelRef, messageID, content string) error

	// SetStatus broadcasts presence (no-op if !Capabilities().Presence).
	SetStatus(status string) error

	// Events is the fan-in source. Closed when the adapter fully stops.
	Events() <-chan Event
}

// SinceFetcher is an optional capability for relay-backed adapters that must
// poll for messages newer than a timestamp (to catch up missed events).
// Adapters with a live event stream (Matrix) do not implement it.
type SinceFetcher interface {
	FetchSince(ctx context.Context, ref ChannelRef, since time.Time) ([]model.Message, error)
}
