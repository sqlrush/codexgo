package networkproxy

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
)

// proxyForHTTP returns the upstream HTTP proxy for plain HTTP requests, reading
// HTTP_PROXY/ALL_PROXY (and lowercase aliases). Only http(s) proxy schemes are
// honored, matching codex's upstream selection for plain HTTP.
func proxyForHTTP() *url.URL {
	if p := readProxyEnv("HTTP_PROXY", "http_proxy"); p != nil {
		return p
	}
	return readProxyEnv("ALL_PROXY", "all_proxy")
}

// proxyForConnect returns the upstream HTTP proxy for CONNECT tunnels, preferring
// HTTPS_PROXY then HTTP_PROXY then ALL_PROXY (http schemes only).
func proxyForConnect() *url.URL {
	if p := readProxyEnv("HTTPS_PROXY", "https_proxy"); p != nil {
		return p
	}
	if p := readProxyEnv("HTTP_PROXY", "http_proxy"); p != nil {
		return p
	}
	return readProxyEnv("ALL_PROXY", "all_proxy")
}

func readProxyEnv(keys ...string) *url.URL {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil {
			continue
		}
		// Only honor http(s) upstream proxies; SOCKS upstreams are not supported
		// for upstream chaining here (matching codex's http-only filter).
		scheme := strings.ToLower(parsed.Scheme)
		if scheme == "http" || scheme == "https" || scheme == "" {
			if parsed.Host == "" {
				continue
			}
			return parsed
		}
	}
	return nil
}

func errIsNetClosed(err error) bool {
	return errors.Is(err, net.ErrClosed)
}
