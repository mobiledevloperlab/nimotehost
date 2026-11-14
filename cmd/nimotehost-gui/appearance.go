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

const (
	surfaceColor       fyne.ThemeColorName = "nimoteSurface"
	sidebarColor       fyne.ThemeColorName = "nimoteSidebar"
	accentSurfaceColor fyne.ThemeColorName = "nimoteAccentSurface"
)

const pagePadding float32 = 24

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
		return pair(0xF6F7F9, 0x14161B)
	case surfaceColor, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return pair(0xFFFFFF, 0x1E2128)
	case sidebarColor:
		return pair(0xECEEF3, 0x191C22)
	case accentSurfaceColor:
		return pair(0xEEF2FF, 0x232B42)
	case theme.ColorNameForeground:
		return pair(0x222B3B, 0xE8ECF3)
	case theme.ColorNamePrimary:
		return pair(0x4858D8, 0x8C9CFF)
	case theme.ColorNameForegroundOnPrimary:
		return pair(0xFFFFFF, 0x172037)
	case theme.ColorNameSelection:
		return pair(0xE5E9FF, 0x303A5B)
	case theme.ColorNameInputBackground, theme.ColorNameButton:
		return pair(0xF0F2F6, 0x2A303B)
	case theme.ColorNameDisabledButton:
		return pair(0xF0F2F6, 0x252A33)
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return pair(0xDFE4EC, 0x353E4B)
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return pair(0x606B7E, 0x9AA6B9)
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
		return 8
	case theme.SizeNameInnerPadding:
		return 6
	case theme.SizeNameHeadingText:
		return 28
	case theme.SizeNameSubHeadingText:
		return 18
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius, theme.SizeNameButtonRadius:
		return 8
	case theme.SizeNameCardRadius, theme.SizeNameDialogRadius, theme.SizeNameMenuRadius, theme.SizeNamePopupRadius:
		return 12
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
	return tintedSurface(object, surfaceColor, 18)
}

// Keep surface decoration separate from content, without Card's extra title
// spacing. The renderer resolves colours again when the app theme changes.
type panelSurface struct {
	widget.BaseWidget
	content fyne.CanvasObject
	colour  fyne.ThemeColorName
	padding float32
}

func tintedSurface(content fyne.CanvasObject, colour fyne.ThemeColorName, padding float32) fyne.CanvasObject {
	p := &panelSurface{content: content, colour: colour, padding: padding}
	p.ExtendBaseWidget(p)
	return container.NewThemeOverride(p, surfaceTheme{})
}

func (p *panelSurface) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(theme.Color(p.colour))
	bg.CornerRadius = 14
	bg.StrokeWidth = 1
	bg.StrokeColor = theme.Color(theme.ColorNameSeparator)
	body := container.NewStack(bg, inset(p.content, p.padding))
	return &panelRenderer{WidgetRenderer: widget.NewSimpleRenderer(body), background: bg, panel: p}
}

type panelRenderer struct {
	fyne.WidgetRenderer
	background *canvas.Rectangle
	panel      *panelSurface
}

func (r *panelRenderer) Refresh() {
	r.background.FillColor = theme.Color(r.panel.colour)
	r.background.StrokeColor = theme.Color(theme.ColorNameSeparator)
	r.WidgetRenderer.Refresh()
}

// Explicit rhythm for page sections; hidden sections don't leave empty gaps.
func column(gap float32, objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(columnLayout{gap: gap}, objects...)
}

type columnLayout struct{ gap float32 }

func (l columnLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	size := fyne.NewSize(0, 0)
	count := 0
	for _, obj := range objects {
		if !obj.Visible() {
			continue
		}
		min := obj.MinSize()
		size.Width = max(size.Width, min.Width)
		size.Height += min.Height
		count++
	}
	if count > 1 {
		size.Height += float32(count-1) * l.gap
	}
	return size
}

func (l columnLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var y float32
	for _, obj := range objects {
		if !obj.Visible() {
			continue
		}
		height := obj.MinSize().Height
		obj.Move(fyne.NewPos(0, y))
		obj.Resize(fyne.NewSize(size.Width, height))
		y += height + l.gap
	}
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
