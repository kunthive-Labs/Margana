package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Per-channel unread tracking (D4). Unlike unreadCount (which counts new
// messages below the scroll position in the *active* channel), these maps track
// unread and mention counts for every channel across every network, keyed by a
// network-qualified key so same-named channels on different networks don't
// collide.

// unreadKey builds a stable, network-qualified map key. A NUL byte can appear in
// neither a network id nor a channel name, so it is a safe separator.
func unreadKey(net, channel string) string {
	return net + "\x00" + channel
}

// clearUnread zeroes the unread + mention counters for a channel — called when
// that channel becomes the one on screen.
func (m *Model) clearUnread(net, channel string) {
	key := unreadKey(net, channel)
	delete(m.unread, key)
	delete(m.mentions, key)
}

// channelBadge returns a short unread/mention badge for a channel in the active
// network's sidebar, or "" if it has none. Mentions win over plain unread. The
// glyphs (@ / ●) carry the meaning without relying on color.
func (m Model) channelBadge(channel string) string {
	key := unreadKey(string(m.active), channel)
	if mc := m.mentions[key]; mc > 0 {
		return mentionBadgeStyle().Render(fmt.Sprintf("@%d", mc))
	}
	if uc := m.unread[key]; uc > 0 {
		return lipgloss.NewStyle().Foreground(themeAccent).Render(fmt.Sprintf("●%d", uc))
	}
	return ""
}

// unreadSummary renders a compact per-network rollup for the status bar, e.g.
// "matrix @2 ●5". Networks with nothing unread are omitted; empty when nothing
// is unread anywhere. This is how unread on a *non-active* network stays visible
// even though the sidebar only lists the active network's channels.
func (m Model) unreadSummary() string {
	type agg struct{ unread, mentions int }
	byNet := make(map[string]*agg)
	add := func(key string, mention bool, n int) {
		net := key
		if i := strings.IndexByte(key, 0); i >= 0 {
			net = key[:i]
		}
		if byNet[net] == nil {
			byNet[net] = &agg{}
		}
		if mention {
			byNet[net].mentions += n
		} else {
			byNet[net].unread += n
		}
	}
	for k, n := range m.unread {
		add(k, false, n)
	}
	for k, n := range m.mentions {
		add(k, true, n)
	}

	var parts []string
	for _, id := range m.networkIDs() {
		a := byNet[string(id)]
		if a == nil || (a.unread == 0 && a.mentions == 0) {
			continue
		}
		s := string(id)
		if a.mentions > 0 {
			s += fmt.Sprintf(" @%d", a.mentions)
		}
		if a.unread > 0 {
			s += fmt.Sprintf(" ●%d", a.unread)
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(themeDim).Render(strings.Join(parts, "  "))
}
