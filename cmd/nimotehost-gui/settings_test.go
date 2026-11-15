// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mobiledevloperlab/nimotehost/internal/desktop"
)

func TestSettingsValidationDraftAndLifecycle(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	w := a.NewWindow("Settings")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: filepath.Join(t.TempDir(), "host")}}
	u.build("en", 3)
	defer func() { u.cancel() }()
	p := u.settings
	if !p.port.Disabled() || !p.start.Disabled() {
		t.Fatal("service controls enabled before status is known")
	}
	u.snapshot = desktop.Snapshot{State: "Stopped"}
	p.update(u.snapshot, nil, false)
	if p.port.Disabled() || p.start.Disabled() || !p.save.Disabled() || !p.reset.Disabled() {
		t.Fatal("incorrect clean stopped state")
	}
	for _, value := range []string{"", "abc", "1023", "65536"} {
		p.port.SetText(value)
		if p.port.Validate() == nil || !p.save.Disabled() || p.reset.Disabled() {
			t.Fatalf("invalid port %q was accepted or cannot be reverted", value)
		}
	}
	for _, value := range []string{"1024", "65535", " 48000 "} {
		p.port.SetText(value)
		if p.port.Validate() != nil || p.save.Disabled() {
			t.Fatalf("valid port %q was rejected", value)
		}
	}
	p.level.SetSelected("debug")
	p.update(u.snapshot, nil, true)
	if !p.port.Disabled() || !p.save.Disabled() || !p.start.Disabled() || !p.language.Disabled() {
		t.Fatal("busy controls remain enabled")
	}
	p.update(u.snapshot, nil, false)
	if p.save.Disabled() || p.status.Text != u.t("settings.unsaved") {
		t.Fatal("refresh discarded draft state")
	}
	u.changeLanguage("zh-Hans")
	p = u.settings
	if p.port.Text != " 48000 " || p.level.Selected != "debug" || p.save.Disabled() {
		t.Fatal("language switch discarded the configuration draft")
	}
	p.reset.OnTapped()
	if p.port.Text != "47831" || p.level.Selected != "info" || !p.save.Disabled() || !p.reset.Disabled() {
		t.Fatal("discard did not restore persisted values")
	}
	p.update(desktop.Snapshot{State: "Running"}, nil, false)
	if !p.port.Disabled() || !p.start.Disabled() || p.stop.Disabled() || p.restart.Disabled() {
		t.Fatal("incorrect running controls")
	}
	p.update(u.snapshot, errors.New("status unavailable"), false)
	if !p.port.Disabled() || !p.startup.Disabled() || !p.start.Disabled() || !p.stop.Disabled() {
		t.Fatal("unknown status permits a service change")
	}
}

func TestSettingsKeepConfigurationReadFailureVisible(t *testing.T) {
	a := newUITestApp()
	defer a.Quit()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	w := a.NewWindow("Settings")
	defer w.Close()
	u := &managerWindow{app: a, window: w, manager: &desktop.Manager{Dir: dir}}
	u.build("en", 3)
	defer u.cancel()
	p := u.settings
	p.update(desktop.Snapshot{State: "Stopped"}, nil, false)
	if p.advancedHint.Text != u.t("settings.configError") || !p.save.Disabled() || !p.port.Disabled() {
		t.Fatal("refresh concealed configuration read failure")
	}
}
