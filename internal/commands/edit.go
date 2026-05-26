package commands

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type EditCmd struct{}

func NewEditCmd() *EditCmd {
	return &EditCmd{}
}

func (c *EditCmd) Name() string { return "edit" }

func (c *EditCmd) Description() string {
	return "Edit your message: /edit, /edit last, or /edit <id> [text]"
}

func (c *EditCmd) Execute(args []string) (tea.Cmd, error) {
	if len(args) == 0 {
		return func() tea.Msg {
			return StartEditMsg{}
		}, nil
	}

	target := strings.TrimSpace(args[0])
	if target == "" {
		return nil, fmt.Errorf("usage: /edit, /edit last, or /edit <message-id> [text]")
	}
	if len(args) == 1 {
		return func() tea.Msg {
			return StartEditMsg{Target: target}
		}, nil
	}

	content := strings.TrimSpace(strings.Join(args[1:], " "))
	if content == "" {
		return nil, fmt.Errorf("usage: /edit, /edit last, or /edit <message-id> [text]")
	}
	return func() tea.Msg {
		return EditMessageMsg{Target: target, Content: content}
	}, nil
}
