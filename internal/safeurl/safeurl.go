// Package safeurl rejects probe destinations that are unsafe for a
// server-side fetcher (loopback, private, link-local, metadata).
package safeurl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrBlocked is returned when a URL or resolved address is not a
// permitted probe destination.
var ErrBlocked = errors.New("blocked destination")

// cgnat is RFC 6598 shared address space (100.64.0.0/10).
var cgnat = func() *net.IPNet {
	_, n, err := net.ParseCIDR("100.64.0.0/10")
	if err != nil {
		panic(err)
	}
	return n
}()

// Validate checks that raw is an absolute http(s) URL whose host is
// not an obvious SSRF target. When allowPrivate is true, private and
// link-local destinations are accepted (tests and operators who
// intentionally monitor an internal URL).
func Validate(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBlocked, err)
	}
	return ValidateURL(u, allowPrivate)
}

// ValidateURL applies the same policy as Validate to a parsed URL.
func ValidateURL(u *url.URL, allowPrivate bool) error {
	if u == nil {
		return fmt.Errorf("%w: empty url", ErrBlocked)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https", ErrBlocked)
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrBlocked)
	}
	if !allowPrivate && blockedHostname(host) {
		return fmt.Errorf("%w: host %q", ErrBlocked, host)
	}
	if ip := net.ParseIP(host); ip != nil && !allowPrivate && blockedIP(ip) {
		return fmt.Errorf("%w: address %s", ErrBlocked, ip)
	}
	return nil
}

// RestrictedDialContext resolves address, drops blocked IPs, then
// dials the remainder. The TCP handshake never reaches a blocked
// address unless allowPrivate is set.
func RestrictedDialContext(base *net.Dialer, allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	if base == nil {
		base = &net.Dialer{}
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		resolver := base.Resolver
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}

		var last error
		tried := 0
		for _, ipa := range ips {
			if !allowPrivate && blockedIP(ipa.IP) {
				last = fmt.Errorf("%w: address %s", ErrBlocked, ipa.IP)
				continue
			}
			tried++
			conn, err := base.DialContext(ctx, network, net.JoinHostPort(ipa.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if tried == 0 {
			if last == nil {
				last = fmt.Errorf("%w: no allowed addresses for %s", ErrBlocked, host)
			}
			return nil, last
		}
		return nil, last
	}
}

func blockedHostname(host string) bool {
	switch host {
	case "localhost", "localhost.localdomain", "metadata", "metadata.google.internal", "metadata.goog":
		return true
	}
	return strings.HasSuffix(host, ".localhost")
}

func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	return cgnat.Contains(ip)
}
