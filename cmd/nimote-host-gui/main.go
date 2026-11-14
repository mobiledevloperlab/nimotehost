// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fynedesktop "fyne.io/fyne/v2/driver/desktop"
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
func main() {
	a := app.NewWithID("com.nimote.host.desktop")
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	name := "nimote-host"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Release bundles keep the CLI beside the desktop executable, including inside .app.
	m, err := desktop.New(filepath.Join(filepath.Dir(executable), name))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	runUI(a, m)
}
func runUI(a fyne.App, m *desktop.Manager) {
	w, cleanup := newManagerWindow(a, m, true)
	defer cleanup()
	w.ShowAndRun()
}
func newManagerWindow(a fyne.App, m *desktop.Manager, poll bool) (fyne.Window, context.CancelFunc) {
	w := a.NewWindow("Nimote Host Desktop")
	w.Resize(fyne.NewSize(840, 580))
	a.SetIcon(theme.ComputerIcon())
	status := widget.NewLabel("Connecting to local Host…")
	details := widget.NewLabel("")
	details.Wrapping = fyne.TextWrapWord
	feedback := widget.NewLabel("")
	feedback.Wrapping = fyne.TextWrapWord
	sessions := widget.NewMultiLineEntry()
	sessions.Disable()
	sessionSelect := widget.NewSelect(nil, nil)
	sessionView := container.NewBorder(container.NewHBox(sessionSelect, widget.NewButton("Inspect Agent / approvals", func() { inspectAgent(w, m, sessionSelect.Selected) })), nil, nil, nil, sessions)
	diagnostics := widget.NewMultiLineEntry()
	diagnostics.Disable()
	logs := widget.NewMultiLineEntry()
	logs.Disable()
	auto := widget.NewLabel("")
	var tray fynedesktop.App
	// Linux tray availability cannot be inferred from the desktop driver interface.
	// Keep a recoverable window by default; users can opt into a known working tray.
	trayEnabled := runtime.GOOS != "linux" || a.Preferences().Bool("linux-tray")
	if d, ok := a.(fynedesktop.App); ok && trayEnabled {
		tray = d
		tray.SetSystemTrayIcon(theme.ComputerIcon())
	}
	ctx, cancel := context.WithCancel(context.Background())
	refresh := func() {
		c, done := context.WithTimeout(ctx, 4*time.Second)
		defer done()
		s, err := m.Status(c)
		text := fmt.Sprintf("%s · %s\n%s · Host %s\nLocal IPC %s\nLAN %s (connect through existing SSH)", s.Name, s.Platform, s.State, s.Version, s.Address, strings.Join(s.LAN, ", "))
		sessionText := "Host is stopped."
		var sessionIDs []string
		if err != nil {
			sessionText = "Unavailable: " + err.Error()
		} else if s.State == "Running" {
			list, e := m.Sessions(c)
			if e != nil {
				sessionText = "Unavailable: " + e.Error()
			} else {
				var rows []string
				for _, item := range list {
					if item.Kind == "agent" && item.Interaction == "structured" {
						sessionIDs = append(sessionIDs, item.ID)
					}
					rows = append(rows, fmt.Sprintf("%s  %s  %s  %s  %s", item.ID, item.Kind, item.Agent, item.State, item.AgentStatus))
				}
				sessionText = strings.Join(rows, "\n")
				if sessionText == "" {
					sessionText = "No recorded sessions."
				}
			}
		}
		diagnosticText := m.Diagnostics(c)
		logText, e := host.ReadRunLog(m.Dir)
		if e != nil {
			logText = e.Error()
		}
		fyne.Do(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				status.SetText("Host status unavailable")
				details.SetText(err.Error())
			} else {
				status.SetText("Host " + s.State)
				details.SetText(text)
			}
			sessions.SetText(sessionText)
			sessionSelect.SetOptions(sessionIDs)
			diagnostics.SetText(diagnosticText)
			logs.SetText(logText)
			auto.SetText(fmt.Sprintf("Login service definition installed: %t", s.Autostart))
			if tray != nil {
				tray.SetSystemTrayMenu(fyne.NewMenu("Nimote Host · "+s.State, fyne.NewMenuItem("Host "+s.State, nil), fyne.NewMenuItem("Open Manager", func() { w.Show(); w.RequestFocus() }), fyne.NewMenuItem("Start", func() { perform(w, m, "start", feedback) }), fyne.NewMenuItem("Stop Host…", func() { confirmChange(w, m, "stop", feedback) }), fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Quit GUI (keep Host running)", a.Quit)))
			}
		})
	}
	action := func(name string) *widget.Button {
		return widget.NewButton(strings.Title(name), func() {
			if name == "start" {
				perform(w, m, name, feedback)
			} else {
				confirmChange(w, m, name, feedback)
			}
		})
	}
	overview := container.NewVBox(status, details, container.NewHBox(action("start"), action("stop"), action("restart"), widget.NewButton("Refresh", func() { go refresh() })), widget.NewSeparator(), widget.NewLabel("Closing or quitting this manager keeps Host and its tasks running."), feedback)
	port := widget.NewEntry()
	level := widget.NewSelect([]string{"debug", "info", "warn", "error"}, nil)
	config, e := host.ReadConfig(m.Dir)
	if e != nil {
		feedback.SetText(e.Error())
	} else {
		port.SetText(strconv.Itoa(config.Port))
		level.SetSelected(config.LogLevel)
	}
	chooseTheme := func(value string) {
		switch value {
		case "Light":
			a.Settings().SetTheme(selectedTheme{theme.DefaultTheme(), theme.VariantLight})
		case "Dark":
			a.Settings().SetTheme(selectedTheme{theme.DefaultTheme(), theme.VariantDark})
		default:
			a.Settings().SetTheme(theme.DefaultTheme())
		}
		a.Preferences().SetString("theme", value)
	}
	themeSelect := widget.NewSelect([]string{"System", "Light", "Dark"}, chooseTheme)
	themeSelect.SetSelected(a.Preferences().StringWithFallback("theme", "System"))
	save := widget.NewButton("Save Host settings (Host must be stopped)", func() {
		p, err := strconv.Atoi(port.Text)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		c := host.Config{Port: p, LogLevel: level.Selected}
		go func() {
			err := m.SaveConfig(c)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, w)
				} else {
					feedback.SetText("Host settings saved. Start Host to apply.")
				}
			})
		}()
	})
	settings := container.NewVBox(widget.NewForm(widget.NewFormItem("Host loopback port", port), widget.NewFormItem("Host log level", level), widget.NewFormItem("Appearance", themeSelect)), save, widget.NewSeparator(), auto, widget.NewLabel("Stop Host before changing login startup. Enabling it also starts Host."), container.NewHBox(widget.NewButton("Enable login startup", func() { perform(w, m, "enable-autostart", feedback) }), widget.NewButton("Disable login startup", func() { perform(w, m, "disable-autostart", feedback) })))
	if runtime.GOOS == "linux" {
		check := widget.NewCheck("Use system tray (requires tray support; reopen GUI to apply)", func(v bool) { a.Preferences().SetBool("linux-tray", v) })
		check.SetChecked(a.Preferences().Bool("linux-tray"))
		settings.Add(check)
	}
	devices := devicePanel(w, m)
	export := widget.NewButton("Export diagnostics…", func() {
		d := dialog.NewFileSave(func(out fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if out == nil {
				return
			}
			_, err = out.Write([]byte(diagnostics.Text))
			closeErr := out.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				dialog.ShowError(err, w)
			}
		}, w)
		d.SetFileName("nimote-host-diagnostics.txt")
		d.Show()
	})
	diagnosticView := container.NewBorder(container.NewHBox(widget.NewButton("Copy diagnostics", func() { w.Clipboard().SetContent(diagnostics.Text) }), export), nil, nil, nil, container.NewVSplit(diagnostics, logs))
	tabs := container.NewAppTabs(container.NewTabItem("Host", overview), container.NewTabItem("Sessions", sessionView), container.NewTabItem("History", historyPanel(w, m)), container.NewTabItem("Devices", devices), container.NewTabItem("Settings", container.NewVScroll(settings)), container.NewTabItem("Diagnostics", diagnosticView))
	settings.Add(widget.NewSeparator())
	settings.Add(widget.NewLabel("Desktop version: " + host.Version))
	settings.Add(widget.NewButton("Check published version…", func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			release, err := desktop.LatestRelease(ctx)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				dialog.ShowConfirm("Published version", "Installed: "+host.Version+"\nLatest published: "+release.Version+"\nOpen official downloads? Installation uses the existing verified bundle installer; active tasks must finish before updating.", func(ok bool) {
					if ok {
						u, _ := url.Parse(release.URL)
						if err := a.OpenURL(u); err != nil {
							dialog.ShowError(err, w)
						}
					}
				}, w)
			})
		}()
	}))
	w.SetContent(container.NewBorder(nil, container.NewHBox(widget.NewButton("Quit GUI · keep Host running", a.Quit)), nil, nil, tabs))
	if tray != nil {
		w.SetCloseIntercept(w.Hide)
	} // Without a usable tray, normal window close quits only GUI.
	if poll {
		go func() {
			refresh()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refresh()
				}
			}
		}()
	}
	return w, cancel
}
func confirmChange(w fyne.Window, m *desktop.Manager, action string, feedback *widget.Label) {
	dialog.ShowConfirm(strings.Title(action)+" Host?", "Stopping or restarting Host ends active Host tasks and terminal sessions. Existing SSH is managed separately. Quitting the GUI keeps Host running.", func(ok bool) {
		if ok {
			perform(w, m, action, feedback)
		}
	}, w)
}
func perform(w fyne.Window, m *desktop.Manager, action string, feedback *widget.Label) {
	feedback.SetText("Working: " + action)
	go func() {
		err := m.Change(context.Background(), action)
		fyne.Do(func() {
			if err != nil {
				feedback.SetText(action + ": " + err.Error())
				dialog.ShowError(err, w)
			} else {
				feedback.SetText(action + " completed; status refreshes automatically.")
			}
		})
	}()
}
