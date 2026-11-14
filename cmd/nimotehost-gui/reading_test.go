// SPDX-License-Identifier: Apache-2.0
package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

func TestReadingAreasAndAboutAcrossLanguages(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("Reading preview")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	defer func() { u.cancel() }()
	capture := func(name string) {
		if dir := os.Getenv("NIMOTE_GUI_PREVIEW_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, w.Canvas().Capture()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, mode := range []string{"Light", "Dark"} {
		a.Preferences().SetString("theme", mode)
		for _, code := range languageCodes {
			u.build(code, 3)
			w.Resize(fyne.NewSize(960, 640))
			w.Show()
			// Saving and discarding remain in the visible window even when the
			// settings content is longer than the available viewport.
			for _, button := range []*widget.Button{u.settings.save, u.settings.reset} {
				pos := a.Driver().AbsolutePositionForObject(button)
				if pos.Y+button.Size().Height > w.Canvas().Size().Height {
					t.Errorf("%s/%s hides the save bar", mode, code)
				}
			}
			u.showAbout()
			overlay := w.Canvas().Overlays().Top()
			var tabs *container.AppTabs
			visitObjects(overlay, func(obj fyne.CanvasObject) {
				if value, ok := obj.(*container.AppTabs); ok {
					tabs = value
				}
			})
			if tabs == nil || len(tabs.Items) != 3 {
				t.Fatal("about pages are missing")
			}
			for i, item := range tabs.Items {
				tabs.SelectIndex(i)
				if item.Content.Size().Height < 250 {
					t.Errorf("%s/%s about reading area too short: %v", mode, code, item.Content.Size())
				}
				capture(mode + "-about-" + code + "-" + []string{"overview", "privacy", "appearance"}[i])
			}
			w.Canvas().Overlays().Remove(overlay)
			text := strings.Repeat("A long diagnostic line / workspace / output / detail\n", 100)
			u.showReader(u.t("details.technical"), text)
			overlay = w.Canvas().Overlays().Top()
			var reader *widget.Label
			visitObjects(overlay, func(obj fyne.CanvasObject) {
				if value, ok := obj.(*widget.Label); ok && value.Text == text {
					reader = value
				}
				if scroll, ok := obj.(*container.Scroll); ok && scroll.Size().Height < 250 {
					t.Errorf("%s/%s reader viewport too short: %v", mode, code, scroll.Size())
				}
			})
			if reader == nil || !reader.Selectable {
				t.Fatal("reader text cannot be selected")
			}
			capture(mode + "-reader-" + code)
			w.Canvas().Overlays().Remove(overlay)
		}
	}
}

func TestPollingPreservesInputFocusAndLanguagePreservesSessionContext(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("Interaction")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	u.build("en", 3)
	defer func() { u.cancel() }()
	w.Resize(fyne.NewSize(1180, 820))
	w.Show()
	u.snapshot = desktop.Snapshot{State: "Stopped"}
	u.settings.update(u.snapshot, nil, false)
	w.Canvas().Focus(u.settings.port)
	u.settings.port.SetText("48000")
	u.settings.update(u.snapshot, nil, false)
	if w.Canvas().Focused() != u.settings.port || u.settings.save.Disabled() {
		t.Fatal("polling interrupted the active configuration input")
	}
	u.snapshot.State = "Running"
	sessions := []host.Session{{ID: "a", Kind: "agent", Agent: "codex", State: "running", AgentStatus: "needs_approval", Directory: "/work/nimote"}}
	u.sessions.update(sessions, nil, nil, "Running")
	u.sessions.search.SetText("nimote")
	u.sessions.filter.SetSelectedIndex(2)
	u.sessions.tabs.SelectIndex(1)
	u.changeLanguage("zh-Hans")
	if u.sessions.search.Text != "nimote" || u.sessions.filter.SelectedIndex() != 2 || u.sessions.tabs.SelectedIndex() != 1 || len(u.sessions.rows) != 1 || u.overview.state != "Running" {
		t.Fatal("language change lost session context or flashed an unknown service state")
	}

}

func TestRoundedSurfaceFollowsLiveAppearance(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	applyAppearance(a, "Light")
	panel := tintedSurface(widget.NewLabel("Content"), accentSurfaceColor, 12)
	var background *canvas.Rectangle
	visitObjects(panel, func(obj fyne.CanvasObject) {
		if value, ok := obj.(*canvas.Rectangle); ok && value.CornerRadius == 14 {
			background = value
		}
	})
	if background == nil {
		t.Fatal("surface background missing")
	}
	light := background.FillColor
	applyAppearance(a, "Dark")
	panel.Refresh()
	if background.FillColor == light || background.FillColor != theme.Color(accentSurfaceColor) {
		t.Fatal("surface retained its old palette after changing appearance")
	}
}
