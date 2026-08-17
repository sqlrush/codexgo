package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
)

// connectTimeout is the dial timeout applied when probing or talking to a local
// server, mirroring the Rust reqwest connect_timeout(Duration::from_secs(5)).
const connectTimeout = 5 * time.Second

// Client interacts with a local Ollama instance. It mirrors the Rust
// OllamaClient.
type Client struct {
	httpClient       *http.Client
	hostRoot         string
	usesOpenAICompat bool
}

// newHTTPClient builds an http.Client whose dialer enforces the 5s connect
// timeout used by the reference client. Unlike reqwest's connect_timeout, Go has
// no built-in connect-only timeout, so it is applied at the dialer.
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

// TryFromOSSProvider constructs a client for the built-in open-source ("oss")
// model provider and verifies that a local Ollama server is reachable.
//
// The provider is looked up from the supplied provider map (which the caller
// should derive from config so user overrides in config.toml are honored), under
// the modelproviderinfo.OllamaOSSProviderID key. It mirrors the Rust
// OllamaClient::try_from_oss_provider, except the provider map is passed in
// rather than read from a Config (which is outside this package's dependencies).
func TryFromOSSProvider(ctx context.Context, providers map[string]modelproviderinfo.ModelProviderInfo) (*Client, error) {
	provider, ok := providers[modelproviderinfo.OllamaOSSProviderID]
	if !ok {
		return nil, fmt.Errorf("Built-in provider %s not found", modelproviderinfo.OllamaOSSProviderID)
	}
	return TryFromProvider(ctx, provider)
}

// TryFromProvider builds a client from a provider definition and verifies the
// server is reachable. It mirrors the Rust OllamaClient::try_from_provider.
func TryFromProvider(ctx context.Context, provider modelproviderinfo.ModelProviderInfo) (*Client, error) {
	if provider.BaseURL == nil {
		return nil, errors.New("oss provider must have a base_url")
	}
	baseURL := *provider.BaseURL
	c := &Client{
		httpClient:       newHTTPClient(),
		hostRoot:         BaseURLToHostRoot(baseURL),
		usesOpenAICompat: IsOpenAICompatibleBaseURL(baseURL),
	}
	if err := c.probeServer(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// FromHostRoot builds a client targeting a raw host root, e.g.
// "http://localhost:11434", without probing. It mirrors the Rust
// OllamaClient::from_host_root and is primarily useful for tests and callers
// that have already verified reachability.
func FromHostRoot(hostRoot string) *Client {
	return &Client{
		httpClient: newHTTPClient(),
		hostRoot:   hostRoot,
	}
}

func (c *Client) trimmedHostRoot() string {
	return strings.TrimRight(c.hostRoot, "/")
}

// probeServer checks reachability via the health endpoint appropriate to the
// configured wire shape. It mirrors the Rust OllamaClient::probe_server.
func (c *Client) probeServer(ctx context.Context) error {
	var url string
	if c.usesOpenAICompat {
		url = c.trimmedHostRoot() + "/v1/models"
	} else {
		url = c.trimmedHostRoot() + "/api/tags"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.New(ollamaConnectionError)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.New(ollamaConnectionError)
	}
	defer drainAndClose(resp.Body)
	if isSuccess(resp.StatusCode) {
		return nil
	}
	return errors.New(ollamaConnectionError)
}

// FetchModels returns the list of model names known to the local Ollama
// instance. A non-success status yields an empty list (not an error), matching
// the Rust OllamaClient::fetch_models.
func (c *Client) FetchModels(ctx context.Context) ([]string, error) {
	url := c.trimmedHostRoot() + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("ollama: build tags request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: fetch models: %w", err)
	}
	defer drainAndClose(resp.Body)
	if !isSuccess(resp.StatusCode) {
		return []string{}, nil
	}

	var payload struct {
		Models []struct {
			Name *string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("ollama: decode models: %w", err)
	}
	names := make([]string, 0, len(payload.Models))
	for _, m := range payload.Models {
		if m.Name != nil {
			names = append(names, *m.Name)
		}
	}
	return names, nil
}

// FetchVersion queries the server for its version, returning (nil, nil) when the
// endpoint is unavailable or the version is missing/unparsable. It mirrors the
// Rust OllamaClient::fetch_version.
func (c *Client) FetchVersion(ctx context.Context) (*Version, error) {
	url := c.trimmedHostRoot() + "/api/version"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("ollama: build version request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: fetch version: %w", err)
	}
	defer drainAndClose(resp.Body)
	if !isSuccess(resp.StatusCode) {
		return nil, nil
	}

	var payload struct {
		Version *string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("ollama: decode version: %w", err)
	}
	if payload.Version == nil {
		return nil, nil
	}
	versionStr := strings.TrimSpace(*payload.Version)
	version, err := ParseVersion(versionStr)
	if err != nil {
		// Unparsable version is non-fatal; the caller proceeds as if unknown.
		return nil, nil
	}
	return &version, nil
}

// PullModelStream starts a model pull and returns a channel of streaming events.
// The channel is closed after a Success event is observed, an error event is
// produced, or the server closes the connection. It mirrors the Rust
// OllamaClient::pull_model_stream, which returns a BoxStream<PullEvent>.
//
// The returned error reflects only the initial request; per-line errors arrive
// as PullEventError events on the channel.
func (c *Client) PullModelStream(ctx context.Context, model string) (<-chan PullEvent, error) {
	url := c.trimmedHostRoot() + "/api/pull"
	body, err := json.Marshal(map[string]any{"model": model, "stream": true})
	if err != nil {
		return nil, fmt.Errorf("ollama: encode pull request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: build pull request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: start pull: %w", err)
	}
	if !isSuccess(resp.StatusCode) {
		drainAndClose(resp.Body)
		return nil, fmt.Errorf("failed to start pull: HTTP %d", resp.StatusCode)
	}

	events := make(chan PullEvent)
	go streamPullEvents(resp.Body, events)
	return events, nil
}

// streamPullEvents reads newline-delimited JSON objects from body and forwards
// the decoded events to out, closing both when the stream ends. It mirrors the
// async_stream loop in the Rust pull_model_stream.
func streamPullEvents(body io.ReadCloser, out chan<- PullEvent) {
	defer close(out)
	defer drainAndClose(body)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var update pullUpdate
		if err := json.Unmarshal([]byte(text), &update); err != nil {
			// Non-JSON lines are skipped, matching the Rust `if let Ok(..)` guard.
			continue
		}
		for _, ev := range pullEventsFromValue(update) {
			out <- ev
		}
		if update.Error != nil {
			out <- NewPullError(*update.Error)
			return
		}
		if update.Status != nil && *update.Status == "success" {
			out <- NewPullSuccess()
			return
		}
	}
	// Connection error or EOF: end the stream (scanner.Err is intentionally not
	// surfaced, matching the Rust behavior of simply returning on a chunk error).
}

// PullWithReporter pulls a model and drives a progress reporter. It mirrors the
// Rust OllamaClient::pull_with_reporter, including the leading status event and
// the rule that an error event (despite an HTTP 200) yields an error result.
func (c *Client) PullWithReporter(ctx context.Context, model string, reporter PullProgressReporter) error {
	if err := reporter.OnEvent(NewPullStatus(fmt.Sprintf("Pulling model %s...", model))); err != nil {
		return err
	}
	events, err := c.PullModelStream(ctx, model)
	if err != nil {
		return err
	}
	for event := range events {
		if err := reporter.OnEvent(event); err != nil {
			drainPullEvents(events)
			return err
		}
		switch event.Kind {
		case PullEventSuccess:
			drainPullEvents(events)
			return nil
		case PullEventError:
			// Empirically, ollama returns 200 OK even when the output stream
			// includes an error message, so we must inspect the event stream
			// rather than the HTTP status to decide whether to fail.
			drainPullEvents(events)
			return fmt.Errorf("Pull failed: %s", event.Status)
		default:
			continue
		}
	}
	return errors.New("Pull stream ended unexpectedly without success.")
}

// drainPullEvents consumes any remaining events so the producer goroutine can
// finish and close the response body.
func drainPullEvents(events <-chan PullEvent) {
	for range events {
	}
}

func isSuccess(status int) bool {
	return status >= 200 && status < 300
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
