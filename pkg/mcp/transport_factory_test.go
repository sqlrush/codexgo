package mcp

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/keyring"
)

func TestDefaultFactoryUnknownTransport(t *testing.T) {
	t.Parallel()
	factory := NewDefaultTransportFactory(nil, config.OAuthCredentialsStoreAuto, nil, fakeEnv(nil))
	cfg := config.McpServerConfig{
		Transport: config.McpServerTransportConfig{Kind: config.McpServerTransportKind(99)},
	}
	if _, err := factory.NewTransport(context.Background(), "srv", cfg, ""); err == nil {
		t.Fatal("expected error for unknown transport kind")
	}
}

func TestDefaultFactoryStdioTransport(t *testing.T) {
	t.Parallel()
	factory := NewDefaultTransportFactory(nil, config.OAuthCredentialsStoreAuto, nil, fakeEnv(map[string]string{"PATH": "/usr/bin"}))
	cfg := config.McpServerConfig{
		Transport: config.McpServerTransportConfig{
			Kind:    config.McpTransportStdio,
			Command: "/bin/sh",
			Args:    []string{"-c", "true"},
		},
	}
	tr, err := factory.NewTransport(context.Background(), "srv", cfg, t.TempDir())
	if err != nil {
		t.Fatalf("NewTransport (stdio): %v", err)
	}
	if tr == nil {
		t.Fatal("nil transport")
	}
	_ = tr.Close()
}

func TestDefaultFactoryHTTPTransportUsesStoredToken(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	store := NewOAuthStoreWith(keyring.NewMemoryStore(), home)
	tok := sampleTokens()
	if err := store.Save(tok, config.OAuthCredentialsStoreAuto); err != nil {
		t.Fatalf("Save: %v", err)
	}

	factory := NewDefaultTransportFactory(store, config.OAuthCredentialsStoreAuto, nil, fakeEnv(nil))
	cfg := config.McpServerConfig{
		Transport: config.McpServerTransportConfig{
			Kind: config.McpTransportStreamableHTTP,
			URL:  tok.URL,
		},
	}
	tr, err := factory.NewTransport(context.Background(), tok.ServerName, cfg, "")
	if err != nil {
		t.Fatalf("NewTransport (http): %v", err)
	}
	defer tr.Close()

	ht, ok := tr.(*httpTransport)
	if !ok {
		t.Fatalf("expected *httpTransport, got %T", tr)
	}
	if ht.bearerToken != tok.TokenResponse.AccessToken {
		t.Fatalf("bearer token=%q want stored access token %q", ht.bearerToken, tok.TokenResponse.AccessToken)
	}
}

func TestDefaultFactoryHTTPBearerEnvVarMissing(t *testing.T) {
	t.Parallel()
	factory := NewDefaultTransportFactory(nil, config.OAuthCredentialsStoreAuto, nil, fakeEnv(nil))
	v := "MISSING_TOKEN"
	cfg := config.McpServerConfig{
		Transport: config.McpServerTransportConfig{
			Kind:              config.McpTransportStreamableHTTP,
			URL:               "https://example.com/mcp",
			BearerTokenEnvVar: &v,
		},
	}
	if _, err := factory.NewTransport(context.Background(), "srv", cfg, ""); err == nil {
		t.Fatal("expected error for missing bearer token env var")
	}
}

func TestDereferenceMap(t *testing.T) {
	t.Parallel()
	if got := dereferenceMap(nil); got != nil {
		t.Fatalf("nil map: got %v", got)
	}
	m := map[string]string{"a": "b"}
	if got := dereferenceMap(&m); got["a"] != "b" {
		t.Fatalf("deref: got %v", got)
	}
}
