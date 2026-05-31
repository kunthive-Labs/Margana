package discord

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/kunthive-Labs/Margana/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAuthorizationURLIncludesDiscordOAuthFields(t *testing.T) {
	auth := &Authenticator{
		ClientID:    "12345",
		RedirectURL: "http://127.0.0.1:53682/callback",
	}

	raw := auth.AuthorizationURL("state-1", "challenge-1")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	q := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "discord.com" {
		t.Fatalf("expected discord authorization host, got %s", parsed.Host)
	}
	if q.Get("client_id") != "12345" {
		t.Fatalf("expected client_id to be set, got %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:53682/callback" {
		t.Fatalf("expected redirect_uri to be set, got %q", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "identify guilds" {
		t.Fatalf("expected identify guilds scope, got %q", q.Get("scope"))
	}
	if q.Get("response_type") != "code" {
		t.Fatalf("expected response_type=code, got %q", q.Get("response_type"))
	}
	if q.Get("state") != "state-1" {
		t.Fatalf("expected state to be set, got %q", q.Get("state"))
	}
	if q.Get("code_challenge") != "challenge-1" {
		t.Fatalf("expected code_challenge to be set, got %q", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("expected code_challenge_method=S256, got %q", q.Get("code_challenge_method"))
	}
}

func TestRandomCodeVerifierFormat(t *testing.T) {
	verifier, err := randomCodeVerifier()
	if err != nil {
		t.Fatalf("randomCodeVerifier: %v", err)
	}

	if len(verifier) != base64.RawURLEncoding.EncodedLen(pkceEntropy) {
		t.Fatalf("expected verifier length %d, got %d", base64.RawURLEncoding.EncodedLen(pkceEntropy), len(verifier))
	}
	if strings.Contains(verifier, "=") {
		t.Fatalf("verifier must not use base64 padding, got %q", verifier)
	}
	for _, ch := range verifier {
		isLetter := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
		isDigit := ch >= '0' && ch <= '9'
		if !(isLetter || isDigit || ch == '-' || ch == '_') {
			t.Fatalf("verifier contains invalid PKCE character %q", ch)
		}
	}
}

func TestCodeChallengeS256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := codeChallengeS256(verifier); got != want {
		t.Fatalf("expected challenge %q, got %q", want, got)
	}
}

func TestExchangeCodeIncludesCodeVerifierForPublicClient(t *testing.T) {
	var form url.Values
	auth := &Authenticator{
		ClientID:    "client-id",
		RedirectURL: "http://127.0.0.1:53682/callback",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost {
					t.Fatalf("expected POST request, got %s", req.Method)
				}
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				form, err = url.ParseQuery(string(body))
				if err != nil {
					t.Fatalf("parse request body as form: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"access_token":"access-token",
						"refresh_token":"refresh-token",
						"token_type":"Bearer",
						"expires_in":3600,
						"scope":"identify guilds"
					}`)),
					Header: make(http.Header),
				}, nil
			}),
		},
	}

	token, err := auth.exchangeCode(context.Background(), "oauth-code", "pkce-verifier")
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	if token.AccessToken != "access-token" {
		t.Fatalf("expected access token in response, got %q", token.AccessToken)
	}
	if form.Get("grant_type") != "authorization_code" {
		t.Fatalf("expected grant_type=authorization_code, got %q", form.Get("grant_type"))
	}
	if form.Get("code") != "oauth-code" {
		t.Fatalf("expected code to be sent, got %q", form.Get("code"))
	}
	if form.Get("redirect_uri") != auth.RedirectURL {
		t.Fatalf("expected redirect_uri %q, got %q", auth.RedirectURL, form.Get("redirect_uri"))
	}
	if form.Get("client_id") != auth.ClientID {
		t.Fatalf("expected client_id %q, got %q", auth.ClientID, form.Get("client_id"))
	}
	if form.Get("code_verifier") != "pkce-verifier" {
		t.Fatalf("expected code_verifier to be sent, got %q", form.Get("code_verifier"))
	}
	if form.Get("client_secret") != "" {
		t.Fatalf("did not expect client_secret for public client, got %q", form.Get("client_secret"))
	}
}

func TestExchangeCodeIgnoresClientSecretWhenConfigured(t *testing.T) {
	var form url.Values
	auth := &Authenticator{
		ClientID:     "client-id",
		ClientSecret: "private-secret",
		RedirectURL:  "http://127.0.0.1:53682/callback",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				form, err = url.ParseQuery(string(body))
				if err != nil {
					t.Fatalf("parse request body as form: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"access_token":"access-token",
						"refresh_token":"refresh-token",
						"token_type":"Bearer",
						"expires_in":3600,
						"scope":"identify guilds"
					}`)),
					Header: make(http.Header),
				}, nil
			}),
		},
	}

	if _, err := auth.exchangeCode(context.Background(), "oauth-code", "pkce-verifier"); err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	if form.Get("client_secret") != "" {
		t.Fatalf("did not expect client_secret in CLI token request, got %q", form.Get("client_secret"))
	}
}

func TestRefreshSendsClientSecretWhenConfigured(t *testing.T) {
	var form url.Values
	auth := &Authenticator{
		ClientID:     "client-id",
		ClientSecret: "private-secret",
		RedirectURL:  "http://127.0.0.1:53682/callback",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == http.MethodPost && req.URL.String() == tokenURL:
					body, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read request body: %v", err)
					}
					form, err = url.ParseQuery(string(body))
					if err != nil {
						t.Fatalf("parse request body as form: %v", err)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader(`{
							"access_token":"access-token",
							"refresh_token":"refresh-token",
							"token_type":"Bearer",
							"expires_in":3600,
							"scope":"identify guilds"
						}`)),
						Header: make(http.Header),
					}, nil
				case req.Method == http.MethodGet && req.URL.String() == userURL:
					return &http.Response{
						StatusCode: http.StatusOK,
						Body: io.NopCloser(strings.NewReader(`{
							"id":"1",
							"username":"tester"
						}`)),
						Header: make(http.Header),
					}, nil
				default:
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
					return nil, nil
				}
			}),
		},
	}

	session, err := auth.Refresh(context.Background(), "refresh-token")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if session.AccessToken != "access-token" {
		t.Fatalf("expected access token in session, got %q", session.AccessToken)
	}
	if form.Get("grant_type") != "refresh_token" {
		t.Fatalf("expected grant_type=refresh_token, got %q", form.Get("grant_type"))
	}
	if form.Get("refresh_token") != "refresh-token" {
		t.Fatalf("expected refresh_token to be sent, got %q", form.Get("refresh_token"))
	}
	if form.Get("client_id") != "client-id" {
		t.Fatalf("expected client_id to be sent, got %q", form.Get("client_id"))
	}
	if form.Get("client_secret") != "private-secret" {
		t.Fatalf("expected client_secret in CLI refresh request, got %q", form.Get("client_secret"))
	}
}

func TestApplyDiscordIdentityUsesDiscordUsernameOverGlobalName(t *testing.T) {
	cfg := config.Default()

	applyDiscordIdentity(cfg, User{
		ID:         "1",
		Username:   "discorduser",
		GlobalName: "Display Name",
		Avatar:     "hash",
	})

	if cfg.General.Username != "discorduser" {
		t.Fatalf("expected discord username to become terminal username, got %q", cfg.General.Username)
	}
	if cfg.General.DiscordGlobalName != "Display Name" {
		t.Fatalf("expected global_name to be preserved as alias, got %q", cfg.General.DiscordGlobalName)
	}
	if cfg.General.DiscordAvatarURL == "" {
		t.Fatal("expected avatar URL to be populated")
	}
}

func TestApplyDiscordIdentityFallsBackToGlobalNameWhenUsernameMissing(t *testing.T) {
	cfg := config.Default()

	applyDiscordIdentity(cfg, User{
		ID:         "1",
		GlobalName: "Display Name",
	})

	if cfg.General.Username != "Display Name" {
		t.Fatalf("expected global_name fallback, got %q", cfg.General.Username)
	}
}

func TestSaveConfigMergedPreservesDefaultFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.toml"

	// Create a minimal config file on disk simulating the bug
	initialTOML := `[server]
websocket_url = "ws://custom-websocket"
relay_url = "http://custom-relay"
web_setup_url = ""
`
	if err := os.WriteFile(configPath, []byte(initialTOML), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Prepare a config struct
	cfg := config.Default()
	cfg.Auth.Discord.AccessToken = "test-token"
	cfg.Auth.Discord.ClientID = "test-client-id" // required now that there is no bundled default

	// Run saveConfigMerged
	if err := saveConfigMerged(cfg, configPath); err != nil {
		t.Fatalf("saveConfigMerged: %v", err)
	}

	// Load the config back from disk using standard config.Load
	os.Args = []string{"marga", "-c", configPath}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Verify that the custom values are preserved
	if loaded.Server.WebsocketURL != "ws://custom-websocket" {
		t.Errorf("expected WebsocketURL to be 'ws://custom-websocket', got %q", loaded.Server.WebsocketURL)
	}
	if loaded.Server.RelayURL != "http://custom-relay" {
		t.Errorf("expected RelayURL to be 'http://custom-relay', got %q", loaded.Server.RelayURL)
	}

	// web_setup_url has no bundled default anymore; the explicit empty value in
	// the file is preserved (not silently replaced with a hosted placeholder).
	if loaded.Server.WebSetupURL != "" {
		t.Errorf("expected WebSetupURL to stay empty (no default), got %q", loaded.Server.WebSetupURL)
	}
	if loaded.Auth.Discord.AccessToken != "test-token" {
		t.Errorf("expected AccessToken to be 'test-token', got %q", loaded.Auth.Discord.AccessToken)
	}
}
