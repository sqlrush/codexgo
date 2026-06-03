package feedback

import (
	"reflect"
	"testing"
)

func TestCollectFromPairs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pairs map[string]string
		want  FeedbackDiagnostics
	}{
		{
			name: "reports_values_in_fixed_order",
			pairs: map[string]string{
				"HTTPS_PROXY": "https://user:password@secure-proxy.example.com:443?secret=1",
				"http_proxy":  "proxy.example.com:8080",
				"all_proxy":   "socks5h://all-proxy.example.com:1080",
			},
			want: FeedbackDiagnostics{diagnostics: []FeedbackDiagnostic{{
				Headline: "Proxy environment variables are set and may affect connectivity.",
				// Order follows proxyEnvVars: http_proxy, HTTPS_PROXY, all_proxy.
				Details: []string{
					"http_proxy = proxy.example.com:8080",
					"HTTPS_PROXY = https://user:password@secure-proxy.example.com:443?secret=1",
					"all_proxy = socks5h://all-proxy.example.com:1080",
				},
			}}},
		},
		{
			name:  "absent_values_yield_empty",
			pairs: map[string]string{},
			want:  FeedbackDiagnostics{},
		},
		{
			name:  "preserves_whitespace",
			pairs: map[string]string{"HTTP_PROXY": "  proxy with spaces  "},
			want: FeedbackDiagnostics{diagnostics: []FeedbackDiagnostic{{
				Headline: "Proxy environment variables are set and may affect connectivity.",
				Details:  []string{"HTTP_PROXY =   proxy with spaces  "},
			}}},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CollectFromPairs(tt.pairs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v want %#v", got, tt.want)
			}
		})
	}
}

func TestAttachmentText(t *testing.T) {
	t.Parallel()
	d := CollectFromPairs(map[string]string{
		"HTTP_PROXY":  "proxy.example.com:8080",
		"HTTPS_PROXY": "https://user:password@secure-proxy.example.com:443?secret=1",
		"all_proxy":   "socks5h://all-proxy.example.com:1080",
	})
	got, ok := d.AttachmentText()
	if !ok {
		t.Fatal("expected attachment text")
	}
	want := "Connectivity diagnostics\n\n" +
		"- Proxy environment variables are set and may affect connectivity.\n" +
		"  - HTTP_PROXY = proxy.example.com:8080\n" +
		"  - HTTPS_PROXY = https://user:password@secure-proxy.example.com:443?secret=1\n" +
		"  - all_proxy = socks5h://all-proxy.example.com:1080"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestAttachmentTextEmpty(t *testing.T) {
	t.Parallel()
	if _, ok := (FeedbackDiagnostics{}).AttachmentText(); ok {
		t.Error("empty diagnostics should not produce attachment text")
	}
}
