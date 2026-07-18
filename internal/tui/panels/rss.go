package panels

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// rssSource parses an RSS 2.0 feed and surfaces its most recent items. It uses
// only the stdlib encoding/xml decoder (no third-party feed library).
type rssSource struct {
	url    string
	client *http.Client // defaults to the shared 10s client; set in tests
}

func (s *rssSource) Type() string { return "rss" }

// rssDocument maps the subset of RSS 2.0 that a panel needs:
// <rss><channel><item><title/><link/><pubDate/></item></channel></rss>.
// XMLName is intentionally omitted so the root element name is not enforced.
type rssDocument struct {
	Channel struct {
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func (s *rssSource) Fetch(ctx context.Context) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return Result{Err: err}
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")
	resp, err := clientOr(s.client).Do(req)
	if err != nil {
		return Result{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{Err: fmt.Errorf("rss: HTTP %d", resp.StatusCode)}
	}

	var doc rssDocument
	dec := xml.NewDecoder(resp.Body)
	// Some feeds declare non-UTF-8 charsets; pass those bytes through unchanged
	// rather than failing outright.
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	if err := dec.Decode(&doc); err != nil {
		return Result{Err: fmt.Errorf("rss: %w", err)}
	}

	items := make([]Item, 0, len(doc.Channel.Items))
	for i, it := range doc.Channel.Items {
		if i >= maxItems {
			break
		}
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = strings.TrimSpace(it.Link)
		}
		items = append(items, Item{
			Primary:   title,
			Secondary: "",
			Timestamp: parseRSSTime(it.PubDate),
		})
	}
	return Result{Items: items}
}

// parseRSSTime tolerates the several date layouts feeds use in the wild,
// returning the zero time when none match (the item is still shown, just
// without a timestamp line).
func parseRSSTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC1123Z, // "Mon, 02 Jan 2006 15:04:05 -0700"
		time.RFC1123,  // "Mon, 02 Jan 2006 15:04:05 MST"
		time.RFC822Z,
		time.RFC822,
		time.RFC3339, // Atom-style dates seen in some RSS feeds
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
