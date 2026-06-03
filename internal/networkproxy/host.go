package networkproxy

import (
	"net/netip"
	"strings"
)

// NormalizeHost normalizes host fragments for policy matching: trims whitespace,
// strips brackets/ports, lowercases, removes trailing dots, and canonicalizes IP
// literals (including scoped IPv6). It is a faithful port of the Rust
// `normalize_host` function and is exported because it forms part of the policy
// contract (callers normalize hosts before adding allow/deny entries).
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") {
		if end := strings.IndexByte(host, ']'); end >= 0 {
			return normalizeDNSHostOrIPLiteral(host[1:end])
		}
	}

	// The proxy stack typically hands us a host without a port, but be
	// defensive and strip ":port" when there is exactly one colon.
	if strings.Count(host, ":") == 1 {
		return normalizeDNSHostOrIPLiteral(strings.SplitN(host, ":", 2)[0])
	}

	// Avoid mangling unbracketed IPv6 literals; strip trailing dots so FQDNs are
	// treated the same as their dotless variants.
	return normalizeDNSHostOrIPLiteral(host)
}

func normalizeDNSHostOrIPLiteral(host string) string {
	host = strings.ToLower(host)
	host = strings.TrimRight(host, ".")
	if ip, ok := normalizeIPLiteral(host); ok {
		return ip
	}
	return host
}

// unscopedIPLiteral returns the IP portion of a scoped literal like
// "fe80::1%lo0" when the prefix parses as an IP, mirroring Rust's
// `unscoped_ip_literal`.
func unscopedIPLiteral(host string) (string, bool) {
	idx := strings.IndexByte(host, '%')
	if idx < 0 {
		return "", false
	}
	ip := host[:idx]
	if _, err := netip.ParseAddr(ip); err != nil {
		return "", false
	}
	return ip, true
}

func normalizeIPLiteral(host string) (string, bool) {
	// Rust's std `IpAddr` does not accept scoped literals, so its
	// `normalize_ip_literal` first tries the bare parse, then splits on
	// "%25"/"%". Go's netip.ParseAddr DOES accept scopes (and does not decode
	// "%25"), so we must handle scope splitting first to match Rust's output of a
	// single "%"-delimited literal with the percent-decoded scope.
	for _, delimiter := range []string{"%25", "%"} {
		idx := strings.Index(host, delimiter)
		if idx < 0 {
			continue
		}
		ip := host[:idx]
		scope := host[idx+len(delimiter):]
		if isUnscopedIP(ip) {
			return ip + "%" + scope, true
		}
	}
	if isUnscopedIP(host) {
		return host, true
	}
	return "", false
}

// isUnscopedIP reports whether s parses as an IP literal without a zone/scope.
func isUnscopedIP(s string) bool {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	return addr.Zone() == ""
}

// parseHost validates and normalizes a host string. An empty normalized host is
// rejected, matching Rust's `Host::parse`.
func parseHost(input string) (string, bool) {
	normalized := NormalizeHost(input)
	if normalized == "" {
		return "", false
	}
	return normalized, true
}

// isLoopbackHost reports whether the (already-normalized) host is a loopback
// hostname or IP literal.
func isLoopbackHost(host string) bool {
	if ip, ok := unscopedIPLiteral(host); ok {
		host = ip
	}
	if host == "localhost" {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.IsLoopback()
	}
	return false
}

// isNonPublicIP reports whether an IP is loopback, private, link-local, or
// otherwise non-globally-routable. It is a faithful port of the Rust
// `is_non_public_ip` SSRF-prevention classifier, including the CIDR blocks that
// Go's stdlib helpers do not cover (CGNAT, TEST-NET, benchmarking, reserved).
func isNonPublicIP(addr netip.Addr) bool {
	if addr.Is4In6() {
		v4 := addr.Unmap()
		return isNonPublicIPv4(v4) || addr.IsLoopback()
	}
	if addr.Is4() {
		return isNonPublicIPv4(addr)
	}
	return isNonPublicIPv6(addr)
}

func isNonPublicIPv4(addr netip.Addr) bool {
	if addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsUnspecified() ||
		addr.IsMulticast() {
		return true
	}
	b := addr.As4()
	if b == [4]byte{255, 255, 255, 255} { // broadcast
		return true
	}
	return ipv4InCIDR(b, [4]byte{0, 0, 0, 0}, 8) || // "this network" (RFC 1122)
		ipv4InCIDR(b, [4]byte{100, 64, 0, 0}, 10) || // CGNAT (RFC 6598)
		ipv4InCIDR(b, [4]byte{192, 0, 0, 0}, 24) || // IETF Protocol Assignments
		ipv4InCIDR(b, [4]byte{192, 0, 2, 0}, 24) || // TEST-NET-1 (RFC 5737)
		ipv4InCIDR(b, [4]byte{198, 18, 0, 0}, 15) || // Benchmarking (RFC 2544)
		ipv4InCIDR(b, [4]byte{198, 51, 100, 0}, 24) || // TEST-NET-2 (RFC 5737)
		ipv4InCIDR(b, [4]byte{203, 0, 113, 0}, 24) || // TEST-NET-3 (RFC 5737)
		ipv4InCIDR(b, [4]byte{240, 0, 0, 0}, 4) // Reserved (RFC 6890)
}

func ipv4InCIDR(ip, base [4]byte, prefix uint) bool {
	toU32 := func(b [4]byte) uint32 {
		return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	}
	var mask uint32
	if prefix == 0 {
		mask = 0
	} else {
		mask = ^uint32(0) << (32 - prefix)
	}
	return (toU32(ip) & mask) == (toU32(base) & mask)
}

func isNonPublicIPv6(addr netip.Addr) bool {
	// Treat anything not globally routable as "local" for SSRF prevention:
	// ::1 loopback, fc00::/7 unique-local, fe80::/10 link-local, :: unspecified,
	// and multicast ranges.
	return addr.IsLoopback() ||
		addr.IsUnspecified() ||
		addr.IsMulticast() ||
		addr.IsPrivate() || // fc00::/7 unique-local
		addr.IsLinkLocalUnicast()
}
