package commands

import (
	tea "github.com/charmbracelet/bubbletea"
)

type SetupCmd struct{}

func NewSetupCmd() *SetupCmd {
	return &SetupCmd{}
}

func (c *SetupCmd) Name() string { return "setup" }

func (c *SetupCmd) Description() string {
	return "Restart marga in server setup mode"
}

func (c *SetupCmd) Execute(args []string) (tea.Cmd, error) {
	return func() tea.Msg {
		return SetupRestartMsg{}
	}, nil
}
