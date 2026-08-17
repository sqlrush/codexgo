package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sqlrush/codexgo/internal/keyring"
	"github.com/sqlrush/codexgo/pkg/config"
)

func TestMcpAuthStatusString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status McpAuthStatus
		want   string
	}{
		{McpAuthStatusUnsupported, "Unsupported"},
		{McpAuthStatusNotLoggedIn, "Not logged in"},
		{McpAuthStatusBearerToken, "Bearer token"},
		{McpAuthStatusOAuth, "OAuth"},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()
			if got := tc.status.String(); got != tc.want {
				t.Fatalf("String()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestDiscoveryPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		base string
		want []string
	}{
		{
			name: "empty path",
			base: "",
			want: []string{"/.well-known/oauth-authorization-server"},
		},
		{
			name: "with path component",
			base: "/mcp",
			want: []string{
				"/.well-known/oauth-authorization-server/mcp",
				"/mcp/.well-known/oauth-authorization-server",
				"/.well-known/oauth-authorization-server",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := discoveryPaths(tc.base)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("path[%d]=%q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeScopes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "dedup and trim", in: []string{" read ", "read", "write", ""}, want: []string{"read", "write"}},
		{name: "all empty", in: []string{"", "   "}, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeScopes(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("scope[%d]=%q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDetermineAuthStatusBearerEnvVar(t *testing.T) {
	t.Parallel()
	store := NewOAuthStoreWith(keyring.NewMemoryStore(), t.TempDir())
	v := "TOKEN_VAR"
	status, err := DetermineStreamableHTTPAuthStatus(
		context.Background(), store, "srv", "https://example.com/mcp",
		&v, nil, nil, config.OAuthCredentialsStoreAuto,
	)
	if err != nil {
		t.Fatalf("DetermineStreamableHTTPAuthStatus: %v", err)
	}
	if status != McpAuthStatusBearerToken {
		t.Fatalf("status=%q want bearer_token", status)
	}
}

func TestDetermineAuthStatusStoredOAuth(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	store := NewOAuthStoreWith(keyring.NewMemoryStore(), home)
	tok := sampleTokens()
	if err := store.Save(tok, config.OAuthCredentialsStoreAuto); err != nil {
		t.Fatalf("Save: %v", err)
	}

	status, err := DetermineStreamableHTTPAuthStatus(
		context.Background(), store, tok.ServerName, tok.URL,
		nil, nil, nil, config.OAuthCredentialsStoreAuto,
	)
	if err != nil {
		t.Fatalf("DetermineStreamableHTTPAuthStatus: %v", err)
	}
	if status != McpAuthStatusOAuth {
		t.Fatalf("status=%q want oauth", status)
	}
}

func TestDiscoverStreamableHTTPOAuth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"authorization_endpoint":"https://auth.example/authorize",
				"token_endpoint":"https://auth.example/token",
				"scopes_supported":["read","write","read"]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	discovery, err := DiscoverStreamableHTTPOAuth(context.Background(), srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("DiscoverStreamableHTTPOAuth: %v", err)
	}
	if discovery == nil {
		t.Fatal("expected discovery result")
	}
	if len(discovery.ScopesSupported) != 2 {
		t.Fatalf("scopes=%v", discovery.ScopesSupported)
	}

	ok, err := SupportsOAuthLogin(context.Background(), srv.URL)
	if err != nil || !ok {
		t.Fatalf("SupportsOAuthLogin ok=%v err=%v", ok, err)
	}
}

func TestDiscoverStreamableHTTPOAuthUnsupported(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	discovery, err := DiscoverStreamableHTTPOAuth(context.Background(), srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("DiscoverStreamableHTTPOAuth: %v", err)
	}
	if discovery != nil {
		t.Fatalf("expected nil discovery, got %+v", discovery)
	}
}
