package sandbox

import (
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/networkproxy"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// unixDomainSocketKind selects how AF_UNIX traffic is treated by the generated
// Seatbelt policy. Mirrors the UnixDomainSocketPolicy enum: AllowAll and the
// allowlisted Restricted mode are kept disjoint so no ignored state is carried.
type unixDomainSocketKind int

const (
	// unixDomainSocketRestricted limits AF_UNIX traffic to an allowlist of
	// socket paths (possibly empty). This is the default.
	unixDomainSocketRestricted unixDomainSocketKind = iota
	// unixDomainSocketAllowAll permits all AF_UNIX traffic.
	unixDomainSocketAllowAll
)

// proxyPolicyInputs is the resolved network-proxy state the dynamic Seatbelt
// network policy is built from. Mirrors the ProxyPolicyInputs struct.
type proxyPolicyInputs struct {
	ports             []uint16
	hasProxyConfig    bool
	allowLocalBinding bool
	udsKind           unixDomainSocketKind
	udsAllowed        []string
}

// isLoopbackHost mirrors is_loopback_host.
func isLoopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

// proxySchemeDefaultPort mirrors proxy_scheme_default_port.
func proxySchemeDefaultPort(scheme string) uint16 {
	switch scheme {
	case "https":
		return 443
	case "socks5", "socks5h", "socks4", "socks4a":
		return 1080
	default:
		return 80
	}
}

// proxyLoopbackPortsFromEnv extracts the distinct, sorted loopback proxy ports
// from the *_PROXY_URL environment variables. Mirrors
// proxy_loopback_ports_from_env (which collects into a BTreeSet, hence sorted).
func proxyLoopbackPortsFromEnv(env map[string]string) []uint16 {
	seen := make(map[uint16]struct{})
	for _, key := range networkproxy.ProxyURLEnvKeys {
		proxyURL, ok := networkproxy.ProxyURLEnvValue(env, key)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(proxyURL)
		if trimmed == "" {
			continue
		}

		candidate := trimmed
		if !strings.Contains(trimmed, "://") {
			candidate = "http://" + trimmed
		}
		parsed, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		host := parsed.Hostname()
		if host == "" || !isLoopbackHost(host) {
			continue
		}

		scheme := strings.ToLower(parsed.Scheme)
		port := proxySchemeDefaultPort(scheme)
		if portStr := parsed.Port(); portStr != "" {
			if parsedPort, perr := parsePort(portStr); perr == nil {
				port = parsedPort
			} else {
				continue
			}
		}
		seen[port] = struct{}{}
	}

	ports := make([]uint16, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	return ports
}

// parsePort parses a base-10 u16 port, rejecting out-of-range values.
func parsePort(s string) (uint16, error) {
	var v uint32
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errInvalidPort
		}
		v = v*10 + uint32(r-'0')
		if v > 0xffff {
			return 0, errInvalidPort
		}
	}
	return uint16(v), nil
}

// buildProxyPolicyInputs mirrors proxy_policy_inputs: it folds the running
// network proxy (if any) and any extra allowed unix-socket paths into the
// resolved policy inputs. extraAllowUnixSockets must be absolute paths.
func buildProxyPolicyInputs(network *networkproxy.NetworkProxy, extraAllowUnixSockets []string) proxyPolicyInputs {
	extraAllowed := make([]string, 0, len(extraAllowUnixSockets))
	for _, socket := range extraAllowUnixSockets {
		if normalized, ok := normalizePathForSandbox(socket); ok {
			extraAllowed = append(extraAllowed, normalized)
		}
	}

	if network == nil {
		return proxyPolicyInputs{
			udsKind:    unixDomainSocketRestricted,
			udsAllowed: extraAllowed,
		}
	}

	env := make(map[string]string)
	network.ApplyToEnv(env)
	settings := network.State().CurrentConfig().Network

	if settings.DangerouslyAllowAllUnixSockets {
		return proxyPolicyInputs{
			ports:             proxyLoopbackPortsFromEnv(env),
			hasProxyConfig:    networkproxy.HasProxyURLEnvVars(env),
			allowLocalBinding: settings.AllowLocalBinding,
			udsKind:           unixDomainSocketAllowAll,
		}
	}

	allowed := make([]string, 0, len(settings.AllowUnixSockets())+len(extraAllowed))
	for _, socket := range settings.AllowUnixSockets() {
		if normalized, ok := normalizePathForSandbox(socket); ok {
			allowed = append(allowed, normalized)
		}
		// Non-normalizable entries are skipped (the Rust port warns and drops them).
	}
	allowed = append(allowed, extraAllowed...)

	return proxyPolicyInputs{
		ports:             proxyLoopbackPortsFromEnv(env),
		hasProxyConfig:    networkproxy.HasProxyURLEnvVars(env),
		allowLocalBinding: settings.AllowLocalBinding,
		udsKind:           unixDomainSocketRestricted,
		udsAllowed:        allowed,
	}
}

// normalizePathForSandbox mirrors normalize_path_for_sandbox: it rejects relative
// paths, then canonicalizes (resolving symlinks) when possible, falling back to
// the lexically-normalized absolute path.
func normalizePathForSandbox(p string) (string, bool) {
	if !filepath.IsAbs(p) {
		return "", false
	}
	abs, err := abspath.FromAbsolutePath(p)
	if err != nil {
		return "", false
	}
	if canonical, err := filepath.EvalSymlinks(abs.Path()); err == nil {
		if canonAbs, err := abspath.FromAbsolutePath(canonical); err == nil {
			return canonAbs.Path(), true
		}
	}
	return abs.Path(), true
}

// unixSocketParam is a single allowlisted unix-socket path and its 0-based index.
type unixSocketParam struct {
	index int
	path  string
}

// unixSocketPathParams mirrors unix_socket_path_params: it dedups the allowlisted
// socket paths (sorted by string, as the Rust BTreeMap does) and assigns each a
// stable index. AllowAll yields no path params.
func (in proxyPolicyInputs) unixSocketPathParams() []unixSocketParam {
	if in.udsKind != unixDomainSocketRestricted {
		return nil
	}
	dedup := make(map[string]struct{}, len(in.udsAllowed))
	keys := make([]string, 0, len(in.udsAllowed))
	for _, p := range in.udsAllowed {
		if _, ok := dedup[p]; ok {
			continue
		}
		dedup[p] = struct{}{}
		keys = append(keys, p)
	}
	sort.Strings(keys)

	params := make([]unixSocketParam, 0, len(keys))
	for i, p := range keys {
		params = append(params, unixSocketParam{index: i, path: p})
	}
	return params
}

// unixSocketPathParamKey mirrors unix_socket_path_param_key.
func unixSocketPathParamKey(index int) string {
	return "UNIX_SOCKET_PATH_" + itoa(index)
}

// unixSocketDirParams mirrors unix_socket_dir_params: the (key, path) -D params
// for each allowlisted socket directory.
func (in proxyPolicyInputs) unixSocketDirParams() []dirParam {
	params := in.unixSocketPathParams()
	out := make([]dirParam, 0, len(params))
	for _, param := range params {
		out = append(out, dirParam{key: unixSocketPathParamKey(param.index), path: param.path})
	}
	return out
}

// unixSocketPolicy mirrors unix_socket_policy: zero or more complete Seatbelt
// policy lines for unix-socket rules. When non-empty the result is
// newline-terminated so callers can append it directly.
func (in proxyPolicyInputs) unixSocketPolicy() string {
	params := in.unixSocketPathParams()
	hasAccess := in.udsKind == unixDomainSocketAllowAll || len(params) > 0
	if !hasAccess {
		return ""
	}

	var b strings.Builder
	b.WriteString("(allow system-socket (socket-domain AF_UNIX))\n")
	if in.udsKind == unixDomainSocketAllowAll {
		b.WriteString("(allow network-bind (local unix-socket))\n")
		b.WriteString("(allow network-outbound (remote unix-socket))\n")
		return b.String()
	}

	for _, param := range params {
		key := unixSocketPathParamKey(param.index)
		b.WriteString("(allow network-bind (local unix-socket (subpath (param \"" + key + "\"))))\n")
		b.WriteString("(allow network-outbound (remote unix-socket (subpath (param \"" + key + "\"))))\n")
	}
	return b.String()
}

// hasSomeUnixSocketAccess reports whether any AF_UNIX traffic is permitted.
func (in proxyPolicyInputs) hasSomeUnixSocketAccess() bool {
	if in.udsKind == unixDomainSocketAllowAll {
		return true
	}
	return len(in.udsAllowed) > 0
}
