// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"fmt"
)

// These methods run under the agent mutex, using the active provider process.
// History is a passive projection: it must not create turns or approvals.
func (a *structuredAgent) requestCodexHistory(cursor string) {
	if a.historyCursors[cursor] {
		err := errors.New("Codex history returned a repeated cursor")
		a.completeStartup(err)
		a.emit("error", err.Error())
		return
	}
	a.historyCursors[cursor] = true
	params := map[string]any{"threadId": a.historyThread, "limit": 50, "sortDirection": "asc"}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if err := a.send(map[string]any{"id": "host-history", "method": "thread/items/list", "params": params}); err != nil {
		a.completeStartup(err)
		a.emit("error", err.Error())
	}
}

func (a *structuredAgent) receiveCodexHistory(raw json.RawMessage) {
	if a.historyThread == "" {
		return
	}
	page, err := projectCodexHistory(raw)
	if err != nil {
		a.completeStartup(fmt.Errorf("decode Codex history: %w", err))
		a.emit("error", "Invalid Codex history response")
		return
	}
	// Keep individual log records small enough for bounded client decoders.
	for _, item := range page["items"].([]map[string]any) {
		a.emit("history", []map[string]any{item})
	}
	if cursor, ok := page["next_cursor"].(string); ok && cursor != "" {
		nativeCursor, err := decodeCodexCursor(cursor)
		if err != nil {
			a.completeStartup(err)
			return
		}
		a.requestCodexHistory(nativeCursor)
		return
	}
	a.thread = a.historyThread
	a.historyThread = ""
	a.historyCursors = nil
	if a.onConversationID != nil {
		a.onConversationID(a.thread)
	}
	a.publishControls()
	a.emit("ready", map[string]string{"thread_id": a.thread})
	a.completeStartup(nil)
}
