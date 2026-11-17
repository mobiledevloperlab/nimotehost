// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// The SDK owns wire correlation but does not expose inbound request IDs to its
// method handler. Retain a trusted copy inside local params so the provider's
// serverRequest/resolved broadcast can retire the matching mobile approval.
// This marker is only injected on the local receive path, never sent upstream.
type codexAttachmentReader struct {
	scanner   *bufio.Scanner
	remaining []byte
}

func newCodexAttachmentReader(r io.Reader) io.Reader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 2<<20)
	return &codexAttachmentReader{scanner: s}
}
func (r *codexAttachmentReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.remaining) == 0 {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		line := r.scanner.Bytes()
		var frame map[string]json.RawMessage
		if json.Unmarshal(line, &frame) == nil {
			if len(frame["id"]) > 256 {
				return 0, errors.New("external native request identifier exceeds limit")
			}
			var method string
			_ = json.Unmarshal(frame["method"], &method)
			if len(frame["id"]) > 0 && !bytes.Equal(bytes.TrimSpace(frame["id"]), []byte("null")) && method != "" {
				var params map[string]json.RawMessage
				if json.Unmarshal(frame["params"], &params) == nil && params != nil {
					params["_nimote_native_request_id"] = frame["id"]
					frame["params"], _ = json.Marshal(params)
					line, _ = json.Marshal(frame)
				}
			}
		}
		r.remaining = append(append([]byte(nil), line...), '\n')
	}
	n := copy(p, r.remaining)
	r.remaining = r.remaining[n:]
	return n, nil
}
