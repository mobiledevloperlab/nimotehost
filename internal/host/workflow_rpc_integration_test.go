// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
)

// Opt-in helper for the sibling Flutter protocol integration test. Uses the
// actual Host HTTP server and encoded CLI boundary, with an isolated state dir.
func TestHostWorkflowRPCChild(t *testing.T) {
	dir := os.Getenv("NIMOTE_HOST_FIXTURE_DIR")
	if dir == "" {
		return
	}
	if len(os.Args) > 2 && os.Args[len(os.Args)-2] == "--" {
		data, e := base64.StdEncoding.DecodeString(os.Args[len(os.Args)-1])
		if e != nil {
			os.Exit(2)
		}
		var req RPCRequest
		if decodeJSON(data, &req) != nil {
			os.Exit(2)
		}
		b, e := Call(context.Background(), dir, req)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(b)
		os.Exit(0)
	}
	if e := Serve(context.Background(), dir, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	os.Exit(0)
}
