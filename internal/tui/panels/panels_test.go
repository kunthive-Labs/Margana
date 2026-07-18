package panels

import (
	"testing"
	"time"

	"github.com/kunthive-Labs/Margana/internal/config"
)

func TestNewSelectsSourceByType(t *testing.T) {
	tests := []struct {
		typ       string
		source    string
		wantTitle string
		wantID    string
	}{
		{"github", "kunthive-Labs/Margana", "GitHub: kunthive-Labs/Margana", "github:kunthive-Labs/Margana"},
		{"ci", "kunthive-Labs/Margana", "CI: kunthive-Labs/Margana", "ci:kunthive-Labs/Margana"},
		{"rss", "https://example.com/feed.xml", "RSS", "rss:https://example.com/feed.xml"},
	}
	for _, tc := range tests {
		t.Run(tc.typ, func(t *testing.T) {
			p, err := New(config.PanelConfig{Type: tc.typ, Source: tc.source})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if p.Source == nil || p.Source.Type() != tc.typ {
				t.Fatalf("expected source type %q, got %#v", tc.typ, p.Source)
			}
			if p.Title != tc.wantTitle {
				t.Errorf("expected default title %q, got %q", tc.wantTitle, p.Title)
			}
			if p.ID != tc.wantID {
				t.Errorf("expected id %q, got %q", tc.wantID, p.ID)
			}
			if p.Interval != defaultInterval {
				t.Errorf("expected default interval %v, got %v", defaultInterval, p.Interval)
			}
		})
	}
}

func TestNewTitleOverrideAndRefresh(t *testing.T) {
	p, err := New(config.PanelConfig{
		Type:    "github",
		Source:  "o/r",
		Title:   "My Repo",
		Refresh: "30s",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Title != "My Repo" {
		t.Errorf("expected overridden title, got %q", p.Title)
	}
	if p.Interval != 30*time.Second {
		t.Errorf("expected 30s interval, got %v", p.Interval)
	}
}

func TestNewErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.PanelConfig
	}{
		{"missing type", config.PanelConfig{Source: "o/r"}},
		{"missing source", config.PanelConfig{Type: "github"}},
		{"unknown type", config.PanelConfig{Type: "slack", Source: "x"}},
		{"bad refresh", config.PanelConfig{Type: "github", Source: "o/r", Refresh: "soon"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
