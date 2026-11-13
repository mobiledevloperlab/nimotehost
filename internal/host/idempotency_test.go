// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHTTPCreateReceiptRequiresAuthenticationAndBindsPayload(t *testing.T) {
	m := newTestManager(t)
	server := &Server{manager: m, token: strings.Repeat("a", 64)}
	req := childRequest(t, "output")
	req.RequestID = "http-create"
	encoded, _ := json.Marshal(req)
	bad := request(t, server, "POST", "/v1/sessions", string(encoded), "wrong", "1")
	if bad.Code != 401 || len(m.List(0)) != 0 {
		t.Fatal("unauthorized request dispatched")
	}
	first := request(t, server, "POST", "/v1/sessions", string(encoded), server.token, "1")
	second := request(t, server, "POST", "/v1/sessions", string(encoded), server.token, "1")
	if first.Code != 201 || second.Code != 201 || first.Body.String() != second.Body.String() || len(m.List(0)) != 1 {
		t.Fatalf("duplicate create: %d %s / %d %s", first.Code, first.Body, second.Code, second.Body)
	}
	req.Args = append(req.Args, "changed")
	encoded, _ = json.Marshal(req)
	conflict := request(t, server, "POST", "/v1/sessions", string(encoded), server.token, "1")
	if conflict.Code != 409 {
		t.Fatalf("payload conflict: %d", conflict.Code)
	}
}

func TestRequestQuotaRejectsBeforeSideEffects(t *testing.T) {
	m := newTestManager(t)
	for _, active := range []bool{false, true} {
		m.requests.count, m.requests.active = 0, 0
		if active {
			m.requests.active = 64
		} else {
			m.requests.count = 65536
		}
		_, err := runOnce(m, "quota", "mutation", "payload", func() (bool, error) {
			t.Error("quota allowed a side effect")
			return true, nil
		})
		if err == nil {
			t.Fatal("expected resource limit")
		}
	}
}

func TestStartRequestSurvivesRetryAndRestart(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "output")
	req.RequestID = "create-from-phone"
	first, err := m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Start(req)
	if err != nil || first.ID != second.ID || len(m.List(0)) != 1 {
		t.Fatalf("retry started another process: %+v %v", second, err)
	}
	awaitSession(t, m, first.ID)
	m.Close()
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	third, err := recovered.Start(req)
	if err != nil || third.ID != first.ID || len(recovered.List(0)) != 1 {
		t.Fatalf("restart replayed create: %+v %v", third, err)
	}
	req.Args = append(req.Args, "different")
	if _, err := recovered.Start(req); !errors.Is(err, errRequestConflict) {
		t.Fatalf("changed payload accepted: %v", err)
	}
}

func TestRequestReservationNeverReplaysUncertainMutation(t *testing.T) {
	m := newTestManager(t)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := runOnce(m, "input", "session.input/a", "payload", func() (bool, error) {
			close(entered)
			<-release
			return true, nil
		})
		done <- err
	}()
	<-entered
	_, err := runOnce(m, "input", "session.input/a", "payload", func() (bool, error) {
		t.Error("concurrent mutation replayed")
		return false, nil
	})
	if !errors.Is(err, errRequestUncertain) {
		t.Errorf("got %v", err)
	}
	// An unrelated operation must remain usable during the slow mutation.
	_, err = runOnce(m, "other", "session.input/b", "payload", func() (bool, error) { return true, nil })
	if err != nil {
		t.Error(err)
	}
	// Reopening the ledger simulates a daemon dying with an unfinished write.
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runOnce(recovered, "input", "session.input/a", "payload", func() (bool, error) {
		t.Error("reserved mutation replayed after restart")
		return false, nil
	})
	if !errors.Is(err, errRequestUncertain) {
		t.Errorf("got %v", err)
	}
	recovered.Close()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRequestReceiptBindsSessionAndPreservesPartialWriteUncertainty(t *testing.T) {
	m := newTestManager(t)
	run := func() (bool, error) { return false, errors.Join(errRequestUncertain, errors.New("partial write")) }
	for range 2 {
		if _, err := runOnce(m, "write", "session.input/a", "text", run); !errors.Is(err, errRequestUncertain) {
			t.Fatalf("uncertainty lost: %v", err)
		}
	}
	if _, err := runOnce(m, "write", "session.input/b", "text", run); !errors.Is(err, errRequestConflict) {
		t.Fatalf("wrong session accepted: %v", err)
	}
}
