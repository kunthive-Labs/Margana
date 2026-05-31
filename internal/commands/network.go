package commands

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// NetworkCmd lists the connected networks, or switches the active one.
type NetworkCmd struct{}

func NewNetworkCmd() *NetworkCmd {
	return &NetworkCmd{}
}

func (c *NetworkCmd) Name() string {
	return "network"
}

func (c *NetworkCmd) Description() string {
	return "List networks, or switch the active one: /network matrix"
}

func (c *NetworkCmd) Execute(args []string) (tea.Cmd, error) {
	target := ""
	if len(args) > 0 {
		target = strings.TrimSpace(args[0])
	}
	return func() tea.Msg {
		return SwitchNetworkMsg{Network: target}
	}, nil
}
