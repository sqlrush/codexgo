package cli

import (
	"context"
	"net/http"
	"testing"

	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// clearAuthEnv clears every environment variable that could inject credentials so
// the resolver tests are deterministic regardless of the host environment.
func clearAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEX_API_KEY", "")
	t.Setenv("CODEX_ACCESS_TOKEN", "")
}

func TestLoginAuthResolverNoCredentials(t *testing.T) {
	clearAuthEnv(t)
	codexHome := t.TempDir()

	resolver := newLoginAuthResolver(authResolverConfig{
		CodexHome:            codexHome,
		StoreMode:            config.AuthCredentialsStoreFile,
		EnableCodexAPIKeyEnv: true,
		HTTPClient:           http.DefaultClient,
	})

	resolved, err := resolver.Resolve(context.Background(), protocol.NewThreadID("t"), core.SessionConfiguration{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.HasCredentials {
		t.Fatal("HasCredentials = true, want false with no stored auth and no env keys")
	}
	if resolved.AuthProvider != nil {
		t.Fatal("AuthProvider should be nil when no credentials are present")
	}
}

func TestLoginAuthResolverStoredAPIKey(t *testing.T) {
	clearAuthEnv(t)
	codexHome := t.TempDir()
	const apiKey = "sk-test-stored-key"

	if err := login.LoginWithAPIKey(codexHome, apiKey, config.AuthCredentialsStoreFile); err != nil {
		t.Fatalf("LoginWithAPIKey: %v", err)
	}

	resolver := newLoginAuthResolver(authResolverConfig{
		CodexHome:            codexHome,
		StoreMode:            config.AuthCredentialsStoreFile,
		EnableCodexAPIKeyEnv: true,
		HTTPClient:           http.DefaultClient,
	})

	resolved, err := resolver.Resolve(context.Background(), protocol.NewThreadID("t"), core.SessionConfiguration{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resolved.HasCredentials {
		t.Fatal("HasCredentials = false, want true with a stored API key")
	}
	if resolved.AuthProvider == nil {
		t.Fatal("AuthProvider is nil, want a bearer provider")
	}

	headers := http.Header{}
	resolved.AuthProvider.AddAuthHeaders(headers)
	if got := headers.Get("Authorization"); got != "Bearer "+apiKey {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer "+apiKey)
	}
}

func TestLoginAuthResolverEnvAPIKey(t *testing.T) {
	clearAuthEnv(t)
	codexHome := t.TempDir()
	const apiKey = "sk-test-env-key"
	t.Setenv("OPENAI_API_KEY", apiKey)

	resolver := newLoginAuthResolver(authResolverConfig{
		CodexHome:            codexHome,
		StoreMode:            config.AuthCredentialsStoreFile,
		EnableCodexAPIKeyEnv: true,
		HTTPClient:           http.DefaultClient,
	})

	resolved, err := resolver.Resolve(context.Background(), protocol.NewThreadID("t"), core.SessionConfiguration{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resolved.HasCredentials {
		t.Fatal("HasCredentials = false, want true when OPENAI_API_KEY is set")
	}
	headers := http.Header{}
	resolved.AuthProvider.AddAuthHeaders(headers)
	if got := headers.Get("Authorization"); got != "Bearer "+apiKey {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer "+apiKey)
	}
}

func TestCodexAuthProviderHeaders(t *testing.T) {
	provider, err := newCodexAuthProvider(login.FromAPIKey("sk-abc"))
	if err != nil {
		t.Fatalf("newCodexAuthProvider: %v", err)
	}

	// ApplyAuth must not mutate the caller's request headers.
	req := client.NewRequest(http.MethodPost, "https://example.com/v1/responses")
	out, authErr := provider.ApplyAuth(context.Background(), req)
	if authErr != nil {
		t.Fatalf("ApplyAuth: %v", authErr)
	}
	if out.Headers.Get("Authorization") != "Bearer sk-abc" {
		t.Fatalf("Authorization = %q, want %q", out.Headers.Get("Authorization"), "Bearer sk-abc")
	}
	if req.Headers.Get("Authorization") != "" {
		t.Fatal("ApplyAuth mutated the caller's headers")
	}
}

func TestHasAnyCredentials(t *testing.T) {
	clearAuthEnv(t)
	codexHome := t.TempDir()
	cfg := authResolverConfig{
		CodexHome:            codexHome,
		StoreMode:            config.AuthCredentialsStoreFile,
		EnableCodexAPIKeyEnv: true,
	}

	if hasAnyCredentials(cfg) {
		t.Fatal("hasAnyCredentials = true, want false with no credentials")
	}

	if err := login.LoginWithAPIKey(codexHome, "sk-stored", config.AuthCredentialsStoreFile); err != nil {
		t.Fatalf("LoginWithAPIKey: %v", err)
	}
	if !hasAnyCredentials(cfg) {
		t.Fatal("hasAnyCredentials = false, want true after storing an API key")
	}
}
