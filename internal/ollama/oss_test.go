package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

func TestSupportsResponses(t *testing.T) {
	tests := []struct {
		name    string
		version Version
		want    bool
	}{
		{"dev zero always supported", NewVersion(0, 0, 0), true},
		{"before cutoff", NewVersion(0, 13, 3), false},
		{"at cutoff", NewVersion(0, 13, 4), true},
		{"after cutoff", NewVersion(0, 14, 0), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SupportsResponses(tc.version); got != tc.want {
				t.Errorf("SupportsResponses(%v) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}

func TestMinResponsesVersion(t *testing.T) {
	if got := MinResponsesVersion(); got != NewVersion(0, 13, 4) {
		t.Errorf("MinResponsesVersion() = %v, want 0.13.4", got)
	}
}

func TestEnsureResponsesSupportedTooOld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.13.3"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := EnsureResponsesSupported(context.Background(), modelproviderinfo.CreateOSSProviderWithBaseURL(srv.URL, modelproviderinfo.WireApiResponses))
	if err == nil || !strings.Contains(err.Error(), "Ollama 0.13.3 is too old") {
		t.Fatalf("error = %v, want too old", err)
	}
	if !strings.Contains(err.Error(), "Ollama 0.13.4 or newer") {
		t.Errorf("error = %v, want min version mention", err)
	}
}

func TestEnsureResponsesSupportedNewEnough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.14.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := EnsureResponsesSupported(context.Background(), modelproviderinfo.CreateOSSProviderWithBaseURL(srv.URL, modelproviderinfo.WireApiResponses)); err != nil {
		t.Fatalf("EnsureResponsesSupported: %v", err)
	}
}

func TestEnsureResponsesSupportedMissingVersionEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r) // /api/version 404 -> treated as unknown
		}
	}))
	defer srv.Close()

	if err := EnsureResponsesSupported(context.Background(), modelproviderinfo.CreateOSSProviderWithBaseURL(srv.URL, modelproviderinfo.WireApiResponses)); err != nil {
		t.Fatalf("EnsureResponsesSupported should be nil when version missing: %v", err)
	}
}

func TestEnsureOSSReadyModelPresentSkipsPull(t *testing.T) {
	var pullCalled bool
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"gpt-oss:20b"}]}`))
		case "/api/pull":
			mu.Lock()
			pullCalled = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := EnsureOSSReady(context.Background(), ossProviders(srv.URL), "gpt-oss:20b")
	if err != nil {
		t.Fatalf("EnsureOSSReady: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if pullCalled {
		t.Error("pull should not be called when model present")
	}
}

func TestEnsureOSSReadyDefaultModel(t *testing.T) {
	var pullModel string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/pull":
			var body struct {
				Model string `json:"model"`
			}
			_ = decodeJSONBody(r, &body)
			mu.Lock()
			pullModel = body.Model
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Empty model string -> DefaultOSSModel pulled.
	if err := EnsureOSSReady(context.Background(), ossProviders(srv.URL), ""); err != nil {
		t.Fatalf("EnsureOSSReady: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if pullModel != DefaultOSSModel {
		t.Errorf("pulled model = %q, want %q", pullModel, DefaultOSSModel)
	}
}
