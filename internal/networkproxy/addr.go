package networkproxy

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// RuntimeConfig holds the resolved listener bind addresses.
type RuntimeConfig struct {
	HTTPAddr  netip.AddrPort
	SocksAddr netip.AddrPort
}

// ResolveRuntime validates the unix-socket allowlist and resolves the HTTP and
// SOCKS5 bind addresses with loopback clamping, mirroring Rust's
// `resolve_runtime`.
func ResolveRuntime(cfg NetworkProxyConfig) (RuntimeConfig, error) {
	if err := validateUnixSocketAllowlistPaths(cfg); err != nil {
		return RuntimeConfig{}, err
	}
	httpAddr, err := resolveAddr(cfg.Network.ProxyURL, 3128)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("invalid network.proxy_url %q: %w", cfg.Network.ProxyURL, err)
	}
	socksAddr, err := resolveAddr(cfg.Network.SocksURL, 8081)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("invalid network.socks_url %q: %w", cfg.Network.SocksURL, err)
	}
	httpAddr, socksAddr = clampBindAddrs(httpAddr, socksAddr, cfg.Network)
	return RuntimeConfig{HTTPAddr: httpAddr, SocksAddr: socksAddr}, nil
}

func resolveAddr(rawURL string, defaultPort uint16) (netip.AddrPort, error) {
	parts, err := parseHostPort(rawURL, defaultPort)
	if err != nil {
		return netip.AddrPort{}, err
	}
	host := parts.host
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	if addr, perr := netip.ParseAddr(host); perr == nil {
		return netip.AddrPortFrom(addr, parts.port), nil
	}
	// Hostnames cannot be bound directly; fall back to loopback.
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), parts.port), nil
}

type socketAddressParts struct {
	host string
	port uint16
}

func parseHostPort(rawURL string, defaultPort uint16) (socketAddressParts, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return socketAddressParts{}, fmt.Errorf("missing host in network proxy address: %s", rawURL)
	}

	// Avoid treating unbracketed IPv6 literals like "2001:db8::1" as URLs.
	if addr, err := netip.ParseAddr(trimmed); err == nil && addr.Is6() && !strings.HasPrefix(trimmed, "[") {
		return socketAddressParts{host: trimmed, port: defaultPort}, nil
	}

	// Strip any scheme, userinfo, and path; then parse host:port manually so we
	// match the Rust behavior (default-port fallback on invalid ports, etc.).
	withoutScheme := trimmed
	if idx := strings.Index(withoutScheme, "://"); idx >= 0 {
		withoutScheme = withoutScheme[idx+3:]
	}
	hostPort := withoutScheme
	if idx := strings.IndexByte(hostPort, '/'); idx >= 0 {
		hostPort = hostPort[:idx]
	}
	if idx := strings.LastIndexByte(hostPort, '@'); idx >= 0 {
		hostPort = hostPort[idx+1:]
	}

	if strings.HasPrefix(hostPort, "[") {
		if end := strings.IndexByte(hostPort, ']'); end >= 0 {
			host := hostPort[1:end]
			port := defaultPort
			if rest := hostPort[end+1:]; strings.HasPrefix(rest, ":") {
				if p, err := strconv.ParseUint(rest[1:], 10, 16); err == nil {
					port = uint16(p)
				}
			}
			if host == "" {
				return socketAddressParts{}, fmt.Errorf("missing host in network proxy address: %s", rawURL)
			}
			return socketAddressParts{host: host, port: port}, nil
		}
	}

	// Only treat as host:port when there's a single colon (avoids misreading
	// unbracketed IPv6 literals).
	if strings.Count(hostPort, ":") == 1 {
		host, portStr, _ := strings.Cut(hostPort, ":")
		if host == "" {
			return socketAddressParts{}, fmt.Errorf("missing host in network proxy address: %s", rawURL)
		}
		port := defaultPort
		if p, err := strconv.ParseUint(portStr, 10, 16); err == nil {
			port = uint16(p)
		}
		return socketAddressParts{host: host, port: port}, nil
	}

	if hostPort == "" {
		return socketAddressParts{}, fmt.Errorf("missing host in network proxy address: %s", rawURL)
	}
	return socketAddressParts{host: hostPort, port: defaultPort}, nil
}

// HostAndPortFromNetworkAddr formats a host:port string from a loose network
// address for display, mirroring Rust's `host_and_port_from_network_addr`.
func HostAndPortFromNetworkAddr(value string, defaultPort uint16) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "<missing>"
	}
	parts, err := parseHostPort(trimmed, defaultPort)
	if err != nil {
		return formatHostAndPort(trimmed, defaultPort)
	}
	return formatHostAndPort(parts.host, parts.port)
}

func formatHostAndPort(host string, port uint16) string {
	if strings.Contains(host, ":") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// clampBindAddrs clamps non-loopback binds to loopback unless explicitly
// allowed, and forces loopback when unix-socket proxying is enabled. Faithful
// port of Rust's `clamp_bind_addrs`.
func clampBindAddrs(httpAddr, socksAddr netip.AddrPort, cfg NetworkProxySettings) (netip.AddrPort, netip.AddrPort) {
	httpAddr = clampNonLoopback(httpAddr, cfg.DangerouslyAllowNonLoopbackProxy)
	socksAddr = clampNonLoopback(socksAddr, cfg.DangerouslyAllowNonLoopbackProxy)
	if len(cfg.AllowUnixSockets()) == 0 && !cfg.DangerouslyAllowAllUnixSockets {
		return httpAddr, socksAddr
	}
	// Unix-socket proxying forces loopback to avoid becoming a remote bridge to
	// local daemons.
	loopback := netip.AddrFrom4([4]byte{127, 0, 0, 1})
	return netip.AddrPortFrom(loopback, httpAddr.Port()), netip.AddrPortFrom(loopback, socksAddr.Port())
}

func clampNonLoopback(addr netip.AddrPort, allowNonLoopback bool) netip.AddrPort {
	if addr.Addr().IsLoopback() {
		return addr
	}
	if allowNonLoopback {
		return addr
	}
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), addr.Port())
}

// netipFromTCPAddr converts a *net.TCPAddr to an AddrPort.
func netipFromTCPAddr(addr net.Addr) (netip.AddrPort, bool) {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return netip.AddrPort{}, false
	}
	a, ok := netip.AddrFromSlice(tcp.IP)
	if !ok {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(a.Unmap(), uint16(tcp.Port)), true
}
