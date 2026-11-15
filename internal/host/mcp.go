// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func mcpTools(write bool) []mcpTool {
	empty := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	id := map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}
	schema := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	tools := []mcpTool{
		{"host_capabilities", "Inspect optional Host capabilities and environment", empty},
		{"host_sessions", "List durable Host sessions", empty},
		{"host_output", "Read up to 64 KiB from a session byte offset", schema(map[string]any{"id": id, "offset": map[string]any{"type": "integer", "minimum": 0}}, "id")},
	}
	tools = append(tools, mcpTool{"host_capability_query", "Query plugin capabilities, results and event cursors through HCP 1.0", schema(map[string]any{"protocol": map[string]any{"type": "string", "enum": []string{"1.0"}}, "operation": map[string]any{"type": "string", "enum": []string{"capabilities.list", "capabilities.get", "capabilities.result", "capabilities.subscribe", "plugins.list"}}, "id": map[string]string{"type": "string"}, "epoch": map[string]string{"type": "string"}, "after": map[string]string{"type": "integer"}}, "protocol", "operation")})
	if write {
		tools = append(tools, mcpTool{"host_capability_invoke", "Invoke or cancel a plugin capability; sensitive calls require confirmation", schema(map[string]any{"protocol": map[string]any{"type": "string", "enum": []string{"1.0"}}, "operation": map[string]any{"type": "string", "enum": []string{"capabilities.invoke", "capabilities.cancel"}}, "id": map[string]string{"type": "string"}, "epoch": map[string]string{"type": "string"}, "request_id": map[string]string{"type": "string"}, "input": map[string]any{}, "confirmed": map[string]string{"type": "boolean"}}, "protocol", "operation", "id")})
		tools = append(tools,
			mcpTool{"host_start", "Start an authorized task, terminal or CLI agent", schema(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"task", "terminal", "agent"}}, "command": map[string]string{"type": "string"}, "args": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}, "directory": map[string]string{"type": "string"}, "agent": map[string]any{"type": "string", "enum": []string{"codex", "claude", "opencode"}}}, "kind", "directory")},
			mcpTool{"host_cancel", "Cancel a supervised session and its process tree", schema(map[string]any{"id": id}, "id")},
		)
	}
	return tools
}

// ServeMCP delegates JSON-RPC, lifecycle and protocol negotiation to the official
// SDK. Host retains tool authorization and authenticated loopback RPC only.
func ServeMCP(ctx context.Context, dir string, in io.Reader, out io.Writer, write bool) error {
	reader, ok := in.(io.ReadCloser)
	if !ok {
		reader = io.NopCloser(in)
	}
	return newMCPServer(write, func(ctx context.Context, req RPCRequest) ([]byte, error) {
		return Call(ctx, dir, req)
	}).Run(ctx, &mcp.IOTransport{Reader: reader, Writer: mcpWriter{out}, MaxLineLength: maxRequest})
}

type mcpWriter struct{ io.Writer }

func (mcpWriter) Close() error { return nil }

func newMCPServer(write bool, call func(context.Context, RPCRequest) ([]byte, error)) *mcp.Server {
	var ready sync.Map
	server := mcp.NewServer(&mcp.Implementation{Name: "nimotehost", Version: Version}, &mcp.ServerOptions{
		SupportedProtocolVersions: []string{"2025-11-25"},
		InitializedHandler:        func(_ context.Context, req *mcp.InitializedRequest) { ready.Store(req.Session, true) },
	})
	// SDK v1.8 does not yet enforce receipt of notifications/initialized for
	// tools. Preserve Host's existing gate without duplicating SDK negotiation.
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" || method == "tools/call" {
				if _, ok := ready.Load(req.GetSession()); !ok {
					return nil, &jsonrpc.Error{Code: -32000, Message: "initialize and notifications/initialized required"}
				}
			}
			return next(ctx, method, req)
		}
	})
	for _, tool := range mcpTools(write) {
		mcp.AddTool(server, &mcp.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema},
			func(ctx context.Context, req *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
				raw := req.Params.Arguments
				if len(raw) == 0 {
					raw = json.RawMessage(`{}`)
				}
				rpc, err := mcpRequest(tool.Name, raw)
				if err != nil {
					return nil, nil, err
				}
				data, err := call(ctx, rpc)
				result := &mcp.CallToolResult{}
				if err != nil {
					result.IsError = true
					data = []byte(err.Error())
				}
				result.Content = []mcp.Content{&mcp.TextContent{Text: string(data)}}
				return result, nil, nil
			})
	}
	return server
}
func mcpRequest(name string, args json.RawMessage) (RPCRequest, error) {
	switch name {
	case "host_capability_query", "host_capability_invoke":
		var request capabilityRequest
		if err := decodeJSON(args, &request); err != nil {
			return RPCRequest{}, err
		}
		allowed := request.Operation == "capabilities.list" || request.Operation == "capabilities.get" || request.Operation == "capabilities.result" || request.Operation == "capabilities.subscribe" || request.Operation == "plugins.list"
		if name == "host_capability_invoke" {
			allowed = request.Operation == "capabilities.invoke" || request.Operation == "capabilities.cancel"
		}
		if !allowed {
			return RPCRequest{}, fmt.Errorf("operation not permitted by this MCP tool")
		}
		return RPCRequest{Method: "POST", Path: "/v1/extensions", Body: args}, nil
	case "host_capabilities":
		return RPCRequest{Method: "GET", Path: "/v1/capabilities"}, nil
	case "host_sessions":
		return RPCRequest{Method: "GET", Path: "/v1/sessions"}, nil
	case "host_start":
		var start StartRequest
		if err := decodeJSON(args, &start); err != nil {
			return RPCRequest{}, err
		}
		return RPCRequest{Method: "POST", Path: "/v1/sessions", Body: args}, nil
	default:
		var params struct {
			ID     string `json:"id"`
			Offset int64  `json:"offset"`
		}
		if err := decodeJSON(args, &params); err != nil {
			return RPCRequest{}, err
		}
		if !validID(params.ID) || params.Offset < 0 {
			return RPCRequest{}, fmt.Errorf("invalid session ID or offset")
		}
		if name == "host_cancel" {
			return RPCRequest{Method: "POST", Path: "/v1/sessions/" + params.ID + "/cancel"}, nil
		}
		return RPCRequest{Method: "GET", Path: fmt.Sprintf("/v1/sessions/%s/output?offset=%d", params.ID, params.Offset)}, nil
	}
}
