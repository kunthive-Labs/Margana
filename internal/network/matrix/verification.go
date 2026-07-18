package matrix

import (
	"context"
	"fmt"

	"maunium.net/go/mautrix/crypto/verificationhelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/kunthive-Labs/Margana/internal/network"
)

// Adapter implements network.Verifier when crypto is available (see the nil
// guards in the methods below).
var _ network.Verifier = (*Adapter)(nil)

// setupVerification builds the mautrix verification helper on top of the
// initialized Olm machine and registers its sync event handlers. It offers SAS
// only (no QR show/scan), which is the MVP; cross-signing bootstrap is a larger
// follow-up. Must be called after setupCrypto succeeds (client.Crypto set) and
// after registerHandlers, so the DefaultSyncer is in place.
func (a *Adapter) setupVerification(ctx context.Context) error {
	if a.crypto == nil {
		return fmt.Errorf("crypto not initialized")
	}
	shim := &verificationShim{a: a}
	// supportsQRShow=false, supportsQRScan=false, supportsSAS=true.
	vh := verificationhelper.NewVerificationHelper(
		a.client, a.crypto.Machine(), verificationhelper.NewInMemoryVerificationStore(),
		shim, false, false, true,
	)
	if err := vh.Init(ctx); err != nil {
		return err
	}
	a.verifier = vh
	return nil
}

// verifyContext returns the connection-lifetime context (falling back to
// Background) for verification API calls made from the TUI.
func (a *Adapter) verifyContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// StartVerification begins an interactive SAS verification with userID's
// device(s). Implements network.Verifier.
func (a *Adapter) StartVerification(userID string) error {
	if a.verifier == nil {
		return fmt.Errorf("matrix: device verification is unavailable (encryption not initialized)")
	}
	if _, err := a.verifier.StartVerification(a.verifyContext(), id.UserID(userID)); err != nil {
		return err
	}
	return nil
}

// ConfirmSAS confirms the short authentication string matched for the given
// transaction. Implements network.Verifier.
func (a *Adapter) ConfirmSAS(txnID string) error {
	if a.verifier == nil {
		return fmt.Errorf("matrix: device verification is unavailable")
	}
	return a.verifier.ConfirmSAS(a.verifyContext(), id.VerificationTransactionID(txnID))
}

// CancelVerification aborts the given transaction (user rejected/dismissed).
// Implements network.Verifier.
func (a *Adapter) CancelVerification(txnID string) error {
	if a.verifier == nil {
		return fmt.Errorf("matrix: device verification is unavailable")
	}
	return a.verifier.CancelVerification(
		a.verifyContext(), id.VerificationTransactionID(txnID),
		event.VerificationCancelCodeUser, "cancelled by user",
	)
}

// verificationShim adapts mautrix's verificationhelper callbacks onto the
// neutral network.Event stream. Each callback translates to an
// EventVerification event the TUI renders as the SAS emoji-compare modal.
//
// Concurrency: VerificationReady may be invoked while the helper holds its
// internal transaction lock (it fires from AcceptVerification and
// onVerificationReady, both of which hold the lock). Any helper method that
// re-acquires that lock — StartSAS, AcceptVerification — must therefore be
// called from a fresh goroutine to avoid a self-deadlock.
type verificationShim struct {
	a *Adapter
}

var (
	_ verificationhelper.RequiredCallbacks = (*verificationShim)(nil)
	_ verificationhelper.ShowSASCallbacks  = (*verificationShim)(nil)
)

// VerificationRequested fires when another device asks to verify. For the MVP
// we auto-accept: the actual trust decision is the emoji comparison the user
// still has to confirm, so accepting only agrees to run the SAS dance. The
// modal is raised immediately for feedback.
func (s *verificationShim) VerificationRequested(ctx context.Context, txnID id.VerificationTransactionID, from id.UserID, fromDevice id.DeviceID) {
	s.a.emit(ctx, network.Event{
		Network: ID,
		Kind:    network.EventVerification,
		Verification: &network.VerificationPrompt{
			TxnID:      txnID.String(),
			FromUser:   from.String(),
			FromDevice: fromDevice.String(),
			Phase:      network.VerificationRequested,
		},
	})
	// Accept off the sync goroutine: AcceptVerification does network I/O and,
	// via the VerificationReady callback, will kick off SAS.
	go func() {
		if s.a.verifier == nil {
			return
		}
		if err := s.a.verifier.AcceptVerification(s.a.verifyContext(), txnID); err != nil {
			s.a.logger.Named("matrix").Warn("accept verification failed", "err", err)
		}
	}()
}

// VerificationReady fires once both parties agreed on methods. If SAS is on the
// table we start it; mautrix resolves the case where both sides start
// simultaneously, so a duplicate StartSAS is harmless (it errors and we ignore).
func (s *verificationShim) VerificationReady(ctx context.Context, txnID id.VerificationTransactionID, otherDeviceID id.DeviceID, supportsSAS, supportsScanQRCode bool, qrCode *verificationhelper.QRCode) {
	s.a.emit(ctx, network.Event{
		Network: ID,
		Kind:    network.EventVerification,
		Verification: &network.VerificationPrompt{
			TxnID:      txnID.String(),
			FromDevice: otherDeviceID.String(),
			Phase:      network.VerificationReady,
		},
	})
	if !supportsSAS {
		s.a.logger.Named("matrix").Warn("device verification: other device does not support SAS", "txn", txnID.String())
		return
	}
	// Run in a goroutine: this callback may hold the helper's transaction lock,
	// and StartSAS re-acquires it.
	go func() {
		if s.a.verifier == nil {
			return
		}
		if err := s.a.verifier.StartSAS(s.a.verifyContext(), txnID); err != nil {
			// Commonly "start event already sent or received" when the other
			// side started first — not fatal; the SAS dance still completes.
			s.a.logger.Named("matrix").Debug("start SAS", "err", err)
		}
	}()
}

// ShowSAS delivers the emoji/decimal short authentication string to compare.
func (s *verificationShim) ShowSAS(ctx context.Context, txnID id.VerificationTransactionID, emojis []rune, emojiDescriptions []string, decimals []int) {
	s.a.emit(ctx, network.Event{
		Network: ID,
		Kind:    network.EventVerification,
		Verification: &network.VerificationPrompt{
			TxnID:        txnID.String(),
			Emojis:       emojis,
			Descriptions: emojiDescriptions,
			Decimals:     decimals,
			Phase:        network.VerificationShowSAS,
		},
	})
}

// VerificationCancelled fires when a transaction is cancelled, rejected, or
// times out.
func (s *verificationShim) VerificationCancelled(ctx context.Context, txnID id.VerificationTransactionID, code event.VerificationCancelCode, reason string) {
	if reason == "" {
		reason = string(code)
	}
	s.a.emit(ctx, network.Event{
		Network: ID,
		Kind:    network.EventVerification,
		Verification: &network.VerificationPrompt{
			TxnID:  txnID.String(),
			Phase:  network.VerificationCancelled,
			Reason: reason,
		},
	})
}

// VerificationDone fires when both devices have verified each other.
func (s *verificationShim) VerificationDone(ctx context.Context, txnID id.VerificationTransactionID, method event.VerificationMethod) {
	s.a.emit(ctx, network.Event{
		Network: ID,
		Kind:    network.EventVerification,
		Verification: &network.VerificationPrompt{
			TxnID: txnID.String(),
			Phase: network.VerificationDone,
		},
	})
}
