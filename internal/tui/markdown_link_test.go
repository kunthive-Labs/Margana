package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestHyperlinkZeroWidth guards the invariant that OSC 8 hyperlink escapes are
// zero-width to lipgloss.Width. wrapText/messageLineCount measure with it, so if
// the escapes counted as width, link lines would be mis-wrapped and corrupted.
func TestHyperlinkZeroWidth(t *testing.T) {
	label := "example"
	h := renderHyperlink("https://example.com/very/long/path", label)
	if got := lipgloss.Width(h); got != len(label) {
		t.Fatalf("OSC 8 hyperlink must have visible width %d, got %d", len(label), got)
	}
	if !strings.Contains(h, "\x1b]8;;https://example.com/very/long/path\x1b\\") {
		t.Fatalf("expected OSC 8 open sequence carrying the URL, got %q", h)
	}
}

func TestMarkdownLinkLabel(t *testing.T) {
	result := renderInline("see [the docs](https://example.com/docs) here", "")
	if !strings.Contains(result, "the docs") {
		t.Fatalf("expected link label 'the docs', got %q", result)
	}
	if strings.Contains(result, "](") {
		t.Fatalf("markdown link syntax should be consumed, got %q", result)
	}
	if !strings.Contains(result, "https://example.com/docs") {
		t.Fatalf("expected the URL in the OSC 8 sequence, got %q", result)
	}
}

func TestBareURLStillRenders(t *testing.T) {
	result := renderInline("go to https://example.com now", "")
	if !strings.Contains(result, "https://example.com") {
		t.Fatalf("expected bare URL to render, got %q", result)
	}
}

func TestMalformedMarkdownLinkPassesThrough(t *testing.T) {
	result := renderInline("array[0] index", "")
	if !strings.Contains(result, "array[0] index") {
		t.Fatalf("expected literal '[' to pass through, got %q", result)
	}
}
