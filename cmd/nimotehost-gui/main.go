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
	binary := filepath.Join(filepath.Dir(executable), name)
	if runtime.GOOS != "windows" {
		if home, err := os.UserHomeDir(); err == nil {
			installed := filepath.Join(home, ".local", "share", "nimotehost", name)
			if info, err := os.Stat(installed); err == nil && info.Mode().IsRegular() {
				binary = installed
			}
		}
	}
	m, err := desktop.New(binary)
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
