package login

import (
	"net/http"
	"os"
	"strings"

	"github.com/sqlrush/codexgo/internal/client"
)

// Originator constants mirror default_client.rs.
const (
	// DefaultOriginator is the originator header value for the Rust CLI.
	DefaultOriginator = "codex_cli_rs"
	// OriginatorOverrideEnvVar overrides the originator value.
	OriginatorOverrideEnvVar = "CODEXGO_INTERNAL_ORIGINATOR_OVERRIDE"
)

// originatorValue returns the originator string, honoring the override env var.
// Mirrors default_client::originator (the value field only; the Go port does not
// reproduce the global cache, which is purely an optimization).
func originatorValue() string {
	if v, ok := os.LookupEnv(OriginatorOverrideEnvVar); ok && v != "" {
		return v
	}
	return DefaultOriginator
}

// NewHTTPClient builds the default HTTP client used for login traffic, applying
// any custom CA bundle. It is the public entry point callers use to obtain the
// client passed to ExchangeCodeForTokens, RefreshChatgptToken, etc.
func NewHTTPClient() (*http.Client, error) {
	return newHTTPClient()
}

// newHTTPClient builds the default HTTP client used for login traffic. It
// applies any custom CA bundle configured via CODEXGO_CA_CERTIFICATE /
// SSL_CERT_FILE, mirroring default_client::build_reqwest_client. The originator
// header is added per-request by callers that need it.
func newHTTPClient() (*http.Client, error) {
	tlsConfig, err := client.BuildHTTPClientTLSConfig()
	if err != nil {
		return nil, err
	}
	if tlsConfig == nil {
		return &http.Client{}, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport}, nil
}

// trimTrailingSlash removes a single trailing slash group from a URL, mirroring
// the Rust trim_end_matches('/').
func trimTrailingSlash(s string) string {
	return strings.TrimRight(s, "/")
}

// urlEncode percent-encodes a string exactly like the Rust `urlencoding::encode`
// crate used throughout server.rs: every byte is percent-encoded except the
// RFC 3986 unreserved set (A-Z a-z 0-9 - _ . ~). In particular spaces become
// "%20" (NOT "+" as Go's url.QueryEscape would produce), which is required for
// byte-for-byte parity with the codex authorize/token/success URLs.
func urlEncode(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isURLEncodeUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperhex[c>>4])
		b.WriteByte(upperhex[c&0x0f])
	}
	return b.String()
}

// isURLEncodeUnreserved reports whether b is in the RFC 3986 unreserved set that
// the `urlencoding` crate leaves untouched.
func isURLEncodeUnreserved(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z':
		return true
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_' || b == '.' || b == '~':
		return true
	default:
		return false
	}
}
