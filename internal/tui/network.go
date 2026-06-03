package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

func (m Model) loadLocalHistory(channel string, limit int) tea.Cmd {
	if m.store == nil {
		return nil
	}
	return func() tea.Msg {
		messages, err := m.store.GetMessages(channel, limit, nil)
		if err != nil {
			return localHistoryMsg{Err: err}
		}
		channels, err := m.store.GetChannels()
		if err != nil {
			return localHistoryMsg{Err: err}
		}
		return localHistoryMsg{
			Messages: reverseMessages(messages),
			Channels: channels,
			Channel:  channel,
		}
	}
}

// activeAdapter returns the network adapter backing the current channel.
func (m Model) activeAdapter() network.Network {
	return m.adapters[m.active]
}

// closeAdapters disconnects every adapter; used on quit and before a restart.
func (m Model) closeAdapters() {
	for _, a := range m.adapters {
		if a != nil {
			_ = a.Disconnect()
		}
	}
}

// networkIDs returns the connected network IDs in a stable (sorted) order.
func (m Model) networkIDs() []network.NetworkID {
	ids := make([]network.NetworkID, 0, len(m.adapters))
	for id := range m.adapters {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// switchNetwork makes target the active network, resetting the view so the new
// network's channels load fresh. An empty target lists available networks.
func (m Model) switchNetwork(target network.NetworkID) (Model, tea.Cmd) {
	if target == "" {
		var b strings.Builder
		b.WriteString("networks:")
		for _, id := range m.networkIDs() {
			marker := "  "
			if id == m.active {
				marker = "* "
			}
			fmt.Fprintf(&b, "\n  %s%s", marker, id)
		}
		b.WriteString("\nswitch with /network <name> or ctrl+t")
		m.msgs = append(m.msgs, commands.SystemMsg(b.String()))
		m.scrollOffset = 0
		return m, nil
	}
	if target == m.active {
		m.msgs = append(m.msgs, commands.SystemMsg(fmt.Sprintf("already on network %q", target)))
		m.scrollOffset = 0
		return m, nil
	}
	if _, ok := m.adapters[target]; !ok {
		m.msgs = append(m.msgs, commands.SystemMsg(fmt.Sprintf("unknown network %q — use /network to list", target)))
		m.scrollOffset = 0
		return m, nil
	}

	m.active = target
	// Reset the per-network view. fetchChannelsCmd lists the new network's
	// channels; the ChannelsResultMsg handler then auto-selects the first one
	// (m.channel == "" is never in the new set) and loads its history.
	m.channel = ""
	m.channels = nil
	m.available = make(map[string]struct{})
	m.channelsOK = false
	m.msgs = []model.Message{commands.SystemMsg(fmt.Sprintf("switched to network %q", target))}
	m.scrollOffset = 0
	m.unreadCount = 0
	m.historyLoaded = false
	m.allHistoryLoaded = false
	m.loadingHistory = false
	m.replyTo = nil
	m.users = nil
	m.terminalOnline = nil
	m.typingUsers = make(map[string]time.Time)
	m.presences = make(map[string]model.UserPresence)

	return m, m.fetchChannelsCmd()
}

// cycleNetwork advances to the next connected network (ctrl+t). It is a no-op
// when only one network is connected.
func (m Model) cycleNetwork() (Model, tea.Cmd) {
	ids := m.networkIDs()
	if len(ids) < 2 {
		return m, nil
	}
	cur := 0
	for i, id := range ids {
		if id == m.active {
			cur = i
			break
		}
	}
	return m.switchNetwork(ids[(cur+1)%len(ids)])
}

// ref builds a ChannelRef on the active network for a channel name. For the
// relay, the channel name is also its native id.
