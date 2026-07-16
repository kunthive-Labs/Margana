// Command marga is the entry point for Marga, a terminal-native, multi-network
// chat client. It loads configuration, runs first-run setup if needed, opens
// the local store, connects each enabled network adapter, and launches the TUI.
// Run `marga --help` for command-line flags.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/kunthive-Labs/Margana/internal/auth/discord"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/db"
	"github.com/kunthive-Labs/Margana/internal/guilds"
	"github.com/kunthive-Labs/Margana/internal/logging"
	"github.com/kunthive-Labs/Margana/internal/network"
	"github.com/kunthive-Labs/Margana/internal/network/demo"
	"github.com/kunthive-Labs/Margana/internal/network/discordrelay"
	"github.com/kunthive-Labs/Margana/internal/network/matrix"
	"github.com/kunthive-Labs/Margana/internal/setup"
	"github.com/kunthive-Labs/Margana/internal/tui"
)

var version = "dev"

// resolveVersion returns the version string to display. It prefers the value
// stamped at build time via -ldflags "-X main.version=..." (goreleaser and
// `make build` both set it). When that is absent — most notably for
// `go install github.com/kunthive-Labs/Margana/cmd/marga@latest`, where the Go
// toolchain records the module version in the build info but does not set
// main.version — it falls back to the module version from the build info. The
// leading "v" is trimmed so output matches goreleaser's convention (0.1.0, not
// v0.1.0), and a plain `go build` of the source tree keeps reporting "dev".
func resolveVersion() string {
	if version != "dev" && version != "" {
		return strings.TrimPrefix(version, "v")
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}
	return version
}

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
		"                                             ",
		"                                             ",
		"                                             ",
		"88888b.d88b.  8888b. 888d888 .d88b.  8888b.  ",
		"888 \"888 \"88b    \"88b888P\"  d88P\"88b    \"88b ",
		"888  888  888.d888888888    888  888.d888888 ",
		"888  888  888888  888888    Y88b 888888  888 ",
		"888  888  888\"Y888888888     \"Y88888\"Y888888 ",
		"                                 888         ",
		"                            Y8b d88P         ",
		"                             \"Y88P\"          ",
	}
	for i, line := range logo {
		if i == 5 {
			fmt.Println("  " + cAccent("> ") + cBoldW(line))
		} else {
			fmt.Println("    " + cBoldW(line))
		}
	}
	fmt.Println()
	fmt.Printf("    %s %s\n", cDimmed("the terminal-native multi-network chat experience"), cGray+"v"+version+cReset)
	fmt.Println()
}

// cliFlags holds the parsed command-line options.
type cliFlags struct {
	setup    bool
	version  bool
	help     bool
	debug    bool
	demo     bool
	logFile  string
	logLevel string
}

// parseFlags parses Marga's command-line flags. It tolerates the --config/-c
// flag (consumed separately by the config loader) so both can coexist.
func parseFlags(args []string) (cliFlags, error) {
	var f cliFlags
	var configPath string // accepted here so -c/--config doesn't error; resolved by config.Load
	fs := flag.NewFlagSet("marga", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we print our own usage
	fs.BoolVar(&f.setup, "setup", false, "force the setup wizard")
	fs.BoolVar(&f.setup, "s", false, "force the setup wizard (shorthand)")
	fs.BoolVar(&f.version, "version", false, "print version and exit")
	fs.BoolVar(&f.version, "v", false, "print version and exit (shorthand)")
	fs.BoolVar(&f.help, "help", false, "print usage and exit")
	fs.BoolVar(&f.help, "h", false, "print usage and exit (shorthand)")
	fs.BoolVar(&f.debug, "debug", false, "enable debug logging to the default log file")
	fs.BoolVar(&f.demo, "demo", false, "run a scripted offline demo (no config or network needed)")
	fs.StringVar(&f.logFile, "log-file", "", "write logs to this file")
	fs.StringVar(&f.logLevel, "log-level", "", "log level: debug, info, warn, error")
	fs.StringVar(&configPath, "config", "", "path to config file")
	fs.StringVar(&configPath, "c", "", "path to config file (shorthand)")
	err := fs.Parse(args)
	return f, err
}

func printUsage() {
	fmt.Printf(`marga — terminal-native, multi-network chat (v%s)

Usage:
  marga [flags]

Flags:
  -c, --config PATH    path to config file (default: platform config dir)
  -s, --setup          force the interactive setup wizard
      --demo           run a scripted offline demo (no config or network needed)
      --debug          enable debug logging to the default log file
      --log-file PATH   write logs to PATH (enables logging)
      --log-level LVL   log level: debug, info, warn, error (default: info)
  -v, --version        print version and exit
  -h, --help           print this help and exit

Logging is off by default. Logs are written to a file (never the terminal,
which would corrupt the TUI). See docs/CONFIGURATION.md and
docs/TROUBLESHOOTING.md.
`, version)
}

// setupLogging resolves logging configuration (config file + env, overridden by
// flags) and opens the logger. A failure to open the log file is reported to
// stderr but never fatal: Marga continues with logging disabled.
func setupLogging(cfg *config.Config, flags cliFlags) *logging.Logger {
	defaultLogPath, _ := config.DefaultLogPath()
	opts := logging.Resolve(
		logging.Settings{Level: cfg.Logging.Level, File: cfg.Logging.File, Format: cfg.Logging.Format},
		logging.Flags{Debug: flags.debug, File: flags.logFile, Level: flags.logLevel},
		defaultLogPath,
	)
	l, err := logging.New(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s logging disabled: %v\n", cAccent("!"), err)
		return logging.Disabled()
	}
	return l
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
	version = resolveVersion()

	flags, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n\n", cAccent("✗"), err)
		printUsage()
		os.Exit(2)
	}
	if flags.help {
		printUsage()
		return
	}
	if flags.version {
		fmt.Printf("marga %s\n", version)
		return
	}
	if flags.demo || os.Getenv("MARGA_DEMO") != "" {
		runDemo(version)
		return
	}

	clearScreen()
	printGreeting()

	forceSetup := flags.setup

	configPath, err := config.ConfigPathFromArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s config: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		// A brand-new user has no config file yet, and the default config enables
		// Discord auth without relay endpoints, so Load's validation fails. Rather
		// than strand them, start onboarding from a clean slate with Discord
		// disabled; they choose Matrix (zero-setup) or Discord in the network-first
		// wizard. A validation error on an *existing* file is still fatal. Load
		// succeeds for env-only configs, so headless MARGA_* setups are preserved.
		if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
			cfg = config.Default()
			cfg.Auth.Enabled = false
			forceSetup = true
		} else {
			fmt.Fprintf(os.Stderr, "%s config: %v\n", cAccent("✗"), err)
			os.Exit(1)
		}
	}

	// Set up logging as early as the config allows. It is off unless the
	// operator opted in (--debug/--log-file or the [logging] config section).
	appLogger := setupLogging(cfg, flags)
	defer appLogger.Close()
	appLogger.Info("marga starting", "version", version, "config", configPath)

	// Returning Discord users get their token ensured/refreshed up front. New and
	// Matrix-only users skip this: EnsureUserConfig triggers Discord OAuth, which
	// must not run before the user has actually chosen Discord in the wizard.
	if !setup.NeedsOnboarding(cfg, forceSetup) {
		if err := discord.EnsureUserConfig(context.Background(), cfg, configPath); err != nil {
			fmt.Fprintf(os.Stderr, "%s auth: %v\n", cAccent("✗"), err)
			os.Exit(1)
		}
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

	// Build one adapter per enabled network. Discord is included only when it is
	// actually enabled, so a Matrix-only config needs no relay.
	var adapters []network.Network
	var active network.NetworkID
	for _, n := range cfg.EnabledNetworks() {
		switch n.Type {
		case "discord":
			d := discordrelay.New(cfg)
			d.SetLogger(appLogger.StdLogger("discord", slog.LevelInfo))
			adapters = append(adapters, d)
			active = d.ID() // prefer Discord as the initial active network
		case "matrix":
			statePath, _ := config.NetworkStatePath(n.ID, "sync")
			mx := matrix.New(n, statePath)
			mx.SetLogger(appLogger)
			adapters = append(adapters, mx)
		}
	}
	if len(adapters) == 0 {
		fmt.Fprintf(os.Stderr, "%s no networks enabled — configure Discord or add a [[networks]] entry\n", cAccent("✗"))
		os.Exit(1)
	}
	if active == "" {
		active = adapters[0].ID()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, a := range adapters {
		if err := a.Connect(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n", cAccent("!"), a.ID(), err)
			appLogger.Warn("network connect failed", "network", a.ID(), "err", err)
		} else {
			appLogger.Info("network connected", "network", a.ID())
		}
	}

	registry := commands.NewRegistry()
	registry.Register(commands.NewHelpCmd(registry))
	registry.Register(commands.NewJoinCmd())
	registry.Register(commands.NewNetworkCmd())
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
	tui.RegisterCustomThemes(cfg.Themes)
	tui.ApplyTheme(cfg.UI.Theme)

	model := tui.New(adapters, active, store, registry,
		cfg.General.Channel, cfg.General.Username, cfg.General.DiscordID,
		cfg.General.DiscordUsername, cfg.General.DiscordGlobalName, cfg.General.GuildName,
		cfg.ConfiguredGuilds,
		cfg.Auth.Discord.AccessToken, cfg.Server.BotClientID,
		configPath, cfg, version,
	)
	model = model.WithLogger(appLogger.StdLogger("tui", slog.LevelInfo))
	if cfg.Github.Repo != "" {
		model = model.WithGithub(cfg.Github.Repo, cfg.Github.Token)
	}
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithReportFocus())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		appLogger.Info("shutdown signal received, disconnecting networks")
		for _, a := range adapters {
			_ = a.Disconnect()
		}
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
	// Discord auth is handled inside the wizard when the user picks Discord, so
	// don't force it here — a Matrix-only user re-running setup must not be
	// dropped into Discord OAuth.
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

// runDemo launches Marga against scripted, offline demo networks — no config,
// credentials, or connectivity required. Used by `marga --demo` / MARGA_DEMO=1
// and by the vhs recording (docs/demo.tape) so the demo GIF can be generated in
// CI. It uses a throwaway history store and never touches the user's config.
func runDemo(version string) {
	dbPath := filepath.Join(os.TempDir(), "marga-demo.db")
	_ = os.Remove(dbPath)
	store, err := db.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s demo db: %v\n", cAccent("✗"), err)
		os.Exit(1)
	}
	defer store.Close()

	cfg := config.Default()
	cfg.General.Username = "you"
	cfg.General.Channel = "general"
	cfg.General.GuildName = "Demo"
	cfg.UI.CoachShown = true // skip the first-run overlay in the scripted demo

	adapters := demo.Adapters()
	active := adapters[0].ID()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, a := range adapters {
		_ = a.Connect(ctx)
	}

	registry := commands.NewRegistry()
	registry.Register(commands.NewHelpCmd(registry))
	registry.Register(commands.NewJoinCmd())
	registry.Register(commands.NewNetworkCmd())
	registry.Register(commands.NewHistoryCmd())
	registry.Register(commands.NewSearchCmd(store))
	registry.Register(commands.NewStatusCmd())
	registry.Register(commands.NewClearMentionsCmd())
	registry.Register(commands.NewQuitCmd())

	tui.ApplyTheme(cfg.UI.Theme)
	model := tui.New(adapters, active, store, registry,
		cfg.General.Channel, cfg.General.Username, "", "", "", cfg.General.GuildName,
		nil, "", "", "", cfg, version,
	)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithReportFocus())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		for _, a := range adapters {
			_ = a.Disconnect()
		}
		cancel()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", cAccent("✗"), err)
		os.Exit(1)
	}
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
