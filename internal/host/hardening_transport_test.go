// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockedStreamOutput struct {
	mu      sync.Mutex
	writes  int
	blocked chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (w *blockedStreamOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.writes++
	n := w.writes
	w.mu.Unlock()
	if n == 1 {
		return len(p), nil
	}
	w.once.Do(func() { close(w.blocked) })
	<-w.closed
	return 0, io.ErrClosedPipe
}
func (w *blockedStreamOutput) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}
func TestRPCStreamCancellationReleasesBlockedOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, writer := io.Pipe()
	defer writer.Close()
	out := &blockedStreamOutput{blocked: make(chan struct{}), closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- serveRPCStream(ctx, in, out, func(context.Context, RPCRequest) ([]byte, error) { return []byte(`{}`), nil })
	}()
	if _, err := io.WriteString(writer, "{\"id\":1,\"method\":\"GET\",\"path\":\"/v1/status\"}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-out.blocked:
	case <-time.After(time.Second):
		t.Fatal("output did not block")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation silently succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked output leaked stream workers")
	}
}
func TestRPCStreamRejectsConcurrentDuplicateIDs(t *testing.T) {
	var out bytes.Buffer
	err := serveRPCStream(context.Background(), strings.NewReader("{\"id\":1,\"method\":\"GET\",\"path\":\"/v1/status\"}\n{\"id\":1,\"method\":\"GET\",\"path\":\"/v1/status\"}\n"), &out, func(ctx context.Context, _ RPCRequest) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() })
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatal("duplicate in-flight IDs accepted", err)
	}
}
func TestEventsRecoveryValidatesBoundsAndOrdering(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, value := range []any{[]Event{{Revision: 2, SessionID: id}, {Revision: 1, SessionID: id}}, []Event{{Revision: 1, SessionID: "../token"}}, make([]Event, 1025), []Event{{Revision: ^uint64(0), SessionID: id}}} {
		dir := t.TempDir()
		_ = os.Chmod(dir, 0700)
		if err := saveJSON(filepath.Join(dir, "events"), value); err != nil {
			t.Fatal(err)
		}
		if m, err := NewManager(dir); err == nil {
			m.Close()
			t.Fatal("corrupt event journal accepted")
		}
	}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	if err := os.WriteFile(filepath.Join(dir, "events"), bytes.Repeat([]byte(" "), (256<<10)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if m, err := NewManager(dir); err == nil {
		m.Close()
		t.Fatal("oversized event journal accepted")
	}
}
func TestOutputFutureCursorAddsResyncWithoutBreakingLegacyContract(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID)
	data, start, next, err := m.OutputPage(s.ID, 100)
	if err != nil || len(data) != 0 || start != 100 || next != 100 {
		t.Fatal("legacy cursor contract changed", start, next, err)
	}
	w := deviceRequest(&Server{manager: m, token: "owner"}, "owner", "GET", "/v1/sessions/"+s.ID+"/output?offset=100&wait_ms=1000", "")
	var page struct {
		Resync bool `json:"resync_required"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || !page.Resync {
		t.Fatal(w.Body.String())
	}
}
func TestOutputCloseIsIdempotentAndWakesWaiters(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "ring")
	if err != nil {
		t.Fatal(err)
	}
	w := &outputWriter{file: f}
	signal := w.signal()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal("duplicate close failed", err)
	}
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("close did not wake subscriber")
	}
	if _, err := w.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}

func TestLegacyLogResyncUsesRealFileBoundary(t *testing.T) {
	m := newTestManager(t)
	id := strings.Repeat("e", 64)
	// Old records may have no persisted output byte count.
	m.sessions[id] = &managed{session: Session{ID: id, State: "succeeded"}}
	if err := os.WriteFile(filepath.Join(m.dir, id+".log"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		offset string
		resync bool
	}{{"2", false}, {"6", false}, {"7", true}} {
		w := deviceRequest(&Server{manager: m, token: "owner"}, "owner", "GET", "/v1/sessions/"+id+"/output?offset="+tc.offset, "")
		var page struct {
			Resync bool `json:"resync_required"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Resync != tc.resync {
			t.Fatalf("legacy offset %s: %s", tc.offset, w.Body.String())
		}
	}
}
