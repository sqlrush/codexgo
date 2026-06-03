package login

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// revokeHTTPTimeout bounds revoke requests. Mirrors REVOKE_HTTP_TIMEOUT.
const revokeHTTPTimeout = 10 * time.Second

// revokeTokenKind selects which token is being revoked. Mirrors RevokeTokenKind.
type revokeTokenKind int

const (
	revokeAccess revokeTokenKind = iota
	revokeRefresh
)

func (k revokeTokenKind) asString() string {
	switch k {
	case revokeAccess:
		return "access_token"
	default:
		return "refresh_token"
	}
}

// clientID returns the client id included with refresh-token revocations only.
// Mirrors RevokeTokenKind::client_id.
func (k revokeTokenKind) clientID() *string {
	if k == revokeRefresh {
		id := ClientID
		return &id
	}
	return nil
}

// revokeTokenRequest is the JSON body for a revoke request. client_id is omitted
// when nil (skip_serializing_if). Field order matches the Rust struct.
type revokeTokenRequest struct {
	Token         string  `json:"token"`
	TokenTypeHint string  `json:"token_type_hint"`
	ClientID      *string `json:"client_id,omitempty"`
}

// revokeAuthTokens best-effort revokes the managed ChatGPT token in
// authDotJson, preferring the refresh token. Mirrors revoke::revoke_auth_tokens.
func revokeAuthTokens(ctx context.Context, httpClient *http.Client, authDotJson *AuthDotJson) error {
	token, kind, ok := revocableToken(authDotJson)
	if !ok {
		return nil
	}
	endpoint := revokeTokenEndpoint()
	return revokeOAuthToken(ctx, httpClient, endpoint, token, kind, revokeHTTPTimeout)
}

// shouldRevokeAuthTokens reports whether the previous auth's token should be
// revoked given a replacement auth. Mirrors revoke::should_revoke_auth_tokens.
func shouldRevokeAuthTokens(authDotJson *AuthDotJson, replacement *AuthDotJson) bool {
	token, kind, ok := revocableToken(authDotJson)
	if !ok {
		return false
	}
	replacementTokens := managedChatgptTokens(replacement)
	if replacementTokens == nil {
		return true
	}
	switch kind {
	case revokeAccess:
		return replacementTokens.AccessToken != token
	default:
		return replacementTokens.RefreshToken != token
	}
}

// revocableToken returns the revocable token and its kind, if any. Mirrors
// revoke::revocable_token.
func revocableToken(authDotJson *AuthDotJson) (string, revokeTokenKind, bool) {
	tokens := managedChatgptTokens(authDotJson)
	if tokens == nil {
		return "", revokeAccess, false
	}
	if tokens.RefreshToken != "" {
		return tokens.RefreshToken, revokeRefresh, true
	}
	if tokens.AccessToken != "" {
		return tokens.AccessToken, revokeAccess, true
	}
	return "", revokeAccess, false
}

// managedChatgptTokens returns the TokenData only when the auth resolves to
// managed ChatGPT mode. Mirrors revoke::managed_chatgpt_tokens.
func managedChatgptTokens(authDotJson *AuthDotJson) *TokenData {
	if authDotJson == nil {
		return nil
	}
	if resolvedAuthModeForRevoke(authDotJson) == appserverproto.AuthModeChatgpt {
		return authDotJson.Tokens
	}
	return nil
}

// resolvedAuthModeForRevoke resolves the auth mode for revoke decisions. Mirrors
// revoke::resolved_auth_mode.
func resolvedAuthModeForRevoke(authDotJson *AuthDotJson) appserverproto.AuthMode {
	if authDotJson.AuthMode != nil {
		return *authDotJson.AuthMode
	}
	if authDotJson.OpenAIAPIKey != nil {
		return appserverproto.AuthModeApiKey
	}
	return appserverproto.AuthModeChatgpt
}

// revokeOAuthToken sends a single revoke request. Mirrors revoke::revoke_oauth_token.
func revokeOAuthToken(ctx context.Context, httpClient *http.Client, endpoint, token string, kind revokeTokenKind, timeout time.Duration) error {
	body, err := json.Marshal(revokeTokenRequest{
		Token:         token,
		TokenTypeHint: kind.asString(),
		ClientID:      kind.clientID(),
	})
	if err != nil {
		return fmt.Errorf("serialize revoke request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build revoke request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("originator", originatorValue())

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("revoke request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	message := tryParseErrorMessage(string(bodyBytes))
	return fmt.Errorf("failed to revoke %s: %d: %s", kind.asString(), resp.StatusCode, message)
}

// revokeTokenEndpoint returns the revoke endpoint, honoring the revoke override,
// then deriving from the refresh override, then falling back to the default.
// Mirrors revoke::revoke_token_endpoint.
func revokeTokenEndpoint() string {
	if v, ok := os.LookupEnv(RevokeTokenURLOverrideEnvVar); ok {
		return v
	}
	if refreshEndpoint, ok := os.LookupEnv(RefreshTokenURLOverrideEnvVar); ok {
		if derived, ok := deriveRevokeTokenEndpoint(refreshEndpoint); ok {
			return derived
		}
	}
	return revokeTokenURL
}

// deriveRevokeTokenEndpoint derives a revoke endpoint from a refresh endpoint by
// replacing its path with /oauth/revoke and clearing the query. Mirrors
// revoke::derive_revoke_token_endpoint.
func deriveRevokeTokenEndpoint(refreshEndpoint string) (string, bool) {
	u, err := url.Parse(refreshEndpoint)
	if err != nil {
		return "", false
	}
	u.Path = "/oauth/revoke"
	u.RawQuery = ""
	return u.String(), true
}
