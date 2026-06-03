package lmstudio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

func ossProviders(baseURL string) map[string]modelproviderinfo.ModelProviderInfo {
	return map[string]modelproviderinfo.ModelProviderInfo{
		modelproviderinfo.LMStudioOSSProviderID: modelproviderinfo.CreateOSSProviderWithBaseURL(baseURL, modelproviderinfo.WireApiResponses),
	}
}

func TestFetchModelsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-oss-20b"}]}`))
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	models, err := client.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if !containsString(models, "openai/gpt-oss-20b") {
		t.Errorf("unexpected models: %v", models)
	}
}

func TestFetchModelsNoDataArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	_, err := client.FetchModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "No 'data' array in response") {
		t.Fatalf("error = %v, want no data array", err)
	}
}

func TestFetchModelsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	_, err := client.FetchModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Failed to fetch models: 500") {
		t.Fatalf("error = %v, want failed to fetch models: 500", err)
	}
}

func TestCheckServerHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	if err := client.checkServer(context.Background()); err != nil {
		t.Fatalf("checkServer: %v", err)
	}
}

func TestCheckServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	err := client.checkServer(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Server returned error: 404") {
		t.Fatalf("error = %v, want server returned error: 404", err)
	}
}

func TestCheckServerTransportError(t *testing.T) {
	// Point at a closed server to force a transport error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := FromHostRoot(url)
	err := client.checkServer(context.Background())
	if err == nil || err.Error() != lmstudioConnectionError {
		t.Fatalf("error = %v, want connection error", err)
	}
}

func TestLoadModelHappyPath(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		_ = decodeJSONBody(r, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	if err := client.LoadModel(context.Background(), "openai/gpt-oss-20b"); err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	if gotBody["model"] != "openai/gpt-oss-20b" {
		t.Errorf("model = %v, want openai/gpt-oss-20b", gotBody["model"])
	}
	if gotBody["input"] != "" {
		t.Errorf("input = %v, want empty", gotBody["input"])
	}
	if got, ok := gotBody["max_output_tokens"].(float64); !ok || got != 1 {
		t.Errorf("max_output_tokens = %v, want 1", gotBody["max_output_tokens"])
	}
}

func TestLoadModelError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := FromHostRoot(srv.URL)
	err := client.LoadModel(context.Background(), "m")
	if err == nil || !strings.Contains(err.Error(), "Failed to load model: 500") {
		t.Fatalf("error = %v, want failed to load model: 500", err)
	}
}

func TestTryFromProviderOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := TryFromProvider(context.Background(), ossProviders(srv.URL)); err != nil {
		t.Fatalf("TryFromProvider: %v", err)
	}
}

func TestTryFromProviderMissingProvider(t *testing.T) {
	_, err := TryFromProvider(context.Background(), map[string]modelproviderinfo.ModelProviderInfo{})
	if err == nil || !strings.Contains(err.Error(), "Built-in provider lmstudio not found") {
		t.Fatalf("error = %v, want not-found", err)
	}
}

func TestTryFromProviderNoBaseURL(t *testing.T) {
	providers := map[string]modelproviderinfo.ModelProviderInfo{
		modelproviderinfo.LMStudioOSSProviderID: {BaseURL: nil},
	}
	_, err := TryFromProvider(context.Background(), providers)
	if err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url error", err)
	}
}

func TestFindLMSWithMockHome(t *testing.T) {
	// When lms is not in PATH and the fallback path does not exist, an
	// informative error is returned. (If lms happens to be installed in PATH on
	// the test machine, findLMS returns it; both outcomes are acceptable.)
	result, err := findLMSWithHomeDir("/nonexistent/test/home")
	if err != nil {
		if !strings.Contains(err.Error(), "LM Studio not found") {
			t.Errorf("error = %v, want LM Studio not found", err)
		}
		return
	}
	if result == "" {
		t.Error("expected non-empty lms path when found")
	}
}

func TestFromHostRoot(t *testing.T) {
	if c := FromHostRoot("http://localhost:1234"); c.baseURL != "http://localhost:1234" {
		t.Errorf("baseURL = %q, want http://localhost:1234", c.baseURL)
	}
	if c := FromHostRoot("https://example.com:8080/api"); c.baseURL != "https://example.com:8080/api" {
		t.Errorf("baseURL = %q, want https://example.com:8080/api", c.baseURL)
	}
}
