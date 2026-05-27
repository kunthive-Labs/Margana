package commands

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type JoinCmd struct{}

func NewJoinCmd() *JoinCmd {
	return &JoinCmd{}
}

func (c *JoinCmd) Name() string {
	return "join"
}

func (c *JoinCmd) Description() string {
	return "Switch to a channel: /join #general"
}

func (c *JoinCmd) Execute(args []string) (tea.Cmd, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: /join #channel")
	}

	channel := strings.TrimPrefix(args[0], "#")
	if channel == "" {
		return nil, fmt.Errorf("invalid channel name")
	}

	return func() tea.Msg {
		return SwitchChannelMsg{Channel: channel}
	}, nil
}
