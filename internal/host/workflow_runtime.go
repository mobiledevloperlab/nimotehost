// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// WorkflowRuntime lets linked orchestration use the live Host manager and its
// durable ledger while keeping the session and process internals private.
type WorkflowRuntime struct {
	*Manager
}

func NewWorkflowRuntime(manager *Manager) *WorkflowRuntime { return &WorkflowRuntime{manager} }
func (m *WorkflowRuntime) SessionDir() string              { return m.dir }
func (m *WorkflowRuntime) Context() context.Context        { return m.background }
func (m *WorkflowRuntime) Stopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// BeginMutation keeps idle shutdown from interrupting a synchronous workflow
// mutation before its asynchronous worker has been registered.
func (m *WorkflowRuntime) BeginMutation() (func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, false
	}
	m.workflowRequests++
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.workflowRequests--
	}, true
}

func (m *WorkflowRuntime) Running(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.workflowRuns[id]
}
func (m *WorkflowRuntime) Launch(id string, run func()) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.workflowRuns[id] {
		return false
	}
	if m.workflowRuns == nil {
		m.workflowRuns = make(map[string]bool)
	}
	m.workflowRuns[id] = true
	return m.startBackgroundLocked(func() {
		defer func() {
			m.mu.Lock()
			delete(m.workflowRuns, id)
			m.mu.Unlock()
		}()
		run()
	})
}

func (m *WorkflowRuntime) Start(req StartRequest, baseCommit string) (Session, error) {
	req.baseCommit = baseCommit
	return m.Manager.Start(req)
}

func (m *WorkflowRuntime) ReadReceipt(id string) (requestRecord, error) {
	key := sha256.Sum256([]byte(id))
	var record requestRecord
	m.requests.mu.Lock()
	defer m.requests.mu.Unlock()
	data, err := readPrivateLimit(filepath.Join(m.requests.dir, hex.EncodeToString(key[:])), maxReceipt)
	if err != nil {
		return record, err
	}
	if err := decodeJSON(data, &record); err != nil {
		return record, errors.New("invalid prompt receipt; inspect retained conversation")
	}
	return record, nil
}

func (m *WorkflowRuntime) SaveVerification(id string, report VerificationReport) error {
	if !validID(id) {
		return os.ErrNotExist
	}
	lock := m.verificationLock(id)
	lock.Lock()
	defer lock.Unlock()
	return saveJSON(filepath.Join(m.dir, id+".verification"), report)
}

func (m *WorkflowRuntime) MarkVerificationRemoteStale(id, reason string) error {
	if !validID(id) {
		return os.ErrNotExist
	}
	lock := m.verificationLock(id)
	lock.Lock()
	defer lock.Unlock()
	path := filepath.Join(m.dir, id+".verification")
	data, err := readPrivateLimit(path, maxEvidenceBytes)
	if err != nil {
		return nil
	}
	var report VerificationReport
	if decodeJSON(data, &report) != nil {
		return nil
	}
	report.RemoteStale = true
	report.Stale = true
	report.Unknown = append(report.Unknown, reason)
	return saveJSON(path, report)
}

func (m *WorkflowRuntime) WaitAgentReady(id string) error {
	m.mu.Lock()
	item := m.sessions[id]
	m.mu.Unlock()
	stopStartup := func(reason string) error {
		if item != nil && item.agent != nil {
			item.agent.fail(errors.New(reason))
			select {
			case <-item.done:
			case <-m.background.Done():
			}
		}
		return errors.New(reason)
	}
	for deadline := time.Now().Add(agentCreateResponseTimeout); time.Now().Before(deadline); {
		if m.Stopped() {
			return errors.New("Host stopped; continue this task after restart")
		}
		if item != nil && item.agent != nil {
			select {
			case startupErr := <-item.agent.startup:
				if startupErr != nil {
					return stopStartup("Agent protocol startup rejected; inspect retained Session diagnostics")
				}
			default:
			}
		}
		state, e := m.AgentSnapshot(id)
		if e != nil {
			return e
		}
		if state["ready"] == true {
			return nil
		}
		if state["status"] == "disconnected" {
			return errors.New("Agent disconnected before startup completed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return stopStartup("Agent startup timed out; retained Session can be inspected")
}

// RunOnce uses the same receipt store and request fingerprint as core RPC.
func RunOnce[T any](m *WorkflowRuntime, id, operation string, payload any, run func() (T, error)) (T, error) {
	return runOnce(m.Manager, id, operation, payload, run)
}

type MutationReceipt = mutationReceipt

var ErrRequestConflict = errRequestConflict
var ErrRequestUncertain = errRequestUncertain

const StructuredAgentSupported = structuredAgentSupported

func ValidID(id string) bool                                          { return validID(id) }
func SupportsStructuredAgent(agent string) bool                       { return supportsStructuredAgent(agent) }
func ValidExecutionPolicy(policy string) bool                         { return validExecutionPolicy(policy) }
func SameWorkspaceDirectory(a, b string) bool                         { return sameWorkspaceDirectory(a, b) }
func CanonicalDirectory(path string) string                           { return canonicalDirectory(path) }
func PrivateDir(path string) error                                    { return privateDir(path) }
func AtomicWrite(path string, data []byte) error                      { return atomicWrite(path, data) }
func ReadPrivateLimit(path string, limit int64) ([]byte, error)       { return readPrivateLimit(path, limit) }
func DecodeJSON(data []byte, value any) error                         { return decodeJSON(data, value) }
func Respond(w http.ResponseWriter, status int, value any)            { respond(w, status, value) }
func Fail(w http.ResponseWriter, status int, message string)          { fail(w, status, message) }
func ReadJSON(w http.ResponseWriter, r *http.Request, value any) bool { return readJSON(w, r, value) }
func MutationFailure(w http.ResponseWriter, err error)                { mutationFailure(w, err) }
func GitEvidence(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return gitEvidence(ctx, dir, args...)
}
func RunEvidenceProcess(ctx context.Context, cmd *exec.Cmd, input io.Reader) ([]byte, error) {
	return runEvidenceProcess(ctx, cmd, input)
}
func CaptureCode(ctx context.Context, dir string) (CodeVersion, string) { return captureCode(ctx, dir) }
