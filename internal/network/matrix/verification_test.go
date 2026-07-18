package matrix

import (
	"context"
	"reflect"
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/kunthive-Labs/Margana/internal/logging"
	"github.com/kunthive-Labs/Margana/internal/network"
)

func newVerifyTestAdapter() *Adapter {
	// verifier stays nil: the auto-accept / auto-start goroutines are guarded
	// against a nil helper, so callbacks can be exercised offline.
	return &Adapter{events: make(chan network.Event, 8), logger: logging.Disabled()}
}

// TestVerificationShimEmitsEvents maps each mautrix verificationhelper callback
// onto the neutral network.Event the TUI consumes. No live homeserver.
func TestVerificationShimEmitsEvents(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		call func(s *verificationShim)
		want network.VerificationPrompt
	}{
		{
			name: "requested",
			call: func(s *verificationShim) {
				s.VerificationRequested(ctx, id.VerificationTransactionID("t1"), id.UserID("@alice:hs"), id.DeviceID("DEV1"))
			},
			want: network.VerificationPrompt{TxnID: "t1", FromUser: "@alice:hs", FromDevice: "DEV1", Phase: network.VerificationRequested},
		},
		{
			name: "ready",
			call: func(s *verificationShim) {
				s.VerificationReady(ctx, id.VerificationTransactionID("t2"), id.DeviceID("DEV2"), true, false, nil)
			},
			want: network.VerificationPrompt{TxnID: "t2", FromDevice: "DEV2", Phase: network.VerificationReady},
		},
		{
			name: "show SAS",
			call: func(s *verificationShim) {
				s.ShowSAS(ctx, id.VerificationTransactionID("t3"), []rune{'🐶', '🐱'}, []string{"Dog", "Cat"}, []int{111, 222, 333})
			},
			want: network.VerificationPrompt{TxnID: "t3", Emojis: []rune{'🐶', '🐱'}, Descriptions: []string{"Dog", "Cat"}, Decimals: []int{111, 222, 333}, Phase: network.VerificationShowSAS},
		},
		{
			name: "cancelled with reason",
			call: func(s *verificationShim) {
				s.VerificationCancelled(ctx, id.VerificationTransactionID("t4"), event.VerificationCancelCodeUser, "rejected")
			},
			want: network.VerificationPrompt{TxnID: "t4", Phase: network.VerificationCancelled, Reason: "rejected"},
		},
		{
			name: "cancelled falls back to code",
			call: func(s *verificationShim) {
				s.VerificationCancelled(ctx, id.VerificationTransactionID("t5"), event.VerificationCancelCodeTimeout, "")
			},
			want: network.VerificationPrompt{TxnID: "t5", Phase: network.VerificationCancelled, Reason: string(event.VerificationCancelCodeTimeout)},
		},
		{
			name: "done",
			call: func(s *verificationShim) {
				s.VerificationDone(ctx, id.VerificationTransactionID("t6"), event.VerificationMethodSAS)
			},
			want: network.VerificationPrompt{TxnID: "t6", Phase: network.VerificationDone},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newVerifyTestAdapter()
			s := &verificationShim{a: a}

			tt.call(s)

			ev := <-a.events
			if ev.Kind != network.EventVerification {
				t.Fatalf("kind = %v, want EventVerification", ev.Kind)
			}
			if ev.Network != ID {
				t.Fatalf("network = %q, want %q", ev.Network, ID)
			}
			if ev.Verification == nil {
				t.Fatal("verification payload is nil")
			}
			if !reflect.DeepEqual(*ev.Verification, tt.want) {
				t.Fatalf("prompt mismatch:\n got %+v\nwant %+v", *ev.Verification, tt.want)
			}
		})
	}
}

// TestAdapterVerifierRequiresCrypto confirms the Verifier methods degrade with a
// clear error (rather than panicking) when verification was never initialized.
func TestAdapterVerifierRequiresCrypto(t *testing.T) {
	a := &Adapter{}
	if err := a.StartVerification("@bob:hs"); err == nil {
		t.Fatal("StartVerification: expected error when verification unavailable")
	}
	if err := a.ConfirmSAS("txn"); err == nil {
		t.Fatal("ConfirmSAS: expected error when verification unavailable")
	}
	if err := a.CancelVerification("txn"); err == nil {
		t.Fatal("CancelVerification: expected error when verification unavailable")
	}
}
