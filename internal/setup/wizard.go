// Package setup runs Marga's interactive first-run wizard: it walks the user
// through Discord authentication, relay endpoints, and server selection, then
// writes the resulting configuration.
package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/kunthive-Labs/Margana/internal/auth/discord"
	"github.com/kunthive-Labs/Margana/internal/config"
)

const botPermissions = 536988672

type Step int

const (
	StepChooseMethod Step = iota
	StepPickGuild
	StepConfirmGuild
	StepInviteBot
	StepWaitForBot
	StepDone
)

type WizardState struct {
	Step          Step
	Method        string
	FromWeb       bool
	Guilds        []discord.Guild
	SelectedGuild discord.Guild
	PrevGuild     discord.Guild
}

func readLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		text, err := reader.ReadString('\n')
		ch <- result{strings.TrimSpace(text), err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.text, r.err
	}
}

func RunSetup(ctx context.Context, cfg *config.Config, configPath string, auth *discord.Authenticator, force bool) error {
	if !force && (cfg.General.GuildID != "" || len(cfg.ConfiguredGuilds) > 0) {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	reader := bufio.NewReader(os.Stdin)
	state := &WizardState{Step: StepChooseMethod}

	clearScreen()
	printLogo()
	fmt.Printf("  %s\n", bold("Setup Wizard"))
	fmt.Printf("  %s\n", dim("Connect Marga to your Discord server."))
	fmt.Println()

	for state.Step != StepDone {
		if ctx.Err() != nil {
			fmt.Println()
			fmt.Printf("  %s\n", dim("Setup cancelled."))
			return nil
		}

		switch state.Step {
		case StepChooseMethod:
			if err := handleChooseMethod(ctx, reader, cfg, state); err != nil {
				return err
			}
		case StepPickGuild:
			if err := handlePickGuild(ctx, reader, auth, cfg, state); err != nil {
				return err
			}
		case StepConfirmGuild:
			if err := handleConfirmGuild(ctx, reader, state); err != nil {
				return err
			}
		case StepInviteBot:
			if err := handleInviteBot(ctx, reader, cfg, state); err != nil {
				return err
			}
		case StepWaitForBot:
			if err := handleWaitForBot(ctx, reader, cfg, state); err != nil {
				return err
			}
		}
	}

	select {
	case <-ctx.Done():
		fmt.Println()
		fmt.Printf("  %s\n", dim("Setup cancelled."))
		return nil
	default:
	}

	return saveConfig(cfg, configPath, state)
}

func handleChooseMethod(ctx context.Context, reader *bufio.Reader, cfg *config.Config, state *WizardState) error {
	state.Method = ""
	step("How would you like to configure?")
	fmt.Println()
	option("1", "Terminal "+dim("(recommended)"))
	option("2", "Web browser")
	fmt.Println()

	for state.Method == "" {
		if ctx.Err() != nil {
			return nil
		}

		prompt("Choice (1/2): ")
		choice, err := readLine(ctx, reader)
		if err != nil {
			return nil
		}

		switch choice {
		case "1":
			state.Method = "terminal"
			state.Step = StepPickGuild
		case "2":
			state.Method = "web"
			webURL := fmt.Sprintf("%s/terminal-setup#token=%s", cfg.Server.WebSetupURL, cfg.Auth.Discord.AccessToken)
			if err := openBrowser(webURL); err != nil {
				fmt.Printf("\n  %s\n", dim("Could not open browser automatically."))
			}
			fmt.Println()
			fmt.Printf("  %s\n", bold("Setup URL:"))
			fmt.Printf("  %s\n\n", accent(webURL))
			fmt.Printf("  %s\n", dim("Open the link above in your browser and complete setup there."))
			prompt("Press Enter when done... ")
			if _, err := readLine(ctx, reader); err != nil {
				return nil
			}
			state.FromWeb = true
			if webConfig, ok := fetchWebConfig(cfg, state); ok {
				state.SelectedGuild = discord.Guild{
					ID:   webConfig.GuildID,
					Name: webConfig.GuildName,
				}
				state.Step = StepDone
			} else {
				fmt.Println()
				fmt.Printf("  %s\n", errText("Could not retrieve web setup configuration. Falling back to terminal setup..."))
				state.Method = "terminal"
				state.Step = StepPickGuild
			}
		default:
			fmt.Printf("  %s\n\n", errText("Please enter 1 or 2."))
		}
	}
	return nil
}

func handlePickGuild(ctx context.Context, reader *bufio.Reader, auth *discord.Authenticator, cfg *config.Config, state *WizardState) error {
	clearScreen()
	printLogo()
	fmt.Printf("  %s\n", dim("Fetching your servers..."))

	allGuilds, err := auth.FetchUserGuilds(ctx, cfg.Auth.Discord.AccessToken)
	if err != nil {
		fmt.Printf("  %s %s\n", warn("⚠"), dim(fmt.Sprintf("Could not fetch servers: %v", err)))
		prompt("Press Enter to skip... ")
		if _, err := readLine(ctx, reader); err != nil {
			return nil
		}
		state.Step = StepDone
		return nil
	}

	var availableGuilds []discord.Guild
	for _, g := range discord.FilterAdminGuilds(allGuilds) {
		isConfigured := false
		for _, cg := range cfg.ConfiguredGuilds {
			if cg.ID == g.ID {
				isConfigured = true
				break
			}
		}
		if !isConfigured {
			availableGuilds = append(availableGuilds, g)
		}
	}
	state.Guilds = availableGuilds
	if len(state.Guilds) == 0 {
		fmt.Printf("  %s\n", warn("No new servers found with admin permissions (all may already be configured)."))
		prompt("Press Enter to continue... ")
		if _, err := readLine(ctx, reader); err != nil {
			return nil
		}
		state.Step = StepDone
		return nil
	}

	botPresence := make(map[string]bool)
	if state.FromWeb {
		for _, g := range state.Guilds {
			checkURL := fmt.Sprintf("%s/api/bot/check/%s", cfg.Server.RelayURL, g.ID)
			if inGuild, err := pollBotPresence(ctx, checkURL, cfg.Server.APIKey); err == nil && inGuild {
				botPresence[g.ID] = true
			}
		}
	}

	fmt.Println()
	step("Select a server:")
	fmt.Println()
	for i, g := range state.Guilds {
		tag := ""
		if g.Owner {
			tag = " " + dim("(owner)")
		}
		botTag := ""
		if botPresence[g.ID] {
			botTag = " " + success("(bot connected)")
		}
		fmt.Printf("    %s  %s%s%s\n", accent(fmt.Sprintf("[%d]", i+1)), g.Name, tag, botTag)
	}
	fmt.Println()

	for {
		if ctx.Err() != nil {
			return nil
		}

		prompt("Server number: ")
		choice, err := readLine(ctx, reader)
		if err != nil {
			return nil
		}

		var idx int
		_, err = fmt.Sscanf(choice, "%d", &idx)
		if err != nil || idx < 1 || idx > len(state.Guilds) {
			fmt.Printf("  %s\n", errText("Invalid — pick a number from the list."))
			continue
		}

		state.PrevGuild = state.SelectedGuild
		state.SelectedGuild = state.Guilds[idx-1]

		if botPresence[state.SelectedGuild.ID] {
			fmt.Printf("  %s\n", success("✓ Bot already connected to this server."))
			state.Step = StepDone
			return nil
		}

		state.Step = StepConfirmGuild
		return nil
	}
}

func handleConfirmGuild(ctx context.Context, reader *bufio.Reader, state *WizardState) error {
	clearScreen()
	printLogo()
	fmt.Printf("  Selected: %s\n", bold(state.SelectedGuild.Name))
	fmt.Println()
	option("y", "Confirm")
	option("n", "Go back")
	prompt("(y/n): ")

	choice, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	choice = strings.ToLower(choice)

	switch choice {
	case "y", "yes", "":
		state.Step = StepInviteBot
	case "n", "no":
		if state.PrevGuild.ID != "" {
			state.SelectedGuild = state.PrevGuild
		}
		state.Step = StepPickGuild
	default:
		fmt.Printf("  %s\n", errText("Enter y or n."))
	}
	return nil
}

func handleInviteBot(ctx context.Context, reader *bufio.Reader, cfg *config.Config, state *WizardState) error {
	botClientID := cfg.Server.BotClientID
	if botClientID == "" {
		botClientID = cfg.Auth.Discord.ClientID
	}

	inviteURL := fmt.Sprintf(
		"https://discord.com/oauth2/authorize?client_id=%s&permissions=%d&scope=bot&guild_id=%s",
		botClientID, botPermissions, state.SelectedGuild.ID,
	)

	clearScreen()
	printLogo()
	step("Invite the bot to your server")
	fmt.Println()
	fmt.Printf("  %s %s\n", dim("Link:"), accent(inviteURL))
	_ = openBrowser(inviteURL)
	fmt.Printf("  %s\n", dim("Browser opened with invite link."))
	fmt.Println()
	option("Enter", "I've invited — verify & continue")
	option("b", "Go back")
	prompt("Choice: ")

	choice, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	choice = strings.ToLower(choice)

	switch choice {
	case "b":
		state.Step = StepPickGuild
	default:
		state.Step = StepWaitForBot
	}
	return nil
}

func handleWaitForBot(ctx context.Context, reader *bufio.Reader, cfg *config.Config, state *WizardState) error {
	clearScreen()
	printLogo()
	fmt.Printf("  %s", dim("Verifying bot presence "))

	maxAttempts := 30
	pollInterval := 2 * time.Second
	checkURL := fmt.Sprintf("%s/api/bot/check/%s", cfg.Server.RelayURL, state.SelectedGuild.ID)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("\r  %s %s", dim("Verifying bot presence"), dim(fmt.Sprintf("[%d/%d]", attempt, maxAttempts)))

		inGuild, err := pollBotPresence(ctx, checkURL, cfg.Server.APIKey)
		if err == nil && inGuild {
			fmt.Println()
			fmt.Printf("  %s\n", success("✓ Bot joined successfully!"))
			fmt.Println()
			state.Step = StepDone
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	fmt.Println()
	fmt.Printf("  %s\n", warn("Bot not detected — it may need more time."))
	fmt.Println()
	option("r", "Retry")
	option("s", "Skip — save & continue")
	option("b", "Go back")
	prompt("Choice: ")

	choice, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	choice = strings.ToLower(choice)

	switch choice {
	case "r":
		return handleWaitForBot(ctx, reader, cfg, state)
	case "b":
		state.Step = StepInviteBot
		return nil
	default:
		fmt.Printf("  %s\n", dim("Saving configuration — verify later."))
		state.Step = StepDone
	}
	return nil
}

func pollBotPresence(ctx context.Context, checkURL, apiKey string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false, err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var result struct {
		BotInGuild bool `json:"bot_in_guild"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}
	return result.BotInGuild, nil
}

func saveConfig(cfg *config.Config, configPath string, state *WizardState) error {
	if state.SelectedGuild.ID == "" {
		return nil
	}

	if cfg.General.GuildID == "" {
		cfg.General.GuildID = state.SelectedGuild.ID
		cfg.General.GuildName = state.SelectedGuild.Name
	}

	channel := cfg.General.Channel
	if state.FromWeb {
		if webConfig, ok := fetchWebConfig(cfg, state); ok && webConfig.ChannelName != "" {
			channel = webConfig.ChannelName
		}
	}

	entry := config.GuildEntry{
		ID:         state.SelectedGuild.ID,
		Name:       state.SelectedGuild.Name,
		Channel:    channel,
		Configured: true,
	}

	found := false
	for i, g := range cfg.ConfiguredGuilds {
		if g.ID == entry.ID {
			cfg.ConfiguredGuilds[i] = entry
			found = true
			break
		}
	}
	if !found {
		cfg.ConfiguredGuilds = append(cfg.ConfiguredGuilds, entry)
	}

	if err := cfg.Save(configPath); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}

	fmt.Println()
	fmt.Printf("  %s %s\n", success("✓"), bold("Configuration saved"))
	bullet("Server", state.SelectedGuild.Name)
	if channel != "" {
		bullet("Channel", channel)
	}
	fmt.Println()
	return nil
}

type webSetupConfig struct {
	GuildID     string `json:"guild_id"`
	GuildName   string `json:"guild_name"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
}

func fetchWebConfig(cfg *config.Config, state *WizardState) (*webSetupConfig, bool) {
	discordID := cfg.General.DiscordID
	if discordID == "" {
		return nil, false
	}

	url := fmt.Sprintf("%s/api/setup/config/%s", cfg.Server.RelayURL, discordID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	if cfg.Server.APIKey != "" {
		req.Header.Set("X-API-Key", cfg.Server.APIKey)
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		GuildID     string `json:"guild_id"`
		GuildName   string `json:"guild_name"`
		ChannelID   string `json:"channel_id"`
		ChannelName string `json:"channel_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || !result.OK {
		return nil, false
	}

	return &webSetupConfig{
		GuildID:     result.GuildID,
		GuildName:   result.GuildName,
		ChannelID:   result.ChannelID,
		ChannelName: result.ChannelName,
	}, true
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
