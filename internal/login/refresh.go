package login

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// OAuth client id and endpoints, mirroring manager.rs constants.
const (
	// ClientID is the OAuth client id used for the ChatGPT login flow.
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// DefaultChatgptBackendBaseURL is the default ChatGPT backend base URL.
	DefaultChatgptBackendBaseURL = "https://chatgpt.com/backend-api"

	refreshTokenURL = "https://auth.openai.com/oauth/token"
	revokeTokenURL  = "https://auth.openai.com/oauth/revoke"

	// RefreshTokenURLOverrideEnvVar overrides the refresh-token endpoint.
	RefreshTokenURLOverrideEnvVar = "CODEXGO_REFRESH_TOKEN_URL_OVERRIDE"
	// RevokeTokenURLOverrideEnvVar overrides the revoke-token endpoint.
	RevokeTokenURLOverrideEnvVar = "CODEXGO_REVOKE_TOKEN_URL_OVERRIDE"
)

// Refresh-token failure user-facing messages, mirroring manager.rs constants.
const (
	refreshTokenExpiredMessage         = "Your access token could not be refreshed because your refresh token has expired. Please log out and sign in again."
	refreshTokenReusedMessage          = "Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again."
	refreshTokenInvalidatedMessage     = "Your access token could not be refreshed because your refresh token was revoked. Please log out and sign in again."
	refreshTokenUnknownMessage         = "Your access token could not be refreshed. Please log out and sign in again."
	refreshTokenAccountMismatchMessage = "Your access token could not be refreshed because you have since logged out or signed in to another account. Please sign in again."
)

// RefreshTokenError reports a token refresh failure. It mirrors the Rust
// RefreshTokenError enum: Permanent failures wrap a classified
// RefreshTokenFailedError, while Transient failures wrap a lower-level error.
type RefreshTokenError struct {
	// Permanent is set for permanent (non-retryable) refresh failures.
	Permanent *protocol.RefreshTokenFailedError
	// Transient is set for transient (retryable) refresh failures.
	Transient error
}

// Error implements the error interface.
func (e *RefreshTokenError) Error() string {
	if e.Permanent != nil {
		return e.Permanent.Error()
	}
	if e.Transient != nil {
		return e.Transient.Error()
	}
	return "refresh token error"
}

// Unwrap exposes the wrapped transient error for errors.Is/As.
func (e *RefreshTokenError) Unwrap() error {
	if e.Transient != nil {
		return e.Transient
	}
	if e.Permanent != nil {
		return *e.Permanent
	}
	return nil
}

// FailedReason returns the classified failure reason for permanent errors, or
// nil for transient ones. Mirrors RefreshTokenError::failed_reason.
func (e *RefreshTokenError) FailedReason() *protocol.RefreshTokenFailedReason {
	if e.Permanent != nil {
		r := e.Permanent.Reason
		return &r
	}
	return nil
}

// permanentRefreshError builds a permanent RefreshTokenError.
func permanentRefreshError(reason protocol.RefreshTokenFailedReason, message string) *RefreshTokenError {
	failed := protocol.NewRefreshTokenFailedError(reason, message)
	return &RefreshTokenError{Permanent: &failed}
}

// transientRefreshError builds a transient RefreshTokenError.
func transientRefreshError(err error) *RefreshTokenError {
	return &RefreshTokenError{Transient: err}
}

// refreshRequest is the JSON body sent to the refresh-token endpoint. Field
// order matches the Rust RefreshRequest struct.
type refreshRequest struct {
	ClientID     string `json:"client_id"`
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

// refreshResponse is the JSON returned by the refresh-token endpoint.
type refreshResponse struct {
	IDToken      *string `json:"id_token"`
	AccessToken  *string `json:"access_token"`
	RefreshToken *string `json:"refresh_token"`
}

// refreshTokenEndpoint returns the refresh-token endpoint, honoring the override
// env var. Mirrors manager::refresh_token_endpoint.
func refreshTokenEndpoint() string {
	if v, ok := os.LookupEnv(RefreshTokenURLOverrideEnvVar); ok {
		return v
	}
	return refreshTokenURL
}

// requestChatgptTokenRefresh requests refreshed ChatGPT OAuth tokens. The caller
// is responsible for persisting any returned tokens. Mirrors
// manager::request_chatgpt_token_refresh.
func requestChatgptTokenRefresh(ctx context.Context, httpClient *http.Client, refreshToken string) (refreshResponse, *RefreshTokenError) {
	body, err := json.Marshal(refreshRequest{
		ClientID:     ClientID,
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
	})
	if err != nil {
		return refreshResponse{}, transientRefreshError(fmt.Errorf("serialize refresh request: %w", err))
	}

	endpoint := refreshTokenEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return refreshResponse{}, transientRefreshError(fmt.Errorf("build refresh request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("originator", originatorValue())

	resp, err := httpClient.Do(req)
	if err != nil {
		return refreshResponse{}, transientRefreshError(fmt.Errorf("refresh request failed: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var parsed refreshResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return refreshResponse{}, transientRefreshError(fmt.Errorf("decode refresh response: %w", err))
		}
		return parsed, nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	failed := classifyRefreshTokenFailure(bodyStr)
	if resp.StatusCode == http.StatusUnauthorized || failed.Reason != protocol.RefreshTokenFailedOther {
		return refreshResponse{}, &RefreshTokenError{Permanent: &failed}
	}
	message := tryParseErrorMessage(bodyStr)
	return refreshResponse{}, transientRefreshError(
		fmt.Errorf("Failed to refresh token: %d: %s", resp.StatusCode, message),
	)
}

// classifyRefreshTokenFailure maps a refresh error body to a classified
// RefreshTokenFailedError. Mirrors manager::classify_refresh_token_failure.
func classifyRefreshTokenFailure(body string) protocol.RefreshTokenFailedError {
	code := extractRefreshTokenErrorCode(body)
	var normalized string
	if code != nil {
		normalized = strings.ToLower(*code)
	}

	var reason protocol.RefreshTokenFailedReason
	switch normalized {
	case "refresh_token_expired":
		reason = protocol.RefreshTokenFailedExpired
	case "refresh_token_reused":
		reason = protocol.RefreshTokenFailedExhausted
	case "refresh_token_invalidated":
		reason = protocol.RefreshTokenFailedRevoked
	default:
		reason = protocol.RefreshTokenFailedOther
	}

	var message string
	switch reason {
	case protocol.RefreshTokenFailedExpired:
		message = refreshTokenExpiredMessage
	case protocol.RefreshTokenFailedExhausted:
		message = refreshTokenReusedMessage
	case protocol.RefreshTokenFailedRevoked:
		message = refreshTokenInvalidatedMessage
	default:
		message = refreshTokenUnknownMessage
	}

	return protocol.NewRefreshTokenFailedError(reason, message)
}

// extractRefreshTokenErrorCode extracts the error code from a refresh error
// body. It supports a string "error", an object "error" with a "code" field, or
// a top-level "code". Mirrors manager::extract_refresh_token_error_code.
func extractRefreshTokenErrorCode(body string) *string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil
	}

	if errorValue, ok := raw["error"]; ok {
		// Try object with "code".
		var asObj map[string]json.RawMessage
		if json.Unmarshal(errorValue, &asObj) == nil {
			if codeRaw, ok := asObj["code"]; ok {
				var code string
				if json.Unmarshal(codeRaw, &code) == nil {
					return &code
				}
			}
		}
		// Try string.
		var asStr string
		if json.Unmarshal(errorValue, &asStr) == nil {
			return &asStr
		}
	}

	if codeRaw, ok := raw["code"]; ok {
		var code string
		if json.Unmarshal(codeRaw, &code) == nil {
			return &code
		}
	}
	return nil
}

// tryParseErrorMessage extracts a human-readable error message from a server
// error body, falling back to the raw text. Mirrors util::try_parse_error_message.
func tryParseErrorMessage(text string) string {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &raw) == nil {
		if errRaw, ok := raw["error"]; ok {
			var errObj map[string]json.RawMessage
			if json.Unmarshal(errRaw, &errObj) == nil {
				if msgRaw, ok := errObj["message"]; ok {
					var msg string
					if json.Unmarshal(msgRaw, &msg) == nil {
						return msg
					}
				}
			}
		}
	}
	if text == "" {
		return "Unknown error"
	}
	return text
}
