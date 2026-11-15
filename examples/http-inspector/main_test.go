// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*http.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
	}, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, server
}
func TestInspectorMethodsQueryHeadersBody(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != method || r.URL.Query().Get("q") != "a&b" || r.Header.Get("X-Test") != "yes" {
					t.Error("request changed")
				}
				b, _ := io.ReadAll(r.Body)
				if string(b) != "payload" {
					t.Error(string(b))
				}
				w.Header().Set("Set-Cookie", "SECRET")
				w.Header().Set("X-Api-Key", "SECRET")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(201)
				_, _ = w.Write([]byte(`{"ok":true}`))
			})
			raw, _ := json.Marshal(request{Method: method, URL: "http://example.com/api", Query: map[string]string{"q": "a&b"}, Headers: map[string]string{"X-Test": "yes"}, Body: "payload"})
			out, err := inspect(context.Background(), raw, client)
			if err != nil {
				t.Fatal(err)
			}
			if out.Status != 201 || out.Format != "json" || string(out.JSON) != `{"ok":true}` {
				t.Fatal(out)
			}
			b, _ := json.Marshal(out.Headers)
			if strings.Contains(string(b), "SECRET") {
				t.Fatal("credential header leak")
			}
		})
	}
}
func TestInspectorTextLimitsRedirectsAndCancellation(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantError bool
	}{
		{"text", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("hello")) }, false},
		{"oversized", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		}, true},
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://127.0.0.1/SECRET", 302) }, true},
		{"timeout", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
		}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _ := testClient(t, test.handler)
			raw, _ := json.Marshal(request{Method: "GET", URL: "http://example.com", TimeoutMS: 50})
			out, err := inspect(context.Background(), raw, client)
			if (err != nil) != test.wantError {
				t.Fatal(out, err)
			}
			if test.name == "text" && (out.Format != "text" || out.Body != "hello") {
				t.Fatal(out)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Fatal("error leaks URL")
			}
		})
	}
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspect(ctx, json.RawMessage(`{"method":"GET","url":"http://example.com"}`), client); err == nil {
		t.Fatal("cancellation ignored")
	}
	raw, _ := json.Marshal(request{Method: "POST", URL: "http://example.com", Body: strings.Repeat("x", maxRequestBytes)})
	if _, err := inspect(context.Background(), raw, client); err == nil {
		t.Fatal("request limit ignored")
	}
}
func TestInspectorSSRFPolicy(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "http://localhost", "http://foo.localhost", "http://metadata.internal", "http://127.0.0.1", "http://10.1.2.3", "http://169.254.169.254/latest", "http://100.100.100.200", "http://[::1]", "http://[::ffff:127.0.0.1]", "http://[64:ff9b::7f00:1]", "http://[fe80::1%25en0]", "http://user:SECRET@example.com", "http://example.com:22", "http://example.com/#SECRET"} {
		if _, err := safeURL(raw); err == nil {
			t.Errorf("unsafe URL accepted: %s", raw)
		}
	}
	for _, s := range []string{"0.0.0.0", "192.168.0.1", "198.18.0.1", "224.0.0.1", "240.0.0.1", "2001:db8::1", "2002:7f00:1::", "fc00::1", "::", "::ffff:8.8.8.8"} {
		if publicIP(netip.MustParseAddr(s)) {
			t.Error("unsafe IP accepted", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(s)) {
			t.Error("public IP rejected", s)
		}
	}
	// This uses only numeric local addresses and never contacts the public network.
	for _, host := range []string{"127.0.0.1:80", "169.254.169.254:80", "[::1]:80"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := dialPublic(ctx, "tcp", host)
		cancel()
		if conn != nil {
			conn.Close()
		}
		if err == nil {
			t.Error("local connection accepted", host)
		}
	}
	for _, header := range []string{"Authorization", "COOKIE", "X-Api-Key", "Host", "Connection", "Content-Length"} {
		if !forbiddenHeader(header) {
			t.Error("sensitive header allowed", header)
		}
	}
}
func TestInspectorRejectsSensitiveInputHeaders(t *testing.T) {
	client := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("sensitive request was sent"); return nil, nil })}
	for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Api-Key"} {
		raw, _ := json.Marshal(request{Method: "GET", URL: "http://example.com", Headers: map[string]string{name: "SECRET"}})
		if _, err := inspect(context.Background(), raw, client); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal(err)
		}
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInspectorDNSPinningAndMixedAnswers(t *testing.T) {
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dial before all DNS answers validated")
		return nil, nil
	}
	if _, err := dialPublicWith(context.Background(), "tcp", "example.com:80", lookup, dial); err == nil {
		t.Fatal("mixed public/private DNS allowed")
	}
	lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	var peer net.Conn
	dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:80" {
			t.Fatal("hostname resolved again", address)
		}
		a, b := net.Pipe()
		peer = b
		return a, nil
	}
	conn, err := dialPublicWith(context.Background(), "tcp", "example.com:80", lookup, dial)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	peer.Close()
}
