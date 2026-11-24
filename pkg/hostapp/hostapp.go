// SPDX-License-Identifier: Apache-2.0

// Package hostapp links the Host CLI and one optional first-party workflow into
// a distribution. Out-of-process integrations use hostclient or pluginsdk.
package hostapp

import (
	"context"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"github.com/mobiledevloperlab/nimotehost/internal/hostcli"
	"io"
	"net/http"
	"os/exec"
)

type (
	Extension          = host.Extension
	ScopedHandler      = host.ScopedHandler
	WorkflowRuntime    = host.WorkflowRuntime
	StartRequest       = host.StartRequest
	TaskReference      = host.TaskReference
	WorktreeRequest    = host.WorktreeRequest
	AgentResume        = host.AgentResume
	AgentMessage       = host.AgentMessage
	TestCommand        = host.TestCommand
	VerificationReport = host.VerificationReport
	MutationReceipt    = host.MutationReceipt
)

const StructuredAgentSupported = host.StructuredAgentSupported

func Main(extension Extension) { hostcli.Main(extension) }

// OpenWorkflowRuntime opens retained state for a standalone workflow fixture.
// Services use the live runtime provided to Extension.NewHandler.
func OpenWorkflowRuntime(sessionDir string) (*WorkflowRuntime, error) {
	m, err := host.NewManager(sessionDir)
	if err != nil {
		return nil, err
	}
	return host.NewWorkflowRuntime(m), nil
}

var ErrRequestConflict = host.ErrRequestConflict
var ErrRequestUncertain = host.ErrRequestUncertain

func ValidID(id string) bool                     { return host.ValidID(id) }
func SupportsStructuredAgent(agent string) bool  { return host.SupportsStructuredAgent(agent) }
func ValidExecutionPolicy(policy string) bool    { return host.ValidExecutionPolicy(policy) }
func SameWorkspaceDirectory(a, b string) bool    { return host.SameWorkspaceDirectory(a, b) }
func CanonicalDirectory(path string) string      { return host.CanonicalDirectory(path) }
func PrivateDir(path string) error               { return host.PrivateDir(path) }
func AtomicWrite(path string, data []byte) error { return host.AtomicWrite(path, data) }
func ReadPrivateLimit(path string, limit int64) ([]byte, error) {
	return host.ReadPrivateLimit(path, limit)
}
func DecodeJSON(data []byte, value any) error                { return host.DecodeJSON(data, value) }
func Respond(w http.ResponseWriter, status int, value any)   { host.Respond(w, status, value) }
func Fail(w http.ResponseWriter, status int, message string) { host.Fail(w, status, message) }
func ReadJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	return host.ReadJSON(w, r, value)
}
func MutationFailure(w http.ResponseWriter, err error) { host.MutationFailure(w, err) }
func GitEvidence(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return host.GitEvidence(ctx, dir, args...)
}
func RunEvidenceProcess(ctx context.Context, cmd *exec.Cmd, input io.Reader) ([]byte, error) {
	return host.RunEvidenceProcess(ctx, cmd, input)
}
func CaptureCode(ctx context.Context, dir string) (host.CodeVersion, string) {
	return host.CaptureCode(ctx, dir)
}
func RunOnce[T any](m *WorkflowRuntime, id, operation string, payload any, run func() (T, error)) (T, error) {
	return host.RunOnce(m, id, operation, payload, run)
}
