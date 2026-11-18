// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRPCStreamOversizedResponseKeepsBridgeOpen(t *testing.T) {
	requests := strings.NewReader(
		"{\"id\":1,\"method\":\"GET\",\"path\":\"/v1/large\"}\n" +
			"{\"id\":2,\"method\":\"GET\",\"path\":\"/v1/error\"}\n" +
			"{\"id\":3,\"method\":\"GET\",\"path\":\"/v1/status\"}\n",
	)
	var replies bytes.Buffer
	err := serveRPCStream(context.Background(), requests, &replies, func(_ context.Context, req RPCRequest) ([]byte, error) {
		switch req.Path {
		case "/v1/large":
			// The HTTP body fits, but escaping it inside the JSONL frame does not.
			return []byte(`"` + strings.Repeat("<", maxRPCStreamFrame/6) + `"`), nil
		case "/v1/error":
			return nil, errors.New(strings.Repeat("\n", maxRPCStreamFrame/2))
		default:
			return []byte(`{"ok":true}`), nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(replies.Bytes()), []byte{'\n'})
	if len(lines) != 4 {
		t.Fatalf("got %d frames, want greeting and three replies", len(lines))
	}
	for _, line := range lines[1:] {
		if len(line) > maxRPCStreamFrame {
			t.Fatalf("oversized frame: %d bytes", len(line))
		}
		var response rpcStreamResponse
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatal(err)
		}
		if response.ID == 3 {
			if string(response.Result) != `{"ok":true}` || response.Error != nil {
				t.Fatalf("invalid status reply: %s", line)
			}
		} else if response.Error == nil || *response.Error != "Host RPC response exceeds stream frame limit" {
			t.Fatalf("expected per-request size error for %d", response.ID)
		}
	}
}

func TestRPCStreamMultiplexesOutputWaitAndControlRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newTestManager(t)
	server := &Server{manager: m, token: "test-token"}
	call := func(ctx context.Context, request RPCRequest) ([]byte, error) {
		req := httptest.NewRequest(request.Method, request.Path, bytes.NewReader(request.Body)).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Nimote-Host-Protocol", "1")
		req.Header.Set("Content-Type", "application/json")
		reply := httptest.NewRecorder()
		server.ServeHTTP(reply, req)
		if reply.Code != 200 {
			return nil, fmt.Errorf("HTTP %d", reply.Code)
		}
		return reply.Body.Bytes(), nil
	}
	session, err := m.Start(childRequest(t, "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	requestsR, requestsW := io.Pipe()
	repliesR, repliesW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- serveRPCStream(ctx, requestsR, repliesW, call); repliesW.Close() }()
	decoder := json.NewDecoder(repliesR)
	var frame map[string]any
	if err = decoder.Decode(&frame); err != nil || frame["stream_protocol"] != float64(1) {
		t.Fatal(frame, err)
	}
	encoder := json.NewEncoder(requestsW)
	if err = encoder.Encode(map[string]any{"id": 1, "method": "GET", "path": "/v1/sessions/" + session.ID + "/output?offset=0&wait_ms=1000"}); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	if err = encoder.Encode(map[string]any{"id": 2, "method": "GET", "path": "/v1/status"}); err != nil {
		t.Fatal(err)
	}
	if err = decoder.Decode(&frame); err != nil || frame["id"] != float64(2) {
		t.Fatal(frame, err)
	}
	if time.Since(begin) > 500*time.Millisecond {
		t.Fatal("output wait blocked concurrent request")
	}
	if err = decoder.Decode(&frame); err != nil || frame["id"] != float64(1) {
		t.Fatal(frame, err)
	}
	requestsW.Close()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	requestsR.Close()
	repliesR.Close()
	// Detaching the bridge must not terminate its daemon-owned task.
	session, err = m.Get(session.ID)
	if err != nil || session.State != "running" {
		t.Fatal(session, err)
	}
}

func TestOutputWaitWakesOnWriteAndCancellation(t *testing.T) {
	m := newTestManager(t)
	id := strings.Repeat("a", 64)
	f, err := os.CreateTemp(m.dir, "wait-output")
	if err != nil {
		t.Fatal(err)
	}
	writer := &outputWriter{file: f}
	defer writer.Close()
	m.sessions[id] = &managed{session: Session{ID: id}, output: writer}
	server := &Server{manager: m, token: "test-token"}
	request := func(ctx context.Context) *http.Request {
		req := httptest.NewRequest("GET", "/v1/sessions/"+id+"/output?offset=0&wait_ms=1000", nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Nimote-Host-Protocol", "1")
		return req
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		reply := httptest.NewRecorder()
		server.ServeHTTP(reply, request(context.Background()))
		done <- reply
	}()
	time.Sleep(25 * time.Millisecond)
	begin := time.Now()
	if _, err = writer.Write([]byte("LIVE")); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-done:
		if reply.Code != 200 || !bytes.Contains(reply.Body.Bytes(), []byte("TElWRQ==")) {
			t.Fatal(reply.Body.String())
		}
		if time.Since(begin) > 250*time.Millisecond {
			t.Fatal("new output was not delivered promptly")
		}
	case <-time.After(time.Second):
		t.Fatal("output wait did not wake")
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := request(ctx)
	req.URL.RawQuery = "offset=4&wait_ms=1000"
	go func() { reply := httptest.NewRecorder(); server.ServeHTTP(reply, req); done <- reply }()
	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("cancelled output request did not stop")
	}
}

func TestOutputWaitReturnsWhenSessionAlreadyFinished(t *testing.T) {
	m := newTestManager(t)
	id := strings.Repeat("f", 64)
	f, err := os.Create(filepath.Join(m.dir, id+".ring"))
	if err != nil {
		t.Fatal(err)
	}
	writer := &outputWriter{file: f}
	if _, err := writer.Write([]byte("done")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	m.sessions[id] = &managed{session: Session{ID: id, State: "succeeded"}, output: writer}
	server := &Server{manager: m, token: "test-token"}
	req := httptest.NewRequest("GET", "/v1/sessions/"+id+"/output?offset=4&wait_ms=1000", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Nimote-Host-Protocol", "1")
	start := time.Now()
	reply := httptest.NewRecorder()
	server.ServeHTTP(reply, req)
	if reply.Code != 200 || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("completed output wait took %v: %s", time.Since(start), reply.Body.String())
	}
}

func TestRPCStreamCancellationUnblocksIdleInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		done <- serveRPCStream(ctx, input, io.Discard, func(context.Context, RPCRequest) ([]byte, error) {
			return nil, nil
		})
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled stream stayed blocked on stdin")
	}
}

func TestRPCStreamEOFDrainsAcceptedRequest(t *testing.T) {
	var replies bytes.Buffer
	err := serveRPCStream(context.Background(), strings.NewReader("{\"id\":1,\"method\":\"GET\",\"path\":\"/v1/status\"}\n"), &replies,
		func(ctx context.Context, request RPCRequest) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return []byte(`{"running":true}`), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(replies.Bytes(), []byte(`"result":{"running":true}`)) {
		t.Fatal(replies.String())
	}
}
