// Package db is Marga's local SQLite store for messages, channels, presence,
// and notifications, with FTS5 full-text search over message history. Each
// Discord server (and other networks) gets its own database file.
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kunthive-Labs/Margana/internal/model"
)

type Store struct {
	db *sql.DB
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS messages (
		id        TEXT PRIMARY KEY,
		username  TEXT NOT NULL,
		content   TEXT NOT NULL,
		channel   TEXT NOT NULL,
		timestamp DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS channels (
		name      TEXT PRIMARY KEY,
		joined_at DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS user_presence (
		username   TEXT PRIMARY KEY,
		status     TEXT NOT NULL DEFAULT '',
		online     INTEGER NOT NULL DEFAULT 0,
		last_seen  DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS notifications (
		msg_id    TEXT PRIMARY KEY,
		channel   TEXT NOT NULL,
		username  TEXT NOT NULL,
		content   TEXT NOT NULL,
		timestamp DATETIME NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel, timestamp)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_content ON messages(content)`,
	`ALTER TABLE messages ADD COLUMN attachments_json TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE messages ADD COLUMN editable INTEGER NOT NULL DEFAULT 0`,
}

func New(dbPath string) (*Store, error) {
	if dbPath == "" {
		var err error
		dbPath, err = DefaultDBPath()
		if err != nil {
			return nil, fmt.Errorf("resolving default DB path: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			// Ignore "duplicate column" errors from ALTER TABLE migration
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			db.Close()
			return nil, fmt.Errorf("executing schema migration: %w", err)
		}
	}

	if err := migrateFTS(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// migrateFTS sets up the FTS5 full-text index over message content. It uses an
// external-content table (the index stores no copy of the text, only the
// inverted index) kept in sync with `messages` via triggers. On a database
// that predates the index, the existing rows are backfilled once via 'rebuild'.
func migrateFTS(db *sql.DB) error {
	stmts := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			content,
			content='messages',
			content_rowid='rowid'
		)`,
		`CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
			INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
		END`,
		// The FTS index supersedes the plain content index used by the old
		// LIKE search; drop it to save write overhead.
		`DROP INDEX IF EXISTS idx_messages_content`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("creating FTS index: %w", err)
		}
	}

	var msgCount, ftsCount int
	if err := db.QueryRow(`SELECT count(*) FROM messages`).Scan(&msgCount); err != nil {
		return fmt.Errorf("counting messages: %w", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM messages_fts`).Scan(&ftsCount); err != nil {
		return fmt.Errorf("counting FTS rows: %w", err)
	}
	if msgCount != ftsCount {
		if _, err := db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES ('rebuild')`); err != nil {
			return fmt.Errorf("backfilling FTS index: %w", err)
		}
	}
	return nil
}

func (s *Store) InsertMessage(msg model.Message) error {
	attJSON := marshalAttachments(msg.Attachments)
	_, err := s.db.Exec(
		`INSERT INTO messages (id, username, content, channel, timestamp, attachments_json, editable) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   username=excluded.username,
		   content=excluded.content,
		   channel=excluded.channel,
		   timestamp=excluded.timestamp,
		   attachments_json=excluded.attachments_json,
		   editable=excluded.editable`,
		msg.ID, msg.Username, msg.Content, msg.Channel, msg.Timestamp.UTC(), attJSON, boolToInt(msg.Editable),
	)
	if err != nil {
		return fmt.Errorf("inserting message: %w", err)
	}
	return nil
}

func (s *Store) UpdateMessage(msg model.Message) error {
	_, err := s.db.Exec(
		`UPDATE messages SET content = ?, username = ?, channel = ?, attachments_json = ?, editable = ? WHERE id = ?`,
		msg.Content, msg.Username, msg.Channel, marshalAttachments(msg.Attachments), boolToInt(msg.Editable), msg.ID,
	)
	if err != nil {
		return fmt.Errorf("updating message: %w", err)
	}
	return nil
}

func (s *Store) GetMessages(channel string, limit int, before *time.Time) ([]model.Message, error) {
	var args []interface{}
	var query string

	if before != nil {
		query = `SELECT id, username, content, channel, timestamp, attachments_json, editable FROM messages WHERE channel = ? AND timestamp < ? ORDER BY timestamp DESC LIMIT ?`
		args = []interface{}{channel, before.UTC(), limit}
	} else {
		query = `SELECT id, username, content, channel, timestamp, attachments_json, editable FROM messages WHERE channel = ? ORDER BY timestamp DESC LIMIT ?`
		args = []interface{}{channel, limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	return scanMessages(rows)
}

// SearchMessages runs a full-text search over message content using the FTS5
// index, ranked newest-first and capped at 100 hits. The query is tokenized
// and each term is prefix-matched (so "depl" finds "deploy"). If the FTS query
// is rejected (e.g. it tokenizes to nothing) it falls back to a LIKE scan so
// search never hard-fails on odd input.
func (s *Store) SearchMessages(query string) ([]model.Message, error) {
	if match := buildFTSMatch(query); match != "" {
		rows, err := s.db.Query(
			`SELECT m.id, m.username, m.content, m.channel, m.timestamp, m.attachments_json, m.editable
			 FROM messages_fts f JOIN messages m ON m.rowid = f.rowid
			 WHERE f MATCH ? ORDER BY m.timestamp DESC LIMIT 100`,
			match,
		)
		if err == nil {
			if msgs, scanErr := scanMessages(rows); scanErr == nil {
				return msgs, nil
			}
		}
		// Fall through to LIKE on any FTS error.
	}

	rows, err := s.db.Query(
		`SELECT id, username, content, channel, timestamp, attachments_json, editable FROM messages WHERE content LIKE ? ORDER BY timestamp DESC LIMIT 100`,
		"%"+query+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("searching messages: %w", err)
	}
	return scanMessages(rows)
}

// buildFTSMatch turns a free-text query into an FTS5 MATCH expression: each
// whitespace-separated term becomes a quoted, prefix-matched token ANDed
// together. Embedded double quotes are escaped by doubling. Returns "" when
// the query has no terms.
func buildFTSMatch(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		escaped := strings.ReplaceAll(f, `"`, `""`)
		terms = append(terms, `"`+escaped+`"*`)
	}
	return strings.Join(terms, " ")
}

// scanMessages reads message rows selecting the standard column set and closes
// the rows when done.
func scanMessages(rows *sql.Rows) ([]model.Message, error) {
	defer rows.Close()

	var msgs []model.Message
	for rows.Next() {
		var m model.Message
		var attJSON string
		var editable int
		if err := rows.Scan(&m.ID, &m.Username, &m.Content, &m.Channel, &m.Timestamp, &attJSON, &editable); err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		m.Attachments = unmarshalAttachments(attJSON)
		m.Editable = editable == 1
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating message rows: %w", err)
	}
	return msgs, nil
}

func (s *Store) InsertNotification(n model.Notification) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO notifications (msg_id, channel, username, content, timestamp) VALUES (?, ?, ?, ?, ?)`,
		n.MsgID, n.Channel, n.Username, n.Content, n.Timestamp.UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting notification: %w", err)
	}
	return nil
}

func (s *Store) GetNotifications() ([]model.Notification, error) {
	rows, err := s.db.Query(
		`SELECT msg_id, channel, username, content, timestamp FROM notifications ORDER BY timestamp ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying notifications: %w", err)
	}
	defer rows.Close()

	var result []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(&n.MsgID, &n.Channel, &n.Username, &n.Content, &n.Timestamp); err != nil {
			return nil, fmt.Errorf("scanning notification row: %w", err)
		}
		result = append(result, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating notification rows: %w", err)
	}

	return result, nil
}

func (s *Store) ClearNotifications() error {
	_, err := s.db.Exec(`DELETE FROM notifications`)
	return err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) InsertChannel(name string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO channels (name, joined_at) VALUES (?, ?)`,
		name, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting channel: %w", err)
	}
	return nil
}

func (s *Store) GetChannels() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM channels ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying channels: %w", err)
	}
	defer rows.Close()

	var channels []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning channel row: %w", err)
		}
		channels = append(channels, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating channel rows: %w", err)
	}

	return channels, nil
}

func (s *Store) DeleteChannel(name string) error {
	_, err := s.db.Exec(`DELETE FROM channels WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("deleting channel: %w", err)
	}
	return nil
}

func (s *Store) ReplaceChannels(names []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM channels`); err != nil {
		return fmt.Errorf("clearing channels: %w", err)
	}

	now := time.Now().UTC()
	for _, name := range names {
		if _, err := tx.Exec(`INSERT INTO channels (name, joined_at) VALUES (?, ?)`, name, now); err != nil {
			return fmt.Errorf("inserting channel %q: %w", name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing channel sync: %w", err)
	}
	return nil
}

func (s *Store) UpsertPresence(p model.UserPresence) error {
	online := 0
	if p.Online {
		online = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO user_presence (username, status, online, last_seen, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(username) DO UPDATE SET
		   status=excluded.status, online=excluded.online,
		   last_seen=excluded.last_seen, updated_at=excluded.updated_at`,
		p.Username, p.Status, online, p.LastSeen.UTC(), p.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upserting presence: %w", err)
	}
	return nil
}

func (s *Store) GetAllPresence() ([]model.UserPresence, error) {
	rows, err := s.db.Query(
		`SELECT username, status, online, last_seen, updated_at FROM user_presence ORDER BY username`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying presence: %w", err)
	}
	defer rows.Close()

	var result []model.UserPresence
	for rows.Next() {
		var p model.UserPresence
		var onlineInt int
		if err := rows.Scan(&p.Username, &p.Status, &onlineInt, &p.LastSeen, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning presence row: %w", err)
		}
		p.Online = onlineInt != 0
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating presence rows: %w", err)
	}
	return result, nil
}

func (s *Store) SetUserOffline(username string) error {
	_, err := s.db.Exec(
		`UPDATE user_presence SET online=0, last_seen=? WHERE username=?`,
		time.Now().UTC(), username,
	)
	if err != nil {
		return fmt.Errorf("setting user offline: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func marshalAttachments(atts []model.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	data, err := json.Marshal(atts)
	if err != nil {
		return ""
	}
	return string(data)
}

func unmarshalAttachments(s string) []model.Attachment {
	if s == "" {
		return nil
	}
	var atts []model.Attachment
	if err := json.Unmarshal([]byte(s), &atts); err != nil {
		return nil
	}
	return atts
}

func DefaultDBPath() (string, error) {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = os.Getenv("APPDATA")
		}
		if localAppData == "" {
			return "", fmt.Errorf("cannot determine Windows data directory: LOCALAPPDATA and APPDATA not set")
		}
		return filepath.Join(localAppData, "marga", "marga.db"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "marga", "marga.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "marga", "marga.db"), nil
}
