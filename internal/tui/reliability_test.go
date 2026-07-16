package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/network"
)

func TestReconnectSeconds(t *testing.T) {
	if got := reconnectSeconds(time.Time{}); got != 0 {
		t.Errorf("zero time => %d, want 0", got)
	}
	if got := reconnectSeconds(time.Now().Add(-time.Second)); got != 0 {
		t.Errorf("past time => %d, want 0", got)
	}
	if got := reconnectSeconds(time.Now().Add(3 * time.Second)); got < 2 || got > 3 {
		t.Errorf("future ~3s => %d, want 2..3", got)
	}
}

func TestOnStatusEventReconnectAndClear(t *testing.T) {
	m := Model{}
	retryAt := time.Now().Add(3 * time.Second)

	m, cmds := m.onStatusEvent(network.StateReconnecting, nil, retryAt)
	if !m.reconnectAt.Equal(retryAt) {
		t.Errorf("reconnectAt = %v, want %v", m.reconnectAt, retryAt)
	}
	if !m.reconnectTicking {
		t.Error("expected reconnectTicking=true")
	}
	if len(cmds) == 0 {
		t.Error("expected a reconnect tick command")
	}

	// A second reconnecting event must not start a second ticker.
	m, cmds = m.onStatusEvent(network.StateReconnecting, nil, retryAt)
	if len(cmds) != 0 {
		t.Error("expected no new ticker on repeat reconnecting")
	}

	// Connecting clears the countdown.
	m, _ = m.onStatusEvent(network.StateConnected, nil, time.Time{})
	if !m.reconnectAt.IsZero() {
		t.Error("expected reconnectAt cleared on connect")
	}
}

func TestChatEmptyHintByState(t *testing.T) {
	m := Model{channel: "general"}

	m.status = network.StateConnected
	if got := m.chatEmptyHint(); !strings.Contains(got, "general") {
		t.Errorf("connected hint should mention channel: %q", got)
	}
	m.status = network.StateReconnecting
	if got := strings.ToLower(m.chatEmptyHint()); !strings.Contains(got, "reconnect") {
		t.Errorf("reconnecting hint unexpected: %q", got)
	}
	m.status = network.StateDisconnected
	if got := strings.ToLower(m.chatEmptyHint()); !strings.Contains(got, "offline") {
		t.Errorf("offline hint unexpected: %q", got)
	}
}

func TestDesktopNotifyGate(t *testing.T) {
	m := Model{}
	if m.desktopNotify() {
		t.Error("nil setupCfg => desktopNotify must be false")
	}
	m.setupCfg = &config.Config{}
	if m.desktopNotify() {
		t.Error("default config => desktopNotify false")
	}
	m.setupCfg.Notifications.Desktop = true
	if !m.desktopNotify() {
		t.Error("Desktop=true => desktopNotify true")
	}
}

func TestOfflineBannerRendersCountdown(t *testing.T) {
	m := Model{width: 80, status: network.StateReconnecting, reconnectAt: time.Now().Add(4 * time.Second)}
	if out := m.renderOfflineBanner(); !strings.Contains(out, "reconnecting") {
		t.Errorf("reconnecting banner missing 'reconnecting': %q", out)
	}
	m.status = network.StateConnected
	if out := m.renderOfflineBanner(); out != "" {
		t.Errorf("connected banner should be empty, got %q", out)
	}
}
