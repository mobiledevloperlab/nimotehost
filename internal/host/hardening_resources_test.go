// SPDX-License-Identifier: Apache-2.0
package host

import (
	"errors"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResourcePolicyDefaultsValidationAndSafeUnsupported(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	defaults, err := ReadResourceLimits(dir)
	if err != nil || defaults != DefaultResourceLimits() {
		t.Fatal(defaults, err)
	}
	for _, raw := range []string{`{"max_active_sessions":0}`, `{"max_active_sessions":33}`, `{"max_retained_sessions":1}`, `{"max_runtime_seconds":-1}`, `{"cpu_percent":10}`, `{"memory_bytes":4096}`, `{"unknown":1}`, `null`} {
		p := filepath.Join(dir, "resources.json")
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadResourceLimits(dir); err == nil {
			t.Fatalf("unsupported policy accepted: %s", raw)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "resources.json"), []byte(`{"max_active_sessions":2,"max_retained_sessions":4,"max_runtime_seconds":5,"min_free_disk_bytes":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadResourceLimits(dir)
	if err != nil || got.MaxActiveSessions != 2 || got.MaxRuntimeSeconds != 5 {
		t.Fatal(got, err)
	}
	if _, err := availableDiskBytes(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing filesystem reported healthy")
	}
}
func TestResourceConcurrentAdmissionAndCancellationRelease(t *testing.T) {
	limits := DefaultResourceLimits()
	limits.MaxActiveSessions = 2
	m, err := NewManagerWithLimits(filepath.Join(t.TempDir(), "sessions"), limits)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var wg sync.WaitGroup
	var admitted atomic.Int32
	ids := make(chan string, 12)
	for range 12 {
		req := childRequest(t, "sleep")
		wg.Go(func() {
			s, err := m.Start(req)
			if err == nil {
				admitted.Add(1)
				ids <- s.ID
			} else if !errors.Is(err, errResourceLimit) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(ids)
	if admitted.Load() != 2 || m.Resources().Active != 2 {
		t.Fatal("concurrent admission exceeded limit", admitted.Load())
	}
	for id := range ids {
		if err := m.Cancel(id); err != nil {
			t.Fatal(err)
		}
		awaitSession(t, m, id)
	}
	s, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal("capacity not released", err)
	}
	awaitSession(t, m, s.ID)
}
func TestResourceDiskAdmissionRuntimeAndOutputFailure(t *testing.T) {
	limits := DefaultResourceLimits()
	limits.MinFreeDiskBytes = math.MaxUint64
	m, err := NewManagerWithLimits(filepath.Join(t.TempDir(), "sessions"), limits)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.Start(childRequest(t, "sleep")); !errors.Is(err, errResourceLimit) {
		t.Fatal("low disk admission accepted", err)
	}
	if len(m.List(0)) != 0 {
		t.Fatal("rejected admission created session")
	}
	for _, failure := range []string{"runtime", "disk", "output"} {
		t.Run(failure, func(t *testing.T) {
			limits := DefaultResourceLimits()
			limits.MaxRuntimeSeconds = 1
			m, err := NewManagerWithLimits(filepath.Join(t.TempDir(), "sessions"), limits)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			s, err := m.Start(childRequest(t, "sleep"))
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "runtime": // Exercise the real Manager monitor, not a synthetic timeout callback.
			case "disk":
				m.enforceResources(time.Now(), errors.Join(errResourceLimit, errors.New("insufficient free disk")))
			case "output":
				m.mu.Lock()
				out := m.sessions[s.ID].output
				m.mu.Unlock()
				out.mu.Lock()
				out.err = errors.New("simulated disk write failure")
				out.mu.Unlock()
				m.enforceResources(time.Now(), nil)
			}
			result := awaitSession(t, m, s.ID)
			if result.State != "failed" || result.Error == "" {
				t.Fatal("resource termination not recorded", result)
			}
			m.Close()
			recovered, err := NewManager(m.dir)
			if err != nil {
				t.Fatal(err)
			}
			defer recovered.Close()
			saved, err := recovered.Get(s.ID)
			if err != nil || saved.State != "failed" || saved.Error == "" {
				t.Fatal("resource failure not durable", saved, err)
			}
		})
	}
}
func TestRequestResourceErrorsAndHTTPAdmission(t *testing.T) {
	m := newTestManager(t)
	api := &Server{manager: m, token: "owner", requests: make(chan struct{}, 1), helpers: make(chan struct{}, 1)}
	api.requests <- struct{}{}
	w := deviceRequest(api, "owner", "GET", "/v1/status", "")
	if w.Code != 503 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), `"code":"unavailable"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	<-api.requests
	api.helpers <- struct{}{}
	if w := deviceRequest(api, "owner", "GET", "/v1/agent-history", ""); w.Code != 503 {
		t.Fatal("unbounded history helper admission", w.Code)
	}
	<-api.helpers
	w = deviceRequest(api, "owner", "GET", "/v1/status", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"resources"`) {
		t.Fatal(w.Body.String())
	}
}

func TestResourceErrorClassificationSurvivesReceiptReload(t *testing.T) {
	m := newTestManager(t)
	for range 2 {
		_, err := runOnce(m, "resource-request", "resource-test", true, func() (bool, error) { return false, errors.Join(errResourceLimit, errors.New("capacity exhausted")) })
		if !errors.Is(err, errResourceLimit) {
			t.Fatal("resource error lost category", err)
		}
	}
	m.Close()
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	_, err = runOnce(recovered, "resource-request", "resource-test", true, func() (bool, error) { t.Fatal("failed admission was replayed"); return true, nil })
	if !errors.Is(err, errResourceLimit) {
		t.Fatal("persisted category lost", err)
	}
}
func TestReadPressureCannotConsumeMutationReserve(t *testing.T) {
	m := newTestManager(t)
	api := &Server{manager: m, token: "owner", requests: make(chan struct{}, 1), controls: make(chan struct{}, 1)}
	api.requests <- struct{}{}
	stopped := false
	api.shutdown = func() { stopped = true }
	w := deviceRequest(api, "owner", "POST", "/v1/shutdown", "")
	if w.Code != 200 || !stopped {
		t.Fatal("reads blocked owner shutdown", w.Code)
	}
	api.controls <- struct{}{}
	if w := deviceRequest(api, "owner", "POST", "/v1/shutdown", ""); w.Code != 503 {
		t.Fatal("mutation admission unbounded", w.Code)
	}
}

func TestUncertainMutationIsDistinctFromTransientCapacity(t *testing.T) {
	w := httptest.NewRecorder()
	mutationFailure(w, errors.Join(errRequestUncertain, errors.New("receipt write failed")))
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"request_uncertain"`) || w.Header().Get("Retry-After") != "" {
		t.Fatal("uncertain result invites capacity retry", w.Code, w.Body.String())
	}
}
