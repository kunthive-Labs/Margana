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

type TriggerHistoryLoadMsg struct{}

type DeleteChannelMsg struct {
	Channel string
}

type SendRawMsg struct {
	Content string
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
