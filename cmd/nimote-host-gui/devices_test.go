// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
	"os"
	"path/filepath"
	"testing"
)

func TestDevicesPanelRegistersIndependentObservers(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("Devices")
	defer w.Close()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	panel := devicePanel(w, &desktop.Manager{Dir: dir, Binary: filepath.Join(dir, "nimote-host")})
	w.SetContent(panel)
	var name *widget.Entry
	var add *widget.Button
	var visit func(fyne.CanvasObject)
	visit = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *fyne.Container:
			for _, child := range v.Objects {
				visit(child)
			}
		case *widget.Entry:
			if v.PlaceHolder == "Device name" {
				name = v
			}
		case *widget.Button:
			if v.Text == "Register read-only device" {
				add = v
			}
		}
	}
	visit(panel)
	if name == nil || add == nil {
		t.Fatal("registration controls unavailable")
	}
	for _, value := range []string{"Phone A", "Tablet B"} {
		name.SetText(value)
		test.Tap(add)
	}
	devices, err := host.ListDevices(dir)
	if err != nil || len(devices) != 2 {
		t.Fatalf("registration: %v %v", devices, err)
	}
	if devices[0].ID == devices[1].ID || devices[0].Role != "observer" || devices[1].Role != "observer" {
		t.Fatal("device identities not independent read-only observers")
	}
}
