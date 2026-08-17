package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/pkg/config"
)

// refreshTimeout bounds a single token-refresh request (upstream: 45s).
const refreshTimeout = 45 * time.Second

// ErrNoRefreshToken indicates the stored credentials cannot be refreshed
// because no refresh_token is present (the caller must re-authorize).
var ErrNoRefreshToken = fmt.Errorf("mcp: oauth credentials have no refresh_token")

// RefreshTokens performs the OAuth2 refresh_token grant (spec 49 need 5):
// POST tokenEndpoint with grant_type=refresh_token. The refresh_token is
// preserved from the request when the server omits a rotated one (upstream
// behavior). Does NOT run the authorization-code flow (non-goal).
func RefreshTokens(ctx context.Context, httpClient *http.Client, tokenEndpoint, clientID, refreshToken string) (OAuthTokenResponse, error) {
	if refreshToken == "" {
		return OAuthTokenResponse{}, ErrNoRefreshToken
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if clientID != "" {
		form.Set("client_id", clientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthTokenResponse{}, fmt.Errorf("mcp: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return OAuthTokenResponse{}, fmt.Errorf("mcp: token refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthTokenResponse{}, fmt.Errorf("mcp: token refresh failed: status %d", resp.StatusCode)
	}

	var refreshed OAuthTokenResponse
	if err := json.Unmarshal(body, &refreshed); err != nil {
		return OAuthTokenResponse{}, fmt.Errorf("mcp: decode refresh response: %w", err)
	}
	// Preserve the prior refresh_token when the server does not rotate it.
	if refreshed.RefreshToken == nil {
		rt := refreshToken
		refreshed.RefreshToken = &rt
	}
	return refreshed, nil
}

// TokenEndpointResolver resolves the token endpoint for a server URL (satisfied
// by the existing OAuth discovery). Kept as a function so refresh does not
// hard-depend on the discovery implementation and stays unit-testable.
type TokenEndpointResolver func(ctx context.Context, serverURL string) (string, error)

// RefreshIfNeeded refreshes and persists the stored tokens when they are within
// the skew window of expiry. Returns the (possibly unchanged) tokens and whether
// a refresh occurred. A missing refresh_token surfaces ErrNoRefreshToken.
func RefreshIfNeeded(
	ctx context.Context,
	store *OAuthStore,
	tokens StoredOAuthTokens,
	mode config.OAuthCredentialsStoreMode,
	resolve TokenEndpointResolver,
	now time.Time,
) (StoredOAuthTokens, bool, error) {
	if !TokenNeedsRefresh(tokens.ExpiresAt, now) {
		return tokens, false, nil
	}
	refreshToken := ""
	if tokens.TokenResponse.RefreshToken != nil {
		refreshToken = *tokens.TokenResponse.RefreshToken
	}
	if refreshToken == "" {
		return tokens, false, ErrNoRefreshToken
	}

	endpoint, err := resolve(ctx, tokens.URL)
	if err != nil {
		return tokens, false, fmt.Errorf("mcp: resolve token endpoint: %w", err)
	}

	refreshCtx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	client := &http.Client{Timeout: refreshTimeout}
	refreshed, err := RefreshTokens(refreshCtx, client, endpoint, tokens.ClientID, refreshToken)
	if err != nil {
		return tokens, false, err
	}

	updated := tokens
	updated.TokenResponse = refreshed
	updated.ExpiresAt = refreshed.ComputeExpiresAtMillis(now)
	if err := store.Save(updated, mode); err != nil {
		return tokens, false, fmt.Errorf("mcp: persist refreshed tokens: %w", err)
	}
	return updated, true, nil
}
