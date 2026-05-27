package commands

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/model"
)

type ServersCmd struct {
	cfg        *config.Config
	configPath string
}

func NewServersCmd(cfg *config.Config, configPath string) *ServersCmd {
	return &ServersCmd{cfg: cfg, configPath: configPath}
}

func (c *ServersCmd) Name() string { return "servers" }

func (c *ServersCmd) Description() string {
	return "Exit and choose a different Discord server"
}

func (c *ServersCmd) Execute(args []string) (tea.Cmd, error) {
	if len(c.cfg.ConfiguredGuilds) == 0 {
		msg := SystemMsg("no configured servers — run setup wizard on next launch or use /join to browse channels")
		return func() tea.Msg {
			return CommandOutputMsg{Messages: []model.Message{msg}}
		}, nil
	}

	if len(args) == 0 {
		if len(c.cfg.ConfiguredGuilds) == 1 {
			msg := SystemMsg("only one server configured — use /servers <1> to restart with it")
			return func() tea.Msg {
				return CommandOutputMsg{Messages: []model.Message{msg}}
			}, nil
		}
		return func() tea.Msg {
			return ServersMsg{}
		}, nil
	}

	var idx int
	if _, err := fmt.Sscanf(args[0], "%d", &idx); err != nil || idx < 1 || idx > len(c.cfg.ConfiguredGuilds) {
		msg := SystemMsg(fmt.Sprintf("invalid server number — use /servers to see available servers (1-%d)", len(c.cfg.ConfiguredGuilds)))
		return func() tea.Msg {
			return CommandOutputMsg{Messages: []model.Message{msg}}
		}, nil
	}

	g := c.cfg.ConfiguredGuilds[idx-1]
	prevGuildID := c.cfg.General.GuildID
	prevGuildName := c.cfg.General.GuildName
	prevChannel := c.cfg.General.Channel

	c.cfg.General.GuildID = g.ID
	c.cfg.General.GuildName = g.Name
	c.cfg.General.Channel = g.Channel

	if err := c.cfg.Save(c.configPath); err != nil {
		c.cfg.General.GuildID = prevGuildID
		c.cfg.General.GuildName = prevGuildName
		c.cfg.General.Channel = prevChannel
		return nil, fmt.Errorf("saving config: %w", err)
	}

	return func() tea.Msg {
		return ServersMsg{}
	}, nil
}

type ServersMsg struct{}
