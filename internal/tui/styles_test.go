package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/model"
)

func TestUsernameColorDeterministic(t *testing.T) {
	c1 := usernameColor("arnav")
	c2 := usernameColor("arnav")
	if c1 != c2 {
		t.Error("expected same color for same username")
	}
}

func TestColoredUsername(t *testing.T) {
	result := coloredUsername("arnav")
	if result == "" {
		t.Error("expected non-empty colored username")
	}
	if !strings.Contains(result, "arnav") {
		t.Errorf("expected result to contain username, got: %s", result)
	}
}

func TestMinInt(t *testing.T) {
	if minInt(1, 2) != 1 {
		t.Error("expected 1")
	}
	if minInt(5, 3) != 3 {
		t.Error("expected 3")
	}
	if minInt(4, 4) != 4 {
		t.Error("expected 4")
	}
}

func TestMaxInt(t *testing.T) {
	if maxInt(1, 2) != 2 {
		t.Error("expected 2")
	}
	if maxInt(5, 3) != 5 {
		t.Error("expected 5")
	}
}

func TestClampInt(t *testing.T) {
	if clampInt(5, 0, 10) != 5 {
		t.Error("expected 5")
	}
	if clampInt(-1, 0, 10) != 0 {
		t.Errorf("expected 0, got %d", clampInt(-1, 0, 10))
	}
	if clampInt(15, 0, 10) != 10 {
		t.Errorf("expected 10, got %d", clampInt(15, 0, 10))
	}
	if clampInt(0, 0, 10) != 0 {
		t.Error("expected 0")
	}
	if clampInt(10, 0, 10) != 10 {
		t.Error("expected 10")
	}
}

func TestWrapText(t *testing.T) {
	result := wrapText("hello world", 5)
	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 wrapped lines, got %d: %q", len(lines), result)
	}
	if lines[0] != "hello" {
		t.Errorf("expected 'hello', got %q", lines[0])
	}
	if lines[1] != " worl" {
		t.Errorf("expected ' worl', got %q", lines[1])
	}
	if lines[2] != "d" {
		t.Errorf("expected 'd', got %q", lines[2])
	}
}

func TestWrapTextShortText(t *testing.T) {
	result := wrapText("hi", 80)
	if result != "hi" {
		t.Errorf("expected 'hi', got %q", result)
	}
}

func TestWrapTextZeroWidth(t *testing.T) {
	result := wrapText("hello", 0)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestMsgsToUsers(t *testing.T) {
	msgs := []model.Message{
		{Username: "alice"},
		{Username: "bob"},
		{Username: "alice"},
		{Username: "charlie"},
	}
	users := msgsToUsers(msgs)
	if len(users) != 3 {
		t.Fatalf("expected 3 unique users, got %d", len(users))
	}
	if users[0] != "alice" || users[1] != "bob" || users[2] != "charlie" {
		t.Errorf("unexpected user order: %v", users)
	}
}

func TestMsgsToUsersEmpty(t *testing.T) {
	users := msgsToUsers(nil)
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestApplyThemeDracula(t *testing.T) {
	ApplyTheme("dracula")
	if themeBg != lipgloss.Color("#282a36") {
		t.Fatalf("expected dracula background, got %q", themeBg)
	}
	if themeAccent != lipgloss.Color("#bd93f9") {
		t.Fatalf("expected dracula accent, got %q", themeAccent)
	}
	if len(usernameColors) == 0 {
		t.Fatal("expected username colors for dracula")
	}
}

func TestApplyThemeSolarized(t *testing.T) {
	ApplyTheme("solarized")
	if themeBg != lipgloss.Color("#002b36") {
		t.Fatalf("expected solarized background, got %q", themeBg)
	}
	if themeAccent != lipgloss.Color("#eee8d5") {
		t.Fatalf("expected solarized accent, got %q", themeAccent)
	}
	if len(usernameColors) == 0 {
		t.Fatal("expected username colors for solarized")
	}
}

func TestApplyThemeDefaultAndUnknown(t *testing.T) {
	ApplyTheme("dracula")
	ApplyTheme("unknown-theme")
	if themeBg != lipgloss.Color("#000000") {
		t.Fatalf("expected default background for unknown theme, got %q", themeBg)
	}
	if themeAccent != lipgloss.Color("#ffffff") {
		t.Fatalf("expected default accent for unknown theme, got %q", themeAccent)
	}
}
