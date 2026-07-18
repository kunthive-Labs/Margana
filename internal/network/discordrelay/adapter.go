// Package discordrelay implements network.Network on top of Marga's existing
// relay client: a WebSocket event stream, a webhook/relay sender, a REST
// history fetcher, and a guild lookup client. It is a thin composition — the
// wrapped packages keep all their behavior (reconnect/backoff, dedup, etc.).
package discordrelay

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/guilds"
	"github.com/kunthive-Labs/Margana/internal/history"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
	"github.com/kunthive-Labs/Margana/internal/webhook"
	"github.com/kunthive-Labs/Margana/internal/wsclient"
)

// ID is the stable handle for the Discord-via-relay network.
const ID network.NetworkID = "discord"

var _ network.Network = (*Adapter)(nil)

// Adapter wires the relay client packages behind the network.Network interface.
type Adapter struct {
	ws       *wsclient.Client
	sender   *webhook.Sender
	fetcher  *history.Fetcher
	guilds   *guilds.Client
	identity network.Identity

	events chan network.Event
	done   chan struct{}
}

// New builds a Discord-via-relay adapter from config. The wrapped clients are
// constructed exactly as cmd/marga did before the adapter existed.
func New(cfg *config.Config) *Adapter {
	ws := wsclient.New(cfg.Server.WebsocketURL, cfg.General.Username, cfg.General.Channel, cfg.Server.APIKey)
	sender := webhook.New(cfg.Server.WebhookURL, cfg.Server.RelayURL, cfg.Server.APIKey, cfg.General.Username, cfg.General.DiscordAvatarURL, cfg.General.GuildID)
	fetcher := history.New(cfg.Server.RelayURL, cfg.Server.APIKey)
	if cfg.General.GuildID != "" {
		fetcher = fetcher.WithGuild(cfg.General.GuildID)
	}
	return &Adapter{
		ws:      ws,
		sender:  sender,
		fetcher: fetcher,
		guilds:  guilds.NewClient(cfg.Server.RelayURL, cfg.Server.APIKey),
		identity: network.Identity{
			Network:     ID,
			UserID:      cfg.General.DiscordID,
			Username:    cfg.General.Username,
			DisplayName: cfg.General.DiscordGlobalName,
			AvatarURL:   cfg.General.DiscordAvatarURL,
		},
		events: make(chan network.Event, 256),
		done:   make(chan struct{}),
	}
}

// SetLogger directs the adapter's WebSocket client diagnostics to l. Off by
// default. Wired from cmd/marga when logging is enabled.
func (a *Adapter) SetLogger(l *log.Logger) {
	a.ws.SetLogger(l)
}

func (a *Adapter) ID() network.NetworkID { return ID }

func (a *Adapter) Capabilities() network.Capabilities {
	return network.Capabilities{
		Edit:       true,
		FileUpload: true,
		Typing:     true,
		Presence:   true,
		History:    true,
		ServerList: true,
		Reactions:  false,
		Encryption: false, // the relay handles transport; no per-message E2EE
	}
}

func (a *Adapter) Connect(ctx context.Context) error {
	go a.ws.ConnectWithRetry(ctx)
	go a.fanIn(ctx)
	return nil
}

func (a *Adapter) Disconnect() error {
	close(a.done)
	return a.ws.Close()
}

func (a *Adapter) CurrentUser() network.Identity { return a.identity }

func (a *Adapter) ListServers(ctx context.Context) ([]network.Server, error) {
	gs, err := a.guilds.FetchGuilds("")
	if err != nil {
		return nil, err
	}
	servers := make([]network.Server, 0, len(gs))
	for _, g := range gs {
		servers = append(servers, network.Server{ID: g.ID, Name: g.Name})
	}
	return servers, nil
}

func (a *Adapter) ListChannels(ctx context.Context, serverID string) ([]network.ChannelRef, error) {
	names, err := a.fetcher.FetchChannels()
	if err != nil {
		return nil, err
	}
	refs := make([]network.ChannelRef, 0, len(names))
	for _, name := range names {
		// For the relay, the channel name is also its native id.
		refs = append(refs, network.ChannelRef{Network: ID, ServerID: serverID, ID: name, Name: name})
	}
	return refs, nil
}

func (a *Adapter) Subscribe(ref network.ChannelRef) error {
	return a.ws.Subscribe(ref.ID)
}

func (a *Adapter) Unsubscribe(ref network.ChannelRef) error {
	return a.ws.Unsubscribe(ref.ID)
}

func (a *Adapter) FetchHistory(ctx context.Context, ref network.ChannelRef, limit int, before *time.Time) ([]model.Message, error) {
	msgs, err := a.fetcher.Fetch(ref.ID, limit, before)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		msgs[i].Network = string(ID)
	}
	return msgs, nil
}

func (a *Adapter) Send(ctx context.Context, ref network.ChannelRef, content, replyToID string) (string, error) {
	return a.sender.Send(content, ref.ID, replyToID)
}

func (a *Adapter) SendFile(ctx context.Context, ref network.ChannelRef, path, content string) (string, error) {
	return a.sender.SendFile(path, ref.ID, content)
}

func (a *Adapter) Edit(ctx context.Context, ref network.ChannelRef, messageID, content string) error {
	return a.sender.Edit(messageID, ref.ID, content)
}

// React is unsupported: the relay does not yet emit or accept reaction events,
// so Capabilities().Reactions stays false and this is an explicit no-op error.
func (a *Adapter) React(ref network.ChannelRef, messageID, emoji string) error {
	return errors.New("discord: reactions not supported by the relay")
}

func (a *Adapter) SetStatus(status string) error {
	return a.ws.SendStatus(status)
}

// FetchSince implements network.SinceFetcher for relay catch-up polling.
func (a *Adapter) FetchSince(ctx context.Context, ref network.ChannelRef, since time.Time) ([]model.Message, error) {
	msgs, err := a.fetcher.FetchSinceMessages(ref.ID, since)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		msgs[i].Network = string(ID)
	}
	return msgs, nil
}

func (a *Adapter) Events() <-chan network.Event { return a.events }

// WSClient returns the underlying WebSocket client. It and the sibling raw
// accessors below let the TUI/commands keep using the wrapped clients during
// the incremental migration; new code should prefer the interface methods.
func (a *Adapter) WSClient() *wsclient.Client { return a.ws }
func (a *Adapter) Sender() *webhook.Sender    { return a.sender }
func (a *Adapter) Fetcher() *history.Fetcher  { return a.fetcher }

// fanIn collapses the five wsclient channels into the unified Event stream,
// stamping every event with this network's ID.
func (a *Adapter) fanIn(ctx context.Context) {
	defer close(a.events)
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.done:
			return
		case m := <-a.ws.Messages():
			m.Network = string(ID)
			msg := m
			a.emit(ctx, network.Event{Network: ID, Kind: network.EventMessage, Message: &msg})
		case t := <-a.ws.TypingEvents():
			te := t
			a.emit(ctx, network.Event{Network: ID, Kind: network.EventTyping, Typing: &te})
		case p := <-a.ws.Presences():
			pr := p
			a.emit(ctx, network.Event{Network: ID, Kind: network.EventPresence, Presence: &pr})
		case sc := <-a.ws.StatusChanges():
			var retryAt time.Time
			if sc.RetryIn > 0 {
				retryAt = time.Now().Add(sc.RetryIn)
			}
			a.emit(ctx, network.Event{Network: ID, Kind: network.EventStatus, State: network.ConnState(sc.Status), Err: sc.Err, RetryAt: retryAt})
		case u := <-a.ws.TerminalUsers():
			a.emit(ctx, network.Event{Network: ID, Kind: network.EventPresentUsers, Users: u})
		}
	}
}

func (a *Adapter) emit(ctx context.Context, ev network.Event) {
	select {
	case a.events <- ev:
	case <-ctx.Done():
	case <-a.done:
	}
}
