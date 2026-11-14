// SPDX-License-Identifier: Apache-2.0
package host

import "encoding/json"

// Budget encoded JSON, including escaping and nested tool fields, rather than
// just source text. Fifty items stay comfortably below the 2 MiB RPC limit.
const maxHistoryItemBytes = 24 << 10
const maxHistoryTranscriptBytes = 1 << 20
const maxHistoryUpdates = 2000

func boundHistoryItem(item map[string]any) map[string]any {
	encoded, _ := json.Marshal(item)
	if len(encoded) <= maxHistoryItemBytes {
		return item
	}
	item["truncated"] = true
	// Opaque provider structures cannot be truncated safely inside their schema.
	// Preserve the readable text and explicitly mark omitted tool details.
	item["tool"] = map[string]any{"truncated": true}
	for _, key := range []string{"id", "kind"} {
		value, _ := item[key].(string)
		item[key] = boundedHistoryText(value, 256)
	}
	text, _ := item["text"].(string)
	for limit := min(len(text), 8<<10); ; limit /= 2 {
		item["text"] = boundedHistoryText(text, limit)
		encoded, _ = json.Marshal(item)
		if len(encoded) <= maxHistoryItemBytes {
			return item
		}
	}
}

// ACP replays history as notifications, without a transcript cursor. Continue
// draining the SDK stream after this budget fills, but disclose every omission.
type historyTranscript struct {
	Items     []map[string]any `json:"items"`
	Truncated bool             `json:"truncated"`
	bytes     int
}

func (h *historyTranscript) append(item map[string]any) {
	if h.Truncated {
		return
	}
	item = boundHistoryItem(item)
	encoded, _ := json.Marshal(item)
	if len(h.Items) >= maxHistoryUpdates || h.bytes+len(encoded)+1 > maxHistoryTranscriptBytes {
		h.Truncated = true
		return
	}
	h.bytes += len(encoded) + 1
	h.Items = append(h.Items, item)
}
