package commands

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// StartVerificationMsg asks the TUI to begin verifying another user's device
// (Matrix SAS). Target is a network-native user id (MXID).
type StartVerificationMsg struct {
	Target string
}

// VerifyCmd starts interactive emoji device verification on Matrix.
type VerifyCmd struct{}

func NewVerifyCmd() *VerifyCmd { return &VerifyCmd{} }

func (c *VerifyCmd) Name() string { return "verify" }

func (c *VerifyCmd) Description() string {
	return "Verify a Matrix device via emoji (SAS): /verify @user:server"
}

func (c *VerifyCmd) Execute(args []string) (tea.Cmd, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: /verify @user:server")
	}
	target := strings.TrimSpace(args[0])
	if target == "" {
		return nil, fmt.Errorf("usage: /verify @user:server")
	}
	return func() tea.Msg {
		return StartVerificationMsg{Target: target}
	}, nil
}
