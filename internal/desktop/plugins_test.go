// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

func TestDesktopPluginCallUsesAuthenticatedPublicProtocol(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/extensions" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Nimote-Host-Protocol") != "1" {
			t.Error("incorrect authenticated transport")
		}
		var req PluginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.Protocol != "1.0" || req.Operation != "plugins.enable" || !req.Confirmed || len(req.Grants) != 1 {
			t.Error("request changed", req)
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`{"protocol":"1.0","result":{"ok":true}}`))
		} else {
			_, _ = w.Write([]byte(`{"protocol":"9.0","result":{}}`))
		}
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "host")
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	if err := host.SaveConfig(dir, host.Config{Port: port, LogLevel: "info"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Dir: dir}
	req := PluginRequest{Operation: "plugins.enable", ID: "example", Grants: []string{"process.same-user"}, Confirmed: true}
	if _, err := m.PluginCall(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PluginCall(context.Background(), req); err == nil {
		t.Fatal("unsupported response protocol accepted")
	}
	a, err := PluginRequestID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := PluginRequestID()
	if !sessionID(a) || a == b {
		t.Fatal("invalid or reused request ID")
	}
}
func TestReviewPluginBundlePinsManifestWithoutExecuting(t *testing.T) {
	dir := t.TempDir()
	m := sdk.Manifest{Protocol: "1.0", ID: "review", Version: "1.0.0", Publisher: "test", Source: "https://example.org", Entrypoint: "plugin", SHA256: strings.Repeat("a", 64), Platforms: []string{runtime.GOOS + "/" + runtime.GOARCH}, Permissions: []string{"process.same-user"}, Capabilities: []sdk.Capability{{Name: "echo", Kind: "tool", Version: "1.0.0", Title: "Echo", Input: json.RawMessage(`{"type":"object"}`), Output: json.RawMessage(`{"type":"object"}`)}}}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	reviewed, digest, err := ReviewPluginBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if reviewed.ID != m.ID || digest != hex.EncodeToString(sum[:]) {
		t.Fatal("review digest not pinned")
	}
	if _, _, err = ReviewPluginBundle("relative"); err == nil {
		t.Fatal("relative path accepted")
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ReviewPluginBundle(dir); err == nil {
		t.Fatal("invalid manifest accepted")
	}
}
