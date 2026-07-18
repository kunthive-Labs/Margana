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

	"golang.org/x/term"

	"github.com/kunthive-Labs/Margana/internal/auth/discord"
	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/network/credstore"
)

const botPermissions = 536988672

type Step int

const (
	StepChooseNetwork Step = iota
	StepMatrixConnect
	StepIRCConnect
	StepPickGuild
	StepConfirmGuild
	StepInviteBot
	StepWaitForBot
	StepDone
)

type WizardState struct {
	Step          Step
	Network       string
	Guilds        []discord.Guild
	SelectedGuild discord.Guild
	PrevGuild     discord.Guild
}

// NeedsOnboarding reports whether the first-run wizard should run. It returns
// true when nothing is configured yet — no Discord server (guild) and no
// [[networks]] entry — or when setup is forced (marga --setup). It is the single
// source of truth shared by cmd/marga (to decide whether to run the wizard and
// whether to skip the pre-wizard Discord OAuth) and RunSetup's own guard.
func NeedsOnboarding(cfg *config.Config, force bool) bool {
	return force || (cfg.General.GuildID == "" && len(cfg.ConfiguredGuilds) == 0 && len(cfg.Networks) == 0)
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
	if !NeedsOnboarding(cfg, force) {
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
	state := &WizardState{Step: StepChooseNetwork}

	clearScreen()
	printLogo()
	fmt.Printf("  %s\n", bold("Setup Wizard"))
	fmt.Printf("  %s\n", dim("Connect Marga to a chat network."))
	fmt.Println()

	for state.Step != StepDone {
		if ctx.Err() != nil {
			fmt.Println()
			fmt.Printf("  %s\n", dim("Setup cancelled."))
			return nil
		}

		switch state.Step {
		case StepChooseNetwork:
			if err := handleChooseNetwork(ctx, reader, cfg, configPath, state); err != nil {
				return err
			}
		case StepMatrixConnect:
			if err := handleMatrixConnect(ctx, reader, cfg, configPath, state); err != nil {
				return err
			}
		case StepIRCConnect:
			if err := handleIRCConnect(ctx, reader, cfg, configPath, state); err != nil {
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

func handleChooseNetwork(ctx context.Context, reader *bufio.Reader, cfg *config.Config, configPath string, state *WizardState) error {
	state.Network = ""
	step("Which network would you like to connect?")
	fmt.Println()
	option("1", "Matrix  "+dim("— works now, no server needed"))
	option("2", "IRC     "+dim("— classic, connect to any IRC network"))
	option("3", "Discord "+dim("— advanced, needs a self-hosted relay"))
	fmt.Println()

	for state.Network == "" {
		if ctx.Err() != nil {
			return nil
		}

		prompt("Choice (1/2/3): ")
		choice, err := readLine(ctx, reader)
		if err != nil {
			return nil
		}

		switch choice {
		case "1":
			state.Network = "matrix"
			state.Step = StepMatrixConnect
		case "2":
			state.Network = "irc"
			state.Step = StepIRCConnect
		case "3":
			// Discord is the advanced path: it needs a self-hosted relay and a
			// registered Discord application. Only continue when that infra is
			// present; otherwise point the user at the docs and loop back so a
			// first-timer isn't stranded at a dead end.
			switch {
			case cfg.Auth.Discord.ClientID != "" && cfg.Server.WebsocketURL != "":
				// Relay + Discord app configured: ensure a valid token (refresh or
				// OAuth as needed), then choose a server.
				cfg.Auth.Enabled = true
				cfg.Auth.Provider = "discord"
				if err := discord.EnsureUserConfig(ctx, cfg, configPath); err != nil {
					fmt.Printf("\n  %s\n\n", errText(fmt.Sprintf("Discord authentication failed: %v", err)))
					continue
				}
				state.Network = "discord"
				state.Step = StepPickGuild
			case cfg.Auth.Discord.AccessToken != "":
				// A token is already present (e.g. env-configured) but the app/relay
				// aren't set here — proceed with what we have.
				state.Network = "discord"
				state.Step = StepPickGuild
			default:
				fmt.Println()
				fmt.Printf("  %s\n", warn("Discord needs a self-hosted relay and a registered Discord app."))
				fmt.Printf("  %s\n", dim("See docs/SELF_HOSTING.md, or choose Matrix (1) for a zero-setup connection."))
				fmt.Println()
			}
		default:
			fmt.Printf("  %s\n\n", errText("Please enter 1, 2, or 3."))
		}
	}
	return nil
}

// handleMatrixConnect gathers the non-secret Matrix connection details
// (homeserver + user id), writes them as an enabled [[networks]] entry, and
// disables Discord for a Matrix-only first run so config validation doesn't
// demand relay endpoints. The password is never handled here: the Matrix adapter
// prompts for it at Connect time and exchanges it for a keyring-stored token.
func handleMatrixConnect(ctx context.Context, reader *bufio.Reader, cfg *config.Config, configPath string, state *WizardState) error {
	clearScreen()
	printLogo()
	step("Matrix quick-connect")
	fmt.Println()
	fmt.Printf("  %s\n", dim("Connect directly to a Matrix homeserver — no relay, end-to-end encrypted."))
	fmt.Println()

	prompt("Homeserver URL (e.g. https://matrix.org): ")
	hs, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	hs = normalizeHomeserver(hs)

	prompt("User ID (e.g. @you:matrix.org): ")
	uid, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	uid = strings.TrimSpace(uid)

	if hs == "" || uid == "" {
		fmt.Printf("\n  %s\n\n", errText("Homeserver and user ID are both required."))
		return nil // stay on StepMatrixConnect; the wizard loop re-enters this step
	}

	entry := config.NetworkConfig{ID: "matrix", Type: "matrix", Enabled: true, Homeserver: hs, UserID: uid}
	replaced := false
	for i := range cfg.Networks {
		if cfg.Networks[i].ID == "matrix" {
			cfg.Networks[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Networks = append(cfg.Networks, entry)
	}

	// Disable Discord only for a clean Matrix-only first run; preserve it when a
	// Discord server or app is already configured (so multi-network setups keep
	// working).
	if cfg.General.GuildID == "" && len(cfg.ConfiguredGuilds) == 0 && cfg.Auth.Discord.ClientID == "" {
		cfg.Auth.Enabled = false
	}

	if err := cfg.Save(configPath); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}

	fmt.Println()
	fmt.Printf("  %s %s\n", success("✓"), bold("Matrix configured"))
	bullet("Homeserver", hs)
	bullet("User ID", uid)
	fmt.Printf("\n  %s\n", dim("You'll be asked for your password when Marga connects."))
	fmt.Println()
	state.Step = StepDone
	return nil
}

// normalizeHomeserver defaults a missing scheme to https and trims a trailing
// slash, mirroring the Matrix adapter's own normalization so a value typed here
// (e.g. "matrix.org") resolves the same way when the adapter builds its client.
func normalizeHomeserver(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	return strings.TrimSuffix(s, "/")
}

// handleIRCConnect gathers the non-secret IRC connection details (server, TLS,
// nick, and an initial channel), writes them as an enabled [[networks]] entry,
// and disables Discord for an IRC-only first run so config validation doesn't
// demand relay endpoints. If the user supplies a SASL account, the password is
// read without echo and stored in the OS keyring (service "marga-irc") — never
// in the config file.
func handleIRCConnect(ctx context.Context, reader *bufio.Reader, cfg *config.Config, configPath string, state *WizardState) error {
	clearScreen()
	printLogo()
	step("IRC quick-connect")
	fmt.Println()
	fmt.Printf("  %s\n", dim("Connect directly to any IRC network — no relay."))
	fmt.Println()

	prompt("Server (e.g. irc.libera.chat): ")
	server, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	server = strings.TrimSpace(server)

	prompt("Nickname: ")
	nick, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	nick = strings.TrimSpace(nick)

	if server == "" || nick == "" {
		fmt.Printf("\n  %s\n\n", errText("Server and nickname are both required."))
		return nil // stay on StepIRCConnect; the wizard loop re-enters this step
	}

	prompt("Use TLS? (Y/n): ")
	tlsChoice, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	useTLS := !strings.EqualFold(strings.TrimSpace(tlsChoice), "n")
	port := 6667
	if useTLS {
		port = 6697
	}

	prompt("Channel to join (e.g. #general): ")
	channel, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	channel = normalizeChannel(channel)

	prompt("SASL account (optional — press Enter to skip): ")
	saslUser, err := readLine(ctx, reader)
	if err != nil {
		return nil
	}
	saslUser = strings.TrimSpace(saslUser)

	entry := config.NetworkConfig{
		ID: "irc", Type: "irc", Enabled: true,
		Server: server, Port: port, TLS: useTLS, Nick: nick, SASLUser: saslUser,
	}
	replaced := false
	for i := range cfg.Networks {
		if cfg.Networks[i].ID == "irc" {
			cfg.Networks[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Networks = append(cfg.Networks, entry)
	}

	if channel != "" {
		cfg.General.Channel = channel
	}

	// Store the SASL password in the OS keyring (never the config file). The
	// adapter reads it at connect time; MARGA_IRC_PASSWORD overrides it.
	if saslUser != "" {
		if pw, perr := readSecret("SASL password: "); perr == nil && pw != "" {
			if serr := credstore.Set("irc", "sasl_password", pw); serr != nil {
				fmt.Printf("  %s\n", warn(fmt.Sprintf("Could not store SASL password in the keyring: %v", serr)))
				fmt.Printf("  %s\n", dim("Set MARGA_IRC_PASSWORD in the environment instead."))
			}
		}
	}

	// Disable Discord only for a clean IRC-only first run; preserve it when a
	// Discord server or app is already configured.
	if cfg.General.GuildID == "" && len(cfg.ConfiguredGuilds) == 0 && cfg.Auth.Discord.ClientID == "" {
		cfg.Auth.Enabled = false
	}

	if err := cfg.Save(configPath); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}

	fmt.Println()
	fmt.Printf("  %s %s\n", success("✓"), bold("IRC configured"))
	bullet("Server", fmt.Sprintf("%s:%d", server, port))
	bullet("Nickname", nick)
	if channel != "" {
		bullet("Channel", channel)
	}
	if saslUser != "" {
		bullet("SASL account", saslUser)
	}
	fmt.Println()
	state.Step = StepDone
	return nil
}

// normalizeChannel ensures an IRC channel name carries a valid prefix (defaults
// to "#"), so "general" and "#general" resolve to the same channel — and match
// the channel the adapter stamps on incoming messages.
func normalizeChannel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	switch s[0] {
	case '#', '&', '!', '+':
		return s
	default:
		return "#" + s
	}
}

// readSecret reads a line from the terminal without echoing it, for passwords.
func readSecret(promptText string) (string, error) {
	fmt.Printf("  %s %s", accent("›"), promptText)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
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
