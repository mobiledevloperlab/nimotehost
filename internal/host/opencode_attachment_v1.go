// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// V1 and V2 share the same reconciliation, approval and request-id state
// machine. Only the documented wire shapes differ here. No server is started.
// See OpenCode v1.19.6's instance/httpapi groups and schema/src/v1.
func (c *openCodeHTTP) requestV1(ctx context.Context, method, path string, body, result any) error {
	u, err := url.Parse(strings.TrimPrefix(path, "/api"))
	if err != nil {
		return errors.New("invalid external request")
	}
	q := u.Query()
	q.Del("order")
	q.Del("cursor")
	if c.directory != "" {
		q.Set("directory", c.directory)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	sessionID := ""
	if len(parts) > 1 && parts[0] == "session" {
		sessionID = parts[1]
	}
	if method == http.MethodPost {
		switch {
		case strings.HasSuffix(u.Path, "/prompt"):
			v, ok := body.(map[string]string)
			if !ok {
				return errors.New("invalid external prompt")
			}
			u.Path = strings.TrimSuffix(u.Path, "/prompt") + "/prompt_async"
			body = map[string]any{"messageID": v["id"], "parts": []map[string]string{{"type": "text", "text": v["text"]}}}
		case strings.HasSuffix(u.Path, "/interrupt"):
			u.Path = strings.TrimSuffix(u.Path, "/interrupt") + "/abort"
			q.Del("resume")
		case len(parts) == 5 && parts[2] == "permission" && parts[4] == "reply":
			v, ok := body.(map[string]string)
			if !ok || (v["decision"] != "once" && v["decision"] != "reject") {
				return errors.New("invalid external approval")
			}
			u.Path = "/permission/" + parts[3] + "/reply"
			body = map[string]string{"reply": v["decision"]}
		default:
			return errors.New("unsupported OpenCode v1 operation")
		}
		u.RawQuery = q.Encode()
		if strings.HasSuffix(u.Path, "/abort") || strings.HasSuffix(u.Path, "/reply") {
			var accepted bool
			if err := c.requestRaw(ctx, method, u.String(), body, &accepted); err != nil {
				return err
			}
			if !accepted {
				return errors.New("OpenCode v1 did not accept the native operation")
			}
			return nil
		}
		return c.requestRaw(ctx, method, u.String(), body, result)
	}
	if method != http.MethodGet {
		return errors.New("unsupported OpenCode v1 operation")
	}
	original := u.Path
	switch {
	case original == "/session/active":
		u.Path = "/session/status"
	case strings.HasSuffix(original, "/permission"):
		u.Path = "/permission"
	case original == "/session", strings.HasSuffix(original, "/message"):
		// V1 has no stable session-list cursor. Fetch one bounded authoritative
		// list and page by native ID below. For transcripts, include one extra
		// message so a truncated history fails explicitly rather than appearing
		// complete. The shared 2 MiB response budget still applies.
		q.Set("limit", strconv.Itoa(maxHistoryUpdates+1))
	}
	u.RawQuery = q.Encode()
	var raw json.RawMessage
	if err := c.requestRaw(ctx, method, u.String(), nil, &raw); err != nil {
		return err
	}
	incompatible := errors.New("OpenCode v1 protocol is incompatible or exceeds the snapshot budget")
	decode := func(value any) error {
		encoded, err := json.Marshal(value)
		if err != nil || json.Unmarshal(encoded, result) != nil {
			return incompatible
		}
		return nil
	}
	readSession := func(raw json.RawMessage) (openCodeSession, error) {
		var s openCodeSession
		var flat struct {
			Directory string `json:"directory"`
		}
		if json.Unmarshal(raw, &s) != nil || json.Unmarshal(raw, &flat) != nil || !validExternalConversationID(s.ID) || flat.Directory == "" {
			return s, incompatible
		}
		s.Location.Directory = flat.Directory
		return s, nil
	}
	switch {
	case original == "/session":
		var rows []json.RawMessage
		if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > maxHistoryUpdates {
			return incompatible
		}
		sessions := make([]openCodeSession, 0, len(rows))
		for _, row := range rows {
			s, err := readSession(row)
			if err != nil {
				return err
			}
			sessions = append(sessions, s)
		}
		requested, _ := url.Parse(path)
		cursor := requested.Query().Get("cursor")
		start := 0
		if cursor != "" {
			found := false
			for i, s := range sessions {
				if s.ID == cursor {
					start, found = i+1, true
					break
				}
			}
			if !found {
				return errors.New("OpenCode v1 session cursor expired; refresh discovery")
			}
		}
		end := min(start+maxHistoryPage, len(sessions))
		var next *string
		if end < len(sessions) {
			id := sessions[end-1].ID
			next = &id
		}
		return decode(map[string]any{"data": sessions[start:end], "cursor": map[string]any{"next": next}})
	case original == "/session/active":
		var statuses map[string]struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &statuses) != nil || statuses == nil {
			return incompatible
		}
		for id, status := range statuses {
			switch status.Type {
			case "busy", "retry":
				status.Type = "running"
			case "idle":
			default:
				return incompatible
			}
			statuses[id] = status
		}
		return decode(map[string]any{"data": statuses})
	case strings.HasSuffix(original, "/permission"):
		var rows []struct {
			ID         string   `json:"id"`
			SessionID  string   `json:"sessionID"`
			Permission string   `json:"permission"`
			Patterns   []string `json:"patterns"`
		}
		if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > maxHistoryUpdates {
			return incompatible
		}
		pending := []openCodePermission{}
		for _, row := range rows {
			// The V1 endpoint lists all sessions. Only the selected original
			// session's requests may be projected or answered by this handle.
			if row.SessionID == sessionID {
				pending = append(pending, openCodePermission{ID: row.ID, SessionID: row.SessionID, Action: row.Permission, Resources: row.Patterns})
			}
		}
		return decode(map[string]any{"data": pending})
	case strings.HasSuffix(original, "/message"):
		var rows []struct {
			Info struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionID"`
				Role      string `json:"role"`
			} `json:"info"`
			Parts []json.RawMessage `json:"parts"`
		}
		if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > maxHistoryUpdates {
			return incompatible
		}
		messages := make([]openCodeMessage, 0, len(rows))
		for _, row := range rows {
			if row.Info.SessionID != sessionID || !validExternalConversationID(row.Info.ID) || (row.Info.Role != "user" && row.Info.Role != "assistant") {
				return incompatible
			}
			m := openCodeMessage{ID: row.Info.ID, Type: row.Info.Role}
			for _, rawPart := range row.Parts {
				var part struct {
					Type      string `json:"type"`
					Text      string `json:"text"`
					CallID    string `json:"callID"`
					Tool      string `json:"tool"`
					SessionID string `json:"sessionID"`
					MessageID string `json:"messageID"`
					State     struct {
						Status string         `json:"status"`
						Input  map[string]any `json:"input"`
						Output any            `json:"output"`
						Error  string         `json:"error"`
					} `json:"state"`
				}
				if json.Unmarshal(rawPart, &part) != nil || part.SessionID != sessionID || part.MessageID != m.ID {
					return incompatible
				}
				if part.Type == "text" {
					m.Text += part.Text
				}
				if part.Type == "tool" {
					if !validExternalConversationID(part.CallID) {
						return incompatible
					}
					output := part.State.Output
					if part.State.Status == "error" {
						output = part.State.Error
					}
					encoded, _ := json.Marshal(map[string]any{"type": "tool", "id": part.CallID, "name": part.Tool, "state": map[string]any{"status": part.State.Status, "input": part.State.Input, "content": output}})
					m.Content = append(m.Content, encoded)
				}
			}
			messages = append(messages, m)
		}
		return decode(map[string]any{"data": messages})
	case len(parts) == 2 && parts[0] == "session":
		s, err := readSession(raw)
		if err != nil {
			return err
		}
		return decode(map[string]any{"data": s})
	default:
		return incompatible
	}
}
