// SPDX-License-Identifier: Apache-2.0
package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

var navigationKeys = []string{"nav.overview", "nav.sessions", "nav.devices", "nav.settings", "nav.plugins"}

func (u *managerWindow) newSidebar() fyne.CanvasObject {
	icons := []fyne.Resource{theme.HomeIcon(), theme.ListIcon(), theme.ComputerIcon(), theme.SettingsIcon(), theme.GridIcon()}
	u.navigation = widget.NewList(func() int { return len(navigationKeys) }, func() fyne.CanvasObject {
		return inset(container.NewBorder(nil, nil, widget.NewIcon(theme.HomeIcon()), nil, widget.NewLabel("")), 5)
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		row := obj.(*fyne.Container).Objects[0].(*fyne.Container)
		row.Objects[0].(*widget.Label).SetText(u.t(navigationKeys[id]))
		row.Objects[1].(*widget.Icon).SetResource(icons[id])
		row.Refresh()
	})
	u.navigation.HideSeparators = true
	u.navigation.OnSelected = func(id widget.ListItemID) { u.navigate(id) }
	brand := container.NewBorder(nil, nil, brandImage(36), nil, column(0, sectionTitle("NimoteHost"), mutedLabel("DESKTOP")))
	u.sidebarName = mutedLabel(u.t("connection.computer"))
	u.sidebarName.Wrapping = fyne.TextWrapOff
	u.sidebarName.Truncation = fyne.TextTruncateEllipsis
	u.sidebarStatus = mutedLabel(u.t("state.unknown"))
	help := widget.NewButtonWithIcon(u.t("nav.help"), theme.HelpIcon(), u.showHelp)
	help.Importance = widget.LowImportance
	help.Alignment = widget.ButtonAlignLeading
	footer := container.NewVBox(widget.NewSeparator(), u.sidebarName, u.sidebarStatus, help, mutedLabel("v"+host.Version))
	body := inset(container.NewBorder(inset(brand, 8), footer, nil, nil, u.navigation), 14)
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(188, 0))
	return tintedSurface(container.NewStack(width, body), sidebarColor, 0)
}

func (u *managerWindow) navigate(index int) {
	if index < 0 || index >= len(u.pages) || u.currentPage == index {
		return
	}
	u.currentPage = index
	for i, page := range u.pages {
		if i == index {
			page.Show()
		} else {
			page.Hide()
		}
	}
	// Container.Refresh walks every child, including hidden pages. Only attach
	// the active page so navigation and resizing cannot relayout inactive trees.
	u.content.Objects = []fyne.CanvasObject{u.pages[index]}
	u.content.Refresh()
	u.navigation.Select(index)
	if index == 1 && u.sessions.tabs.SelectedIndex() == 2 {
		u.sessions.inbox.refresh()
	}
	if index == 4 {
		u.plugins.refresh()
	}
}

func (u *managerWindow) updateSidebar(snapshot desktop.Snapshot, err error) {
	u.sidebarName.SetText(snapshot.Name)
	key := "state.unknown"
	if err == nil && snapshot.State == "Running" {
		key = "state.running"
	}
	if err == nil && snapshot.State == "Stopped" {
		key = "state.stopped"
	}
	importance := widget.LowImportance
	if key == "state.running" {
		importance = widget.SuccessImportance
	}
	if u.sidebarStatus.Importance != importance {
		u.sidebarStatus.Importance = importance
		u.sidebarStatus.Refresh()
	}
	u.sidebarStatus.SetText(u.t(key))
}
