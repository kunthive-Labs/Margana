package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// panelFetchTimeout bounds a single panel fetch. It is longer than the sources'
// own 10s HTTP timeout so a slow-but-live request is not cut short.
const panelFetchTimeout = 15 * time.Second

// panelIndex returns the index of the panel with the given ID, or -1.
func (m Model) panelIndex(id string) int {
	for i, p := range m.panels {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// panelFetchCmd fetches panel idx immediately (used on startup so the sidebar
// isn't blank until the first tick). The result comes back as a panelDataMsg.
func (m Model) panelFetchCmd(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.panels) {
		return nil
	}
	p := m.panels[idx]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), panelFetchTimeout)
		defer cancel()
		return panelDataMsg{PanelID: p.ID, Result: p.Source.Fetch(ctx)}
	}
}

// panelPollCmd schedules the next fetch of panel idx after its refresh
// interval, re-arming the poll loop. Returns nil for an out-of-range index.
func (m Model) panelPollCmd(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.panels) {
		return nil
	}
	p := m.panels[idx]
	return tea.Tick(p.Interval, func(time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), panelFetchTimeout)
		defer cancel()
		return panelDataMsg{PanelID: p.ID, Result: p.Source.Fetch(ctx)}
	})
}
