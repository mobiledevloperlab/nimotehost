// SPDX-License-Identifier: Apache-2.0
package main

import (
	"image/color"
	"math"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

func TestPaletteTextContrastAndLiveAppearance(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	for _, mode := range []string{"Light", "Dark"} {
		applyAppearance(a, mode)
		palette := a.Settings().Theme()
		for _, background := range []string{string(theme.ColorNameBackground), string(surfaceColor), string(sidebarColor), string(accentSurfaceColor), string(theme.ColorNameInputBackground)} {
			for _, foreground := range []string{string(theme.ColorNameForeground), string(theme.ColorNameDisabled)} {
				contrast := contrastRatio(palette.Color(fyne.ThemeColorName(foreground), theme.VariantLight), palette.Color(fyne.ThemeColorName(background), theme.VariantLight))
				if contrast < 4.5 {
					t.Errorf("%s %s/%s contrast %.2f", mode, foreground, background, contrast)
				}
			}
		}
		if contrast := contrastRatio(palette.Color(theme.ColorNameForegroundOnPrimary, theme.VariantLight), palette.Color(theme.ColorNamePrimary, theme.VariantLight)); contrast < 4.5 {
			t.Errorf("%s primary action contrast %.2f", mode, contrast)
		}
		if got := (surfaceTheme{}).Color(theme.ColorNameBackground, theme.VariantLight); got != palette.Color(surfaceColor, theme.VariantLight) {
			t.Fatal("surface did not follow the current app appearance")
		}
	}
}

func contrastRatio(a, b color.Color) float64 {
	luminance := func(c color.Color) float64 {
		r, g, b, _ := c.RGBA()
		linear := func(x uint32) float64 {
			v := float64(x) / 65535
			if v <= .04045 {
				return v / 12.92
			}
			return math.Pow((v+.055)/1.055, 2.4)
		}
		return .2126*linear(r) + .7152*linear(g) + .0722*linear(b)
	}
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + .05) / (y + .05)
}

func TestOverviewReviewOpensCurrentApprovalList(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	w := a.NewWindow("Review")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	u.build("en", 0)
	defer u.cancel()
	u.sessions.update([]host.Session{{ID: "pending", Kind: "agent", State: "running", AgentStatus: "needs_approval"}}, nil, nil, "Running")
	u.sessions.search.SetText("old query")
	u.sessions.tabs.SelectIndex(1)
	u.overview.update(desktop.Snapshot{State: "Running"}, u.sessions.source, nil, nil)
	u.overview.review.OnTapped()
	if u.currentPage != 1 || u.sessions.tabs.SelectedIndex() != 0 || u.sessions.search.Text != "" || len(u.sessions.rows) != 1 {
		t.Fatal("review action left the request hidden by previous navigation or filters")
	}
}
