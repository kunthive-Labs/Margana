package commands

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type ReactCmd struct{}

func NewReactCmd() *ReactCmd {
	return &ReactCmd{}
}

func (c *ReactCmd) Name() string { return "react" }

func (c *ReactCmd) Description() string {
	return "React to the latest message: /react <emoji> (e.g. /react 👍 or /react :tada:)"
}

func (c *ReactCmd) Execute(args []string) (tea.Cmd, error) {
	emoji := strings.TrimSpace(strings.Join(args, " "))
	if emoji == "" {
		return nil, fmt.Errorf("usage: /react <emoji> (e.g. /react 👍 or /react :tada:)")
	}
	return func() tea.Msg {
		return ReactMsg{Emoji: emoji}
	}, nil
}
