// SPDX-License-Identifier: Apache-2.0
package main

import (
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

type overviewPanel struct {
	actions                             *fyne.Container
	root                                fyne.CanvasObject
	ui                                  *managerWindow
	title, subtitle, activity, observed *widget.Label
	icon                                *widget.Icon
	primary, connection, review         *widget.Button
	activeCount, approvalCount          *widget.RichText
	state                               string
}

func newOverviewPanel(u *managerWindow) *overviewPanel {
	p := &overviewPanel{ui: u}
	p.title = widget.NewLabelWithStyle(u.t("overview.checking"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	p.subtitle = mutedLabel(u.t("overview.checkingHint"))
	p.icon = widget.NewIcon(theme.ComputerIcon())
	p.primary = widget.NewButtonWithIcon(u.t("action.start"), theme.MediaPlayIcon(), func() {
		switch p.state {
		case "Running":
			u.sessions.tabs.SelectIndex(0)
			u.navigate(1)
		case "Stopped":
			u.runAction("start")
		default:
			u.requestRefresh()
		}
	})
	p.primary.Importance = widget.HighImportance
	p.primary.Disable()
	more := widget.NewButtonWithIcon(u.t("action.more"), theme.MoreHorizontalIcon(), nil)
	// Anchor menus to the invoking control, including on small windows.
	more.OnTapped = func() {
		items := []*fyne.MenuItem{fyne.NewMenuItem(u.t("action.refresh"), u.requestRefresh), fyne.NewMenuItem(u.t("help.title"), u.showHelp)}
		if p.state == "Running" {
			items = append(items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem(u.t("action.restart"), func() { u.confirmAction("restart") }), fyne.NewMenuItem(u.t("action.stop"), func() { u.confirmAction("stop") }))
		}
		widget.NewPopUpMenu(fyne.NewMenu("", items...), u.window.Canvas()).ShowAtPosition(fyne.CurrentApp().Driver().AbsolutePositionForObject(more).Add(fyne.NewPos(0, more.Size().Height)))
	}
	p.activity = mutedLabel(u.t("overview.activityEmpty"))
	p.review = widget.NewButton(u.t("overview.review"), func() {
		u.sessions.search.SetText("")
		u.sessions.filter.SetSelectedIndex(2)
		u.sessions.tabs.SelectIndex(0)
		u.navigate(1)
	})
	p.review.Disable()
	p.observed = mutedLabel("")
	p.connection = widget.NewButtonWithIcon(u.t("connection.open"), theme.ComputerIcon(), func() { u.showConnection() })
	p.activeCount = metricValue()
	p.approvalCount = metricValue()
	p.review.Importance = widget.LowImportance
	p.connection.Importance = widget.LowImportance
	p.actions = container.NewHBox(p.primary, more)

	hero := tintedSurface(column(8,
		container.NewBorder(nil, nil, p.icon, nil, container.NewThemeOverride(p.title, titleTheme{})),
		p.subtitle, p.actions), accentSurfaceColor, 18)
	metrics := container.NewGridWithColumns(2,
		tintedSurface(column(2, p.activeCount, mutedLabel(u.t("overview.metricActive"))), surfaceColor, 12),
		tintedSurface(column(2, p.approvalCount, mutedLabel(u.t("overview.metricApproval"))), surfaceColor, 12))
	activity := surface(column(8, sectionTitle(u.t("overview.activity")), p.activity, container.NewHBox(p.review)))
	connect := surface(column(8, sectionTitle(u.t("connection.title")), mutedLabel(u.t("connection.intro")), container.NewHBox(p.connection)))
	panels := container.NewGridWithColumns(2, activity, connect)
	footer := column(2, mutedLabel(u.t("app.background")), p.observed)
	body := container.NewVScroll(column(16, hero, metrics, panels, inset(footer, 4)))
	p.root = inset(container.NewBorder(pageHeading(u.t("nav.overview"), u.t("overview.intro")), nil, nil, nil, body), pagePadding)

	return p
}

func (p *overviewPanel) update(s desktop.Snapshot, sessions []host.Session, err, sessionErr error) {
	u := p.ui

	state := s.State
	if err != nil {
		state = "Unknown"
	}
	setEnabled(p.primary, !u.busy)
	// State changes update the hero once; polling does not repaint unchanged
	// controls or cycle metrics through placeholder values.
	if p.state != state {
		p.state = state
		switch state {
		case "Running":
			p.title.SetText(u.t("overview.running"))
			p.subtitle.SetText(u.t("overview.runningHint"))
			p.icon.SetResource(theme.NewSuccessThemedResource(theme.ConfirmIcon()))
			p.primary.SetText(u.t("sessions.open"))
			p.primary.SetIcon(theme.ListIcon())
		case "Stopped":
			p.title.SetText(u.t("overview.stopped"))
			p.subtitle.SetText(u.t("overview.stoppedHint"))
			p.icon.SetResource(theme.MediaPauseIcon())
			p.primary.SetText(u.t("action.start"))
			p.primary.SetIcon(theme.MediaPlayIcon())
		default:
			p.title.SetText(u.t("overview.unavailable"))
			p.subtitle.SetText(u.t("overview.unavailableHint"))
			p.icon.SetResource(theme.NewWarningThemedResource(theme.WarningIcon()))
			p.primary.SetText(u.t("action.retry"))
			p.primary.SetIcon(theme.ViewRefreshIcon())
		}
		p.actions.Refresh()
	}
	reviewEnabled := false
	activeValue, approvalValue := "—", "—"
	if err != nil || sessionErr != nil {
		p.activity.SetText(u.t("sessions.unavailable"))
	} else if s.State != "Running" {
		p.activity.SetText(u.t("sessions.hostStopped"))
	} else {
		running, attention := 0, 0
		for _, item := range sessions {
			if item.State == "running" {
				running++
			}
			if item.AgentStatus == "needs_approval" && item.State == "running" {
				attention++
			}
		}
		activeValue, approvalValue = strconv.Itoa(running), strconv.Itoa(attention)
		reviewEnabled = attention > 0
		if running == 0 {
			p.activity.SetText(u.t("overview.activityEmpty"))
		} else {
			p.activity.SetText("")
		}

	}
	setMetric(p.activeCount, activeValue)
	setMetric(p.approvalCount, approvalValue)
	setEnabled(p.review, reviewEnabled)
	if err == nil {
		p.observed.SetText(u.t("overview.updated", map[string]any{"Time": time.Now().Format("15:04:05")}))
	} else {
		p.observed.SetText("")
	}
}

func (u *managerWindow) showConnection() {
	s := u.snapshot
	addresses := strings.Join(s.LAN, "\n")
	if addresses == "" {
		addresses = u.t("connection.noAddress")
	}
	content := container.NewVBox(wrappedLabel(u.t("connection.steps")), widget.NewCard(u.t("connection.computer"), s.Name, readOnlyText(addresses)), wrappedLabel(u.t("connection.sshNote")))
	copy := widget.NewButtonWithIcon(u.t("connection.copy"), theme.ContentCopyIcon(), func() { u.window.Clipboard().SetContent(strings.Join(s.LAN, "\n")) })
	if len(s.LAN) == 0 {
		copy.Disable()
	}
	content.Add(copy)
	d := dialog.NewCustom(u.t("connection.title"), u.t("action.close"), content, u.window)
	d.Resize(fyne.NewSize(560, 440))
	d.Show()
}

func metricValue() *widget.RichText {
	return widget.NewRichText(&widget.TextSegment{Text: "—", Style: widget.RichTextStyleHeading})
}
func setMetric(view *widget.RichText, value string) {
	segment := view.Segments[0].(*widget.TextSegment)
	if segment.Text == value {
		return
	}
	segment.Text = value
	view.Refresh()
}
