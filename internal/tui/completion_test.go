package tui

import (
	"strings"
	"testing"
)

func TestTabCompletionUser(t *testing.T) {
	m := newInput("> ")
	m.SetValue("hello @ar")
	m.SetCompletions([]string{"@arnov"})
	m.ApplyNextCompletion()
	if !strings.Contains(m.Value(), "arnov") {
		t.Errorf("expected completion, got: %q", m.Value())
	}
}

func TestTabCompletionCycles(t *testing.T) {
	m := newInput("> ")
	m.SetValue("@a")
	m.SetCompletions([]string{"@alice", "@arnov"})
	m.ApplyNextCompletion()
	first := m.Value()
	m.SetValue("@a")
	m.SetCompletions([]string{"@alice", "@arnov"})
	m.ApplyNextCompletion()
	m.ApplyNextCompletion()
	second := m.Value()
	if first == second {
		t.Errorf("expected different completions, both got: %q", first)
	}
}

func TestWordAtCursorWithPrefix(t *testing.T) {
	m := newInput("> ")
	m.SetValue("hello @arno")
	word, prefix := m.WordAtCursor()
	if prefix != "@" {
		t.Errorf("expected prefix '@', got %q", prefix)
	}
	if word != "arno" {
		t.Errorf("expected word 'arno', got %q", word)
	}
}

func TestWordAtCursorChannelPrefix(t *testing.T) {
	m := newInput("> ")
	m.SetValue("#gen")
	word, prefix := m.WordAtCursor()
	if prefix != "#" {
		t.Errorf("expected prefix '#', got %q", prefix)
	}
	if word != "gen" {
		t.Errorf("expected word 'gen', got %q", word)
	}
}

func TestWordAtCursorNoPrefix(t *testing.T) {
	m := newInput("> ")
	m.SetValue("hello")
	word, prefix := m.WordAtCursor()
	if prefix != "" {
		t.Errorf("expected no prefix, got %q", prefix)
	}
	if word != "hello" {
		t.Errorf("expected word 'hello', got %q", word)
	}
}

func TestMentionCompletionsCollapseSelfDiscordAliases(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "general", "puang59", "657064257552384044", "puang59", "puang", "test-guild", nil, "", "", "", nil, "")
	m.terminalOnline = []string{"puang", "Vontrix", "puang59", "arnavop7"}
	m.users = []string{"puang", "Vontrix", "puang59", "arnavop7"}
	m.input.SetValue("@")

	m.maybeAutoComplete()

	completions, _ := m.input.CurrentCompletion()
	got := strings.Join(completions, " ")
	if got != "@puang59 @Vontrix @arnavop7" {
		t.Fatalf("expected self aliases collapsed in completions, got %q", got)
	}
}

func TestOnlineUsersCollapseSelfDiscordAliases(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "general", "puang59", "657064257552384044", "puang59", "puang", "test-guild", nil, "", "", "", nil, "")
	m.terminalOnline = []string{"puang", "Vontrix", "puang59", "arnavop7"}

	got := strings.Join(m.onlineUsers(), " ")
	if got != "puang59 Vontrix arnavop7" {
		t.Fatalf("expected self aliases collapsed in online users, got %q", got)
	}
}

func TestCtrlUClearsInput(t *testing.T) {
	m := newInput("> ")
	m.SetValue("hello world")
	// Simulate ctrl+u via the switch in handleInputKey
	m.Clear()
	if m.Value() != "" {
		t.Errorf("expected empty after clear, got %q", m.Value())
	}
}
