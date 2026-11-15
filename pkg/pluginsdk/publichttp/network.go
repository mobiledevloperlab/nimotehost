// SPDX-License-Identifier: Apache-2.0
// Package publichttp provides public-address DNS-pinned dialing for SDK users.
// It is an application-layer defense, not an OS network sandbox.
package publichttp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

// Conservative globally-routable policy. Reject local, mapped, transition,
// documentation, benchmarking, multicast, CGNAT and reserved destinations.
var forbiddenPrefixes = func() []netip.Prefix {
	out := []netip.Prefix{}
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func IsPublicIP(ip netip.Addr) bool {
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
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return DialWith(ctx, network, address, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 5 * time.Second}).DialContext)
}
func DialWith(ctx context.Context, network, address string, lookup func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
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
		if !IsPublicIP(ip) {
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
