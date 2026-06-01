package guilds

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchGuildsParsesResponseAndSendsAPIKey(t *testing.T) {
	var gotPath, gotQuery, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("q")
		gotKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"guilds":[{"id":"1","name":"Alpha"},{"id":"2","name":"Beta"}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret-key")
	got, err := c.FetchGuilds("alp")
	if err != nil {
		t.Fatalf("FetchGuilds: %v", err)
	}

	if gotPath != "/api/guilds" {
		t.Errorf("path = %q, want /api/guilds", gotPath)
	}
	if gotQuery != "alp" {
		t.Errorf("query q = %q, want alp", gotQuery)
	}
	if gotKey != "secret-key" {
		t.Errorf("X-API-Key = %q, want secret-key", gotKey)
	}
	if len(got) != 2 || got[0].Name != "Alpha" || got[1].ID != "2" {
		t.Errorf("unexpected guilds: %+v", got)
	}
}

func TestFetchGuildsEmptyRelayURL(t *testing.T) {
	c := NewClient("", "")
	if _, err := c.FetchGuilds(""); err == nil {
		t.Fatal("expected error when relay URL is empty")
	}
}

func TestFetchGuildsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if _, err := c.FetchGuilds(""); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestFetchGuildsNullGuildsBecomesEmptySlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"guilds":null}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	got, err := c.FetchGuilds("")
	if err != nil {
		t.Fatalf("FetchGuilds: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
}

func TestFetchGuildsNoAPIKeyOmitsHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["X-Api-Key"]; ok {
			t.Errorf("X-API-Key header should be absent when no key configured")
		}
		_, _ = w.Write([]byte(`{"guilds":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	if _, err := c.FetchGuilds(""); err != nil {
		t.Fatalf("FetchGuilds: %v", err)
	}
}
