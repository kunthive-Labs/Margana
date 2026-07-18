package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/history"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
	"github.com/kunthive-Labs/Margana/internal/webhook"
)

func (m Model) ref(channel string) network.ChannelRef {
	return network.ChannelRef{Network: m.active, ID: channel, Name: channel}
}

func (m Model) subscribeCmd(channel string) tea.Cmd {
	a := m.activeAdapter()
	ref := m.ref(channel)
	return func() tea.Msg {
		if a != nil {
			_ = a.Subscribe(ref)
		}
		return nil
	}
}

func (m Model) subscribeSwitchCmd(oldChannel, newChannel string) tea.Cmd {
	a := m.activeAdapter()
	oldRef := m.ref(oldChannel)
	newRef := m.ref(newChannel)
	return func() tea.Msg {
		if a != nil {
			if oldChannel != "" {
				_ = a.Unsubscribe(oldRef)
			}
			_ = a.Subscribe(newRef)
		}
		return nil
	}
}

func (m Model) sendWithEcho(content string) (tea.Model, tea.Cmd) {
	content = normalizeSingleLineCodeFence(content)
	key := contentHash(m.username, m.channel, content)
	m.sentHashes[key] = time.Now()

	replyToID := ""
	if m.replyTo != nil {
		replyToID = m.replyTo.ID
		m.replyTo = nil
	}

	// Scroll to bottom on send so the incoming WS confirmation is visible.
	m.scrollOffset = 0
	m.unreadCount = 0

	return m, m.SendMessage(content, m.channel, replyToID)
}

func normalizeSingleLineCodeFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") || strings.Contains(trimmed, "\n") || len(trimmed) <= 6 {
		return content
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "```"), "```")
	parts := strings.SplitN(strings.TrimSpace(inner), " ", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return content
	}
	return fmt.Sprintf("```%s\n%s\n```", parts[0], parts[1])
}

func (m Model) sendFileWithEcho(path, content string) (tea.Model, tea.Cmd) {
	m.scrollOffset = 0
	m.unreadCount = 0
	return m, m.SendFile(path, m.channel, content)
}

func (m Model) startReplyPicker() (tea.Model, tea.Cmd) {
	m.editSelectMode = false
	m.editSelectIdx = -1
	m.replySelectMode = true
	m.replySelectIdx = -1
	for i := len(m.msgs) - 1; i >= 0; i-- {
		if m.msgs[i].Username != "system" {
			m.replySelectIdx = i
			break
		}
	}
	if m.replySelectIdx >= 0 {
		m.ensureReplySelectVisible()
	}
	return m, nil
}

func (m Model) startEdit(target string) (tea.Model, tea.Cmd) {
	m.replyTo = nil
	m.replySelectMode = false
	m.replySelectIdx = -1
	target = strings.TrimSpace(target)
	if target == "" {
		m.editingMessage = nil
		m.editSelectMode = true
		m.editSelectIdx = m.findMostRecentOwnMessageIndex()
		if m.editSelectIdx >= 0 {
			m.ensureEditSelectVisible()
			return m, nil
		}
		sysMsg := commands.SystemMsg("no editable message found in this channel")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	if strings.EqualFold(target, "last") {
		idx := m.findMostRecentOwnMessageIndex()
		if idx < 0 {
			sysMsg := commands.SystemMsg("no editable message found in this channel")
			m.msgs = append(m.msgs, sysMsg)
			m.scrollOffset = 0
			return m, nil
		}
		return m.prefillEditFromIndex(idx)
	}
	idx := m.findMessageIndex(target)
	if idx < 0 {
		sysMsg := commands.SystemMsg("message not found in current channel")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	if !m.isOwnMessage(m.msgs[idx]) {
		sysMsg := commands.SystemMsg("cannot edit a message from another user")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	return m.prefillEditFromIndex(idx)
}

func (m Model) handleEditCommand(msg commands.EditMessageMsg) (tea.Model, tea.Cmd) {
	target := strings.TrimSpace(msg.Target)
	content := normalizeSingleLineCodeFence(strings.TrimSpace(msg.Content))
	if target == "" || content == "" {
		sysMsg := commands.SystemMsg("usage: /edit, /edit last, or /edit <message-id> [text]")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	messageID := target
	if strings.EqualFold(target, "last") {
		found := false
		for i := len(m.msgs) - 1; i >= 0; i-- {
			if m.msgs[i].Username == "system" || isLocalEchoID(m.msgs[i].ID) {
				continue
			}
			if m.isOwnMessage(m.msgs[i]) {
				messageID = m.msgs[i].ID
				found = true
				break
			}
		}
		if !found {
			sysMsg := commands.SystemMsg("no editable message found in this channel")
			m.msgs = append(m.msgs, sysMsg)
			m.scrollOffset = 0
			return m, nil
		}
	} else if idx := m.findMessageIndex(target); idx >= 0 {
		if !m.isOwnMessage(m.msgs[idx]) {
			sysMsg := commands.SystemMsg("cannot edit a message from another user")
			m.msgs = append(m.msgs, sysMsg)
			m.scrollOffset = 0
			return m, nil
		}
		if !m.msgs[idx].Editable {
			sysMsg := commands.SystemMsg("that message is not editable by this relay; send a new message from this client and edit that one")
			m.msgs = append(m.msgs, sysMsg)
			m.scrollOffset = 0
			return m, nil
		}
	}
	return m, m.EditMessage(messageID, content)
}

func (m Model) confirmEditSelection() (tea.Model, tea.Cmd) {
	if m.editSelectIdx < 0 || m.editSelectIdx >= len(m.msgs) {
		m.editSelectMode = false
		m.editSelectIdx = -1
		return m, nil
	}
	return m.prefillEditFromIndex(m.editSelectIdx)
}

func (m Model) prefillEditFromIndex(idx int) (tea.Model, tea.Cmd) {
	msg := m.msgs[idx]
	if !m.isOwnMessage(msg) {
		sysMsg := commands.SystemMsg("cannot edit a message from another user")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	if !msg.Editable {
		sysMsg := commands.SystemMsg("that message is not editable by this relay; send a new message from this client and edit that one")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	m.editSelectMode = false
	m.editSelectIdx = -1
	m.editingMessage = &msg
	m.input.SetValue(msg.Content)
	m.input.Focus()
	return m, nil
}

func (m *Model) applyMessageUpdate(updated model.Message) *model.Message {
	if updated.ID == "" {
		return nil
	}
	for i := range m.msgs {
		if m.msgs[i].ID != updated.ID {
			continue
		}
		if updated.Username != "" {
			m.msgs[i].Username = updated.Username
		}
		m.msgs[i].Content = updated.Content
		if updated.Channel != "" {
			m.msgs[i].Channel = updated.Channel
		}
		if !updated.Timestamp.IsZero() && m.msgs[i].Timestamp.IsZero() {
			m.msgs[i].Timestamp = updated.Timestamp
		}
		if updated.ReplyToID != "" {
			m.msgs[i].ReplyToID = updated.ReplyToID
			m.msgs[i].ReplyToContent = updated.ReplyToContent
			m.msgs[i].ReplyToAuthor = updated.ReplyToAuthor
		}
		if updated.Attachments != nil {
			m.msgs[i].Attachments = updated.Attachments
		}
		if m.editingMessage != nil && m.editingMessage.ID == updated.ID {
			current := m.editingMessage
			*current = m.msgs[i]
		}
		applied := m.msgs[i]
		return &applied
	}
	return nil
}

// applyReaction folds one reaction delta into the target message's aggregated
// Reactions and returns the updated message (for persistence), or nil if the
// target is not currently loaded. add=false removes one occurrence; me marks
// whether the delta is the local user's own reaction.
func (m *Model) applyReaction(targetID, emoji string, add, me bool) *model.Message {
	if targetID == "" || emoji == "" {
		return nil
	}
	for i := range m.msgs {
		if m.msgs[i].ID != targetID {
			continue
		}
		m.msgs[i].Reactions = updateReactions(m.msgs[i].Reactions, emoji, add, me)
		applied := m.msgs[i]
		return &applied
	}
	return nil
}

// updateReactions applies a single add/remove of emoji to an aggregated set. It
// keeps counts non-negative, drops an emoji whose count reaches zero, and only
// ever upgrades/clears the Me flag on the local user's own add/remove.
func updateReactions(reactions []model.Reaction, emoji string, add, me bool) []model.Reaction {
	idx := -1
	for i := range reactions {
		if reactions[i].Emoji == emoji {
			idx = i
			break
		}
	}
	if add {
		if idx < 0 {
			return append(reactions, model.Reaction{Emoji: emoji, Count: 1, Me: me})
		}
		reactions[idx].Count++
		if me {
			reactions[idx].Me = true
		}
		return reactions
	}
	if idx < 0 {
		return reactions
	}
	reactions[idx].Count--
	if me {
		reactions[idx].Me = false
	}
	if reactions[idx].Count <= 0 {
		return append(reactions[:idx], reactions[idx+1:]...)
	}
	return reactions
}

// handleReact adds an emoji reaction to the most recent real message in the
// active channel, gated on the active adapter advertising the capability.
func (m Model) handleReact(emoji string) (tea.Model, tea.Cmd) {
	a := m.activeAdapter()
	if a == nil || !a.Capabilities().Reactions {
		m.msgs = append(m.msgs, commands.SystemMsg("reactions are not supported on this network"))
		m.scrollOffset = 0
		return m, nil
	}
	idx := -1
	for i := len(m.msgs) - 1; i >= 0; i-- {
		if m.msgs[i].Username == "system" || isLocalEchoID(m.msgs[i].ID) {
			continue
		}
		idx = i
		break
	}
	if idx < 0 {
		m.msgs = append(m.msgs, commands.SystemMsg("no message to react to in this channel"))
		m.scrollOffset = 0
		return m, nil
	}
	resolved := resolveEmoji(emoji)
	targetID := m.msgs[idx].ID
	ref := m.ref(m.channel)
	return m, func() tea.Msg {
		err := a.React(ref, targetID, resolved)
		return reactResultMsg{Err: err}
	}
}

func (m Model) findMessageIndex(id string) int {
	for i := range m.msgs {
		if m.msgs[i].ID == id {
			return i
		}
	}
	return -1
}

func (m Model) isOwnMessage(msg model.Message) bool {
	un := strings.ToLower(msg.Username)
	return un == strings.ToLower(m.username) ||
		(m.discordUsername != "" && un == strings.ToLower(m.discordUsername)) ||
		(m.discordGlobalName != "" && un == strings.ToLower(m.discordGlobalName))
}

func (m Model) findMostRecentOwnMessageIndex() int {
	for i := len(m.msgs) - 1; i >= 0; i-- {
		if m.msgs[i].Username == "system" || isLocalEchoID(m.msgs[i].ID) {
			continue
		}
		if m.isOwnMessage(m.msgs[i]) && m.msgs[i].Editable {
			return i
		}
	}
	return -1
}

func (m Model) openImage(index int) (tea.Model, tea.Cmd) {
	var images []model.Attachment
	for i := len(m.msgs) - 1; i >= 0; i-- {
		for _, att := range m.msgs[i].Attachments {
			if isImageAttachment(att) {
				images = append(images, att)
			}
		}
	}
	if len(images) == 0 {
		sysMsg := commands.SystemMsg("no images found in this channel")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	if index > len(images) {
		sysMsg := commands.SystemMsg(fmt.Sprintf("only %d image(s) available", len(images)))
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	img := images[index-1]
	url := img.ProxyURL
	if url == "" {
		url = img.URL
	}
	if url == "" {
		sysMsg := commands.SystemMsg("image has no URL")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil
	}
	go func() {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Run()
	}()
	sysMsg := commands.SystemMsg(fmt.Sprintf("opening %s", img.Filename))
	m.msgs = append(m.msgs, sysMsg)
	m.scrollOffset = 0
	return m, nil
}

func (m Model) SendMessage(content, channel, replyToID string) tea.Cmd {
	a := m.activeAdapter()
	if a == nil {
		return nil
	}
	ref := m.ref(channel)
	return func() tea.Msg {
		msgID, err := a.Send(context.Background(), ref, content, replyToID)
		return webhook.SendResultMsg{Content: content, MessageID: msgID, Err: err}
	}
}

func (m Model) SendFile(path, channel, content string) tea.Cmd {
	a := m.activeAdapter()
	if a == nil {
		return nil
	}
	ref := m.ref(channel)
	return func() tea.Msg {
		msgID, err := a.SendFile(context.Background(), ref, path, content)
		return webhook.SendFileResultMsg{Path: path, Content: content, MessageID: msgID, Err: err}
	}
}

func (m Model) EditMessage(messageID, content string) tea.Cmd {
	a := m.activeAdapter()
	if a == nil {
		return nil
	}
	ref := m.ref(m.channel)
	return func() tea.Msg {
		err := a.Edit(context.Background(), ref, messageID, content)
		return webhook.EditResultMsg{MessageID: messageID, Content: content, Err: err}
	}
}

// fetchHistoryCmd loads a page of history from the active adapter, returning a
// history.FetchResultMsg so existing Update handlers stay unchanged.
func (m Model) fetchHistoryCmd(channel string, limit int, before *time.Time) tea.Cmd {
	a := m.activeAdapter()
	if a == nil {
		return nil
	}
	ref := m.ref(channel)
	return func() tea.Msg {
		msgs, err := a.FetchHistory(context.Background(), ref, limit, before)
		return history.FetchResultMsg{Messages: msgs, Channel: channel, Err: err}
	}
}

func (m Model) initialFetchCmd(channel string, limit int) tea.Cmd {
	if limit <= 0 {
		limit = 100
	}
	return m.fetchHistoryCmd(channel, limit, nil)
}

func (m Model) loadOlderCmd(channel string, oldest time.Time) tea.Cmd {
	before := oldest
	return m.fetchHistoryCmd(channel, 100, &before)
}

// fetchChannelsCmd lists channels on the active adapter, returning the existing
// history.ChannelsResultMsg type.
func (m Model) fetchChannelsCmd() tea.Cmd {
	a := m.activeAdapter()
	if a == nil {
		return nil
	}
	server := ""
	return func() tea.Msg {
		refs, err := a.ListChannels(context.Background(), server)
		if err != nil {
			return history.ChannelsResultMsg{Err: err}
		}
		names := make([]string, 0, len(refs))
		for _, r := range refs {
			names = append(names, r.Name)
		}
		return history.ChannelsResultMsg{Channels: names}
	}
}

// fetchSinceCmd polls for messages newer than `since`. Only adapters that
// implement network.SinceFetcher (the relay) support catch-up polling; others
// rely on their live event stream, so this is a no-op for them.
func (m Model) fetchSinceCmd(channel string, since time.Time) tea.Cmd {
	a := m.activeAdapter()
	sf, ok := a.(network.SinceFetcher)
	if !ok {
		return nil
	}
	ref := m.ref(channel)
	return func() tea.Msg {
		msgs, err := sf.FetchSince(context.Background(), ref, since)
		if err != nil {
			return history.FetchResultMsg{Channel: channel, Err: err}
		}
		return history.FetchResultMsg{Messages: msgs, Channel: channel}
	}
}

func (m Model) persistMessage(msg model.Message) tea.Cmd {
	if m.store == nil {
		return nil
	}
	return func() tea.Msg {
		err := m.store.InsertMessage(msg)
		return dbWriteResultMsg{Err: err}
	}
}

func (m Model) persistMessageUpdate(msg model.Message) tea.Cmd {
	if m.store == nil {
		return nil
	}
	return func() tea.Msg {
		err := m.store.UpdateMessage(msg)
		return dbWriteResultMsg{Err: err}
	}
}

func (m Model) persistNotification(n model.Notification) tea.Cmd {
	if m.store == nil {
		return nil
	}
	return func() tea.Msg {
		err := m.store.InsertNotification(n)
		return dbWriteResultMsg{Err: err}
	}
}
