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
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

// A window owns presentation and refresh lifetime, never the Host process.
// All widget mutations, navigation and action state run on Fyne's app goroutine.
type managerWindow struct {
	app                        fyne.App
	window                     fyne.Window
	manager                    *desktop.Manager
	messages                   *messages
	ctx                        context.Context
	cancel                     context.CancelFunc
	poll                       bool
	refresh                    chan struct{}
	busy                       bool
	snapshot                   desktop.Snapshot
	statusErr, sessionErr      error
	pages                      []fyne.CanvasObject
	content                    *fyne.Container
	navigation                 *widget.List
	currentPage                int
	sidebarName, sidebarStatus *widget.Label
	overview                   *overviewPanel
	sessions                   *sessionsPanel
	settings                   *settingsPanel
	feedbackArea               *fyne.Container
	feedback                   *widget.Label
	noticeID                   uint64
	tray                       fynedesktop.App
}

func newManagerWindow(a fyne.App, m *desktop.Manager, poll bool) (fyne.Window, context.CancelFunc) {
	a.SetIcon(applicationLogo)
	w := a.NewWindow("NimoteHost")
	w.SetIcon(applicationLogo)
	w.Resize(fyne.NewSize(1180, 820))
	ui := &managerWindow{app: a, window: w, manager: m, poll: poll}
	trayEnabled := runtime.GOOS != "linux" || a.Preferences().Bool("linux-tray")
	if tray, ok := a.(fynedesktop.App); ok && trayEnabled {
		ui.tray = tray
		tray.SetSystemTrayIcon(trayLogo)
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
	applyAppearance(u.app, u.app.Preferences().StringWithFallback("theme", "System"))
	u.ctx, u.cancel = context.WithCancel(context.Background())
	u.refresh = make(chan struct{}, 1)
	u.feedback = wrappedLabel("")
	u.feedback.Hide()
	dismiss := widget.NewButtonWithIcon("", theme.CancelIcon(), u.dismissNotice)
	dismiss.Importance = widget.LowImportance
	u.feedbackArea = inset(surface(container.NewBorder(nil, nil, widget.NewIcon(theme.InfoIcon()), dismiss, u.feedback)), 8)
	u.feedbackArea.Hide()
	u.overview = newOverviewPanel(u)
	u.sessions = newSessionsPanel(u)
	u.settings = newSettingsPanel(u)
	u.pages = []fyne.CanvasObject{u.overview.root, u.sessions.root, devicePanel(u), u.settings.root}
	u.content = container.NewStack(u.pages...)
	sidebar := u.newSidebar()
	u.currentPage = -1
	u.navigate(tab)
	main := container.NewBorder(nil, u.feedbackArea, nil, nil, u.content)
	u.window.SetContent(desktopBounds(container.NewBorder(nil, nil, sidebar, nil, main)))
	u.installShortcuts()

	quit := fyne.NewMenuItem(u.t("app.quit"), u.app.Quit)
	quit.IsQuit = true
	u.window.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("NimoteHost", fyne.NewMenuItem(u.t("about.open"), u.showAbout), fyne.NewMenuItem(u.t("help.title"), u.showHelp), fyne.NewMenuItemSeparator(), quit)))
	u.updateTray()
	if u.poll {
		go u.watch(u.ctx, u.refresh)
	}
}

func (u *managerWindow) changeLanguage(code string) {
	if u.busy || code == u.messages.code {
		return
	}
	// Preserve the service draft when rebuilding translated controls.
	port, level := u.settings.port.Text, u.settings.level.Selected
	sessions := u.sessions.source
	query, filter, sessionTab := u.sessions.search.Text, u.sessions.filter.SelectedIndex(), u.sessions.tabs.SelectedIndex()
	u.app.Preferences().SetString("language", code)
	u.build(code, u.currentPage)
	u.settings.port.SetText(port)
	u.settings.level.SetSelected(level)
	u.sessions.search.SetText(query)
	u.sessions.filter.SetSelectedIndex(filter)
	u.sessions.tabs.SelectIndex(sessionTab)
	if u.snapshot.State != "" || u.statusErr != nil {
		u.overview.update(u.snapshot, sessions, u.statusErr, u.sessionErr)
		u.sessions.update(sessions, u.statusErr, u.sessionErr, u.snapshot.State)
		u.updateSidebar(u.snapshot, u.statusErr)
	}
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
		u.statusErr, u.sessionErr = err, sessionErr
		u.overview.update(snapshot, sessions, err, sessionErr)
		u.sessions.update(sessions, err, sessionErr, snapshot.State)
		u.settings.update(snapshot, err, u.busy)
		u.updateTray()
		u.updateSidebar(snapshot, err)
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
	u.tray.SetSystemTrayMenu(fyne.NewMenu("NimoteHost · "+state,
		fyne.NewMenuItem(u.t("app.open"), func() { u.window.Show(); u.window.RequestFocus() }),
		start, stop,
		fyne.NewMenuItem(u.t("nav.settings"), func() { u.navigate(3); u.window.Show(); u.window.RequestFocus() }),
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

func (u *managerWindow) notice(text string) {
	u.noticeID++
	id, ctx := u.noticeID, u.ctx
	u.feedback.SetText(text)
	u.feedback.Show()
	u.feedbackArea.Show()
	if !u.busy {
		time.AfterFunc(6*time.Second, func() {
			fyne.Do(func() {
				if ctx.Err() == nil && u.noticeID == id {
					u.dismissNotice()
				}
			})
		})
	}
}
func (u *managerWindow) dismissNotice() {
	u.noticeID++
	u.feedbackArea.Hide()
}
func (u *managerWindow) showFailure(key string, err error) {
	u.dismissNotice()
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
func readOnlyText(text string) *widget.Label {
	label := wrappedLabel(text)
	label.TextStyle = fyne.TextStyle{Monospace: true}
	label.Selectable = true
	label.Wrapping = fyne.TextWrapBreak
	return label
}
func pageHeading(title, subtitle string) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(0, 20, 0, 0), column(2, widget.NewRichText(&widget.TextSegment{Text: title, Style: widget.RichTextStyleHeading}), mutedLabel(subtitle)))
}

func setEnabled(control fyne.Disableable, enabled bool) {
	if enabled {
		control.Enable()
	} else {
		control.Disable()
	}
}

// These shortcuts work even while an entry has focus, and rebuild safely when
// the display language changes because Canvas replaces handlers by key.
func (u *managerWindow) installShortcuts() {
	modifier := fyne.KeyModifierControl
	if runtime.GOOS == "darwin" {
		modifier = fyne.KeyModifierSuper
	}
	add := func(key fyne.KeyName, action func()) {
		u.window.Canvas().AddShortcut(&fynedesktop.CustomShortcut{KeyName: key, Modifier: modifier}, func(fyne.Shortcut) { action() })
	}
	for index, key := range []fyne.KeyName{fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4} {
		add(key, func() { u.navigate(index) })
	}
	add(fyne.KeyComma, func() { u.navigate(3) })
	add(fyne.KeyF, func() { u.navigate(1); u.sessions.tabs.SelectIndex(0); u.window.Canvas().Focus(u.sessions.search) })
	add(fyne.KeyR, u.requestRefresh)
}

func (u *managerWindow) readerSize() fyne.Size {
	size := u.window.Canvas().Size()
	return fyne.NewSize(size.Width*.88, size.Height*.84)
}

func (u *managerWindow) showReader(title, text string) {
	copy := widget.NewButtonWithIcon(u.t("action.copy"), theme.ContentCopyIcon(), func() { u.window.Clipboard().SetContent(text) })
	body := container.NewBorder(nil, container.NewHBox(copy), nil, nil, container.NewVScroll(readOnlyText(text)))
	d := dialog.NewCustom(title, u.t("action.close"), body, u.window)
	d.Resize(u.readerSize())
	d.Show()
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
