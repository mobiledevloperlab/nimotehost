// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type startupRewriteTransport struct {
	target    *url.URL
	transport http.RoundTripper
}

func (r startupRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	return r.transport.RoundTrip(clone)
}

func TestColdAgentStartupSurvivesAllHTTPDeadlines(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestCodexProtocolChild\n"
	if err = os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NIMOTE_CODEX_PROTOCOL_CHILD", "1")
	// Exceed the old 5/7-second handshake and 10-second HTTP limits. This is
	// intentionally a real subprocess and socket, not a mocked deadline constant.
	t.Setenv("NIMOTE_CODEX_INIT_DELAY_MS", "12500")
	m := newTestManager(t)
	server := httptest.NewUnstartedServer(&Server{manager: m, token: "startup-test-token", hostID: "startup-test"})
	server.Config.ReadHeaderTimeout = 5 * time.Second
	server.Config.ReadTimeout = 10 * time.Second
	server.Config.WriteTimeout = time.Second
	server.Start()
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	previous := loopbackClient
	loopbackClient = &http.Client{Timeout: 500 * time.Millisecond, Transport: startupRewriteTransport{target: target, transport: transport}}
	defer func() { loopbackClient = previous }()
	credentialDir := t.TempDir()
	if err = os.Chmod(credentialDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(credentialDir, "token"), []byte("startup-test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: dir})
	type answer struct {
		data []byte
		err  error
	}
	result := make(chan answer, 1)
	go func() {
		data, err := Call(context.Background(), credentialDir, RPCRequest{Method: "POST", Path: "/v1/sessions", Body: body})
		result <- answer{data, err}
	}()
	// Initialization must not own the manager lock or hold up ordinary requests.
	deadline := time.Now().Add(5 * time.Second)
	for len(m.List(0)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(m.List(0)) == 0 {
		select {
		case reply := <-result:
			t.Fatalf("initialization did not start: %s %v", reply.data, reply.err)
		default:
			t.Fatal("initialization did not start")
		}
	}
	if _, err = Call(context.Background(), credentialDir, RPCRequest{Method: "GET", Path: "/v1/status"}); err != nil {
		t.Fatal("startup blocked status RPC", err)
	}
	reply := <-result
	if reply.err != nil {
		t.Fatal("cold start was cut off by an enclosing RPC deadline", reply.err)
	}
	var session Session
	if err = json.Unmarshal(reply.data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Interaction != "structured" || session.CompatibilityFallback != "" {
		t.Fatalf("slow compatible agent was downgraded: %+v", session)
	}
	if err = m.Cancel(session.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID)
}
