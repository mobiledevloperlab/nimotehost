// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

func settingField(title, hint string, control fyne.CanvasObject) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	fields := column(4, label, control)
	if hint != "" {
		fields.Add(mutedLabel(hint))
	}
	return fields
}

func (u *managerWindow) showAbout() {
	intro := column(12,
		container.NewBorder(nil, nil, brandImage(64), nil, column(0, sectionTitle("NimoteHost"), mutedLabel(u.t("about.version", map[string]any{"Version": host.Version})))),
		wrappedLabel(u.t("about.summary")),
		sectionTitle(u.t("about.companionTitle")),
		wrappedLabel(u.t("about.companion")),
		wrappedLabel(u.t("connection.steps")),
		mutedLabel(u.t("connection.sshNote")))
	privacy := column(12,
		sectionTitle(u.t("about.privacyTitle")),
		wrappedLabel(u.t("about.localData")),
		wrappedLabel(u.t("about.access")),
		wrappedLabel(u.t("about.agents")),
		mutedLabel(u.t("about.network")))
	appearance := column(12,
		sectionTitle(u.t("settings.appearance")),
		wrappedLabel(u.t("about.appearance")),
		sectionTitle(u.t("about.shortcutsTitle")),
		wrappedLabel(u.t("about.shortcuts")))
	tabs := container.NewAppTabs(
		container.NewTabItem(u.t("nav.overview"), container.NewVScroll(inset(intro, 16))),
		container.NewTabItem(u.t("about.privacyTitle"), container.NewVScroll(inset(privacy, 16))),
		container.NewTabItem(u.t("settings.appearance"), container.NewVScroll(inset(appearance, 16))))
	help := widget.NewButtonWithIcon(u.t("help.title"), theme.HelpIcon(), u.showHelp)
	content := container.NewBorder(nil, container.NewHBox(help, mutedLabel("© 2026 Nimote contributors · Apache-2.0")), nil, nil, tabs)
	d := dialog.NewCustom(u.t("about.open"), u.t("action.close"), content, u.window)
	d.Resize(u.readerSize())
	d.Show()
}
