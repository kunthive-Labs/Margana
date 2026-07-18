// Command relay is Marga's self-hostable reference relay. It speaks the same
// wire contract Marga's TUI expects — a WebSocket event stream plus REST
// send/history APIs — and ships a local echo backend, so `docker compose up`
// yields a working, ToS-safe chat relay with message history, a retention
// window, and a delete-my-data endpoint. No Discord account, bot token, or
// gateway connection is involved; the production Discord bridge lives in the
// separate kunthive-Labs/marga-discord-relay repository.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kunthive-Labs/Margana/internal/relay"
)

type config struct {
	listenAddr     string
	apiKey         string
	dbPath         string
	retention      time.Duration
	backend        string
	defaultChannel string
}

func main() {
	cfg := loadConfig()

	logger := log.New(os.Stderr, "relay ", log.LstdFlags|log.Lmsgprefix)

	store, err := relay.NewStore(cfg.dbPath)
	if err != nil {
		logger.Fatalf("opening store: %v", err)
	}
	defer func() { _ = store.Close() }()

	hub := relay.NewHub(logger)

	var backend relay.Backend
	switch strings.ToLower(cfg.backend) {
	case "", "local":
		backend = relay.NewLocalBackend(store, hub, cfg.defaultChannel)
	case "discord":
		backend = relay.NewDiscordStub()
		logger.Printf("backend=discord selected, but the Discord bridge is not built into this reference relay; run kunthive-Labs/marga-discord-relay for a live bridge")
	default:
		logger.Fatalf("unknown RELAY_BACKEND %q (want \"local\" or \"discord\")", cfg.backend)
	}

	srv := relay.NewServer(store, hub, backend, cfg.apiKey, cfg.retention, logger)

	httpServer := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Retention pruner: periodically delete rows past the window. Read-path
	// filtering already hides such rows; this reclaims disk. 0 keeps forever.
	if cfg.retention > 0 {
		go runPruner(ctx, store, cfg.retention, logger)
	}

	logger.Printf("listening on %s (backend=%s, auth=%v, db=%s, retention=%s)",
		cfg.listenAddr, strings.ToLower(cfg.backend), cfg.apiKey != "", cfg.dbPath, retentionLabel(cfg.retention))

	errCh := make(chan error, 1)
	go func() {
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case serveErr := <-errCh:
		logger.Fatalf("http server: %v", serveErr)
	case <-ctx.Done():
		logger.Printf("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func loadConfig() config {
	listenAddr := flag.String("listen", envOr("LISTEN_ADDR", ":8443"), "address to listen on (env LISTEN_ADDR)")
	apiKey := flag.String("api-key", os.Getenv("API_KEY"), "required X-API-Key value; empty disables auth (env API_KEY)")
	dbPath := flag.String("db", envOr("RELAY_DB_PATH", "relay.db"), "path to the SQLite database (env RELAY_DB_PATH)")
	retentionStr := flag.String("retention", envOr("RELAY_RETENTION", "0"), "message retention window as a Go duration; 0 keeps forever (env RELAY_RETENTION)")
	backendName := flag.String("backend", envOr("RELAY_BACKEND", "local"), "backend: local (echo) | discord (stub) (env RELAY_BACKEND)")
	defaultChannel := flag.String("default-channel", envOr("RELAY_DEFAULT_CHANNEL", "general"), "channel advertised before any message is sent (env RELAY_DEFAULT_CHANNEL)")
	flag.Parse()

	retention, err := parseRetention(*retentionStr)
	if err != nil {
		log.Fatalf("invalid retention %q: %v", *retentionStr, err)
	}

	return config{
		listenAddr:     *listenAddr,
		apiKey:         *apiKey,
		dbPath:         *dbPath,
		retention:      retention,
		backend:        *backendName,
		defaultChannel: *defaultChannel,
	}
}

// parseRetention accepts a Go duration; "", "0", and "0s" all mean "keep
// forever" (retention disabled).
func parseRetention(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, errors.New("retention must not be negative")
	}
	return d, nil
}

// runPruner sweeps expired rows on startup and then on a ticker sized to the
// window (bounded to [1m, 1h]).
func runPruner(ctx context.Context, store *relay.Store, retention time.Duration, logger *log.Logger) {
	prune := func() {
		if n, err := store.PruneOlderThan(retention); err != nil {
			logger.Printf("prune: %v", err)
		} else if n > 0 {
			logger.Printf("pruned %d message(s) older than %s", n, retention)
		}
	}

	prune()

	interval := retention / 6
	if interval < time.Minute {
		interval = time.Minute
	}
	if interval > time.Hour {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}

func retentionLabel(d time.Duration) string {
	if d <= 0 {
		return "disabled"
	}
	return d.String()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
