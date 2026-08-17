package lmstudio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
)

func TestDefaultOSSModel(t *testing.T) {
	if DefaultOSSModel != "openai/gpt-oss-20b" {
		t.Errorf("DefaultOSSModel = %q, want openai/gpt-oss-20b", DefaultOSSModel)
	}
}

func TestEnsureOSSReadyModelPresentSkipsDownload(t *testing.T) {
	loaded := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-oss-20b"}]}`))
		case "/responses":
			w.WriteHeader(http.StatusOK)
			select {
			case loaded <- struct{}{}:
			default:
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := EnsureOSSReady(context.Background(), ossProviders(srv.URL), "openai/gpt-oss-20b"); err != nil {
		t.Fatalf("EnsureOSSReady: %v", err)
	}

	// The background load is best-effort; wait briefly so the goroutine can run
	// before the server is torn down (so we don't race on srv.Close()).
	select {
	case <-loaded:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background LoadModel to hit /responses")
	}
}

func TestEnsureOSSReadyFetchModelsFailureIsNonFatal(t *testing.T) {
	var mu sync.Mutex
	checkPassed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			if r.Method == http.MethodGet {
				mu.Lock()
				first := !checkPassed
				checkPassed = true
				mu.Unlock()
				if first {
					// First GET is the reachability check: succeed.
					w.WriteHeader(http.StatusOK)
					return
				}
				// Subsequent GET (FetchModels) fails; EnsureOSSReady must treat
				// this as non-fatal and not attempt a download.
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			http.NotFound(w, r)
		case "/responses":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// check_server and fetch_models both hit GET /models. The check succeeds and
	// the subsequent fetch fails; EnsureOSSReady should still return nil.
	if err := EnsureOSSReady(context.Background(), ossProviders(srv.URL), "openai/gpt-oss-20b"); err != nil {
		t.Fatalf("EnsureOSSReady should be nil on fetch failure: %v", err)
	}
	// Give the detached load goroutine a moment so it completes against the live
	// server rather than after Close().
	time.Sleep(50 * time.Millisecond)
}

func TestEnsureOSSReadyServerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	providers := map[string]modelproviderinfo.ModelProviderInfo{
		modelproviderinfo.LMStudioOSSProviderID: modelproviderinfo.CreateOSSProviderWithBaseURL(url, modelproviderinfo.WireApiResponses),
	}
	if err := EnsureOSSReady(context.Background(), providers, ""); err == nil {
		t.Fatal("expected error when server unreachable")
	}
}
