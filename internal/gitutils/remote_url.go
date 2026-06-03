package gitutils

import (
	"strings"
)

// CanonicalizeGitRemoteURL normalizes a git remote URL into a canonical
// `host/owner/repo` form, returning false when the value does not look like a
// repository URL.
//
// Mirrors the Rust `canonicalize_git_remote_url`. GitHub paths are lowercased;
// the path for other hosts is preserved. The `.git` suffix and trailing slashes
// are stripped, default ports for known schemes are dropped, and SCP-like
// (`git@host:owner/repo`) forms are handled.
func CanonicalizeGitRemoteURL(url string) (string, bool) {
	// Rust: trim whitespace, then strip trailing '/', then strip a single
	// ".git" suffix.
	trimmed := trimGitSuffix(strings.TrimRight(strings.TrimSpace(url), "/"))
	if trimmed == "" {
		return "", false
	}

	if scheme, rest, ok := strings.Cut(trimmed, "://"); ok {
		return canonicalizeGitURLLikeRemote(scheme, rest)
	}

	if hostPart, path, ok := parseScpLikeRemote(trimmed); ok {
		return canonicalizeGitRemoteHostPath(hostPart, path, "")
	}

	hostPart, path, ok := strings.Cut(trimmed, "/")
	if !ok {
		return "", false
	}
	return canonicalizeGitRemoteHostPath(hostPart, path, "")
}

func canonicalizeGitURLLikeRemote(scheme, rest string) (string, bool) {
	var defaultPort string
	switch scheme {
	case "git":
		defaultPort = "9418"
	case "http":
		defaultPort = "80"
	case "https":
		defaultPort = "443"
	case "ssh":
		defaultPort = "22"
	default:
		return "", false
	}

	if idx := strings.IndexAny(rest, "?#"); idx >= 0 {
		rest = rest[:idx]
	}
	hostPart, path, ok := strings.Cut(rest, "/")
	if !ok {
		return "", false
	}
	return canonicalizeGitRemoteHostPath(hostPart, path, defaultPort)
}

// parseScpLikeRemote parses an SCP-like remote (`host:path`) into its host and
// path components. It returns false when the value contains a slash that
// precedes any colon (i.e. it looks like a normal path, not SCP form).
func parseScpLikeRemote(remote string) (string, string, bool) {
	slash := strings.IndexByte(remote, '/')
	colon := strings.IndexByte(remote, ':')
	if slash >= 0 {
		// Rust: reject when there is no colon, or the first slash is before the colon.
		if colon < 0 || slash < colon {
			return "", "", false
		}
	}

	hostPart, path, ok := strings.Cut(remote, ":")
	if !ok {
		return "", "", false
	}
	if hostPart == "" || path == "" {
		return "", "", false
	}
	return hostPart, path, true
}

func canonicalizeGitRemoteHostPath(hostPart, path, defaultPort string) (string, bool) {
	// Strip any user-info prefix (`user@`) by taking the segment after the last '@'.
	hostNoUser := hostPart
	if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
		hostNoUser = hostPart[at+1:]
	}
	hostNoUser = strings.TrimRight(strings.TrimSpace(hostNoUser), "/")
	host := normalizeRemoteHost(hostNoUser, defaultPort)
	if host == "" {
		return "", false
	}

	path = trimGitSuffix(strings.Trim(strings.TrimSpace(path), "/"))
	components := make([]string, 0)
	for _, component := range strings.Split(path, "/") {
		if component != "" {
			components = append(components, component)
		}
	}
	if len(components) < 2 {
		return "", false
	}
	owner, repo := components[0], components[1]
	if owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", false
	}
	joined := strings.Join(components, "/")

	if host == "github.com" {
		return host + "/" + strings.ToLower(joined), true
	}
	return host + "/" + joined, true
}

func normalizeRemoteHost(host, defaultPort string) string {
	host = strings.ToLower(host)
	if defaultPort != "" {
		if hostWithoutPort, port, ok := lastCut(host, ':'); ok && port == defaultPort {
			return hostWithoutPort
		}
	}
	return host
}

func trimGitSuffix(value string) string {
	return strings.TrimSuffix(value, ".git")
}

// lastCut splits s at the last occurrence of sep, mirroring Rust's `rsplit_once`.
func lastCut(s string, sep byte) (before, after string, found bool) {
	idx := strings.LastIndexByte(s, sep)
	if idx < 0 {
		return s, "", false
	}
	return s[:idx], s[idx+1:], true
}
