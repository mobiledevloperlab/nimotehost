// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
)

func main() {
	a := app.NewWithID("com.nimote.host.desktop")
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	name := "nimote-host"
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
