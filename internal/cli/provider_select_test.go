package cli

import (
	"context"
	"net/http"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// strp returns a pointer to s, used to build optional config fields in tests.
func strp(s string) *string { return &s }

func TestResolveSelectedProviderDefaultsToOpenAI(t *testing.T) {
	selected, err := resolveSelectedProvider(loadedConfig{})
	if err != nil {
		t.Fatalf("resolveSelectedProvider: %v", err)
	}
	if selected.ID != modelproviderinfo.OpenAIProviderID {
		t.Fatalf("ID = %q, want %q", selected.ID, modelproviderinfo.OpenAIProviderID)
	}
	if !selected.Info.RequiresOpenAIAuth {
		t.Fatal("default provider should require OpenAI auth")
	}
	if !isOpenAIAuthProvider(selected.Info) {
		t.Fatal("isOpenAIAuthProvider should be true for the built-in OpenAI provider")
	}
}

func TestResolveSelectedProviderCustomEnvKey(t *testing.T) {
	base := "https://example.test/v1"
	cfg := loadedConfig{
		ModelProviderID: strp("custom"),
		ModelProviders: map[string]modelproviderinfo.ModelProviderInfo{
			"custom": {
				Name:               "custom",
				BaseURL:            &base,
				EnvKey:             strp("CUSTOM_API_KEY"),
				WireApi:            modelproviderinfo.WireApiResponses,
				RequiresOpenAIAuth: false,
			},
		},
	}

	selected, err := resolveSelectedProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSelectedProvider: %v", err)
	}
	if selected.ID != "custom" {
		t.Fatalf("ID = %q, want %q", selected.ID, "custom")
	}
	if selected.Info.BaseURL == nil || *selected.Info.BaseURL != base {
		t.Fatalf("BaseURL = %v, want %q", selected.Info.BaseURL, base)
	}
	if isOpenAIAuthProvider(selected.Info) {
		t.Fatal("custom env_key provider should not use the OpenAI auth path")
	}
	if !envKeyDefined(selected.Info) {
		t.Fatal("custom provider declares an env_key")
	}
}

func TestResolveSelectedProviderUnknownID(t *testing.T) {
	cfg := loadedConfig{ModelProviderID: strp("nope")}
	if _, err := resolveSelectedProvider(cfg); err == nil {
		t.Fatal("expected error for unknown provider id, got nil")
	}
}

func TestResolveSelectedProviderOpenAIBaseURLOverride(t *testing.T) {
	override := "https://proxy.test/v1"
	cfg := loadedConfig{OpenAIBaseURL: &override}

	selected, err := resolveSelectedProvider(cfg)
	if err != nil {
		t.Fatalf("resolveSelectedProvider: %v", err)
	}
	if selected.Info.BaseURL == nil || *selected.Info.BaseURL != override {
		t.Fatalf("BaseURL = %v, want %q", selected.Info.BaseURL, override)
	}
}

func TestEnvKeyAuthResolverPresent(t *testing.T) {
	const key = "secret-token"
	t.Setenv("CUSTOM_API_KEY", key)

	resolver := &envKeyAuthResolver{info: modelproviderinfo.ModelProviderInfo{
		Name:    "custom",
		EnvKey:  strp("CUSTOM_API_KEY"),
		WireApi: modelproviderinfo.WireApiResponses,
	}}

	resolved, err := resolver.Resolve(context.Background(), protocol.NewThreadID("t"), core.SessionConfiguration{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resolved.HasCredentials {
		t.Fatal("HasCredentials = false, want true when env_key value is set")
	}
	if resolved.AuthMode == nil || *resolved.AuthMode != appserverproto.AuthModeApiKey {
		t.Fatalf("AuthMode = %v, want apikey", resolved.AuthMode)
	}
	headers := http.Header{}
	resolved.AuthProvider.AddAuthHeaders(headers)
	if got := headers.Get("Authorization"); got != "Bearer "+key {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer "+key)
	}
}

func TestEnvKeyAuthResolverMissingFallsBackToMock(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "")

	resolver := &envKeyAuthResolver{info: modelproviderinfo.ModelProviderInfo{
		Name:    "custom",
		EnvKey:  strp("CUSTOM_API_KEY"),
		WireApi: modelproviderinfo.WireApiResponses,
	}}

	resolved, err := resolver.Resolve(context.Background(), protocol.NewThreadID("t"), core.SessionConfiguration{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.HasCredentials {
		t.Fatal("HasCredentials = true, want false when env_key value is missing")
	}
}

func TestBuildProviderAuthResolverSelection(t *testing.T) {
	openai, err := resolveSelectedProvider(loadedConfig{})
	if err != nil {
		t.Fatalf("resolveSelectedProvider(openai): %v", err)
	}
	if _, ok := buildProviderAuthResolver(loadedConfig{}, openai); !ok {
		t.Fatal("OpenAI provider should yield a credential resolver")
	}

	base := "https://example.test/v1"
	customCfg := loadedConfig{
		ModelProviderID: strp("custom"),
		ModelProviders: map[string]modelproviderinfo.ModelProviderInfo{
			"custom": {
				Name:               "custom",
				BaseURL:            &base,
				EnvKey:             strp("CUSTOM_API_KEY"),
				WireApi:            modelproviderinfo.WireApiResponses,
				RequiresOpenAIAuth: false,
			},
		},
	}
	custom, err := resolveSelectedProvider(customCfg)
	if err != nil {
		t.Fatalf("resolveSelectedProvider(custom): %v", err)
	}
	if _, ok := buildProviderAuthResolver(customCfg, custom); !ok {
		t.Fatal("custom env_key provider should yield a credential resolver")
	}

	// A custom provider with neither requires_openai_auth nor an env_key has no
	// credential source, so no resolver applies (mock fallback).
	noAuthCfg := loadedConfig{
		ModelProviderID: strp("oss"),
		ModelProviders: map[string]modelproviderinfo.ModelProviderInfo{
			"oss": {
				Name:               "oss",
				BaseURL:            &base,
				WireApi:            modelproviderinfo.WireApiResponses,
				RequiresOpenAIAuth: false,
			},
		},
	}
	oss, err := resolveSelectedProvider(noAuthCfg)
	if err != nil {
		t.Fatalf("resolveSelectedProvider(oss): %v", err)
	}
	if _, ok := buildProviderAuthResolver(noAuthCfg, oss); ok {
		t.Fatal("provider without env_key or OpenAI auth should not yield a resolver")
	}
}

func TestResolveDefaultModelPrecedence(t *testing.T) {
	t.Setenv("CODEX_MODEL", "env-model")
	if got := resolveDefaultModel("config-model"); got != "config-model" {
		t.Fatalf("config model should win: got %q", got)
	}
	if got := resolveDefaultModel(""); got != "env-model" {
		t.Fatalf("CODEX_MODEL should win over mock: got %q", got)
	}
	t.Setenv("CODEX_MODEL", "")
	if got := resolveDefaultModel(""); got != defaultMockModelSlug {
		t.Fatalf("mock slug should be the final fallback: got %q", got)
	}
}
