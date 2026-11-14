// SPDX-License-Identifier: Apache-2.0
package host

import (
	"os"
	"testing"
	"time"
)

// Explicitly enabled by the upstream monitor. Exercise the production startup
// path through session creation, without sending a prompt or accepting fallback.
// The caller supplies temporary CLI installations and an empty HOME/config.
func TestInstalledAgentCompatibility(t *testing.T) {
	provider := os.Getenv("NIMOTE_TEST_COMPAT_AGENT")
	if provider == "" {
		t.Skip("set NIMOTE_TEST_COMPAT_AGENT for a no-model installed CLI check")
	}
	if !supportsStructuredAgent(provider) {
		t.Fatal("unsupported compatibility provider")
	}
	m := newTestManager(t)
	session, err := m.Start(StartRequest{Kind: "agent", Agent: provider, Interaction: "structured", Directory: t.TempDir()})
	if err != nil {
		t.Fatal("installed CLI could not start:", err)
	}
	m.mu.Lock()
	item := m.sessions[session.ID]
	m.mu.Unlock()
	select {
	case err := <-item.agent.startup:
		if err != nil {
			t.Fatal("installed CLI initialization failed:", err)
		}
	case <-time.After(agentStartupTimeout):
		t.Fatal("installed CLI initialization timed out")
	}
	if err := m.Cancel(session.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID)
}
