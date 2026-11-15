// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

func TestPluginPageDiscoveryGatesUnavailableCapabilities(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("plugins")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	u.build("en", 0)
	defer u.cancel()
	if len(u.pages) != 5 || navigationKeys[4] != "nav.plugins" {
		t.Fatal("plugin navigation missing")
	}
	plugins := []desktop.PluginInfo{{ID: "community", State: "disabled", Manifest: sdk.Manifest{Version: "1.0.0", Publisher: "test"}}}
	caps := []desktop.PluginCapability{{ID: "community/tool", Plugin: "community", State: "disabled", Capability: sdk.Capability{Title: "Discovered tool", Kind: "tool", Version: "1.0.0"}}}
	u.plugins.render(plugins, caps, nil)
	var invoke, enable *widget.Button
	visitObjects(u.plugins.root, func(obj fyne.CanvasObject) {
		if b, ok := obj.(*widget.Button); ok {
			if b.Text == u.t("plugins.invoke") {
				invoke = b
			}
			if b.Text == u.t("plugins.enable") {
				enable = b
			}
		}
	})
	if invoke == nil || !invoke.Disabled() || enable == nil {
		t.Fatal("disabled capability can run or enable missing")
	}
	caps[0].State = "ready"
	plugins[0].State = "ready"
	u.plugins.render(plugins, caps, nil)
	visitObjects(u.plugins.root, func(obj fyne.CanvasObject) {
		if b, ok := obj.(*widget.Button); ok && b.Text == u.t("plugins.invoke") {
			invoke = b
		}
	})
	if invoke.Disabled() {
		t.Fatal("ready standard capability unavailable")
	}
}
func TestPluginInputSeedsRequiredSchemaFields(t *testing.T) {
	cap := desktop.PluginCapability{Capability: sdk.Capability{Input: json.RawMessage(`{"type":"object","properties":{"method":{"type":"string","enum":["GET","POST"]},"url":{"type":"string"},"limit":{"type":"integer","default":5}},"required":["method","url","limit"]}`)}}
	var value map[string]any
	if err := json.Unmarshal([]byte(pluginInput(cap)), &value); err != nil {
		t.Fatal(err)
	}
	if value["method"] != "GET" || value["url"] != "" || value["limit"] != float64(5) {
		t.Fatal(value)
	}
	cap.ACP = 1
	if !json.Valid([]byte(pluginInput(cap))) {
		t.Fatal("invalid ACP session input")
	}
}

func TestPluginPageKeepsLocalAndCatalogInstallPaths(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("plugins")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	u.build("en", 0)
	defer u.cancel()
	local, catalog := false, false
	visitObjects(u.plugins.root, func(obj fyne.CanvasObject) {
		if button, ok := obj.(*widget.Button); ok {
			local = local || button.Text == u.t("catalog.localInstall")
			catalog = catalog || button.Text == u.t("catalog.title")
		}
	})
	if !local || !catalog {
		t.Fatal("local or catalog installation missing")
	}
}
