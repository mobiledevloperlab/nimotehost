// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeviceRevocationAndOwnerIsolation(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	a, err := CreateDevice(dir, "Phone A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateDevice(dir, "Phone B")
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := deviceCredential(DeviceContext(context.Background(), a.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := deviceCredential(DeviceContext(context.Background(), b.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{dir: dir, token: strings.Repeat("a", 64)}
	call := func(token, method, path string) int {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Nimote-Host-Protocol", "1")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w.Code
	}
	if code := call(secret, "GET", "/v1/status"); code != 200 {
		t.Fatalf("authorized status: %d", code)
	}
	for _, path := range []string{"/v1/shutdown", "/v1/sessions", "/v1/sessions/" + strings.Repeat("b", 64) + "/input", "/v1/sessions/" + strings.Repeat("b", 64) + "/agent-message"} {
		if code := call(secret, "POST", path); code != 403 {
			t.Fatalf("observer mutation accepted %s %d", path, code)
		}
	}
	for _, path := range []string{"/v1/agent-history", "/v1/not-real", "/v1/sessions/../shutdown"} {
		if code := call(secret, "GET", path); code != 403 {
			t.Fatalf("observer escape %s %d", path, code)
		}
	}
	if err = RevokeDevice(dir, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = RevokeDevice(dir, a.ID); err != nil {
		t.Fatal(err)
	}
	if code := call(secret, "GET", "/v1/status"); code != 401 {
		t.Fatalf("revoked credential still accepted %d", code)
	}
	if code := call(other, "GET", "/v1/status"); code != 200 {
		t.Fatal("revoked other device")
	}
	if code := call(server.token, "GET", "/v1/status"); code != 200 {
		t.Fatal("revoked owner")
	}
	if _, _, err = deviceCredential(DeviceContext(context.Background(), a.ID), dir); err == nil {
		t.Fatal("bridge retained revoked credential")
	}
	records, err := ListDevices(dir)
	if err != nil || len(records) != 2 {
		t.Fatalf("list %v %v", records, err)
	}
	raw, _ := json.Marshal(records)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "credential_hash") {
		t.Fatal("credential leaked in device list")
	}
	// Restart/reload cannot reactivate a revoked identity.
	if authenticateDevice(dir, secret) {
		t.Fatal("revocation not persisted")
	}
	if err = os.WriteFile(filepath.Join(dir, "devices", b.ID+".json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if authenticateDevice(dir, other) {
		t.Fatal("corrupt record failed open")
	}
}
func TestDeviceInputsAndRestrictions(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	for _, name := range []string{"", "\n", "a\nb", strings.Repeat("a", 129)} {
		if _, err := CreateDevice(dir, name); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	if RevokeDevice(dir, "../../token") == nil {
		t.Fatal("traversal accepted")
	}
	for _, id := range []string{"../token", "", strings.Repeat("z", 64)} {
		if _, err := readDevice(dir, id); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
}

func TestLiveRPCStreamChecksRevocationPerRequest(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	d, err := CreateDevice(dir, "stream client")
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := deviceCredential(DeviceContext(context.Background(), d.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	api := &Server{dir: dir, token: strings.Repeat("f", 64)}
	client := func(ctx context.Context, req RPCRequest) ([]byte, error) {
		r := httptest.NewRequest(req.Method, req.Path, nil)
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Nimote-Host-Protocol", "1")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 200 {
			return nil, fmt.Errorf("status %d", w.Code)
		}
		return w.Body.Bytes(), nil
	}
	in, writer := io.Pipe()
	out, reader := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- serveRPCStream(context.Background(), in, reader, client); reader.Close() }()
	decoder := json.NewDecoder(out)
	var frame map[string]json.RawMessage
	if err = decoder.Decode(&frame); err != nil {
		t.Fatal(err)
	}
	send := func(id int) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"id": id, "method": "GET", "path": "/v1/status"})
		if _, err = writer.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
		if err = decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
	}
	send(1)
	if _, failed := frame["error"]; failed {
		t.Fatal("authorized request failed")
	}
	if err = RevokeDevice(dir, d.ID); err != nil {
		t.Fatal(err)
	}
	send(2)
	if _, failed := frame["error"]; !failed {
		t.Fatal("existing stream bypassed revocation")
	}
	writer.Close()
	out.Close()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func FuzzObserverBoundary(f *testing.F) {
	for _, seed := range []struct{ method, path string }{{"GET", "/v1/status"}, {"POST", "/v1/shutdown"}, {"GET", "/v1/sessions/" + strings.Repeat("a", 64) + "/output"}, {"GET", "/v1/sessions/../agent-history"}} {
		f.Add(seed.method, seed.path)
	}
	f.Fuzz(func(t *testing.T, method, path string) {
		if observerAllowed(method, path) {
			if method != "GET" {
				t.Fatal("write method allowed")
			}
			if strings.Contains(path, "..") || strings.ContainsAny(path, "?#\\\r\n") {
				t.Fatal("noncanonical path allowed")
			}
		}
	})
}
