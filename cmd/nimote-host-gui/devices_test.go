// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

func TestDevicesShowNamesWithoutExposingIDsOnTheList(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	dir := filepath.Join(t.TempDir(), "host")
	var devices []host.Device
	for _, name := range []string{"My phone", "Work tablet"} {
		device, err := host.CreateDevice(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		devices = append(devices, device)
	}
	w := a.NewWindow("Devices")
	defer w.Close()
	messages, err := newMessages("en")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: dir}, messages: messages, ctx: ctx}
	panel := devicePanel(u)
	w.SetContent(panel)
	labels := ""
	var add *widget.Button
	visitObjects(panel, func(obj fyne.CanvasObject) {
		switch value := obj.(type) {
		case *widget.Label:
			labels += value.Text + "\n"
		case *widget.Button:
			if value.Text == messages.t("devices.add") {
				add = value
			}
		}
	})
	for _, device := range devices {
		if !strings.Contains(labels, device.Name) || strings.Contains(labels, device.ID) {
			t.Fatal("list should identify devices by their names", labels)
		}
		if device.Role != "observer" {
			t.Fatal("UI changed device permissions")
		}
	}
	if add == nil {
		t.Fatal("device registration is not discoverable")
	}
	test.Tap(add)
	var name *widget.Entry
	visitObjects(w.Canvas().Overlays().Top(), func(obj fyne.CanvasObject) {
		if entry, ok := obj.(*widget.Entry); ok && entry.PlaceHolder == messages.t("devices.namePlaceholder") {
			name = entry
		}
	})
	if name == nil {
		t.Fatal("add device did not open its name form")
	}
	if name.Validate() == nil {
		t.Fatal("empty device name passed validation")
	}
	name.SetText("A new phone")
	if err := name.Validate(); err != nil {
		t.Fatal(err)
	}
}
