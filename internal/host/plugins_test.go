// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mobiledevloperlab/nimotehost/internal/plugins"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

func TestCommunityPluginRealProcessAndRPC(t *testing.T) {
	source := t.TempDir()
	entry := "plugin"
	if runtime.GOOS == "windows" {
		entry += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(source, entry), "./testdata/community-plugin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build external plugin: %v %s", err, out)
	}
	binary, err := os.ReadFile(filepath.Join(source, entry))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	manifest := sdk.Manifest{Protocol: "1.0", ID: "independent-echo", Version: "1.0.0", Publisher: "test", Source: "https://example.org/echo", Entrypoint: entry, SHA256: hex.EncodeToString(sum[:]), Platforms: []string{runtime.GOOS + "/" + runtime.GOARCH}, Permissions: []string{"process.same-user"}, Capabilities: []sdk.Capability{{Name: "echo", Kind: "tool", Version: "1.0.0", Title: "Independent echo", Input: json.RawMessage(`{"type":"object"}`), Output: json.RawMessage(`{"type":"object"}`)}}}
	manifest.Capabilities = append(manifest.Capabilities, sdk.Capability{Name: "agent", Kind: "agent", Version: "1.0.0", Title: "Community ACP", ACP: 1, Input: json.RawMessage(`{"type":"object"}`), Output: json.RawMessage(`{"type":"object"}`)})
	b, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(source, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(b)
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	r, err := plugins.New(filepath.Join(dir, "plugins"), "integration", pluginLauncher(DefaultResourceLimits()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	manager := newTestManager(t)
	s := &Server{dir: dir, token: "owner", plugins: r, manager: manager}
	call := func(auth string, req capabilityRequest) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(req)
		request := httptest.NewRequest(http.MethodPost, "/v1/extensions", bytes.NewReader(b))
		request.Header.Set("Authorization", "Bearer "+auth)
		request.Header.Set("Nimote-Host-Protocol", "1")
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, request)
		return w
	}
	if w := call("bad", capabilityRequest{Protocol: "1.0", Operation: "plugins.list"}); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("owner", capabilityRequest{Protocol: "0.9", Operation: "plugins.list"}); w.Code != 426 {
		t.Fatal(w.Code)
	}
	if w := call("owner", capabilityRequest{Protocol: "1.0", Operation: "plugins.install", Source: source, SHA256: hex.EncodeToString(sum[:]), Confirmed: true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call("owner", capabilityRequest{Protocol: "1.0", Operation: "plugins.enable", ID: "independent-echo", Grants: manifest.Permissions, Confirmed: true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	deadline := time.Now().Add(6 * time.Second)
	for r.List()[0].State != "ready" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.List()[0].State != "ready" {
		t.Fatal(r.List())
	}
	w := call("owner", capabilityRequest{Protocol: "1.0", Operation: "capabilities.invoke", Epoch: "integration", RequestID: strings.Repeat("a", 64), ID: "independent-echo/echo", Input: json.RawMessage(`{"message":"community"}`)})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var accepted struct {
		Result plugins.Invocation `json:"result"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	var run plugins.Invocation
	for time.Now().Before(deadline) {
		run, err = r.GetInvocation(accepted.Result.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.State != "succeeded" || !strings.Contains(string(run.Result), "community") {
		t.Fatal(run)
	}
	if w = call("owner", capabilityRequest{Protocol: "1.0", Operation: "capabilities.subscribe", Epoch: "integration"}); w.Code != 200 || !strings.Contains(w.Body.String(), "invocation.state") {
		t.Fatal(w.Code, w.Body)
	}
	// A plugin crash fails one invocation and the real supervisor recovers it.
	crash, err := r.Invoke("independent-echo/echo", json.RawMessage(`{"crash":true}`), true)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		run, _ = r.GetInvocation(crash.ID)
		if run.State == "failed" && r.List()[0].State == "ready" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.State != "failed" || r.List()[0].State != "ready" {
		t.Fatal("crash recovery failed", run, r.List())
	}
	blocked, err := r.Invoke("independent-echo/echo", json.RawMessage(`{"wait":true}`), true)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err = r.Cancel(blocked.ID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		run, _ = r.GetInvocation(blocked.ID)
		readyCount := 0
		for _, event := range r.Events("integration", 0).Events {
			if event.Type == "plugin.state" && event.State == "ready" {
				readyCount++
			}
		}
		if run.State == "canceled" && r.List()[0].State == "ready" && readyCount >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.State != "canceled" || r.List()[0].State != "ready" {
		t.Fatal("real process cancellation failed", run)
	}
	// Descriptor discovery reuses the existing ACP Agent/session implementation.
	communityAdapters.Lock()
	communityAdapters.runtime = r
	communityAdapters.dir = filepath.Join(dir, "plugins")
	communityAdapters.Unlock()
	defer func() { communityAdapters.Lock(); communityAdapters.runtime = nil; communityAdapters.Unlock() }()
	acpSession, err := manager.Start(StartRequest{Kind: "agent", Agent: "independent-echo/agent", Interaction: "structured", Directory: source})
	if err != nil {
		t.Fatal("community ACP start", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := manager.AgentSnapshot(acpSession.ID)
		if snapshot["ready"] == true {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = manager.AgentMessage(acpSession.ID, AgentMessage{Action: "prompt", RequestID: "community-prompt", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		output, _, _ := manager.Output(acpSession.ID, 0)
		if strings.Contains(string(output), "community ACP response") {
			found = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found {
		t.Fatal("community ACP did not use existing Agent events")
	}
	device, err := CreateDevice(dir, "observer")
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := deviceCredential(DeviceContext(context.Background(), device.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	if w = call(credential, capabilityRequest{Protocol: "1.0", Operation: "plugins.list"}); w.Code != 403 {
		t.Fatal("device bypass", w.Code, w.Body)
	}
	if w = call("owner", capabilityRequest{Protocol: "1.0", Operation: "plugins.disable", ID: "independent-echo"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w = call("owner", capabilityRequest{Protocol: "1.0", Operation: "capabilities.invoke", Epoch: "integration", RequestID: strings.Repeat("a", 64), ID: "independent-echo/echo", Input: json.RawMessage(`{}`)}); w.Code != 400 {
		t.Fatal("disabled invocation accepted", w.Code)
	}
}
func TestPluginEnvironmentAndMCPBoundary(t *testing.T) {
	t.Setenv("PLUGIN_TEST_SECRET", "SECRET")
	if strings.Contains(strings.Join(pluginEnvironment(t.TempDir()), "\n"), "SECRET") {
		t.Fatal("environment credential leak")
	}
	if _, err := mcpRequest("host_capability_query", json.RawMessage(`{"protocol":"1.0","operation":"plugins.enable","id":"example"}`)); err == nil {
		t.Fatal("read MCP tool accepted mutation")
	}
	if _, err := mcpRequest("host_capability_invoke", json.RawMessage(`{"protocol":"1.0","operation":"plugins.install"}`)); err == nil {
		t.Fatal("MCP installation allowed")
	}
}

func TestPluginCatalogManagementReusesOwnerAuthorization(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	catalog, err := plugins.NewCatalog(filepath.Join(dir, "catalogs.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	r, err := plugins.New(filepath.Join(dir, "plugins"), "catalog-test", pluginLauncher(DefaultResourceLimits()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	device, err := CreateDevice(dir, "catalog-observer")
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := deviceCredential(DeviceContext(context.Background(), device.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{dir: dir, token: "owner", plugins: r, catalog: catalog}
	call := func(auth string, request capabilityRequest) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(request)
		req := httptest.NewRequest("POST", "/v1/extensions", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Nimote-Host-Protocol", "1")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w
	}
	for _, op := range []string{"plugins.catalog.sources", "plugins.catalog.search", "plugins.catalog.add", "plugins.install_remote", "plugins.install.jobs"} {
		if w := call(credential, capabilityRequest{Protocol: "1.0", Operation: op}); w.Code != 403 {
			t.Fatal("device catalog access", op, w.Code)
		}
	}
	req := capabilityRequest{Protocol: "1.0", Operation: "plugins.catalog.add", ID: "custom", Source: "https://github.com/author/catalog/blob/main/index.json"}
	if w := call("owner", req); w.Code != 403 {
		t.Fatal("source confirmation bypass", w.Code, w.Body)
	}
	req.Confirmed = true
	if w := call("owner", req); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call("owner", capabilityRequest{Protocol: "1.0", Operation: "plugins.catalog.sources"}); w.Code != 200 || !strings.Contains(w.Body.String(), "raw.githubusercontent.com/author/catalog/main/index.json") {
		t.Fatal(w.Code, w.Body)
	}
	if w := call("owner", capabilityRequest{Protocol: "1.0", Operation: "plugins.install_remote", Source: "custom", ID: "example", SHA256: strings.Repeat("a", 64)}); w.Code != 403 {
		t.Fatal("remote confirmation bypass", w.Code)
	}
	if w := call("owner", capabilityRequest{Protocol: "1.0", Operation: "plugins.catalog.search", Query: strings.Repeat("q", 257)}); w.Code != 400 {
		t.Fatal("unbounded search", w.Code)
	}
}
