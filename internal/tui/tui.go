// Package tui implements Marga's terminal user interface using Bubble Tea. It
// consumes the unified network.Event stream from one or more network adapters
// and renders the chat view, channel/user sidebars, input, and slash-command
// output.
package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/db"
	"github.com/kunthive-Labs/Margana/internal/history"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
	"github.com/kunthive-Labs/Margana/internal/webhook"
)

// networkEventMsg carries one event from any network adapter into the
// bubbletea Update loop. Exactly one listenNetwork command is in flight per
// adapter at a time; each event handled re-arms that adapter's listener.
type networkEventMsg network.Event

type typingTickMsg struct{}

type dbWriteResultMsg struct {
	Err error
}

type localHistoryMsg struct {
	Messages []model.Message
	Channels []string
	Channel  string
	Err      error
}

type GithubActivityEvent struct {
	Type      string
	Repo      string
	Actor     string
	Title     string
	Timestamp time.Time
}

type githubActivityMsg struct {
	Events  []GithubActivityEvent
	Err     error
	Warning string
}

type trackedError struct {
	Timestamp time.Time
	Message   string
}

type periodicRefreshMsg struct{}

type Model struct {
	width  int
	height int

	adapters map[network.NetworkID]network.Network
	active   network.NetworkID
	store    *db.Store
	registry *commands.Registry

	msgs       []model.Message
	status     network.ConnState
	channel    string
	username   string
	channels   []string
	available  map[string]struct{}
	channelsOK bool
	users      []string
	lastSendOk bool
	sendErr    string

	loadingHistory   bool
	historyLoaded    bool
	allHistoryLoaded bool

	scrollOffset int
	unreadCount  int
	input        InputModel
	log          *log.Logger

	channelsVisible bool
	usersVisible    bool
	notifVisible    bool

	typingUsers map[string]time.Time
	sentHashes  map[string]time.Time
	presences   map[string]model.UserPresence
	myStatus    string

	terminalOnline []string

	replyTo       *model.Message
	notifications []model.Notification
	notifIdx      int
	notifFocused  bool
	jumpToID      string

	replySelectMode bool
	replySelectIdx  int
	editSelectMode  bool
	editSelectIdx   int
	editingMessage  *model.Message

	discordID         string
	discordUsername   string
	discordGlobalName string
	guildName         string
	githubRepo        string
	githubToken       string
	githubEvents      []GithubActivityEvent
	githubLastFetch   time.Time

	errors         []trackedError
	errorsVisible  bool
	errorFocused   bool
	errorScrollIdx int
	errorScrollOff int

	helpVisible      bool
	configuredGuilds []config.GuildEntry

	setupStep          setupStep
	setupGuilds        []setupGuild
	setupSelectedIdx   int
	setupErr           string
	discordAccessToken string
	discordClientID    string
	setupConfigPath    string
	setupCfg           *config.Config
	version            string
}

func New(adapters []network.Network, active network.NetworkID, store *db.Store, registry *commands.Registry, channel, username, discordID, discordUsername, discordGlobalName, guildName string, configuredGuilds []config.GuildEntry, discordAccessToken, discordClientID string, setupConfigPath string, setupCfg *config.Config, version string) Model {
	channels := []string{channel}
	var notifications []model.Notification
	if store != nil {
		if storedChannels, err := store.GetChannels(); err == nil {
			channels = mergeChannels(channels, storedChannels)
		}
		_ = store.InsertChannel(channel)
		if storedNotifs, err := store.GetNotifications(); err == nil {
			notifications = storedNotifs
		}
	}

	adapterMap := make(map[network.NetworkID]network.Network, len(adapters))
	for _, a := range adapters {
		adapterMap[a.ID()] = a
	}
	if active == "" && len(adapters) > 0 {
		active = adapters[0].ID()
	}

	return Model{
		adapters:           adapterMap,
		active:             active,
		store:              store,
		registry:           registry,
		channel:            channel,
		username:           username,
		discordID:          discordID,
		discordUsername:    discordUsername,
		discordGlobalName:  discordGlobalName,
		guildName:          guildName,
		configuredGuilds:   configuredGuilds,
		discordAccessToken: discordAccessToken,
		discordClientID:    discordClientID,
		setupConfigPath:    setupConfigPath,
		setupCfg:           setupCfg,
		channels:           channels,
		available:          make(map[string]struct{}),
		status:             network.StateDisconnected,
		lastSendOk:         true,
		loadingHistory:     false,
		historyLoaded:      false,
		allHistoryLoaded:   false,
		input:              newInput("> "),
		channelsVisible:    true,
		usersVisible:       true,
		notifVisible:       true,
		notifications:      notifications,
		typingUsers:        make(map[string]time.Time),
		sentHashes:         make(map[string]time.Time),
		presences:          make(map[string]model.UserPresence),
		log:                log.New(io.Discard, "", 0),
		version:            version,
	}
}

type updateCheckMsg struct {
	latestVersion string
}

func (m Model) checkUpdates() tea.Cmd {
	return func() tea.Msg {
		if m.version == "dev" || m.version == "" {
			return nil
		}

		req, err := http.NewRequest("GET", "https://api.github.com/repos/kunthive-Labs/Margana/releases/latest", nil)
		if err != nil {
			return nil
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil
		}

		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return nil
		}

		return updateCheckMsg{latestVersion: release.TagName}
	}
}

func (m Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.adapters)+8)
	for id := range m.adapters {
		cmds = append(cmds, m.listenNetwork(id))
	}
	cmds = append(cmds,
		m.fetchChannelsCmd(),
		m.loadLocalHistory(m.channel, 100),
		m.initialFetchCmd(m.channel, 100),
		m.checkUpdates(),
		m.input.CursorBlinkCmd(),
		periodicRefreshCmd(),
		m.githubPollCmd(),
	)
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(m.chatWidth())
		return m, nil

	case setupGuildsMsg:
		return m.handleSetupGuilds(msg)

	case commands.SetupWizardMsg:
		return m.openSetupWizard()

	case commands.SetupRestartMsg:
		sysMsg := commands.SystemMsg("restarting in server setup mode — marga will reopen the setup wizard")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		m.closeAdapters()
		if m.setupConfigPath != "" {
			_ = os.WriteFile(m.setupConfigPath+".setup-flag", []byte("1"), 0o644)
		}
		return m, tea.Quit

	case commands.ServersMsg:
		sysMsg := commands.SystemMsg("exiting to server selection...")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		m.closeAdapters()
		if m.setupConfigPath != "" {
			_ = os.WriteFile(m.setupConfigPath+".servers-flag", []byte("1"), 0o644)
		}
		return m, tea.Quit

	case commands.GlobalDiscoverMsg:
		sysMsg := commands.SystemMsg("exiting to discover servers...")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		m.closeAdapters()
		if m.setupConfigPath != "" {
			_ = os.WriteFile(m.setupConfigPath+".global-flag", []byte("1"), 0o644)
		}
		return m, tea.Quit

	case updateCheckMsg:
		if msg.latestVersion != "" {
			local := strings.TrimPrefix(m.version, "v")
			remote := strings.TrimPrefix(msg.latestVersion, "v")
			if remote != local {
				notif := model.Notification{
					Channel:   "system",
					Username:  "update-available",
					Content:   fmt.Sprintf("Marga %s is available (you have v%s). run your package manager to update.", msg.latestVersion, local),
					Timestamp: time.Now(),
				}
				if m.store != nil {
					_ = m.store.InsertNotification(notif)
				}
				m.notifications = append([]model.Notification{notif}, m.notifications...)
				m.notifVisible = true
			}
		}
		return m, nil

	case networkEventMsg:
		ev := network.Event(msg)
		fromActive := ev.Network == m.active
		var cmds []tea.Cmd
		switch ev.Kind {
		case network.EventMessage:
			if ev.Message != nil {
				em := *ev.Message
				if em.Network == "" {
					em.Network = string(ev.Network)
				}
				m, cmds = m.onMessageEvent(em, fromActive)
			}
		case network.EventStatus:
			// Status reflects whichever network most recently changed state.
			m, cmds = m.onStatusEvent(ev.State, ev.Err)
		case network.EventTyping:
			// Typing and presence are scoped to the visible channel, so only the
			// active network's indicators are applied.
			if fromActive && ev.Typing != nil {
				m, cmds = m.onTypingEvent(*ev.Typing)
			}
		case network.EventPresence:
			if fromActive && ev.Presence != nil {
				m, cmds = m.onPresenceEvent(*ev.Presence)
			}
		case network.EventPresentUsers:
			if fromActive {
				m, cmds = m.onPresentUsers(ev.Users)
			}
		case network.EventChannelList:
			// reserved: adapters that push topology updates land here
		}
		cmds = append(cmds, m.listenNetwork(ev.Network))
		return m, tea.Batch(cmds...)

	case typingTickMsg:
		now := time.Now()
		for user, lastSeen := range m.typingUsers {
			if now.Sub(lastSeen) > 3*time.Second {
				delete(m.typingUsers, user)
			}
		}
		for h, ts := range m.sentHashes {
			if now.Sub(ts) > 10*time.Second {
				delete(m.sentHashes, h)
			}
		}
		var cmds []tea.Cmd
		if len(m.typingUsers) > 0 {
			cmds = append(cmds, typingTickCmd())
		}
		return m, tea.Batch(cmds...)

	case history.ChannelsResultMsg:
		if msg.Err != nil {
			m.log.Printf("channels: %v", msg.Err)
			m.addError(fmt.Sprintf("channels: %v", msg.Err))
			m.channelsOK = true // allow /join even if channels couldn't load
			return m, nil
		}
		m.channelsOK = true
		if len(msg.Channels) == 0 {
			m.channels = nil
			m.available = make(map[string]struct{})
			if m.store != nil {
				_ = m.store.ReplaceChannels(nil)
			}
			return m, nil
		}
		m.channels = mergeChannels(nil, msg.Channels)
		m.available = channelsToSet(m.channels)
		if m.store != nil {
			_ = m.store.ReplaceChannels(m.channels)
		}
		if _, ok := m.available[m.channel]; !ok {
			oldChannel := m.channel
			m.channel = m.channels[0]
			m.msgs = nil
			m.scrollOffset = 0
			m.allHistoryLoaded = false
			m.historyLoaded = false
			m.replyTo = nil

			sysMsg := commands.SystemMsg(fmt.Sprintf("switched to #%s", m.channel))
			m.msgs = append(m.msgs, sysMsg)

			cmds = append(cmds, m.subscribeSwitchCmd(oldChannel, m.channel))
			cmds = append(cmds, m.loadLocalHistory(m.channel, 100))
			cmds = append(cmds, m.initialFetchCmd(m.channel, 100))
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case history.FetchResultMsg:
		if msg.Channel != "" && msg.Channel != m.channel {
			return m, nil
		}
		m.loadingHistory = false
		if msg.Err != nil {
			m.log.Printf("history: %v", msg.Err)
			m.addError(fmt.Sprintf("history: %v", msg.Err))
			return m, nil
		}
		if len(msg.Messages) == 0 {
			m.allHistoryLoaded = true
			return m, nil
		}
		if len(msg.Messages) < 100 {
			m.allHistoryLoaded = true
		}
		m.msgs = mergeMessages(m.msgs, msg.Messages)
		m.historyLoaded = true
		if m.jumpToID != "" && m.jumpToMessage(m.jumpToID) {
			m.jumpToID = ""
		}
		for _, msg := range msg.Messages {
			cmds = append(cmds, m.persistMessage(msg))
		}
		m.users = msgsToUsers(m.msgs)
		return m, tea.Batch(cmds...)

	case localHistoryMsg:
		if msg.Channel != "" && msg.Channel != m.channel {
			return m, nil
		}
		if msg.Err != nil {
			m.log.Printf("local history: %v", msg.Err)
			m.addError(fmt.Sprintf("local history: %v", msg.Err))
			return m, nil
		}
		if len(m.channels) == 0 {
			m.channels = mergeChannels([]string{m.channel}, msg.Channels)
		}
		if len(msg.Messages) > 0 {
			m.msgs = mergeMessages(m.msgs, msg.Messages)
			m.users = msgsToUsers(m.msgs)
			m.historyLoaded = true
			if m.jumpToID != "" && m.jumpToMessage(m.jumpToID) {
				m.jumpToID = ""
			}
		}
		return m, nil

	case webhook.SendResultMsg:
		if msg.Err != nil {
			m.lastSendOk = false
			m.sendErr = msg.Err.Error()
			m.addError(fmt.Sprintf("send: %v", msg.Err))
		} else {
			m.lastSendOk = true
			m.sendErr = ""
		}
		return m, nil

	case webhook.SendFileResultMsg:
		if msg.Err != nil {
			m.lastSendOk = false
			m.sendErr = msg.Err.Error()
			m.addError(fmt.Sprintf("file send: %v", msg.Err))
		} else {
			m.lastSendOk = true
			m.sendErr = ""
		}
		return m, nil

	case webhook.EditResultMsg:
		if msg.Err != nil {
			m.lastSendOk = false
			m.sendErr = msg.Err.Error()
			m.addError(fmt.Sprintf("edit: %v", msg.Err))
			if m.editingMessage != nil {
				m.input.SetValue(msg.Content)
			}
			return m, nil
		}
		m.lastSendOk = true
		m.sendErr = ""
		m.editingMessage = nil
		updated := model.Message{
			ID:        msg.MessageID,
			Username:  m.username,
			Content:   msg.Content,
			Channel:   m.channel,
			Timestamp: time.Now(),
		}
		if applied := m.applyMessageUpdate(updated); applied != nil {
			return m, m.persistMessageUpdate(*applied)
		}
		return m, nil

	case dbWriteResultMsg:
		if msg.Err != nil {
			m.log.Printf("db: %v", msg.Err)
			m.addError(fmt.Sprintf("db: %v", msg.Err))
		}
		return m, nil

	case commands.CommandOutputMsg:
		m.msgs = append(m.msgs, msg.Messages...)
		if len(m.msgs) > 1000 {
			m.msgs = m.msgs[len(m.msgs)-1000:]
		}
		m.scrollOffset = 0
		return m, nil

	case commands.SwitchChannelMsg:
		oldChannel := m.channel
		m.channel = msg.Channel

		m.addChannel(msg.Channel)

		m.msgs = nil
		m.scrollOffset = 0
		m.allHistoryLoaded = false
		m.historyLoaded = false
		m.replyTo = nil

		sysMsg := commands.SystemMsg(fmt.Sprintf("switched to #%s", msg.Channel))
		m.msgs = append(m.msgs, sysMsg)

		cmds = append(cmds, m.subscribeSwitchCmd(oldChannel, msg.Channel))
		cmds = append(cmds, m.loadLocalHistory(msg.Channel, 100))
		cmds = append(cmds, m.initialFetchCmd(msg.Channel, 100))

		return m, tea.Batch(cmds...)

	case commands.SwitchNetworkMsg:
		return m.switchNetwork(network.NetworkID(msg.Network))

	case commands.DeleteChannelMsg:
		chName := msg.Channel
		if len(m.channels) <= 1 {
			sysMsg := commands.SystemMsg("cannot leave — you must be in at least one channel")
			m.msgs = append(m.msgs, sysMsg)
			m.scrollOffset = 0
			return m, nil
		}
		found := false
		for _, ch := range m.channels {
			if ch == chName {
				found = true
				break
			}
		}
		if !found {
			sysMsg := commands.SystemMsg(fmt.Sprintf("channel #%s not found in your list", chName))
			m.msgs = append(m.msgs, sysMsg)
			m.scrollOffset = 0
			return m, nil
		}
		m.removeChannel(chName)
		if m.store != nil {
			_ = m.store.DeleteChannel(chName)
		}
		if m.channel == chName {
			newChannel := m.channels[0]
			m.channel = newChannel
			m.msgs = nil
			m.scrollOffset = 0
			m.allHistoryLoaded = false
			m.historyLoaded = false
			sysMsg := commands.SystemMsg(fmt.Sprintf("left #%s, switched to #%s", chName, newChannel))
			m.msgs = append(m.msgs, sysMsg)
			cmds = append(cmds, m.subscribeSwitchCmd(chName, newChannel))
			cmds = append(cmds, m.loadLocalHistory(newChannel, 100))
			cmds = append(cmds, m.initialFetchCmd(newChannel, 100))
		} else {
			sysMsg := commands.SystemMsg(fmt.Sprintf("removed #%s from channels", chName))
			m.msgs = append(m.msgs, sysMsg)
			m.scrollOffset = 0
		}
		return m, tea.Batch(cmds...)

	case commands.TriggerHistoryLoadMsg:
		if m.loadingHistory || m.allHistoryLoaded || len(m.msgs) == 0 {
			return m, nil
		}
		m.loadingHistory = true
		oldest := m.msgs[0].Timestamp
		return m, m.loadOlderCmd(m.channel, oldest)

	case commands.SendRawMsg:
		return m, m.SendMessage(msg.Content, m.channel, "")

	case commands.SendFileMsg:
		return m.sendFileWithEcho(msg.Path, msg.Content)

	case commands.EditMessageMsg:
		return m.handleEditCommand(msg)

	case commands.StartEditMsg:
		return m.startEdit(msg.Target)

	case commands.OpenImageMsg:
		return m.openImage(msg.Index)

	case commands.LogoutMsg:
		sysMsg := commands.SystemMsg("logged out — restart marga to re-authenticate")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		m.closeAdapters()
		return m, tea.Quit

	case commands.SetStatusMsg:
		m.myStatus = msg.Status
		now := time.Now()
		p := model.UserPresence{
			Username:  m.username,
			Status:    msg.Status,
			Online:    true,
			LastSeen:  now,
			UpdatedAt: now,
		}
		m.presences[m.username] = p
		var sysContent string
		if msg.Status == "" {
			sysContent = "status cleared"
		} else {
			sysContent = fmt.Sprintf("status set: %s", msg.Status)
		}
		sysMsg := commands.SystemMsg(sysContent)
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		a := m.activeAdapter()
		store := m.store
		return m, func() tea.Msg {
			if a != nil {
				_ = a.SetStatus(msg.Status)
			}
			if store != nil {
				_ = store.UpsertPresence(p)
			}
			return nil
		}

	case commands.ClearNotificationsMsg:
		m.notifications = nil
		m.notifIdx = 0
		if m.store != nil {
			go func() { _ = m.store.ClearNotifications() }()
		}
		sysMsg := commands.SystemMsg("mentions cleared")
		m.msgs = append(m.msgs, sysMsg)
		m.scrollOffset = 0
		return m, nil

	case periodicRefreshMsg:
		if m.historyLoaded && len(m.msgs) > 0 {
			return m, tea.Batch(periodicRefreshCmd(), m.fetchSinceCmd(m.channel, m.msgs[len(m.msgs)-1].Timestamp))
		}
		return m, periodicRefreshCmd()

	case githubActivityMsg:
		if msg.Err == nil {
			m.githubEvents = msg.Events
			m.githubLastFetch = time.Now()
		} else {
			m.addError(fmt.Sprintf("github: %v", msg.Err))
		}
		if msg.Warning != "" {
			m.addError(fmt.Sprintf("github: %s", msg.Warning))
		}
		return m, m.githubPollCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	if inputCmd != nil {
		cmds = append(cmds, inputCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isSetupVisible() {
		return m.handleSetupKey(msg)
	}

	// Handle Alt+R or Ctrl+G: enter reply select mode
	altR := msg.Alt && len(msg.Runes) == 1 && (msg.Runes[0] == 'r' || msg.Runes[0] == 'R')
	altE := msg.Alt && len(msg.Runes) == 1 && (msg.Runes[0] == 'e' || msg.Runes[0] == 'E')
	isCtrlG := msg.String() == "ctrl+g"
	isCtrlE := msg.String() == "ctrl+e"
	if altR || isCtrlG {
		if m.replySelectMode || m.editSelectMode {
			return m, nil
		}
		return m.startReplyPicker()
	}
	if altE || isCtrlE {
		if m.replySelectMode || m.editSelectMode {
			return m, nil
		}
		return m.startEdit("")
	}

	switch msg.String() {
	case "ctrl+s":
		if !m.isSetupVisible() {
			return m.openSetupWizard()
		}
		return m, nil

	case "ctrl+h":
		m.helpVisible = !m.helpVisible
		return m, nil

	case "ctrl+o":
		m.errorsVisible = !m.errorsVisible
		if m.errorsVisible {
			m.errorFocused = true
			m.errorScrollIdx = len(m.errors) - 1
			if m.errorScrollIdx < 0 {
				m.errorScrollIdx = 0
			}
			m.errorScrollOff = 0
		} else {
			m.errorFocused = false
		}
		return m, nil

	case "ctrl+v":
		if m.activeAdapter() != nil && m.channel != "" {
			if path := readClipboardImage(); path != "" {
				content := fmt.Sprintf("attached `%s`", filepath.Base(path))
				return m.sendFileWithEcho(path, content)
			}
		}

	case "ctrl+c":
		m.closeAdapters()
		return m, tea.Quit

	case "ctrl+q":
		m.closeAdapters()
		return m, tea.Quit

	case "esc":
		if m.helpVisible {
			m.helpVisible = false
			return m, nil
		}
		if m.errorsVisible && m.errorFocused {
			m.errorsVisible = false
			m.errorFocused = false
			return m, nil
		}
		if m.replySelectMode {
			m.replySelectMode = false
			m.replySelectIdx = -1
			return m, nil
		}
		if m.editSelectMode {
			m.editSelectMode = false
			m.editSelectIdx = -1
			return m, nil
		}
		if m.replyTo != nil {
			m.replyTo = nil
			return m, nil
		}
		if m.editingMessage != nil {
			m.editingMessage = nil
			m.input.Clear()
			return m, nil
		}
		if m.input.Value() != "" {
			m.input.Clear()
		}
		return m, nil

	case "ctrl+b":
		m.channelsVisible = !m.channelsVisible
		return m, nil

	case "ctrl+y":
		m.usersVisible = !m.usersVisible
		return m, nil

	case "ctrl+l":
		if m.errorsVisible && m.errorFocused {
			return m, nil
		}
		m.scrollOffset = 0
		m.unreadCount = 0
		return m, nil

	case "ctrl+p":
		m.prevChannel()
		m.replyTo = nil
		return m, m.subscribeCmd(m.channel)

	case "ctrl+n":
		m.nextChannel()
		m.replyTo = nil
		return m, m.subscribeCmd(m.channel)

	case "ctrl+t":
		return m.cycleNetwork()

	case "ctrl+r":
		// Reply to most recent non-system message
		for i := len(m.msgs) - 1; i >= 0; i-- {
			if m.msgs[i].Username != "system" {
				m.replyTo = &m.msgs[i]
				break
			}
		}
		return m, nil

	case "ctrl+]":
		m.notifVisible = true
		if !m.notifFocused {
			m.notifFocused = true
		} else if len(m.notifications) > 0 {
			m.notifIdx = (m.notifIdx + 1) % len(m.notifications)
		}
		return m, nil

	case "up":
		if m.errorsVisible && m.errorFocused {
			if m.errorScrollIdx < len(m.errors)-1 {
				m.errorScrollIdx++
			}
			m.errorScrollOff = 0
			return m, nil
		}
		if m.replySelectMode {
			m.moveReplySelectUp()
			return m, nil
		}
		if m.editSelectMode {
			m.moveEditSelectUp()
			return m, nil
		}
		if m.notifFocused {
			if m.notifIdx > 0 {
				m.notifIdx--
			}
			return m, nil
		}
		m.scrollOffset++
		if m.scrollOffset > len(m.msgs)-1 {
			m.scrollOffset = len(m.msgs) - 1
		}
		return m, m.loadOlderIfNeeded()

	case "down":
		if m.errorsVisible && m.errorFocused {
			if m.errorScrollIdx > 0 {
				m.errorScrollIdx--
			}
			m.errorScrollOff = 0
			return m, nil
		}
		if m.replySelectMode {
			m.moveReplySelectDown()
			return m, nil
		}
		if m.editSelectMode {
			m.moveEditSelectDown()
			return m, nil
		}
		if m.notifFocused {
			if m.notifIdx < len(m.notifications)-1 {
				m.notifIdx++
			}
			return m, nil
		}
		m.scrollOffset--
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
			m.unreadCount = 0
		}
		return m, nil

	case "pgup":
		if m.errorsVisible && m.errorFocused {
			m.errorScrollIdx += 5
			if m.errorScrollIdx >= len(m.errors) {
				m.errorScrollIdx = len(m.errors) - 1
			}
			m.errorScrollOff = 0
			return m, nil
		}
		m.scrollOffset += m.chatHeight() / 2
		if m.scrollOffset > len(m.msgs)-1 {
			m.scrollOffset = len(m.msgs) - 1
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		return m, m.loadOlderIfNeeded()

	case "pgdown":
		if m.errorsVisible && m.errorFocused {
			m.errorScrollIdx -= 5
			if m.errorScrollIdx < 0 {
				m.errorScrollIdx = 0
			}
			m.errorScrollOff = 0
			return m, nil
		}
		m.scrollOffset -= m.chatHeight() / 2
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
			m.unreadCount = 0
		}
		return m, nil

	case "enter":
		if m.helpVisible {
			m.helpVisible = false
			return m, nil
		}
		if m.errorsVisible && m.errorFocused {
			m.errorsVisible = false
			m.errorFocused = false
			return m, nil
		}
		// Confirm reply selection
		if m.replySelectMode {
			if m.replySelectIdx >= 0 && m.replySelectIdx < len(m.msgs) && m.msgs[m.replySelectIdx].Username != "system" {
				msg := m.msgs[m.replySelectIdx]
				m.replyTo = &msg
			}
			m.replySelectMode = false
			m.replySelectIdx = -1
			return m, nil
		}
		if m.editSelectMode {
			return m.confirmEditSelection()
		}
		// Jump to notification's channel
		if m.notifFocused && len(m.notifications) > 0 && m.notifIdx < len(m.notifications) {
			n := m.notifications[m.notifIdx]
			if n.Channel != m.channel {
				m.jumpToID = n.MsgID
				return m, func() tea.Msg {
					return commands.SwitchChannelMsg{Channel: n.Channel}
				}
			}
			m.jumpToMessage(n.MsgID)
			return m, nil
		}

	case "tab":
		word, prefix := m.input.WordAtCursor()
		var candidates []string
		if m.isJoinInput() {
			needle := strings.TrimPrefix(word, "#")
			for _, ch := range m.channels {
				if strings.HasPrefix(strings.ToLower(ch), strings.ToLower(needle)) {
					candidates = append(candidates, "#"+ch)
				}
			}
		} else if m.isOpenInput() {
			// Autocomplete image indices for /open
			inputStr := strings.TrimSpace(string(m.input.text))
			parts := strings.Fields(inputStr)
			needle := ""
			if len(parts) > 1 {
				needle = parts[1]
			}
			var images []model.Attachment
			for i := len(m.msgs) - 1; i >= 0; i-- {
				for _, att := range m.msgs[i].Attachments {
					if isImageAttachment(att) {
						images = append(images, att)
					}
				}
			}
			for idx, img := range images {
				n := idx + 1
				candidate := fmt.Sprintf("%d", n)
				if needle == "" || strings.HasPrefix(candidate, needle) {
					candidates = append(candidates, fmt.Sprintf("%d (%s)", n, img.Filename))
				}
			}
		} else if m.isServersInput() {
			inputStr := strings.TrimSpace(string(m.input.text))
			parts := strings.Fields(inputStr)
			needle := ""
			if len(parts) > 1 {
				needle = strings.ToLower(parts[1])
			}
			for i, g := range m.configuredGuilds {
				numStr := fmt.Sprintf("%d", i+1)
				nameLower := strings.ToLower(g.Name)
				if needle == "" || strings.HasPrefix(numStr, needle) || strings.Contains(nameLower, needle) {
					prefix := "  "
					if m.setupCfg != nil && g.ID == m.setupCfg.General.GuildID {
						prefix = "* "
					}
					candidates = append(candidates, fmt.Sprintf("%d %s%s", i+1, prefix, g.Name))
				}
			}
		} else {
			switch prefix {
			case "/":
				for _, c := range m.commandCandidates(word) {
					candidates = append(candidates, "/"+c.Name())
				}
			case "@":
				allUsers := m.allKnownUsers()
				for _, u := range allUsers {
					if strings.HasPrefix(strings.ToLower(u), strings.ToLower(word)) {
						candidates = append(candidates, "@"+u)
					}
				}
			case "#":
				for _, ch := range m.channels {
					if strings.HasPrefix(strings.ToLower(ch), strings.ToLower(word)) {
						candidates = append(candidates, "#"+ch)
					}
				}
			default:
				if word != "" {
					allUsers := m.allKnownUsers()
					for _, u := range allUsers {
						if strings.HasPrefix(strings.ToLower(u), strings.ToLower(word)) {
							candidates = append(candidates, "@"+u)
						}
					}
				}
			}
		}
		if len(candidates) > 0 {
			if len(m.input.completions) == 0 {
				m.input.SetCompletions(candidates)
			}
			m.input.ApplyNextCompletion()
		}
		return m, nil
	}

	if m.replySelectMode || m.editSelectMode || (m.errorsVisible && m.errorFocused) {
		return m, nil
	}

	submitted, value := handleInputKey(msg, &m.input)
	if submitted {
		if strings.TrimSpace(value) == "" {
			return m, nil
		}
		val := strings.TrimRight(value, "\n")

		if strings.HasPrefix(strings.TrimSpace(val), "/") {
			cmdName, args := commands.ParseInput(strings.TrimSpace(val))
			if cmdName == "" {
				return m, nil
			}
			if cmdName == "join" {
				if err := m.validateJoin(args); err != nil {
					sysMsg := commands.SystemMsg(err.Error())
					m.msgs = append(m.msgs, sysMsg)
					m.scrollOffset = 0
					return m, nil
				}
			}
			cmd, err := m.registry.Execute(cmdName, args)
			if err != nil {
				sysMsg := commands.SystemMsg(fmt.Sprintf("error: /%s — %v", cmdName, err))
				m.msgs = append(m.msgs, sysMsg)
				m.scrollOffset = 0
				m.addError(fmt.Sprintf("command /%s: %v", cmdName, err))
				return m, nil
			}
			return m, cmd
		}

		if m.editingMessage != nil {
			target := m.editingMessage.ID
			return m, m.EditMessage(target, val)
		}
		return m.sendWithEcho(val)
	}

	m.maybeAutoComplete()

	return m, nil
}

func (m *Model) loadOlderIfNeeded() tea.Cmd {
	if !m.loadingHistory && !m.allHistoryLoaded && m.historyLoaded && len(m.msgs) > 0 {
		if m.scrollOffset >= len(m.msgs)-m.chatHeight() {
			oldest := m.msgs[0].Timestamp
			m.loadingHistory = true
			return m.loadOlderCmd(m.channel, oldest)
		}
	}
	return nil
}

// onlineUsers returns terminal-connected users, always including self.
func (m Model) onlineUsers() []string {
	seen := make(map[string]struct{})
	var users []string
	addUser := func(u string) {
		u = m.canonicalUserSuggestion(u)
		if u == "" {
			return
		}
		key := strings.ToLower(u)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		users = append(users, u)
	}
	addUser(m.username)
	for _, u := range m.terminalOnline {
		addUser(u)
	}
	return users
}

func (m *Model) moveReplySelectUp() {
	if len(m.msgs) == 0 {
		return
	}
	// Move to previous non-system message
	for i := m.replySelectIdx - 1; i >= 0; i-- {
		if m.msgs[i].Username != "system" {
			m.replySelectIdx = i
			m.ensureReplySelectVisible()
			return
		}
	}
}

func (m *Model) moveReplySelectDown() {
	if len(m.msgs) == 0 {
		return
	}
	// Move to next non-system message
	for i := m.replySelectIdx + 1; i < len(m.msgs); i++ {
		if m.msgs[i].Username != "system" {
			m.replySelectIdx = i
			m.ensureReplySelectVisible()
			return
		}
	}
}

func (m *Model) moveEditSelectUp() {
	if len(m.msgs) == 0 {
		return
	}
	for i := m.editSelectIdx - 1; i >= 0; i-- {
		if m.isOwnMessage(m.msgs[i]) && m.msgs[i].Username != "system" && !isLocalEchoID(m.msgs[i].ID) {
			m.editSelectIdx = i
			m.ensureEditSelectVisible()
			return
		}
	}
}

func (m *Model) moveEditSelectDown() {
	if len(m.msgs) == 0 {
		return
	}
	for i := m.editSelectIdx + 1; i < len(m.msgs); i++ {
		if m.isOwnMessage(m.msgs[i]) && m.msgs[i].Username != "system" && !isLocalEchoID(m.msgs[i].ID) {
			m.editSelectIdx = i
			m.ensureEditSelectVisible()
			return
		}
	}
}

func (m *Model) ensureReplySelectVisible() {
	if len(m.msgs) == 0 {
		return
	}
	total := len(m.msgs)
	fromBottom := total - 1 - m.replySelectIdx
	if fromBottom < 0 {
		fromBottom = 0
	}
	chatH := m.chatHeight()
	if chatH < 1 {
		chatH = 1
	}
	m.scrollOffset = clampInt(fromBottom-chatH/2, 0, total-1)
}

func (m *Model) ensureEditSelectVisible() {
	if len(m.msgs) == 0 {
		return
	}
	total := len(m.msgs)
	fromBottom := total - 1 - m.editSelectIdx
	if fromBottom < 0 {
		fromBottom = 0
	}
	chatH := m.chatHeight()
	if chatH < 1 {
		chatH = 1
	}
	m.scrollOffset = clampInt(fromBottom-chatH/2, 0, total-1)
}

func (m Model) activeSelectedIndex() int {
	if m.editSelectMode {
		return m.editSelectIdx
	}
	return m.replySelectIdx
}

func (m *Model) maybeAutoComplete() {
	word, prefix := m.input.WordAtCursor()
	if prefix != "@" && prefix != "#" {
		return
	}
	var candidates []string
	switch prefix {
	case "@":
		allUsers := m.allKnownUsers()
		for _, u := range allUsers {
			if strings.HasPrefix(strings.ToLower(u), strings.ToLower(word)) {
				candidates = append(candidates, "@"+u)
			}
		}
	case "#":
		for _, ch := range m.channels {
			if strings.HasPrefix(strings.ToLower(ch), strings.ToLower(word)) {
				candidates = append(candidates, "#"+ch)
			}
		}
	}
	if len(candidates) > 0 {
		m.input.SetCompletions(candidates)
	}
}

func (m Model) allKnownUsers() []string {
	seen := make(map[string]struct{})
	var users []string
	addUser := func(u string) {
		u = m.canonicalUserSuggestion(u)
		if u == "" {
			return
		}
		key := strings.ToLower(u)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		users = append(users, u)
	}
	for _, u := range m.onlineUsers() {
		addUser(u)
	}
	for _, u := range m.users {
		addUser(u)
	}
	return users
}

func (m Model) canonicalUserSuggestion(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	canonical := m.username
	if canonical == "" {
		canonical = m.discordUsername
	}
	if canonical == "" {
		return username
	}
	if strings.EqualFold(username, m.username) ||
		strings.EqualFold(username, m.discordUsername) ||
		strings.EqualFold(username, m.discordGlobalName) {
		return canonical
	}
	return username
}

func (m *Model) jumpToMessage(id string) bool {
	if id == "" {
		return false
	}
	for i, msg := range m.msgs {
		if msg.ID == id {
			fromBottom := len(m.msgs) - 1 - i
			if fromBottom < 0 {
				fromBottom = 0
			}
			m.scrollOffset = fromBottom
			m.unreadCount = 0
			return true
		}
	}
	return false
}

func (m *Model) addChannel(channel string) {
	channel = strings.TrimPrefix(strings.TrimSpace(channel), "#")
	if channel == "" {
		return
	}
	for _, ch := range m.channels {
		if ch == channel {
			return
		}
	}
	if m.channelsOK && m.setupCfg != nil && m.setupCfg.General.GuildID != "" {
		if m.available != nil {
			if _, ok := m.available[channel]; !ok {
				return
			}
		}
	}
	m.channels = append(m.channels, channel)
	sort.Strings(m.channels)
	if m.store != nil && m.channelsOK {
		_ = m.store.InsertChannel(channel)
	}
	if m.channelsOK {
		if m.available == nil {
			m.available = make(map[string]struct{})
		}
		m.available[channel] = struct{}{}
	}
}

func (m *Model) removeChannel(channel string) {
	var newChannels []string
	for _, ch := range m.channels {
		if ch != channel {
			newChannels = append(newChannels, ch)
		}
	}
	m.channels = newChannels
	delete(m.available, channel)
}

func (m Model) validateJoin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /join #channel")
	}
	channel := strings.TrimPrefix(strings.TrimSpace(args[0]), "#")
	if channel == "" {
		return fmt.Errorf("invalid channel name")
	}
	// Allow joining any channel; if relay doesn't know it, send will fail gracefully
	return nil
}

func (m Model) isJoinInput() bool {
	isJoin, _ := splitJoinInput(m.input.Value())
	return isJoin
}

func (m Model) isOpenInput() bool {
	val := strings.TrimSpace(m.input.Value())
	return strings.HasPrefix(val, "/open")
}

func (m Model) isServersInput() bool {
	val := strings.TrimSpace(m.input.Value())
	return strings.HasPrefix(val, "/servers")
}

func (m Model) commandCandidates(prefix string) []commands.Command {
	if m.registry == nil {
		return nil
	}
	prefix = strings.ToLower(prefix)
	var candidates []commands.Command
	for _, c := range m.registry.List() {
		if strings.HasPrefix(strings.ToLower(c.Name()), prefix) {
			candidates = append(candidates, c)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Name() < candidates[j].Name()
	})
	return candidates
}

func (m *Model) addError(msg string) {
	m.errors = append(m.errors, trackedError{
		Timestamp: time.Now(),
		Message:   msg,
	})
	if len(m.errors) > 100 {
		m.errors = m.errors[len(m.errors)-100:]
	}
}

func (m Model) chatWidth() int {
	w := m.width
	if m.channelsVisible {
		w -= m.channelSidebarWidth()
	}
	if rightWidth := m.rightSidebarWidth(); rightWidth > 0 {
		w -= rightWidth
	}
	if w < 20 {
		w = 20
	}
	return w
}

func (m Model) chatHeight() int {
	h := m.height - 1
	inputH := m.input.LineCount() + 2
	if inputH > 8 {
		inputH = 8
	}
	chatBoxH := h - inputH - 1
	chatH := borderedContentHeight(chatBoxH)
	if chatH < 1 {
		chatH = 1
	}
	return chatH
}

func (m Model) channelSidebarWidth() int {
	if !m.channelsVisible {
		return 0
	}
	if m.width < 72 {
		return 0
	}
	w := m.width * 18 / 100
	w = clampInt(w, 16, 28)
	return w
}

func (m Model) userSidebarWidth() int {
	if !m.usersVisible {
		return 0
	}
	if m.width < 96 {
		return 0
	}
	w := m.width * 20 / 100
	w = clampInt(w, 22, 34)
	return w
}

func (m Model) notifSidebarWidth() int {
	if !m.notifVisible {
		return 0
	}
	if m.width < 96 {
		return 0
	}
	w := m.width * 28 / 100
	w = clampInt(w, 28, 44)
	return w
}

func (m Model) rightSidebarWidth() int {
	if !m.notifVisible || m.width < 96 {
		return 0
	}
	w := m.width * 20 / 100
	return clampInt(w, 28, 38)
}

func (m *Model) nextChannel() {
	if len(m.channels) == 0 {
		return
	}
	idx := 0
	for i, ch := range m.channels {
		if ch == m.channel {
			idx = i
			break
		}
	}
	m.channel = m.channels[(idx+1)%len(m.channels)]
}

func (m *Model) prevChannel() {
	if len(m.channels) == 0 {
		return
	}
	idx := 0
	for i, ch := range m.channels {
		if ch == m.channel {
			idx = i
			break
		}
	}
	m.channel = m.channels[(idx-1+len(m.channels))%len(m.channels)]
}

func (m Model) WithGithub(repo, token string) Model {
	m.githubRepo = repo
	m.githubToken = token
	return m
}

// WithLogger directs the model's diagnostic output (history/channel/db errors)
// to l. By default the model discards all log output, since writing to the
// terminal would corrupt the TUI. Wired from cmd/marga when logging is enabled.
func (m Model) WithLogger(l *log.Logger) Model {
	if l != nil {
		m.log = l
	}
	return m
}

// githubPollCmd schedules a GitHub activity fetch if a repo is configured.
func centerInTerm(content string, modalW, modalH, termW, termH int) string {
	lines := strings.Split(content, "\n")

	leftPad := (termW - modalW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	topPad := (termH - modalH) / 2
	if topPad < 0 {
		topPad = 0
	}

	padding := strings.Repeat(" ", leftPad)

	var result strings.Builder
	for i := 0; i < topPad; i++ {
		result.WriteString("\n")
	}
	for _, line := range lines {
		result.WriteString(padding)
		result.WriteString(line)
		result.WriteString("\n")
	}
	remaining := termH - topPad - len(lines)
	for i := 0; i < remaining; i++ {
		result.WriteString("\n")
	}

	return lipgloss.NewStyle().
		Width(termW).Height(termH).
		Background(themeBg).
		Render(result.String())
}
