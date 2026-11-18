// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mobiledevloperlab/nimotehost/pkg/hostclient"
)

func TestRPCRejectionPreservesLegacyAndStructuredDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fail(w, 403, "denied") }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	dir := filepath.Join(t.TempDir(), "state")
	if err := SaveConfig(dir, Config{Port: port, LogLevel: "info"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Call(context.Background(), dir, RPCRequest{Method: "GET", Path: "/v1/status"})
	t.Setenv("NIMOTE_ERROR_BRIDGE_DIR", dir)
	client := hostclient.Client{Transport: hostclient.CommandTransport(os.Args[0], "-test.run=^TestRPCErrorBridgeChild$")}
	sdkErr := client.Do(context.Background(), "GET", "/v1/status", nil, nil)
	var sdkDetail *hostclient.Error
	if !errors.As(sdkErr, &sdkDetail) || sdkDetail.Status != 403 || sdkDetail.Code != "permission_denied" {
		t.Fatalf("SDK bridge: %v", sdkErr)
	}
	var detail *hostclient.Error
	if !errors.As(err, &detail) || detail.Status != 403 || detail.Code != "permission_denied" || detail.Message != "denied" {
		t.Fatalf("%v", err)
	}
	if err.Error() != `Host RPC 403: {"code":"permission_denied","error":"denied"}` {
		t.Fatalf("legacy message changed: %v", err)
	}
	var frame struct {
		Error  string            `json:"error"`
		Detail *hostclient.Error `json:"error_detail"`
	}
	if json.Unmarshal(encodeRPCStreamResponse(1, nil, fmt.Errorf("wrapped: %w", err)), &frame) != nil || frame.Detail == nil || frame.Detail.Status != 403 || frame.Error != "wrapped: "+err.Error() {
		t.Fatalf("%+v", frame)
	}
	server.Close()
	_, stoppedErr := Call(context.Background(), dir, RPCRequest{Method: "GET", Path: "/v1/status"})
	var stopped *hostclient.Error
	if !errors.As(stoppedErr, &stopped) || stopped.Code != "host_stopped" || !errors.Is(stoppedErr, rpcConnectionRefused) {
		t.Fatalf("connection refusal lost typed code or cause: %v", stoppedErr)
	}
}

// This child runs the actual bridge against the parent's isolated HTTP fixture.
func TestRPCErrorBridgeChild(t *testing.T) {
	dir := os.Getenv("NIMOTE_ERROR_BRIDGE_DIR")
	if dir == "" {
		return
	}
	if err := ServeRPCStream(context.Background(), dir, os.Stdin, os.Stdout); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
