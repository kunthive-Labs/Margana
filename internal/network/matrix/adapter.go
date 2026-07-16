// Package matrix implements network.Network by connecting directly to a Matrix
// homeserver via the client-server API (mautrix-go). Unlike the Discord adapter
// it needs no relay. End-to-end encrypted rooms are supported: the Olm machine
// (mautrix cryptohelper, pure-Go Olm) decrypts incoming events during /sync and
// encrypts outgoing messages, with keys stored in a local crypto database and
// the OS keyring. If crypto cannot be initialized, the adapter degrades to
// surfacing encrypted rooms without decrypting them.
package matrix

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/logging"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
	"github.com/kunthive-Labs/Margana/internal/network/credstore"
)

// ID is the stable handle for the Matrix network.
const ID network.NetworkID = "matrix"

var _ network.Network = (*Adapter)(nil)

// Adapter is a direct Matrix client behind the network.Network interface.
type Adapter struct {
	homeserver string
	userID     string
	storePath  string

	client   *mautrix.Client
	identity network.Identity

	// crypto is the Olm machine for end-to-end encryption; nil when E2EE could
	// not be initialized (encrypted rooms then stay undecrypted).
	crypto *cryptohelper.CryptoHelper

	// password holds a credential gathered from the environment or an
	// interactive prompt, used once during login and then cleared.
	password string

	events chan network.Event
	cancel context.CancelFunc

	histMu     sync.Mutex
	histTokens map[string]string // roomID -> next backward pagination token

	// logger captures adapter lifecycle events and (when enabled) mautrix's
	// own client/crypto logs. Defaults to a disabled no-op logger.
	logger *logging.Logger
}

// New builds a Matrix adapter from its [[networks]] entry. Credentials are read
// from the keyring (service "marga-matrix") at Connect time.
func New(n config.NetworkConfig, storePath string) *Adapter {
	return &Adapter{
		homeserver: n.Homeserver,
		userID:     n.UserID,
		storePath:  storePath,
		identity: network.Identity{
			Network:  ID,
			UserID:   n.UserID,
			Username: localpart(n.UserID),
		},
		events:     make(chan network.Event, 256),
		histTokens: make(map[string]string),
		logger:     logging.Disabled(),
	}
}

// SetLogger enables Matrix diagnostics. Adapter lifecycle events are written via
// slog, and mautrix's own client/crypto logs are routed to the same destination
// (as zerolog JSON lines). Off by default; call before Connect to capture
// connection setup.
func (a *Adapter) SetLogger(l *logging.Logger) {
	if l != nil {
		a.logger = l
	}
}

func (a *Adapter) ID() network.NetworkID { return ID }

func (a *Adapter) Capabilities() network.Capabilities {
	return network.Capabilities{
		Edit:       true,
		FileUpload: true,
		Typing:     true,
		Presence:   true,
		History:    true,
		ServerList: true, // homeserver + joined spaces are surfaced as servers
		Reactions:  false,
		Encryption: true, // E2EE rooms are decrypted/encrypted when crypto inits
	}
}

// Connect authenticates (using a stored token, or a password from
// MARGA_MATRIX_PASSWORD on first run) and starts the /sync loop.
func (a *Adapter) Connect(ctx context.Context) error {
	token, _ := credstore.Get("matrix", "access_token")
	deviceID, _ := credstore.Get("matrix", "device_id")

	// First run (no stored token): gather any missing connection details and a
	// password, interactively if stdin is a terminal, else from the environment.
	if token == "" {
		if err := a.resolveCredentials(); err != nil {
			return err
		}
	}

	if a.homeserver == "" || a.userID == "" {
		return fmt.Errorf("matrix: homeserver and user_id must be configured")
	}

	client, err := mautrix.NewClient(a.homeserver, id.UserID(a.userID), token)
	if err != nil {
		return fmt.Errorf("matrix: new client: %w", err)
	}
	client.DeviceID = id.DeviceID(deviceID)
	client.Store = newFileSyncStore(a.storePath)

	// Route mautrix's internal client/crypto logs to the same file (as zerolog
	// JSON lines) when logging is enabled; otherwise leave its default Nop
	// logger in place so nothing reaches the terminal.
	if a.logger.Enabled() {
		client.Log = zerolog.New(a.logger.Writer()).
			Level(zerologLevel(a.logger.Level())).
			With().Timestamp().Str("component", "matrix-mautrix").Logger()
	}

	if token == "" {
		if err := a.login(ctx, client); err != nil {
			return err
		}
	}

	a.client = client
	a.registerHandlers()

	// Enable end-to-end encryption. On failure, degrade gracefully: connect
	// anyway, leaving encrypted rooms undecrypted rather than failing startup.
	if helper, err := setupCrypto(ctx, client, cryptoDBPath(a.storePath)); err != nil {
		fmt.Fprintf(os.Stderr, "matrix: end-to-end encryption unavailable: %v\n", err)
		a.logger.Named("matrix").Warn("end-to-end encryption unavailable", "err", err)
	} else {
		a.crypto = helper
	}

	a.logger.Named("matrix").Info("connected",
		"homeserver", a.homeserver, "user_id", a.userID, "encryption", a.crypto != nil)

	syncCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go a.syncLoop(syncCtx)
	return nil
}

// zerologLevel maps a slog level onto the nearest zerolog level so mautrix logs
// honor the configured verbosity.
func zerologLevel(l slog.Level) zerolog.Level {
	switch {
	case l <= slog.LevelDebug:
		return zerolog.DebugLevel
	case l <= slog.LevelInfo:
		return zerolog.InfoLevel
	case l <= slog.LevelWarn:
		return zerolog.WarnLevel
	default:
		return zerolog.ErrorLevel
	}
}

// resolveCredentials populates a.password (and any missing homeserver/user id)
// before login. MARGA_MATRIX_PASSWORD wins when set — keeping headless and CI
// runs non-interactive. Otherwise, if stdin is a terminal, it prompts; the
// homeserver and user id are only asked for when not already configured.
func (a *Adapter) resolveCredentials() error {
	if pw := os.Getenv("MARGA_MATRIX_PASSWORD"); pw != "" {
		a.password = pw
		return nil
	}
	if !stdinIsTerminal() {
		return fmt.Errorf("matrix: no stored token — set MARGA_MATRIX_PASSWORD or run in an interactive terminal to log in")
	}

	fmt.Fprintln(os.Stderr, "Matrix login — credentials are exchanged for a token stored in your OS keyring.")
	d, err := gatherLogin(os.Stderr, bufio.NewReader(os.Stdin), readSecret, a.homeserver, a.userID)
	if err != nil {
		return fmt.Errorf("matrix: %w", err)
	}
	a.homeserver = d.homeserver
	a.userID = d.userID
	a.identity.UserID = d.userID
	a.identity.Username = localpart(d.userID)
	a.password = d.password
	return nil
}

// login performs an m.login.password flow with the resolved password and
// persists the returned token/device to the keyring, then clears the password.
func (a *Adapter) login(ctx context.Context, client *mautrix.Client) error {
	if a.password == "" {
		return fmt.Errorf("matrix: no password available for login")
	}
	resp, err := client.Login(ctx, &mautrix.ReqLogin{
		Type:                     mautrix.AuthTypePassword,
		Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: localpart(a.userID)},
		Password:                 a.password,
		InitialDeviceDisplayName: "Marga",
		StoreCredentials:         true,
	})
	a.password = "" // do not keep the plaintext password in memory after use
	if err != nil {
		return fmt.Errorf("matrix: login: %w", err)
	}
	_ = credstore.Set("matrix", "access_token", resp.AccessToken)
	_ = credstore.Set("matrix", "device_id", resp.DeviceID.String())
	a.logger.Named("matrix").Info("logged in via password", "user_id", a.userID, "device_id", resp.DeviceID.String())
	return nil
}

func (a *Adapter) syncLoop(ctx context.Context) {
	defer close(a.events)
	a.emit(ctx, network.Event{Network: ID, Kind: network.EventStatus, State: network.StateConnected})
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		err := a.client.SyncWithContext(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		// Transient sync failure: report reconnecting and retry after a pause.
		a.logger.Named("matrix").Warn("sync failed, retrying", "err", err)
		a.emit(ctx, network.Event{Network: ID, Kind: network.EventStatus, State: network.StateReconnecting, Err: err, RetryAt: time.Now().Add(5 * time.Second)})
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (a *Adapter) registerHandlers() {
	syncer, ok := a.client.Syncer.(*mautrix.DefaultSyncer)
	if !ok {
		return
	}
	syncer.OnEventType(event.EventMessage, a.onRoomMessage)
	syncer.OnEventType(event.EphemeralEventTyping, a.onTyping)
}

func (a *Adapter) onRoomMessage(ctx context.Context, evt *event.Event) {
	// The TUI dedups by message id and sender, so always forward (including
	// our own echo).
	if msg := a.eventToMessage(evt); msg != nil {
		a.emit(ctx, network.Event{Network: ID, Kind: network.EventMessage, Message: msg})
	}
}

// eventToMessage maps an m.room.message event to the neutral model.Message.
// Shared by the live /sync handler and the /messages history backfill. Returns
// nil for events that aren't text/media messages.
func (a *Adapter) eventToMessage(evt *event.Event) *model.Message {
	content := evt.Content.AsMessage()
	if content == nil {
		return nil
	}

	msg := &model.Message{
		Network:   string(ID),
		ID:        evt.ID.String(),
		UserID:    evt.Sender.String(),
		Username:  displayName(evt.Sender),
		Content:   content.Body,
		Channel:   evt.RoomID.String(),
		Timestamp: time.UnixMilli(evt.Timestamp),
		Editable:  evt.Sender == a.client.UserID,
	}

	if rel := content.RelatesTo; rel != nil {
		if replaceID := rel.GetReplaceID(); replaceID != "" {
			// An edit: target the replaced event and use the new body.
			msg.EventType = "message_update"
			msg.ID = replaceID.String()
			if content.NewContent != nil {
				msg.Content = content.NewContent.Body
			}
		} else if replyID := rel.GetReplyTo(); replyID != "" {
			msg.ReplyToID = replyID.String()
		}
	}
	if msg.EventType == "" {
		msg.EventType = "message_create"
	}

	if att := mediaAttachment(content); att != nil {
		msg.Attachments = []model.Attachment{*att}
	}

	return msg
}

func (a *Adapter) onTyping(ctx context.Context, evt *event.Event) {
	content, ok := evt.Content.Parsed.(*event.TypingEventContent)
	if !ok {
		return
	}
	for _, uid := range content.UserIDs {
		if uid == a.client.UserID {
			continue
		}
		te := model.TypingEvent{Type: "typing", Username: displayName(uid), Channel: evt.RoomID.String()}
		a.emit(ctx, network.Event{Network: ID, Kind: network.EventTyping, Typing: &te})
	}
}

func (a *Adapter) Disconnect() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.client != nil {
		a.client.StopSync()
	}
	if a.crypto != nil {
		_ = a.crypto.Close()
	}
	return nil
}

func (a *Adapter) CurrentUser() network.Identity { return a.identity }

// ListServers, ListChannels, and the space-discovery helpers live in spaces.go.

func (a *Adapter) Subscribe(network.ChannelRef) error   { return nil } // sync delivers all joined rooms
func (a *Adapter) Unsubscribe(network.ChannelRef) error { return nil }

// FetchHistory backfills a room's history via the /messages endpoint, paging
// backward. The interface pages by `before` timestamp, but Matrix pages by
// opaque token, so we keep a per-room cursor: before==nil starts from the most
// recent message; a non-nil before continues from where the last call ended.
func (a *Adapter) FetchHistory(ctx context.Context, ref network.ChannelRef, limit int, before *time.Time) ([]model.Message, error) {
	if a.client == nil {
		return nil, fmt.Errorf("matrix: not connected")
	}
	if limit <= 0 {
		limit = 50
	}

	a.histMu.Lock()
	from := ""
	if before != nil {
		from = a.histTokens[ref.ID] // continue paging older
	}
	a.histMu.Unlock()

	resp, err := a.client.Messages(ctx, id.RoomID(ref.ID), from, "", mautrix.DirectionBackward, nil, limit)
	if err != nil {
		return nil, err
	}

	a.histMu.Lock()
	a.histTokens[ref.ID] = resp.End // next call resumes from here
	a.histMu.Unlock()

	// Backward pagination yields newest-first, matching the interface contract.
	msgs := make([]model.Message, 0, len(resp.Chunk))
	for _, evt := range resp.Chunk {
		// Decrypt encrypted history when we have the keys; skip if we can't
		// (no session yet) rather than showing ciphertext.
		if evt.Type == event.EventEncrypted && a.crypto != nil {
			dec, err := a.crypto.Decrypt(ctx, evt)
			if err != nil {
				continue
			}
			evt = dec
		}
		if evt.Type != event.EventMessage {
			continue
		}
		if evt.Content.Parsed == nil {
			if err := evt.Content.ParseRaw(evt.Type); err != nil {
				continue
			}
		}
		// Skip edits during backfill; the original is shown and live /sync
		// applies edits as they arrive.
		if m := a.eventToMessage(evt); m != nil && m.EventType == "message_create" {
			msgs = append(msgs, *m)
		}
	}
	return msgs, nil
}

func (a *Adapter) Send(ctx context.Context, ref network.ChannelRef, content, replyToID string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("matrix: not connected")
	}
	msgContent := event.MessageEventContent{MsgType: event.MsgText, Body: content}
	if replyToID != "" {
		msgContent.RelatesTo = &event.RelatesTo{InReplyTo: &event.InReplyTo{EventID: id.EventID(replyToID)}}
	}
	resp, err := a.client.SendMessageEvent(ctx, id.RoomID(ref.ID), event.EventMessage, &msgContent)
	if err != nil {
		return "", err
	}
	return resp.EventID.String(), nil
}

func (a *Adapter) SendFile(ctx context.Context, ref network.ChannelRef, path, content string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("matrix: not connected")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	name := filepathBase(path)
	upload, err := a.client.UploadMedia(ctx, mautrix.ReqUploadMedia{
		ContentBytes: data,
		FileName:     name,
		ContentType:  detectContentType(name),
	})
	if err != nil {
		return "", err
	}
	body := name
	if content != "" {
		body = content
	}
	msgContent := event.MessageEventContent{
		MsgType:  msgTypeForFile(name),
		Body:     body,
		FileName: name,
		URL:      upload.ContentURI.CUString(),
	}
	resp, err := a.client.SendMessageEvent(ctx, id.RoomID(ref.ID), event.EventMessage, &msgContent)
	if err != nil {
		return "", err
	}
	return resp.EventID.String(), nil
}

func (a *Adapter) Edit(ctx context.Context, ref network.ChannelRef, messageID, content string) error {
	if a.client == nil {
		return fmt.Errorf("matrix: not connected")
	}
	newContent := event.MessageEventContent{MsgType: event.MsgText, Body: content}
	msgContent := event.MessageEventContent{
		MsgType:    event.MsgText,
		Body:       "* " + content,
		NewContent: &newContent,
		RelatesTo:  &event.RelatesTo{Type: event.RelReplace, EventID: id.EventID(messageID)},
	}
	_, err := a.client.SendMessageEvent(ctx, id.RoomID(ref.ID), event.EventMessage, &msgContent)
	return err
}

// SetStatus broadcasts presence to the homeserver.
func (a *Adapter) SetStatus(status string) error {
	if a.client == nil {
		return fmt.Errorf("matrix: not connected")
	}
	presence := event.PresenceOnline
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "away", "idle", "unavailable":
		presence = event.PresenceUnavailable
	case "offline", "invisible":
		presence = event.PresenceOffline
	}
	return a.client.SetPresence(context.Background(), mautrix.ReqPresence{Presence: presence})
}

func (a *Adapter) Events() <-chan network.Event { return a.events }

func (a *Adapter) emit(ctx context.Context, ev network.Event) {
	select {
	case a.events <- ev:
	case <-ctx.Done():
	}
}

// --- helpers ---

func localpart(mxid string) string {
	s := strings.TrimPrefix(mxid, "@")
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i]
	}
	return s
}

func displayName(uid id.UserID) string {
	return localpart(uid.String())
}

func mediaAttachment(content *event.MessageEventContent) *model.Attachment {
	switch content.MsgType {
	case event.MsgImage, event.MsgFile, event.MsgVideo, event.MsgAudio:
		name := content.FileName
		if name == "" {
			name = content.Body
		}
		att := &model.Attachment{
			URL:      string(content.URL),
			Filename: name,
		}
		if content.Info != nil {
			att.ContentType = content.Info.MimeType
			att.Width = content.Info.Width
			att.Height = content.Info.Height
			att.Size = content.Info.Size
		}
		return att
	default:
		return nil
	}
}
