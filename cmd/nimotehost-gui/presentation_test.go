// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

func visitObjects(root fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	seen := map[fyne.CanvasObject]bool{}
	var walk func(fyne.CanvasObject)
	walk = func(obj fyne.CanvasObject) {
		if obj == nil || seen[obj] {
			return
		}
		seen[obj] = true
		visit(obj)
		if c, ok := obj.(*fyne.Container); ok {
			for _, child := range c.Objects {
				walk(child)
			}
		}
		if w, ok := obj.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(root)
}

func TestSessionFilteringPrioritizesApprovalWithoutChangingSource(t *testing.T) {
	now := time.Now()
	source := []host.Session{
		{ID: "done", State: "succeeded", Directory: "/work/api", Started: now},
		{ID: "running", State: "running", Directory: "/work/web", Started: now.Add(-time.Minute)},
		{ID: "approval", State: "running", AgentStatus: "needs_approval", Directory: "/work/api", Started: now.Add(-time.Hour), Task: &host.TaskReference{Label: "Fix login"}},
	}
	all := filterSessions(source, "", 0)
	if len(all) != 3 || all[0].ID != "approval" || all[1].ID != "running" || source[0].ID != "done" {
		t.Fatal("attention ordering is wrong or source was mutated", all)
	}
	for _, tc := range []struct {
		query  string
		filter int
		want   int
	}{{"", 1, 2}, {"", 2, 1}, {"", 3, 1}, {"API", 0, 2}, {"login", 0, 1}, {"missing", 0, 0}} {
		if got := filterSessions(source, tc.query, tc.filter); len(got) != tc.want {
			t.Fatalf("filter %v returned %d", tc, len(got))
		}
	}
}

func TestOverviewDistinguishesStoppedFromUnavailableAndKeepsSafeActions(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	w := a.NewWindow("Preview")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	u.build("en", 0)
	defer func() { u.cancel() }()
	for _, tc := range []struct {
		state, key string
		err        error
	}{{"Running", "sessions.open", nil}, {"Stopped", "action.start", nil}, {"Unknown", "action.retry", errors.New("private backend details")}} {
		u.overview.update(desktop.Snapshot{State: tc.state}, nil, tc.err, nil)
		if u.overview.primary.Text != u.t(tc.key) || u.overview.primary.Disabled() {
			t.Fatal("incorrect primary action", tc, u.overview.primary.Text)
		}
		if u.overview.subtitle.Text == "private backend details" {
			t.Fatal("technical errors leaked onto the overview")
		}
	}
	if len(u.pages) != 5 {
		t.Fatal("advanced tools became top-level navigation")
	}
	for _, code := range languageCodes {
		u.changeLanguage(code)
		if u.messages.code != code || u.app.Preferences().String("language") != code && code != "en" {
			t.Fatal("language did not persist", code)
		}
		if u.messages.t(navigationKeys[0]) != u.t("nav.overview") {
			t.Fatal("navigation was not translated")
		}
	}
}

func TestSessionStatusFitsAfterListRowIsReused(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	w := a.NewWindow("Sessions")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	u.build("en", 0)
	defer func() { u.cancel() }()
	p := u.sessions
	p.update([]host.Session{{Kind: "agent", Agent: "codex", State: "running", AgentStatus: "needs_approval"}}, nil, nil, "Running")
	row := p.list.CreateItem().(*fyne.Container)
	row.Resize(fyne.NewSize(700, 80))
	p.list.UpdateItem(0, row)
	badge := row.Objects[2]
	if badge.Size().Width < badge.MinSize().Width || badge.Position().X+badge.Size().Width > row.Size().Width {
		t.Fatal("approval status is clipped after updating a recycled row")
	}
	if len(filterSessions([]host.Session{{State: "future-state"}}, "", 3)) != 0 {
		t.Fatal("unknown lifecycle state was presented as finished")
	}
}

func TestAgentStatusDoesNotGuessActivityFromAProcess(t *testing.T) {
	messages, err := newMessages("en")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"unknown", "", "future-status"} {
		got := sessionStatus(messages, host.Session{Kind: "agent", State: "running", AgentStatus: status})
		if got != messages.t("state.unknown") {
			t.Fatalf("agent %q was displayed as %q", status, got)
		}
	}
	if got := sessionStatus(messages, host.Session{Kind: "task", State: "running"}); got != messages.t("state.running") {
		t.Fatal("task lifecycle state was lost", got)
	}
}

func TestConversationToolDetailsRemainReadableWithoutTheProtocolEnvelope(t *testing.T) {
	message := historyMessage{Role: "tool", Text: "go test ./...", Tool: map[string]any{
		"command": "go test ./...", "output": "ok package", "changes": []any{map[string]any{"path": "main.go", "diff": "+ fixed"}},
	}}
	text := historyDisplayText(message)
	for _, want := range []string{"go test ./...", "ok package", "main.go", "+ fixed"} {
		if !strings.Contains(text, want) {
			t.Fatal("tool information was hidden", text)
		}
	}
	if strings.Count(text, "go test ./...") != 1 {
		t.Fatal("tool command was duplicated", text)
	}
}
