package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kunthive-Labs/Margana/internal/commands"
)

type fakePaletteCommand struct {
	name string
	desc string
}

func (c fakePaletteCommand) Name() string                           { return c.name }
func (c fakePaletteCommand) Description() string                    { return c.desc }
func (c fakePaletteCommand) Execute(args []string) (tea.Cmd, error) { return nil, nil }

func TestPaletteBuildsAllTargetKinds(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	m.channels = []string{"general", "random"}
	reg := commands.NewRegistry()
	reg.Register(fakePaletteCommand{name: "join", desc: "switch to a channel"})
	m.registry = reg

	var haveChan, haveCmd, haveNet bool
	for _, it := range m.buildPaletteItems() {
		switch it.kind {
		case paletteChannel:
			haveChan = haveChan || it.target == "general"
		case paletteCommand:
			haveCmd = haveCmd || it.label == "/join"
		case paletteNetwork:
			haveNet = haveNet || it.target == "discord"
		}
	}
	if !haveChan || !haveCmd || !haveNet {
		t.Fatalf("missing targets: chan=%v cmd=%v net=%v", haveChan, haveCmd, haveNet)
	}
}

func TestPaletteFilterRanksBestMatchFirst(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	m.channels = []string{"general", "random", "golang"}
	m.openPalette()
	for _, r := range "gol" {
		m.palette.input.Insert(r)
	}
	m.refilterPalette()

	if len(m.palette.matches) == 0 {
		t.Fatal("expected matches for 'gol'")
	}
	if top := m.palette.matches[0].item; top.target != "golang" {
		t.Fatalf("expected #golang ranked first, got %q", top.label)
	}
}

func TestPaletteEnterSwitchesChannel(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	m.channels = []string{"general", "golang"}
	m.openPalette()
	for _, r := range "golang" {
		m.palette.input.Insert(r)
	}
	m.refilterPalette()

	_, cmd := m.runSelectedPaletteItem()
	if m.palette.visible {
		t.Fatal("palette should close after selection")
	}
	if cmd == nil {
		t.Fatal("expected a command from selection")
	}
	sw, ok := cmd().(commands.SwitchChannelMsg)
	if !ok || sw.Channel != "golang" {
		t.Fatalf("expected SwitchChannelMsg{golang}, got %#v", cmd())
	}
}

func TestPaletteCommandPrefillsInput(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	reg := commands.NewRegistry()
	reg.Register(fakePaletteCommand{name: "search", desc: "search history"})
	m.registry = reg
	m.openPalette()
	for _, r := range "search" {
		m.palette.input.Insert(r)
	}
	m.refilterPalette()

	m.runSelectedPaletteItem()
	if got := m.input.Value(); got != "/search " {
		t.Fatalf("expected input prefilled with '/search ', got %q", got)
	}
}

func TestPaletteOpensAndClosesViaCtrlK(t *testing.T) {
	m := newTestModel(newFakeAdapter("discord"))
	// Key messages route through the pointer-receiver handleKey, so Update
	// returns *Model on this path.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	mp, ok := updated.(*Model)
	if !ok {
		t.Fatalf("key update should return *Model, got %T", updated)
	}
	if !mp.palette.visible {
		t.Fatal("ctrl+k should open the palette")
	}
	updated, _ = mp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(*Model).palette.visible {
		t.Fatal("esc should close the palette")
	}
}
