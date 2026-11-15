// SPDX-License-Identifier: Apache-2.0
// External test fixture: only public SDKs, no Host internals.
package main

import (
	"context"
	"encoding/json"
	acp "github.com/coder/acp-go-sdk"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
	"os"
	"time"
)

type agent struct {
	acp.Agent
	conn *acp.AgentSideConnection
}

func (a *agent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: 1}, nil
}
func (a *agent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	return acp.NewSessionResponse{SessionId: "community-session"}, nil
}
func (a *agent) Cancel(context.Context, acp.CancelNotification) error { return nil }
func (a *agent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	_ = a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: p.SessionId, Update: acp.UpdateAgentMessageText("community ACP response")})
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--acp" {
		a := &agent{}
		a.conn = acp.NewAgentSideConnection(a, os.Stdout, os.Stdin)
		<-a.conn.Done()
		return
	}
	_ = sdk.Serve(context.Background(), os.Stdin, os.Stdout, func(ctx context.Context, name string, raw json.RawMessage, progress func(float64)) (any, error) {
		var input map[string]any
		_ = json.Unmarshal(raw, &input)
		if input["crash"] == true {
			os.Exit(7)
		}
		if input["wait"] == true {
			select {
			case <-ctx.Done():
			case <-time.After(time.Minute):
			}
			return nil, ctx.Err()
		}
		return input, nil
	})
}
