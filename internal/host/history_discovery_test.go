// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"strings"
	"testing"
)

func TestHistoryDiscoveryDistinguishesInstallationAndAttach(t *testing.T) {
	p, e := aggregateHistory(context.Background(), "", "", map[string]AgentInstallation{})
	if e != nil || len(p.Items) != 0 || p.LiveAttach || p.LiveAttachReason == "" || p.Providers["claude"] != "adapter_not_installed" {
		t.Fatal(p, e)
	}
	if _, e = aggregateHistory(context.Background(), "", "!invalid", nil); e == nil {
		t.Fatal("invalid cursor accepted")
	}
	for _, a := range installedAgents() {
		if a.LiveAttach || a.LiveAttachReason == "" || a.History == "" || a.Resume == "" {
			t.Fatal(a)
		}
	}
}
func TestResumeReusesActiveConversationWithoutSendingPrompt(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	id := strings.Repeat("a", 64)
	m.sessions[id] = &managed{session: Session{ID: id, Revision: 1, Kind: "agent", Agent: "codex", Interaction: "structured", Directory: dir, ConversationID: "existing", State: "running"}, activity: &activityJournal{}}
	req := StartRequest{Kind: "agent", Agent: "codex", Interaction: "structured", Directory: dir, Resume: &AgentResume{ConversationID: "existing"}, RequestID: "first-resume"}
	s, e := m.Start(req)
	if e != nil || s.ID != id || len(m.List(0)) != 1 {
		t.Fatal(s, e)
	}
	req.RequestID = "second-resume"
	s, e = m.Start(req)
	if e != nil || s.ID != id || len(m.List(0)) != 1 {
		t.Fatal(s, e)
	}
	req.RequestID = "wrong-workspace"
	req.Directory = t.TempDir()
	if _, e = m.Start(req); e == nil {
		t.Fatal("active conversation rebound to another workspace")
	}
}
