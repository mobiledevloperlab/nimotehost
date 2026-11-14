// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

func devicePanel(u *managerWindow) fyne.CanvasObject {
	cards := container.NewVBox(wrappedLabel(u.t("action.loading")))
	ctx := u.ctx
	var refresh func()
	refresh = func() {
		devices, err := host.ListDevices(u.manager.Dir)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			cards.Objects = []fyne.CanvasObject{wrappedLabel(u.t("devices.loadError")), widget.NewButton(u.t("action.retry"), refresh)}
			cards.Refresh()
			return
		}
		cards.Objects = nil
		if len(devices) == 0 {
			cards.Add(widget.NewCard(u.t("devices.empty"), "", wrappedLabel(u.t("devices.emptyHint"))))
		}
		for _, device := range devices {
			d := device
			name := widget.NewLabelWithStyle(d.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			name.Truncation = fyne.TextTruncateEllipsis
			state := u.t("devices.registered")
			if d.Revoked != nil {
				state = u.t("devices.revoked")
			}
			actions := container.NewHBox(widget.NewButton(u.t("devices.details"), func() { u.showDevice(d) }))
			if d.Revoked == nil {
				actions.Add(widget.NewButton(u.t("devices.revoke"), func() {
					confirmDialog(u.window, u.t("devices.revoke"), u.t("devices.revokeConfirm", map[string]any{"Name": d.Name}), u.t("devices.revoke"), u.t("action.cancel"), func() {
						go func() {
							err := host.RevokeDevice(u.manager.Dir, d.ID)
							fyne.Do(func() {
								if ctx.Err() != nil {
									return
								}
								if err != nil {
									u.showFailure("devices.changeError", err)
								} else {
									u.notice(u.t("devices.revoked"))
									refresh()
								}
							})
						}()
					})
				}))
			}
			cards.Add(widget.NewCard("", "", container.NewPadded(container.NewBorder(nil, nil, nil, actions, container.NewVBox(name, wrappedLabel(state))))))
		}
		cards.Refresh()
	}
	add := widget.NewButtonWithIcon(u.t("devices.add"), theme.ContentAddIcon(), func() {
		name := widget.NewEntry()
		name.SetPlaceHolder(u.t("devices.namePlaceholder"))
		name.Validator = func(value string) error {
			if strings.TrimSpace(value) == "" || len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
				return errors.New(u.t("devices.nameRequired"))
			}
			return nil
		}
		d := dialog.NewForm(u.t("devices.add"), u.t("devices.register"), u.t("action.cancel"), []*widget.FormItem{widget.NewFormItem(u.t("devices.name"), name)}, func(ok bool) {
			if !ok {
				return
			}
			value := name.Text
			go func() {
				device, err := host.CreateDevice(u.manager.Dir, value)
				fyne.Do(func() {
					if ctx.Err() != nil {
						return
					}
					if err != nil {
						u.showFailure("devices.changeError", err)
						return
					}
					refresh()
					u.showDevice(device)
				})
			}()
		}, u.window)
		d.SetDismissText(u.t("action.cancel"))
		d.Show()
	})
	add.Importance = widget.HighImportance
	refresh()
	header := container.NewVBox(pageHeading(u.t("nav.devices"), u.t("devices.intro")), container.NewHBox(add, widget.NewButtonWithIcon(u.t("action.refresh"), theme.ViewRefreshIcon(), refresh)))
	return container.NewPadded(container.NewBorder(header, nil, nil, nil, container.NewVScroll(cards)))
}

func (u *managerWindow) showDevice(d host.Device) {
	state := u.t("devices.registered")
	if d.Revoked != nil {
		state = u.t("devices.revoked")
	}
	content := container.NewVBox(wrappedLabel(state), wrappedLabel(u.t("devices.permissions")), wrappedLabel(u.t("devices.telemetry")))
	if d.Revoked == nil {
		content.Add(wrappedLabel(u.t("devices.setupHint")))
		if runtime.GOOS != "windows" {
			command, err := host.DeviceSSHCommand(u.manager.Binary, d.ID)
			if err == nil {
				escaped := strings.ReplaceAll(strings.ReplaceAll(command, "\\", "\\\\"), "\"", "\\\"")
				line := "restrict,command=\"" + escaped + "\" ssh-ed25519 <PUBLIC-KEY>"
				content.Add(widget.NewAccordion(widget.NewAccordionItem(u.t("devices.sshSetup"), container.NewVBox(readOnlyText(line), widget.NewButtonWithIcon(u.t("devices.copySetup"), theme.ContentCopyIcon(), func() { u.window.Clipboard().SetContent(line) }), wrappedLabel(u.t("devices.keyWarning"))))))
			}
		} else {
			content.Add(wrappedLabel(u.t("devices.windowsSetup")))
		}
	}
	content.Add(widget.NewAccordion(widget.NewAccordionItem(u.t("details.technical"), readOnlyText(d.ID))))
	dialog := dialog.NewCustom(d.Name, u.t("action.close"), container.NewVScroll(content), u.window)
	dialog.Resize(fyne.NewSize(660, 480))
	dialog.Show()
}
