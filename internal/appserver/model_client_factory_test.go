package appserver

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/client"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
	"github.com/sqlrush/codexgo/pkg/modelsmanager"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// staticAuthResolver is a test AuthResolver returning a fixed ResolvedAuth (or
// error) for every spawn.
type staticAuthResolver struct {
	resolved ResolvedAuth
	err      error
}

func (s staticAuthResolver) Resolve(context.Context, protocol.ThreadID, core.SessionConfiguration) (ResolvedAuth, error) {
	return s.resolved, s.err
}

// fakeAuthProvider is a no-op api.AuthProvider used to satisfy the real-client
// path without performing network I/O.
type fakeAuthProvider struct{}

func (fakeAuthProvider) AddAuthHeaders(http.Header) {}
func (fakeAuthProvider) ApplyAuth(_ context.Context, req client.Request) (client.Request, *api.AuthError) {
	return req, nil
}

// sentinelFallback returns a recognizable mock client and records whether it was
// invoked.
func sentinelFallback(called *bool) ModelClientFactory {
	return func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
		*called = true
		slug := cfg.Model()
		if slug == "" {
			slug = "gpt-mock"
		}
		return core.NewMockModelClient(slug, nil), nil
	}
}

func apiKeyMode() *appserverproto.AuthMode {
	m := appserverproto.AuthModeApiKey
	return &m
}

func TestNewModelClientFactoryValidatesConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  RealModelClientFactoryConfig
	}{
		{
			name: "missing auth resolver",
			cfg: RealModelClientFactoryConfig{
				Fallback: func(context.Context, protocol.ThreadID, core.SessionConfiguration) (core.ModelClient, error) {
					return nil, nil
				},
			},
		},
		{
			name: "missing fallback",
			cfg: RealModelClientFactoryConfig{
				AuthResolver: staticAuthResolver{},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewModelClientFactory(tc.cfg); err == nil {
				t.Fatalf("NewModelClientFactory(%s) error = nil, want non-nil", tc.name)
			}
		})
	}
}

func TestModelClientFactorySelectsRealVsMock(t *testing.T) {
	threadID := protocol.NewThreadID("thread-1")
	sessionCfg := core.SessionConfiguration{
		ProviderID:        "openai",
		CollaborationMode: protocol.CollaborationMode{Settings: protocol.Settings{Model: "gpt-5"}},
	}

	tests := []struct {
		name         string
		resolved     ResolvedAuth
		wantReal     bool
		wantFallback bool
	}{
		{
			name: "credentials present selects real client",
			resolved: ResolvedAuth{
				HasCredentials: true,
				AuthProvider:   fakeAuthProvider{},
				AuthMode:       apiKeyMode(),
			},
			wantReal:     true,
			wantFallback: false,
		},
		{
			name:         "no credentials selects mock fallback",
			resolved:     ResolvedAuth{HasCredentials: false},
			wantReal:     false,
			wantFallback: true,
		},
		{
			name: "credentials flagged but provider nil selects mock fallback",
			resolved: ResolvedAuth{
				HasCredentials: true,
				AuthProvider:   nil,
			},
			wantReal:     false,
			wantFallback: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fallbackCalled := false
			factory, err := NewModelClientFactory(RealModelClientFactoryConfig{
				AuthResolver:   staticAuthResolver{resolved: tc.resolved},
				Provider:       modelproviderinfo.CreateOpenAIProvider(nil),
				InstallationID: "install-1",
				Fallback:       sentinelFallback(&fallbackCalled),
			})
			if err != nil {
				t.Fatalf("NewModelClientFactory: %v", err)
			}

			mc, err := factory(context.Background(), threadID, sessionCfg)
			if err != nil {
				t.Fatalf("factory: %v", err)
			}

			_, isReal := mc.(*core.ResponsesModelClient)
			if isReal != tc.wantReal {
				t.Fatalf("client is *ResponsesModelClient = %v, want %v", isReal, tc.wantReal)
			}
			if fallbackCalled != tc.wantFallback {
				t.Fatalf("fallback called = %v, want %v", fallbackCalled, tc.wantFallback)
			}

			// When the real client is selected, the slug must flow through to the
			// model metadata.
			if tc.wantReal && mc.ModelSlug() != "gpt-5" {
				t.Fatalf("ModelSlug = %q, want gpt-5", mc.ModelSlug())
			}
		})
	}
}

func TestModelClientFactoryPropagatesResolverError(t *testing.T) {
	wantErr := errors.New("boom")
	fallbackCalled := false
	factory, err := NewModelClientFactory(RealModelClientFactoryConfig{
		AuthResolver: staticAuthResolver{err: wantErr},
		Fallback:     sentinelFallback(&fallbackCalled),
	})
	if err != nil {
		t.Fatalf("NewModelClientFactory: %v", err)
	}

	_, err = factory(context.Background(), protocol.NewThreadID("t"), core.SessionConfiguration{})
	if err == nil {
		t.Fatal("factory error = nil, want non-nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("factory error = %v, want wrap of %v", err, wantErr)
	}
	if fallbackCalled {
		t.Fatal("fallback should not be called when the resolver errors")
	}
}

func TestModelClientFactoryDefaultsProvider(t *testing.T) {
	// A zero Provider should default to the built-in OpenAI provider, so the real
	// client is built successfully.
	factory, err := NewModelClientFactory(RealModelClientFactoryConfig{
		AuthResolver: staticAuthResolver{resolved: ResolvedAuth{
			HasCredentials: true,
			AuthProvider:   fakeAuthProvider{},
			AuthMode:       apiKeyMode(),
		}},
		Fallback: sentinelFallback(new(bool)),
	})
	if err != nil {
		t.Fatalf("NewModelClientFactory: %v", err)
	}

	mc, err := factory(context.Background(), protocol.NewThreadID("t"), core.SessionConfiguration{
		CollaborationMode: protocol.CollaborationMode{Settings: protocol.Settings{Model: "gpt-5"}},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if _, ok := mc.(*core.ResponsesModelClient); !ok {
		t.Fatalf("client type = %T, want *core.ResponsesModelClient", mc)
	}
}

func TestResolveModelInfoUsesCatalogThenFallback(t *testing.T) {
	window := int64(200000)

	// No catalog: fall back to slug-derived metadata.
	got := resolveModelInfo("gpt-5", nil)
	if got.Slug != "gpt-5" {
		t.Fatalf("fallback slug = %q, want gpt-5", got.Slug)
	}

	// Catalog hit carries the richer metadata while preserving the requested slug.
	cat := modelsmanager.ModelInfo{Slug: "gpt-5", ContextWindow: &window}
	hit := resolveModelInfo("gpt-5", []modelsmanager.ModelInfo{cat})
	if hit.Slug != "gpt-5" {
		t.Fatalf("catalog hit slug = %q, want gpt-5", hit.Slug)
	}
	if hit.ContextWindow == nil || *hit.ContextWindow != window {
		t.Fatalf("catalog hit context window = %v, want %d", hit.ContextWindow, window)
	}
}
