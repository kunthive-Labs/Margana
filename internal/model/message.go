// Package model defines the network-neutral data types — messages,
// attachments, typing events, and user presence — that every network adapter
// produces and the TUI consumes, so the UI never sees protocol-specific wire
// formats.
package model

import "time"

type Attachment struct {
	URL         string `json:"url"`
	ProxyURL    string `json:"proxy_url,omitempty"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Size        int    `json:"size,omitempty"`
}

// Reaction is one aggregated emoji reaction on a message: the emoji itself, how
// many users reacted with it, and whether the local user is one of them.
type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Me    bool   `json:"me,omitempty"`
}

type Message struct {
	Network        string       `json:"network,omitempty"`
	EventType      string       `json:"type,omitempty"`
	ID             string       `json:"id"`
	Username       string       `json:"username"`
	UserID         string       `json:"user_id,omitempty"`
	Content        string       `json:"content"`
	Channel        string       `json:"channel"`
	Timestamp      time.Time    `json:"timestamp"`
	ReplyToID      string       `json:"reply_to_id,omitempty"`
	ReplyToContent string       `json:"reply_to_content,omitempty"`
	ReplyToAuthor  string       `json:"reply_to_author,omitempty"`
	Attachments    []Attachment `json:"attachments,omitempty"`
	Editable       bool         `json:"editable,omitempty"`
	// Reactions is the aggregated set of emoji reactions on this message.
	Reactions []Reaction `json:"reactions,omitempty"`
	// ThreadID, when set, is the id of the thread-root event this message
	// belongs to (a lightweight thread indicator; Marga has no separate thread
	// pane).
	ThreadID string `json:"thread_id,omitempty"`
}

type Channel struct {
	Name     string    `json:"name"`
	Type     int       `json:"type,omitempty"`
	JoinedAt time.Time `json:"joined_at"`
}

type TypingEvent struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Channel  string `json:"channel"`
}

type RawEvent struct {
	Type string `json:"type"`
}

type UserPresence struct {
	Username  string    `json:"username"`
	Status    string    `json:"status"`
	Online    bool      `json:"online"`
	LastSeen  time.Time `json:"last_seen"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Notification struct {
	Channel   string
	Username  string
	Content   string
	Timestamp time.Time
	MsgID     string
}
