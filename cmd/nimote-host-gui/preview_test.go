// SPDX-License-Identifier: Apache-2.0
package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

func TestDesktopLayoutsAcrossLanguages(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("Nimote Host")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	defer func() {
		if u.cancel != nil {
			u.cancel()
		}
	}()
	sessions := []host.Session{
		{ID: "approval", Kind: "agent", Agent: "codex", Interaction: "structured", State: "running", AgentStatus: "needs_approval", Directory: "/workspace/nimote", Started: time.Now()},
		{ID: "terminal", Kind: "terminal", State: "running", Directory: "/workspace/web", Started: time.Now()},
	}
	for _, appearance := range []string{"Light", "Dark"} {
		a.Preferences().SetString("theme", appearance)
		for _, code := range languageCodes {
			u.build(code, 0)
			u.snapshot = desktop.Snapshot{Name: "My Mac", State: "Running", Version: "1.0.0-dev", Autostart: true}
			u.updateSidebar(u.snapshot, nil)
			u.overview.update(u.snapshot, sessions, nil, nil)
			u.sessions.update(sessions, nil, nil, "Running")
			u.settings.update(u.snapshot, nil, false)
			for index, name := range []string{"overview", "sessions", "devices", "settings"} {
				u.navigate(index)
				w.Resize(fyne.NewSize(1000, 700))
				w.Show()
				if index == 0 {
					for _, button := range []*widget.Button{u.overview.primary, u.overview.review, u.overview.connection} {
						if button.Size().Width+1 < button.MinSize().Width {
							t.Errorf("%s/%s clips button %q: %v < %v", appearance, code, button.Text, button.Size(), button.MinSize())
						}
					}
				}
				if min := w.Content().MinSize(); min.Width > 1000 || min.Height > 700 {
					t.Errorf("%s/%s overflows a 1000x700 window: %v", code, name, min)
				}
				if dir := os.Getenv("NIMOTE_GUI_PREVIEW_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, appearance+"-"+name+"-"+code+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, w.Canvas().Capture())
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}
