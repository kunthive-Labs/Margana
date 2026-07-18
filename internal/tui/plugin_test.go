package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/plugin"
)

// newKeybindManager loads a single plugin that binds ctrl+j to a set_input
// effect, for exercising the TUI's plugin-key dispatch.
func newKeybindManager(t *testing.T) *plugin.Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.lua")
	src := `marga.keybind{ key = "ctrl+j", description = "set input", run = function() marga.set_input("from-plugin") end }`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("writing plugin: %v", err)
	}
	mgr := plugin.New(plugin.Options{
		Files:   []plugin.FileSpec{{Path: path, Enabled: true}},
		Timeout: time.Second,
	}, nil)
	t.Cleanup(mgr.Close)
	return mgr
}

// TestPluginKeyRoutes verifies a bound plugin key reaches its Lua handler.
func TestPluginKeyRoutes(t *testing.T) {
	m := newTestModel(newFakeAdapter("test"))
	m = m.WithPlugins(newKeybindManager(t))

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if cmd == nil {
		t.Fatal("bound plugin key ctrl+j produced no command")
	}
	msg := cmd()
	si, ok := msg.(commands.SetInputMsg)
	if !ok || si.Value != "from-plugin" {
		t.Fatalf("expected commands.SetInputMsg{from-plugin}, got %T %+v", msg, msg)
	}
}

// TestPluginKeyDoesNotShadowBuiltins verifies a built-in key still runs its
// built-in behavior even when plugin keys are present.
func TestPluginKeyDoesNotShadowBuiltins(t *testing.T) {
	m := newTestModel(newFakeAdapter("test"))
	m = m.WithPlugins(newKeybindManager(t))

	before := m.channelsVisible
	// ctrl+b is a built-in (toggle channel sidebar). It must win over any plugin
	// binding and change model state rather than dispatch to a plugin.
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB}); cmd != nil {
		t.Fatalf("built-in ctrl+b should not return a command, got one")
	}
	if m.channelsVisible == before {
		t.Errorf("built-in ctrl+b did not toggle channelsVisible (stayed %v)", before)
	}
}

// TestUnboundKeyIgnoredWithPlugins verifies an unbound control key falls
// through to normal input handling without dispatching to a plugin.
func TestUnboundKeyIgnoredWithPlugins(t *testing.T) {
	m := newTestModel(newFakeAdapter("test"))
	m = m.WithPlugins(newKeybindManager(t))

	// ctrl+j is bound; ctrl+u is not. An unbound key must not route to a plugin.
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU}); cmd != nil {
		if _, ok := cmd().(commands.SetInputMsg); ok {
			t.Error("unbound key ctrl+u must not trigger a plugin keybinding")
		}
	}
}
