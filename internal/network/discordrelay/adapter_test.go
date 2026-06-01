package discordrelay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/network"
)

// the adapter must satisfy both the base interface and the catch-up poller.
var (
	_ network.Network      = (*Adapter)(nil)
	_ network.SinceFetcher = (*Adapter)(nil)
)

func newTestConfig(relayURL string) *config.Config {
	cfg := &config.Config{}
	cfg.Server.RelayURL = relayURL
	cfg.Server.WebsocketURL = "wss://relay.example/ws"
	cfg.Server.APIKey = "key"
	cfg.General.Username = "alice"
	cfg.General.DiscordID = "42"
	cfg.General.DiscordGlobalName = "Alice"
	cfg.General.DiscordAvatarURL = "https://cdn/avatar.png"
	return cfg
}

func TestAdapterIdentityAndCapabilities(t *testing.T) {
	a := New(newTestConfig(""))

	if a.ID() != "discord" {
		t.Errorf("ID() = %q, want discord", a.ID())
	}
	caps := a.Capabilities()
	if !caps.Edit || !caps.FileUpload || !caps.Typing || !caps.History || !caps.ServerList {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
	if caps.Reactions {
		t.Error("Reactions not supported yet, should be false")
	}
	if caps.Encryption {
		t.Error("relay transport has no per-message E2EE, Encryption should be false")
	}

	id := a.CurrentUser()
	if id.Network != "discord" || id.UserID != "42" || id.Username != "alice" || id.DisplayName != "Alice" {
		t.Errorf("identity mapping wrong: %+v", id)
	}
}

func TestAdapterListServers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/guilds") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"guilds":[{"id":"g1","name":"Alpha"},{"id":"g2","name":"Beta"}]}`))
	}))
	defer srv.Close()

	a := New(newTestConfig(srv.URL))
	servers, err := a.ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(servers) != 2 || servers[0].ID != "g1" || servers[1].Name != "Beta" {
		t.Errorf("unexpected servers: %+v", servers)
	}
}

func TestAdapterListChannelsStampsNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"channels":[{"name":"general","type":"text"},{"name":"random"}]}`))
	}))
	defer srv.Close()

	a := New(newTestConfig(srv.URL))
	refs, err := a.ListChannels(context.Background(), "g1")
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 channels, got %d: %+v", len(refs), refs)
	}
	for _, ref := range refs {
		if ref.Network != "discord" {
			t.Errorf("channel ref not stamped with network: %+v", ref)
		}
		if ref.ServerID != "g1" {
			t.Errorf("channel ref server id = %q, want g1", ref.ServerID)
		}
		// For the relay, the channel name doubles as its native id.
		if ref.ID != ref.Name {
			t.Errorf("expected ID==Name for relay channels, got %+v", ref)
		}
	}
}
