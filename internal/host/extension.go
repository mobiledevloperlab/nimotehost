// SPDX-License-Identifier: Apache-2.0
package host

import "net/http"

// ScopedHandler runs after Host authentication, protocol and request limits.
// A non-nil session predicate limits reads to the caller's authorized sessions.
// Only the owner can call mutation methods.
type ScopedHandler func(http.ResponseWriter, *http.Request, func(string) bool)

// Extension registers a workflow handler and its negotiated feature flags.
type Extension struct {
	Path       string
	NewHandler func(*WorkflowRuntime) ScopedHandler
	Features   func(map[string]string) map[string]bool
}

func (s *Server) registerExtension(extension Extension) {
	if extension.NewHandler != nil && extension.Path != "" {
		s.extension = extension
		s.extensionHandler = extension.NewHandler(NewWorkflowRuntime(s.manager))
	}
}
