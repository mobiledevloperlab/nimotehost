// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"fmt"
	"image/color"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

type selectedTheme struct {
	fyne.Theme
	variant fyne.ThemeVariant
}

func (t selectedTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.Theme.Color(n, t.variant)
}

type settingsPanel struct {
	root                      fyne.CanvasObject
	ui                        *managerWindow
	language                  *widget.Select
	startup                   *widget.Check
	startupHint, advancedHint *widget.Label
	port                      *widget.Entry
	level                     *widget.Select
	save, restart, stop       *widget.Button
	syncing                   bool
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
		switch index {
		case 1:
			u.app.Settings().SetTheme(selectedTheme{theme.DefaultTheme(), theme.VariantLight})
		case 2:
			u.app.Settings().SetTheme(selectedTheme{theme.DefaultTheme(), theme.VariantDark})
		default:
			u.app.Settings().SetTheme(theme.DefaultTheme())
		}
	}
	fitSelect(appearance)
	applyTheme(appearance.SelectedIndex())
	appearance.OnChanged = func(string) {
		i := appearance.SelectedIndex()
		if i >= 0 {
			applyTheme(i)
			u.app.Preferences().SetString("theme", themeNames[i])
		}
	}
	p.startupHint = wrappedLabel("")
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
	general := container.NewVBox(widget.NewForm(widget.NewFormItem(u.t("settings.language"), p.language), widget.NewFormItem(u.t("settings.appearance"), appearance)), p.startup, p.startupHint)
	if runtime.GOOS == "linux" {
		tray := widget.NewCheck(u.t("settings.linuxTray"), nil)
		tray.SetChecked(u.app.Preferences().Bool("linux-tray"))
		tray.OnChanged = func(on bool) { u.app.Preferences().SetBool("linux-tray", on); u.notice(u.t("settings.reopen")) }
		general.Add(tray)
	}
	p.port = widget.NewEntry()
	p.level = widget.NewSelect([]string{"debug", "info", "warn", "error"}, nil)
	config, err := host.ReadConfig(u.manager.Dir)
	if err == nil {
		p.port.SetText(strconv.Itoa(config.Port))
		p.level.SetSelected(config.LogLevel)
	}
	p.advancedHint = wrappedLabel(u.t("settings.stopToEdit"))
	if err != nil {
		p.advancedHint.SetText(u.t("settings.configError"))
	}
	p.save = widget.NewButton(u.t("settings.save"), func() {
		port, err := strconv.Atoi(p.port.Text)
		config := host.Config{Port: port, LogLevel: p.level.Selected}
		if err != nil || config.Validate() != nil {
			u.notice(u.t("settings.invalid"))
			return
		}
		if u.busy {
			return
		}
		u.busy = true
		p.update(u.snapshot, nil, true)
		u.overview.primary.Disable()
		ctx := u.ctx
		go func() {
			err := u.manager.SaveConfig(config)
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				u.busy = false
				u.overview.primary.Enable()
				p.update(u.snapshot, nil, false)
				if err != nil {
					u.showFailure("settings.saveError", err)
				} else {
					u.notice(u.t("settings.saved"))
				}
				u.requestRefresh()
			})
		}()
	})
	advanced := container.NewVBox(p.advancedHint, widget.NewForm(widget.NewFormItem(u.t("settings.port"), p.port), widget.NewFormItem(u.t("settings.logLevel"), p.level)), p.save)
	p.restart = widget.NewButton(u.t("action.restart"), func() { u.confirmAction("restart") })
	p.stop = widget.NewButton(u.t("action.stop"), func() { u.confirmAction("stop") })
	maintenance := container.NewHBox(p.restart, p.stop)
	advanced.Add(widget.NewSeparator())
	advanced.Add(maintenance)
	about := container.NewVBox(widget.NewLabel("Nimote Host "+host.Version), container.NewHBox(widget.NewButton(u.t("updates.check"), u.checkUpdates), widget.NewButtonWithIcon(u.t("help.title"), theme.HelpIcon(), u.showHelp)))
	p.root = container.NewPadded(container.NewVScroll(container.NewVBox(pageHeading(u.t("nav.settings"), u.t("settings.intro")), widget.NewCard(u.t("settings.general"), "", container.NewPadded(general)), widget.NewAccordion(widget.NewAccordionItem(u.t("settings.advanced"), advanced)), widget.NewCard(u.t("settings.about"), "", container.NewPadded(about)))))
	return p
}

func (p *settingsPanel) update(s desktop.Snapshot, err error, busy bool) {
	p.syncing = true
	p.startup.SetChecked(s.Autostart)
	p.syncing = false
	p.language.Enable()
	if busy {
		p.language.Disable()
	}
	p.restart.Disable()
	p.stop.Disable()
	if !busy && err == nil && s.State == "Running" {
		p.restart.Enable()
		p.stop.Enable()
	}
	editable := !busy && err == nil && s.State == "Stopped"
	if editable {
		p.startup.Enable()
		p.port.Enable()
		p.level.Enable()
		p.save.Enable()
		p.startupHint.SetText(p.ui.t("settings.startupHint"))
		p.advancedHint.SetText(p.ui.t("settings.advancedHint"))
	} else {
		p.startup.Disable()
		p.port.Disable()
		p.level.Disable()
		p.save.Disable()
		p.startupHint.SetText(p.ui.t("settings.stopForStartup"))
		p.advancedHint.SetText(p.ui.t("settings.stopToEdit"))
	}
}

func (u *managerWindow) checkUpdates() {
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
	content := container.NewVBox(wrappedLabel(u.t("help.intro")), container.NewHBox(copy, export), widget.NewAccordion(widget.NewAccordionItem(u.t("help.diagnostics"), details), widget.NewAccordionItem(u.t("help.logs"), logs)))
	d := dialog.NewCustom(u.t("help.title"), u.t("action.close"), container.NewVScroll(content), u.window)
	d.Resize(fyne.NewSize(700, 500))
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
	d.SetFileName("nimote-host-diagnostics.txt")
	d.SetConfirmText(u.t("action.save"))
	d.Show()
}
