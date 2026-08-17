package api

import (
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/client"
)

func TestURLForPath(t *testing.T) {
	p := Provider{BaseURL: "https://api.openai.com/v1/"}
	if got := p.URLForPath("/responses"); got != "https://api.openai.com/v1/responses" {
		t.Fatalf("unexpected url: %s", got)
	}
	if got := p.URLForPath(""); got != "https://api.openai.com/v1" {
		t.Fatalf("unexpected empty-path url: %s", got)
	}
}

func TestURLForPathWithSortedQueryParams(t *testing.T) {
	p := Provider{
		BaseURL:     "https://example.com",
		QueryParams: map[string]string{"b": "2", "a": "1"},
	}
	got := p.URLForPath("responses")
	want := "https://example.com/responses?a=1&b=2"
	if got != want {
		t.Fatalf("unexpected url: %s want %s", got, want)
	}
}

func TestWebsocketURLForPath(t *testing.T) {
	tests := []struct{ base, want string }{
		{"https://example.com", "wss://example.com/responses"},
		{"http://example.com", "ws://example.com/responses"},
		{"wss://example.com", "wss://example.com/responses"},
	}
	for _, tt := range tests {
		p := Provider{BaseURL: tt.base}
		if got := p.WebsocketURLForPath("responses"); got != tt.want {
			t.Fatalf("base %s: got %s want %s", tt.base, got, tt.want)
		}
	}
}

func TestIsAzureResponsesProvider(t *testing.T) {
	positive := []string{
		"https://foo.openai.azure.com/openai",
		"https://foo.cognitiveservices.azure.cn/openai",
		"https://foo.aoai.azure.com/openai",
		"https://foo.openai.azure-api.net/openai",
		"https://foo.z01.azurefd.net/",
	}
	for _, base := range positive {
		if !IsAzureResponsesProvider("test", base) {
			t.Fatalf("expected %s detected as azure", base)
		}
	}
	if !IsAzureResponsesProvider("Azure", "https://example.com") {
		t.Fatalf("expected name azure detected")
	}
	negative := []string{
		"https://api.openai.com/v1",
		"https://example.com/openai",
		"https://myproxy.azurewebsites.net/openai",
	}
	for _, base := range negative {
		if IsAzureResponsesProvider("test", base) {
			t.Fatalf("expected %s NOT detected as azure", base)
		}
	}
}

func TestRetryConfigToPolicy(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 4, BaseDelay: 200 * time.Millisecond, Retry5xx: true, RetryTransport: true}
	policy := cfg.ToPolicy()
	if policy.MaxAttempts != 4 || policy.BaseDelay != 200*time.Millisecond {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	if policy.RetryOn.Retry429 || !policy.RetryOn.Retry5xx || !policy.RetryOn.RetryTransport {
		t.Fatalf("unexpected retry on: %+v", policy.RetryOn)
	}
}

func TestDefaultRetryConfigMatchesCodexDefaults(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != client.DefaultRequestMaxRetries {
		t.Fatalf("unexpected max attempts %d", cfg.MaxAttempts)
	}
	if cfg.Retry429 {
		t.Fatalf("codex default must not retry 429")
	}
}

func TestBuildRequestCopiesProviderHeaders(t *testing.T) {
	p := Provider{BaseURL: "https://example.com"}
	p.Headers = make(map[string][]string)
	p.Headers.Set("X-Provider", "v")
	req := p.BuildRequest("POST", "responses")
	req.Headers.Set("X-Provider", "mutated")
	if p.Headers.Get("X-Provider") != "v" {
		t.Fatalf("provider headers were mutated")
	}
}
