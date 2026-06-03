// Package discord implements Discord OAuth2 (authorization-code flow with PKCE)
// over a loopback redirect, plus token refresh and the user/guild lookups Marga
// needs to identify the signed-in account. Tokens are persisted in the OS
// keyring by the config package, not here.
package discord

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/kunthive-Labs/Margana/internal/config"
)

const (
	authorizeURL  = "https://discord.com/oauth2/authorize"
	tokenURL      = "https://discord.com/api/oauth2/token"
	userURL       = "https://discord.com/api/v10/users/@me"
	guildsURL     = "https://discord.com/api/v10/users/@me/guilds"
	scopeIdentify = "identify guilds"
	pkceEntropy   = 32
)

type Authenticator struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
	OpenURL      func(string) error
	Notify       func(string)
}

type Session struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresAt    time.Time
	User         User
}

type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	GlobalName    string `json:"global_name"`
	Avatar        string `json:"avatar"`
	Discriminator string `json:"discriminator"`
	Locale        string `json:"locale"`
}

type Guild struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Owner       bool   `json:"owner"`
	Permissions string `json:"permissions"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type callbackResult struct {
	Code  string
	State string
	Err   string
}

func EnsureUserConfig(ctx context.Context, cfg *config.Config, configPath string) error {
	if !cfg.UsesDiscordAuth() {
		return nil
	}

	auth := New(cfg)

	user, err := auth.FetchCurrentUser(ctx, cfg.Auth.Discord.AccessToken)
	switch {
	case err == nil && !needsReauth(cfg.Auth.Discord.Scope):
		applyDiscordIdentity(cfg, user)
	case needsReauth(cfg.Auth.Discord.Scope):
		session, authErr := auth.Authenticate(ctx)
		if authErr != nil {
			return fmt.Errorf("authenticating with discord (scope upgrade): %w", authErr)
		}
		applySession(cfg, session)
	case cfg.Auth.Discord.RefreshToken != "":
		session, refreshErr := auth.Refresh(ctx, cfg.Auth.Discord.RefreshToken)
		if refreshErr != nil {
			return fmt.Errorf("refreshing discord token: %w (original fetch error: %v)", refreshErr, err)
		}
		applySession(cfg, session)
	default:
		session, authErr := auth.Authenticate(ctx)
		if authErr != nil {
			return fmt.Errorf("authenticating with discord: %w", authErr)
		}
		applySession(cfg, session)
	}

	return saveConfigMerged(cfg, configPath)
}

func needsReauth(scope string) bool {
	return !strings.Contains(scope, "guilds")
}

func New(cfg *config.Config) *Authenticator {
	return &Authenticator{
		ClientID:     cfg.Auth.Discord.ClientID,
		ClientSecret: cfg.Auth.Discord.ClientSecret,
		RedirectURL:  cfg.Auth.Discord.RedirectURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		OpenURL: openBrowser,
		Notify:  func(msg string) { fmt.Printf("  \033[2m•\033[0m %s\n", msg) },
	}
}

func (a *Authenticator) Authenticate(ctx context.Context) (*Session, error) {
	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("generating oauth state: %w", err)
	}
	codeVerifier, err := randomCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generating pkce code verifier: %w", err)
	}
	codeChallenge := codeChallengeS256(codeVerifier)

	redirect, err := url.Parse(a.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("parsing redirect url: %w", err)
	}
	if redirect.Scheme != "http" {
		return nil, fmt.Errorf("redirect url must use http loopback for terminal auth, got %q", redirect.Scheme)
	}

	addr := redirect.Host
	if !strings.Contains(addr, ":") {
		addr += ":80"
	}

	codeCh := make(chan callbackResult, 1)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != redirect.Path {
				http.NotFound(w, r)
				return
			}
			result := callbackResult{
				Code:  r.URL.Query().Get("code"),
				State: r.URL.Query().Get("state"),
				Err:   r.URL.Query().Get("error"),
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if result.Err != "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, "Discord authentication failed. You can close this tab.")
			} else {
				_, _ = io.WriteString(w, "Discord authentication complete. You can return to Marga.")
			}
			select {
			case codeCh <- result:
			default:
			}
		}),
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on redirect address %s: %w", addr, err)
	}
	defer ln.Close()

	go func() {
		_ = server.Serve(ln)
	}()
	defer func() { _ = server.Shutdown(context.Background()) }()

	authURL := a.AuthorizationURL(state, codeChallenge)
	if a.Notify != nil {
		a.Notify("\033[36mOpening Discord auth...\033[0m")
	}
	if a.OpenURL != nil {
		if err := a.OpenURL(authURL); err != nil {
			if a.Notify == nil {
				return nil, fmt.Errorf("opening browser for %s: %w", authURL, err)
			}
			a.Notify(fmt.Sprintf("\033[33m⚠\033[0m Browser failed — open manually:\n  \033[2m%s\033[0m", authURL))
		}
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-codeCh:
		if result.Err != "" {
			return nil, fmt.Errorf("discord returned oauth error %q", result.Err)
		}
		if result.State != state {
			return nil, errors.New("oauth state mismatch")
		}
		token, err := a.exchangeCode(ctx, result.Code, codeVerifier)
		if err != nil {
			return nil, err
		}
		user, err := a.FetchCurrentUser(ctx, token.AccessToken)
		if err != nil {
			return nil, err
		}
		if a.Notify != nil {
			a.Notify(fmt.Sprintf("\033[32m✓\033[0m Logged in as \033[1m\033[97m%s\033[0m", preferredTerminalUsername(user)))
		}
		return &Session{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			TokenType:    token.TokenType,
			Scope:        token.Scope,
			ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC(),
			User:         user,
		}, nil
	}
}

func (a *Authenticator) AuthorizationURL(state, codeChallenge string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", a.ClientID)
	q.Set("scope", scopeIdentify)
	q.Set("state", state)
	q.Set("redirect_uri", a.RedirectURL)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	return authorizeURL + "?" + q.Encode()
}

func (a *Authenticator) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", a.ClientID)
	// Discord requires client_secret on refresh_token grants for confidential
	// clients. Without it the token endpoint returns 401.
	if a.ClientSecret != "" {
		form.Set("client_secret", a.ClientSecret)
	}

	token, err := a.doTokenRequest(ctx, form)
	if err != nil {
		return nil, err
	}

	user, err := a.FetchCurrentUser(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}

	return &Session{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Scope:        token.Scope,
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC(),
		User:         user,
	}, nil
}

func (a *Authenticator) FetchCurrentUser(ctx context.Context, accessToken string) (User, error) {
	if accessToken == "" {
		return User{}, errors.New("missing access token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return User{}, fmt.Errorf("building user request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return User{}, fmt.Errorf("requesting discord user profile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return User{}, fmt.Errorf("discord user profile request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return User{}, fmt.Errorf("decoding discord user profile: %w", err)
	}

	return user, nil
}

func (a *Authenticator) FetchUserGuilds(ctx context.Context, accessToken string) ([]Guild, error) {
	if accessToken == "" {
		return nil, errors.New("missing access token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, guildsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building guilds request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting discord guilds: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("discord guilds request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var guilds []Guild
	if err := json.NewDecoder(resp.Body).Decode(&guilds); err != nil {
		return nil, fmt.Errorf("decoding discord guilds: %w", err)
	}

	return guilds, nil
}

const (
	permAdministrator = 0x8
	permManageGuild   = 0x20
)

func HasAdminAccess(guild Guild) bool {
	if guild.Owner {
		return true
	}
	var perms int64
	_, _ = fmt.Sscanf(guild.Permissions, "%d", &perms)
	return (perms&permAdministrator) != 0 || (perms&permManageGuild) != 0
}

func FilterAdminGuilds(guilds []Guild) []Guild {
	var filtered []Guild
	for _, g := range guilds {
		if HasAdminAccess(g) {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

func (a *Authenticator) exchangeCode(ctx context.Context, code, codeVerifier string) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", a.RedirectURL)
	form.Set("client_id", a.ClientID)
	form.Set("code_verifier", codeVerifier)
	return a.doTokenRequest(ctx, form)
}

func (a *Authenticator) doTokenRequest(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting discord token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("discord token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decoding discord token response: %w", err)
	}
	return &token, nil
}

func (a *Authenticator) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

func applySession(cfg *config.Config, session *Session) {
	cfg.Auth.Discord.AccessToken = session.AccessToken
	cfg.Auth.Discord.RefreshToken = session.RefreshToken
	cfg.Auth.Discord.TokenType = session.TokenType
	cfg.Auth.Discord.Scope = session.Scope
	cfg.Auth.Discord.Expiry = session.ExpiresAt.Format(time.RFC3339)
	applyDiscordIdentity(cfg, session.User)
}

func applyDiscordIdentity(cfg *config.Config, user User) {
	cfg.General.DiscordID = user.ID
	cfg.General.DiscordUsername = user.Username
	cfg.General.DiscordGlobalName = user.GlobalName
	cfg.General.DiscordAvatarURL = avatarURL(user.ID, user.Avatar)
	cfg.General.Username = preferredTerminalUsername(user)
}

func preferredTerminalUsername(user User) string {
	if user.Username != "" {
		return user.Username
	}
	return user.GlobalName
}

func avatarURL(userID, avatarHash string) string {
	if userID == "" || avatarHash == "" {
		return ""
	}
	return fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", userID, avatarHash)
}

func randomState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomCodeVerifier() (string, error) {
	buf := make([]byte, pkceEntropy)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func codeChallengeS256(codeVerifier string) string {
	sum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
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

func saveConfigMerged(cfg *config.Config, configPath string) error {
	existing := config.Default()
	existingData, err := os.ReadFile(configPath)
	if err == nil {
		if _, err := toml.Decode(string(existingData), existing); err == nil {
			cfg.Server = existing.Server
			cfg.UI = existing.UI
			cfg.Github = existing.Github
			cfg.ConfiguredGuilds = existing.ConfiguredGuilds
			cfg.General.Channel = existing.General.Channel
			cfg.General.GuildID = existing.General.GuildID
			cfg.General.GuildName = existing.General.GuildName
		}
	}
	cfg.ApplyDefaults()
	return cfg.Save(configPath)
}
