// SPDX-License-Identifier: Apache-2.0
package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const surfaceColor fyne.ThemeColorName = "nimoteSurface"

// Keep rendering, focus, input and accessibility in Fyne. This theme only
// supplies the application's palette and typography through its public API.
type desktopTheme struct {
	fyne.Theme
	appearance int // system, light, dark
}

func (t desktopTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if t.appearance == 1 {
		variant = theme.VariantLight
	}
	if t.appearance == 2 {
		variant = theme.VariantDark
	}
	dark := variant == theme.VariantDark
	pair := func(light, darkColor uint32) color.Color {
		value := light
		if dark {
			value = darkColor
		}
		return color.NRGBA{R: uint8(value >> 16), G: uint8(value >> 8), B: uint8(value), A: 255}
	}
	switch name {
	case theme.ColorNameBackground:
		return pair(0xF5F6FA, 0x14171C)
	case surfaceColor, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return pair(0xFFFFFF, 0x1D222A)
	case theme.ColorNameForeground:
		return pair(0x222B3B, 0xE8ECF3)
	case theme.ColorNamePrimary:
		return pair(0x4858D8, 0x8C9CFF)
	case theme.ColorNameForegroundOnPrimary:
		return pair(0xFFFFFF, 0x172037)
	case theme.ColorNameSelection:
		return pair(0xE5E9FF, 0x303A5B)
	case theme.ColorNameInputBackground, theme.ColorNameButton:
		return pair(0xECEFF5, 0x2A313C)
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return pair(0xDFE4EC, 0x353E4B)
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return pair(0x626D81, 0x9AA6B9)
	case theme.ColorNameSuccess:
		return pair(0x217450, 0x83D8AD)
	case theme.ColorNameWarning:
		return pair(0x956000, 0xECC176)
	case theme.ColorNameShadow:
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}

func (t desktopTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameHeadingText:
		return 28
	case theme.SizeNameSubHeadingText:
		return 18
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	}
	return t.Theme.Size(name)
}

// A surface follows the currently selected app theme, including live changes.
// It doesn't retain a stale light/dark palette when preferences change.
type surfaceTheme struct{}

func (surfaceTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameBackground {
		name = surfaceColor
	}
	return fyne.CurrentApp().Settings().Theme().Color(name, v)
}
func (surfaceTheme) Font(style fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(style)
}
func (surfaceTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(name)
}
func (surfaceTheme) Size(name fyne.ThemeSizeName) float32 {
	return fyne.CurrentApp().Settings().Theme().Size(name)
}

func inset(object fyne.CanvasObject, padding float32) *fyne.Container {
	return container.New(layout.NewCustomPaddedLayout(padding, padding, padding, padding), object)
}
func surface(object fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(widget.NewCard("", "", inset(object, 12)), surfaceTheme{})
}
func sectionTitle(text string) *widget.RichText {
	return widget.NewRichText(&widget.TextSegment{Text: text, Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}, SizeName: theme.SizeNameSubHeadingText}})
}
func mutedLabel(text string) *widget.Label {
	label := wrappedLabel(text)
	label.Importance = widget.LowImportance
	return label
}

type titleTheme struct{ surfaceTheme }

func (titleTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return 22
	}
	return surfaceTheme{}.Size(name)
}

func applyAppearance(app fyne.App, name string) {
	index := 0
	if name == "Light" {
		index = 1
	}
	if name == "Dark" {
		index = 2
	}
	app.Settings().SetTheme(desktopTheme{Theme: theme.DefaultTheme(), appearance: index})
}

func desktopBounds(content fyne.CanvasObject) fyne.CanvasObject {
	minimum := canvas.NewRectangle(color.Transparent)
	minimum.SetMinSize(fyne.NewSize(960, 640))
	return container.NewStack(minimum, content)
}
