// SPDX-License-Identifier: Apache-2.0
package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func bundle(t *testing.T, id string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	binary := []byte("test plugin")
	sum := sha256.Sum256(binary)
	m := sdk.Manifest{Protocol: sdk.Protocol, ID: id, Version: "1.0.0", Publisher: "test", Source: "https://example.org/plugin", Entrypoint: "plugin", SHA256: hex.EncodeToString(sum[:]), Platforms: []string{runtime.GOOS + "/" + runtime.GOARCH}, Permissions: []string{"process.same-user"}, Capabilities: []sdk.Capability{{Name: "echo", Kind: "tool", Version: "1.0.0", Title: "Echo", Input: json.RawMessage(`{"type":"object","properties":{"wait":{"type":"boolean"}},"additionalProperties":false}`), Output: json.RawMessage(`{"type":"object"}`)}}}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin"), binary, 0700); err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(b)
	return dir, hex.EncodeToString(sum[:])
}
func fakeLauncher(count *atomic.Int32) Launcher {
	return func(ctx context.Context, path, dir string) (*Connection, error) {
		count.Add(1)
		input, hostWrite := io.Pipe()
		hostRead, output := io.Pipe()
		ctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = sdk.Serve(ctx, input, output, func(ctx context.Context, name string, raw json.RawMessage, progress func(float64)) (any, error) {
				if string(raw) == `{"wait":true}` {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				progress(0.5)
				return map[string]bool{"ok": true}, nil
			})
			output.Close()
		}()
		var once sync.Once
		return &Connection{Input: hostWrite, Output: hostRead, Stop: func() {
			once.Do(func() {
				cancel()
				input.Close()
				output.Close()
				hostRead.Close()
				hostWrite.Close()
				<-done
			})
		}}, nil
	}
}
func waitReady(t *testing.T, r *Runtime, id string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range r.List() {
			if p.ID == id && p.State == "ready" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("not ready: %+v", r.List())
}
func waitResult(t *testing.T, r *Runtime, id string) Invocation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := r.GetInvocation(id)
		if err != nil {
			t.Fatal(err)
		}
		if run.State != "running" {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("invocation did not finish")
	return Invocation{}
}
func TestRuntimeLifecycleConcurrencyRecovery(t *testing.T) {
	var launches atomic.Int32
	r, err := New(filepath.Join(t.TempDir(), "plugins"), "epoch", fakeLauncher(&launches))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, id := range []string{"first", "second"} {
		source, sum := bundle(t, id)
		if err = r.Install(source, sum, false); err != nil {
			t.Fatal(err)
		}
		if err = r.Enable(id, []string{"process.same-user"}, false); err == nil {
			t.Fatal("enabled without confirmation")
		}
		if err = r.Enable(id, []string{"process.same-user"}, true); err != nil {
			t.Fatal(err)
		}
		waitReady(t, r, id)
	}
	if len(r.Capabilities()) != 2 {
		t.Fatal("registry missing entries")
	}
	// Returned metadata must not allow callers to mutate registry schemas.
	list := r.Capabilities()
	list[0].Input[0] = '!'
	if r.Capabilities()[0].Input[0] != '{' {
		t.Fatal("registry mutation")
	}
	if _, err = r.Invoke("first/echo", json.RawMessage(`{"unexpected":1}`), true); err == nil {
		t.Fatal("schema not enforced")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := r.Invoke("second/echo", json.RawMessage(`{}`), true)
			if err != nil {
				t.Error(err)
				return
			}
			if got := waitResult(t, r, run.ID); got.State != "succeeded" {
				t.Error(got)
			}
		}()
	}
	wg.Wait()
	run, err := r.Invoke("first/echo", json.RawMessage(`{"wait":true}`), true)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if err = r.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitResult(t, r, run.ID); got.State != "canceled" {
		t.Fatal(got)
	}
	// Another plugin keeps serving while the canceled worker recovers.
	next, err := r.Invoke("second/echo", json.RawMessage(`{}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if waitResult(t, r, next.ID).State != "succeeded" {
		t.Fatal("other plugin affected")
	}
	waitReady(t, r, "first")
	if launches.Load() < 3 {
		t.Fatal("plugin was not recovered")
	}
	page := r.Events("epoch", 0)
	if page.Resync || len(page.Events) == 0 {
		t.Fatal(page)
	}
	if !r.Events("old-epoch", 0).Resync {
		t.Fatal("restart resync missing")
	}
	if err = r.Disable("first"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Invoke("first/echo", json.RawMessage(`{}`), true); err == nil {
		t.Fatal("revocation bypass")
	}
	if err = r.Uninstall("first"); err != nil {
		t.Fatal(err)
	}
	if len(r.Capabilities()) != 1 {
		t.Fatal("not unregistered")
	}
}
func TestIntegrityUpdateAndRestart(t *testing.T) {
	var count atomic.Int32
	dir := filepath.Join(t.TempDir(), "plugins")
	r, err := New(dir, "one", fakeLauncher(&count))
	if err != nil {
		t.Fatal(err)
	}
	source, sum := bundle(t, "example")
	if err = r.Install(source, "wrong", false); err == nil {
		t.Fatal("manifest digest ignored")
	}
	if err = r.Install(source, sum, false); err != nil {
		t.Fatal(err)
	}
	if err = r.Enable("example", []string{"process.same-user", "network.public"}, true); err == nil {
		t.Fatal("extra permissions accepted")
	}
	if err = r.Enable("example", []string{"process.same-user"}, true); err != nil {
		t.Fatal(err)
	}
	waitReady(t, r, "example")
	if err = r.Install(source, sum, true); err == nil {
		t.Fatal("enabled update allowed")
	}
	r.Close()
	r, err = New(dir, "two", fakeLauncher(&count))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitReady(t, r, "example")
	if err = r.Disable("example"); err != nil {
		t.Fatal(err)
	}
	if err = r.Install(source, sum, true); err != nil {
		t.Fatal(err)
	}
	if r.List()[0].State != "disabled" || len(r.List()[0].Grants) != 0 {
		t.Fatal("update retained grants")
	}
	if err = os.WriteFile(filepath.Join(dir, "example", "plugin"), []byte("modified"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = r.Enable("example", []string{"process.same-user"}, true); err == nil {
		t.Fatal("integrity bypass")
	}
}
func TestCrashBudgetAndInvalidPackageIsolation(t *testing.T) {
	var count atomic.Int32
	dir := filepath.Join(t.TempDir(), "plugins")
	r, err := New(dir, "epoch", func(context.Context, string, string) (*Connection, error) {
		count.Add(1)
		return nil, errors.New("launch failure")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	source, sum := bundle(t, "broken")
	if err = r.Install(source, sum, false); err != nil {
		t.Fatal(err)
	}
	if err = r.Enable("broken", []string{"process.same-user"}, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && r.List()[0].State != "failed" {
		time.Sleep(10 * time.Millisecond)
	}
	if count.Load() != 3 || r.List()[0].State != "failed" {
		t.Fatal("restart storm or missing failure", count.Load(), r.List())
	}
	r.Close()
	if err = os.WriteFile(filepath.Join(dir, "broken", "installed.json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	clean, err := New(dir, "next", fakeLauncher(&count))
	if err != nil {
		t.Fatal("broken plugin prevented Host startup", err)
	}
	defer clean.Close()
	if len(clean.List()) != 0 {
		t.Fatal("bad package loaded")
	}
}
func TestSchemaExternalReferenceAndEventWindow(t *testing.T) {
	if _, err := compile(json.RawMessage(`{"$ref":"https://example.org/schema"}`)); err == nil {
		t.Fatal("external schema allowed")
	}
	r, _ := New(filepath.Join(t.TempDir(), "missing"), "epoch", nil)
	defer r.Close()
	r.mu.Lock()
	for i := 0; i < 300; i++ {
		r.emitLocked(Event{Type: "test", ID: "x"})
	}
	r.mu.Unlock()
	if !r.Events("epoch", 0).Resync {
		t.Fatal("lost cursor not detected")
	}
	if r.Events("epoch", 299).Resync {
		t.Fatal("valid cursor rejected")
	}
}

func TestInvocationReceiptsAndRetirement(t *testing.T) {
	var count atomic.Int32
	r, err := New(filepath.Join(t.TempDir(), "plugins"), "epoch", fakeLauncher(&count))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	source, sum := bundle(t, "receipts")
	if err = r.Install(source, sum, false); err != nil {
		t.Fatal(err)
	}
	if err = r.Enable("receipts", []string{"process.same-user"}, true); err != nil {
		t.Fatal(err)
	}
	waitReady(t, r, "receipts")
	first, err := r.Invoke("receipts/echo", json.RawMessage(`{}`), true, "key")
	if err != nil {
		t.Fatal(err)
	}
	waitResult(t, r, first.ID)
	retry, err := r.Invoke("receipts/echo", json.RawMessage(`{}`), true, "key")
	if err != nil || retry.ID != first.ID {
		t.Fatal("retry executed again", retry, err)
	}
	if _, err = r.Invoke("receipts/echo", json.RawMessage(`{"wait":false}`), true, "key"); err == nil {
		t.Fatal("changed retry accepted")
	}
	for i := 0; i < 32; i++ {
		run, err := r.Invoke("receipts/echo", json.RawMessage(`{}`), true)
		if err != nil {
			t.Fatal(err)
		}
		waitResult(t, r, run.ID)
	}
	if _, err = r.Invoke("receipts/echo", json.RawMessage(`{}`), true, "key"); err == nil {
		t.Fatal("retired invocation replayed")
	}
}
func TestPublicManifestCompatibility(t *testing.T) {
	// This is a real client-facing compatibility check, not a copy of runtime validation.
	raw, err := os.ReadFile("../../docs/plugin-system/spec/v1/manifest.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"http-inspector", "plugin-template"} {
		raw, err = os.ReadFile("../../examples/" + name + "/manifest.template.json")
		if err != nil {
			t.Fatal(err)
		}
		var manifest any
		if err = json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		if err = schema.Validate(manifest); err != nil {
			t.Fatalf("%s public manifest mismatch: %v", name, err)
		}
	}
	for _, name := range []string{"request.schema.json", "response.schema.json", "plugin-wire.schema.json"} {
		raw, err = os.ReadFile("../../docs/plugin-system/spec/v1/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = compile(raw); err != nil {
			t.Fatal(name, err)
		}
	}
}
