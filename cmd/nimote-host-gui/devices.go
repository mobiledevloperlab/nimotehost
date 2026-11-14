// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
	"runtime"
	"strings"
)

func devicePanel(w fyne.Window, m *desktop.Manager) fyne.CanvasObject {
	name := widget.NewEntry()
	name.SetPlaceHolder("Device name")
	selected := widget.NewSelect(nil, nil)
	info := widget.NewMultiLineEntry()
	info.Disable()
	var devices []host.Device
	show := func(id string) {
		for _, d := range devices {
			if d.ID == id {
				state := "Authorized (connection state unknown)"
				if d.Revoked != nil {
					state = "Revoked"
				}
				text := fmt.Sprintf("%s\nID: %s\nRole: observer (read-only)\n%s\nCreated: %s\n", d.Name, d.ID, state, d.Created)
				if runtime.GOOS != "windows" {
					command, err := host.DeviceSSHCommand(m.Binary, d.ID)
					if err == nil {
						escaped := strings.ReplaceAll(strings.ReplaceAll(command, "\\", "\\\\"), "\"", "\\\"")
						text += "\nBind a UNIQUE SSH public key in authorized_keys:\nrestrict,command=\"" + escaped + "\" ssh-ed25519 <PUBLIC-KEY>\n\nNever also authorize this key for a normal shell. Existing full-SSH credentials are unaffected.\n"
					}
				} else {
					text += "\nWindows: configure the unique key with an OpenSSH forced command using the server's configured shell quoting. Automatic key installation is not available.\n"
				}
				info.SetText(text)
				return
			}
		}
	}
	selected.OnChanged = show
	refresh := func() {
		var err error
		devices, err = host.ListDevices(m.Dir)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		ids := []string{}
		for _, d := range devices {
			ids = append(ids, d.ID)
		}
		selected.SetOptions(ids)
		show(selected.Selected)
	}
	add := widget.NewButton("Register read-only device", func() {
		d, err := host.CreateDevice(m.Dir, name.Text)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		refresh()
		selected.SetSelected(d.ID)
		name.SetText("")
	})
	revoke := widget.NewButton("Revoke selected device…", func() {
		id := selected.Selected
		if id == "" {
			return
		}
		dialog.ShowConfirm("Revoke device?", "The next Host request from this device will be rejected, including requests on an existing RPC stream. Other devices and running tasks remain unchanged. Remove its restricted SSH key separately to prevent new SSH logins.", func(ok bool) {
			if !ok {
				return
			}
			if err := host.RevokeDevice(m.Dir, id); err != nil {
				dialog.ShowError(err, w)
				return
			}
			refresh()
		}, w)
	})
	notice := widget.NewLabel("Read-only devices can inspect Host sessions/output. They cannot execute commands, send terminal input, approve tasks or manage Host. Full SSH accounts remain outside device isolation.")
	notice.Wrapping = fyne.TextWrapWord
	refresh()
	return container.NewBorder(container.NewVBox(notice, name, add, selected, container.NewHBox(widget.NewButton("Refresh", refresh), revoke)), nil, nil, nil, info)
}
