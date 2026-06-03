package lmstudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

// connectTimeout is the dial timeout applied when talking to a local server,
// mirroring the Rust reqwest connect_timeout(Duration::from_secs(5)).
const connectTimeout = 5 * time.Second

// lmstudioConnectionError is the message surfaced when the LM Studio server is
// unreachable. It mirrors the Rust LMSTUDIO_CONNECTION_ERROR constant verbatim.
const lmstudioConnectionError = "LM Studio is not responding. Install from https://lmstudio.ai/download and run 'lms server start'."

// Client interacts with a local LM Studio server. It mirrors the Rust
// LMStudioClient. The zero value is not usable; construct via TryFromProvider or
// FromHostRoot.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// newHTTPClient builds an http.Client whose dialer enforces the 5s connect
// timeout used by the reference client.
func newHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: connectTimeout}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   connectTimeout,
			ExpectContinueTimeout: time.Second,
		},
	}
}

// TryFromProvider builds a client for the built-in LM Studio provider and
// verifies the server is reachable.
//
// The provider is looked up from the supplied provider map (derived from config
// so user overrides are honored) under modelproviderinfo.LMStudioOSSProviderID.
// It mirrors the Rust LMStudioClient::try_from_provider, except the provider map
// is passed in rather than read from a Config.
func TryFromProvider(ctx context.Context, providers map[string]modelproviderinfo.ModelProviderInfo) (*Client, error) {
	provider, ok := providers[modelproviderinfo.LMStudioOSSProviderID]
	if !ok {
		return nil, fmt.Errorf("Built-in provider %s not found", modelproviderinfo.LMStudioOSSProviderID)
	}
	if provider.BaseURL == nil {
		return nil, errors.New("oss provider must have a base_url")
	}
	c := &Client{
		httpClient: newHTTPClient(),
		baseURL:    *provider.BaseURL,
	}
	if err := c.checkServer(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// FromHostRoot builds a client targeting a raw base URL, e.g.
// "http://localhost:1234", without checking reachability. It mirrors the Rust
// LMStudioClient::from_host_root and is primarily useful for tests.
func FromHostRoot(baseURL string) *Client {
	return &Client{
		httpClient: newHTTPClient(),
		baseURL:    baseURL,
	}
}

func (c *Client) trimmedBaseURL() string {
	return strings.TrimRight(c.baseURL, "/")
}

// checkServer verifies the server responds on /models. It mirrors the Rust
// LMStudioClient::check_server, including the distinct messages for a transport
// failure versus a non-success HTTP status.
func (c *Client) checkServer(ctx context.Context) error {
	url := c.trimmedBaseURL() + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.New(lmstudioConnectionError)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.New(lmstudioConnectionError)
	}
	defer drainAndClose(resp.Body)
	if isSuccess(resp.StatusCode) {
		return nil
	}
	return fmt.Errorf("Server returned error: %s %s", httpStatus(resp.StatusCode), lmstudioConnectionError)
}

// LoadModel warms up a model by issuing an empty Responses request with
// max_output_tokens 1. It mirrors the Rust LMStudioClient::load_model.
func (c *Client) LoadModel(ctx context.Context, model string) error {
	url := c.trimmedBaseURL() + "/responses"
	body, err := json.Marshal(map[string]any{
		"model":             model,
		"input":             "",
		"max_output_tokens": 1,
	})
	if err != nil {
		return fmt.Errorf("Request failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Request failed: %w", err)
	}
	defer drainAndClose(resp.Body)
	if isSuccess(resp.StatusCode) {
		return nil
	}
	return fmt.Errorf("Failed to load model: %s", httpStatus(resp.StatusCode))
}

// FetchModels returns the list of models available on the LM Studio server,
// reading ids from the OpenAI-compatible {"data": [{"id": ...}]} response. It
// mirrors the Rust LMStudioClient::fetch_models, including the error when no
// "data" array is present.
func (c *Client) FetchModels(ctx context.Context) ([]string, error) {
	url := c.trimmedBaseURL() + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("Request failed: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Request failed: %w", err)
	}
	defer drainAndClose(resp.Body)
	if !isSuccess(resp.StatusCode) {
		return nil, fmt.Errorf("Failed to fetch models: %s", httpStatus(resp.StatusCode))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}
	var payload struct {
		Data *[]struct {
			ID *string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}
	if payload.Data == nil {
		return nil, errors.New("No 'data' array in response")
	}
	models := make([]string, 0, len(*payload.Data))
	for _, m := range *payload.Data {
		if m.ID != nil {
			models = append(models, *m.ID)
		}
	}
	return models, nil
}

// DownloadModel downloads a model by invoking the `lms` CLI ("lms get --yes
// <model>"). Stdout is inherited so progress is visible; stderr is discarded. It
// mirrors the Rust LMStudioClient::download_model.
func (c *Client) DownloadModel(ctx context.Context, model string) error {
	lms, err := findLMS()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Downloading model: %s\n", model)

	cmd := exec.CommandContext(ctx, lms, "get", "--yes", model)
	cmd.Stdout = os.Stdout
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("Model download failed with exit code: %d", exitErr.ExitCode())
		}
		return fmt.Errorf("Failed to execute '%s get --yes %s': %w", lms, model, err)
	}
	return nil
}

// findLMS locates the `lms` executable, checking PATH first and then the
// platform-specific fallback under the user's home directory. It mirrors the
// Rust LMStudioClient::find_lms.
func findLMS() (string, error) {
	return findLMSWithHomeDir("")
}

// findLMSWithHomeDir is findLMS with an injectable home directory, mirroring the
// Rust find_lms_with_home_dir used by tests. An empty homeDir falls back to the
// platform home env var (HOME on unix, USERPROFILE on windows).
func findLMSWithHomeDir(homeDir string) (string, error) {
	if _, err := exec.LookPath("lms"); err == nil {
		return "lms", nil
	}

	home := homeDir
	if home == "" {
		if runtime.GOOS == "windows" {
			home = os.Getenv("USERPROFILE")
		} else {
			home = os.Getenv("HOME")
		}
	}

	var fallback string
	if runtime.GOOS == "windows" {
		fallback = filepath.Join(home, ".lmstudio", "bin", "lms.exe")
	} else {
		fallback = filepath.Join(home, ".lmstudio", "bin", "lms")
	}

	if fileExists(fallback) {
		return fallback, nil
	}
	return "", errors.New("LM Studio not found. Please install LM Studio from https://lmstudio.ai/")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isSuccess(status int) bool {
	return status >= 200 && status < 300
}

// httpStatus renders a status code the way reqwest's StatusCode Display does
// (e.g. "404 Not Found"), so error messages match the reference output.
func httpStatus(code int) string {
	text := http.StatusText(code)
	if text == "" {
		return fmt.Sprintf("%d", code)
	}
	return fmt.Sprintf("%d %s", code, text)
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
