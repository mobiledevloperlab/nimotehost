// SPDX-License-Identifier: Apache-2.0
// HTTP API Inspector is an independent public-SDK plugin. Host has no knowledge
// of its methods, URLs, output, or security policy.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

const maxRequestBytes = 64 << 10
const maxResponseBytes = 128 << 10

type request struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Query     map[string]string `json:"query,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}
type response struct {
	Status    int                 `json:"status"`
	Headers   map[string][]string `json:"headers"`
	ElapsedMS int64               `json:"elapsed_ms"`
	Format    string              `json:"format"`
	Body      string              `json:"body"`
	JSON      json.RawMessage     `json:"json,omitempty"`
}

// Conservative globally-routable policy. Reject local, mapped, transition,
// documentation, benchmarking, multicast, CGNAT and reserved destinations.
var forbiddenPrefixes = func() []netip.Prefix {
	out := []netip.Prefix{}
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func publicIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Is4In6() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range forbiddenPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
func safeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || strings.Contains(u.Hostname(), "%") {
		return nil, errors.New("invalid public HTTP URL")
	}
	if u.Port() != "" && u.Port() != "80" && u.Port() != "443" {
		return nil, errors.New("only HTTP/HTTPS ports allowed")
	}
	if u.Scheme == "https" && u.Port() == "80" || u.Scheme == "http" && u.Port() == "443" {
		return nil, errors.New("scheme/port mismatch")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return nil, errors.New("local hostname forbidden")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicIP(ip) {
		return nil, errors.New("non-public address forbidden")
	}
	return u, nil
}
func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	return dialPublicWith(ctx, network, address, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 5 * time.Second}).DialContext)
}
func dialPublicWith(ctx context.Context, network, address string, lookup func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("DNS resolution failed")
	}
	// Reject mixed answers as well as wholly private answers; connect to the exact
	// checked numeric address, never resolve the hostname again (DNS rebinding).
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("DNS resolved a non-public address")
		}
	}
	var last error
	for _, ip := range ips {
		conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}
func forbiddenHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "host", "connection", "upgrade", "transfer-encoding", "content-length", "trailer", "te", "proxy-connection", "x-api-key", "api-key":
		return true
	}
	return false
}
func inspect(ctx context.Context, raw json.RawMessage, client *http.Client) (response, error) {
	var input request
	if len(raw) > maxRequestBytes {
		return response{}, errors.New("request too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return response{}, err
	}
	switch input.Method {
	case "GET", "POST", "PUT", "DELETE":
	default:
		return response{}, errors.New("unsupported method")
	}
	u, err := safeURL(input.URL)
	if err != nil {
		return response{}, err
	}
	query := u.Query()
	for k, v := range input.Query {
		query.Set(k, v)
	}
	u.RawQuery = query.Encode()
	timeout := input.TimeoutMS
	if timeout == 0 {
		timeout = 10000
	}
	if timeout < 1 || timeout > 20000 {
		return response{}, errors.New("timeout out of range")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, input.Method, u.String(), strings.NewReader(input.Body))
	if err != nil {
		return response{}, errors.New("invalid request")
	}
	size := len(input.Body) + len(u.String())
	for k, v := range input.Headers {
		if forbiddenHeader(k) {
			return response{}, errors.New("sensitive or transport header forbidden")
		}
		size += len(k) + len(v)
		req.Header.Set(k, v)
	}
	if size > maxRequestBytes {
		return response{}, errors.New("request too large")
	}
	started := time.Now()
	res, err := client.Do(req)
	if err != nil {
		return response{}, errors.New("HTTP request failed")
	}
	defer res.Body.Close()
	// No redirect, cookie jar, proxy environment, decompression, or credentials.
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return response{}, errors.New("redirects forbidden")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return response{}, errors.New("response read failed")
	}
	if len(body) > maxResponseBytes {
		return response{}, errors.New("response too large")
	}
	headers := map[string][]string{}
	// A positive allowlist excludes cookies, auth challenges, signed redirect URLs,
	// and custom response headers that commonly carry credentials.
	for _, key := range []string{"Content-Type", "Content-Length", "Date", "Allow", "Cache-Control"} {
		if values := res.Header.Values(key); len(values) > 0 {
			headers[key] = values
		}
	}
	out := response{Status: res.StatusCode, Headers: headers, ElapsedMS: time.Since(started).Milliseconds(), Format: "text", Body: string(body)}
	if json.Valid(body) {
		out.Format = "json"
		out.JSON = json.RawMessage(body)
		out.Body = ""
	}
	return out, nil
}
func main() {
	transport := &http.Transport{Proxy: nil, DialContext: dialPublic, DisableCompression: true, MaxResponseHeaderBytes: 16 << 10, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	err := sdk.Serve(context.Background(), os.Stdin, os.Stdout, func(ctx context.Context, name string, input json.RawMessage, progress func(float64)) (any, error) {
		if name != "request" {
			return nil, errors.New("unknown capability")
		}
		progress(0)
		out, err := inspect(ctx, input, client)
		if err == nil {
			progress(1)
		}
		return out, err
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "plugin transport closed")
		os.Exit(1)
	}
}
