package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kunthive-Labs/Margana/internal/commands"
)

// The command palette (Ctrl+K) is a fuzzy quick-switcher over every jump target
// the user has: channels, slash commands, and networks. It reuses the same
// modal-overlay pattern as the help/coach modals (a bool on Model, an early
// return in View, and a render*/handle* pair), and dispatches through the
// existing SwitchChannelMsg / SwitchNetworkMsg paths.

type paletteKind int

const (
	paletteChannel paletteKind = iota
	paletteCommand
	paletteNetwork
)

type paletteItem struct {
	kind   paletteKind
	label  string // shown left, e.g. "#general", "/join", "@matrix"
	hint   string // shown right, dim
	target string // channel name / command name / network id
}

type paletteMatch struct {
	item  paletteItem
	score int
}

type paletteState struct {
	visible bool
	input   InputModel
	items   []paletteItem  // full unfiltered set, built on open
	matches []paletteMatch // current filtered + scored view
	idx     int            // selected index into matches
}

// buildPaletteItems collects every jump target from the current model state.
// Order (channels, commands, networks) also happens to be the ASCII order of
// their prefixes ('#' < '/' < '@'), so an empty query stays naturally grouped.
func (m Model) buildPaletteItems() []paletteItem {
	var items []paletteItem
	for _, c := range m.channels {
		items = append(items, paletteItem{kind: paletteChannel, label: "#" + c, hint: "channel", target: c})
	}
	if m.registry != nil {
		cmds := m.registry.List()
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
		for _, c := range cmds {
			items = append(items, paletteItem{kind: paletteCommand, label: "/" + c.Name(), hint: c.Description(), target: c.Name()})
		}
	}
	for _, id := range m.networkIDs() {
		hint := "switch network"
		if id == m.active {
			hint = "active network"
		}
		items = append(items, paletteItem{kind: paletteNetwork, label: "@" + string(id), hint: hint, target: string(id)})
	}
	return items
}

func (m *Model) openPalette() {
	m.palette.visible = true
	m.palette.input = newInput("")
	m.palette.items = m.buildPaletteItems()
	m.palette.idx = 0
	m.refilterPalette()
}

// refilterPalette re-scores every item against the current query and re-sorts.
func (m *Model) refilterPalette() {
	q := strings.TrimSpace(m.palette.input.Value())
	matches := make([]paletteMatch, 0, len(m.palette.items))
	for _, it := range m.palette.items {
		best, ok := fuzzyScore(q, it.label)
		if !ok {
			// Fall back to matching the description, at a discount, so a query
			// like "channel" can still surface "/join".
			if hs, hok := fuzzyScore(q, it.hint); hok {
				best, ok = hs-4, true
			}
		}
		if ok {
			matches = append(matches, paletteMatch{item: it, score: best})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].item.label < matches[j].item.label
	})
	m.palette.matches = matches
	if m.palette.idx >= len(matches) {
		m.palette.idx = len(matches) - 1
	}
	if m.palette.idx < 0 {
		m.palette.idx = 0
	}
}

func (m *Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+q":
		m.closeAdapters()
		return m, tea.Quit
	case "esc", "ctrl+k":
		m.palette.visible = false
		return m, nil
	case "up", "ctrl+p":
		if m.palette.idx > 0 {
			m.palette.idx--
		}
		return m, nil
	case "down", "ctrl+n":
		if m.palette.idx < len(m.palette.matches)-1 {
			m.palette.idx++
		}
		return m, nil
	case "enter":
		return m.runSelectedPaletteItem()
	case "backspace":
		m.palette.input.Backspace()
		m.refilterPalette()
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				m.palette.input.Insert(r)
			}
			m.refilterPalette()
		}
		return m, nil
	}
}

func (m *Model) runSelectedPaletteItem() (tea.Model, tea.Cmd) {
	m.palette.visible = false
	if m.palette.idx < 0 || m.palette.idx >= len(m.palette.matches) {
		return m, nil
	}
	item := m.palette.matches[m.palette.idx].item
	switch item.kind {
	case paletteChannel:
		ch := item.target
		return m, func() tea.Msg { return commands.SwitchChannelMsg{Channel: ch} }
	case paletteNetwork:
		net := item.target
		return m, func() tea.Msg { return commands.SwitchNetworkMsg{Network: net} }
	case paletteCommand:
		// Prefill the input so the user can add arguments before running.
		m.input.SetValue("/" + item.target + " ")
		return m, nil
	}
	return m, nil
}

func (m Model) renderPalette(width, height int) string {
	title := panelTitleStyle().Render(" Go to ")

	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}
	innerH := height - 4
	if innerH < 3 {
		innerH = 3
	}

	queryLine := lipgloss.NewStyle().Foreground(themeAccent).Bold(true).Render("› ") +
		lipgloss.NewStyle().Foreground(themeFg).Render(m.palette.input.Value()) +
		lipgloss.NewStyle().Foreground(themeAccent).Render("▏")
	hint := lipgloss.NewStyle().Foreground(themeDim).Render("↑/↓ move · enter select · esc close")

	listH := innerH - 2
	if listH < 1 {
		listH = 1
	}
	start := 0
	if m.palette.idx >= listH {
		start = m.palette.idx - listH + 1
	}

	var rows []string
	if len(m.palette.matches) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(themeDim).Render("  no matches"))
	}
	for i := start; i < len(m.palette.matches) && i < start+listH; i++ {
		rows = append(rows, renderPaletteRow(m.palette.matches[i].item, i == m.palette.idx, innerW))
	}

	body := queryLine + "\n" + hint + "\n" + strings.Join(rows, "\n")
	box := renderBorderedBox(panelStyle(), width, height, body)
	return title + "\n" + box
}

// renderPaletteRow shows the label on the left and a dim hint on the right. The
// selected row gets a "›" marker (not just color) so it reads under NO_COLOR.
func renderPaletteRow(it paletteItem, selected bool, width int) string {
	marker := "  "
	labelStyle := lipgloss.NewStyle().Foreground(themeFg)
	if selected {
		marker = "› "
		labelStyle = lipgloss.NewStyle().Foreground(themeAccent).Bold(true)
	}
	left := marker + labelStyle.Render(it.label)
	if gap := width - lipgloss.Width(left) - lipgloss.Width(it.hint); gap >= 1 {
		return left + strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(themeDim).Render(it.hint)
	}
	return left
}
