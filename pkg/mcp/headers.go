package mcp

import (
	"net/http"
	"net/textproto"
	"os"
	"strings"
)

// BuildHTTPHeaders assembles the default header set for a streamable HTTP MCP
// server. It mirrors the reference `build_default_headers`:
//
//   - Static entries from httpHeaders are added verbatim, skipping any whose
//     name or value is invalid.
//   - For each entry in envHTTPHeaders, the named environment variable is read;
//     present, non-blank values are added under the header name, skipping blank
//     values and invalid names.
//
// lookup may be nil to read the process environment. The returned header is a
// fresh value the caller owns.
func BuildHTTPHeaders(httpHeaders, envHTTPHeaders map[string]string, lookup EnvLookup) http.Header {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	headers := http.Header{}

	for name, value := range httpHeaders {
		if !validHeaderName(name) || !validHeaderValue(value) {
			continue
		}
		headers.Set(textproto.CanonicalMIMEHeaderKey(name), value)
	}

	for name, envVar := range envHTTPHeaders {
		value, ok := lookup(envVar)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		if !validHeaderName(name) || !validHeaderValue(value) {
			continue
		}
		headers.Set(textproto.CanonicalMIMEHeaderKey(name), value)
	}

	return headers
}

// validHeaderName reports whether name is a non-empty RFC 7230 token suitable
// for use as an HTTP header field name.
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r > 0x7f {
			return false
		}
		if !isTokenChar(byte(r)) {
			return false
		}
	}
	return true
}

// validHeaderValue reports whether value contains only bytes permitted in an
// HTTP header field value (no control characters except horizontal tab).
func validHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b == '\t' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

// isTokenChar reports whether b is a valid RFC 7230 token character.
func isTokenChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
