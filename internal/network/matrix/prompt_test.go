package matrix

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestNormalizeHomeserver(t *testing.T) {
	cases := map[string]string{
		"matrix.org":             "https://matrix.org",
		"https://matrix.org":     "https://matrix.org",
		"https://matrix.org/":    "https://matrix.org",
		"http://localhost:8008/": "http://localhost:8008",
		"  example.org  ":        "https://example.org",
		"":                       "",
	}
	for in, want := range cases {
		if got := normalizeHomeserver(in); got != want {
			t.Errorf("normalizeHomeserver(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGatherLoginPromptsOnlyForMissingFields(t *testing.T) {
	// Homeserver and user already configured: only the password is requested.
	var out bytes.Buffer
	in := bufio.NewReader(strings.NewReader(""))
	secretCalls := 0
	readSecret := func(prompt string) (string, error) {
		secretCalls++
		if !strings.Contains(prompt, "@me:hs") {
			t.Errorf("password prompt should name the user, got %q", prompt)
		}
		return "hunter2", nil
	}

	d, err := gatherLogin(&out, in, readSecret, "https://hs", "@me:hs")
	if err != nil {
		t.Fatalf("gatherLogin: %v", err)
	}
	if d.homeserver != "https://hs" || d.userID != "@me:hs" || d.password != "hunter2" {
		t.Errorf("unexpected details: %+v", d)
	}
	if secretCalls != 1 {
		t.Errorf("expected exactly one password prompt, got %d", secretCalls)
	}
	if strings.Contains(out.String(), "homeserver") {
		t.Errorf("should not prompt for an already-configured homeserver: %q", out.String())
	}
}

func TestGatherLoginPromptsForEverythingWhenEmpty(t *testing.T) {
	var out bytes.Buffer
	in := bufio.NewReader(strings.NewReader("matrix.org\n@alice:matrix.org\n"))
	readSecret := func(string) (string, error) { return "secret", nil }

	d, err := gatherLogin(&out, in, readSecret, "", "")
	if err != nil {
		t.Fatalf("gatherLogin: %v", err)
	}
	if d.homeserver != "https://matrix.org" {
		t.Errorf("homeserver = %q, want normalized https://matrix.org", d.homeserver)
	}
	if d.userID != "@alice:matrix.org" {
		t.Errorf("userID = %q", d.userID)
	}
	if d.password != "secret" {
		t.Errorf("password = %q", d.password)
	}
}

func TestGatherLoginRejectsEmptyPassword(t *testing.T) {
	in := bufio.NewReader(strings.NewReader(""))
	readSecret := func(string) (string, error) { return "", nil }
	if _, err := gatherLogin(&bytes.Buffer{}, in, readSecret, "https://hs", "@me:hs"); err == nil {
		t.Fatal("expected error on empty password")
	}
}

func TestGatherLoginRejectsEmptyUserID(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("\n")) // blank user id line
	readSecret := func(string) (string, error) { return "secret", nil }
	if _, err := gatherLogin(&bytes.Buffer{}, in, readSecret, "https://hs", ""); err == nil {
		t.Fatal("expected error on empty user id")
	}
}
