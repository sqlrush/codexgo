package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
)

func ossProviders(baseURL string) map[string]modelproviderinfo.ModelProviderInfo {
	return map[string]modelproviderinfo.ModelProviderInfo{
		modelproviderinfo.OllamaOSSProviderID: modelproviderinfo.CreateOSSProviderWithBaseURL(baseURL, modelproviderinfo.WireApiResponses),
	}
}

func TestFetchModelsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:3b"},{"name":"mistral"}]}`))
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	models, err := client.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if !containsString(models, "llama3.2:3b") || !containsString(models, "mistral") {
		t.Errorf("unexpected models: %v", models)
	}
}

func TestFetchModelsNonSuccessReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	models, err := client.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected empty list, got %v", models)
	}
}

func TestFetchVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.14.1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := TryFromProvider(context.Background(), modelproviderinfo.CreateOSSProviderWithBaseURL(srv.URL, modelproviderinfo.WireApiResponses))
	if err != nil {
		t.Fatalf("TryFromProvider: %v", err)
	}
	version, err := client.FetchVersion(context.Background())
	if err != nil {
		t.Fatalf("FetchVersion: %v", err)
	}
	if version == nil || *version != NewVersion(0, 14, 1) {
		t.Errorf("version = %v, want 0.14.1", version)
	}
}

func TestFetchVersionUnparsableReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"not-a-version"}`))
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	version, err := client.FetchVersion(context.Background())
	if err != nil {
		t.Fatalf("FetchVersion: %v", err)
	}
	if version != nil {
		t.Errorf("version = %v, want nil for unparsable", version)
	}
}

func TestProbeServerNativeAndOpenAICompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags", "/v1/models":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Native: base URL without /v1 probes /api/tags.
	if _, err := TryFromProvider(context.Background(), modelproviderinfo.CreateOSSProviderWithBaseURL(srv.URL, modelproviderinfo.WireApiResponses)); err != nil {
		t.Fatalf("native probe failed: %v", err)
	}
	// OpenAI-compat: base URL ending in /v1 probes /v1/models.
	if _, err := TryFromProvider(context.Background(), modelproviderinfo.CreateOSSProviderWithBaseURL(srv.URL+"/v1", modelproviderinfo.WireApiResponses)); err != nil {
		t.Fatalf("openai-compat probe failed: %v", err)
	}
}

func TestTryFromOSSProviderOKWhenServerRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := TryFromOSSProvider(context.Background(), ossProviders(srv.URL+"/v1")); err != nil {
		t.Fatalf("TryFromOSSProvider: %v", err)
	}
}

func TestTryFromOSSProviderErrWhenServerMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := TryFromOSSProvider(context.Background(), ossProviders(srv.URL+"/v1"))
	if err == nil {
		t.Fatal("expected error when server missing")
	}
	if err.Error() != ollamaConnectionError {
		t.Errorf("error = %q, want %q", err.Error(), ollamaConnectionError)
	}
}

func TestTryFromOSSProviderMissingProvider(t *testing.T) {
	_, err := TryFromOSSProvider(context.Background(), map[string]modelproviderinfo.ModelProviderInfo{})
	if err == nil || !strings.Contains(err.Error(), "Built-in provider ollama not found") {
		t.Fatalf("error = %v, want not-found", err)
	}
}

func TestTryFromProviderNoBaseURL(t *testing.T) {
	_, err := TryFromProvider(context.Background(), modelproviderinfo.ModelProviderInfo{BaseURL: nil})
	if err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url error", err)
	}
}

func TestPullWithReporterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"pulling manifest"}` + "\n"))
		_, _ = w.Write([]byte(`{"digest":"sha256:a","total":100,"completed":50}` + "\n"))
		_, _ = w.Write([]byte(`{"status":"success"}` + "\n"))
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	rep := &recordingReporter{}
	if err := client.PullWithReporter(context.Background(), "gpt-oss:20b", rep); err != nil {
		t.Fatalf("PullWithReporter: %v", err)
	}
	if !rep.sawSuccess {
		t.Errorf("expected a success event, got %+v", rep.events)
	}
}

func TestPullWithReporterErrorEventFailsDespite200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"file does not exist"}` + "\n"))
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	rep := &recordingReporter{}
	err := client.PullWithReporter(context.Background(), "bogus", rep)
	if err == nil || !strings.Contains(err.Error(), "Pull failed: file does not exist") {
		t.Fatalf("error = %v, want pull failed", err)
	}
}

func TestPullModelStreamNonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	_, err := client.PullModelStream(context.Background(), "m")
	if err == nil || !strings.Contains(err.Error(), "failed to start pull: HTTP 400") {
		t.Fatalf("error = %v, want HTTP 400", err)
	}
}

// recordingReporter captures events for assertions.
type recordingReporter struct {
	events     []PullEvent
	sawSuccess bool
}

func (r *recordingReporter) OnEvent(event PullEvent) error {
	r.events = append(r.events, event)
	if event.Kind == PullEventSuccess {
		r.sawSuccess = true
	}
	return nil
}
