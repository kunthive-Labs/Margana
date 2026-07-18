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

func reactionEvent(roomID, sender, targetID, key string) *event.Event {
	return &event.Event{
		ID:        id.EventID("$react1"),
		Sender:    id.UserID(sender),
		RoomID:    id.RoomID(roomID),
		Timestamp: 1700000000000,
		Type:      event.EventReaction,
		Content: event.Content{Parsed: &event.ReactionEventContent{
			RelatesTo: event.RelatesTo{Type: event.RelAnnotation, EventID: id.EventID(targetID), Key: key},
		}},
	}
}

func TestOnReactionMapsToDelta(t *testing.T) {
	a := newTestAdapter(t)
	evt := reactionEvent("!room:example.org", "@bob:example.org", "$target", "👍")

	a.onReaction(context.Background(), evt)

	ev := <-a.events
	if ev.Kind != network.EventMessage || ev.Message == nil {
		t.Fatalf("expected message event, got %+v", ev)
	}
	m := ev.Message
	if m.EventType != "reaction_add" {
		t.Fatalf("expected reaction_add, got %q", m.EventType)
	}
	if m.ID != "$target" {
		t.Fatalf("reaction must target the annotated event id, got %q", m.ID)
	}
	if m.Content != "👍" {
		t.Fatalf("reaction emoji must ride in Content, got %q", m.Content)
	}
	if m.Channel != "!room:example.org" {
		t.Fatalf("reaction channel = %q", m.Channel)
	}
	if m.Username != "bob" || m.UserID != "@bob:example.org" {
		t.Fatalf("reactor identity not set: username=%q userID=%q", m.Username, m.UserID)
	}
	if m.Network != string(ID) {
		t.Fatalf("network not stamped: %q", m.Network)
	}
}

func TestReactionToMessageRejectsMalformed(t *testing.T) {
	a := newTestAdapter(t)
	cases := []struct {
		name string
		evt  *event.Event
	}{
		{"missing key", reactionEvent("!r:hs", "@b:hs", "$t", "")},
		{"missing target", reactionEvent("!r:hs", "@b:hs", "", "👍")},
		{"wrong parsed type", &event.Event{
			Type:    event.EventReaction,
			Content: event.Content{Parsed: &event.MessageEventContent{MsgType: event.MsgText}},
		}},
		{"nil parsed", &event.Event{Type: event.EventReaction, Content: event.Content{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if m := a.reactionToMessage(tc.evt); m != nil {
				t.Fatalf("expected nil for %s, got %+v", tc.name, m)
			}
		})
	}
}

func TestOnRoomMessageThread(t *testing.T) {
	cases := []struct {
		name        string
		rel         *event.RelatesTo
		wantThread  string
		wantReplyTo string
	}{
		{
			name: "thread with fallback reply hides the reply",
			rel: &event.RelatesTo{
				Type:          event.RelThread,
				EventID:       id.EventID("$threadroot"),
				InReplyTo:     &event.InReplyTo{EventID: id.EventID("$prev")},
				IsFallingBack: true,
			},
			wantThread:  "$threadroot",
			wantReplyTo: "",
		},
		{
			name: "thread with genuine reply keeps both",
			rel: &event.RelatesTo{
				Type:      event.RelThread,
				EventID:   id.EventID("$threadroot"),
				InReplyTo: &event.InReplyTo{EventID: id.EventID("$real")},
			},
			wantThread:  "$threadroot",
			wantReplyTo: "$real",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAdapter(t)
			content := &event.MessageEventContent{MsgType: event.MsgText, RelatesTo: tc.rel}
			evt := roomMessageEvent("!room:example.org", "@bob:example.org", "in thread", content)

			a.onRoomMessage(context.Background(), evt)

			m := (<-a.events).Message
			if m.ThreadID != tc.wantThread {
				t.Fatalf("ThreadID = %q, want %q", m.ThreadID, tc.wantThread)
			}
			if m.ReplyToID != tc.wantReplyTo {
				t.Fatalf("ReplyToID = %q, want %q", m.ReplyToID, tc.wantReplyTo)
			}
			if m.EventType != "message_create" {
				t.Fatalf("EventType = %q, want message_create", m.EventType)
			}
		})
	}
}

func TestReactRequiresClient(t *testing.T) {
	a := &Adapter{}
	if err := a.React(network.ChannelRef{ID: "!r:hs"}, "$e", "👍"); err == nil {
		t.Fatal("React: expected error when not connected")
	}
}

func TestCapabilitiesReactions(t *testing.T) {
	a := newTestAdapter(t)
	if !a.Capabilities().Reactions {
		t.Fatal("matrix adapter must advertise Reactions capability")
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

func TestListServersReturnsHomeserver(t *testing.T) {
	a := &Adapter{homeserver: "https://matrix.example.org"}
	servers, err := a.ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].ID != "https://matrix.example.org" {
		t.Errorf("server ID = %q", servers[0].ID)
	}
	if servers[0].Name != "matrix.example.org" {
		t.Errorf("server Name = %q, want stripped host", servers[0].Name)
	}
}

func TestFetchHistoryAndSetStatusRequireClient(t *testing.T) {
	a := &Adapter{}
	if _, err := a.FetchHistory(context.Background(), network.ChannelRef{ID: "!r:hs"}, 10, nil); err == nil {
		t.Fatal("FetchHistory: expected error when not connected")
	}
	if err := a.SetStatus("online"); err == nil {
		t.Fatal("SetStatus: expected error when not connected")
	}
}
