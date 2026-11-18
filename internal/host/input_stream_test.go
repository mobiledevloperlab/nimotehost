// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func inputStreamFixture(t testing.TB) (*Manager, *Server, string, *receiptTerminal) {
	t.Helper()
	m, err := NewManager(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	id := strings.Repeat("b", 64)
	terminal := &receiptTerminal{}
	done := make(chan struct{})
	close(done)
	m.sessions[id] = &managed{session: Session{ID: id, State: "running", Kind: "terminal"}, terminal: terminal, process: receiptProcess{}, done: done}
	return m, &Server{manager: m, token: strings.Repeat("a", 64)}, id, terminal
}

func TestInputStreamOrderingAndDuplicateReceipts(t *testing.T) {
	m, _, id, terminal := inputStreamFixture(t)
	stream, err := m.OpenInputStream(id, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []uint64{2, 9007199254740992, 0} {
		if _, err := m.StreamInput(id, "owner", stream, sequence, []byte("x")); err == nil {
			t.Fatalf("accepted invalid first sequence %d", sequence)
		}
	}
	for range 2 {
		receipt, err := m.StreamInput(id, "owner", stream, 1, []byte("x"))
		if err != nil || !receipt.OK || receipt.Sequence != 1 || receipt.StreamID != stream {
			t.Fatal(receipt, err)
		}
	}
	if _, err := m.StreamInput(id, "owner", stream, 1, []byte("different")); !errors.Is(err, errRequestConflict) {
		t.Fatal("changed duplicate accepted", err)
	}
	if _, err := m.StreamInput(id, "owner", stream, 2, []byte("y")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StreamInput(id, "owner", stream, 1, []byte("x")); err == nil {
		t.Fatal("old sequence accepted")
	}
	if terminal.writes != 2 || m.RequestStorage().Receipts != 0 {
		t.Fatal("duplicate write or durable receipt", terminal.writes, m.RequestStorage())
	}
}

func TestInputStreamPartialWritePoisonsFollowingFrames(t *testing.T) {
	m, _, id, terminal := inputStreamFixture(t)
	terminal.partial = true
	stream, _ := m.OpenInputStream(id, "owner")
	for _, sequence := range []uint64{1, 1, 2, 3} {
		if _, err := m.StreamInput(id, "owner", stream, sequence, []byte("test")); !errors.Is(err, errRequestUncertain) {
			t.Fatal("failed write was not uncertain", sequence, err)
		}
	}
	if terminal.writes != 1 {
		t.Fatal("uncertain bytes were rewritten", terminal.writes)
	}
}

func TestInputStreamsAreSessionAndPrincipalBoundAndBounded(t *testing.T) {
	m, _, id, terminal := inputStreamFixture(t)
	stream, _ := m.OpenInputStream(id, "owner")
	if _, err := m.StreamInput(id, "other-device", stream, 1, []byte("x")); err == nil {
		t.Fatal("another principal used stream")
	}
	otherID := strings.Repeat("c", 64)
	m.sessions[otherID] = &managed{session: Session{ID: otherID, State: "running"}, process: receiptProcess{}, terminal: terminal, done: m.sessions[id].done}
	if _, err := m.StreamInput(otherID, "owner", stream, 1, []byte("x")); err == nil {
		t.Fatal("another session used stream")
	}
	for range maxInputStreams {
		if _, err := m.OpenInputStream(id, "owner"); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.sessions[id].inputStreams) != maxInputStreams {
		t.Fatal("stream state is unbounded")
	}
	if _, err := m.StreamInput(id, "owner", stream, 1, []byte("x")); err == nil {
		t.Fatal("evicted stream was recreated")
	}
	if terminal.writes != 0 {
		t.Fatal("rejected frame reached terminal")
	}
}

func TestInputStreamConcurrentDuplicatesWriteOnce(t *testing.T) {
	m, _, id, terminal := inputStreamFixture(t)
	stream, _ := m.OpenInputStream(id, "owner")
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if _, err := m.StreamInput(id, "owner", stream, 1, []byte("x")); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if terminal.writes != 1 {
		t.Fatal("concurrent retry duplicated input", terminal.writes)
	}
}

func TestInputStreamRestartAndFinishedSessionRejectFrames(t *testing.T) {
	m, _, id, terminal := inputStreamFixture(t)
	stream, _ := m.OpenInputStream(id, "owner")
	if err := saveJSON(filepath.Join(m.dir, id+".json"), m.sessions[id].session); err != nil {
		t.Fatal(err)
	}
	m.Close()
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recovered.Close)
	if _, err := recovered.StreamInput(id, "owner", stream, 1, []byte("x")); err == nil {
		t.Fatal("old daemon stream accepted")
	}
	if _, err := recovered.OpenInputStream(id, "owner"); err == nil {
		t.Fatal("recovered terminal was restarted by input")
	}
	if terminal.writes != 0 {
		t.Fatal("restart replayed input")
	}
}

func TestInputStreamHTTPAndLegacyCompatibility(t *testing.T) {
	m, server, id, terminal := inputStreamFixture(t)
	path := "/v1/sessions/" + id
	opened := request(t, server, "POST", path+"/input-stream", `{}`, server.token, "1")
	var value map[string]string
	if opened.Code != 200 || json.Unmarshal(opened.Body.Bytes(), &value) != nil || !validID(value["stream_id"]) {
		t.Fatal(opened.Code, opened.Body)
	}
	body := fmt.Sprintf(`{"stream_id":%q,"sequence":1,"data":"eA=="}`, value["stream_id"])
	for range 2 {
		response := request(t, server, "POST", path+"/input", body, server.token, "1")
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"sequence":1`) {
			t.Fatal(response.Code, response.Body)
		}
	}
	for _, invalid := range []string{
		fmt.Sprintf(`{"stream_id":%q,"sequence":2,"request_id":"mixed","data":"eA=="}`, value["stream_id"]),
		`{"sequence":1,"data":"eA=="}`, `{"stream_id":"bad","data":"eA=="}`,
	} {
		if response := request(t, server, "POST", path+"/input", invalid, server.token, "1"); response.Code != 400 {
			t.Fatal("invalid stream request accepted", response.Code, response.Body)
		}
	}
	// An on-disk receipt written with the old payload shape must still match.
	legacy := struct {
		Data      []byte `json:"data"`
		RequestID string `json:"request_id,omitempty"`
	}{[]byte("old"), "legacy"}
	_, err := runOnce(m, legacy.RequestID, "session.input/"+id, legacy, func() (mutationReceipt, error) {
		return mutationReceipt{OK: true, RequestID: legacy.RequestID, Stage: "terminal_written"}, m.Input(id, legacy.Data)
	})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server, "POST", path+"/input", `{"request_id":"legacy","data":"b2xk"}`, server.token, "1")
	if response.Code != 200 || terminal.writes != 2 {
		t.Fatal("legacy receipt changed or was replayed", response.Code, response.Body, terminal.writes)
	}
	if response := request(t, server, "POST", path+"/input", `{"data":"eA=="}`, server.token, "1"); response.Code != 200 {
		t.Fatal("old non-idempotent input failed", response.Code, response.Body)
	}
	entries, _ := os.ReadDir(m.requests.dir)
	if len(entries) != 2 { // retirement watermark plus legacy receipt
		t.Fatal("stream input created disk receipts", len(entries))
	}
}

func TestInputStreamDevicePermissions(t *testing.T) {
	id := strings.Repeat("b", 64)
	for _, role := range []string{"observer", "session_operator", "session_controller"} {
		d := Device{Role: role, Sessions: []string{id}}
		if allowed := d.allows("POST", "/v1/sessions/"+id+"/input-stream"); allowed != (role == "session_controller") {
			t.Fatal("input stream permission mismatch", role, allowed)
		}
		if d.allows("POST", "/v1/sessions/"+strings.Repeat("c", 64)+"/input-stream") {
			t.Fatal("device escaped session scope", role)
		}
	}
}

func BenchmarkTerminalInputReceipt(b *testing.B) {
	for _, mode := range []string{"durable", "stream"} {
		b.Run(mode, func(b *testing.B) {
			m, server, id, _ := inputStreamFixture(b)
			stream, _ := m.OpenInputStream(id, "session.input/"+id)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				body := fmt.Sprintf(`{"request_id":"bench-%d","data":"eA=="}`, i)
				if mode == "stream" {
					body = fmt.Sprintf(`{"stream_id":%q,"sequence":%d,"data":"eA=="}`, stream, i+1)
				}
				r := httptest.NewRequest("POST", "/v1/sessions/"+id+"/input", strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+server.token)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Nimote-Host-Protocol", "1")
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)
				if w.Code != 200 {
					b.Fatal(w.Code, w.Body)
				}
			}
		})
	}
}
