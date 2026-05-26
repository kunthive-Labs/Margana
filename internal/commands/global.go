package commands

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/config"
)

type GlobalCmd struct {
	cfg        *config.Config
	configPath string
	relayURL   string
	apiKey     string
}

func NewGlobalCmd(cfg *config.Config, configPath, relayURL, apiKey string) *GlobalCmd {
	return &GlobalCmd{
		cfg:        cfg,
		configPath: configPath,
		relayURL:   relayURL,
		apiKey:     apiKey,
	}
}

func (c *GlobalCmd) Name() string { return "global" }

func (c *GlobalCmd) Description() string {
	return "Discover global configured servers you are in: /global"
}

func (c *GlobalCmd) Execute(args []string) (tea.Cmd, error) {
	if c.relayURL == "" {
		return nil, fmt.Errorf("server.relay_url is not configured")
	}

	return func() tea.Msg {
		return GlobalDiscoverMsg{}
	}, nil
}

type GlobalDiscoverMsg struct{}
