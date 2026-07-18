package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

// reactionEvent builds a networkEventMsg carrying a reaction delta.
func reactionEvent(kind, emoji, targetID, reactor string) networkEventMsg {
	return networkEventMsg{Network: "discord", Kind: network.EventMessage, Message: &model.Message{
		EventType: kind,
		ID:        targetID,
		Content:   emoji,
		Channel:   "general",
		Username:  reactor,
		UserID:    reactor,
	}}
}

func findMsg(m Model, id string) (model.Message, bool) {
	for _, msg := range m.msgs {
		if msg.ID == id {
			return msg, true
		}
	}
	return model.Message{}, false
}

// TestApplyReactionAggregates drives reaction_add / reaction_remove deltas
// through the event path and asserts the aggregated counts and the local user's
// own-reaction flag.
func TestApplyReactionAggregates(t *testing.T) {
	a := newFakeAdapter("discord")
	m := newTestModel(a) // active=discord, channel=general, username=me

	// Seed a target message in the active channel.
	seeded, _ := m.Update(networkEventMsg{Network: "discord", Kind: network.EventMessage, Message: &model.Message{
		ID: "m1", Username: "alice", Content: "hi", Channel: "general", Timestamp: time.Now(),
	}})
	m = seeded.(Model)

	apply := func(ev networkEventMsg) {
		updated, _ := m.Update(ev)
		m = updated.(Model)
	}

	// alice 👍
	apply(reactionEvent("reaction_add", "👍", "m1", "alice"))
	// me 👍  -> count 2, mine
	apply(reactionEvent("reaction_add", "👍", "m1", "me"))
	// bob 🎉
	apply(reactionEvent("reaction_add", "🎉", "m1", "bob"))

	msg, ok := findMsg(m, "m1")
	if !ok {
		t.Fatal("target message m1 missing")
	}
	if len(msg.Reactions) != 2 {
		t.Fatalf("expected 2 distinct reactions, got %d (%+v)", len(msg.Reactions), msg.Reactions)
	}
	thumbs := reactionByEmoji(msg.Reactions, "👍")
	if thumbs == nil || thumbs.Count != 2 || !thumbs.Me {
		t.Fatalf("👍 aggregate wrong: %+v", thumbs)
	}
	party := reactionByEmoji(msg.Reactions, "🎉")
	if party == nil || party.Count != 1 || party.Me {
		t.Fatalf("🎉 aggregate wrong: %+v", party)
	}

	// me removes 👍 -> count 1, no longer mine
	apply(reactionEvent("reaction_remove", "👍", "m1", "me"))
	msg, _ = findMsg(m, "m1")
	thumbs = reactionByEmoji(msg.Reactions, "👍")
	if thumbs == nil || thumbs.Count != 1 || thumbs.Me {
		t.Fatalf("after self-remove, 👍 should be count 1 not mine: %+v", thumbs)
	}

	// alice removes 👍 -> emoji dropped entirely
	apply(reactionEvent("reaction_remove", "👍", "m1", "alice"))
	msg, _ = findMsg(m, "m1")
	if reactionByEmoji(msg.Reactions, "👍") != nil {
		t.Fatalf("👍 should be gone at count 0, got %+v", msg.Reactions)
	}
}

func reactionByEmoji(rs []model.Reaction, emoji string) *model.Reaction {
	for i := range rs {
		if rs[i].Emoji == emoji {
			return &rs[i]
		}
	}
	return nil
}

// TestApplyReactionUnknownTargetIsNoop verifies a delta for a message not in the
// view is ignored (no panic, no phantom message).
func TestApplyReactionUnknownTargetIsNoop(t *testing.T) {
	a := newFakeAdapter("discord")
	m := newTestModel(a)
	before := len(m.msgs)
	updated, _ := m.Update(reactionEvent("reaction_add", "👍", "ghost", "alice"))
	m = updated.(Model)
	if len(m.msgs) != before {
		t.Fatalf("reaction on missing target must not add a message: %d -> %d", before, len(m.msgs))
	}
}

// TestMessageLineCountAccountsForExtras is the invariant that messageLineCount
// stays in sync with the extra rows the viewport renders for reply, thread, and
// reaction lines.
func TestMessageLineCountAccountsForExtras(t *testing.T) {
	const width = 80
	base := model.Message{
		ID: "x", Username: "alice", Content: "one line", Channel: "general", Timestamp: time.Now(),
	}
	baseCount := messageLineCount(base, width, "me")
	if baseCount < 1 {
		t.Fatalf("base line count must be >= 1, got %d", baseCount)
	}

	withReply := base
	withReply.ReplyToID = "r1"
	if got := messageLineCount(withReply, width, "me"); got != baseCount+1 {
		t.Fatalf("reply must add one line: base=%d got=%d", baseCount, got)
	}

	withThread := base
	withThread.ThreadID = "t1"
	if got := messageLineCount(withThread, width, "me"); got != baseCount+1 {
		t.Fatalf("thread indicator must add one line: base=%d got=%d", baseCount, got)
	}

	withReaction := base
	withReaction.Reactions = []model.Reaction{{Emoji: "👍", Count: 1}}
	if got := messageLineCount(withReaction, width, "me"); got != baseCount+1 {
		t.Fatalf("reactions must add one line: base=%d got=%d", baseCount, got)
	}

	// A reaction with a zero count renders nothing and must not be counted.
	zeroReaction := base
	zeroReaction.Reactions = []model.Reaction{{Emoji: "👍", Count: 0}}
	if got := messageLineCount(zeroReaction, width, "me"); got != baseCount {
		t.Fatalf("empty reactions must add no line: base=%d got=%d", baseCount, got)
	}

	all := base
	all.ReplyToID = "r1"
	all.ThreadID = "t1"
	all.Reactions = []model.Reaction{{Emoji: "👍", Count: 3}}
	if got := messageLineCount(all, width, "me"); got != baseCount+3 {
		t.Fatalf("reply+thread+reactions must add three lines: base=%d got=%d", baseCount, got)
	}
}

// TestReactionsLineRendersCountsAndShortcodes verifies the summary renders the
// count and resolves :shortcodes: to unicode emoji.
func TestReactionsLineRendersCountsAndShortcodes(t *testing.T) {
	m := model.Message{Reactions: []model.Reaction{
		{Emoji: ":tada:", Count: 2, Me: true},
		{Emoji: "👍", Count: 1},
	}}
	line := reactionsLine(m)
	if !strings.Contains(line, "🎉") {
		t.Fatalf(":tada: shortcode not resolved: %q", line)
	}
	if !strings.Contains(line, "👍") {
		t.Fatalf("raw emoji missing: %q", line)
	}
	if !strings.Contains(line, "2") || !strings.Contains(line, "1") {
		t.Fatalf("counts missing: %q", line)
	}
}
