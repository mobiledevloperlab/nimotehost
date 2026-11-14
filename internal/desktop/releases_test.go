// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseMetadata(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
		ok   bool
	}{{`{"tag_name":"v1.2.3","html_url":"` + ReleasesURL + `/tag/v1.2.3"}`, 200, true}, {`{"tag_name":"v1","html_url":"https://evil.invalid"}`, 200, false}, {`{`, 200, false}, {`{}`, 404, false}} {
		t.Run(tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			_, err := latestRelease(context.Background(), server.Client(), server.URL)
			if (err == nil) != tc.ok {
				t.Fatalf("lookup success=%v error=%v", tc.ok, err)
			}
		})
	}
}
