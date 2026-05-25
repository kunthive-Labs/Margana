package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/kunthive-Labs/Margana/internal/auth/discord"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/db"
	"github.com/kunthive-Labs/Margana/internal/guilds"
	"github.com/kunthive-Labs/Margana/internal/history"
	"github.com/kunthive-Labs/Margana/internal/setup"
	"github.com/kunthive-Labs/Margana/internal/tui"
	"github.com/kunthive-Labs/Margana/internal/webhook"
	"github.com/kunthive-Labs/Margana/internal/wsclient"
)

var version = "dev"

// ANSI helpers
const (
	cReset = "\033[0m"
	cBold  = "\033[1m"
	cDim   = "\033[2m"
	cWhite = "\033[97m"
	cCyan  = "\033[36m"
	cGreen = "\033[32m"
	cGray  = "\033[90m"
)

func cAccent(s string) string { return cCyan + s + cReset }
func cBoldW(s string) string  { return cBold + cWhite + s + cReset }
func cDimmed(s string) string { return cDim + s + cReset }

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

func printGreeting() {
	fmt.Println()
	logo := []string{
		"                       888 888         ",
		"                       888 888         ",
		"                       888 888         ",
		"88888b.d88b.  .d88b.  888 888 888  888",
		"888 \"888 \"88b d88\"\"88b 888 888 888  888",
		"888  888  888 888  888 888 888 888  888",
		"888  888  888 Y88..88P 888 888 Y88b 888",
		"888  888  888  \"Y88P\"  888 888  \"Y88888",
		"                                    888",
		"                               Y8b d88P",
		"                               d88P\"   ",
	}
	for i, line := range logo {
		if i == 5 {
			fmt.Println("  " + cAccent("> ") + cBoldW(line))
		} else {
			fmt.Println("    " + cBoldW(line))
		}
	}
	fmt.Println()
	fmt.Printf("    %s %s\n", cDimmed("the terminal-native discord experience"), cGray+"v"+version+cReset)
	fmt.Println()
}

func readLineWithSignal(ctx context.Context, reader *bufio.Reader) (string, error) {
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

func main() {
	_ = godotenv.Load()

	clearScreen()
	printGreeting()

	forceSetup := false
	for _, arg := range os.Args[1:] {
		if arg == "--setup" || arg == "-s" {
			forceSetup = true
		}
	}

	configPath, err := config.ConfigPathFromArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s config: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s config: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}

	if err := discord.EnsureUserConfig(context.Background(), cfg, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "%s auth: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}

	auth := discord.New(cfg)

	if err := setup.RunSetup(context.Background(), cfg, configPath, auth, forceSetup); err != nil {
		fmt.Fprintf(os.Stderr, "%s setup: %v\n", cAccent("✗"), err)
	}

	// Reload config after setup so any guilds saved during the wizard
	// (including via web setup) are reflected in ConfiguredGuilds.
	if freshCfg, err := config.Load(); err == nil {
		cfg = freshCfg
	}

	if len(cfg.ConfiguredGuilds) >= 1 {
		showServerPicker(cfg, configPath)
		// Reload once more to pick up any server switch the user just made.
		if freshCfg, err := config.Load(); err == nil {
			cfg = freshCfg
		}
	}

	dbPath, err := config.GuildDBPath(cfg.General.GuildID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s database path: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}
	store, err := db.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s database: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}
	defer store.Close()

	client := wsclient.New(cfg.Server.WebsocketURL, cfg.General.Username, cfg.General.Channel)
	sender := webhook.New(cfg.Server.WebhookURL, cfg.Server.RelayURL, cfg.Server.APIKey, cfg.General.Username, cfg.General.DiscordAvatarURL, cfg.General.GuildID)
	fetcher := history.New(cfg.Server.RelayURL)
	if cfg.General.GuildID != "" {
		fetcher = fetcher.WithGuild(cfg.General.GuildID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go client.ConnectWithRetry(ctx)

	registry := commands.NewRegistry()
	registry.Register(commands.NewHelpCmd(registry))
	registry.Register(commands.NewJoinCmd(client, fetcher, store))
	registry.Register(commands.NewHistoryCmd())
	registry.Register(commands.NewSearchCmd(store))
	registry.Register(commands.NewQuitCmd())
	registry.Register(commands.NewLeaveCmd(store))
	registry.Register(commands.NewStatusCmd())
	registry.Register(commands.NewFileCmd())
	registry.Register(commands.NewOpenCmd())
	registry.Register(commands.NewSnippetCmd())
	registry.Register(commands.NewLogoutCmd(cfg, configPath))
	registry.Register(commands.NewClearMentionsCmd())
	registry.Register(commands.NewEditCmd())
	registry.Register(commands.NewServersCmd(cfg, configPath))
	registry.Register(commands.NewSetupCmd())
	registry.Register(commands.NewGlobalCmd(cfg, configPath, cfg.Server.RelayURL, cfg.Server.APIKey))

	tui.InitImageProtocol(cfg.UI.ImageProtocol)
	tui.ApplyTheme(cfg.UI.Theme)

	model := tui.New(client, sender, store, fetcher, registry,
		cfg.General.Channel, cfg.General.Username, cfg.General.DiscordID,
		cfg.General.DiscordUsername, cfg.General.DiscordGlobalName, cfg.General.GuildName,
		cfg.ConfiguredGuilds,
		cfg.Auth.Discord.AccessToken, cfg.Server.BotClientID,
		configPath, cfg, version,
	)
	if cfg.Github.Repo != "" {
		model = model.WithGithub(cfg.Github.Repo, cfg.Github.Token)
	}
	p := tea.NewProgram(model, tea.WithAltScreen())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		_ = client.Close()
		cancel()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", cAccent("✗"), err)
		os.Exit(1)
	}

	serversFlagFile := configPath + ".servers-flag"
	if _, err := os.Stat(serversFlagFile); err == nil {
		_ = os.Remove(serversFlagFile)
		runServerPrompt(configPath)
	}

	globalFlagFile := configPath + ".global-flag"
	if _, err := os.Stat(globalFlagFile); err == nil {
		_ = os.Remove(globalFlagFile)
		runGlobalPrompt(configPath)
	}

	flagFile := configPath + ".setup-flag"
	if _, err := os.Stat(flagFile); err == nil {
		_ = os.Remove(flagFile)
		runSetupRestart(configPath)
	}
}

func showServerPicker(cfg *config.Config, configPath string) {
	// If GuildID is unset but ConfiguredGuilds has entries, auto-select the first one.
	if cfg.General.GuildID == "" && len(cfg.ConfiguredGuilds) > 0 {
		g := cfg.ConfiguredGuilds[0]
		cfg.General.GuildID = g.ID
		cfg.General.GuildName = g.Name
		if cfg.General.Channel == "" {
			cfg.General.Channel = g.Channel
		}
		_ = cfg.Save(configPath)
	}

	// With only 1 configured guild, no switching needed — just confirm it.
	if len(cfg.ConfiguredGuilds) == 1 {
		clearScreen()
		printGreeting()
		fmt.Printf("  %s %s %s\n", cDimmed("•"), cDimmed("Connected to:"), cBoldW(cfg.General.GuildName))
		if cfg.General.Channel != "" {
			fmt.Printf("  %s %s %s\n", cDimmed("•"), cDimmed("Channel:"), cDimmed(cfg.General.Channel))
		}
		fmt.Println()
		fmt.Printf("  %s %s\n", cGreen+"✓"+cReset, cBoldW("Ready — press Enter to continue"))
		fmt.Printf("\n  %s ", cAccent("›"))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			cancel()
		}()
		reader := bufio.NewReader(os.Stdin)
		_, _ = readLineWithSignal(ctx, reader)
		fmt.Println()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	reader := bufio.NewReader(os.Stdin)

	clearScreen()
	printGreeting()
	currentName := cfg.General.GuildName
	if currentName == "" && len(cfg.ConfiguredGuilds) > 0 {
		currentName = cfg.ConfiguredGuilds[0].Name
	}
	fmt.Printf("  %s %s %s\n", cDimmed("•"), cDimmed("Server:"), cBoldW(currentName))
	fmt.Println()
	for i, g := range cfg.ConfiguredGuilds {
		marker := " "
		if g.ID == cfg.General.GuildID {
			marker = cGreen + "●" + cReset
		}
		fmt.Printf("    %s %s %s  %s\n", marker, cAccent(fmt.Sprintf("[%d]", i+1)), g.Name, cDimmed(g.Channel))
	}
	fmt.Println()
	fmt.Printf("  %s %s  %s\n", cAccent("[↵]"), "Continue", cDimmed("current server"))
	fmt.Printf("\n  %s Choice: ", cAccent("›"))

	choice, err := readLineWithSignal(ctx, reader)
	if err != nil {
		fmt.Printf("\n  %s\n", cDimmed("Exiting."))
		os.Exit(0)
	}

	if choice != "" {
		var idx int
		if _, err := fmt.Sscanf(choice, "%d", &idx); err == nil && idx >= 1 && idx <= len(cfg.ConfiguredGuilds) {
			g := cfg.ConfiguredGuilds[idx-1]
			cfg.General.GuildID = g.ID
			cfg.General.GuildName = g.Name
			cfg.General.Channel = g.Channel
			_ = cfg.Save(configPath)
			fmt.Printf("  %s Switched to %s\n", cGreen+"✓"+cReset, cBoldW(g.Name))
		}
	}
	fmt.Println()
}

func runSetupRestart(configPath string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s config: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}
	if err := discord.EnsureUserConfig(context.Background(), cfg, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "%s auth: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}
	auth := discord.New(cfg)
	if err := setup.RunSetup(context.Background(), cfg, configPath, auth, true); err != nil {
		fmt.Fprintf(os.Stderr, "%s setup: %v\n", cAccent("✗"), err)
	}

	// Reload config to get the latest configured guilds
	cfg, err = config.Load()
	if err == nil && len(cfg.ConfiguredGuilds) >= 1 {
		showServerPicker(cfg, configPath)
	}

	execPath, _ := os.Executable()
	_ = syscall.Exec(execPath, os.Args, os.Environ())
}

func runServerPrompt(configPath string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s config: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}

	if len(cfg.ConfiguredGuilds) <= 1 {
		return
	}

	showServerPicker(cfg, configPath)

	execPath, _ := os.Executable()
	_ = syscall.Exec(execPath, os.Args, os.Environ())
}

func runGlobalPrompt(configPath string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s config: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}

	clearScreen()
	printGreeting()
	fmt.Printf("  %s %s\n", cDimmed("•"), cBoldW("Discover Servers"))
	fmt.Println()

	auth := discord.New(cfg)
	userGuilds, err := auth.FetchUserGuilds(context.Background(), cfg.Auth.Discord.AccessToken)
	if err != nil {
		fmt.Printf("  %s Failed to fetch your servers: %v\n\n", cAccent("✗"), err)
		os.Exit(1)
	}
	userGuildMap := make(map[string]string)
	for _, g := range userGuilds {
		userGuildMap[g.ID] = g.Name
	}

	cl := guilds.NewClient(cfg.Server.RelayURL, cfg.Server.APIKey)
	configuredGuilds, err := cl.FetchGuilds("")
	if err != nil {
		fmt.Printf("  %s Failed to fetch configured servers: %v\n\n", cAccent("✗"), err)
		os.Exit(1)
	}

	var available []guilds.Guild
	for _, g := range configuredGuilds {
		if realName, ok := userGuildMap[g.ID]; ok {
			g.Name = realName
			if g.Name == "" {
				g.Name = "Unknown Server"
			}
			available = append(available, g)
		}
	}

	if len(available) == 0 {
		fmt.Printf("  %s\n\n", cDimmed("No configured servers found that you are a member of."))
		os.Exit(0)
	}

	alreadyConfigured := make(map[string]bool)
	for _, g := range cfg.ConfiguredGuilds {
		alreadyConfigured[g.ID] = true
	}

	for i, g := range available {
		marker := "  "
		if alreadyConfigured[g.ID] {
			marker = "+ "
		}
		fmt.Printf("    %s%s %s\n", marker, cAccent(fmt.Sprintf("[%d]", i+1)), g.Name)
	}
	fmt.Println()
	fmt.Printf("  %s %s  %s\n", cAccent("[↵]"), "Cancel", cDimmed("return to chat"))
	fmt.Printf("\n  %s Type number to add a server: ", cAccent("›"))

	reader := bufio.NewReader(os.Stdin)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	choice, err := readLineWithSignal(ctx, reader)
	if err != nil {
		fmt.Printf("\n  %s\n", cDimmed("Exiting."))
		os.Exit(0)
	}

	if choice != "" {
		var idx int
		if _, err := fmt.Sscanf(choice, "%d", &idx); err == nil && idx >= 1 && idx <= len(available) {
			g := available[idx-1]

			if !alreadyConfigured[g.ID] {
				entry := config.GuildEntry{
					ID:         g.ID,
					Name:       g.Name,
					Channel:    cfg.General.Channel,
					Configured: true,
				}
				if entry.Channel == "" {
					entry.Channel = "general"
				}
				cfg.ConfiguredGuilds = append(cfg.ConfiguredGuilds, entry)
			}

			cfg.General.GuildID = g.ID
			cfg.General.GuildName = g.Name
			if cfg.General.Channel == "" {
				cfg.General.Channel = "general"
			}
			_ = cfg.Save(configPath)
			fmt.Printf("  %s Joined %s\n", cGreen+"✓"+cReset, cBoldW(g.Name))
		}
	}
	fmt.Println()

	execPath, _ := os.Executable()
	_ = syscall.Exec(execPath, os.Args, os.Environ())
}
