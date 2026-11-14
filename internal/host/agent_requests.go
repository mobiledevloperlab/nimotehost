// SPDX-License-Identifier: Apache-2.0
package host

import (
	"crypto/sha256"
	"encoding/json"
)

func agentMessageFingerprint(req AgentMessage) [32]byte {
	encoded, _ := json.Marshal(req) // AgentMessage contains only strings.
	return sha256.Sum256(encoded)
}

// Caller holds a.mu. The durable HTTP ledger remains authoritative across
// restarts; this protects direct Core callers and in-process control retries too.
func (a *structuredAgent) replayedMessage(req AgentMessage) (bool, error) {
	if !a.seen[req.RequestID] {
		return false, nil
	}
	previous, ok := a.messageHashes[req.RequestID]
	if !ok {
		return true, errRequestUncertain
	}
	if previous != agentMessageFingerprint(req) {
		return true, errRequestConflict
	}
	return true, nil
}
func (a *structuredAgent) rememberMessage(req AgentMessage) {
	if a.messageHashes == nil {
		a.messageHashes = make(map[string][32]byte)
	}
	a.messageHashes[req.RequestID] = agentMessageFingerprint(req)
	a.seen[req.RequestID] = true
}
