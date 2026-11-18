// SPDX-License-Identifier: Apache-2.0
package host

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestOutputWaitAdmissionReservesReadsAndReleases(t *testing.T) {
	for _, owner := range []bool{false, true} {
		t.Run(map[bool]string{false: "device", true: "owner"}[owner], func(t *testing.T) {
			a := newRequestAdmission()
			var release []func()
			for range 4 {
				f, ok := a.acquire("one", owner, false, true)
				if !ok {
					t.Fatal("early output rejection")
				}
				release = append(release, f)
			}
			if _, ok := a.acquire("one", owner, false, true); ok {
				t.Fatal("output consumed reserved reads")
			}
			for range 4 {
				f, ok := a.acquire("one", owner, false, false)
				if !ok {
					t.Fatal("metadata lost reserved reads")
				}
				release = append(release, f)
			}
			if _, ok := a.acquire("one", owner, false, false); ok {
				t.Fatal("total read limit increased")
			}
			for _, f := range release {
				f()
			}
			if len(a.devices) != 0 || len(a.ownerReads) != 0 || len(a.ownerOutputWaits) != 0 {
				t.Fatal("admission slots leaked")
			}
			f, ok := a.acquire("one", owner, false, true)
			if !ok {
				t.Fatal("output did not recover capacity")
			}
			f()
		})
	}
}

func TestPrincipalAdmissionIsolationAndRelease(t *testing.T) {
	a := newRequestAdmission()
	var release []func()
	for range 8 {
		f, ok := a.acquire("a", false, false, false)
		if !ok {
			t.Fatal("early rejection")
		}
		release = append(release, f)
	}
	if _, ok := a.acquire("a", false, false, false); ok {
		t.Fatal("unbounded device reads")
	}
	for _, principal := range []struct {
		id    string
		owner bool
	}{{"b", false}, {"", true}} {
		f, ok := a.acquire(principal.id, principal.owner, false, false)
		if !ok {
			t.Fatal("one device exhausted another principal")
		}
		f()
	}
	var mutations []func()
	for range 2 {
		f, ok := a.acquire("a", false, true, false)
		if !ok {
			t.Fatal("reads exhausted control slots")
		}
		mutations = append(mutations, f)
	}
	if _, ok := a.acquire("a", false, true, false); ok {
		t.Fatal("unbounded device mutations")
	}
	for _, f := range append(release, mutations...) {
		f()
	}
	if len(a.devices) != 0 {
		t.Fatal("idle principal entries leaked")
	}
}
func TestPrincipalAdmissionConcurrentBound(t *testing.T) {
	a := newRequestAdmission()
	var wg sync.WaitGroup
	start := make(chan struct{})
	accepted := make(chan func(), 64)
	for range 64 {
		wg.Go(func() {
			<-start
			if release, ok := a.acquire("one", false, false, false); ok {
				accepted <- release
			}
		})
	}
	close(start)
	wg.Wait()
	close(accepted)
	count := 0
	for f := range accepted {
		count++
		f()
	}
	if count != 8 {
		t.Fatal("concurrent admission escaped per-device bound", count)
	}
}
func TestBoundedListenerCloseUnblocksCapacityAndReleaseOnce(t *testing.T) {
	raw, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := newBoundedListener(raw, 1)
	defer l.Close()
	client, err := net.Dial("tcp4", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := l.Accept(); done <- err }()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accept survived close")
		}
	case <-time.After(time.Second):
		t.Fatal("capacity waiter leaked")
	}
	_ = conn.Close()
	_ = conn.Close()
	if len(l.slots) != 0 {
		t.Fatal("connection capacity leaked")
	}
}
func TestOwnerReservedAdmissionSurvivesDeviceSaturation(t *testing.T) {
	m := newTestManager(t)
	api := &Server{manager: m, token: "owner", admission: newRequestAdmission(), requests: make(chan struct{}, 1), controls: make(chan struct{}, 1)}
	api.requests <- struct{}{}
	api.controls <- struct{}{}
	if w := deviceRequest(api, "owner", "GET", "/v1/status", ""); w.Code != 200 {
		t.Fatal("device saturation blocked owner", w.Code)
	}
}
