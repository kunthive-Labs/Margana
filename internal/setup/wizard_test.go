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

func TestNeedsOnboarding(t *testing.T) {
	guild := func() *config.Config { c := &config.Config{}; c.General.GuildID = "g1"; return c }
	cases := []struct {
		name  string
		cfg   *config.Config
		force bool
		want  bool
	}{
		{"nothing configured triggers onboarding", &config.Config{}, false, true},
		{"force always triggers", guild(), true, true},
		{"discord guild set skips", guild(), false, false},
		{"configured guild skips", &config.Config{ConfiguredGuilds: []config.GuildEntry{{ID: "g1"}}}, false, false},
		{"matrix network skips", &config.Config{Networks: []config.NetworkConfig{{ID: "matrix", Type: "matrix"}}}, false, false},
	}
	for _, tc := range cases {
		if got := NeedsOnboarding(tc.cfg, tc.force); got != tc.want {
			t.Errorf("%s: NeedsOnboarding = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeHomeserver(t *testing.T) {
	cases := map[string]string{
		"matrix.org":            "https://matrix.org",
		"https://matrix.org/":   "https://matrix.org",
		"http://localhost:8008": "http://localhost:8008",
		"  matrix.org  ":        "https://matrix.org",
		"":                      "",
	}
	for in, want := range cases {
		if got := normalizeHomeserver(in); got != want {
			t.Errorf("normalizeHomeserver(%q) = %q, want %q", in, got, want)
		}
	}
}
