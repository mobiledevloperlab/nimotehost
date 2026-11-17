// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/coder/websocket"
)

type codexAttachmentFixture struct {
	mu          sync.Mutex
	writeMu     sync.Mutex
	conn        *websocket.Conn
	directory   string
	pending     bool
	decisions   int
	resumes     int
	badOverride bool
}

func (f *codexAttachmentFixture) send(value any) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	if c != nil {
		data, _ := json.Marshal(value)
		_ = c.Write(context.Background(), websocket.MessageText, data)
	}
}
func (f *codexAttachmentFixture) serve(ctx context.Context, c *websocket.Conn) {
	f.mu.Lock()
	f.conn = c
	f.mu.Unlock()
	defer c.CloseNow()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
			Result map[string]any  `json:"result"`
		}
		if json.Unmarshal(data, &frame) != nil {
			return
		}
		if frame.Method == "" {
			if decision, ok := frame.Result["decision"]; ok {
				f.mu.Lock()
				f.decisions++
				if decision == "accept" || decision == "decline" {
					f.pending = false
				}
				f.mu.Unlock()
			}
			continue
		}
		if len(frame.ID) == 0 {
			continue
		}
		thread := map[string]any{"id": "original-thread", "cwd": f.directory, "status": map[string]string{"type": "active"}, "turns": []any{map[string]string{"id": "original-turn", "status": "inProgress"}}}
		var result any = map[string]any{}
		switch frame.Method {
		case "thread/read":
			delete(thread, "turns")
			result = map[string]any{"thread": thread}
		case "thread/resume":
			f.mu.Lock()
			f.resumes++
			if len(frame.Params) != 1 || frame.Params["threadId"] != "original-thread" {
				f.badOverride = true
			}
			f.mu.Unlock()
			result = map[string]any{"thread": thread}
		case "thread/items/list":
			result = map[string]any{"data": []any{}}
		case "thread/list":
			result = map[string]any{"data": []any{thread}}
		case "thread/start":
			f.mu.Lock()
			f.badOverride = true
			f.mu.Unlock()
		}
		f.send(map[string]any{"id": frame.ID, "result": result})
		if frame.Method == "thread/resume" {
			f.mu.Lock()
			pending := f.pending
			f.mu.Unlock()
			if pending {
				f.send(map[string]any{"id": "native-permission", "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "original-thread", "turnId": "original-turn", "command": "pwd", "availableDecisions": []string{"accept", "decline"}}})
			}
		}
	}
}

func newCodexAttachmentFixture(t *testing.T) (*codexAttachmentFixture, ExternalAttachment) {
	t.Helper()
	// Unix socket paths have a small kernel limit; use a short, owned temp dir.
	root, err := os.MkdirTemp("/private/tmp", "nimote-attach-")
	if err != nil {
		root, err = os.MkdirTemp("", "na-")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	path := filepath.Join(root, "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	f := &codexAttachmentFixture{directory: t.TempDir(), pending: true}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		f.serve(r.Context(), c)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	// Existing socket control must not need or launch any Agent executable.
	t.Setenv("PATH", t.TempDir())
	return f, ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "codex-control", Endpoint: path}}
}

func TestCodexAttachExistingServerRestoresPendingAndDesktopResolution(t *testing.T) {
	f, c := newCodexAttachmentFixture(t)
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: f.directory, RequestID: "attach", Resume: &AgentResume{ConversationID: "original-thread"}, Attach: &c})
	if err != nil {
		t.Fatal(err)
	}
	var permissionID string
	waitExternalCondition(t, func() bool {
		snap, e := m.AgentSnapshot(s.ID)
		if e != nil {
			return false
		}
		pending := snap["approvals"].([]any)
		if len(pending) != 1 {
			return false
		}
		permissionID = fmt.Sprint(pending[0].(map[string]any)["id"])
		return snap["ready"] == true && snap["status"] == "needs_approval" && snap["turn_id"] == "original-turn"
	})
	if permissionID != `"native-permission"` {
		t.Fatal("native approval identity lost", permissionID)
	}
	// A desktop subscriber resolves the original request first.
	f.mu.Lock()
	f.pending = false
	f.mu.Unlock()
	f.send(map[string]any{"method": "serverRequest/resolved", "params": map[string]string{"threadId": "original-thread", "requestId": "native-permission"}})
	waitExternalCondition(t, func() bool { snap, _ := m.AgentSnapshot(s.ID); return len(snap["approvals"].([]any)) == 0 })
	if err := m.AgentMessage(s.ID, AgentMessage{Action: "approval", RequestID: "stale-mobile", ApprovalID: permissionID, Decision: "accept"}); err == nil {
		t.Fatal("stale mobile approval accepted")
	}
	m.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.badOverride || f.decisions != 0 || f.resumes != 1 {
		t.Fatal("attachment changed safety config or repeated native decision", f.badOverride, f.decisions, f.resumes)
	}
}

func TestCodexAttachShutdownDoesNotDeclineNativePending(t *testing.T) {
	f, c := newCodexAttachmentFixture(t)
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: f.directory, RequestID: "attach", Resume: &AgentResume{ConversationID: "original-thread"}, Attach: &c})
	if err != nil {
		t.Fatal(err)
	}
	waitExternalCondition(t, func() bool { snap, _ := m.AgentSnapshot(s.ID); return len(snap["approvals"].([]any)) == 1 })
	m.Close()
	f.mu.Lock()
	pending, decisions := f.pending, f.decisions
	f.mu.Unlock()
	if !pending || decisions != 0 {
		t.Fatal("Host shutdown declined external native permission", pending, decisions)
	}
	// The original service still accepts a new independent attachment.
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	second, err := recovered.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: f.directory, RequestID: "reattach", Resume: &AgentResume{ConversationID: "original-thread"}, Attach: &c})
	if err != nil {
		t.Fatal(err)
	}
	if second.ConversationID != s.ConversationID || second.ID == s.ID {
		t.Fatal("original native session replaced")
	}
	waitExternalCondition(t, func() bool { snap, _ := recovered.AgentSnapshot(second.ID); return len(snap["approvals"].([]any)) == 1 })
}

func TestCodexSocketAliasOwnershipIsReadOnly(t *testing.T) {
	_, connection := newCodexAttachmentFixture(t)
	parent := t.TempDir()
	alias := filepath.Join(parent, "alias.sock")
	if err := os.Symlink(connection.Endpoint, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0755); err != nil {
		t.Fatal(err)
	}
	connection.Endpoint = alias
	socket, err := dialCodexControl(t.Context(), connection)
	if err != nil {
		t.Fatal("official owner-controlled 0755 alias rejected", err)
	}
	socket.Close()
	info, err := os.Stat(parent)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("provider directory permissions changed", err)
	}
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	if socket, err := dialCodexControl(t.Context(), connection); err == nil {
		socket.Close()
		t.Fatal("world-writable alias accepted")
	}
	info, _ = os.Stat(parent)
	if info.Mode().Perm() != 0777 {
		t.Fatal("unsafe directory was silently modified")
	}
}
