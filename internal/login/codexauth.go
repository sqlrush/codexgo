package login

import (
	"errors"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// CodexAuthKind discriminates the authentication mechanism. Mirrors the Rust
// CodexAuth enum variants.
type CodexAuthKind int

const (
	// CodexAuthAPIKey is OpenAI API key auth.
	CodexAuthAPIKey CodexAuthKind = iota
	// CodexAuthChatgpt is managed ChatGPT OAuth auth backed by storage.
	CodexAuthChatgpt
	// CodexAuthChatgptAuthTokens is host-app-supplied in-memory ChatGPT tokens.
	CodexAuthChatgptAuthTokens
	// CodexAuthAgentIdentity is agent-identity auth backed by a verified record.
	CodexAuthAgentIdentity
)

// Token-refresh timing constants, mirroring manager.rs.
const (
	tokenRefreshIntervalDays                = 8
	chatgptAccessTokenRefreshWindowMin      = 5
	chatgptAccessTokenRefreshWindowDuration = chatgptAccessTokenRefreshWindowMin * time.Minute
	tokenRefreshIntervalDuration            = tokenRefreshIntervalDays * 24 * time.Hour
)

// CodexAuth is the authentication mechanism used by the current user. It mirrors
// the Rust CodexAuth enum; the concrete payload depends on Kind.
//
// This is an immutable snapshot: accessors never mutate the value. The Chatgpt /
// ChatgptAuthTokens variants hold a cached AuthDotJson snapshot.
type CodexAuth struct {
	Kind CodexAuthKind

	// apiKey is set when Kind == CodexAuthAPIKey.
	apiKey string
	// authDotJson is the cached snapshot for the ChatGPT variants.
	authDotJson *AuthDotJson
	// record is set when Kind == CodexAuthAgentIdentity.
	record *AgentIdentityAuthRecord
}

// FromAPIKey builds an API-key CodexAuth. Mirrors CodexAuth::from_api_key.
func FromAPIKey(apiKey string) CodexAuth {
	return CodexAuth{Kind: CodexAuthAPIKey, apiKey: apiKey}
}

// chatgptAuth builds a managed-ChatGPT CodexAuth from a cached snapshot.
func chatgptAuth(auth *AuthDotJson) CodexAuth {
	return CodexAuth{Kind: CodexAuthChatgpt, authDotJson: auth}
}

// chatgptAuthTokens builds an external-ChatGPT-tokens CodexAuth from a snapshot.
func chatgptAuthTokens(auth *AuthDotJson) CodexAuth {
	return CodexAuth{Kind: CodexAuthChatgptAuthTokens, authDotJson: auth}
}

// agentIdentityAuth builds an agent-identity CodexAuth from a verified record.
func agentIdentityAuth(record AgentIdentityAuthRecord) CodexAuth {
	return CodexAuth{Kind: CodexAuthAgentIdentity, record: &record}
}

// AuthMode returns the high-level auth mode. Mirrors CodexAuth::auth_mode.
func (a CodexAuth) AuthMode() appserverproto.AuthMode {
	switch a.Kind {
	case CodexAuthAPIKey:
		return appserverproto.AuthModeApiKey
	case CodexAuthAgentIdentity:
		return appserverproto.AuthModeAgentIdentity
	default:
		return appserverproto.AuthModeChatgpt
	}
}

// APIAuthMode returns the precise API auth mode (distinguishing the two ChatGPT
// variants). Mirrors CodexAuth::api_auth_mode.
func (a CodexAuth) APIAuthMode() appserverproto.AuthMode {
	switch a.Kind {
	case CodexAuthAPIKey:
		return appserverproto.AuthModeApiKey
	case CodexAuthChatgpt:
		return appserverproto.AuthModeChatgpt
	case CodexAuthChatgptAuthTokens:
		return appserverproto.AuthModeChatgptAuthTokens
	default:
		return appserverproto.AuthModeAgentIdentity
	}
}

// IsAPIKeyAuth reports whether this is API-key auth.
func (a CodexAuth) IsAPIKeyAuth() bool { return a.Kind == CodexAuthAPIKey }

// IsChatgptAuth reports whether this is one of the ChatGPT variants. Mirrors
// CodexAuth::is_chatgpt_auth.
func (a CodexAuth) IsChatgptAuth() bool {
	return a.Kind == CodexAuthChatgpt || a.Kind == CodexAuthChatgptAuthTokens
}

// UsesCodexBackend reports whether the auth talks to the Codex backend. Mirrors
// CodexAuth::uses_codex_backend.
func (a CodexAuth) UsesCodexBackend() bool {
	return a.Kind == CodexAuthChatgpt || a.Kind == CodexAuthChatgptAuthTokens || a.Kind == CodexAuthAgentIdentity
}

// IsExternalChatgptTokens reports whether this is the external-tokens variant.
func (a CodexAuth) IsExternalChatgptTokens() bool { return a.Kind == CodexAuthChatgptAuthTokens }

// APIKey returns the API key for API-key auth, or nil otherwise. Mirrors
// CodexAuth::api_key.
func (a CodexAuth) APIKey() *string {
	if a.Kind == CodexAuthAPIKey {
		key := a.apiKey
		return &key
	}
	return nil
}

// ErrTokenDataUnavailable mirrors the Rust "Token data is not available." error.
var ErrTokenDataUnavailable = errors.New("Token data is not available.")

// GetTokenData returns the token data for token-backed ChatGPT auth, or an error
// when unavailable. Mirrors CodexAuth::get_token_data.
func (a CodexAuth) GetTokenData() (TokenData, error) {
	auth := a.currentAuthJSON()
	if auth != nil && auth.Tokens != nil && auth.LastRefresh != nil {
		return *auth.Tokens, nil
	}
	return TokenData{}, ErrTokenDataUnavailable
}

// GetToken returns the bearer token: the API key for API-key auth, or the access
// token for ChatGPT auth. Agent-identity auth has no bearer token. Mirrors
// CodexAuth::get_token.
func (a CodexAuth) GetToken() (string, error) {
	switch a.Kind {
	case CodexAuthAPIKey:
		return a.apiKey, nil
	case CodexAuthChatgpt, CodexAuthChatgptAuthTokens:
		data, err := a.GetTokenData()
		if err != nil {
			return "", err
		}
		return data.AccessToken, nil
	default:
		return "", errors.New("agent identity auth does not expose a bearer token")
	}
}

// GetAccountID returns the account id from the auth, or nil. Mirrors
// CodexAuth::get_account_id.
func (a CodexAuth) GetAccountID() *string {
	if a.Kind == CodexAuthAgentIdentity {
		id := a.record.AccountID
		return &id
	}
	if data := a.currentTokenData(); data != nil {
		return data.AccountID
	}
	return nil
}

// IsFedrampAccount reports whether the account routes through the FedRAMP edge.
// Mirrors CodexAuth::is_fedramp_account.
func (a CodexAuth) IsFedrampAccount() bool {
	if a.Kind == CodexAuthAgentIdentity {
		return a.record.ChatgptAccountIsFedramp
	}
	if data := a.currentTokenData(); data != nil {
		return data.IDToken.IsFedrampAccount()
	}
	return false
}

// GetAccountEmail returns the account email, or nil. Mirrors
// CodexAuth::get_account_email.
func (a CodexAuth) GetAccountEmail() *string {
	if a.Kind == CodexAuthAgentIdentity {
		email := a.record.Email
		return &email
	}
	if data := a.currentTokenData(); data != nil {
		return data.IDToken.Email
	}
	return nil
}

// GetChatgptUserID returns the ChatGPT user id, or nil. Mirrors
// CodexAuth::get_chatgpt_user_id.
func (a CodexAuth) GetChatgptUserID() *string {
	if a.Kind == CodexAuthAgentIdentity {
		id := a.record.ChatgptUserID
		return &id
	}
	if data := a.currentTokenData(); data != nil {
		return data.IDToken.ChatgptUserID
	}
	return nil
}

// AccountPlanType returns the account-facing plan classification. Mirrors
// CodexAuth::account_plan_type. Returns nil when no token data is available.
func (a CodexAuth) AccountPlanType() *protocol.PlanType {
	if a.Kind == CodexAuthAgentIdentity {
		plan := a.record.PlanType
		return &plan
	}
	data := a.currentTokenData()
	if data == nil {
		return nil
	}
	plan := protocol.PlanTypeUnknown
	if data.IDToken.ChatgptPlanType != nil {
		plan = protocol.PlanTypeFromAuthPlanType(*data.IDToken.ChatgptPlanType)
	}
	return &plan
}

// IsWorkspaceAccount reports whether the resolved plan is a workspace tier.
// Mirrors CodexAuth::is_workspace_account.
func (a CodexAuth) IsWorkspaceAccount() bool {
	plan := a.AccountPlanType()
	return plan != nil && plan.IsWorkspaceAccount()
}

// Record returns the agent-identity record, or nil for other auth kinds.
func (a CodexAuth) Record() *AgentIdentityAuthRecord {
	if a.Kind == CodexAuthAgentIdentity {
		return a.record
	}
	return nil
}

// currentAuthJSON returns the cached snapshot for ChatGPT auth, or nil. Mirrors
// CodexAuth::get_current_auth_json.
func (a CodexAuth) currentAuthJSON() *AuthDotJson {
	if a.Kind == CodexAuthChatgpt || a.Kind == CodexAuthChatgptAuthTokens {
		return a.authDotJson
	}
	return nil
}

// currentTokenData returns the cached token data for ChatGPT auth, or nil.
// Mirrors CodexAuth::get_current_token_data.
func (a CodexAuth) currentTokenData() *TokenData {
	auth := a.currentAuthJSON()
	if auth == nil {
		return nil
	}
	return auth.Tokens
}

// ShouldRefreshProactively reports whether managed ChatGPT auth needs a proactive
// refresh: when the access token expires within the 5-minute window, or when the
// last refresh is older than the refresh interval. Mirrors
// AuthManager::should_refresh_proactively.
func (a CodexAuth) ShouldRefreshProactively() bool {
	if a.Kind != CodexAuthChatgpt {
		return false
	}
	auth := a.currentAuthJSON()
	if auth == nil {
		return false
	}
	if auth.Tokens != nil {
		if expiresAt, err := ParseJWTExpiration(auth.Tokens.AccessToken); err == nil && expiresAt != nil {
			return !expiresAt.After(now().Add(chatgptAccessTokenRefreshWindowDuration))
		}
	}
	if auth.LastRefresh == nil {
		return false
	}
	return auth.LastRefresh.Before(now().Add(-tokenRefreshIntervalDuration))
}
