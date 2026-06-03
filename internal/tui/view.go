package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/network"
)

func fitToSize(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clipped := lipgloss.NewStyle().
		MaxWidth(width).
		MaxHeight(height).
		Render(content)
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, clipped)
}

func borderedStyleWidth(totalWidth int) int {
	if totalWidth <= 2 {
		return 1
	}
	return totalWidth - 2
}

func borderedStyleHeight(totalHeight int) int {
	if totalHeight <= 2 {
		return 0
	}
	return totalHeight - 2
}

func borderedContentHeight(totalHeight int) int {
	if totalHeight <= 2 {
		return 0
	}
	return totalHeight - 2
}

func panelContentWidth(totalWidth int) int {
	if totalWidth <= 4 {
		return 1
	}
	return totalWidth - 4
}

func renderBorderedBox(style lipgloss.Style, width, height int, content string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	style = style.Width(borderedStyleWidth(width)).MaxWidth(width).MaxHeight(height)
	if h := borderedStyleHeight(height); h > 0 {
		style = style.Height(h)
	}
	return fitToSize(style.Render(content), width, height)
}

func (m Model) View() string {
	if m.width < 40 || m.height < 12 {
		return lipgloss.NewStyle().
			Width(m.width).Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(themeAccent).
			Render(fmt.Sprintf("terminal too small (%dx%d)\nmin 80x24 recommended", m.width, m.height))
	}

	if m.isSetupVisible() {
		modalW := m.width * 70 / 100
		modalH := m.height * 60 / 100
		if modalW < 50 {
			modalW = 50
		}
		if modalW > m.width-6 {
			modalW = m.width - 6
		}
		if modalH < 12 {
			modalH = 12
		}
		if modalH > m.height-4 {
			modalH = m.height - 4
		}
		return centerInTerm(m.renderSetupWizard(modalW, modalH), modalW, modalH, m.width, m.height)
	}

	statusBar := fitToSize(m.renderStatusBar(), m.width, 1)
	contentHeight := m.height - 1

	chWidth := m.channelSidebarWidth()
	rightWidth := m.rightSidebarWidth()
	showChannels := m.channelsVisible && chWidth > 0
	showRight := rightWidth > 0

	chatW := m.width
	if showChannels {
		chatW -= chWidth
	}
	if showRight {
		chatW -= rightWidth
	}
	if chatW < 20 {
		chatW = 20
	}

	m.input.SetWidth(chatW)

	channelsPanel := m.renderChannels(chWidth, contentHeight)
	chatPanel := m.renderChatArea(chatW, contentHeight)
	rightPanel := m.renderRightSidebar(rightWidth, contentHeight)

	var content string
	if !showChannels && !showRight {
		content = chatPanel
	} else if !showChannels {
		content = lipgloss.JoinHorizontal(lipgloss.Top, chatPanel, rightPanel)
	} else if !showRight {
		content = lipgloss.JoinHorizontal(lipgloss.Top, channelsPanel, chatPanel)
	} else {
		content = lipgloss.JoinHorizontal(lipgloss.Top, channelsPanel, chatPanel, rightPanel)
	}

	content = fitToSize(content, m.width, contentHeight)
	full := lipgloss.JoinVertical(lipgloss.Left, statusBar, content)

	if m.helpVisible {
		modalW := m.width * 80 / 100
		modalH := m.height * 80 / 100
		if modalW < 50 {
			modalW = 50
		}
		if modalW > m.width-6 {
			modalW = m.width - 6
		}
		if modalH < 15 {
			modalH = 15
		}
		if modalH > m.height-4 {
			modalH = m.height - 4
		}
		helpModal := m.renderHelpModal(modalW, modalH)
		return centerInTerm(helpModal, modalW, modalH, m.width, m.height)
	}

	if m.errorsVisible {
		modalW := m.width * 70 / 100
		modalH := m.height * 70 / 100
		if modalW < 40 {
			modalW = 40
		}
		if modalW > m.width-6 {
			modalW = m.width - 6
		}
		if modalH < 10 {
			modalH = 10
		}
		if modalH > m.height-4 {
			modalH = m.height - 4
		}
		errorModal := m.renderErrors(modalW, modalH)
		return centerInTerm(errorModal, modalW, modalH, m.width, m.height)
	}

	return full
}

func (m Model) renderStatusBar() string {
	connStr := string(m.status)
	switch m.status {
	case network.StateConnected:
		connStr = "connected"
	case network.StateDisconnected:
		connStr = "disconnected"
	case network.StateReconnecting:
		connStr = "reconnecting"
	}

	onlineCount := len(m.onlineUsers())
	serverPart := ""
	if m.guildName != "" {
		serverPart = lipgloss.NewStyle().Foreground(themeCyan).Render(m.guildName) + "  "
	}
	left := fmt.Sprintf(" %s  %s#%s  %d online", connStr, serverPart, m.channel, onlineCount)

	right := lipgloss.NewStyle().Foreground(themeDim).Render("ctrl+h help") + " "

	if !m.lastSendOk && m.sendErr != "" {
		left = statusErrorStyle().Render(fmt.Sprintf(" ⚠ %s", m.sendErr))
	}

	unreadPart := ""
	if m.unreadCount > 0 {
		unreadPart = fmt.Sprintf(" | ↑ %d new", m.unreadCount)
	}

	leftStr := left + unreadPart
	leftW := lipgloss.Width(leftStr)
	rightW := lipgloss.Width(right)
	gap := m.width - 2 - leftW - rightW
	if gap < 0 {
		gap = 0
	}

	bar := leftStr + strings.Repeat(" ", gap) + right
	return statusBarStyle().Width(m.width).MaxWidth(m.width).MaxHeight(1).Render(bar)
}

func (m Model) renderChannels(width, height int) string {
	if width < 8 {
		return ""
	}

	title := panelTitleStyle().Render(fmt.Sprintf(" Channels %d ", len(m.channels)))
	var items []string
	for _, ch := range m.channels {
		active := ch == m.channel
		prefix := "  "
		if active {
			prefix = "* "
		}
		items = append(items, channelStyle(active).Render(prefix+"#"+ch))
	}

	content := strings.Join(items, "\n")
	boxHeight := height - 1
	if boxHeight < 1 {
		boxHeight = 1
	}
	content = clipLines(content, borderedContentHeight(boxHeight))
	box := renderBorderedBox(panelStyle(), width, boxHeight, content)
	return fitToSize(title+"\n"+box, width, height)
}

func (m Model) renderUsers(width, height int) string {
	if width < 8 {
		return ""
	}

	online := m.onlineUsers()
	label := "Online"
	title := panelTitleStyle().Render(fmt.Sprintf(" %s %d ", label, len(online)))
	innerW := panelContentWidth(width)
	var items []string
	for _, u := range online {
		colored := lipgloss.NewStyle().Foreground(usernameColor(u)).Render("@" + u)
		items = append(items, userStyle(false).Render(colored))
		if p, ok := m.presences[u]; ok && p.Status != "" {
			truncated := truncateStatus(p.Status, maxInt(1, innerW-2))
			items = append(items, presenceStatusStyle().Render("↳ "+truncated))
		}
	}

	content := strings.Join(items, "\n")
	boxHeight := height - 1
	if boxHeight < 1 {
		boxHeight = 1
	}
	content = clipLines(content, borderedContentHeight(boxHeight))
	box := renderBorderedBox(panelStyle(), width, boxHeight, content)
	return fitToSize(title+"\n"+box, width, height)
}

func (m Model) renderRightSidebar(width, height int) string {
	if width < 10 {
		return ""
	}

	githubHeight := 0
	if m.githubRepo != "" && len(m.githubEvents) > 0 {
		githubHeight = clampInt(len(m.githubEvents)+3, 5, height/4)
	}

	usersHeight := 0
	if m.usersVisible {
		usersHeight = clampInt(len(m.onlineUsers())+4, 7, height/3)
		if usersHeight < 7 {
			usersHeight = 7
		}
	}

	notificationsHeight := height - usersHeight - githubHeight
	if notificationsHeight < 8 {
		notificationsHeight = 8
		if usersHeight > 0 {
			usersHeight = height - notificationsHeight - githubHeight
			if usersHeight < 0 {
				usersHeight = 0
			}
		}
	}

	var panels []string
	if usersHeight > 0 {
		panels = append(panels, m.renderUsers(width, usersHeight))
	}
	if githubHeight > 0 {
		panels = append(panels, m.renderGithubActivity(width, githubHeight))
	}
	notificationsPanel := m.renderNotifications(width, notificationsHeight)
	panels = append(panels, notificationsPanel)

	if len(panels) == 1 {
		return panels[0]
	}
	return fitToSize(lipgloss.JoinVertical(lipgloss.Left, panels...), width, height)
}

func (m Model) renderGithubActivity(width, height int) string {
	if width < 10 || len(m.githubEvents) == 0 {
		return ""
	}
	title := panelTitleStyle().Render(fmt.Sprintf(" GitHub: %s ", m.githubRepo))
	var items []string
	for _, ev := range m.githubEvents {
		ts := ev.Timestamp.Local().Format("01-02 15:04")
		evType := strings.TrimSuffix(ev.Type, "Event")
		line := fmt.Sprintf("%s %s @%s", ts, evType, ev.Actor)
		items = append(items, lipgloss.NewStyle().Foreground(themeDim).PaddingLeft(1).Render(line))
		if ev.Title != "" {
			preview := ev.Title
			maxW := width - 4
			if maxW < 8 {
				maxW = 8
			}
			if len([]rune(preview)) > maxW {
				preview = string([]rune(preview)[:maxW]) + "…"
			}
			items = append(items, lipgloss.NewStyle().Foreground(themeAccent).PaddingLeft(2).Render(preview))
		}
	}
	content := strings.Join(items, "\n")
	boxHeight := height - 1
	if boxHeight < 1 {
		boxHeight = 1
	}
	content = clipLines(content, borderedContentHeight(boxHeight))
	box := renderBorderedBox(panelStyle(), width, boxHeight, content)
	return fitToSize(title+"\n"+box, width, height)
}

func (m Model) renderNotifications(width, height int) string {
	if width < 10 {
		return ""
	}

	title := panelTitleStyle().Render(fmt.Sprintf(" Mentions %d ", len(m.notifications)))
	var items []string

	if len(m.notifications) == 0 {
		items = append(items, lipgloss.NewStyle().Foreground(themeDim).Italic(true).PaddingLeft(2).Render("no mentions yet"))
	} else {
		maxPreview := width - 6
		if maxPreview < 6 {
			maxPreview = 6
		}
		for i, n := range m.notifications {
			selected := i == m.notifIdx
			ts := n.Timestamp.Local().Format("15:04")
			preview := strings.ReplaceAll(n.Content, "\n", " ")
			if len([]rune(preview)) > maxPreview {
				preview = string([]rune(preview)[:maxPreview]) + "…"
			}
			line := fmt.Sprintf("%s #%s\n  @%s: %s", ts, n.Channel, n.Username, preview)
			items = append(items, notifItemStyle(selected).Width(panelContentWidth(width)).Render(line))
		}
	}

	content := strings.Join(items, "\n")
	boxHeight := height - 1
	if boxHeight < 1 {
		boxHeight = 1
	}
	content = clipLines(content, borderedContentHeight(boxHeight))
	box := renderBorderedBox(panelStyle(), width, boxHeight, content)
	return fitToSize(title+"\n"+box, width, height)
}

func (m Model) renderErrors(width, height int) string {
	if len(m.errors) == 0 {
		boxContent := lipgloss.NewStyle().
			Foreground(themeDim).
			Align(lipgloss.Center, lipgloss.Center).
			Width(width - 4).Height(height - 5).
			Render("no errors recorded")
		box := renderBorderedBox(panelStyle(), width, height, boxContent)
		title := panelTitleStyle().Render(" Errors 0 ")
		return title + "\n" + box
	}

	title := panelTitleStyle().Render(fmt.Sprintf(" Errors %d ", len(m.errors)))

	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}
	innerH := height - 4
	if innerH < 1 {
		innerH = 1
	}

	hintLine := lipgloss.NewStyle().Foreground(themeDim).Render("↑↓ pgup/pgdn scroll  esc/enter close  ctrl+o toggle")

	var lines []string
	lines = append(lines, hintLine)

	total := len(m.errors)
	start := m.errorScrollIdx - innerH + 2
	if start < 0 {
		start = 0
	}
	if start >= total {
		start = maxInt(0, total-1)
	}
	end := start + innerH - 1
	if end > total {
		end = total
	}

	for i := start; i < end; i++ {
		err := m.errors[i]
		ts := err.Timestamp.Local().Format("01-02 15:04:05")
		msg := err.Message
		maxMsgW := innerW - len(ts) - 3
		if maxMsgW < 10 {
			maxMsgW = 10
		}
		msgRunes := []rune(msg)
		if len(msgRunes) > maxMsgW {
			msg = string(msgRunes[:maxMsgW])
		}

		line := fmt.Sprintf("%s  %s", ts, msg)
		isSelected := i == m.errorScrollIdx
		if isSelected {
			line = lipgloss.NewStyle().Foreground(themeAccent).Render(line)
		} else {
			line = lipgloss.NewStyle().Foreground(themeFg).Render(line)
		}
		lines = append(lines, line)
	}

	if end < total {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render(fmt.Sprintf("  + %d more above", total-end)))
	}
	if start > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render(fmt.Sprintf("  + %d more below", start)))
	}

	content := strings.Join(lines, "\n")
	boxContent := clipLines(content, innerH)
	box := renderBorderedBox(panelStyle(), width, height, boxContent)

	return title + "\n" + box
}

func (m Model) renderHelpModal(width, height int) string {
	title := panelTitleStyle().Render(" Help ")

	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}
	innerH := height - 4
	if innerH < 1 {
		innerH = 1
	}

	hintLine := lipgloss.NewStyle().Foreground(themeDim).Render("esc / enter / ctrl+h close")

	var sections []string

	sections = append(sections, m.helpSection("Navigation", innerW, [][2]string{
		{"↑ / ↓ / PgUp / PgDn", "Scroll messages"},
		{"ctrl + l", "Jump to latest message"},
		{"ctrl + b", "Toggle channels sidebar"},
		{"ctrl + y", "Toggle users sidebar"},
		{"ctrl + p / ctrl + n", "Previous / next channel"},
	}))

	sections = append(sections, m.helpSection("Messages", innerW, [][2]string{
		{"ctrl + r", "Reply to latest message"},
		{"ctrl + g", "Select message to reply"},
		{"ctrl + e", "Edit your latest message"},
		{"alt + r", "Select reply target"},
		{"alt + e", "Select message to edit"},
		{"enter", "Send / confirm selection"},
		{"shift + enter", "New line in message"},
		{"esc", "Cancel reply / edit / clear input"},
	}))

	sections = append(sections, m.helpSection("Actions", innerW, [][2]string{
		{"ctrl + ]", "Toggle mentions panel"},
		{"ctrl + o", "Toggle errors panel"},
		{"ctrl + s", "Add / configure a server"},
		{"ctrl + v", "Paste image from clipboard"},
		{"ctrl + c / ctrl + q", "Quit"},
		{"ctrl + h", "Toggle this help"},
	}))

	var cmdLines [][2]string
	if m.registry != nil {
		cmds := m.registry.List()
		sort.Slice(cmds, func(i, j int) bool {
			return cmds[i].Name() < cmds[j].Name()
		})
		for _, c := range cmds {
			cmdLines = append(cmdLines, [2]string{"/" + c.Name(), c.Description()})
		}
	}
	if len(cmdLines) > 0 {
		sections = append(sections, m.helpSection("Commands", innerW, cmdLines))
	}

	content := strings.Join(sections, "\n")
	content = clipLines(content, innerH)
	boxContent := hintLine + "\n" + content
	box := renderBorderedBox(panelStyle(), width, height, boxContent)

	return title + "\n" + box
}

func (m Model) helpSection(name string, width int, items [][2]string) string {
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Foreground(themeAccent).Bold(true).Render(name))
	keyW := 0
	for _, item := range items {
		w := lipgloss.Width(item[0])
		if w > keyW {
			keyW = w
		}
	}
	padding := keyW + 4
	for _, item := range items {
		key := lipgloss.NewStyle().Foreground(themeCyan).Render(item[0])
		desc := item[1]
		avail := width - padding
		if avail < 10 {
			avail = 10
		}
		descRunes := []rune(desc)
		if len(descRunes) > avail {
			desc = string(descRunes[:avail-1]) + "…"
		}
		line := key + strings.Repeat(" ", padding-lipgloss.Width(item[0])) + lipgloss.NewStyle().Foreground(themeFg).Render(desc)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderChatArea(width, height int) string {
	titleText := fmt.Sprintf(" #%s ", m.channel)
	if m.loadingHistory {
		titleText = fmt.Sprintf(" #%s %s", m.channel, loadingStyle().Render("(loading...)"))
	}
	title := panelTitleStyle().Render(titleText)

	typingLine := m.renderTypingIndicator()
	typingHeight := 0
	if typingLine != "" {
		typingHeight = 1
	}

	commandSuggestions := m.renderCommandSuggestions(width)
	suggestionsHeight := 0
	if commandSuggestions != "" {
		suggestionsHeight = lipgloss.Height(commandSuggestions)
	}

	// Reply bar above input
	replyBar := ""
	replyBarHeight := 0
	if m.replySelectMode {
		if m.replySelectIdx >= 0 && m.replySelectIdx < len(m.msgs) {
			sm := m.msgs[m.replySelectIdx]
			snippet := strings.ReplaceAll(sm.Content, "\n", " ")
			if len([]rune(snippet)) > width-30 {
				snippet = string([]rune(snippet)[:width-30]) + "…"
			}
			replyBar = fitToSize(replySelectPromptStyle().Width(width).MaxWidth(width).MaxHeight(1).Render(
				fmt.Sprintf("↩ reply to @%s: %s  [↑↓ move  enter confirm  esc cancel]", sm.Username, snippet),
			), width, 1)
		} else {
			replyBar = fitToSize(replySelectPromptStyle().Width(width).MaxWidth(width).MaxHeight(1).Render(
				"↩ select a message to reply to  [↑↓ move  enter confirm  esc cancel]",
			), width, 1)
		}
		replyBarHeight = 1
	} else if m.editSelectMode {
		if m.editSelectIdx >= 0 && m.editSelectIdx < len(m.msgs) {
			sm := m.msgs[m.editSelectIdx]
			snippet := strings.ReplaceAll(sm.Content, "\n", " ")
			if len([]rune(snippet)) > width-30 {
				snippet = string([]rune(snippet)[:width-30]) + "…"
			}
			replyBar = fitToSize(replySelectPromptStyle().Width(width).MaxWidth(width).MaxHeight(1).Render(
				fmt.Sprintf("✎ edit your message: %s  [↑↓ move  enter edit  esc cancel]", snippet),
			), width, 1)
		} else {
			replyBar = fitToSize(replySelectPromptStyle().Width(width).MaxWidth(width).MaxHeight(1).Render(
				"✎ select one of your messages to edit  [↑↓ move  enter edit  esc cancel]",
			), width, 1)
		}
		replyBarHeight = 1
	} else if m.replyTo != nil {
		snippet := strings.ReplaceAll(m.replyTo.Content, "\n", " ")
		if len([]rune(snippet)) > width-20 {
			snippet = string([]rune(snippet)[:width-20]) + "…"
		}
		replyBar = fitToSize(replyBarStyle().Width(width).MaxWidth(width).MaxHeight(1).Render(
			fmt.Sprintf("↩ replying to @%s: %s  [Esc to cancel]", m.replyTo.Username, snippet),
		), width, 1)
		replyBarHeight = 1
	} else if m.editingMessage != nil {
		snippet := strings.ReplaceAll(m.editingMessage.Content, "\n", " ")
		if len([]rune(snippet)) > width-28 {
			snippet = string([]rune(snippet)[:width-28]) + "…"
		}
		replyBar = fitToSize(replyBarStyle().Width(width).MaxWidth(width).MaxHeight(1).Render(
			fmt.Sprintf("✎ editing your message: %s  [Enter save  Esc cancel]", snippet),
		), width, 1)
		replyBarHeight = 1
	}

	// Mention autocomplete suggestions
	mentionSuggestions := ""
	mentionsHeight := 0
	if !m.replySelectMode && !m.editSelectMode {
		_, prefix := m.input.WordAtCursor()
		if (prefix == "@" || prefix == "#") && len(m.input.completions) > 0 {
			mentionSuggestions = m.renderMentionSuggestions(width)
			if mentionSuggestions != "" {
				mentionsHeight = lipgloss.Height(mentionSuggestions)
			}
		}
	}

	inputLines := m.input.LineCount()
	inputHeight := inputLines + 2
	if inputHeight > 8 {
		inputHeight = 8
	}

	chatBoxHeight := height - inputHeight - typingHeight - suggestionsHeight - replyBarHeight - mentionsHeight - 1
	if chatBoxHeight < 1 {
		chatBoxHeight = 1
	}
	chatH := borderedContentHeight(chatBoxHeight)
	if chatH < 1 {
		chatH = 1
	}
	chatW := panelContentWidth(width)

	vp := ViewportModel{
		width:       chatW,
		height:      chatH,
		offset:      m.scrollOffset,
		messages:    m.msgs,
		loading:     m.loadingHistory,
		allLoaded:   m.allHistoryLoaded,
		myUsername:  m.username,
		selectMode:  m.replySelectMode || m.editSelectMode,
		selectedIdx: m.activeSelectedIndex(),
	}
	chatContent := vp.View()

	chatBox := renderBorderedBox(panelStyle(), width, chatBoxHeight, chatContent)

	inputBox := m.input.ViewHeight(inputHeight)

	parts := []string{title, chatBox}
	if typingLine != "" {
		parts = append(parts, typingLine)
	}
	if commandSuggestions != "" {
		parts = append(parts, commandSuggestions)
	}
	if replyBar != "" {
		parts = append(parts, replyBar)
	}
	if mentionSuggestions != "" {
		parts = append(parts, mentionSuggestions)
	}
	parts = append(parts, inputBox)

	return fitToSize(strings.Join(parts, "\n"), width, height)
}

func (m Model) renderCommandSuggestions(width int) string {
	value := m.input.Value()
	if m.isJoinInput() {
		_, word := splitJoinInput(value)
		needle := strings.TrimPrefix(word, "#")
		var lines []string
		for _, ch := range m.channels {
			if strings.HasPrefix(strings.ToLower(ch), strings.ToLower(needle)) {
				lines = append(lines, fmt.Sprintf("#%s", ch))
			}
		}
		if len(lines) == 0 {
			lines = append(lines, "no matching channels")
		}
		return renderBorderedBox(commandSuggestionStyle(), width, lipgloss.Height(strings.Join(lines, "\n"))+2, "channels: "+strings.Join(lines, "  "))
	}

	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		return ""
	}
	prefix := strings.TrimPrefix(value, "/")
	candidates := m.commandCandidates(prefix)
	if len(candidates) == 0 {
		return ""
	}

	lines := make([]string, 0, len(candidates))
	for _, c := range candidates {
		lines = append(lines, fmt.Sprintf("/%s - %s", c.Name(), c.Description()))
	}
	content := strings.Join(lines, "\n")
	return renderBorderedBox(commandSuggestionStyle(), width, lipgloss.Height(content)+2, content)
}

func (m Model) renderMentionSuggestions(width int) string {
	completions := m.input.completions
	if len(completions) == 0 {
		return ""
	}
	_, compIdx := m.input.CurrentCompletion()
	displayW := width - 4
	if displayW < 20 {
		displayW = 20
	}
	var items []string
	for i, c := range completions {
		highlighted := i == compIdx
		items = append(items, autoCompleteItemStyle(highlighted).Render(c))
	}
	line := strings.Join(items, " ")
	if lipgloss.Width(line) > displayW {
		line = line[:displayW] + "…"
	}
	return autoCompleteStyle().Width(width).MaxWidth(width).Render(line)
}

func (m Model) renderTypingIndicator() string {
	if len(m.typingUsers) == 0 {
		return ""
	}
	var names []string
	for u := range m.typingUsers {
		names = append(names, u)
	}
	sort.Strings(names)
	var text string
	switch len(names) {
	case 1:
		text = fmt.Sprintf(" %s is typing...", names[0])
	case 2:
		text = fmt.Sprintf(" %s and %s are typing...", names[0], names[1])
	default:
		text = fmt.Sprintf(" %s and others are typing...", names[0])
	}
	return typingStyle().Render(text)
}
