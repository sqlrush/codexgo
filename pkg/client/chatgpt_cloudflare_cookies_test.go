package client

import (
	"net/http"
	"net/url"
	"sort"
	"testing"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}

func cookieStrings(cookies []*http.Cookie) []string {
	out := make([]string, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, c.Name+"="+c.Value)
	}
	sort.Strings(out)
	return out
}

func TestStoresAndReturnsCloudflareCookiesForChatGPTHosts(t *testing.T) {
	jar := NewChatGptCloudflareCookieJar()
	u := mustURL(t, "https://chatgpt.com/backend-api/codex/responses")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "_cfuvid", Value: "visitor"},
		{Name: "cf_clearance", Value: "clearance"},
	})
	got := cookieStrings(jar.Cookies(u))
	want := []string{"_cfuvid=visitor", "cf_clearance=clearance"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestIgnoresNonChatGPTCookies(t *testing.T) {
	jar := NewChatGptCloudflareCookieJar()
	u := mustURL(t, "https://api.openai.com/v1/responses")
	jar.SetCookies(u, []*http.Cookie{{Name: "_cfuvid", Value: "visitor"}})
	if c := jar.Cookies(u); c != nil {
		t.Fatalf("expected nil, got %v", c)
	}
}

func TestIgnoresNonCloudflareCookiesForChatGPTHosts(t *testing.T) {
	jar := NewChatGptCloudflareCookieJar()
	u := mustURL(t, "https://chatgpt.com/backend-api/codex/responses")
	jar.SetCookies(u, []*http.Cookie{{Name: "__Secure-next-auth.session-token", Value: "secret"}})
	if c := jar.Cookies(u); c != nil {
		t.Fatalf("expected nil, got %v", c)
	}
}

func TestIgnoresMixedNonCloudflareCookies(t *testing.T) {
	jar := NewChatGptCloudflareCookieJar()
	u := mustURL(t, "https://chatgpt.com/backend-api/codex/responses")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "_cfuvid", Value: "visitor"},
		{Name: "chatgpt_session", Value: "secret"},
	})
	got := cookieStrings(jar.Cookies(u))
	if len(got) != 1 || got[0] != "_cfuvid=visitor" {
		t.Fatalf("got %v", got)
	}
}

func TestDoesNotReturnCookiesForOtherHosts(t *testing.T) {
	jar := NewChatGptCloudflareCookieJar()
	chatgpt := mustURL(t, "https://chatgpt.com/backend-api/codex/responses")
	api := mustURL(t, "https://api.openai.com/v1/responses")
	jar.SetCookies(chatgpt, []*http.Cookie{{Name: "_cfuvid", Value: "visitor"}})
	if c := jar.Cookies(api); c != nil {
		t.Fatalf("expected nil for other host, got %v", c)
	}
}

func TestRejectsPlainHTTPChatGPTCookieURLs(t *testing.T) {
	jar := NewChatGptCloudflareCookieJar()
	httpURL := mustURL(t, "http://chatgpt.com/backend-api/codex/responses")
	httpsURL := mustURL(t, "https://chatgpt.com/backend-api/codex/responses")
	jar.SetCookies(httpURL, []*http.Cookie{{Name: "_cfuvid", Value: "visitor"}})
	if c := jar.Cookies(httpURL); c != nil {
		t.Fatalf("expected nil for http url, got %v", c)
	}
	if c := jar.Cookies(httpsURL); c != nil {
		t.Fatalf("expected nil https since nothing was stored, got %v", c)
	}
}

func TestIsChatGPTCookieURLOnlyHTTPS(t *testing.T) {
	if isChatGPTCookieURL(mustURL(t, "http://chatgpt.com/x")) {
		t.Fatalf("http should not be a cookie url")
	}
	if isChatGPTCookieURL(mustURL(t, "wss://chatgpt.com/x")) {
		t.Fatalf("wss should not be a cookie url")
	}
}

func TestAllowsOnlyKnownCloudflareCookieNames(t *testing.T) {
	allowed := []string{
		"__cf_bm", "__cflb", "__cfruid", "__cfseq", "__cfwaitingroom",
		"_cfuvid", "cf_clearance", "cf_ob_info", "cf_use_ob", "cf_chl_rc_i",
	}
	for _, name := range allowed {
		if !isAllowedCloudflareCookieName(name) {
			t.Fatalf("expected %q allowed", name)
		}
	}
	denied := []string{"__Secure-next-auth.session-token", "chatgpt_session", "oai-auth-token", "not_cf_clearance"}
	for _, name := range denied {
		if isAllowedCloudflareCookieName(name) {
			t.Fatalf("expected %q denied", name)
		}
	}
}

func TestIsAllowedChatGPTHost(t *testing.T) {
	good := []string{
		"chatgpt.com", "foo.chatgpt.com", "staging.chatgpt.com",
		"chat.openai.com", "chatgpt-staging.com", "api.chatgpt-staging.com",
	}
	for _, h := range good {
		if !IsAllowedChatGPTHost(h) {
			t.Fatalf("expected %q allowed", h)
		}
	}
	bad := []string{"evilchatgpt.com", "chatgpt.com.evil.example", "api.openai.com", "foo.chat.openai.com"}
	for _, h := range bad {
		if IsAllowedChatGPTHost(h) {
			t.Fatalf("expected %q denied", h)
		}
	}
}
