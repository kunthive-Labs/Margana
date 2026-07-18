package relay

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10
	wsSendBuffer = 64
	wsReadLimit  = 1 << 20
)

// wsEvent is a server->client frame. Its JSON tags are exactly the set that
// internal/wsclient.relayEvent decodes; every field is omitempty so each frame
// type carries only what the client reads for it. The projections in the
// Broadcast* helpers below are the single source of truth for each frame's
// field set.
type wsEvent struct {
	Type           string   `json:"type"`
	Channel        string   `json:"channel,omitempty"`
	Username       string   `json:"username,omitempty"`
	UserID         string   `json:"user_id,omitempty"`
	Content        string   `json:"content,omitempty"`
	MessageID      string   `json:"message_id,omitempty"`
	Timestamp      string   `json:"timestamp,omitempty"`
	Status         string   `json:"status,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
	ReplyToID      string   `json:"reply_to_id,omitempty"`
	ReplyToContent string   `json:"reply_to_content,omitempty"`
	ReplyToAuthor  string   `json:"reply_to_author,omitempty"`
	Users          []string `json:"users,omitempty"`
	Editable       bool     `json:"editable,omitempty"`
}

// clientAction is a client->server frame. internal/wsclient writes exactly
// these actions: identify, subscribe, unsubscribe, status_update.
type clientAction struct {
	Action   string `json:"action"`
	Username string `json:"username"`
	Channel  string `json:"channel"`
	Status   string `json:"status"`
}

// conn is one connected WebSocket client.
type conn struct {
	hub  *Hub
	ws   *websocket.Conn
	send chan []byte

	mu       sync.Mutex
	username string
	subs     map[string]struct{}
}

// Hub upgrades WebSocket connections, tracks per-channel subscriptions, and
// fans event frames out to subscribers. It is safe for concurrent use: the
// registry is guarded by mu, and each connection has its own buffered write
// pump so one slow client never stalls a broadcast.
type Hub struct {
	upgrader websocket.Upgrader
	log      *log.Logger

	mu       sync.RWMutex
	conns    map[*conn]struct{}
	channels map[string]map[*conn]struct{}
}

// NewHub returns a ready hub. A nil logger discards diagnostics.
func NewHub(logger *log.Logger) *Hub {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Hub{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		log:      logger,
		conns:    make(map[*conn]struct{}),
		channels: make(map[string]map[*conn]struct{}),
	}
}

// ServeWS upgrades an HTTP request to a WebSocket connection and runs its read
// and write pumps. Mount it (behind the server's auth) at GET /ws.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Printf("ws: upgrade failed: %v", err)
		return
	}
	c := &conn{
		hub:  h,
		ws:   ws,
		send: make(chan []byte, wsSendBuffer),
		subs: make(map[string]struct{}),
	}
	h.register(c)
	go c.writePump()
	go c.readPump()
}

func (h *Hub) register(c *conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

// unregister removes c from every registry and closes its send channel. It is
// only called from c's own readPump, so it never races other readers of c's
// state. Broadcasts hold mu for the full fan-out, so once unregister takes the
// write lock no broadcast can still reference c — making close(c.send) safe.
func (h *Hub) unregister(c *conn) {
	h.mu.Lock()
	delete(h.conns, c)
	c.mu.Lock()
	for ch := range c.subs {
		if set := h.channels[ch]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.channels, ch)
			}
		}
	}
	c.subs = make(map[string]struct{})
	c.mu.Unlock()
	h.mu.Unlock()

	close(c.send)
	h.broadcastTerminalOnline()
}

func (h *Hub) subscribe(c *conn, channel string) {
	if channel == "" {
		return
	}
	h.mu.Lock()
	set := h.channels[channel]
	if set == nil {
		set = make(map[*conn]struct{})
		h.channels[channel] = set
	}
	set[c] = struct{}{}
	h.mu.Unlock()

	c.mu.Lock()
	c.subs[channel] = struct{}{}
	c.mu.Unlock()
}

func (h *Hub) unsubscribe(c *conn, channel string) {
	h.mu.Lock()
	if set := h.channels[channel]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.channels, channel)
		}
	}
	h.mu.Unlock()

	c.mu.Lock()
	delete(c.subs, channel)
	c.mu.Unlock()
}

// BroadcastMessage fans a message_create or message_update frame out to the
// message's channel subscribers.
func (h *Hub) BroadcastMessage(m Message, eventType string) {
	h.broadcastChannel(m.Channel, wsEvent{
		Type:      eventType,
		Channel:   m.Channel,
		Username:  m.Username,
		UserID:    m.UserID,
		Content:   m.Content,
		MessageID: m.ID,
		Timestamp: m.Timestamp.UTC().Format(time.RFC3339Nano),
		ReplyToID: m.ReplyToID,
		Editable:  true,
	})
}

// BroadcastTyping fans a typing frame out to a channel's subscribers. The local
// echo backend has no typing source, so it never calls this; it is part of the
// hub's contract for real backends (e.g. the external Discord bridge) and is
// exercised directly by the contract test.
func (h *Hub) BroadcastTyping(channel, username string) {
	h.broadcastChannel(channel, wsEvent{
		Type:     "typing",
		Channel:  channel,
		Username: username,
	})
}

// BroadcastStatus fans a status_update presence frame out to every client.
func (h *Hub) BroadcastStatus(username, status string) {
	h.broadcastAll(wsEvent{
		Type:      "status_update",
		Username:  username,
		Status:    status,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// broadcastTerminalOnline pushes the sorted set of identified usernames to every
// client. It runs whenever a client identifies or disconnects.
func (h *Hub) broadcastTerminalOnline() {
	h.mu.RLock()
	seen := make(map[string]struct{}, len(h.conns))
	users := make([]string, 0, len(h.conns))
	for c := range h.conns {
		c.mu.Lock()
		name := c.username
		c.mu.Unlock()
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		users = append(users, name)
	}
	h.mu.RUnlock()

	sort.Strings(users)
	h.broadcastAll(wsEvent{Type: "terminal_online", Users: users})
}

func (h *Hub) broadcastChannel(channel string, frame wsEvent) {
	data, err := json.Marshal(frame)
	if err != nil {
		h.log.Printf("ws: marshal frame: %v", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.channels[channel] {
		enqueue(h, c, data)
	}
}

func (h *Hub) broadcastAll(frame wsEvent) {
	data, err := json.Marshal(frame)
	if err != nil {
		h.log.Printf("ws: marshal frame: %v", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.conns {
		enqueue(h, c, data)
	}
}

// enqueue queues data on c's write pump, dropping it if the buffer is full so a
// slow or stuck client cannot stall the hub. Callers hold h.mu (read), which is
// mutually exclusive with unregister's close(c.send).
func enqueue(h *Hub, c *conn, data []byte) {
	select {
	case c.send <- data:
	default:
		h.log.Printf("ws: send buffer full, dropping frame for a client")
	}
}

func (c *conn) readPump() {
	defer func() {
		c.hub.unregister(c)
		_ = c.ws.Close()
	}()

	c.ws.SetReadLimit(wsReadLimit)
	_ = c.ws.SetReadDeadline(time.Now().Add(wsPongWait))
	c.ws.SetPongHandler(func(string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var a clientAction
		if err := json.Unmarshal(raw, &a); err != nil {
			c.hub.log.Printf("ws: bad client action: %v", err)
			continue
		}
		c.handleAction(a)
	}
}

func (c *conn) handleAction(a clientAction) {
	switch a.Action {
	case "identify":
		c.mu.Lock()
		c.username = a.Username
		c.mu.Unlock()
		c.hub.broadcastTerminalOnline()
	case "subscribe":
		c.hub.subscribe(c, a.Channel)
	case "unsubscribe":
		c.hub.unsubscribe(c, a.Channel)
	case "status_update":
		c.hub.BroadcastStatus(a.Username, a.Status)
	default:
		// Unknown actions are ignored for forward compatibility.
	}
}

func (c *conn) writePump() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				_ = c.ws.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
