package login

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/config"
)

// OAuth issuer and callback ports, mirroring server.rs constants.
const (
	// DefaultIssuer is the OAuth issuer base URL.
	DefaultIssuer = "https://auth.openai.com"
	// DefaultPort is the preferred local callback port.
	DefaultPort = 1455
	// FallbackPort is the registered fallback callback port.
	FallbackPort = 1457
)

// oauthScope is the requested OAuth scope. Mirrors the scope set in
// build_authorize_url.
const oauthScope = "openid profile email offline_access api.connectors.read api.connectors.invoke"

// ServerOptions configures the local login callback server. Mirrors
// server::ServerOptions.
type ServerOptions struct {
	CodexHome                   string
	ClientID                    string
	Issuer                      string
	Port                        int
	OpenBrowser                 bool
	ForceState                  *string
	ForcedChatgptWorkspaceID    []string
	CodexStreamlinedLogin       bool
	CLIAuthCredentialsStoreMode config.AuthCredentialsStoreMode
}

// NewServerOptions builds ServerOptions with the default issuer and port.
// Mirrors ServerOptions::new.
func NewServerOptions(codexHome, clientID string, forcedWorkspaceID []string, mode config.AuthCredentialsStoreMode) ServerOptions {
	return ServerOptions{
		CodexHome:                   codexHome,
		ClientID:                    clientID,
		Issuer:                      DefaultIssuer,
		Port:                        DefaultPort,
		OpenBrowser:                 true,
		ForcedChatgptWorkspaceID:    forcedWorkspaceID,
		CLIAuthCredentialsStoreMode: mode,
	}
}

// ExchangedTokens are the tokens returned by the authorization-code exchange.
// Mirrors server::ExchangedTokens.
type ExchangedTokens struct {
	IDToken      string
	AccessToken  string
	RefreshToken string
}

// GenerateState returns a fresh URL-safe base64 (no padding) state value of 32
// random bytes. Mirrors server::generate_state.
func GenerateState() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

// BuildAuthorizeURL builds the OAuth authorize URL. Mirrors
// server::build_authorize_url, including parameter order and the optional
// allowed_workspace_id parameter.
func BuildAuthorizeURL(issuer, clientID, redirectURI string, pkce PkceCodes, state string, forcedWorkspaceIDs []string) string {
	type kv struct{ key, value string }
	query := []kv{
		{"response_type", "code"},
		{"client_id", clientID},
		{"redirect_uri", redirectURI},
		{"scope", oauthScope},
		{"code_challenge", pkce.CodeChallenge},
		{"code_challenge_method", "S256"},
		{"id_token_add_organizations", "true"},
		{"codex_cli_simplified_flow", "true"},
		{"state", state},
		{"originator", originatorValue()},
	}
	if len(forcedWorkspaceIDs) > 0 {
		query = append(query, kv{"allowed_workspace_id", strings.Join(forcedWorkspaceIDs, ",")})
	}
	parts := make([]string, 0, len(query))
	for _, q := range query {
		parts = append(parts, q.key+"="+urlEncode(q.value))
	}
	return issuer + "/oauth/authorize?" + strings.Join(parts, "&")
}

// ExchangeCodeForTokens exchanges an authorization code for tokens. Mirrors
// server::exchange_code_for_tokens.
func ExchangeCodeForTokens(ctx context.Context, httpClient *http.Client, issuer, clientID, redirectURI string, pkce PkceCodes, code string) (ExchangedTokens, error) {
	tokenEndpoint := trimTrailingSlash(issuer) + "/oauth/token"
	form := fmt.Sprintf(
		"grant_type=authorization_code&code=%s&redirect_uri=%s&client_id=%s&code_verifier=%s",
		urlEncode(code),
		urlEncode(redirectURI),
		urlEncode(clientID),
		urlEncode(pkce.CodeVerifier),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form))
	if err != nil {
		return ExchangedTokens{}, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("originator", originatorValue())

	resp, err := httpClient.Do(req)
	if err != nil {
		return ExchangedTokens{}, fmt.Errorf("token exchange transport failure: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		detail := parseTokenEndpointError(string(bodyBytes))
		return ExchangedTokens{}, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, detail.displayMessage)
	}

	var parsed struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ExchangedTokens{}, fmt.Errorf("decode token response: %w", err)
	}
	return ExchangedTokens{
		IDToken:      parsed.IDToken,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
	}, nil
}

// ObtainAPIKey exchanges an authenticated ID token for an API-key access token.
// Mirrors server::obtain_api_key.
func ObtainAPIKey(ctx context.Context, httpClient *http.Client, issuer, clientID, idToken string) (string, error) {
	tokenEndpoint := trimTrailingSlash(issuer) + "/oauth/token"
	form := fmt.Sprintf(
		"grant_type=%s&client_id=%s&requested_token=%s&subject_token=%s&subject_token_type=%s",
		urlEncode("urn:ietf:params:oauth:grant-type:token-exchange"),
		urlEncode(clientID),
		urlEncode("openai-api-key"),
		urlEncode(idToken),
		urlEncode("urn:ietf:params:oauth:token-type:id_token"),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form))
	if err != nil {
		return "", fmt.Errorf("build api key exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("originator", originatorValue())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("api key exchange transport failure: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("api key exchange failed with status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode api key exchange response: %w", err)
	}
	return body.AccessToken, nil
}

// PersistTokens persists exchanged credentials, then best-effort revokes any
// superseded managed ChatGPT tokens. Mirrors server::persist_tokens_async.
func PersistTokens(ctx context.Context, httpClient *http.Client, codexHome string, apiKey *string, idToken, accessToken, refreshToken string, mode config.AuthCredentialsStoreMode) error {
	previousAuth, err := LoadAuthDotJson(codexHome, mode)
	if err != nil {
		// Match the Rust warn-and-continue behavior.
		previousAuth = nil
	}

	info, err := ParseChatgptJWTClaims(idToken)
	if err != nil {
		return fmt.Errorf("parse id token: %w", err)
	}
	tokens := &TokenData{
		IDToken:      info,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}
	if acc := jwtAuthClaimString(idToken, "chatgpt_account_id"); acc != nil {
		tokens.AccountID = acc
	}
	authMode := appserverproto.AuthModeChatgpt
	nowTs := now()
	auth := &AuthDotJson{
		AuthMode:     &authMode,
		OpenAIAPIKey: apiKey,
		Tokens:       tokens,
		LastRefresh:  &nowTs,
	}
	if err := SaveAuth(codexHome, auth, mode); err != nil {
		return err
	}

	if shouldRevokeAuthTokens(previousAuth, auth) {
		// Best-effort; revoke failures do not fail login.
		_ = revokeAuthTokens(ctx, httpClient, previousAuth)
	}
	return nil
}

// ComposeSuccessURL builds the local /success redirect URL with claims from the
// id_token and access_token. Mirrors server::compose_success_url.
func ComposeSuccessURL(port int, issuer, idToken, accessToken string, codexStreamlinedLogin bool) string {
	tokenClaims := jwtAuthClaims(idToken)
	accessClaims := jwtAuthClaims(accessToken)

	orgID := claimString(tokenClaims, "organization_id")
	projectID := claimString(tokenClaims, "project_id")
	completedOnboarding := claimBool(tokenClaims, "completed_platform_onboarding")
	isOrgOwner := claimBool(tokenClaims, "is_org_owner")
	needsSetup := !completedOnboarding && isOrgOwner
	planType := claimString(accessClaims, "chatgpt_plan_type")

	platformURL := "https://platform.openai.com"
	if issuer != DefaultIssuer {
		platformURL = "https://platform.api.openai.org"
	}

	type kv struct{ key, value string }
	params := []kv{
		{"id_token", idToken},
		{"needs_setup", boolString(needsSetup)},
		{"org_id", orgID},
		{"project_id", projectID},
		{"plan_type", planType},
		{"platform_url", platformURL},
	}
	if codexStreamlinedLogin {
		params = append(params, kv{"codex_streamlined_login", "true"})
	}
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, p.key+"="+urlEncode(p.value))
	}
	return fmt.Sprintf("http://localhost:%d/success?%s", port, strings.Join(parts, "&"))
}

// EnsureWorkspaceAllowed validates the ID token against an optional workspace
// restriction. Mirrors server::ensure_workspace_allowed.
func EnsureWorkspaceAllowed(expected []string, idToken string) error {
	if expected == nil {
		return nil
	}
	claims := jwtAuthClaims(idToken)
	actual := claimStringPtr(claims, "chatgpt_account_id")
	if actual == nil {
		return errors.New("Login is restricted to a specific workspace, but the token did not include an chatgpt_account_id claim.")
	}
	for _, id := range expected {
		if id == *actual {
			return nil
		}
	}
	return fmt.Errorf("Login is restricted to workspace id(s) %s.", strings.Join(expected, ", "))
}

// jwtAuthClaims extracts the "https://api.openai.com/auth" object from a JWT
// payload. Mirrors server::jwt_auth_claims; on any error it returns an empty map.
func jwtAuthClaims(jwt string) map[string]json.RawMessage {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return map[string]json.RawMessage{}
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]json.RawMessage{}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return map[string]json.RawMessage{}
	}
	authRaw, ok := payload["https://api.openai.com/auth"]
	if !ok {
		return map[string]json.RawMessage{}
	}
	var authObj map[string]json.RawMessage
	if err := json.Unmarshal(authRaw, &authObj); err != nil {
		return map[string]json.RawMessage{}
	}
	return authObj
}

// jwtAuthClaimString returns a string-valued auth claim from the JWT, or nil.
func jwtAuthClaimString(jwt, key string) *string {
	return claimStringPtr(jwtAuthClaims(jwt), key)
}

// claimString returns the string value of key, or "" when absent or non-string.
func claimString(claims map[string]json.RawMessage, key string) string {
	if v := claimStringPtr(claims, key); v != nil {
		return *v
	}
	return ""
}

// claimStringPtr returns the string value of key, or nil when absent or non-string.
func claimStringPtr(claims map[string]json.RawMessage, key string) *string {
	raw, ok := claims[key]
	if !ok {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}

// claimBool returns the bool value of key, or false when absent or non-bool.
func claimBool(claims map[string]json.RawMessage, key string) bool {
	raw, ok := claims[key]
	if !ok {
		return false
	}
	var b bool
	if json.Unmarshal(raw, &b) != nil {
		return false
	}
	return b
}

// boolString renders a bool the way Rust's {bool} Display does ("true"/"false").
func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// tokenEndpointErrorDetail mirrors server::TokenEndpointErrorDetail.
type tokenEndpointErrorDetail struct {
	errorCode      *string
	errorMessage   *string
	displayMessage string
}

// parseTokenEndpointError extracts token endpoint error detail. Mirrors
// server::parse_token_endpoint_error.
func parseTokenEndpointError(body string) tokenEndpointErrorDetail {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return tokenEndpointErrorDetail{displayMessage: "unknown error"}
	}

	var parsed map[string]json.RawMessage
	if json.Unmarshal([]byte(trimmed), &parsed) == nil {
		errorCode := topLevelErrorCode(parsed)
		if desc := stringField(parsed, "error_description"); desc != nil && strings.TrimSpace(*desc) != "" {
			return tokenEndpointErrorDetail{errorCode: errorCode, errorMessage: desc, displayMessage: *desc}
		}
		if errRaw, ok := parsed["error"]; ok {
			var errObj map[string]json.RawMessage
			if json.Unmarshal(errRaw, &errObj) == nil {
				if msg := stringField(errObj, "message"); msg != nil && strings.TrimSpace(*msg) != "" {
					return tokenEndpointErrorDetail{errorCode: errorCode, errorMessage: msg, displayMessage: *msg}
				}
			}
		}
		if errorCode != nil {
			return tokenEndpointErrorDetail{errorCode: errorCode, displayMessage: *errorCode}
		}
	}

	return tokenEndpointErrorDetail{displayMessage: trimmed}
}

// topLevelErrorCode extracts the error code from a string "error" or an object
// "error" with a non-empty "code". Mirrors the first half of
// parse_token_endpoint_error's error_code extraction.
func topLevelErrorCode(parsed map[string]json.RawMessage) *string {
	errRaw, ok := parsed["error"]
	if !ok {
		return nil
	}
	var asStr string
	if json.Unmarshal(errRaw, &asStr) == nil {
		if strings.TrimSpace(asStr) != "" {
			return &asStr
		}
		return nil
	}
	var asObj map[string]json.RawMessage
	if json.Unmarshal(errRaw, &asObj) == nil {
		if code := stringField(asObj, "code"); code != nil && strings.TrimSpace(*code) != "" {
			return code
		}
	}
	return nil
}

// stringField returns a string-valued field, or nil when absent or non-string.
func stringField(m map[string]json.RawMessage, key string) *string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}

// PortAvailabilityChecker reports whether a TCP port can be bound. It abstracts
// the bind probe used by SelectBindPort so tests can drive the fallback logic.
type PortAvailabilityChecker func(port int) bool

// SelectBindPort applies the default->fallback port selection used by
// bind_server: it returns the preferred port when available, otherwise the
// fallback port (only when the preferred port is the default 1455). When neither
// is available it returns an error. The retry/cancel handshake of the Rust
// implementation is a runtime concern handled by the caller; this captures the
// port-selection policy.
func SelectBindPort(preferred int, available PortAvailabilityChecker) (int, error) {
	if available(preferred) {
		return preferred, nil
	}
	if preferred == DefaultPort && available(FallbackPort) {
		return FallbackPort, nil
	}
	return 0, fmt.Errorf("port %d is already in use", preferred)
}

// isMissingCodexEntitlementError reports whether an OAuth callback error means
// the workspace lacks the Codex entitlement. Mirrors
// server::is_missing_codex_entitlement_error.
func isMissingCodexEntitlementError(errorCode string, errorDescription *string) bool {
	return errorCode == "access_denied" &&
		errorDescription != nil &&
		strings.Contains(strings.ToLower(*errorDescription), "missing_codex_entitlement")
}

// oauthCallbackErrorMessage converts an OAuth callback error into a user-facing
// message. Mirrors server::oauth_callback_error_message.
func oauthCallbackErrorMessage(errorCode string, errorDescription *string) string {
	if isMissingCodexEntitlementError(errorCode, errorDescription) {
		return "Codex is not enabled for your workspace. Contact your workspace administrator to request access to Codex."
	}
	if errorDescription != nil && strings.TrimSpace(*errorDescription) != "" {
		return "Sign-in failed: " + *errorDescription
	}
	return "Sign-in failed: " + errorCode
}

// redirectURIForPort builds the local callback redirect URI for the given port.
func redirectURIForPort(port int) string {
	return fmt.Sprintf("http://localhost:%d/auth/callback", port)
}

// htmlEscape escapes a string for safe HTML insertion. Mirrors server::html_escape.
func htmlEscape(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, ch := range input {
		switch ch {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}
