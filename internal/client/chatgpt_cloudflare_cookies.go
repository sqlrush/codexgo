package client

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// ChatGPTCloudflareCookieJar is an http.CookieJar that only ever stores and
// returns the small allowlist of Cloudflare infrastructure cookies, and only for
// ChatGPT hosts over HTTPS. It mirrors the Rust `ChatGptCloudflareCookieStore`.
//
// WARNING: like the Rust store, this must only ever contain Cloudflare
// infrastructure cookies. It must never persist ChatGPT account, session, auth,
// or other user-specific cookie data.
type ChatGptCloudflareCookieJar struct {
	mu sync.Mutex
	// cookies maps a normalized host to that host's stored Cloudflare cookies,
	// keyed by cookie name. Cookies are scoped per registrable host because the
	// allowlist guarantees only Cloudflare service cookies are ever stored.
	cookies map[string]map[string]string
}

// NewChatGptCloudflareCookieJar returns an empty Cloudflare-only cookie jar.
func NewChatGptCloudflareCookieJar() *ChatGptCloudflareCookieJar {
	return &ChatGptCloudflareCookieJar{cookies: map[string]map[string]string{}}
}

// SetCookies stores only allowlisted Cloudflare cookies for ChatGPT HTTPS URLs.
// It implements http.CookieJar.
func (j *ChatGptCloudflareCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if !isChatGPTCookieURL(u) {
		return
	}
	host := u.Hostname()

	j.mu.Lock()
	defer j.mu.Unlock()

	store := j.cookies[host]
	if store == nil {
		store = map[string]string{}
		j.cookies[host] = store
	}
	for _, c := range cookies {
		if c == nil {
			continue
		}
		name := strings.TrimSpace(c.Name)
		if name == "" || !isAllowedCloudflareCookieName(name) {
			continue
		}
		store[name] = c.Value
	}
}

// Cookies returns only allowlisted Cloudflare cookies for ChatGPT HTTPS URLs. It
// implements http.CookieJar.
func (j *ChatGptCloudflareCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if !isChatGPTCookieURL(u) {
		return nil
	}
	host := u.Hostname()

	j.mu.Lock()
	defer j.mu.Unlock()

	store := j.cookies[host]
	if len(store) == 0 {
		return nil
	}
	out := make([]*http.Cookie, 0, len(store))
	for name, value := range store {
		if !isAllowedCloudflareCookieName(name) {
			continue
		}
		out = append(out, &http.Cookie{Name: name, Value: value})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isChatGPTCookieURL reports whether u is an HTTPS URL for an allowed ChatGPT
// host. It mirrors the Rust `is_chatgpt_cookie_url`.
func isChatGPTCookieURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	return IsAllowedChatGPTHost(host)
}

// allowedCloudflareCookieNames is the exact-match Cloudflare service cookie
// allowlist from the Rust `is_allowed_cloudflare_cookie_name`.
var allowedCloudflareCookieNames = map[string]struct{}{
	"__cf_bm":         {},
	"__cflb":          {},
	"__cfruid":        {},
	"__cfseq":         {},
	"__cfwaitingroom": {},
	"_cfuvid":         {},
	"cf_clearance":    {},
	"cf_ob_info":      {},
	"cf_use_ob":       {},
}

// isAllowedCloudflareCookieName reports whether name is one of the documented
// Cloudflare service cookies (or a cf_chl_ challenge cookie). It mirrors the Rust
// `is_allowed_cloudflare_cookie_name`.
func isAllowedCloudflareCookieName(name string) bool {
	if _, ok := allowedCloudflareCookieNames[name]; ok {
		return true
	}
	return strings.HasPrefix(name, "cf_chl_")
}
