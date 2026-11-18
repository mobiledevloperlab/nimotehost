// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOutputLongPollsLeaveStatusCapacity(t *testing.T) {
	m := newTestManager(t)
	id := strings.Repeat("b", 64)
	f, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// A running output source with no bytes keeps admitted long polls waiting.
	m.sessions[id] = &managed{session: Session{ID: id, State: "running"}, output: &outputWriter{file: f, changed: make(chan struct{})}}
	api := &Server{manager: m, token: "owner", admission: newRequestAdmission()}
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	var finished atomic.Int32
	defer func() { cancel(); workers.Wait(); delete(m.sessions, id) }()
	for range 8 {
		workers.Go(func() {
			r := httptest.NewRequest("GET", "/v1/sessions/"+id+"/output?wait_ms=1000", nil).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer owner")
			r.Header.Set("Nimote-Host-Protocol", "1")
			api.ServeHTTP(httptest.NewRecorder(), r)
			finished.Add(1)
		})
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for len(api.admission.ownerReads)+int(finished.Load()) < 8 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(api.admission.ownerReads) != 4 {
		t.Fatal("expected four admitted output readers", len(api.admission.ownerReads))
	}
	if reply := deviceRequest(api, "owner", "GET", "/v1/status", ""); reply.Code != http.StatusOK {
		t.Fatal("output polling made a live Host appear offline", reply.Code, reply.Body.String())
	}
	for wait, want := range map[string]int{"1": http.StatusTooManyRequests, "00": http.StatusOK, "1001": http.StatusBadRequest} {
		reply := deviceRequest(api, "owner", "GET", "/v1/sessions/"+id+"/output?wait_ms="+wait, "")
		if reply.Code != want {
			t.Fatalf("wait_ms=%s returned %d, want %d: %s", wait, reply.Code, want, reply.Body.String())
		}
	}
}

func TestServerRequiresBearerScheme(t *testing.T) {
	for _, auth := range []string{"secret", "Basic secret", "Bearer secret"} {
		t.Run(auth, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
			request.Header.Set("Authorization", auth)
			request.Header.Set("Nimote-Host-Protocol", "1")
			reply := httptest.NewRecorder()
			(&Server{token: "secret"}).ServeHTTP(reply, request)
			want := http.StatusUnauthorized
			if auth == "Bearer secret" {
				want = http.StatusOK
			}
			if reply.Code != want {
				t.Fatalf("status %d, want %d", reply.Code, want)
			}
		})
	}
}

func TestReadJSONMediaTypeAndLimits(t *testing.T) {
	for _, test := range []struct {
		name, mediaType, body string
		status                int
	}{
		{"plain", "application/json", `{}`, 200},
		{"charset", "application/json; charset=utf-8", `{}`, 200},
		{"invalid type", "text/plain", `{}`, 415},
		{"invalid parameter", "application/json; charset", `{}`, 415},
		{"unknown field", "application/json", `{"unknown":1}`, 400},
		{"multiple values", "application/json", `{} {}`, 400},
		{"too large", "application/json", strings.Repeat(" ", maxRequest+1), 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.mediaType)
			reply := httptest.NewRecorder()
			var target struct{}
			accepted := readJSON(reply, request, &target)
			if accepted != (test.status == 200) || reply.Code != test.status {
				t.Fatalf("accepted=%t status=%d", accepted, reply.Code)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCallRejectsOversizedResponse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	saved := loopbackClient
	defer func() { loopbackClient = saved }()
	loopbackClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), (2<<20)+1)))}, nil
	})}
	data, err := Call(context.Background(), dir, RPCRequest{Method: "GET", Path: "/v1/status"})
	if err == nil || data != nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("length=%d error=%v", len(data), err)
	}
}

func TestHistoryDeadlinesEncloseProviderTimeout(t *testing.T) {
	for _, path := range []string{"/v1/agent-history?agent=codex", "/v1/agent-history/thread?agent=claude"} {
		client := rpcHTTPClient(RPCRequest{Method: http.MethodGet, Path: path})
		if client.Timeout <= agentHistoryResponseTimeout || agentHistoryResponseTimeout <= agentHistoryTimeout {
			t.Fatal("history deadlines do not enclose provider timeout")
		}
	}
	if loopbackClient.Timeout != 10*time.Second {
		t.Fatal("default RPC deadline changed")
	}
}

func TestRPCStreamMalformedInputCancelsAcceptedCalls(t *testing.T) {
	for _, tail := range []string{"invalid\n", strings.Repeat("x", maxRequest+1024)} {
		done := make(chan error, 1)
		go func() {
			done <- serveRPCStream(context.Background(), strings.NewReader("{\"id\":1,\"method\":\"GET\",\"path\":\"/v1/status\"}\n"+tail), io.Discard,
				func(ctx context.Context, _ RPCRequest) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() })
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("invalid stream succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("malformed input left an accepted call blocked")
		}
	}
}

type failedReplyWriter struct {
	handshake bool
	err       error
}

func (w *failedReplyWriter) Write(data []byte) (int, error) {
	if !w.handshake {
		w.handshake = true
		return len(data), nil
	}
	return 0, w.err
}
func TestRPCStreamReturnsResponseWriteError(t *testing.T) {
	failure := errors.New("response write failed")
	err := serveRPCStream(context.Background(), strings.NewReader("{\"id\":1,\"method\":\"GET\",\"path\":\"/v1/status\"}\n"), &failedReplyWriter{err: failure},
		func(context.Context, RPCRequest) ([]byte, error) { return []byte(`{}`), nil })
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestSessionResultsOwnMutableFields(t *testing.T) {
	m := newTestManager(t)
	session, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	session = awaitSession(t, m, session.ID)
	finished := *session.Finished
	session.Args[0] = "mutated"
	*session.ExitCode = 123
	*session.Finished = time.Time{}
	fromList := m.List(0)[0]
	if *fromList.ExitCode != 0 || !fromList.Finished.Equal(finished) || fromList.Args[0] == "mutated" {
		t.Fatal("Get leaked manager state")
	}
	*fromList.ExitCode = 321
	fromGet, err := m.Get(session.ID)
	if err != nil || *fromGet.ExitCode != 0 {
		t.Fatal("List leaked manager state", err)
	}
}
