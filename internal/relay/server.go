package relay

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Server exposes the relay's REST + WebSocket surface. It implements
// http.Handler, so it mounts directly on an http.Server. Every request except
// GET /healthz passes through an X-API-Key check; history reads are filtered by
// the retention window so a not-yet-pruned row past the window is never served.
type Server struct {
	store     *Store
	hub       *Hub
	backend   Backend
	apiKey    string
	retention time.Duration
	log       *log.Logger
	mux       *http.ServeMux
}

// NewServer builds the relay HTTP handler. A non-empty apiKey is required in the
// X-API-Key header on every request except GET /healthz. A retention > 0 hides
// messages older than the window on read; 0 serves everything.
func NewServer(store *Store, hub *Hub, backend Backend, apiKey string, retention time.Duration, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	s := &Server{
		store:     store,
		hub:       hub,
		backend:   backend,
		apiKey:    apiKey,
		retention: retention,
		log:       logger,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Realtime.
	s.mux.HandleFunc("GET /ws", s.hub.ServeWS)

	// Outbound.
	s.mux.HandleFunc("POST /message", s.handlePostMessage)
	s.mux.HandleFunc("POST /file", s.handlePostFile)
	s.mux.HandleFunc("PATCH /message/{id}", s.handlePatchMessage)

	// History + discovery.
	s.mux.HandleFunc("GET /api/channels", s.handleListChannels)
	s.mux.HandleFunc("GET /api/channels/{channel}/messages", s.handleHistory)
	s.mux.HandleFunc("GET /api/guilds", s.handleListGuilds)
	s.mux.HandleFunc("GET /api/guilds/{id}/channels", s.handleListChannels)

	// Retention / deletion (added by this reference relay).
	s.mux.HandleFunc("DELETE /api/users/{id}/messages", s.handleDeleteUser)
	s.mux.HandleFunc("POST /api/delete-my-data", s.handleDeleteMyData)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
		return
	}
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing API key")
		return
	}
	s.mux.ServeHTTP(w, r)
}

// authorized reports whether the request carries the configured API key. When
// no key is configured the relay is open (matching a client that sends no
// X-API-Key). The comparison is constant-time.
func (s *Server) authorized(r *http.Request) bool {
	if s.apiKey == "" {
		return true
	}
	got := r.Header.Get("X-API-Key")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.apiKey)) == 1
}

// --- request/response shapes (match the client packages exactly) ----------

type sendRequest struct {
	Channel   string `json:"channel"`
	GuildID   string `json:"guild_id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
	Content   string `json:"content"`
	ReplyToID string `json:"reply_to_id"`
}

type editRequest struct {
	Channel  string `json:"channel"`
	GuildID  string `json:"guild_id"`
	Username string `json:"username"`
	Content  string `json:"content"`
}

type deleteMyDataRequest struct {
	UserID string `json:"user_id"`
}

// --- handlers --------------------------------------------------------------

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Channel == "" {
		writeError(w, http.StatusBadRequest, "channel is required")
		return
	}
	stored, err := s.backend.Publish(Message{
		Channel:   req.Channel,
		GuildID:   req.GuildID,
		Username:  req.Username,
		Content:   req.Content,
		ReplyToID: req.ReplyToID,
	})
	if err != nil {
		s.publishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message_id": stored.ID})
}

func (s *Server) handlePostFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	channel := r.FormValue("channel")
	if channel == "" {
		writeError(w, http.StatusBadRequest, "channel is required")
		return
	}
	content := r.FormValue("content")
	var filename string
	if file, header, err := r.FormFile("file"); err == nil {
		filename = header.Filename
		// The reference relay does not host uploaded bytes — there is no public
		// blob URL to hand back — so it drains the upload and records only the
		// message and filename.
		_, _ = io.Copy(io.Discard, file)
		_ = file.Close()
	}
	if content == "" && filename != "" {
		content = "shared a file: " + filename
	}

	stored, err := s.backend.Publish(Message{
		Channel:  channel,
		GuildID:  r.FormValue("guild_id"),
		Username: r.FormValue("username"),
		Content:  content,
	})
	if err != nil {
		s.publishError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message_id": stored.ID})
}

func (s *Server) handlePatchMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "message id is required")
		return
	}
	var req editRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// Edits broadcast a live message_update. The store's published API is
	// append-only (no update method), so the edit is delivered to subscribers
	// but history keeps the original text — a clearly-scoped reference-relay
	// limitation, documented in docs/RELAY.md.
	s.hub.BroadcastMessage(Message{
		ID:        id,
		Channel:   req.Channel,
		GuildID:   req.GuildID,
		Username:  req.Username,
		Content:   req.Content,
		Timestamp: time.Now().UTC(),
	}, "message_update")
	writeJSON(w, http.StatusOK, map[string]string{"message_id": id})
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	chans, err := s.backend.ListChannels()
	if err != nil {
		s.publishError(w, err)
		return
	}
	if chans == nil {
		chans = []Channel{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": chans})
}

func (s *Server) handleListGuilds(w http.ResponseWriter, r *http.Request) {
	gs, err := s.backend.ListGuilds(r.URL.Query().Get("q"))
	if err != nil {
		s.publishError(w, err)
		return
	}
	if gs == nil {
		gs = []Guild{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"guilds": gs})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	q := r.URL.Query()
	limit := parseLimit(q.Get("limit"))

	var (
		msgs []Message
		err  error
	)
	if sinceStr := q.Get("since"); sinceStr != "" {
		var since time.Time
		if since, err = time.Parse(time.RFC3339Nano, sinceStr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid since timestamp")
			return
		}
		msgs, err = s.store.FetchSince(channel, since)
		if err == nil && len(msgs) > limit {
			msgs = msgs[:limit]
		}
	} else {
		var before time.Time
		if beforeStr := q.Get("before"); beforeStr != "" {
			if before, err = time.Parse(time.RFC3339Nano, beforeStr); err != nil {
				writeError(w, http.StatusBadRequest, "invalid before timestamp")
				return
			}
		}
		msgs, err = s.store.FetchBefore(channel, before, limit)
	}
	if err != nil {
		s.log.Printf("relay: history query failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch history")
		return
	}

	msgs = s.applyRetention(msgs)
	if msgs == nil {
		msgs = []Message{}
	}
	// The client decodes this array straight into []model.Message; relay.Message
	// carries matching JSON tags.
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "user id is required")
		return
	}
	s.deleteAndReport(w, id)
}

func (s *Server) handleDeleteMyData(w http.ResponseWriter, r *http.Request) {
	var req deleteMyDataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	s.deleteAndReport(w, req.UserID)
}

func (s *Server) deleteAndReport(w http.ResponseWriter, userID string) {
	n, err := s.store.DeleteByUser(userID)
	if err != nil {
		s.log.Printf("relay: delete for user %q failed: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to delete messages")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

// --- helpers ---------------------------------------------------------------

// applyRetention drops messages older than the retention window so a row that
// the pruner has not yet swept is never served.
func (s *Server) applyRetention(msgs []Message) []Message {
	if s.retention <= 0 {
		return msgs
	}
	cutoff := time.Now().Add(-s.retention)
	kept := msgs[:0]
	for _, m := range msgs {
		if !m.Timestamp.Before(cutoff) {
			kept = append(kept, m)
		}
	}
	return kept
}

// publishError maps a backend error onto an HTTP status, surfacing the explicit
// "discord backend not built" boundary as 501 Not Implemented.
func (s *Server) publishError(w http.ResponseWriter, err error) {
	if errors.Is(err, errDiscordNotBuilt) {
		writeError(w, http.StatusNotImplemented, err.Error())
		return
	}
	s.log.Printf("relay: backend error: %v", err)
	writeError(w, http.StatusInternalServerError, "backend error")
}

func parseLimit(s string) int {
	if s == "" {
		return 100
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 100
	}
	return clampLimit(n)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
