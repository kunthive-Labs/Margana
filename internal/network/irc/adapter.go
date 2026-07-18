// Package irc implements network.Network by connecting directly to an IRC
// server via the pure-Go, CGo-free girc library. Like the Matrix adapter it
// needs no relay: it dials the server, (optionally) authenticates with SASL,
// joins the channels the TUI subscribes to, and maps PRIVMSG / JOIN / PART /
// QUIT / NAMES traffic onto the neutral network.Event stream. IRC has no
// server-side history, message editing, typing, or presence broadcast, so
// those capabilities are reported as unsupported.
//
// Each configured IRC connection is its own adapter with a per-config ID, so
// several IRC networks (e.g. Libera + OFTC) can run side by side under one TUI.
package irc

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lrstanley/girc"

	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
	"github.com/kunthive-Labs/Margana/internal/network/credstore"
)

var _ network.Network = (*Adapter)(nil)

// Adapter is a direct IRC client behind the network.Network interface.
type Adapter struct {
	id       network.NetworkID
	server   string
	port     int
	useTLS   bool
	nick     string
	saslUser string
	password string // resolved SASL password; used once at connect time

	identity network.Identity

	client *girc.Client
	events chan network.Event

	// dial performs one blocking connection attempt and returns when the
	// session ends. Defaults to the girc client's Connect; tests override it so
	// the lifecycle can be exercised without dialing a real server.
	dial func() error

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	connected atomic.Bool // set by the CONNECTED handler; drives backoff reset
	seq       atomic.Int64

	mu      sync.Mutex
	subs    map[string]bool            // channels we want to be joined to (survives reconnects)
	members map[string]map[string]bool // channel -> set of present nicks
}

// New builds an IRC adapter from its [[networks]] entry. Non-secret connection
// details come from the config; the SASL password is read from the environment
// (MARGA_IRC_PASSWORD) or the OS keyring (service "marga-<id>", key
// "sasl_password"). The ID is per-config, so multiple IRC networks coexist.
func New(n config.NetworkConfig) *Adapter {
	id := network.NetworkID(n.ID)

	port := n.Port
	if port == 0 {
		if n.TLS {
			port = 6697
		} else {
			port = 6667
		}
	}

	nick := strings.TrimSpace(n.Nick)
	if nick == "" {
		nick = "marga"
	}

	return &Adapter{
		id:       id,
		server:   strings.TrimSpace(n.Server),
		port:     port,
		useTLS:   n.TLS,
		nick:     nick,
		saslUser: strings.TrimSpace(n.SASLUser),
		password: resolvePassword(n.ID),
		identity: network.Identity{Network: id, UserID: nick, Username: nick},
		events:   make(chan network.Event, 256),
		subs:     make(map[string]bool),
		members:  make(map[string]map[string]bool),
	}
}

// resolvePassword returns the SASL password, preferring the environment
// (MARGA_IRC_PASSWORD) so headless/CI runs stay non-interactive, then the OS
// keyring. An absent secret yields "" (the adapter then connects without SASL).
func resolvePassword(id string) string {
	if pw := os.Getenv("MARGA_IRC_PASSWORD"); pw != "" {
		return pw
	}
	if pw, err := credstore.Get(id, "sasl_password"); err == nil {
		return pw
	}
	return ""
}

func (a *Adapter) ID() network.NetworkID { return a.id }

// Capabilities is honest about IRC: no server-side history, edits, typing,
// presence broadcast, file upload, reactions, or transparent encryption, and no
// "servers" grouping (channels are flat). The TUI grays out those actions.
func (a *Adapter) Capabilities() network.Capabilities {
	return network.Capabilities{
		Edit:       false,
		FileUpload: false,
		Typing:     false,
		Presence:   false,
		History:    false,
		ServerList: false,
		Reactions:  false,
		Encryption: false,
	}
}

// gircConfig builds the girc client configuration. Debug output is discarded so
// nothing ever reaches the terminal (which would corrupt the TUI), and handler
// panics are recovered (logged to the discarded debug writer) rather than
// crashing Marga. A 433 nick-in-use collision appends an underscore.
func (a *Adapter) gircConfig() girc.Config {
	cfg := girc.Config{
		Server:            a.server,
		Port:              a.port,
		Nick:              a.nick,
		User:              a.nick,
		Name:              a.nick,
		SSL:               a.useTLS,
		Debug:             io.Discard,
		RecoverFunc:       girc.DefaultRecoverHandler,
		HandleNickCollide: func(oldNick string) string { return oldNick + "_" },
	}
	if a.saslUser != "" && a.password != "" {
		cfg.SASL = &girc.SASLPlain{User: a.saslUser, Pass: a.password}
	}
	return cfg
}

// Connect builds the client, registers handlers, and starts the connect/
// reconnect loop bound to ctx. It returns as soon as the loop is kicked off.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.server == "" {
		return fmt.Errorf("irc: server must be configured for network %q", a.id)
	}

	ctx, cancel := context.WithCancel(ctx)
	a.ctx = ctx
	a.cancel = cancel

	if a.client == nil {
		a.client = girc.New(a.gircConfig())
		a.registerHandlers(a.client)
	}
	if a.dial == nil {
		a.dial = a.client.Connect
	}

	go a.run(ctx)
	return nil
}

// run dials the server and, on any disconnect, reports a reconnecting status
// and retries with capped exponential backoff until ctx is cancelled. The
// backoff resets after any session that actually reached CONNECTED. Events()
// is closed exactly once when the loop exits.
func (a *Adapter) run(ctx context.Context) {
	defer a.closeEvents()

	const baseBackoff = 2 * time.Second
	const maxBackoff = 60 * time.Second
	backoff := baseBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		a.connected.Store(false)
		err := a.dial() // blocks until the session ends (error or Close)
		a.resetMembers()

		if ctx.Err() != nil {
			return
		}
		if a.connected.Load() {
			backoff = baseBackoff // a real session happened; reset
		}

		a.emit(network.Event{
			Network: a.id, Kind: network.EventStatus,
			State: network.StateReconnecting, Err: err, RetryAt: time.Now().Add(backoff),
		})

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (a *Adapter) registerHandlers(c *girc.Client) {
	c.Handlers.Add(girc.CONNECTED, a.onConnected)
	c.Handlers.Add(girc.PRIVMSG, a.onMessage)
	c.Handlers.Add(girc.JOIN, a.onJoin)
	c.Handlers.Add(girc.PART, a.onPart)
	c.Handlers.Add(girc.QUIT, a.onQuit)
	c.Handlers.Add(girc.RPL_NAMREPLY, a.onNames)
}

// onConnected fires once registration completes (server welcome). It reports
// connected state and (re)joins every subscribed channel, so subscriptions made
// before connect — and channels lost across a reconnect — are restored.
func (a *Adapter) onConnected(c *girc.Client, _ girc.Event) {
	a.connected.Store(true)
	a.emit(network.Event{Network: a.id, Kind: network.EventStatus, State: network.StateConnected})

	a.mu.Lock()
	channels := make([]string, 0, len(a.subs))
	for ch := range a.subs {
		channels = append(channels, ch)
	}
	a.mu.Unlock()

	for _, ch := range channels {
		c.Cmd.Join(ch)
	}
}

func (a *Adapter) onMessage(_ *girc.Client, e girc.Event) {
	if msg := a.eventToMessage(e); msg != nil {
		a.emit(network.Event{Network: a.id, Kind: network.EventMessage, Message: msg})
	}
}

// eventToMessage maps a PRIVMSG onto the neutral model.Message, or returns nil
// for non-action CTCP (VERSION/PING/etc, which girc answers itself). Channel
// messages carry the channel in Params[0]; a private message uses the sender's
// nick as the channel so DMs surface as a per-user conversation.
func (a *Adapter) eventToMessage(e girc.Event) *model.Message {
	if e.Source == nil {
		return nil
	}
	if ok, _ := e.IsCTCP(); ok && !e.IsAction() {
		return nil
	}

	content := e.Last()
	if e.IsAction() {
		content = e.StripAction()
	}

	channel := e.Source.Name
	if e.IsFromChannel() {
		channel = e.Params[0]
	}

	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	id := ""
	if tag, ok := e.Tags.Get("msgid"); ok {
		id = tag
	}
	if id == "" {
		id = a.nextID()
	}

	return &model.Message{
		Network:   string(a.id),
		EventType: "message_create",
		ID:        id,
		Username:  e.Source.Name,
		UserID:    e.Source.Name,
		Content:   content,
		Channel:   channel,
		Timestamp: ts,
	}
}

func (a *Adapter) onJoin(_ *girc.Client, e girc.Event) {
	if e.Source == nil || len(e.Params) < 1 {
		return
	}
	a.addMember(e.Params[0], e.Source.Name)
	a.emitPresent()
}

func (a *Adapter) onPart(_ *girc.Client, e girc.Event) {
	if e.Source == nil || len(e.Params) < 1 {
		return
	}
	a.removeMember(e.Params[0], e.Source.Name)
	a.emitPresent()
}

func (a *Adapter) onQuit(_ *girc.Client, e girc.Event) {
	if e.Source == nil {
		return
	}
	a.removeMemberAll(e.Source.Name)
	a.emitPresent()
}

// onNames handles RPL_NAMREPLY (353): "<me> <symbol> <channel> :<nick list>".
// The channel is the parameter just before the trailing nick list; each nick
// may carry a membership-prefix (@, +, ...) that is stripped.
func (a *Adapter) onNames(_ *girc.Client, e girc.Event) {
	if len(e.Params) < 2 {
		return
	}
	channel := e.Params[len(e.Params)-2]
	for _, raw := range strings.Fields(e.Last()) {
		if nick := stripNickPrefix(raw); nick != "" {
			a.addMember(channel, nick)
		}
	}
	a.emitPresent()
}

func (a *Adapter) Disconnect() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.client != nil {
		a.client.Close()
	}
	return nil
}

func (a *Adapter) CurrentUser() network.Identity {
	id := a.identity
	// Reflect a collision-adjusted nick (e.g. "marga_") once connected.
	if a.client != nil {
		if nick := a.client.GetNick(); nick != "" {
			id.Username = nick
			id.UserID = nick
		}
	}
	return id
}

// ListServers reports none: IRC is serverless (a flat channel list).
func (a *Adapter) ListServers(ctx context.Context) ([]network.Server, error) {
	return nil, nil
}

// ListChannels returns one ChannelRef per channel we're subscribed to or
// currently joined, with ID and Name both set to the channel name.
func (a *Adapter) ListChannels(ctx context.Context, serverID string) ([]network.ChannelRef, error) {
	set := make(map[string]bool)
	a.mu.Lock()
	for ch := range a.subs {
		set[ch] = true
	}
	a.mu.Unlock()
	if a.client != nil {
		for _, ch := range a.client.ChannelList() {
			set[ch] = true
		}
	}

	channels := make([]string, 0, len(set))
	for ch := range set {
		channels = append(channels, ch)
	}
	sort.Strings(channels)

	refs := make([]network.ChannelRef, 0, len(channels))
	for _, ch := range channels {
		refs = append(refs, network.ChannelRef{Network: a.id, ID: ch, Name: ch})
	}
	return refs, nil
}

// Subscribe records the channel and JOINs it (immediately if connected, else on
// the next CONNECTED).
func (a *Adapter) Subscribe(ref network.ChannelRef) error {
	channel := channelName(ref)
	if channel == "" {
		return fmt.Errorf("irc: subscribe needs a channel")
	}
	a.mu.Lock()
	a.subs[channel] = true
	a.mu.Unlock()
	if a.client != nil && a.client.IsConnected() {
		a.client.Cmd.Join(channel)
	}
	return nil
}

// Unsubscribe forgets the channel and PARTs it if connected.
func (a *Adapter) Unsubscribe(ref network.ChannelRef) error {
	channel := channelName(ref)
	if channel == "" {
		return nil
	}
	a.mu.Lock()
	delete(a.subs, channel)
	a.mu.Unlock()
	if a.client != nil && a.client.IsConnected() {
		a.client.Cmd.Part(channel)
	}
	return nil
}

// FetchHistory returns nothing: IRC has no server-side backlog.
func (a *Adapter) FetchHistory(ctx context.Context, ref network.ChannelRef, limit int, before *time.Time) ([]model.Message, error) {
	return nil, nil
}

// Send delivers a PRIVMSG to the channel or nick. IRC assigns no canonical id
// for our own messages (echo-message is off), so a synthetic id is returned;
// replyToID is ignored (IRC has no native replies).
func (a *Adapter) Send(ctx context.Context, ref network.ChannelRef, content, replyToID string) (string, error) {
	if a.client == nil || !a.client.IsConnected() {
		return "", fmt.Errorf("irc: not connected")
	}
	target := channelName(ref)
	if target == "" {
		return "", fmt.Errorf("irc: send needs a target channel or nick")
	}
	a.client.Cmd.Message(target, content)
	return a.nextID(), nil
}

// SendFile is unsupported: IRC has no file transfer here.
func (a *Adapter) SendFile(ctx context.Context, ref network.ChannelRef, path, content string) (string, error) {
	return "", fmt.Errorf("irc: file upload not supported")
}

// Edit is unsupported: IRC messages cannot be edited.
func (a *Adapter) Edit(ctx context.Context, ref network.ChannelRef, messageID, content string) error {
	return fmt.Errorf("irc: editing messages not supported")
}

// React is unsupported on IRC (Capabilities().Reactions == false).
func (a *Adapter) React(ref network.ChannelRef, messageID, emoji string) error {
	return fmt.Errorf("irc: reactions not supported")
}

// SetStatus maps onto IRC /AWAY (best-effort; a no-op while disconnected).
func (a *Adapter) SetStatus(status string) error {
	if a.client == nil || !a.client.IsConnected() {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "online", "active", "here", "back", "available":
		a.client.Cmd.Back()
	default:
		a.client.Cmd.Away(status)
	}
	return nil
}

func (a *Adapter) Events() <-chan network.Event { return a.events }

// --- helpers ---

func (a *Adapter) emit(ev network.Event) {
	var done <-chan struct{}
	if a.ctx != nil {
		done = a.ctx.Done()
	}
	select {
	case a.events <- ev:
	case <-done:
	}
}

func (a *Adapter) closeEvents() {
	a.once.Do(func() { close(a.events) })
}

func (a *Adapter) nextID() string {
	return fmt.Sprintf("irc-%s-%d", a.id, a.seq.Add(1))
}

func (a *Adapter) addMember(channel, nick string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	set := a.members[channel]
	if set == nil {
		set = make(map[string]bool)
		a.members[channel] = set
	}
	set[nick] = true
}

func (a *Adapter) removeMember(channel, nick string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if set := a.members[channel]; set != nil {
		delete(set, nick)
		if len(set) == 0 {
			delete(a.members, channel)
		}
	}
}

func (a *Adapter) removeMemberAll(nick string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for ch, set := range a.members {
		delete(set, nick)
		if len(set) == 0 {
			delete(a.members, ch)
		}
	}
}

func (a *Adapter) resetMembers() {
	a.mu.Lock()
	a.members = make(map[string]map[string]bool)
	a.mu.Unlock()
}

// presentUsers is the de-duplicated, sorted union of nicks across all channels.
func (a *Adapter) presentUsers() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := make(map[string]bool)
	var users []string
	for _, set := range a.members {
		for nick := range set {
			if !seen[nick] {
				seen[nick] = true
				users = append(users, nick)
			}
		}
	}
	sort.Strings(users)
	return users
}

func (a *Adapter) emitPresent() {
	a.emit(network.Event{Network: a.id, Kind: network.EventPresentUsers, Users: a.presentUsers()})
}

// channelName resolves the IRC target from a ChannelRef, preferring the native
// id and falling back to the display name.
func channelName(ref network.ChannelRef) string {
	if ref.ID != "" {
		return ref.ID
	}
	return ref.Name
}

// stripNickPrefix removes leading IRC membership prefixes (op/voice/etc.) from a
// NAMES entry, e.g. "@alice" -> "alice".
func stripNickPrefix(s string) string {
	return strings.TrimLeft(s, "@+%~&")
}
