// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

func TestDeployJSONStatusDoesNotCreateState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("USERPROFILE", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "localappdata"))
	state, err := host.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		action string
		exit   int
		code   string
	}{
		{"status", 0, ""}, {"health", 5, "host_stopped"}, {"invalid", 2, "invalid_arguments"},
	} {
		var out bytes.Buffer
		err := deploy(context.Background(), []string{test.action}, &out)
		var result deployResult
		if decodeErr := json.Unmarshal(out.Bytes(), &result); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if result.Schema != 1 || result.Action != test.action || result.OS != runtime.GOOS || result.OK != (test.exit == 0) {
			t.Fatalf("%+v", result)
		}
		if test.exit != 0 && (deploymentExit(err) != test.exit || result.Error == nil || result.Error.Code != test.code) {
			t.Fatalf("%v, %+v", err, result.Error)
		}
		if test.exit == 0 && (err != nil || result.Running == nil || *result.Running || result.Installed == nil || *result.Installed) {
			t.Fatalf("%v, %s", err, out.Bytes())
		}
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status created user state: %v", err)
	}
}

func TestDeployHealthAuthenticatesAgainstConfiguredPort(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" || r.Header.Get("Authorization") != "Bearer fixture" ||
			r.Header.Get("Nimote-Host-Protocol") != fmt.Sprint(host.Protocol) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"running":true,"version":"2.5.0"}`)
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	if err := host.SaveConfig(dir, host.Config{Port: port, LogLevel: "info"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := runningDeployStatus(context.Background(), dir)
	if err != nil || status == nil || status.Version != "2.5.0" {
		t.Fatalf("%+v, %v", status, err)
	}
}

func TestNewerStableRejectsDowngradeNumerically(t *testing.T) {
	for _, test := range []struct {
		current, next string
		want          bool
	}{
		{"2.12.0", "2.9.1", true}, {"2.10.10", "2.10.11", false},
		{"3.0.0", "2.99.99", true}, {"2.4.0", "2.4.0", false},
		{"2.4.0-dev", "2.4.0", false},
	} {
		if got := newerStable(test.current, test.next); got != test.want {
			t.Fatalf("%s > %s = %v", test.current, test.next, got)
		}
	}
}
