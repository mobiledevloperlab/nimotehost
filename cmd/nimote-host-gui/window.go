// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fynedesktop "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

// A window owns presentation and refresh lifetime, never the Host process.
// All widget mutations, navigation and action state run on Fyne's app goroutine.
type managerWindow struct {
	app      fyne.App
	window   fyne.Window
	manager  *desktop.Manager
	messages *messages
	ctx      context.Context
	cancel   context.CancelFunc
	poll     bool
	refresh  chan struct{}
	busy     bool
	snapshot desktop.Snapshot
	tabs     *container.AppTabs
	overview *overviewPanel
	sessions *sessionsPanel
	settings *settingsPanel
	feedback *widget.Label
	tray     fynedesktop.App
}

func newManagerWindow(a fyne.App, m *desktop.Manager, poll bool) (fyne.Window, context.CancelFunc) {
	w := a.NewWindow("Nimote Host")
	w.Resize(fyne.NewSize(1000, 700))
	a.SetIcon(theme.ComputerIcon())
	ui := &managerWindow{app: a, window: w, manager: m, poll: poll}
	trayEnabled := runtime.GOOS != "linux" || a.Preferences().Bool("linux-tray")
	if tray, ok := a.(fynedesktop.App); ok && trayEnabled {
		ui.tray = tray
		tray.SetSystemTrayIcon(theme.ComputerIcon())
		w.SetCloseIntercept(w.Hide)
	}
	ui.build(a.Preferences().StringWithFallback("language", "en"), 0)
	return w, func() {
		if ui.cancel != nil {
			ui.cancel()
		}
	}
}

func (u *managerWindow) t(key string, data ...map[string]any) string {
	return u.messages.t(key, data...)
}

func (u *managerWindow) build(code string, tab int) {
	if u.cancel != nil {
		u.cancel()
	}
	messages, err := newMessages(code)
	if err != nil {
		dialog.ShowError(err, u.window)
		return
	}
	u.messages = messages
	u.ctx, u.cancel = context.WithCancel(context.Background())
	u.refresh = make(chan struct{}, 1)
	u.feedback = wrappedLabel("")
	u.feedback.Hide()
	u.overview = newOverviewPanel(u)
	u.sessions = newSessionsPanel(u)
	u.settings = newSettingsPanel(u)
	u.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon(u.t("nav.overview"), theme.HomeIcon(), u.overview.root),
		container.NewTabItemWithIcon(u.t("nav.sessions"), theme.ListIcon(), u.sessions.root),
		container.NewTabItemWithIcon(u.t("nav.devices"), theme.ComputerIcon(), devicePanel(u)),
		container.NewTabItemWithIcon(u.t("nav.settings"), theme.SettingsIcon(), u.settings.root),
	)
	u.tabs.SetTabLocation(container.TabLocationLeading)
	if tab >= 0 && tab < len(u.tabs.Items) {
		u.tabs.SelectIndex(tab)
	}
	u.window.SetContent(container.NewBorder(nil, container.NewPadded(u.feedback), nil, nil, u.tabs))
	quit := fyne.NewMenuItem(u.t("app.quit"), u.app.Quit)
	quit.IsQuit = true
	u.window.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("Nimote Host", fyne.NewMenuItem(u.t("help.title"), u.showHelp), fyne.NewMenuItemSeparator(), quit)))
	u.updateTray()
	if u.poll {
		go u.watch(u.ctx, u.refresh)
	}
}

func (u *managerWindow) changeLanguage(code string) {
	if u.busy || code == u.messages.code {
		return
	}
	u.app.Preferences().SetString("language", code)
	u.build(code, u.tabs.SelectedIndex())
}

func (u *managerWindow) requestRefresh() {
	select {
	case u.refresh <- struct{}{}:
	default:
	}
}

func (u *managerWindow) watch(ctx context.Context, requests <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		u.readSnapshot(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-requests:
		}
	}
}

func (u *managerWindow) readSnapshot(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	snapshot, err := u.manager.Status(c)
	var sessions []host.Session
	var sessionErr error
	if err == nil && snapshot.State == "Running" {
		sessions, sessionErr = u.manager.Sessions(c)
	}
	fyne.Do(func() {
		if ctx.Err() != nil {
			return
		}
		u.snapshot = snapshot
		u.overview.update(snapshot, sessions, err, sessionErr)
		u.sessions.update(sessions, err, sessionErr, snapshot.State)
		u.settings.update(snapshot, err, u.busy)
		u.updateTray()
	})
}

func (u *managerWindow) updateTray() {
	if u.tray == nil {
		return
	}
	state := u.t("state.unknown")
	if u.snapshot.State == "Running" {
		state = u.t("state.running")
	}
	if u.snapshot.State == "Stopped" {
		state = u.t("state.stopped")
	}
	start := fyne.NewMenuItem(u.t("action.start"), func() { u.runAction("start") })
	stop := fyne.NewMenuItem(u.t("action.stop"), func() { u.confirmAction("stop") })
	start.Disabled = u.busy || u.snapshot.State != "Stopped"
	stop.Disabled = u.busy || u.snapshot.State != "Running"
	u.tray.SetSystemTrayMenu(fyne.NewMenu("Nimote Host · "+state,
		fyne.NewMenuItem(u.t("app.open"), func() { u.window.Show(); u.window.RequestFocus() }),
		start, stop,
		fyne.NewMenuItem(u.t("nav.settings"), func() { u.tabs.SelectIndex(3); u.window.Show(); u.window.RequestFocus() }),
		fyne.NewMenuItemSeparator(), fyne.NewMenuItem(u.t("app.quit"), u.app.Quit)))
}

func (u *managerWindow) confirmAction(action string) {
	if u.busy {
		return
	}
	key := "confirm.stop"
	if action == "restart" {
		key = "confirm.restart"
	}
	confirmDialog(u.window, u.t("action."+action), u.t(key), u.t("action."+action), u.t("action.cancel"), func() { u.runAction(action) })
}

func (u *managerWindow) runAction(action string) {
	if u.busy || u.ctx.Err() != nil {
		return
	}
	u.busy = true
	u.notice(u.t("action.working"))
	u.overview.primary.Disable()
	u.settings.update(u.snapshot, nil, true)
	u.updateTray()
	ctx := u.ctx
	go func() {
		err := u.manager.Change(ctx, action)
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			u.busy = false
			u.overview.primary.Enable()
			if err != nil {
				u.showFailure("error.action", err)
			} else {
				u.notice(u.t("action.done"))
			}
			u.requestRefresh()
		})
	}()
}

func (u *managerWindow) notice(text string) { u.feedback.SetText(text); u.feedback.Show() }
func (u *managerWindow) showFailure(key string, err error) {
	content := container.NewVBox(wrappedLabel(u.t(key)), widget.NewAccordion(widget.NewAccordionItem(u.t("details.technical"), readOnlyText(err.Error()))))
	d := dialog.NewCustom(u.t("error.title"), u.t("action.close"), content, u.window)
	d.Resize(fyne.NewSize(600, 260))
	d.Show()
}

func wrappedLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}
func readOnlyText(text string) *widget.Entry {
	e := widget.NewMultiLineEntry()
	e.SetText(text)
	e.Wrapping = fyne.TextWrapWord
	e.Disable()
	return e
}
func pageHeading(title, subtitle string) fyne.CanvasObject {
	return container.NewVBox(widget.NewRichText(&widget.TextSegment{Text: title, Style: widget.RichTextStyleHeading}), wrappedLabel(subtitle))
}
func confirmDialog(w fyne.Window, title, message, confirm, cancel string, accepted func()) {
	dialog.NewCustomConfirm(title, confirm, cancel, wrappedLabel(message), func(ok bool) {
		if ok {
			accepted()
		}
	}, w).Show()
}

// Fyne's Select minimum width is based on its placeholder, not the options.
// These controls always have a selection; reserve enough room for every option
// so changing the application language doesn't clip their current value.
func fitSelect(s *widget.Select) {
	var widest float32
	for _, option := range s.Options {
		width := fyne.MeasureText(option, theme.TextSize(), fyne.TextStyle{}).Width
		if width > widest {
			widest = width
			s.PlaceHolder = option
		}
	}
	s.Refresh()
}
