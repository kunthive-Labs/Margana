package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/model"
)

type setupStep int

const (
	setupIdle setupStep = iota
	setupFetching
	setupPicking
	setupConfirming
	setupDone
)

type setupGuild struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Owner       bool   `json:"owner"`
	Permissions string `json:"permissions"`
}

type setupGuildsMsg struct {
	Guilds []setupGuild
	Err    error
}

func (m *Model) openSetupWizard() (tea.Model, tea.Cmd) {
	if m.setupStep != setupIdle {
		return m, nil
	}
	m.setupStep = setupFetching
	m.setupGuilds = nil
	m.setupSelectedIdx = 0
	m.setupErr = ""

	if m.discordAccessToken == "" {
		m.setupErr = "Discord access token not available. Restart Marga to re-authenticate."
		m.setupStep = setupDone
		return m, nil
	}

	return m, func() tea.Msg {
		guilds, err := fetchDiscordGuilds(m.discordAccessToken)
		if err != nil {
			return setupGuildsMsg{Err: err}
		}
		adminGuilds := filterAdminGuilds(guilds)
		return setupGuildsMsg{Guilds: adminGuilds}
	}
}

func fetchDiscordGuilds(accessToken string) ([]setupGuild, error) {
	req, err := http.NewRequest("GET", "https://discord.com/api/v10/users/@me/guilds", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting guilds: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("guilds request failed: %s: %s", resp.Status, string(body))
	}

	var guilds []setupGuild
	if err := json.NewDecoder(resp.Body).Decode(&guilds); err != nil {
		return nil, fmt.Errorf("decoding guilds: %w", err)
	}
	return guilds, nil
}

const (
	setupPermAdmin      = 0x8
	setupPermManageGuild = 0x20
)

func filterAdminGuilds(guilds []setupGuild) []setupGuild {
	var filtered []setupGuild
	for _, g := range guilds {
		if g.Owner {
			filtered = append(filtered, g)
			continue
		}
		var perms int64
		fmt.Sscanf(g.Permissions, "%d", &perms)
		if (perms & setupPermAdmin) != 0 || (perms & setupPermManageGuild) != 0 {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

func (m *Model) handleSetupGuilds(msg setupGuildsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setupErr = fmt.Sprintf("Failed to fetch servers: %v", msg.Err)
		m.setupStep = setupDone
		return m, nil
	}
	if len(msg.Guilds) == 0 {
		m.setupErr = "No servers found where you have admin permissions."
		m.setupStep = setupDone
		return m, nil
	}
	m.setupGuilds = msg.Guilds
	m.setupStep = setupPicking
	m.setupSelectedIdx = 0
	return m, nil
}

func (m *Model) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.setupStep = setupIdle
		return m, nil

	case "up":
		if m.setupStep == setupPicking && m.setupSelectedIdx > 0 {
			m.setupSelectedIdx--
		}
		return m, nil

	case "down":
		if m.setupStep == setupPicking && m.setupSelectedIdx < len(m.setupGuilds)-1 {
			m.setupSelectedIdx++
		}
		return m, nil

	case "enter":
		switch m.setupStep {
		case setupPicking:
			if m.setupSelectedIdx >= 0 && m.setupSelectedIdx < len(m.setupGuilds) {
				m.setupStep = setupConfirming
			}
		case setupConfirming:
			return m.confirmSetupGuild()
		case setupDone:
			m.setupStep = setupIdle
		}
		return m, nil

	case "y", "Y":
		if m.setupStep == setupConfirming {
			return m.confirmSetupGuild()
		}
	case "n", "N":
		if m.setupStep == setupConfirming {
			m.setupStep = setupPicking
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) confirmSetupGuild() (tea.Model, tea.Cmd) {
	if m.setupSelectedIdx < 0 || m.setupSelectedIdx >= len(m.setupGuilds) {
		return m, nil
	}
	g := m.setupGuilds[m.setupSelectedIdx]

	entry := config.GuildEntry{
		ID:         g.ID,
		Name:       g.Name,
		Channel:    m.channel,
		Configured: true,
	}

	found := false
	for i, eg := range m.configuredGuilds {
		if eg.ID == entry.ID {
			m.configuredGuilds[i] = entry
			found = true
			break
		}
	}
	if !found {
		m.configuredGuilds = append(m.configuredGuilds, entry)
	}

	if m.setupCfg != nil {
		m.setupCfg.ConfiguredGuilds = m.configuredGuilds
		if m.setupCfg.General.GuildID == "" {
			m.setupCfg.General.GuildID = entry.ID
			m.setupCfg.General.GuildName = entry.Name
		}
		if m.setupConfigPath != "" {
			_ = m.setupCfg.Save(m.setupConfigPath)
		}
	}

	botClientID := m.discordClientID
	inviteURL := fmt.Sprintf(
		"https://discord.com/oauth2/authorize?client_id=%s&permissions=536988672&scope=bot&guild_id=%s",
		botClientID, g.ID,
	)

	sysMsg := model.Message{
		ID:        fmt.Sprintf("sys-setup-%d", time.Now().UnixNano()),
		Username:  "system",
		Content:   fmt.Sprintf("Server configured: %s\n\nBot invite link:\n%s\n\nInvite the bot, then restart Marga to connect.", g.Name, inviteURL),
		Timestamp: time.Now(),
	}
	m.msgs = append(m.msgs, sysMsg)
	m.scrollOffset = 0

	m.setupStep = setupIdle
	return m, nil
}

func (m Model) renderSetupWizard(width, height int) string {
	if m.setupStep == setupIdle {
		return ""
	}

	innerW := borderedStyleWidth(width)
	innerH := borderedStyleHeight(height)

	switch m.setupStep {
	case setupFetching:
		title := panelTitleStyle().Render(" Connect Server ")
		content := lipgloss.NewStyle().Foreground(themeDim).Padding(1).Render("Fetching servers you manage...")
		box := renderBorderedBox(panelStyle(), width, height, content)
		return title + "\n" + box

	case setupDone:
		title := panelTitleStyle().Render(" Error ")
		content := lipgloss.NewStyle().Foreground(themeErr).Padding(1).Render(m.setupErr)
		box := renderBorderedBox(panelStyle(), width, height, content)
		return title + "\n" + box

	case setupConfirming:
		if m.setupSelectedIdx >= 0 && m.setupSelectedIdx < len(m.setupGuilds) {
			return m.renderSetupConfirm(width, height)
		}

	case setupPicking:
		return m.renderSetupPicker(width, height, innerW, innerH)
	}

	return ""
}

func (m Model) renderSetupPicker(width, height, innerW, innerH int) string {
	title := panelTitleStyle().Render(" Connect Server ")

	intro := lipgloss.NewStyle().
		Foreground(themeDim).
		Padding(0, 1).
		Render("Select a Discord server to connect:")

	list := m.renderSetupGuildList(innerW)
	hint := lipgloss.NewStyle().
		Foreground(themeAccentDim).
		Padding(1, 1, 0, 1).
		Render(m.setupHint())

	boxContent := intro + "\n" + list + "\n" + hint
	boxContent = clipLines(boxContent, innerH)
	box := renderBorderedBox(panelStyle(), width, height, boxContent)

	return title + "\n" + box
}

func (m Model) renderSetupConfirm(width, height int) string {
	g := m.setupGuilds[m.setupSelectedIdx]

	confirmW := width - 12
	confirmH := 8
	if confirmW < 32 {
		confirmW = 32
	}

	title := panelTitleStyle().Render(" Confirm ")
	nameLine := lipgloss.NewStyle().
		Foreground(themeAccent).
		Bold(true).
		Render(g.Name)
	body := lipgloss.NewStyle().
		Foreground(themeFg).
		Padding(0, 1).
		Render("Add this server to Marga?\n\n" + nameLine)

	yesBtn := lipgloss.NewStyle().
		Foreground(themeAccent).
		Bold(true).
		Render("[y] yes")
	noBtn := lipgloss.NewStyle().
		Foreground(themeDim).
		Render("[n] no")
	buttons := lipgloss.NewStyle().
		PaddingTop(1).
		Render(yesBtn + "    " + noBtn)

	confirmContent := body + "\n" + buttons
	confirmBox := renderBorderedBox(
		lipgloss.NewStyle().
			BorderForeground(themeBorder).
			BorderStyle(lipgloss.RoundedBorder()).
			Padding(1, 2),
		confirmW, confirmH,
		confirmContent,
	)
	centered := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, confirmBox)
	return title + "\n" + centered
}

func (m Model) renderSetupGuildList(width int) string {
	if m.setupStep != setupPicking {
		return ""
	}
	if len(m.setupGuilds) == 0 {
		return lipgloss.NewStyle().Foreground(themeDim).Padding(1).Render("No servers found.")
	}

	names := make([]string, len(m.setupGuilds))
	for i, g := range m.setupGuilds {
		names[i] = g.Name
	}
	sort.Strings(names)

	nameToIdx := make(map[string]int)
	for i, g := range m.setupGuilds {
		nameToIdx[g.Name] = i
	}

	var lines []string
	for _, name := range names {
		idx := nameToIdx[name]
		g := m.setupGuilds[idx]
		num := idx + 1

		var line string
		if idx == m.setupSelectedIdx {
			arrow := lipgloss.NewStyle().Foreground(themeAccent).Bold(true).Render("> ")
			nameStyled := lipgloss.NewStyle().Foreground(themeAccent).Bold(true).Render(g.Name)
			line = arrow + nameStyled
		} else {
			numStr := lipgloss.NewStyle().Foreground(themeDim).Render(fmt.Sprintf("%2d", num))
			nameStyled := lipgloss.NewStyle().Foreground(themeFg).Render(g.Name)
			line = numStr + ". " + nameStyled
		}

		if g.Owner {
			ownerTag := lipgloss.NewStyle().Foreground(themeWarn).Render(" owner")
			line = line + ownerTag
		}

		lines = append(lines, lipgloss.NewStyle().PaddingLeft(1).Render(line))
	}

	return strings.Join(lines, "\n")
}

func (m Model) setupHint() string {
	switch m.setupStep {
	case setupFetching:
		return "Fetching your Discord servers..."
	case setupPicking:
		return "↑↓ navigate · enter select · esc/ctrl+c close"
	case setupConfirming:
		return "y confirm · n cancel · esc/ctrl+c close"
	case setupDone:
		return "enter or esc/ctrl+c to close"
	}
	return ""
}

func (m Model) isSetupVisible() bool {
	return m.setupStep != setupIdle
}
