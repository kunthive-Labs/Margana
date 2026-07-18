package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kunthive-Labs/Margana/internal/config"
	"github.com/kunthive-Labs/Margana/internal/model"
	"github.com/kunthive-Labs/Margana/internal/tui/panels"
)

func newPanelTestModel(t *testing.T) Model {
	t.Helper()
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	return m.WithPanels([]config.PanelConfig{{Type: "github", Source: "o/r", Refresh: "60s"}})
}

func TestWithPanelsBuildsPanels(t *testing.T) {
	m := newPanelTestModel(t)
	if len(m.panels) != 1 {
		t.Fatalf("expected 1 panel, got %d", len(m.panels))
	}
	if m.panels[0].ID != "github:o/r" {
		t.Errorf("unexpected panel id %q", m.panels[0].ID)
	}
	if m.panels[0].Interval != time.Minute {
		t.Errorf("expected 60s interval, got %v", m.panels[0].Interval)
	}
	if m.panelData == nil {
		t.Error("expected panelData map to be initialized")
	}
}

func TestWithPanelsSurfacesBadConfig(t *testing.T) {
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m = m.WithPanels([]config.PanelConfig{{Type: "nope", Source: "x"}})
	if len(m.panels) != 0 {
		t.Fatalf("expected invalid panel to be skipped, got %d", len(m.panels))
	}
	if len(m.errors) == 0 {
		t.Error("expected a tracked error for the invalid panel type")
	}
}

func TestPanelDataMsgStoresAndRearms(t *testing.T) {
	m := newPanelTestModel(t)
	id := m.panels[0].ID

	res := panels.Result{Items: []panels.Item{{Primary: "a commit"}}}
	updated, cmd := m.Update(panelDataMsg{PanelID: id, Result: res})
	got := updated.(Model)

	stored, ok := got.panelData[id]
	if !ok {
		t.Fatalf("expected panelData to contain %q", id)
	}
	if len(stored.Items) != 1 || stored.Items[0].Primary != "a commit" {
		t.Fatalf("unexpected stored result: %#v", stored)
	}
	if cmd == nil {
		t.Fatal("expected a non-nil re-arm command for a known panel")
	}
}

func TestPanelDataMsgTracksErrorAndKeepsItems(t *testing.T) {
	m := newPanelTestModel(t)
	id := m.panels[0].ID
	m.panelData[id] = panels.Result{Items: []panels.Item{{Primary: "old"}}}

	updated, _ := m.Update(panelDataMsg{PanelID: id, Result: panels.Result{Err: fmt.Errorf("boom")}})
	got := updated.(Model)

	if items := got.panelData[id].Items; len(items) != 1 || items[0].Primary != "old" {
		t.Fatalf("expected previous items kept on transient error, got %#v", got.panelData[id])
	}
	if len(got.errors) == 0 {
		t.Fatal("expected the fetch error to be tracked")
	}
}

func TestPanelDataMsgUnknownPanelNoRearm(t *testing.T) {
	m := newPanelTestModel(t)
	_, cmd := m.Update(panelDataMsg{PanelID: "does-not-exist", Result: panels.Result{}})
	if cmd != nil {
		t.Fatal("expected no re-arm command for an unknown panel id")
	}
}

func TestRenderPanelShowsTitleAndItems(t *testing.T) {
	res := panels.Result{Items: []panels.Item{
		{Primary: "shipped the release", Secondary: "Push @octocat", Timestamp: time.Now()},
	}}
	out := renderPanel("GitHub: kunthive-Labs/Margana", res, 34, 8)
	if !strings.Contains(out, "GitHub: kunthive-Labs/Margana") {
		t.Errorf("expected panel title in output:\n%s", out)
	}
	if !strings.Contains(out, "shipped the release") {
		t.Errorf("expected item primary in output:\n%s", out)
	}
}

func TestRenderRightSidebarIncludesPanel(t *testing.T) {
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.usersVisible = false
	m.panels = []panels.Panel{{ID: "github:o/r", Title: "GitHub: o/r", Interval: time.Minute}}
	m.panelData = map[string]panels.Result{
		"github:o/r": {Items: []panels.Item{{Primary: "hello world"}}},
	}

	out := m.renderRightSidebar(32, 30)
	if !strings.Contains(out, "GitHub: o/r") {
		t.Errorf("expected panel title in sidebar output:\n%s", out)
	}
}

// TestViewFitsWithPanels guards the height-budgeting: multiple populated panels
// plus the user list and notifications must still fit the terminal exactly.
func TestViewFitsWithPanels(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m := New(nil, "", nil, nil, "general", "me", "", "", "", "test-guild", nil, "", "", "", nil, "")
	m.width = 120
	m.height = 30
	m.terminalOnline = []string{"me", "alice", "bob"}
	m.panels = []panels.Panel{
		{ID: "github:o/r", Title: "GitHub: o/r", Interval: time.Minute},
		{ID: "ci:o/r", Title: "CI: o/r", Interval: time.Minute},
	}
	m.panelData = map[string]panels.Result{
		"github:o/r": {Items: []panels.Item{
			{Primary: "fix: a long commit message that should be clamped to the panel width", Secondary: "Push @octocat", Timestamp: base},
			{Primary: "docs: update the configuration reference", Secondary: "Push @alice", Timestamp: base},
		}},
		"ci:o/r": {Items: []panels.Item{
			{Primary: "CI run passed on main after the latest push event", Secondary: "success · main", Timestamp: base},
		}},
	}
	for i := 0; i < 20; i++ {
		m.msgs = append(m.msgs, model.Message{
			ID:        fmt.Sprintf("m-%d", i),
			Username:  "alice",
			Content:   "hello there, this is a chat message",
			Channel:   "general",
			Timestamp: base.Add(time.Duration(i) * time.Minute),
		})
	}

	assertViewFits(t, m.View(), m.width, m.height)
}
