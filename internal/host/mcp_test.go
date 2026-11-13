// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPSDKRequiresInitializedNotification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	peer, host := net.Pipe()
	defer peer.Close()
	server := newMCPServer(false, func(context.Context, RPCRequest) ([]byte, error) {
		t.Error("lifecycle probe dispatched a tool")
		return nil, nil
	})
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, &mcp.IOTransport{Reader: host, Writer: host, MaxLineLength: maxRequest})
	}()
	defer func() { _ = peer.Close(); <-done }()
	encoder, decoder := json.NewEncoder(peer), json.NewDecoder(peer)
	call := func(req string) map[string]json.RawMessage {
		t.Helper()
		if err := encoder.Encode(json.RawMessage(req)); err != nil {
			t.Fatal(err)
		}
		var reply map[string]json.RawMessage
		if err := decoder.Decode(&reply); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	if call(`{"jsonrpc":"2.0","id":0,"method":"tools/list"}`)["error"] == nil {
		t.Fatal("tools available before initialization")
	}
	if call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)["error"] != nil {
		t.Fatal("initialize failed")
	}
	if call(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)["error"] == nil {
		t.Fatal("tools available before initialized notification")
	}
	if err := encoder.Encode(json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		t.Fatal(err)
	}
	if call(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)["result"] == nil {
		t.Fatal("initialized tools unavailable")
	}
}

func TestMCPReadOnlyAndLifecycle(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-only", true: "write"}[write], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var calls atomic.Int32
			server := newMCPServer(write, func(_ context.Context, req RPCRequest) ([]byte, error) {
				calls.Add(1)
				switch req.Path {
				case "/v1/sessions":
					return []byte("[]"), nil
				case "/v1/capabilities":
					return nil, errors.New("Host is unavailable")
				default:
					t.Errorf("unexpected RPC: %+v", req)
					return nil, errors.New("unexpected RPC")
				}
			})
			ct, st := mcp.NewInMemoryTransports()
			ss, err := server.Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			tools, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			expected := 3
			if write {
				expected = 5
			}
			if len(tools.Tools) != expected {
				t.Fatalf("got %d tools, want %d", len(tools.Tools), expected)
			}
			if !write {
				if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "host_start", Arguments: map[string]any{}}); err == nil {
					t.Fatal("disabled tool accepted")
				}
			}
			result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "host_sessions", Arguments: map[string]any{}})
			if err != nil || result.IsError || result.Content[0].(*mcp.TextContent).Text != "[]" {
				t.Fatal(result, err)
			}
			result, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "host_capabilities", Arguments: map[string]any{}})
			if err != nil || !result.IsError {
				t.Fatal("RPC error not reported as tool error", result, err)
			}
			if calls.Load() != 2 {
				t.Fatal(calls.Load())
			}
		})
	}
}
func TestDurableEventsAndDeletion(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID)
	page := m.Events(0)
	if len(page.Events) != 2 || page.Events[0].State != "running" || page.Events[1].State != "succeeded" {
		t.Fatalf("%+v", page)
	}
	if err := m.Delete(s.ID); err != nil {
		t.Fatal(err)
	}
	next := m.Events(page.Cursor)
	if len(next.Events) != 1 || next.Events[0].State != "deleted" {
		t.Fatalf("%+v", next)
	}
	m.Close()
	restored, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if len(restored.Events(0).Events) != 3 {
		t.Fatal("events not persisted")
	}
}
