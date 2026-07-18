package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/network"
)

// verifyState holds the in-flight interactive device verification surfaced by
// the modal. net records which adapter the transaction belongs to so confirm /
// cancel call back into the right one even when it isn't the active network.
type verifyState struct {
	net          network.NetworkID
	txnID        string
	fromUser     string
	fromDevice   string
	emojis       []rune
	descriptions []string
	decimals     []int
	phase        network.VerificationPhase
	confirmed    bool // user pressed "match"; waiting on the other device
}

// verifierFor returns the Verifier for a network, or nil if that adapter does
// not support interactive verification.
func (m Model) verifierFor(net network.NetworkID) network.Verifier {
	a := m.adapters[net]
	if a == nil {
		return nil
	}
	v, ok := a.(network.Verifier)
	if !ok {
		return nil
	}
	return v
}

// onVerificationEvent folds an incoming verification step into the modal state.
func (m Model) onVerificationEvent(net network.NetworkID, v network.VerificationPrompt) (Model, []tea.Cmd) {
	switch v.Phase {
	case network.VerificationDone:
		m.verifyVisible = false
		m.verify = nil
		m.msgs = append(m.msgs, commands.SystemMsg("device verification complete — the device is now verified"))
		m.scrollOffset = 0
		return m, nil

	case network.VerificationCancelled:
		m.verifyVisible = false
		m.verify = nil
		reason := v.Reason
		if reason == "" {
			reason = "cancelled"
		}
		m.msgs = append(m.msgs, commands.SystemMsg(fmt.Sprintf("device verification cancelled: %s", reason)))
		m.scrollOffset = 0
		return m, nil

	default:
		// requested / ready / showSAS: (re)raise the modal. Later steps omit the
		// from-user/device, so carry those forward across the same transaction.
		st := &verifyState{
			net:          net,
			txnID:        v.TxnID,
			fromUser:     v.FromUser,
			fromDevice:   v.FromDevice,
			emojis:       v.Emojis,
			descriptions: v.Descriptions,
			decimals:     v.Decimals,
			phase:        v.Phase,
		}
		if prev := m.verify; prev != nil && prev.txnID == v.TxnID {
			if st.fromUser == "" {
				st.fromUser = prev.fromUser
			}
			if st.fromDevice == "" {
				st.fromDevice = prev.fromDevice
			}
			st.confirmed = prev.confirmed
		}
		m.verify = st
		m.verifyVisible = true
		return m, nil
	}
}

// handleVerifyKey processes keys while the verification modal is open. It
// swallows everything except confirm/cancel and the global quit chords.
func (m Model) handleVerifyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+q":
		m.closeAdapters()
		return m, tea.Quit
	case "y", "Y":
		return m.confirmVerify()
	case "n", "N", "esc":
		return m.cancelVerify()
	}
	return m, nil
}

// confirmVerify records the user's "match" and asks the adapter to confirm the
// SAS. The modal stays open in a waiting state until the other side confirms
// (VerificationDone) or the transaction is cancelled.
func (m Model) confirmVerify() (tea.Model, tea.Cmd) {
	if m.verify == nil || m.verify.phase != network.VerificationShowSAS || m.verify.confirmed {
		return m, nil
	}
	st := *m.verify
	st.confirmed = true
	m.verify = &st
	txnID := st.txnID
	v := m.verifierFor(st.net)
	if v == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		if err := v.ConfirmSAS(txnID); err != nil {
			return commands.CommandOutputMsg{Messages: []model.Message{
				commands.SystemMsg(fmt.Sprintf("verification confirm failed: %v", err)),
			}}
		}
		return nil
	}
}

// cancelVerify aborts the current verification and closes the modal.
func (m Model) cancelVerify() (tea.Model, tea.Cmd) {
	if m.verify == nil {
		m.verifyVisible = false
		return m, nil
	}
	txnID := m.verify.txnID
	v := m.verifierFor(m.verify.net)
	m.verifyVisible = false
	m.verify = nil
	if v == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		_ = v.CancelVerification(txnID)
		return nil
	}
}

// startVerification kicks off verifying another user's device via the active
// network (Matrix). Triggered by the /verify command.
func (m Model) startVerification(target string) (tea.Model, tea.Cmd) {
	target = strings.TrimSpace(target)
	if target == "" {
		m.msgs = append(m.msgs, commands.SystemMsg("usage: /verify @user:server"))
		m.scrollOffset = 0
		return m, nil
	}
	v := m.verifierFor(m.active)
	if v == nil {
		m.msgs = append(m.msgs, commands.SystemMsg("device verification is only available on Matrix with encryption enabled"))
		m.scrollOffset = 0
		return m, nil
	}
	return m, func() tea.Msg {
		if err := v.StartVerification(target); err != nil {
			return commands.CommandOutputMsg{Messages: []model.Message{
				commands.SystemMsg(fmt.Sprintf("verification failed: %v", err)),
			}}
		}
		return commands.CommandOutputMsg{Messages: []model.Message{
			commands.SystemMsg(fmt.Sprintf("verification request sent to %s — waiting for them to accept…", target)),
		}}
	}
}

// renderVerifyModal draws the SAS emoji-compare modal (sibling of
// renderHelpModal). It shows the emojis + descriptions and the confirm/cancel
// hint, or a progress line before the SAS is available.
func (m Model) renderVerifyModal(width, height int) string {
	title := panelTitleStyle().Render(" Device Verification ")
	innerH := height - 4
	if innerH < 1 {
		innerH = 1
	}

	v := m.verify
	var lines []string
	if v == nil {
		box := renderBorderedBox(panelStyle(), width, height, "no active verification")
		return title + "\n" + box
	}

	who := v.fromUser
	if who == "" {
		who = "another device"
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(themeFg).Render("Verifying "+who))
	if v.fromDevice != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render("device "+v.fromDevice))
	}
	lines = append(lines, "")

	switch v.phase {
	case network.VerificationShowSAS:
		lines = append(lines, lipgloss.NewStyle().Foreground(themeAccent).Bold(true).Render("Compare with the other device:"))
		lines = append(lines, "")
		if len(v.emojis) > 0 {
			for i, e := range v.emojis {
				desc := ""
				if i < len(v.descriptions) {
					desc = v.descriptions[i]
				}
				num := lipgloss.NewStyle().Foreground(themeDim).Render(fmt.Sprintf("%d.", i+1))
				emo := lipgloss.NewStyle().Foreground(themeCyan).Render(string(e))
				lines = append(lines, fmt.Sprintf("%s %s  %s", num, emo, desc))
			}
		} else if len(v.decimals) > 0 {
			nums := make([]string, len(v.decimals))
			for i, d := range v.decimals {
				nums[i] = fmt.Sprintf("%d", d)
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(themeCyan).Render(strings.Join(nums, "  ")))
		}
		lines = append(lines, "")
		if v.confirmed {
			lines = append(lines, lipgloss.NewStyle().Foreground(themeWarn).Render("waiting for the other device to confirm…"))
			lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render("esc = cancel"))
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(themeFg).Render("Do they match?"))
			lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render("y = match   ·   n / esc = cancel"))
		}

	default:
		lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render("establishing a secure channel…"))
		lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render("esc = cancel"))
	}

	content := clipLines(strings.Join(lines, "\n"), innerH)
	box := renderBorderedBox(panelStyle(), width, height, content)
	return title + "\n" + box
}
