// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
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

type settingsPanel struct {
	root                              fyne.CanvasObject
	ui                                *managerWindow
	language                          *widget.Select
	startup                           *widget.Check
	startupHint, advancedHint         *widget.Label
	port                              *widget.Entry
	level                             *widget.Select
	save, reset, start, restart, stop *widget.Button
	status                            *widget.Label
	saved                             host.Config
	configErr                         error
	editable                          bool
	syncing                           bool
}

func newSettingsPanel(u *managerWindow) *settingsPanel {
	p := &settingsPanel{ui: u}
	p.language = widget.NewSelect(languageNames, nil)
	for i, code := range languageCodes {
		if code == u.messages.code {
			p.language.SetSelectedIndex(i)
		}
	}
	fitSelect(p.language)
	p.language.OnChanged = func(string) {
		if i := p.language.SelectedIndex(); i >= 0 {
			u.changeLanguage(languageCodes[i])
		}
	}
	appearance := widget.NewSelect([]string{u.t("theme.system"), u.t("theme.light"), u.t("theme.dark")}, nil)
	themeNames := []string{"System", "Light", "Dark"}
	savedTheme := u.app.Preferences().StringWithFallback("theme", "System")
	for i, name := range themeNames {
		if savedTheme == name {
			appearance.SetSelectedIndex(i)
		}
	}
	applyTheme := func(index int) {
		u.app.Settings().SetTheme(desktopTheme{Theme: theme.DefaultTheme(), appearance: index})
	}
	fitSelect(appearance)
	appearance.OnChanged = func(string) {
		i := appearance.SelectedIndex()
		if i >= 0 {
			applyTheme(i)
			u.app.Preferences().SetString("theme", themeNames[i])
		}
	}
	p.startupHint = mutedLabel("")
	p.startup = widget.NewCheck(u.t("settings.startup"), func(enabled bool) {
		if p.syncing {
			return
		}
		action := "disable-autostart"
		if enabled {
			action = "enable-autostart"
		}
		u.runAction(action)
	})
	general := column(12,
		container.NewGridWithColumns(2, settingField(u.t("settings.language"), "", p.language), settingField(u.t("settings.appearance"), "", appearance)),
		column(0, p.startup, p.startupHint))
	if runtime.GOOS == "linux" {
		tray := widget.NewCheck(u.t("settings.linuxTray"), nil)
		tray.SetChecked(u.app.Preferences().Bool("linux-tray"))
		tray.OnChanged = func(on bool) { u.app.Preferences().SetBool("linux-tray", on); u.notice(u.t("settings.reopen")) }
		general.Add(tray)
	}
	p.port = widget.NewEntry()
	p.level = widget.NewSelect([]string{"debug", "info", "warn", "error"}, nil)
	config, err := host.ReadConfig(u.manager.Dir)
	p.saved, p.configErr = config, err
	p.port.SetPlaceHolder("47831")
	p.port.Validator = func(value string) error {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1024 || port > 65535 {
			return errors.New(u.t("settings.portHint"))
		}
		return nil
	}
	fitSelect(p.level)
	if err == nil {
		p.port.SetText(strconv.Itoa(config.Port))
		p.level.SetSelected(config.LogLevel)
	}
	p.advancedHint = mutedLabel(u.t("settings.stopToEdit"))
	if err != nil {
		p.advancedHint.SetText(u.t("settings.configError"))
	}
	p.save = widget.NewButton(u.t("settings.save"), func() {
		port, err := strconv.Atoi(strings.TrimSpace(p.port.Text))
		config := host.Config{Port: port, LogLevel: p.level.Selected}
		if err != nil || config.Validate() != nil {
			u.notice(u.t("settings.invalid"))
			return
		}
		if u.busy || !p.editable || p.configErr != nil {
			return
		}
		u.busy = true
		p.update(u.snapshot, nil, true)
		u.overview.primary.Disable()
		u.updateTray()
		ctx := u.ctx
		go func() {
			err := u.manager.SaveConfig(config)
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				u.busy = false
				u.overview.primary.Enable()
				if err == nil {
					p.saved = config
				}
				p.update(u.snapshot, nil, false)
				u.updateTray()
				if err != nil {
					u.showFailure("settings.saveError", err)
				} else {
					u.notice(u.t("settings.saved"))
				}
				u.requestRefresh()
			})
		}()
	})
	p.save.Importance = widget.HighImportance
	p.reset = widget.NewButton(u.t("settings.revert"), func() {
		p.port.SetText(strconv.Itoa(p.saved.Port))
		p.level.SetSelected(p.saved.LogLevel)
		p.updateControls()
	})
	p.status = mutedLabel("")
	p.port.OnChanged = func(string) { p.updateControls() }
	p.level.OnChanged = func(string) { p.updateControls() }

	serviceForm := container.NewGridWithColumns(2,
		settingField(u.t("settings.port"), u.t("settings.portHint"), p.port),
		settingField(u.t("settings.logLevel"), u.t("settings.logHint"), p.level))
	p.start = widget.NewButtonWithIcon(u.t("action.start"), theme.MediaPlayIcon(), func() { u.runAction("start") })
	p.restart = widget.NewButton(u.t("action.restart"), func() { u.confirmAction("restart") })
	p.stop = widget.NewButton(u.t("action.stop"), func() { u.confirmAction("stop") })
	service := column(12, p.advancedHint, container.NewHBox(p.start, p.restart, p.stop), widget.NewSeparator(), serviceForm)
	about := column(8, widget.NewLabelWithStyle("NimoteHost "+host.Version, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		mutedLabel(u.t("about.summary")),
		container.NewHBox(widget.NewButton(u.t("about.open"), u.showAbout), widget.NewButton(u.t("updates.check"), u.checkUpdates)))
	footer := inset(container.NewBorder(nil, nil, nil, container.NewHBox(p.reset, p.save), p.status), 4)
	body := container.NewVScroll(column(16,
		surface(column(12, sectionTitle(u.t("settings.general")), general)),
		surface(column(12, sectionTitle(u.t("settings.service")), service)),
		surface(column(8, sectionTitle(u.t("settings.about")), about))))
	p.root = inset(container.NewBorder(pageHeading(u.t("nav.settings"), u.t("settings.intro")), footer, nil, nil, body), pagePadding)
	p.update(u.snapshot, u.statusErr, u.busy)
	return p
}

func (p *settingsPanel) update(s desktop.Snapshot, err error, busy bool) {
	p.syncing = true
	p.startup.SetChecked(s.Autostart)
	p.syncing = false

	setEnabled(p.language, !busy)
	setEnabled(p.start, !busy && err == nil && s.State == "Stopped")
	setEnabled(p.restart, !busy && err == nil && s.State == "Running")
	setEnabled(p.stop, !busy && err == nil && s.State == "Running")
	editable := !busy && err == nil && s.State == "Stopped"
	p.editable = editable && p.configErr == nil
	if editable {
		p.startup.Enable()
		p.startupHint.SetText(p.ui.t("settings.startupHint"))
		p.advancedHint.SetText(p.ui.t("settings.advancedHint"))
	} else {
		p.startup.Disable()
		p.startupHint.SetText(p.ui.t("settings.stopForStartup"))
		p.advancedHint.SetText(p.ui.t("settings.stopToEdit"))
	}
	if p.configErr != nil {
		p.advancedHint.SetText(p.ui.t("settings.configError"))
	}
	p.updateControls()
}

func (p *settingsPanel) updateControls() {
	if p.save == nil || p.reset == nil {
		return
	}
	if p.editable {
		p.port.Enable()
		p.level.Enable()
	} else {
		p.port.Disable()
		p.level.Disable()
	}
	port, err := strconv.Atoi(strings.TrimSpace(p.port.Text))
	config := host.Config{Port: port, LogLevel: p.level.Selected}
	dirty := p.port.Text != strconv.Itoa(p.saved.Port) || config.LogLevel != p.saved.LogLevel
	setEnabled(p.reset, dirty && p.editable)
	setEnabled(p.save, dirty && p.editable && err == nil && config.Validate() == nil)
	if p.configErr != nil {
		p.status.SetText("")
		return
	}
	if dirty {
		p.status.SetText(p.ui.t("settings.unsaved"))
	} else {
		p.status.SetText(p.ui.t("settings.noChanges"))
	}
}

func (u *managerWindow) checkUpdates() {
	if host.Edition == "connect" {
		dialog.ShowInformation(u.t("updates.title"), u.t("updates.connectManaged"), u.window)
		return
	}
	ctx := u.ctx
	go func() {
		c, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		release, err := desktop.LatestRelease(c)
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				u.showFailure("updates.error", err)
				return
			}
			confirmDialog(u.window, u.t("updates.title"), u.t("updates.available", map[string]any{"Installed": host.Version, "Latest": release.Version}), u.t("updates.download"), u.t("action.cancel"), func() {
				link, err := url.Parse(release.URL)
				if err == nil {
					err = u.app.OpenURL(link)
				}
				if err != nil {
					u.showFailure("updates.error", err)
				}
			})
		})
	}()
}

func (u *managerWindow) showHelp() {
	ctx := u.ctx
	details := readOnlyText(u.t("action.loading"))
	logs := readOnlyText("")
	copy := widget.NewButtonWithIcon(u.t("help.copy"), theme.ContentCopyIcon(), nil)
	copy.Disable()
	export := widget.NewButtonWithIcon(u.t("help.export"), theme.DocumentSaveIcon(), nil)
	export.Disable()
	tabs := container.NewAppTabs(
		container.NewTabItem(u.t("help.diagnostics"), container.NewVScroll(inset(details, 12))),
		container.NewTabItem(u.t("help.logs"), container.NewVScroll(inset(logs, 12))))
	content := container.NewBorder(column(8, wrappedLabel(u.t("help.intro")), container.NewHBox(copy, export)), nil, nil, nil, tabs)
	d := dialog.NewCustom(u.t("help.title"), u.t("action.close"), content, u.window)
	d.Resize(u.readerSize())
	d.Show()
	go func() {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		report := u.manager.Diagnostics(c)
		logText, err := host.ReadRunLog(u.manager.Dir)
		if err != nil {
			logText = err.Error()
		}
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			details.SetText(report)
			logs.SetText(logText)
			copy.OnTapped = func() { u.window.Clipboard().SetContent(report); u.notice(u.t("help.copied")) }
			copy.Enable()
			export.OnTapped = func() { u.exportDiagnostics(report) }
			export.Enable()
		})
	}()
}

func (u *managerWindow) exportDiagnostics(report string) {
	d := dialog.NewFileSave(func(out fyne.URIWriteCloser, err error) {
		if err != nil {
			u.showFailure("help.exportError", err)
			return
		}
		if out == nil {
			return
		}
		_, err = fmt.Fprint(out, report)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			u.showFailure("help.exportError", err)
		}
	}, u.window)
	d.SetFileName("nimotehost-diagnostics.txt")
	d.SetConfirmText(u.t("action.save"))
	d.Show()
}
