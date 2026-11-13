// SPDX-License-Identifier: Apache-2.0
package host

import (
	"io"
	"strings"
	"testing"
)

type receiptTerminal struct {
	writes  int
	partial bool
}

func (t *receiptTerminal) Read([]byte) (int, error) { return 0, io.EOF }
func (t *receiptTerminal) Close() error             { return nil }
func (t *receiptTerminal) Write(p []byte) (int, error) {
	t.writes++
	if t.partial {
		return len(p) / 2, nil
	}
	return len(p), nil
}

type receiptProcess struct{}

func (receiptProcess) Wait() (int, error) { return 0, nil }
func (receiptProcess) Kill() error        { return nil }
func (receiptProcess) Close() error       { return nil }

func TestTerminalInputReceiptNeverRewritesAndPreservesUncertainty(t *testing.T) {
	for _, partial := range []bool{false, true} {
		m := newTestManager(t)
		terminal := &receiptTerminal{partial: partial}
		id := strings.Repeat("b", 64)
		done := make(chan struct{})
		close(done)
		m.sessions[id] = &managed{session: Session{ID: id, State: "running", Kind: "terminal"}, terminal: terminal, process: receiptProcess{}, done: done}
		server := &Server{manager: m, token: strings.Repeat("a", 64)}
		body := `{"request_id":"paste","data":"dGVzdA=="}`
		for range 2 {
			response := request(t, server, "POST", "/v1/sessions/"+id+"/input", body, server.token, "1")
			if partial {
				if response.Code != 503 {
					t.Fatalf("partial write reported as delivered: %d %s", response.Code, response.Body)
				}
			} else if response.Code != 200 || !strings.Contains(response.Body.String(), `"stage":"terminal_written"`) {
				t.Fatalf("bad write receipt: %d %s", response.Code, response.Body)
			}
		}
		if terminal.writes != 1 {
			t.Fatalf("input delivered %d times", terminal.writes)
		}
	}
}
