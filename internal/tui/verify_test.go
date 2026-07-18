package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/network"
)

// fakeVerifyAdapter is a fakeAdapter that also implements network.Verifier,
// recording the calls the TUI makes back into it.
type fakeVerifyAdapter struct {
	*fakeAdapter
	startedUser  string
	confirmedTxn string
	cancelledTxn string
}

var _ network.Verifier = (*fakeVerifyAdapter)(nil)

func newFakeVerifyAdapter(id network.NetworkID) *fakeVerifyAdapter {
	return &fakeVerifyAdapter{fakeAdapter: newFakeAdapter(id)}
}

func (f *fakeVerifyAdapter) StartVerification(userID string) error {
	f.startedUser = userID
	return nil
}
func (f *fakeVerifyAdapter) ConfirmSAS(txnID string) error { f.confirmedTxn = txnID; return nil }
func (f *fakeVerifyAdapter) CancelVerification(txnID string) error {
	f.cancelledTxn = txnID
	return nil
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func showSASEvent(net network.NetworkID, txn string) networkEventMsg {
	return networkEventMsg{Network: net, Kind: network.EventVerification, Verification: &network.VerificationPrompt{
		TxnID:        txn,
		FromUser:     "@alice:hs",
		Emojis:       []rune{'🐶', '🐱', '🦊'},
		Descriptions: []string{"Dog", "Cat", "Fox"},
		Phase:        network.VerificationShowSAS,
	}}
}

// TestVerificationRequestOpensModal drives a requested → showSAS sequence and
// asserts the modal opens and carries identity forward across steps.
func TestVerificationRequestOpensModal(t *testing.T) {
	a := newFakeVerifyAdapter("matrix")
	m := newTestModel(a)

	// Requested step (carries who).
	updated, _ := m.Update(networkEventMsg{Network: "matrix", Kind: network.EventVerification, Verification: &network.VerificationPrompt{
		TxnID: "t1", FromUser: "@alice:hs", FromDevice: "DEV1", Phase: network.VerificationRequested,
	}})
	m = updated.(Model)
	if !m.verifyVisible || m.verify == nil {
		t.Fatal("expected verify modal to open on request")
	}

	// SAS step (omits who; must be carried forward).
	updated, _ = m.Update(showSASEvent("matrix", "t1"))
	m = updated.(Model)
	if !m.verifyVisible {
		t.Fatal("modal should stay open at SAS step")
	}
	if m.verify.phase != network.VerificationShowSAS {
		t.Fatalf("phase = %v, want showSAS", m.verify.phase)
	}
	if m.verify.fromUser != "@alice:hs" {
		t.Fatalf("fromUser not carried forward: %q", m.verify.fromUser)
	}
	if len(m.verify.emojis) != 3 {
		t.Fatalf("expected 3 emojis, got %d", len(m.verify.emojis))
	}

	// The modal renders the SAS content without any line overflowing the
	// terminal width (which would corrupt the TUI). Centered modals render one
	// row taller than the terminal by an accepted centerInTerm quirk shared with
	// the help/coach modals, so only width is asserted here.
	m.width, m.height = 100, 30
	view := m.View()
	if !strings.Contains(view, "Dog") || !strings.Contains(view, "y = match") {
		t.Fatalf("verify modal missing SAS content:\n%s", view)
	}
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line %d exceeds width %d: got %d", i+1, m.width, got)
		}
	}
}

// TestVerificationConfirm asserts "y" confirms the SAS via the adapter and the
// modal waits, then closes on the done event.
func TestVerificationConfirm(t *testing.T) {
	a := newFakeVerifyAdapter("matrix")
	m := newTestModel(a)

	updated, _ := m.Update(showSASEvent("matrix", "txn-confirm"))
	m = updated.(Model)

	updated, cmd := m.Update(keyRunes("y"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("confirm should return a command")
	}
	cmd() // run the closure that calls the adapter

	if a.confirmedTxn != "txn-confirm" {
		t.Fatalf("ConfirmSAS called with %q, want txn-confirm", a.confirmedTxn)
	}
	if !m.verifyVisible || m.verify == nil || !m.verify.confirmed {
		t.Fatal("modal should remain open in a confirmed/waiting state")
	}

	// Done event closes the modal.
	updated, _ = m.Update(networkEventMsg{Network: "matrix", Kind: network.EventVerification, Verification: &network.VerificationPrompt{
		TxnID: "txn-confirm", Phase: network.VerificationDone,
	}})
	m = updated.(Model)
	if m.verifyVisible || m.verify != nil {
		t.Fatal("modal should close on done")
	}
}

// TestVerificationCancel asserts "esc" cancels via the adapter and closes.
func TestVerificationCancel(t *testing.T) {
	a := newFakeVerifyAdapter("matrix")
	m := newTestModel(a)

	updated, _ := m.Update(showSASEvent("matrix", "txn-cancel"))
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("cancel should return a command")
	}
	cmd()

	if a.cancelledTxn != "txn-cancel" {
		t.Fatalf("CancelVerification called with %q, want txn-cancel", a.cancelledTxn)
	}
	if m.verifyVisible || m.verify != nil {
		t.Fatal("modal should close on cancel")
	}
}

// TestVerificationModalSwallowsKeys confirms an arbitrary key does not leak into
// the input while the modal is open.
func TestVerificationModalSwallowsKeys(t *testing.T) {
	a := newFakeVerifyAdapter("matrix")
	m := newTestModel(a)
	updated, _ := m.Update(showSASEvent("matrix", "t"))
	m = updated.(Model)

	updated, _ = m.Update(keyRunes("z"))
	m = updated.(Model)
	if m.input.Value() != "" {
		t.Fatalf("modal must swallow keys; input got %q", m.input.Value())
	}
	if !m.verifyVisible {
		t.Fatal("modal should still be open after an unrelated key")
	}
}

// TestStartVerificationCommand asserts /verify routes to the adapter's Verifier.
func TestStartVerificationCommand(t *testing.T) {
	a := newFakeVerifyAdapter("matrix")
	m := newTestModel(a)

	_, cmd := m.Update(commands.StartVerificationMsg{Target: "@bob:hs"})
	if cmd == nil {
		t.Fatal("expected a command from StartVerificationMsg")
	}
	cmd()
	if a.startedUser != "@bob:hs" {
		t.Fatalf("StartVerification called with %q, want @bob:hs", a.startedUser)
	}
}

// TestStartVerificationUnsupported shows a helpful message when the active
// network has no Verifier.
func TestStartVerificationUnsupported(t *testing.T) {
	a := newFakeAdapter("discord") // no Verifier
	m := newTestModel(a)

	updated, _ := m.Update(commands.StartVerificationMsg{Target: "@bob:hs"})
	m = updated.(Model)
	if len(m.msgs) == 0 {
		t.Fatal("expected a system message when verification is unsupported")
	}
	last := m.msgs[len(m.msgs)-1]
	if last.Username != "system" {
		t.Fatalf("expected system message, got username %q", last.Username)
	}
}
