// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCodexHistoryPageFitsRPCWithEscapedTextAndNestedTools(t *testing.T) {
	rows := make([]any, maxHistoryPage)
	for i := range rows {
		rows[i] = map[string]any{"item": map[string]any{
			"id": "item", "type": "agentMessage", "text": strings.Repeat("\x00界", 8<<10),
			"result": map[string]any{"nested": strings.Repeat("large", 4<<10)},
		}}
	}
	raw, _ := json.Marshal(map[string]any{"data": rows, "nextCursor": "next-page"})
	page, err := projectCodexHistory(raw)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(page)
	if err != nil || len(wire) >= 2<<20 {
		t.Fatalf("response bytes=%d err=%v", len(wire), err)
	}
	items := page["items"].([]map[string]any)
	if len(items) != maxHistoryPage || page["next_cursor"] != encodeCodexCursor("next-page") {
		t.Fatal("pagination was lost")
	}
	for _, item := range items {
		if item["truncated"] != true || !utf8.ValidString(item["text"].(string)) {
			t.Fatal("omitted content was not disclosed, or text was split inside a rune")
		}
	}
}

func TestHistoryTranscriptReportsCountAndByteTruncation(t *testing.T) {
	for _, large := range []bool{false, true} {
		h := historyTranscript{Items: []map[string]any{}}
		text := "small"
		if large {
			text = strings.Repeat("x", 20<<10)
		}
		for range maxHistoryUpdates + 5 {
			h.append(map[string]any{"id": "item", "kind": "agentMessage", "role": "assistant", "text": text})
		}
		if !h.Truncated || len(h.Items) == 0 {
			t.Fatal("missing truncation marker")
		}
		if large && len(h.Items) >= maxHistoryUpdates {
			t.Fatal("aggregate byte budget was ignored")
		}
		wire, err := json.Marshal(h)
		if err != nil || len(wire) >= 2<<20 {
			t.Fatalf("response bytes=%d err=%v", len(wire), err)
		}
	}
}

func TestBoundHistoryItemKeepsSmallToolDetails(t *testing.T) {
	item := boundHistoryItem(map[string]any{"id": "cmd", "kind": "commandExecution", "role": "tool", "text": "pwd", "tool": map[string]any{"command": "pwd", "output": "/workspace"}})
	if item["truncated"] != nil || item["tool"].(map[string]any)["output"] != "/workspace" {
		t.Fatal(item)
	}
	for _, n := range []int{0, 1, 2, 3, 4, 5} {
		if text := boundedHistoryText("中文字符", n); !utf8.ValidString(text) {
			t.Fatalf("limit %d split UTF-8", n)
		}
	}
}
