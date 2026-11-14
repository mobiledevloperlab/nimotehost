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
	for _, code := range languageCodes {
		u.build(code, 0)
		u.snapshot = desktop.Snapshot{Name: "My Mac", State: "Running", Version: "1.0.0-dev", Autostart: true}
		u.overview.update(u.snapshot, sessions, nil, nil)
		u.sessions.update(sessions, nil, nil, "Running")
		u.settings.update(u.snapshot, nil, false)
		for index, name := range []string{"overview", "sessions", "devices", "settings"} {
			u.tabs.SelectIndex(index)
			w.Resize(fyne.NewSize(1000, 700))
			w.Show()
			if min := w.Content().MinSize(); min.Width > 1000 || min.Height > 700 {
				t.Errorf("%s/%s overflows a 1000x700 window: %v", code, name, min)
			}
			if dir := os.Getenv("NIMOTE_GUI_PREVIEW_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				f, err := os.Create(filepath.Join(dir, name+"-"+code+".png"))
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
