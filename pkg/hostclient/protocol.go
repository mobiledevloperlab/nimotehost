// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Package hostclient provides a transport-independent client for the Host API.
// It does not configure SSH, start Host, or retry mutations.
package hostclient

import (
	"encoding/json"
	"fmt"
)

const Protocol = 1

// Request is the envelope accepted by rpc and rpc-stream.
type Request struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// Error describes an HTTP rejection or a bridge failure. Status is zero when
// no HTTP response was received; that does not establish a mutation's outcome.
type Error struct {
	Status  int    `json:"status,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Legacy  string `json:"-"`
	Cause   error  `json:"-"`
}

func (e *Error) Error() string {
	if e.Legacy != "" {
		return e.Legacy
	}
	if e.Status != 0 {
		return fmt.Sprintf("Host RPC %d: %s", e.Status, e.Message)
	}
	return e.Message
}

// Capabilities is the stable negotiation subset. Unknown response fields are
// ignored; optional calls must be gated on a known feature being true.
type Capabilities struct {
	Protocol    int             `json:"protocol"`
	MinProtocol int             `json:"min_protocol"`
	HostID      string          `json:"host_id"`
	Version     string          `json:"version"`
	Features    map[string]bool `json:"features"`
}

func (c Capabilities) Supports(feature string) bool { return c.Features[feature] }

// Unwrap preserves local transport causes for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }
