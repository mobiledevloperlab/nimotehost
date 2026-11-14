// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

type sessionsPanel struct {
	root         fyne.CanvasObject
	tabs         *container.AppTabs
	ui           *managerWindow
	list         *widget.List
	search       *widget.Entry
	filter       *widget.Select
	empty        *widget.Label
	source, rows []host.Session
}

func newSessionsPanel(u *managerWindow) *sessionsPanel {
	p := &sessionsPanel{ui: u}
	p.search = widget.NewEntry()
	p.search.SetPlaceHolder(u.t("sessions.search"))
	p.filter = widget.NewSelect([]string{u.t("filter.all"), u.t("filter.running"), u.t("filter.approval"), u.t("filter.finished")}, nil)
	p.filter.SetSelectedIndex(0)
	fitSelect(p.filter)
	p.empty = wrappedLabel(u.t("overview.checking"))
	p.list = widget.NewList(func() int { return len(p.rows) }, func() fyne.CanvasObject {
		title := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		title.Truncation = fyne.TextTruncateEllipsis
		subtitle := mutedLabel("")
		subtitle.Truncation = fyne.TextTruncateEllipsis
		return container.NewBorder(nil, nil, widget.NewIcon(theme.DocumentIcon()), widget.NewLabel(""), container.NewVBox(title, subtitle))
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		row := obj.(*fyne.Container)
		labels := row.Objects[0].(*fyne.Container)
		item := p.rows[id]
		labels.Objects[0].(*widget.Label).SetText(sessionTitle(u.messages, item))
		labels.Objects[1].(*widget.Label).SetText(item.Directory)
		row.Objects[1].(*widget.Icon).SetResource(sessionIcon(item))
		badge := row.Objects[2].(*widget.Label)
		badge.Importance = widget.MediumImportance
		if item.State == "interrupted" || item.State == "canceled" {
			badge.Importance = widget.LowImportance
		}
		if item.State == "failed" || item.AgentStatus == "needs_approval" {
			badge.Importance = widget.WarningImportance
		}
		if item.State == "succeeded" {
			badge.Importance = widget.SuccessImportance
		}
		badge.SetText(sessionStatus(u.messages, item))
		row.Refresh()
	})
	p.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(p.rows) {
			u.showSession(p.rows[id])
			p.list.UnselectAll()
		}
	}
	p.search.OnChanged = func(string) { p.applyFilter() }
	p.filter.OnChanged = func(string) { p.applyFilter() }
	current := container.NewBorder(container.NewVBox(container.NewBorder(nil, nil, nil, p.filter, p.search), p.empty), nil, nil, nil, p.list)
	p.tabs = container.NewAppTabs(container.NewTabItem(u.t("sessions.current"), current), container.NewTabItem(u.t("history.title"), historyPanel(u)))
	p.root = inset(container.NewBorder(pageHeading(u.t("nav.sessions"), u.t("sessions.intro")), nil, nil, nil, surface(p.tabs)), pagePadding)
	return p
}

func (p *sessionsPanel) update(items []host.Session, statusErr, sessionErr error, state string) {
	p.source = append([]host.Session(nil), items...)
	p.applyFilter()
	if statusErr != nil || sessionErr != nil {
		p.empty.SetText(p.ui.t("sessions.unavailable"))
		p.empty.Show()
	} else if state != "Running" {
		p.empty.SetText(p.ui.t("sessions.hostStopped"))
		p.empty.Show()
	}
}
func (p *sessionsPanel) applyFilter() {
	rows := filterSessions(p.source, p.search.Text, p.filter.SelectedIndex())
	redraw := len(rows) != len(p.rows)
	if !redraw {
		for i, row := range rows {
			old := p.rows[i]
			if row.ID != old.ID || row.Kind != old.Kind || row.State != old.State || row.AgentStatus != old.AgentStatus || row.Directory != old.Directory || sessionTitle(p.ui.messages, row) != sessionTitle(p.ui.messages, old) {
				redraw = true
				break
			}
		}
	}
	p.rows = rows
	if redraw {
		p.list.Refresh()
	}
	if len(p.rows) == 0 {
		p.empty.SetText(p.ui.t("sessions.empty"))
		p.empty.Show()
	} else {
		p.empty.Hide()
	}
}

func filterSessions(source []host.Session, query string, filter int) []host.Session {
	query = strings.ToLower(strings.TrimSpace(query))
	rows := make([]host.Session, 0, len(source))
	for _, s := range source {
		finished := s.State == "succeeded" || s.State == "failed" || s.State == "canceled" || s.State == "interrupted"
		if filter == 1 && s.State != "running" || filter == 2 && (s.State != "running" || s.AgentStatus != "needs_approval") || filter == 3 && !finished {
			continue
		}
		label := ""
		if s.Task != nil {
			label = s.Task.Label
		}
		if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{label, s.Agent, s.Command, s.Directory, s.ID}, " ")), query) {
			continue
		}
		rows = append(rows, s)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		priority := func(s host.Session) int {
			if s.State == "running" && s.AgentStatus == "needs_approval" {
				return 0
			}
			if s.State == "running" {
				return 1
			}
			return 2
		}
		a, b := priority(rows[i]), priority(rows[j])
		if a != b {
			return a < b
		}
		if !rows[i].Started.Equal(rows[j].Started) {
			return rows[i].Started.After(rows[j].Started)
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

func sessionTitle(m *messages, s host.Session) string {
	if s.Task != nil && strings.TrimSpace(s.Task.Label) != "" {
		return s.Task.Label
	}
	if s.Kind == "agent" {
		return agentName(s.Agent)
	}
	if s.Kind == "terminal" {
		return m.t("session.terminal")
	}
	if s.Command != "" {
		return filepath.Base(s.Command)
	}
	return m.t("session.task")
}
func agentName(agent string) string {
	switch agent {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude Code"
	case "opencode":
		return "OpenCode"
	}
	return agent
}
func sessionStatus(m *messages, s host.Session) string {
	if s.State == "running" && s.AgentStatus != "" {
		switch s.AgentStatus {
		case "needs_approval", "idle", "completed", "failed", "running", "disconnected", "unknown":
			return m.t("state." + s.AgentStatus)
		}
	}
	if s.State == "running" && s.Kind == "agent" {
		return m.t("state.unknown")
	}
	switch s.State {
	case "running", "succeeded", "failed", "canceled", "interrupted":
		return m.t("state." + s.State)
	}
	return m.t("state.unknown")
}
func sessionIcon(s host.Session) fyne.Resource {
	if s.AgentStatus == "needs_approval" && s.State == "running" {
		return theme.WarningIcon()
	}
	if s.Kind == "agent" {
		return theme.ComputerIcon()
	}
	return theme.DocumentIcon()
}

func (u *managerWindow) showSession(s host.Session) {
	content := container.NewVBox(widget.NewLabelWithStyle(sessionStatus(u.messages, s), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), wrappedLabel(s.Directory), wrappedLabel(u.t("session.started", map[string]any{"Time": s.Started.Local().Format("2006-01-02 15:04")})))
	if s.Kind == "agent" && s.Interaction == "structured" && s.State == "running" {
		content.Add(widget.NewButtonWithIcon(u.t("approval.open"), theme.VisibilityIcon(), func() { inspectAgent(u, s.ID) }))
	}
	if s.Error != "" {
		content.Add(wrappedLabel(u.t("session.problem")))
	}
	pretty, _ := json.MarshalIndent(s, "", "  ")
	content.Add(widget.NewAccordion(widget.NewAccordionItem(u.t("details.technical"), readOnlyText(string(pretty)))))
	d := dialog.NewCustom(sessionTitle(u.messages, s), u.t("action.close"), container.NewVScroll(content), u.window)
	d.Resize(u.readerSize())
	d.Show()
}
