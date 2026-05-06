// Package truenasrpc holds primitives shared between the production
// TrueNAS client (internal/client) and the operator-facing probe tool
// (scripts/verify-api-key-roles).
//
// Previous to this package those primitives lived inside internal/client
// and were copy-pasted into the probe with comments saying "Mirrors
// internal/client/ws.go" — exactly the drift hazard a security-critical
// validator should not have. The probe runs against real TrueNAS hosts
// with real API keys; if its host validator is weaker than the
// production one (e.g. accepts an underscore-bearing hostname production
// rejects), the bearer token can travel to a destination production
// would have refused.
//
// Keep this package narrowly scoped to RPC-shape primitives. Anything
// stateful (transport, reconnect logic, telemetry) stays in
// internal/client.
package truenasrpc

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateHost rejects TRUENAS_HOST values that would let an attacker
// smuggle a different destination into the Bearer-token upload URL.
// Accepts bare host or host:port; rejects schemes, paths, user-info,
// query, fragment, and DNS labels carrying characters outside the
// strict letter/digit/hyphen/dot set.
//
// The strict-character loop is load-bearing: a value like
// "evil_host.tld" parses cleanly through url.Parse but production has
// historically rejected underscores ("No underscores or other
// surprises") to keep the tolerated set tiny. Callers must not relax
// this without re-thinking the bearer-exfil class of bugs that drove
// the original validator.
func ValidateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host is empty")
	}

	if strings.ContainsAny(host, "/?#@ \t\r\n") {
		return fmt.Errorf("host %q must be a bare hostname or host:port (no scheme, path, or user-info)", host)
	}

	u, err := url.Parse("https://" + host)
	if err != nil {
		return fmt.Errorf("host %q is not a valid hostname: %w", host, err)
	}

	if u.Host != host {
		return fmt.Errorf("host %q normalized to %q — use a bare hostname or host:port", host, u.Host)
	}

	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("host %q must be a bare hostname or host:port", host)
	}

	// Extract hostname without port; allow IP literals and DNS labels.
	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("host %q has no hostname component", host)
	}

	// IPv4/IPv6 literals are fine as-is.
	if net.ParseIP(hostname) != nil {
		return nil
	}

	// DNS labels: letters, digits, hyphens, dots. No underscores or other surprises.
	for _, r := range hostname {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		default:
			return fmt.Errorf("host %q contains invalid character %q", host, r)
		}
	}

	return nil
}

// IsLoopbackHost returns true when host is a loopback literal or an IP
// that resolves to loopback. Used to quiet the cleartext-fallback
// warning on dev/CI setups, which legitimately use ws://127.0.0.1.
func IsLoopbackHost(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	if hostname == "localhost" {
		return true
	}

	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}

	return false
}

// NormalizeParams ensures JSON-RPC params are sent as a positional array.
// TrueNAS 25.10 middleware rejects bare-object params; nil flows through
// as an empty array (the wire shape `[]`) and non-array singletons are
// wrapped in a one-element array. Pre-shaped slices pass through.
func NormalizeParams(params any) any {
	if params == nil {
		return []any{}
	}

	switch params.(type) {
	case []any, []map[string]any, []string, []int:
		return params
	default:
		return []any{params}
	}
}
