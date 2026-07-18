// Package relay is a self-hostable reference implementation of Marga's relay
// wire contract: a WebSocket event hub (hub.go) plus a REST API (server.go)
// backed by a pure-Go SQLite store (this file) and a pluggable backend
// (backend.go). It exists so anyone can `docker compose up` a working,
// ToS-safe relay — with message history, a retention window, and a
// delete-my-data path — without the production Discord bot, which lives in the
// separate kunthive-Labs/marga-discord-relay repository.
package relay

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Message is the reference relay's internal representation of a chat message.
// Its JSON tags mirror internal/model.Message so that a slice of Messages
// marshals straight into the shape the history client (internal/history)
// decodes. The hub separately projects a Message onto the WebSocket event frame
// that internal/wsclient decodes — which names the id field "message_id" — so
// the two wire shapes never leak into each other.
type Message struct {
	ID        string    `json:"id"`
	Channel   string    `json:"channel"`
	GuildID   string    `json:"guild_id,omitempty"`
	UserID    string    `json:"user_id,omitempty"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	ReplyToID string    `json:"reply_to_id,omitempty"`
}

// Store is a single-writer SQLite message store. It uses the pure-Go, CGO-free
// modernc.org/sqlite driver with WAL journaling and a single open connection,
// mirroring internal/network/matrix/crypto.go, so the relay builds and runs
// with CGO_ENABLED=0.
type Store struct {
	db *sql.DB
}

// schemaStmts create the message table and its lookup indexes. Timestamps are
// stored as INTEGER Unix-nanoseconds so before/since range queries and ordering
// are exact and cheap; the column is projected back to a time.Time on read.
var schemaStmts = []string{
	`CREATE TABLE IF NOT EXISTS messages (
		message_id TEXT PRIMARY KEY,
		channel    TEXT,
		guild_id   TEXT,
		user_id    TEXT,
		username   TEXT,
		content    TEXT,
		timestamp  INTEGER
	)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_channel_ts ON messages(channel, timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_user ON messages(user_id)`,
}

// NewStore opens (creating if needed) the SQLite database at path and applies
// the schema. An empty path opens a private in-memory database, which is handy
// for tests.
func NewStore(path string) (*Store, error) {
	var dsn string
	if path == "" {
		dsn = "file::memory:?cache=shared&_pragma=busy_timeout(5000)"
	} else {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("creating db directory: %w", err)
			}
		}
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}
	// A single writer plus WAL keeps the pure-Go driver robust for a
	// long-running server without pulling in a CGo SQLite build.
	db.SetMaxOpenConns(1)

	for _, stmt := range schemaStmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("applying schema: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Insert stores (or replaces, keyed by ID) a message. A zero timestamp is set
// to now.
func (s *Store) Insert(m Message) error {
	ts := m.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO messages
		 (message_id, channel, guild_id, user_id, username, content, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Channel, m.GuildID, m.UserID, m.Username, m.Content, ts.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("inserting message: %w", err)
	}
	return nil
}

// FetchBefore returns up to limit messages in channel that are strictly older
// than before, returned oldest-first. A zero before means "the most recent
// messages". A non-positive limit defaults to 100 (capped at 500).
func (s *Store) FetchBefore(channel string, before time.Time, limit int) ([]Message, error) {
	limit = clampLimit(limit)
	var (
		rows *sql.Rows
		err  error
	)
	const cols = `SELECT message_id, channel, guild_id, user_id, username, content, timestamp FROM messages`
	if before.IsZero() {
		rows, err = s.db.Query(
			cols+` WHERE channel = ? ORDER BY timestamp DESC, message_id DESC LIMIT ?`,
			channel, limit)
	} else {
		rows, err = s.db.Query(
			cols+` WHERE channel = ? AND timestamp < ? ORDER BY timestamp DESC, message_id DESC LIMIT ?`,
			channel, before.UTC().UnixNano(), limit)
	}
	if err != nil {
		return nil, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	// Rows come newest-first (so LIMIT keeps the newest window); flip to
	// oldest-first for chronological display.
	reverse(msgs)
	return msgs, nil
}

// FetchSince returns messages in channel that are strictly newer than since,
// oldest-first.
func (s *Store) FetchSince(channel string, since time.Time) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT message_id, channel, guild_id, user_id, username, content, timestamp
		 FROM messages WHERE channel = ? AND timestamp > ?
		 ORDER BY timestamp ASC, message_id ASC`,
		channel, since.UTC().UnixNano())
	if err != nil {
		return nil, fmt.Errorf("querying since: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// PruneOlderThan deletes messages whose timestamp is older than d ago and
// returns the number removed. A non-positive d is a no-op — 0 means "keep
// forever", preserving the historical behavior when retention is unset.
func (s *Store) PruneOlderThan(d time.Duration) (int64, error) {
	if d <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-d).UTC().UnixNano()
	res, err := s.db.Exec(`DELETE FROM messages WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("pruning old messages: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteByUser removes every message authored by userID and returns the count.
// It backs both DELETE /api/users/{id}/messages and the friendly
// POST /api/delete-my-data endpoint.
func (s *Store) DeleteByUser(userID string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM messages WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("deleting user messages: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func scanMessages(rows *sql.Rows) ([]Message, error) {
	var msgs []Message
	for rows.Next() {
		var (
			m  Message
			ns int64
		)
		if err := rows.Scan(&m.ID, &m.Channel, &m.GuildID, &m.UserID, &m.Username, &m.Content, &ns); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		m.Timestamp = time.Unix(0, ns).UTC()
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading rows: %w", err)
	}
	return msgs, nil
}

func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return 100
	case limit > 500:
		return 500
	default:
		return limit
	}
}

func reverse(m []Message) {
	for i, j := 0, len(m)-1; i < j; i, j = i+1, j-1 {
		m[i], m[j] = m[j], m[i]
	}
}
