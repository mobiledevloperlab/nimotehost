// SPDX-License-Identifier: Apache-2.0
package main

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

type overviewPanel struct {
	root                                          fyne.CanvasObject
	ui                                            *managerWindow
	title, subtitle, activity, observed, computer *widget.Label
	icon                                          *widget.Icon
	primary, connection, review                   *widget.Button
	state                                         string
}

func newOverviewPanel(u *managerWindow) *overviewPanel {
	p := &overviewPanel{ui: u}
	p.title = widget.NewLabelWithStyle(u.t("overview.checking"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	p.subtitle = wrappedLabel(u.t("overview.checkingHint"))
	p.computer = wrappedLabel("")
	p.icon = widget.NewIcon(theme.ComputerIcon())
	p.primary = widget.NewButtonWithIcon(u.t("action.start"), theme.MediaPlayIcon(), func() {
		switch p.state {
		case "Running":
			u.tabs.SelectIndex(1)
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
	p.activity = wrappedLabel(u.t("overview.activityEmpty"))
	p.review = widget.NewButton(u.t("approval.open"), func() { u.sessions.filter.SetSelectedIndex(2); u.tabs.SelectIndex(1) })
	p.review.Hide()
	p.observed = widget.NewLabel("")
	p.connection = widget.NewButtonWithIcon(u.t("connection.open"), theme.ComputerIcon(), func() { u.showConnection() })
	p.root = container.NewPadded(container.NewVScroll(container.NewVBox(
		pageHeading(u.t("nav.overview"), u.t("overview.intro")),
		widget.NewCard("", "", container.NewPadded(container.NewVBox(container.NewBorder(nil, nil, p.icon, nil, container.NewVBox(p.title, p.subtitle)), p.computer, container.NewHBox(p.primary, more)))),
		widget.NewCard(u.t("overview.activity"), "", container.NewPadded(container.NewVBox(p.activity, container.NewHBox(p.review)))),
		widget.NewCard(u.t("connection.title"), "", container.NewPadded(container.NewVBox(wrappedLabel(u.t("connection.intro")), container.NewHBox(p.connection)))),
		layout.NewSpacer(), wrappedLabel(u.t("app.background")), p.observed,
	)))
	return p
}

func (p *overviewPanel) update(s desktop.Snapshot, sessions []host.Session, err, sessionErr error) {
	u := p.ui
	p.state = s.State
	p.computer.SetText(s.Name)
	if !u.busy {
		p.primary.Enable()
	}
	p.primary.SetIcon(theme.MediaPlayIcon())
	switch {
	case err != nil:
		p.state = "Unknown"
		p.title.SetText(u.t("overview.unavailable"))
		p.subtitle.SetText(u.t("overview.unavailableHint"))
		p.icon.SetResource(theme.WarningIcon())
		p.primary.SetText(u.t("action.retry"))
		p.primary.SetIcon(theme.ViewRefreshIcon())
	case s.State == "Running":
		p.title.SetText(u.t("overview.running"))
		p.subtitle.SetText(u.t("overview.runningHint"))
		p.icon.SetResource(theme.ConfirmIcon())
		p.primary.SetText(u.t("sessions.open"))
		p.primary.SetIcon(theme.ListIcon())
	case s.State == "Stopped":
		p.title.SetText(u.t("overview.stopped"))
		p.subtitle.SetText(u.t("overview.stoppedHint"))
		p.icon.SetResource(theme.MediaPauseIcon())
		p.primary.SetText(u.t("action.start"))
	default:
		p.title.SetText(u.t("overview.unavailable"))
		p.subtitle.SetText(u.t("overview.unavailableHint"))
		p.primary.SetText(u.t("action.retry"))
	}
	p.review.Hide()
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
		if attention > 0 {
			p.review.Show()
			p.activity.SetText(u.t("overview.attention", map[string]any{"Count": attention}))
		} else if running > 0 {
			p.activity.SetText(u.t("overview.active", map[string]any{"Count": running}))
		} else {
			p.activity.SetText(u.t("overview.activityEmpty"))
		}
	}
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
