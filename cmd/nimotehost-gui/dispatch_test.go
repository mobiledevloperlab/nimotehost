// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"sync"
	"testing"
	"time"
)

// Fyne's test driver executes Do callbacks immediately on the worker goroutine.
// Queue them instead, matching the production UI event loop. Tests explicitly
// dispatch completions on their own goroutine before inspecting widgets.
type queuedTestDriver struct {
	fyne.Driver
	events chan func()
	done   chan struct{}
}

func (d *queuedTestDriver) DoFromGoroutine(f func(), wait bool) {
	completed := make(chan struct{})
	select {
	case d.events <- func() { f(); close(completed) }:
	case <-d.done:
		return
	}
	if wait {
		select {
		case <-completed:
		case <-d.done:
		}
	}
}

type queuedTestApp struct {
	fyne.App
	driver *queuedTestDriver
	once   sync.Once
}

func newUITestApp() *queuedTestApp {
	base := test.NewApp()
	a := &queuedTestApp{App: base, driver: &queuedTestDriver{Driver: base.Driver(), events: make(chan func(), 128), done: make(chan struct{})}}
	fyne.SetCurrentApp(a)
	return a
}
func (a *queuedTestApp) Driver() fyne.Driver { return a.driver }
func (a *queuedTestApp) Quit()               { a.once.Do(func() { close(a.driver.done); a.App.Quit() }) }
func (a *queuedTestApp) dispatchNext(t *testing.T) {
	t.Helper()
	select {
	case f := <-a.driver.events:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for UI completion")
	}
}
