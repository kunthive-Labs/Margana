package setup

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kunthive-Labs/Margana/internal/config"
)

func TestReadLineTrimsAndReturns(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("  hello world  \n"))
	got, err := readLine(context.Background(), r)
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if got != "hello world" {
		t.Errorf("readLine = %q, want trimmed 'hello world'", got)
	}
}

func TestReadLineRespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Reader that would block forever if consulted.
	r := bufio.NewReader(strings.NewReader(""))
	if _, err := readLine(ctx, r); err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestPollBotPresence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "key" {
			t.Errorf("expected API key header, got %q", r.Header.Get("X-API-Key"))
		}
		_, _ = w.Write([]byte(`{"bot_in_guild":true}`))
	}))
	defer srv.Close()

	in, err := pollBotPresence(context.Background(), srv.URL, "key")
	if err != nil {
		t.Fatalf("pollBotPresence: %v", err)
	}
	if !in {
		t.Error("expected bot_in_guild=true")
	}
}

func TestPollBotPresenceFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"bot_in_guild":false}`))
	}))
	defer srv.Close()

	in, err := pollBotPresence(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("pollBotPresence: %v", err)
	}
	if in {
		t.Error("expected bot_in_guild=false")
	}
}

func TestFetchWebConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/setup/config/user-123") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"guild_id":"g1","guild_name":"Alpha","channel_id":"c1","channel_name":"general"}`))
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.General.DiscordID = "user-123"
	cfg.Server.RelayURL = srv.URL

	got, ok := fetchWebConfig(cfg, &WizardState{})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.GuildID != "g1" || got.GuildName != "Alpha" || got.ChannelName != "general" {
		t.Errorf("unexpected web config: %+v", got)
	}
}

func TestFetchWebConfigRequiresDiscordID(t *testing.T) {
	cfg := &config.Config{}
	if _, ok := fetchWebConfig(cfg, &WizardState{}); ok {
		t.Error("expected ok=false when DiscordID is empty")
	}
}

func TestFetchWebConfigNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.General.DiscordID = "user-123"
	cfg.Server.RelayURL = srv.URL
	if _, ok := fetchWebConfig(cfg, &WizardState{}); ok {
		t.Error("expected ok=false when server reports ok:false")
	}
}
