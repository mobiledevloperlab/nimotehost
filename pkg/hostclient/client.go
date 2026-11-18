// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors
package hostclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Transport carries one request. Custom transports can reuse a persistent SSH
// channel. They must not replay dispatched mutations after transport failures.
type Transport func(context.Context, Request) (json.RawMessage, error)

type Client struct{ Transport Transport }

func (c *Client) Do(ctx context.Context, method, path string, body any, result any) error {
	if c == nil || c.Transport == nil {
		return errors.New("Host transport is required")
	}
	if method != "GET" && method != "POST" && method != "DELETE" {
		return errors.New("unsupported Host method")
	}
	if !strings.HasPrefix(path, "/v1/") || strings.ContainsAny(path, "\r\n#") {
		return errors.New("invalid Host path")
	}
	req := Request{Method: method, Path: path}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req.Body = data
	}
	raw, err := c.Transport(ctx, req)
	if err != nil {
		return err
	}
	if !json.Valid(raw) {
		return errors.New("invalid Host JSON response")
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(raw, result)
}

func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var caps Capabilities
	if err := c.Do(ctx, "GET", "/v1/capabilities", nil, &caps); err != nil {
		return caps, err
	}
	if caps.Protocol != Protocol || caps.MinProtocol != Protocol || caps.HostID == "" || caps.Features == nil {
		return caps, &Error{Code: "incompatible_protocol", Message: "incompatible Host capabilities"}
	}
	return caps, nil
}

// CommandTransport invokes a trusted command once per request. For example:
// CommandTransport("nimotehost", "rpc-stream"), or
// CommandTransport("ssh", "user@host", "nimotehost", "rpc-stream").
// Requests go through stdin, never argv. There is no local shell or automatic
// retry. Configure SSH authentication and host-key verification separately.
// Use a custom Transport for a long-lived multiplexed connection.
func CommandTransport(program string, args ...string) Transport {
	argv := append([]string(nil), args...)
	return func(ctx context.Context, req Request) (result json.RawMessage, resultErr error) {
		// Bound bridge lifetime even when the caller supplies no deadline.
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		defer func() {
			if ctx.Err() != nil {
				result = nil
				resultErr = ctx.Err()
			}
		}()
		frame := struct {
			ID int `json:"id"`
			Request
		}{1, req}
		data, err := json.Marshal(frame)
		if err != nil {
			return nil, err
		}
		if len(data) > (1<<20)+512 {
			return nil, errors.New("Host request exceeds frame limit")
		}
		cmd := exec.CommandContext(ctx, program, argv...)
		cmd.WaitDelay = 2 * time.Second
		cmd.Stdin = bytes.NewReader(append(data, '\n'))
		// Remote diagnostics can contain paths or credentials. Keep stderr out of
		// returned errors; callers can supply their own transport for diagnostics.
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err = cmd.Start(); err != nil {
			return nil, err
		}
		waited := false
		defer func() {
			if !waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), (2<<20)+1)
		if !scanner.Scan() {
			return nil, errors.New("Host bridge greeting missing")
		}
		var greeting struct {
			Protocol int `json:"stream_protocol"`
		}
		if json.Unmarshal(scanner.Bytes(), &greeting) != nil || greeting.Protocol != 1 {
			return nil, errors.New("incompatible Host stream greeting")
		}
		if !scanner.Scan() {
			return nil, errors.New("Host bridge response missing or oversized")
		}
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *string         `json:"error"`
			Detail *Error          `json:"error_detail"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil || response.ID != 1 || (len(response.Result) != 0) == (response.Error != nil) || (response.Detail != nil && response.Error == nil) {
			return nil, errors.New("invalid Host response envelope")
		}
		if scanner.Scan() {
			return nil, errors.New("unexpected extra Host frame")
		}
		if err = scanner.Err(); err != nil {
			return nil, fmt.Errorf("read Host frame: %w", err)
		}
		err = cmd.Wait()
		waited = true
		if err != nil {
			return nil, fmt.Errorf("Host bridge exited: %w", err)
		}
		if response.Error != nil {
			if response.Detail != nil && response.Detail.Code != "" {
				response.Detail.Legacy = *response.Error
				return nil, response.Detail
			}
			return nil, &Error{Code: "unknown", Message: *response.Error, Legacy: *response.Error}
		}
		return response.Result, nil
	}
}
