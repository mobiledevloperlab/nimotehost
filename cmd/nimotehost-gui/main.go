// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
)

func main() {
	if err := migrateDesktopPreferences(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	a := app.NewWithID("com.nimotehost.desktop")
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	name := "nimotehost"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	m, err := desktop.New(filepath.Join(filepath.Dir(executable), name))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	runUI(a, m)
}

func runUI(a fyne.App, m *desktop.Manager) {
	w, cleanup := newManagerWindow(a, m, true)
	defer cleanup()
	w.ShowAndRun()
}
