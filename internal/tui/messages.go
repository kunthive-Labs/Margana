package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/model"
)

func insertSorted(msgs []model.Message, m model.Message) []model.Message {
	for _, existing := range msgs {
		if existing.ID == m.ID {
			return msgs
		}
	}
	for i, existing := range msgs {
		if m.Timestamp.Before(existing.Timestamp) {
			msgs = append(msgs[:i], append([]model.Message{m}, msgs[i:]...)...)
			return msgs
		}
	}
	return append(msgs, m)
}

func mergeMessages(existing, incoming []model.Message) []model.Message {
	all := append([]model.Message(nil), existing...)
	seen := make(map[string]struct{}, len(all)+len(incoming))
	for _, m := range all {
		seen[m.ID] = struct{}{}
	}

	for _, m := range incoming {
		if _, ok := seen[m.ID]; ok {
			for i := range all {
				if all[i].ID == m.ID {
					all[i] = mergeMessageFields(all[i], m)
					break
				}
			}
			continue
		}
		if idx := findLocalEchoMatch(all, m); idx >= 0 {
			delete(seen, all[idx].ID)
			all[idx] = m
			seen[m.ID] = struct{}{}
			continue
		}
		all = append(all, m)
		seen[m.ID] = struct{}{}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.Before(all[j].Timestamp)
	})
	return all
}

func mergeMessageFields(existing, incoming model.Message) model.Message {
	if incoming.Username != "" {
		existing.Username = incoming.Username
	}
	if incoming.Content != "" || existing.Content == "" {
		existing.Content = incoming.Content
	}
	if incoming.Channel != "" {
		existing.Channel = incoming.Channel
	}
	if existing.Timestamp.IsZero() && !incoming.Timestamp.IsZero() {
		existing.Timestamp = incoming.Timestamp
	}
	if incoming.ReplyToID != "" {
		existing.ReplyToID = incoming.ReplyToID
		existing.ReplyToContent = incoming.ReplyToContent
		existing.ReplyToAuthor = incoming.ReplyToAuthor
	}
	if incoming.Attachments != nil {
		existing.Attachments = incoming.Attachments
	}
	if incoming.Editable {
		existing.Editable = true
	}
	return existing
}

func reconcileLocalEcho(msgs []model.Message, replacement model.Message) ([]model.Message, bool) {
	if idx := findLocalEchoMatch(msgs, replacement); idx >= 0 {
		msgs[idx] = replacement
		sort.Slice(msgs, func(i, j int) bool {
			return msgs[i].Timestamp.Before(msgs[j].Timestamp)
		})
		return msgs, true
	}
	return msgs, false
}

func findLocalEchoMatch(msgs []model.Message, replacement model.Message) int {
	if isLocalEchoID(replacement.ID) {
		return -1
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if sameLocalEchoMessage(msgs[i], replacement) {
			return i
		}
	}
	return -1
}

func sameLocalEchoMessage(echo, replacement model.Message) bool {
	if !isLocalEchoID(echo.ID) || isLocalEchoID(replacement.ID) {
		return false
	}
	if echo.Channel != replacement.Channel || echo.Content != replacement.Content || echo.ReplyToID != replacement.ReplyToID {
		return false
	}
	if echo.Timestamp.IsZero() || replacement.Timestamp.IsZero() {
		return true
	}
	delta := echo.Timestamp.Sub(replacement.Timestamp)
	if delta < 0 {
		delta = -delta
	}
	return delta <= 10*time.Minute
}

func isLocalEchoID(id string) bool {
	return strings.HasPrefix(id, "echo-") || strings.HasPrefix(id, "file-echo-")
}

func mergeChannels(existing, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	var merged []string
	for _, ch := range append(existing, incoming...) {
		ch = strings.TrimPrefix(strings.TrimSpace(ch), "#")
		if ch == "" {
			continue
		}
		if _, ok := seen[ch]; ok {
			continue
		}
		seen[ch] = struct{}{}
		merged = append(merged, ch)
	}
	sort.Strings(merged)
	return merged
}

func channelsToSet(channels []string) map[string]struct{} {
	set := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		ch = strings.TrimPrefix(strings.TrimSpace(ch), "#")
		if ch != "" {
			set[ch] = struct{}{}
		}
	}
	return set
}

func splitJoinInput(input string) (bool, string) {
	fields := strings.Fields(input)
	if len(fields) == 0 || fields[0] != "/join" {
		return false, ""
	}
	if len(fields) == 1 {
		if strings.HasSuffix(input, " ") {
			return true, ""
		}
		return false, ""
	}
	return true, fields[1]
}

func reverseMessages(msgs []model.Message) []model.Message {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs
}

func contentHash(username, channel, content string) string {
	h := sha256.New()
	h.Write([]byte(username + "\x00" + channel + "\x00" + content))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// isSelfMessage returns true if the incoming message was sent by the local user.
func (m *Model) isSelfMessage(msg model.Message) bool {
	if m.discordID != "" && msg.UserID != "" {
		return msg.UserID == m.discordID
	}
	un := strings.ToLower(msg.Username)
	return un == strings.ToLower(m.username) ||
		(m.discordUsername != "" && un == strings.ToLower(m.discordUsername)) ||
		(m.discordGlobalName != "" && un == strings.ToLower(m.discordGlobalName))
}

// deduplicateSentMessage checks sentHashes for the message using the received
// username and all known self-username aliases.  Returns true if a hash match
// is found (and removes the hash entry).
func (m *Model) deduplicateSentMessage(username, channel, content string) bool {
	key := contentHash(username, channel, content)
	if _, ok := m.sentHashes[key]; ok {
		delete(m.sentHashes, key)
		return true
	}
	for _, uname := range []string{m.username, m.discordUsername, m.discordGlobalName} {
		if uname == "" || uname == username {
			continue
		}
		key2 := contentHash(uname, channel, content)
		if _, ok := m.sentHashes[key2]; ok {
			delete(m.sentHashes, key2)
			return true
		}
	}
	return false
}

// containsMentionExact checks whether content contains @username as a whole
// word (not as a substring of a longer username).
func containsMentionExact(content, username string) bool {
	if username == "" {
		return false
	}
	lower := strings.ToLower(content)
	target := "@" + strings.ToLower(username)
	i := 0
	for {
		idx := strings.Index(lower[i:], target)
		if idx < 0 {
			return false
		}
		abs := i + idx
		end := abs + len(target)
		if end >= len(lower) || !isWordRune(rune(lower[end])) {
			return true
		}
		i = abs + 1
	}
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// isChannelMuted reports whether mention notifications are suppressed for the
// given channel per [notifications].muted_channels (case-insensitive).
func (m *Model) isChannelMuted(channel string) bool {
	if m.setupCfg == nil {
		return false
	}
	for _, c := range m.setupCfg.Notifications.MutedChannels {
		if strings.EqualFold(c, channel) {
			return true
		}
	}
	return false
}

// bellOnMention reports whether the terminal bell should fire on a mention.
func (m *Model) bellOnMention() bool {
	return m.setupCfg != nil && m.setupCfg.Notifications.BellOnMention
}

// bellCmd writes the BEL control character to the terminal, triggering the
// emulator's audible/visual bell without disturbing the rendered frame.
func bellCmd() tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(os.Stderr, "\a")
		return nil
	}
}

// periodicRefreshCmd schedules a periodic history refresh every 30 seconds.
func periodicRefreshCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return periodicRefreshMsg{}
	})
}

// WithGithub sets the GitHub repo and token for the activity panel.
