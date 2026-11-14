// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

func TestMutationRequestIDNegotiatesExpiryAndPreservesOldHosts(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		t.Run(fmt.Sprint(expiry), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/capabilities" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Nimote-Host-Protocol") != "1" {
					http.Error(w, "invalid authenticated capability request", 400)
					return
				}
				fmt.Fprintf(w, `{"features":{"mutation_expiry_v1":%t}}`, expiry)
			}))
			defer server.Close()
			dir := filepath.Join(t.TempDir(), "host")
			_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			port, _ := strconv.Atoi(portText)
			if err := host.SaveConfig(dir, host.Config{Port: port, LogLevel: "info"}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "token"), []byte("test-token"), 0600); err != nil {
				t.Fatal(err)
			}
			m := &Manager{Dir: dir}
			first, err := m.MutationRequestID(context.Background())
			if err != nil || strings.HasPrefix(first, "v2.") != expiry {
				t.Fatal("wrong contract for Host capability", first, err)
			}
			second, err := m.MutationRequestID(context.Background())
			if err != nil || first == second || len(first) > 128 {
				t.Fatal("mutation keys are not unique and bounded", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if id, err := m.MutationRequestID(ctx); err == nil || id != "" {
				t.Fatal("failed negotiation silently fell back to another contract")
			}
		})
	}
}
