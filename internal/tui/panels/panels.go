// Package panels provides Marga's configurable ambient sidebar panels. Each
// panel has a Source that fetches Items on a refresh interval. Sources are pure
// data fetchers — they perform HTTP + parsing only and never import lipgloss or
// otherwise render — so every Source is independently unit-testable. The TUI
// owns rendering and scheduling; this package owns "what to show".
package panels

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kunthive-Labs/Margana/internal/config"
)

// Item is a single row in a panel: a Primary headline, an optional Secondary
// metadata line, and a Timestamp used for display (and, for some sources,
// ordering).
type Item struct {
	Primary   string
	Secondary string
	Timestamp time.Time
}

// Result is the outcome of one Source.Fetch: the parsed Items, an optional
// non-fatal Warning (e.g. an API rate-limit notice), and a fatal Err set when
// the fetch or parse failed. On Err the Items are typically empty.
type Result struct {
	Items   []Item
	Warning string
	Err     error
}

// Source fetches a panel's Items. Implementations must be render-free (no
// lipgloss) so they can be exercised directly in tests.
type Source interface {
	// Type reports the built-in source kind ("github", "rss", "ci").
	Type() string
	// Fetch performs one refresh, honoring ctx for cancellation/timeout.
	Fetch(ctx context.Context) Result
}

// Panel is a configured, immutable panel: a stable ID, a display Title, its
// refresh Interval, and the data Source that backs it.
type Panel struct {
	ID       string
	Title    string
	Interval time.Duration
	Source   Source
}

// maxItems caps how many entries a source returns; the sidebar is small.
const maxItems = 10

// defaultInterval is used when a panel config omits (or zeroes) refresh.
const defaultInterval = 60 * time.Second

// defaultClient is shared by all sources that don't inject their own client.
// All network access uses a 10s timeout per the panel spec.
var defaultClient = &http.Client{Timeout: 10 * time.Second}

func clientOr(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return defaultClient
}

// New builds a Panel from a config entry, selecting the concrete Source by
// type. An empty source, an unparseable refresh, or an unknown type is an
// error. The default per-type Title can be overridden by cfg.Title.
func New(cfg config.PanelConfig) (Panel, error) {
	typ := strings.TrimSpace(cfg.Type)
	source := strings.TrimSpace(cfg.Source)
	if typ == "" {
		return Panel{}, fmt.Errorf("panel: type is required")
	}
	if source == "" {
		return Panel{}, fmt.Errorf("panel %q: source is required", typ)
	}

	interval := defaultInterval
	if r := strings.TrimSpace(cfg.Refresh); r != "" {
		d, err := time.ParseDuration(r)
		if err != nil {
			return Panel{}, fmt.Errorf("panel %q: invalid refresh %q: %w", typ, cfg.Refresh, err)
		}
		if d > 0 {
			interval = d
		}
	}

	var (
		src   Source
		title string
	)
	switch typ {
	case "github":
		src = &githubSource{repo: source, token: cfg.Token}
		title = "GitHub: " + source
	case "ci":
		src = &ciSource{repo: source, token: cfg.Token}
		title = "CI: " + source
	case "rss":
		src = &rssSource{url: source}
		title = "RSS"
	default:
		return Panel{}, fmt.Errorf("unknown panel type %q", typ)
	}

	if t := strings.TrimSpace(cfg.Title); t != "" {
		title = t
	}

	return Panel{
		ID:       typ + ":" + source,
		Title:    title,
		Interval: interval,
		Source:   src,
	}, nil
}
