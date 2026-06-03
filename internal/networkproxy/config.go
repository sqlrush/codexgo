package networkproxy

import (
	"sort"
)

// NetworkMode controls which HTTP methods are permitted and whether HTTPS
// CONNECT/SOCKS5 are usable.
type NetworkMode string

const (
	// NetworkModeLimited grants read-only access: only GET/HEAD/OPTIONS for
	// HTTP. HTTPS CONNECT requires MITM; SOCKS5 is blocked.
	NetworkModeLimited NetworkMode = "limited"
	// NetworkModeFull grants full network access: all HTTP methods, direct
	// HTTPS CONNECT tunnels.
	NetworkModeFull NetworkMode = "full"
)

// DefaultNetworkMode is the mode used when unset, matching codex (Full).
const DefaultNetworkMode = NetworkModeFull

// AllowsMethod reports whether the mode permits the given HTTP method.
func (m NetworkMode) AllowsMethod(method string) bool {
	switch m {
	case NetworkModeLimited:
		return method == "GET" || method == "HEAD" || method == "OPTIONS"
	default: // Full and unset both allow everything.
		return true
	}
}

func (m NetworkMode) normalized() NetworkMode {
	if m == "" {
		return DefaultNetworkMode
	}
	return m
}

// NetworkDomainPermission is the allow/deny disposition for a domain pattern.
// The ordering None < Allow < Deny encodes effective precedence so deny wins
// over allow when entries for the same pattern conflict.
type NetworkDomainPermission int

const (
	// DomainPermissionNone is the default (no opinion) disposition.
	DomainPermissionNone NetworkDomainPermission = iota
	// DomainPermissionAllow allows matching hosts.
	DomainPermissionAllow
	// DomainPermissionDeny denies matching hosts (wins over allow).
	DomainPermissionDeny
)

// NetworkDomainPermissionEntry is a single ordered domain rule.
type NetworkDomainPermissionEntry struct {
	Pattern    string
	Permission NetworkDomainPermission
}

// NetworkDomainPermissions is an ordered list of domain rules. Duplicate
// patterns are collapsed to their highest-precedence permission.
type NetworkDomainPermissions struct {
	Entries []NetworkDomainPermissionEntry
}

// effectiveEntries collapses duplicate patterns to the highest permission,
// preserving first-seen order, mirroring Rust's `effective_entries`.
func (d NetworkDomainPermissions) effectiveEntries() []NetworkDomainPermissionEntry {
	var order []string
	effective := make(map[string]NetworkDomainPermission)
	for _, entry := range d.Entries {
		if _, ok := effective[entry.Pattern]; !ok {
			order = append(order, entry.Pattern)
			effective[entry.Pattern] = entry.Permission
		} else if entry.Permission > effective[entry.Pattern] {
			effective[entry.Pattern] = entry.Permission
		}
	}
	out := make([]NetworkDomainPermissionEntry, 0, len(order))
	for _, pattern := range order {
		out = append(out, NetworkDomainPermissionEntry{Pattern: pattern, Permission: effective[pattern]})
	}
	return out
}

// NetworkUnixSocketPermission is the disposition for a unix-socket path.
type NetworkUnixSocketPermission string

const (
	// UnixSocketPermissionAllow permits proxying to the socket path.
	UnixSocketPermissionAllow NetworkUnixSocketPermission = "allow"
	// UnixSocketPermissionDeny rejects the socket path.
	UnixSocketPermissionDeny NetworkUnixSocketPermission = "deny"
)

// NetworkUnixSocketPermissions maps absolute socket paths to dispositions.
type NetworkUnixSocketPermissions struct {
	Entries map[string]NetworkUnixSocketPermission
}

// NetworkProxySettings is the network policy configuration. The zero value is
// not the default; use DefaultNetworkProxySettings.
type NetworkProxySettings struct {
	Enabled                          bool
	ProxyURL                         string
	EnableSocks5                     bool
	SocksURL                         string
	EnableSocks5UDP                  bool
	AllowUpstreamProxy               bool
	DangerouslyAllowNonLoopbackProxy bool
	DangerouslyAllowAllUnixSockets   bool
	Mode                             NetworkMode
	Domains                          *NetworkDomainPermissions
	UnixSockets                      *NetworkUnixSocketPermissions
	AllowLocalBinding                bool
	Mitm                             bool
	MitmHooks                        []MitmHookConfig
}

const (
	defaultProxyURL = "http://127.0.0.1:3128"
	defaultSocksURL = "http://127.0.0.1:8081"
)

// DefaultNetworkProxySettings returns the codex baseline defaults: disabled,
// SOCKS5 enabled, upstream-proxy honoring, default-deny domains.
func DefaultNetworkProxySettings() NetworkProxySettings {
	return NetworkProxySettings{
		Enabled:                          false,
		ProxyURL:                         defaultProxyURL,
		EnableSocks5:                     true,
		SocksURL:                         defaultSocksURL,
		EnableSocks5UDP:                  true,
		AllowUpstreamProxy:               true,
		DangerouslyAllowNonLoopbackProxy: false,
		DangerouslyAllowAllUnixSockets:   false,
		Mode:                             DefaultNetworkMode,
		Domains:                          nil,
		UnixSockets:                      nil,
		AllowLocalBinding:                false,
		Mitm:                             false,
		MitmHooks:                        nil,
	}
}

// NetworkProxyConfig wraps the network settings.
type NetworkProxyConfig struct {
	Network NetworkProxySettings
}

// AllowedDomains returns the allow-permission patterns, or nil if none.
func (s NetworkProxySettings) AllowedDomains() []string {
	return s.domainEntries(DomainPermissionAllow)
}

// DeniedDomains returns the deny-permission patterns, or nil if none.
func (s NetworkProxySettings) DeniedDomains() []string {
	return s.domainEntries(DomainPermissionDeny)
}

func (s NetworkProxySettings) domainEntries(permission NetworkDomainPermission) []string {
	if s.Domains == nil {
		return nil
	}
	var out []string
	for _, entry := range s.Domains.effectiveEntries() {
		if entry.Permission == permission {
			out = append(out, entry.Pattern)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AllowUnixSockets returns the allow-permission unix socket paths.
func (s NetworkProxySettings) AllowUnixSockets() []string {
	if s.UnixSockets == nil {
		return nil
	}
	var out []string
	for path, permission := range s.UnixSockets.Entries {
		if permission == UnixSocketPermissionAllow {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// WithAllowedDomains returns a copy with the allow entries replaced.
func (s NetworkProxySettings) WithAllowedDomains(domains []string) NetworkProxySettings {
	return s.withDomainEntries(domains, DomainPermissionAllow)
}

// WithDeniedDomains returns a copy with the deny entries replaced.
func (s NetworkProxySettings) WithDeniedDomains(domains []string) NetworkProxySettings {
	return s.withDomainEntries(domains, DomainPermissionDeny)
}

func (s NetworkProxySettings) withDomainEntries(entries []string, permission NetworkDomainPermission) NetworkProxySettings {
	domains := s.cloneDomains()
	kept := domains.Entries[:0:0]
	for _, e := range domains.Entries {
		if e.Permission != permission {
			kept = append(kept, e)
		}
	}
	for _, entry := range entries {
		exists := false
		for _, e := range kept {
			if e.Pattern == entry && e.Permission == permission {
				exists = true
				break
			}
		}
		if !exists {
			kept = append(kept, NetworkDomainPermissionEntry{Pattern: entry, Permission: permission})
		}
	}
	out := s
	if len(kept) == 0 {
		out.Domains = nil
	} else {
		out.Domains = &NetworkDomainPermissions{Entries: kept}
	}
	return out
}

// WithUpsertDomainPermission returns a copy with the host's permission set,
// replacing any prior entry whose normalized pattern matches. Mirrors Rust's
// `upsert_domain_permission`.
func (s NetworkProxySettings) WithUpsertDomainPermission(host string, permission NetworkDomainPermission) NetworkProxySettings {
	domains := s.cloneDomains()
	normalizedHost := NormalizeHost(host)
	kept := domains.Entries[:0:0]
	for _, e := range domains.Entries {
		if NormalizeHost(e.Pattern) != normalizedHost {
			kept = append(kept, e)
		}
	}
	kept = append(kept, NetworkDomainPermissionEntry{Pattern: host, Permission: permission})
	out := s
	out.Domains = &NetworkDomainPermissions{Entries: kept}
	return out
}

// WithAllowUnixSockets returns a copy with the allow unix-socket entries
// replaced.
func (s NetworkProxySettings) WithAllowUnixSockets(paths []string) NetworkProxySettings {
	entries := make(map[string]NetworkUnixSocketPermission)
	if s.UnixSockets != nil {
		for path, perm := range s.UnixSockets.Entries {
			if perm != UnixSocketPermissionAllow {
				entries[path] = perm
			}
		}
	}
	for _, path := range paths {
		entries[path] = UnixSocketPermissionAllow
	}
	out := s
	if len(entries) == 0 {
		out.UnixSockets = nil
	} else {
		out.UnixSockets = &NetworkUnixSocketPermissions{Entries: entries}
	}
	return out
}

func (s NetworkProxySettings) cloneDomains() NetworkDomainPermissions {
	if s.Domains == nil {
		return NetworkDomainPermissions{}
	}
	entries := make([]NetworkDomainPermissionEntry, len(s.Domains.Entries))
	copy(entries, s.Domains.Entries)
	return NetworkDomainPermissions{Entries: entries}
}
