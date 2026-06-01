package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMessageJSONOmitsEmptyOptionalFields(t *testing.T) {
	m := Message{
		ID:        "1",
		Username:  "alice",
		Content:   "hi",
		Channel:   "general",
		Timestamp: time.Unix(0, 0).UTC(),
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)

	// Required fields are always present.
	for _, field := range []string{`"id"`, `"username"`, `"content"`, `"channel"`, `"timestamp"`} {
		if !strings.Contains(got, field) {
			t.Errorf("expected required field %s in %s", field, got)
		}
	}
	// Optional fields are omitted when empty.
	for _, field := range []string{`"network"`, `"type"`, `"user_id"`, `"reply_to_id"`, `"attachments"`, `"editable"`} {
		if strings.Contains(got, field) {
			t.Errorf("expected optional field %s to be omitted, got %s", field, got)
		}
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	want := Message{
		Network:     "matrix",
		EventType:   "message_create",
		ID:          "$evt",
		Username:    "bob",
		UserID:      "@bob:hs",
		Content:     "hello",
		Channel:     "!room:hs",
		Timestamp:   time.Unix(1700000000, 0).UTC(),
		ReplyToID:   "$parent",
		Attachments: []Attachment{{URL: "mxc://hs/x", Filename: "a.png", ContentType: "image/png", Width: 1, Height: 2, Size: 3}},
		Editable:    true,
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Message
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Network != want.Network || got.EventType != want.EventType || got.ID != want.ID {
		t.Errorf("header mismatch: %+v", got)
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, want.Timestamp)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Filename != "a.png" {
		t.Errorf("attachment round-trip failed: %+v", got.Attachments)
	}
}

func TestAttachmentJSONTag(t *testing.T) {
	raw, err := json.Marshal(Attachment{URL: "u", Filename: "f"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, `"proxy_url"`) || strings.Contains(got, `"content_type"`) {
		t.Errorf("empty optional attachment fields should be omitted: %s", got)
	}
}
