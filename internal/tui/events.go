package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

func typingTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return typingTickMsg{}
	})
}

// listenNetwork blocks on one adapter's event stream and returns the next
// event as a networkEventMsg. Exactly one of these is in flight per adapter;
// the networkEventMsg handler re-arms it after processing.
func (m Model) listenNetwork(id network.NetworkID) tea.Cmd {
	adapter, ok := m.adapters[id]
	if !ok || adapter == nil {
		return nil
	}
	ch := adapter.Events()
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return networkEventMsg(ev)
	}
}

func (m Model) onMessageEvent(msg model.Message, fromActive bool) (Model, []tea.Cmd) {
	// Only the active network feeds the visible channel list and message view.
	// Messages from other networks are still persisted (and may notify) so they
	// are ready when you switch to them.
	if fromActive {
		m.addChannel(msg.Channel)
	}

	if msg.EventType == "message_update" {
		updated := msg
		if fromActive {
			if applied := m.applyMessageUpdate(updated); applied != nil {
				m.users = msgsToUsers(m.msgs)
				updated = *applied
			}
		}
		return m, []tea.Cmd{m.persistMessageUpdate(updated)}
	}

	// Check for @mention — only notify if not from self and the channel
	// isn't muted.
	var notifCmd tea.Cmd
	if !m.isSelfMessage(msg) && containsMentionExact(msg.Content, m.username) && !m.isChannelMuted(msg.Channel) {
		n := model.Notification{
			Channel:   msg.Channel,
			Username:  msg.Username,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			MsgID:     msg.ID,
		}
		m.notifications = append(m.notifications, n)
		notifCmd = m.persistNotification(n)
		if m.bellOnMention() {
			notifCmd = tea.Batch(notifCmd, bellCmd())
		}
		if m.desktopNotify() && (!m.termFocused || msg.Channel != m.channel) {
			notifCmd = tea.Batch(notifCmd, osNotifyCmd(n.Channel, n.Username, n.Content))
		}
	}

	if !fromActive || msg.Channel != m.channel {
		batch := []tea.Cmd{m.persistMessage(msg)}
		if notifCmd != nil {
			batch = append(batch, notifCmd)
		}
		return m, batch
	}
	// Try dedup with message's username and all known self aliases.
	serverMsg := msg
	if m.deduplicateSentMessage(msg.Username, msg.Channel, msg.Content) {
		var reconciled bool
		m.msgs, reconciled = reconcileLocalEcho(m.msgs, serverMsg)
		if !reconciled {
			m.msgs = insertSorted(m.msgs, serverMsg)
		}
		m.users = msgsToUsers(m.msgs)
		return m, []tea.Cmd{m.persistMessage(serverMsg)}
	}
	// No sentHash match — still try to reconcile a local echo (e.g. file
	// messages, which don't register sentHashes).
	if m.isSelfMessage(msg) {
		var reconciled bool
		m.msgs, reconciled = reconcileLocalEcho(m.msgs, serverMsg)
		if reconciled {
			m.users = msgsToUsers(m.msgs)
			return m, []tea.Cmd{m.persistMessage(serverMsg)}
		}
	}
	m.msgs = insertSorted(m.msgs, msg)
	if len(m.msgs) > 1000 {
		m.msgs = m.msgs[len(m.msgs)-1000:]
	}
	m.users = msgsToUsers(m.msgs)
	if m.scrollOffset > 0 {
		m.unreadCount++
	} else {
		m.scrollOffset = 0
	}
	batch := []tea.Cmd{m.persistMessage(msg)}
	if notifCmd != nil {
		batch = append(batch, notifCmd)
	}
	return m, batch
}

func (m Model) onStatusEvent(state network.ConnState, err error, retryAt time.Time) (Model, []tea.Cmd) {
	m.status = state
	if err != nil {
		m.addError(fmt.Sprintf("connection: %v", err))
	}
	if state == network.StateConnected {
		m.errors = nil
		m.reconnectAt = time.Time{}
		if m.channel != "" {
			return m, []tea.Cmd{m.subscribeCmd(m.channel)}
		}
		return m, nil
	}
	// Disconnected / reconnecting: record the projected retry time and start a
	// 1s ticker (once) so the countdown re-renders.
	m.reconnectAt = retryAt
	if !m.reconnectTicking {
		m.reconnectTicking = true
		return m, []tea.Cmd{reconnectTickCmd()}
	}
	return m, nil
}

func (m Model) onTypingEvent(te model.TypingEvent) (Model, []tea.Cmd) {
	m.addChannel(te.Channel)
	if te.Channel == m.channel || te.Channel == "" {
		if m.typingUsers == nil {
			m.typingUsers = make(map[string]time.Time)
		}
		m.typingUsers[te.Username] = time.Now()
	}
	return m, []tea.Cmd{typingTickCmd()}
}

func (m Model) onPresenceEvent(p model.UserPresence) (Model, []tea.Cmd) {
	m.presences[p.Username] = p
	if m.store != nil {
		store := m.store
		go func() { _ = store.UpsertPresence(p) }()
	}
	return m, nil
}

func (m Model) onPresentUsers(users []string) (Model, []tea.Cmd) {
	m.terminalOnline = users
	return m, nil
}
