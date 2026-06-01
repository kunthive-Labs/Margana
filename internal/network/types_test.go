package network

import "testing"

func TestChannelRefKeyIsStableAndScoped(t *testing.T) {
	ref := ChannelRef{Network: "matrix", ServerID: "!space:hs", ID: "!room:hs", Name: "general"}

	// Name is display-only and must not affect the key.
	other := ref
	other.Name = "renamed"
	if ref.Key() != other.Key() {
		t.Errorf("Key must ignore Name: %q != %q", ref.Key(), other.Key())
	}

	// The key is composed of network, server, and channel id.
	want := "matrix\x1f!space:hs\x1f!room:hs"
	if ref.Key() != want {
		t.Errorf("Key() = %q, want %q", ref.Key(), want)
	}
}

func TestChannelRefKeyDistinguishesNetworksAndServers(t *testing.T) {
	base := ChannelRef{Network: "discord", ServerID: "g1", ID: "general"}
	cases := []ChannelRef{
		{Network: "matrix", ServerID: "g1", ID: "general"}, // different network
		{Network: "discord", ServerID: "g2", ID: "general"}, // different server
		{Network: "discord", ServerID: "g1", ID: "random"},  // different channel
	}
	for _, c := range cases {
		if base.Key() == c.Key() {
			t.Errorf("expected distinct keys for %+v and %+v", base, c)
		}
	}
}

func TestChannelRefServerlessKey(t *testing.T) {
	ref := ChannelRef{Network: "irc", ID: "#chan"}
	if got := ref.Key(); got != "irc\x1f\x1f#chan" {
		t.Errorf("serverless Key() = %q", got)
	}
}
