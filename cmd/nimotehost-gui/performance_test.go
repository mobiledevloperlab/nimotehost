// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"fyne.io/fyne/v2"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
	"testing"
)

func BenchmarkPluginUnchangedRefresh(b *testing.B) {
	a := newUITestApp()
	defer a.Quit()
	m, _ := newMessages("en")
	p := newPluginsPanel(&managerWindow{app: a, messages: m})
	var plugins []desktop.PluginInfo
	var caps []desktop.PluginCapability
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("plugin-%d", i)
		plugins = append(plugins, desktop.PluginInfo{ID: id, State: "ready", Manifest: sdk.Manifest{Version: "1.0.0", Publisher: "test"}})
		for j := 0; j < 2; j++ {
			caps = append(caps, desktop.PluginCapability{ID: fmt.Sprintf("%s/%d", id, j), Plugin: id, State: "ready", Capability: sdk.Capability{Title: "Capability", Kind: "tool", Version: "1.0.0"}})
		}
	}
	p.render(plugins, caps, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.render(plugins, caps, nil)
	}
}

func TestUnchangedPluginRefreshPreservesControls(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	m, _ := newMessages("en")
	p := newPluginsPanel(&managerWindow{app: a, messages: m})
	plugins := []desktop.PluginInfo{{ID: "echo", State: "ready"}}
	p.render(plugins, nil, nil)
	first := p.cards.Objects[0]
	p.render(plugins, nil, nil)
	if p.cards.Objects[0] != first {
		t.Fatal("unchanged refresh rebuilt controls")
	}
	plugins[0].State = "disabled"
	p.render(plugins, nil, nil)
	if p.cards.Objects[0] == first {
		t.Fatal("changed state was not rendered")
	}
	p.render(nil, nil, fmt.Errorf("offline"))
	p.render(plugins, nil, nil)
	if len(p.cards.Objects) != 1 {
		t.Fatal("did not recover after error")
	}
}

type recordingTray struct {
	calls int
	menu  *fyne.Menu
}

func (r *recordingTray) SetSystemTrayMenu(m *fyne.Menu) { r.calls++; r.menu = m }
func (*recordingTray) SetSystemTrayIcon(fyne.Resource)  {}
func (*recordingTray) SetSystemTrayWindow(fyne.Window)  {}

func TestTrayUpdatesOnlyWhenStateBusyOrLanguageChanges(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	m, _ := newMessages("en")
	tray := &recordingTray{}
	u := &managerWindow{app: a, messages: m, tray: tray}
	u.updateTray()
	u.updateTray()
	if tray.calls != 1 {
		t.Fatal("unchanged status rebuilt tray")
	}
	u.snapshot.State = "Running"
	u.updateTray()
	if tray.calls != 2 || tray.menu.Items[2].Disabled {
		t.Fatal("running state not applied")
	}
	u.busy = true
	u.updateTray()
	if tray.calls != 3 || !tray.menu.Items[2].Disabled {
		t.Fatal("busy state not applied")
	}
	u.messages, _ = newMessages("zh-Hans")
	u.updateTray()
	if tray.calls != 4 {
		t.Fatal("language change not applied")
	}
}

func BenchmarkGlobalNavigation(b *testing.B) {
	a := newUITestApp()
	defer a.Quit()
	w := a.NewWindow("Navigation")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: b.TempDir()}}
	u.build("en", 0)
	defer u.cancel()
	w.Resize(fyne.NewSize(960, 640))
	w.Show()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.navigate(3)
		u.navigate(0)
	}
}

type pageLayoutProbe struct {
	fyne.CanvasObject
	refreshes, resizes int
}

func (p *pageLayoutProbe) Refresh()              { p.refreshes++; p.CanvasObject.Refresh() }
func (p *pageLayoutProbe) Resize(size fyne.Size) { p.resizes++; p.CanvasObject.Resize(size) }

func TestInactivePagesDoNotParticipateInRefreshOrResize(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	w := a.NewWindow("Navigation")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: t.TempDir()}}
	u.build("en", 0)
	defer u.cancel()
	probe := &pageLayoutProbe{CanvasObject: u.pages[2]}
	u.pages[2] = probe
	u.navigate(2)
	if len(u.content.Objects) != 1 || u.content.Objects[0] != probe || probe.refreshes == 0 {
		t.Fatal("active page is not attached and refreshed")
	}
	u.navigate(3)
	u.settings.port.SetText("48001")
	u.navigate(0)
	probe.refreshes, probe.resizes = 0, 0
	u.content.Refresh()
	u.content.Resize(fyne.NewSize(1000, 700))
	if probe.refreshes != 0 || probe.resizes != 0 {
		t.Fatal("inactive page still participates in layout", probe.refreshes, probe.resizes)
	}
	u.navigate(3)
	if u.settings.port.Text != "48001" {
		t.Fatal("navigation discarded settings draft")
	}
}
