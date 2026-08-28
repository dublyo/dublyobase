package core

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

func validatePublicOutboundHost(host string) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return fmt.Errorf("%w: outbound host is required", ErrValidation)
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("%w: outbound host must not target localhost", ErrValidation)
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedOutboundIP(ip) {
		return fmt.Errorf("%w: outbound host must not target private or local addresses", ErrValidation)
	}
	return nil
}

// validatePublicOutboundTarget adds a DNS check to the literal-address check
// above, so a name pointing at an internal service is caught when the job is
// saved rather than failing on every run. Resolution is best effort: a name
// that does not resolve right now is allowed through, because the dialer
// re-checks at connect time and a transient DNS failure should not block
// saving a legitimate job.
func validatePublicOutboundTarget(ctx context.Context, host string) error {
	if err := validatePublicOutboundHost(host); err != nil {
		return err
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if net.ParseIP(host) != nil {
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil
	}
	for _, candidate := range ips {
		if isBlockedOutboundIP(candidate.IP) {
			return fmt.Errorf("%w: outbound host resolves to a private or local address", ErrValidation)
		}
	}
	return nil
}

func publicTCPDialer(timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		if network != "tcp" && network != "tcp4" && network != "tcp6" {
			return nil, fmt.Errorf("%w: unsupported outbound network", ErrValidation)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if err := validatePublicOutboundHost(host); err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("%w: outbound host has no addresses", ErrValidation)
		}
		for _, candidate := range ips {
			if isBlockedOutboundIP(candidate.IP) {
				return nil, fmt.Errorf("%w: outbound host resolves to a private or local address", ErrValidation)
			}
		}
		var lastErr error
		for _, candidate := range ips {
			dialer := net.Dialer{Timeout: timeout}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func isBlockedOutboundIP(ip net.IP) bool {
	return ip == nil ||
		ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}
