// Package chatgpt is a faithful Go port of the dependency-light parts of the
// Rust `codex-chatgpt` crate: the ChatGPT backend GET helper, task fetching,
// and workspace settings. It talks to the ChatGPT backend-api using a
// login.CodexAuth for bearer/account headers.
//
// The connectors module of the Rust crate is intentionally omitted because it
// depends on codex-core/codex-connectors, which are outside this port's scope.
package chatgpt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/login"
)

const (
	// oaiProductSkuHeader is the header carrying the product SKU.
	oaiProductSkuHeader = "OAI-Product-Sku"
	// codexProductSku is the SKU value sent for Codex requests.
	codexProductSku = "codex"
)

// Session carries the context needed to make ChatGPT backend requests: the base
// URL and the resolved auth. It replaces the Rust `Config` + `AuthManager`
// plumbing with explicit inputs available in this port.
type Session struct {
	// ChatgptBaseURL is the ChatGPT backend base URL (e.g.
	// https://chatgpt.com/backend-api).
	ChatgptBaseURL string
	// Auth is the resolved Codex auth used to build request headers.
	Auth *login.CodexAuth
	// HTTPClient is the HTTP client to use; defaults to http.DefaultClient.
	HTTPClient *http.Client
}

func (s Session) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}

// Get performs a GET request to the ChatGPT backend API and decodes the JSON
// response into out. It mirrors the Rust `chatgpt_get_request`.
func (s Session) Get(ctx context.Context, path string, out any) error {
	return s.GetWithTimeout(ctx, path, 0, out)
}

// GetWithTimeout is Get with an optional per-request timeout (0 means none). It
// mirrors the Rust `chatgpt_get_request_with_timeout`.
func (s Session) GetWithTimeout(ctx context.Context, path string, timeout time.Duration, out any) error {
	if s.Auth == nil {
		return fmt.Errorf("ChatGPT auth not available")
	}
	if !s.Auth.UsesCodexBackend() {
		return fmt.Errorf("ChatGPT backend requests require Codex backend auth")
	}
	if s.Auth.GetAccountID() == nil {
		return fmt.Errorf("ChatGPT account ID not available, please re-run `codex login`")
	}

	url := fmt.Sprintf("%s/%s",
		strings.TrimRight(s.ChatgptBaseURL, "/"),
		strings.TrimLeft(path, "/"),
	)

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if err := s.applyAuthHeaders(req); err != nil {
		return err
	}
	req.Header.Set(oaiProductSkuHeader, codexProductSku)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("Failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		buf.Reset()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Request failed with status %d: %s", resp.StatusCode, buf.String())
	}
	if err := json.Unmarshal(buf.Bytes(), out); err != nil {
		return fmt.Errorf("Failed to parse JSON response: %w", err)
	}
	return nil
}

// applyAuthHeaders sets the Authorization and ChatGPT-Account-Id headers from
// the session auth, mirroring `auth_provider_from_auth(&auth).to_auth_headers()`.
func (s Session) applyAuthHeaders(req *http.Request) error {
	token, err := s.Auth.GetToken()
	if err != nil {
		return fmt.Errorf("ChatGPT auth token not available: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if id := s.Auth.GetAccountID(); id != nil && *id != "" {
		req.Header.Set("ChatGPT-Account-Id", *id)
	}
	if s.Auth.IsFedrampAccount() {
		req.Header.Set("X-OpenAI-Fedramp", "true")
	}
	return nil
}
