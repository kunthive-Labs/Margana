package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

func bgMessage(net, channel, user, content string) networkEventMsg {
	return networkEventMsg{Network: network.NetworkID(net), Kind: network.EventMessage, Message: &model.Message{
		ID:        "id-" + content,
		Network:   net,
		Username:  user,
		Content:   content,
		Channel:   channel,
		Timestamp: time.Now(),
		EventType: "message_create",
	}}
}

func TestUnreadIncrementsForBackgroundChannel(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord")) // active=discord, viewing #general
	updated, _ := m.Update(bgMessage("discord", "random", "alice", "hello"))
	mm := updated.(Model)
	if got := mm.unread[unreadKey("discord", "random")]; got != 1 {
		t.Fatalf("expected unread 1 for #random, got %d", got)
	}
	if got := mm.unread[unreadKey("discord", "general")]; got != 0 {
		t.Fatalf("the viewed channel must not accrue unread, got %d", got)
	}
}

func TestMentionCountedForBackgroundChannel(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord")) // username "me"
	updated, _ := m.Update(bgMessage("discord", "random", "alice", "hey @me look"))
	mm := updated.(Model)
	if got := mm.mentions[unreadKey("discord", "random")]; got != 1 {
		t.Fatalf("expected mention 1 for #random, got %d", got)
	}
}

func TestActiveChannelNoUnread(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	updated, _ := m.Update(bgMessage("discord", "general", "alice", "hi"))
	if got := updated.(Model).unread[unreadKey("discord", "general")]; got != 0 {
		t.Fatalf("viewed channel must not accrue unread, got %d", got)
	}
}

func TestClearUnreadOnSwitch(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	m.unread[unreadKey("discord", "random")] = 3
	m.mentions[unreadKey("discord", "random")] = 1
	updated, _ := m.Update(commands.SwitchChannelMsg{Channel: "random"})
	mm := updated.(Model)
	if mm.unread[unreadKey("discord", "random")] != 0 || mm.mentions[unreadKey("discord", "random")] != 0 {
		t.Fatal("switching to a channel should clear its unread/mentions")
	}
}

func TestAltDigitQuickSwitch(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	m.channels = []string{"general", "random", "golang"}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true})
	if cmd == nil {
		t.Fatal("alt+2 should return a switch command")
	}
	sw, ok := cmd().(commands.SwitchChannelMsg)
	if !ok || sw.Channel != "random" {
		t.Fatalf("alt+2 should switch to channels[1]=random, got %#v", cmd())
	}
}

func TestUnreadSummaryPerNetwork(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	m.unread[unreadKey("discord", "random")] = 5
	m.mentions[unreadKey("discord", "random")] = 2
	if s := m.unreadSummary(); s == "" {
		t.Fatal("expected a non-empty unread summary")
	}
}
