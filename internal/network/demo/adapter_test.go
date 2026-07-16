package demo

import (
	"context"
	"testing"
	"time"

	"github.com/kunthive-Labs/Margana/internal/network"
)

func TestAdapterEmitsScriptedEvents(t *testing.T) {
	a := New(
		"matrix", "srv", "you",
		[]string{"general"}, []string{"alice"},
		map[string]string{"alice": "busy"},
		[]step{{after: 5 * time.Millisecond, channel: "general", user: "alice", content: "hello"}},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var gotConnected, gotPresent, gotMessage bool
	timeout := time.After(2 * time.Second)
	for !(gotConnected && gotMessage) {
		select {
		case ev := <-a.Events():
			switch {
			case ev.Kind == network.EventStatus && ev.State == network.StateConnected:
				gotConnected = true
			case ev.Kind == network.EventPresentUsers:
				gotPresent = true
			case ev.Kind == network.EventMessage && ev.Message != nil && ev.Message.Content == "hello":
				gotMessage = true
			}
		case <-timeout:
			t.Fatalf("timed out: connected=%v present=%v message=%v", gotConnected, gotPresent, gotMessage)
		}
	}
	if !gotPresent {
		t.Error("expected a present-users event before the first message")
	}
}

func TestDefaultAdaptersAreValidNetworks(t *testing.T) {
	adapters := Adapters()
	if len(adapters) < 2 {
		t.Fatalf("expected >=2 demo adapters (for network switching), got %d", len(adapters))
	}
	seen := map[network.NetworkID]bool{}
	for _, a := range adapters {
		if a.ID() == "" {
			t.Error("demo adapter has empty ID")
		}
		if seen[a.ID()] {
			t.Errorf("duplicate demo adapter ID %q", a.ID())
		}
		seen[a.ID()] = true
		// Must be startable and stoppable without error.
		ctx, cancel := context.WithCancel(context.Background())
		if err := a.Connect(ctx); err != nil {
			t.Errorf("%s Connect: %v", a.ID(), err)
		}
		if err := a.Disconnect(); err != nil {
			t.Errorf("%s Disconnect: %v", a.ID(), err)
		}
		cancel()
	}
}
