package panels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <link>https://example.com</link>
    <item>
      <title>First post</title>
      <link>https://example.com/1</link>
      <pubDate>Fri, 02 Jan 2026 15:04:05 -0700</pubDate>
    </item>
    <item>
      <title>Second post</title>
      <link>https://example.com/2</link>
      <pubDate>Thu, 01 Jan 2026 10:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

func TestRSSSourceFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	src := &rssSource{url: srv.URL}
	res := src.Fetch(context.Background())

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 items, got %d: %#v", len(res.Items), res.Items)
	}
	if res.Items[0].Primary != "First post" {
		t.Errorf("expected 'First post', got %q", res.Items[0].Primary)
	}
	if res.Items[1].Primary != "Second post" {
		t.Errorf("expected 'Second post', got %q", res.Items[1].Primary)
	}
	wantTS, _ := time.Parse(time.RFC1123Z, "Fri, 02 Jan 2026 15:04:05 -0700")
	if !res.Items[0].Timestamp.Equal(wantTS) {
		t.Errorf("expected parsed pubDate %v, got %v", wantTS, res.Items[0].Timestamp)
	}
	if res.Items[1].Timestamp.IsZero() {
		t.Error("expected RFC1123 (GMT) pubDate to parse, got zero time")
	}
}

func TestRSSSourceMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Unclosed tags: the decoder must return an error, not panic.
		_, _ = w.Write([]byte(`<rss><channel><item><title>oops`))
	}))
	defer srv.Close()

	src := &rssSource{url: srv.URL}
	res := src.Fetch(context.Background())
	if res.Err == nil {
		t.Fatalf("expected an error for malformed feed, got items %#v", res.Items)
	}
}

func TestRSSSourceHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	src := &rssSource{url: srv.URL}
	res := src.Fetch(context.Background())
	if res.Err == nil {
		t.Error("expected an error for non-200 response")
	}
}

func TestRSSSourceMissingPubDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<rss><channel><item><title>No date</title></item></channel></rss>`))
	}))
	defer srv.Close()

	src := &rssSource{url: srv.URL}
	res := src.Fetch(context.Background())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if len(res.Items) != 1 || res.Items[0].Primary != "No date" {
		t.Fatalf("expected one dateless item, got %#v", res.Items)
	}
	if !res.Items[0].Timestamp.IsZero() {
		t.Errorf("expected zero timestamp for missing pubDate, got %v", res.Items[0].Timestamp)
	}
}

func TestRSSSourceType(t *testing.T) {
	if (&rssSource{}).Type() != "rss" {
		t.Errorf("expected type 'rss'")
	}
}
