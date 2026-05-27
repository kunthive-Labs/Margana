package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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
					Content:   fmt.Sprintf("Margana %s is available (you have v%s). run your package manager to update.", msg.latestVersion, local),
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
		var cmds []tea.Cmd
		switch ev.Kind {
		case network.EventMessage:
			if ev.Message != nil {
				m, cmds = m.onMessageEvent(*ev.Message)
			}
		case network.EventStatus:
			m, cmds = m.onStatusEvent(ev.State, ev.Err)
		case network.EventTyping:
			if ev.Typing != nil {
				m, cmds = m.onTypingEvent(*ev.Typing)
			}
		case network.EventPresence:
			if ev.Presence != nil {
				m, cmds = m.onPresenceEvent(*ev.Presence)
			}
		case network.EventPresentUsers:
			m, cmds = m.onPresentUsers(ev.Users)
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
		for _, sysMsg := range msg.Messages {
			m.msgs = append(m.msgs, sysMsg)
		}
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

func (m Model) onMessageEvent(msg model.Message) (Model, []tea.Cmd) {
	m.addChannel(msg.Channel)

	if msg.EventType == "message_update" {
		updated := msg
		if applied := m.applyMessageUpdate(updated); applied != nil {
			m.users = msgsToUsers(m.msgs)
			updated = *applied
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
	}

	if msg.Channel != m.channel {
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

func (m Model) onStatusEvent(state network.ConnState, err error) (Model, []tea.Cmd) {
	m.status = state
	if err != nil {
		m.addError(fmt.Sprintf("connection: %v", err))
	}
	if state == network.StateConnected {
		m.errors = nil
		if m.channel != "" {
			return m, []tea.Cmd{m.subscribeCmd(m.channel)}
		}
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

// ref builds a ChannelRef on the active network for a channel name. For the
// relay, the channel name is also its native id.
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
func (m Model) WithGithub(repo, token string) Model {
	m.githubRepo = repo
	m.githubToken = token
	return m
}

// githubPollCmd schedules a GitHub activity fetch if a repo is configured.
func (m Model) githubPollCmd() tea.Cmd {
	if m.githubRepo == "" {
		return nil
	}
	return tea.Tick(60*time.Second, func(t time.Time) tea.Msg {
		return m.fetchGithubActivity()
	})
}

// fetchGithubActivity fetches recent events from the GitHub API.
func (m Model) fetchGithubActivity() githubActivityMsg {
	if m.githubRepo == "" {
		return githubActivityMsg{}
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/events", m.githubRepo)
	req, err := newGithubRequest(url, m.githubToken)
	if err != nil {
		return githubActivityMsg{Err: err}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return githubActivityMsg{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubActivityMsg{Err: describeGithubStatus(resp, m.githubRepo)}
	}
	warning := githubRateLimitWarning(resp)
	var raw []struct {
		Type  string `json:"type"`
		Actor struct {
			Login string `json:"login"`
		} `json:"actor"`
		Repo struct {
			Name string `json:"name"`
		} `json:"repo"`
		Payload struct {
			Action  string `json:"action"`
			Commits []struct {
				Message string `json:"message"`
			} `json:"commits"`
		} `json:"payload"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return githubActivityMsg{Err: err}
	}
	events := make([]GithubActivityEvent, 0, len(raw))
	for i, r := range raw {
		if i >= 10 {
			break
		}
		ts, _ := time.Parse(time.RFC3339, r.CreatedAt)
		title := r.Payload.Action
		if len(r.Payload.Commits) > 0 {
			title = r.Payload.Commits[0].Message
			if len(title) > 60 {
				title = title[:60] + "..."
			}
		}
		events = append(events, GithubActivityEvent{
			Type:      r.Type,
			Repo:      r.Repo.Name,
			Actor:     r.Actor.Login,
			Title:     title,
			Timestamp: ts,
		})
	}
	return githubActivityMsg{Events: events, Warning: warning}
}

// describeGithubStatus turns a non-200 GitHub API response into an actionable error.
func describeGithubStatus(resp *http.Response, repo string) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub: token invalid or expired")
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
				return fmt.Errorf("GitHub: rate limit exceeded (resets %s)", time.Unix(reset, 0).Local().Format("15:04:05"))
			}
			return fmt.Errorf("GitHub: rate limit exceeded")
		}
		return fmt.Errorf("GitHub: forbidden — check token scopes for %s", repo)
	case http.StatusNotFound:
		return fmt.Errorf("GitHub: repo %q not found or private", repo)
	default:
		return fmt.Errorf("github API: HTTP %d", resp.StatusCode)
	}
}

// githubRateLimitWarning returns a short warning if the remaining quota is low, else "".
func githubRateLimitWarning(resp *http.Response) string {
	remainingStr := resp.Header.Get("X-RateLimit-Remaining")
	if remainingStr == "" {
		return ""
	}
	remaining, err := strconv.Atoi(remainingStr)
	if err != nil || remaining >= 5 {
		return ""
	}
	if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
		return fmt.Sprintf("rate limit low (%d left, resets %s)", remaining, time.Unix(reset, 0).Local().Format("15:04:05"))
	}
	return fmt.Sprintf("rate limit low (%d left)", remaining)
}

// newGithubRequest builds an HTTP GET request with optional auth token.
func newGithubRequest(url, token string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// centerInTerm pads content (modalW x modalH) to full terminal dimensions
// using spaces. Avoids lipgloss.Place ANSI measurement issues.
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
