// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"strings"
	"testing"
)

const testModelCatalog = `{"data":[{"model":"model-a","displayName":"Model A","isDefault":true,"defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"low"},{"reasoningEffort":"medium"},{"reasoningEffort":"high"}]},{"model":"model-b","defaultReasoningEffort":"low","supportedReasoningEfforts":[{"reasoningEffort":"low"}]}]}`

func TestCodexModelEffortControlsReachTurns(t *testing.T) {
	a, _ := testAgent()
	a.thread = "thread"
	a.model = "model-a"
	if err := a.message(AgentMessage{Action: "discover", RequestID: "discover"}); err != nil {
		t.Fatal(err)
	}
	if f := queuedFrame(t, a); f["method"] != "model/list" {
		t.Fatal(f)
	}
	receiveAgentFrameForTest(a, []byte(`{"id":"host-models","result":`+testModelCatalog+`}`))
	if len(a.controls.Options) != 3 || !a.controls.Codex {
		t.Fatal(a.controls)
	}
	if err := a.message(AgentMessage{Action: "configure", RequestID: "effort", OptionID: "reasoning", Value: "high"}); err != nil {
		t.Fatal(err)
	}
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "turn", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	params := queuedFrame(t, a)["params"].(map[string]any)
	if params["model"] != "model-a" || params["effort"] != "high" {
		t.Fatal(params)
	}
	if err := a.message(AgentMessage{Action: "configure", RequestID: "busy", OptionID: "model", Value: "model-b"}); err == nil {
		t.Fatal("changed model during turn")
	}
	receiveAgentFrameForTest(a, []byte(`{"method":"turn/completed","params":{}}`))
	if err := a.message(AgentMessage{Action: "configure", RequestID: "model", OptionID: "model", Value: "model-b"}); err != nil {
		t.Fatal(err)
	}
	if a.effort != "low" {
		t.Fatal("incompatible effort carried to new model", a.effort)
	}
	if err := a.message(AgentMessage{Action: "configure", RequestID: "invalid", OptionID: "reasoning", Value: "high"}); err == nil {
		t.Fatal("unsupported effort accepted")
	}
	if err := a.message(AgentMessage{Action: "configure", RequestID: "effort", OptionID: "reasoning", Value: "high"}); err != nil {
		t.Fatal("retry rejected", err)
	}
	if a.effort != "low" {
		t.Fatal("late retry mutated later selection")
	}
}
func TestCodexCompactIsRPCAndReleasesBusyOnFailure(t *testing.T) {
	a, _ := testAgent()
	a.thread = "thread"
	req := AgentMessage{Action: "compact", RequestID: "compact"}
	if err := a.message(req); err != nil {
		t.Fatal(err)
	}
	f := queuedFrame(t, a)
	if f["method"] != "thread/compact/start" || !a.busy {
		t.Fatal(f)
	}
	if err := a.message(req); err != nil || queuedAgentOperations(a) != 0 {
		t.Fatal("duplicate compaction", err)
	}
	response, _ := json.Marshal(map[string]any{"id": f["id"], "error": map[string]any{"message": "failed"}})
	receiveAgentFrameForTest(a, response)
	if a.busy {
		t.Fatal("failed compact left session busy")
	}
}
func TestACPDiscoversCommandsBeforeReadyAndConfiguresProviderIDs(t *testing.T) {
	a, out := testACP()
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"acp-session","update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"review","description":"Review workspace"}]}}}`))
	readyACP(t, a)
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"acp-session","update":{"sessionUpdate":"config_option_update","configOptions":[{"id":"provider-model","name":"Model","category":"model","currentValue":"one","options":[{"value":"one","name":"One"},{"value":"two","name":"Two"}]}]}}}`))
	if len(a.controls.Commands) != 1 || a.controls.Commands[0].Name != "review" {
		t.Fatal(a.controls)
	}
	req := AgentMessage{Action: "configure", RequestID: "select", OptionID: "provider-model", Value: "two"}
	if err := a.message(req); err != nil {
		t.Fatal(err)
	}
	frame := queuedFrame(t, a)
	p := frame["params"].(map[string]any)
	if frame["method"] != "session/set_config_option" || p["configId"] != "provider-model" || p["value"] != "two" {
		t.Fatal(frame)
	}
	if a.controls.Options[0].Value != "one" {
		t.Fatal("selection changed before acknowledgment")
	}
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "race", Text: "hello"}); err == nil {
		t.Fatal("prompt raced pending config")
	}
	response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": frame["id"], "result": map[string]any{}})
	receiveAgentFrameForTest(a, response)
	if a.configuring || a.controls.Options[0].Value != "two" {
		t.Fatal(a.controls)
	}
	if !strings.Contains(out.String(), `"type":"controls"`) {
		t.Fatal(out.String())
	}
	if err := a.message(req); err != nil || queuedAgentOperations(a) != 0 {
		t.Fatal("selection retry resent RPC", err)
	}
}
func TestACPModelFailureKeepsSelection(t *testing.T) {
	a, _ := testACP()
	readyACP(t, a)
	a.configACP(json.RawMessage(`{"models":{"currentModelId":"one","availableModels":[{"modelId":"one","name":"One"},{"modelId":"two","name":"Two"}]}}`))
	if err := a.message(AgentMessage{Action: "configure", RequestID: "model", OptionID: "__model", Value: "two"}); err != nil {
		t.Fatal(err)
	}
	f := queuedFrame(t, a)
	if f["method"] != "session/set_model" {
		t.Fatal(f)
	}
	response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": f["id"], "error": map[string]any{"message": "unavailable"}})
	receiveAgentFrameForTest(a, response)
	if a.configuring || a.controls.Options[0].Value != "one" {
		t.Fatal(a.controls)
	}
}

func TestCodexCatalogRefreshPreservesSelectionsAndDeduplicatesInFlight(t *testing.T) {
	a, _ := testAgent()
	a.thread = "thread"
	a.model = "model-a"
	a.receiveModels(json.RawMessage(testModelCatalog))
	if err := a.message(AgentMessage{Action: "discover", RequestID: "refresh"}); err != nil {
		t.Fatal(err)
	}
	_ = queuedFrame(t, a)
	if err := a.message(AgentMessage{Action: "discover", RequestID: "refresh-again"}); err != nil || queuedAgentOperations(a) != 0 {
		t.Fatal("concurrent catalog requests", err)
	}
	receiveAgentFrameForTest(a, []byte(`{"id":"host-models","error":{"message":"offline"}}`))
	if a.loadingModels || len(a.models) != 2 {
		t.Fatal("failed refresh discarded working catalog")
	}
	if err := a.configureCodex("reasoning", "high"); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticCompactionDoesNotReleaseActiveTurn(t *testing.T) {
	a, _ := testAgent()
	a.thread = "thread"
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "prompt", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	_ = queuedFrame(t, a)
	receiveAgentFrameForTest(a, []byte(`{"method":"turn/started","params":{"turn":{"id":"turn"}}}`))
	receiveAgentFrameForTest(a, []byte(`{"method":"thread/compacted","params":{}}`))
	if !a.busy || a.turn != "turn" {
		t.Fatal("auto-compaction released active turn")
	}
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "overlap", Text: "hello"}); err == nil {
		t.Fatal("overlapping prompt accepted")
	}
}
