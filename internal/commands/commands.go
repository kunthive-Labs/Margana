// Package commands implements Marga's slash commands (/help, /join, /search,
// /network, …) and the registry that parses input and dispatches to them.
package commands

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/model"
)

type Command interface {
	Name() string
	Description() string
	Execute(args []string) (tea.Cmd, error)
}

type CommandOutputMsg struct {
	Messages []model.Message
}

type SwitchChannelMsg struct {
	Channel string
}

// SwitchNetworkMsg asks the TUI to make Network the active network. An empty
// Network means "list the available networks" instead of switching.
type SwitchNetworkMsg struct {
	Network string
}

type TriggerHistoryLoadMsg struct{}

type DeleteChannelMsg struct {
	Channel string
}

type SendRawMsg struct {
	Content string
}

// SetInputMsg replaces the input line's contents. Emitted by plugin effects
// (marga.set_input) and handled on the Update loop, since it mutates the TUI's
// input model.
type SetInputMsg struct {
	Value string
}

// NotifyMsg asks the TUI to show an OS desktop notification. Emitted by plugin
// effects (marga.notify); the TUI turns it into a notification command.
type NotifyMsg struct {
	Title string
	Body  string
}

type SendFileMsg struct {
	Path    string
	Content string
}

type EditMessageMsg struct {
	Target  string
	Content string
}

type StartEditMsg struct {
	Target string
}

// ReactMsg asks the TUI to add an emoji reaction to the latest message in the
// active channel. Emoji may be a unicode emoji or a :shortcode:.
type ReactMsg struct {
	Emoji string
}

type OpenImageMsg struct {
	Index int
}

func SystemMsg(content string) model.Message {
	return model.Message{
		ID:        "system-" + time.Now().Format("20060102150405.999999999"),
		Username:  "system",
		Content:   content,
		Timestamp: time.Now(),
	}
}

type SetupWizardMsg struct{}

type SetupRestartMsg struct{}
