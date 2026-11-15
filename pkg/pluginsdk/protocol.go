// SPDX-License-Identifier: Apache-2.0
// Package pluginsdk is the public, transport-independent Nimote capability SDK.
// It imports no Host internals. See docs/plugin-system/spec/v1 for the wire contract.
package pluginsdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"runtime"
)

const Protocol = "1.0"
const MaxFrame = 512 << 10

var Name = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var Version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type Capability struct {
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Version     string          `json:"version"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input_schema"`
	Output      json.RawMessage `json:"output_schema"`
	UI          json.RawMessage `json:"ui,omitempty"`
	Sensitive   bool            `json:"sensitive,omitempty"`
	// ACP uses the existing Host session/approval state machine, never a new wire implementation.
	ACP int `json:"acp_version,omitempty"`
}
type Manifest struct {
	Protocol     string       `json:"protocol"`
	ID           string       `json:"id"`
	Version      string       `json:"version"`
	Publisher    string       `json:"publisher"`
	Source       string       `json:"source"`
	Entrypoint   string       `json:"entrypoint"`
	SHA256       string       `json:"sha256"`
	Platforms    []string     `json:"platforms"`
	Permissions  []string     `json:"permissions"`
	Capabilities []Capability `json:"capabilities"`
}

func (m Manifest) Validate() error {
	if m.Protocol != Protocol || !Name.MatchString(m.ID) || !Version.MatchString(m.Version) || m.Publisher == "" || m.Source == "" || len(m.Capabilities) == 0 || len(m.Capabilities) > 64 {
		return errors.New("invalid or incompatible manifest")
	}
	if m.Entrypoint != "plugin" && m.Entrypoint != "plugin.exe" {
		return errors.New("entrypoint must be plugin or plugin.exe")
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(m.SHA256) {
		return errors.New("SHA256 required")
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	supported := false
	for _, p := range m.Platforms {
		if p == platform {
			supported = true
		}
	}
	if !supported {
		return errors.New("unsupported platform")
	}
	seen := map[string]bool{}
	for _, p := range m.Permissions {
		if p != "network.public" && p != "process.same-user" {
			return errors.New("unknown permission")
		}
		if seen[p] {
			return errors.New("duplicate permission")
		}
		seen[p] = true
	}
	// Process execution always requires explicit acknowledgement of the actual OS boundary.
	if !seen["process.same-user"] {
		return errors.New("process.same-user permission required")
	}
	seen = map[string]bool{}
	for _, c := range m.Capabilities {
		if !Name.MatchString(c.Name) || seen[c.Name] || !Version.MatchString(c.Version) || c.Title == "" {
			return errors.New("invalid capability")
		}
		seen[c.Name] = true
		switch c.Kind {
		case "agent", "tool", "task", "event":
		default:
			return errors.New("unknown capability kind")
		}
		if !json.Valid(c.Input) || !json.Valid(c.Output) || len(bytes.TrimSpace(c.Input)) == 0 || bytes.TrimSpace(c.Input)[0] != '{' || bytes.TrimSpace(c.Output)[0] != '{' {
			return errors.New("schemas required")
		}
		if len(c.UI) > 0 && bytes.TrimSpace(c.UI)[0] != '{' {
			return errors.New("UI descriptor must be an object")
		}
		if c.ACP != 0 && (c.Kind != "agent" || (c.ACP != 1 && c.ACP != 2)) {
			return errors.New("unsupported ACP adapter")
		}
	}
	return nil
}

type Request struct {
	ID         string          `json:"id"`
	Method     string          `json:"method"`
	Protocol   string          `json:"protocol,omitempty"`
	Capability string          `json:"capability,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type Response struct {
	ID       string          `json:"id"`
	Protocol string          `json:"protocol,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *Error          `json:"error,omitempty"`
	Progress *float64        `json:"progress,omitempty"`
}
type Handler func(context.Context, string, json.RawMessage, func(float64)) (any, error)

// Serve processes one invocation at a time. Host cancellation terminates the owned
// process tree; handlers must also observe ctx for direct SDK use. Stdout is wire only.
// Error details are deliberately suppressed: third-party errors may contain credentials.
func Serve(ctx context.Context, in io.Reader, out io.Writer, handler Handler) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), MaxFrame)
	enc := json.NewEncoder(out)
	initialized := false
	for scan.Scan() {
		var req Request
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil || req.ID == "" {
			return errors.New("invalid request")
		}
		res := Response{ID: req.ID}
		switch req.Method {
		case "initialize":
			if req.Protocol != Protocol {
				res.Error = &Error{"incompatible_protocol", "supported protocol: " + Protocol}
			} else {
				initialized = true
				res.Protocol = Protocol
				res.Result = json.RawMessage(`{}`)
			}
		case "ping":
			if initialized {
				res.Result = json.RawMessage(`{}`)
			} else {
				res.Error = &Error{"not_initialized", "initialize first"}
			}
		case "invoke":
			if !initialized {
				res.Error = &Error{"not_initialized", "initialize first"}
				break
			}
			value, err := handler(ctx, req.Capability, req.Input, func(p float64) {
				if p >= 0 && p <= 1 {
					_ = enc.Encode(Response{ID: req.ID, Progress: &p})
				}
			})
			if err != nil {
				res.Error = &Error{"plugin_error", "plugin operation failed"}
			} else {
				res.Result, err = json.Marshal(value)
				if err != nil || len(res.Result) > MaxFrame-1024 {
					res.Result = nil
					res.Error = &Error{"result_too_large", "result cannot be encoded within frame limit"}
				}
			}
		default:
			res.Error = &Error{"method_not_found", "unknown method"}
		}
		if err := enc.Encode(res); err != nil {
			return err
		}
	}
	return scan.Err()
}
