package matrix

import (
	"context"
	"testing"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/kunthive-Labs/Margana/internal/network"
)

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	client, err := mautrix.NewClient("https://example.org", id.UserID("@me:example.org"), "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return &Adapter{client: client, events: make(chan network.Event, 8)}
}

func roomMessageEvent(roomID, sender, body string, content *event.MessageEventContent) *event.Event {
	content.Body = body
	return &event.Event{
		ID:        id.EventID("$evt1"),
		Sender:    id.UserID(sender),
		RoomID:    id.RoomID(roomID),
		Timestamp: 1700000000000,
		Type:      event.EventMessage,
		Content:   event.Content{Parsed: content},
	}
}

func TestOnRoomMessageText(t *testing.T) {
	a := newTestAdapter(t)
	evt := roomMessageEvent("!room:example.org", "@bob:example.org", "hello", &event.MessageEventContent{MsgType: event.MsgText})

	a.onRoomMessage(context.Background(), evt)

	ev := <-a.events
	if ev.Kind != network.EventMessage || ev.Message == nil {
		t.Fatalf("expected message event, got %+v", ev)
	}
	m := ev.Message
	if m.Content != "hello" || m.Channel != "!room:example.org" || m.EventType != "message_create" {
		t.Fatalf("bad mapping: %+v", m)
	}
	if m.Network != string(ID) {
		t.Fatalf("network not stamped: %q", m.Network)
	}
	if m.Username != "bob" {
		t.Fatalf("expected localpart username, got %q", m.Username)
	}
}

func TestOnRoomMessageEdit(t *testing.T) {
	a := newTestAdapter(t)
	content := &event.MessageEventContent{
		MsgType:    event.MsgText,
		NewContent: &event.MessageEventContent{MsgType: event.MsgText, Body: "edited"},
		RelatesTo:  &event.RelatesTo{Type: event.RelReplace, EventID: id.EventID("$original")},
	}
	evt := roomMessageEvent("!room:example.org", "@bob:example.org", "* edited", content)

	a.onRoomMessage(context.Background(), evt)

	m := (<-a.events).Message
	if m.EventType != "message_update" {
		t.Fatalf("expected message_update, got %q", m.EventType)
	}
	if m.ID != "$original" {
		t.Fatalf("edit must target the replaced event id, got %q", m.ID)
	}
	if m.Content != "edited" {
		t.Fatalf("edit must use new content, got %q", m.Content)
	}
}

func TestOnRoomMessageReply(t *testing.T) {
	a := newTestAdapter(t)
	content := &event.MessageEventContent{
		MsgType:   event.MsgText,
		RelatesTo: &event.RelatesTo{InReplyTo: &event.InReplyTo{EventID: id.EventID("$parent")}},
	}
	evt := roomMessageEvent("!room:example.org", "@bob:example.org", "re", content)

	a.onRoomMessage(context.Background(), evt)

	m := (<-a.events).Message
	if m.ReplyToID != "$parent" {
		t.Fatalf("expected reply_to id, got %q", m.ReplyToID)
	}
}

func TestOnRoomMessageImageAttachment(t *testing.T) {
	a := newTestAdapter(t)
	content := &event.MessageEventContent{
		MsgType:  event.MsgImage,
		FileName: "pic.png",
		URL:      id.ContentURIString("mxc://example.org/abc"),
		Info:     &event.FileInfo{MimeType: "image/png", Width: 10, Height: 20, Size: 1234},
	}
	evt := roomMessageEvent("!room:example.org", "@bob:example.org", "pic.png", content)

	a.onRoomMessage(context.Background(), evt)

	m := (<-a.events).Message
	if len(m.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(m.Attachments))
	}
	att := m.Attachments[0]
	if att.Filename != "pic.png" || att.URL != "mxc://example.org/abc" || att.ContentType != "image/png" {
		t.Fatalf("bad attachment mapping: %+v", att)
	}
}

func TestLocalpart(t *testing.T) {
	if got := localpart("@alice:example.org"); got != "alice" {
		t.Fatalf("localpart: got %q", got)
	}
	if got := localpart("bob"); got != "bob" {
		t.Fatalf("localpart no-domain: got %q", got)
	}
}
