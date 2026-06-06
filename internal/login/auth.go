package login

import (
	"os"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/config"
)

// Environment variable names for direct (non-OAuth) auth, mirroring manager.rs.
const (
	// OpenAIAPIKeyEnvVar is the OpenAI API key env var.
	OpenAIAPIKeyEnvVar = "OPENAI_API_KEY"
	// CodexAPIKeyEnvVar is the Codex API key env var (takes precedence when enabled).
	CodexAPIKeyEnvVar = "CODEXGO_API_KEY"
	// CodexAccessTokenEnvVar is the agent-identity access-token env var.
	CodexAccessTokenEnvVar = "CODEXGO_ACCESS_TOKEN"
)

// ReadOpenAIAPIKeyFromEnv returns the trimmed OPENAI_API_KEY, or nil when unset
// or empty. Mirrors read_openai_api_key_from_env.
func ReadOpenAIAPIKeyFromEnv() *string {
	return readNonEmptyEnvVar(OpenAIAPIKeyEnvVar)
}

// ReadCodexAPIKeyFromEnv returns the trimmed CODEXGO_API_KEY, or nil when unset or
// empty. Mirrors read_codex_api_key_from_env.
func ReadCodexAPIKeyFromEnv() *string {
	return readNonEmptyEnvVar(CodexAPIKeyEnvVar)
}

// ReadCodexAccessTokenFromEnv returns the trimmed CODEXGO_ACCESS_TOKEN, or nil when
// unset or empty. Mirrors read_codex_access_token_from_env.
func ReadCodexAccessTokenFromEnv() *string {
	return readNonEmptyEnvVar(CodexAccessTokenEnvVar)
}

// readNonEmptyEnvVar reads key, trims whitespace, and returns nil when the value
// is unset or empty. Mirrors read_non_empty_env_var.
func readNonEmptyEnvVar(key string) *string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// resolvedMode resolves the auth mode for an AuthDotJson, defaulting to ApiKey
// when an API key is present and Chatgpt otherwise. Mirrors
// AuthDotJson::resolved_mode.
func (a AuthDotJson) resolvedMode() appserverproto.AuthMode {
	if a.AuthMode != nil {
		return *a.AuthMode
	}
	if a.OpenAIAPIKey != nil {
		return appserverproto.AuthModeApiKey
	}
	return appserverproto.AuthModeChatgpt
}

// storageMode forces ephemeral storage for external ChatGPT tokens and otherwise
// uses the requested mode. Mirrors AuthDotJson::storage_mode.
func (a AuthDotJson) storageMode(requested config.AuthCredentialsStoreMode) config.AuthCredentialsStoreMode {
	if a.resolvedMode() == appserverproto.AuthModeChatgptAuthTokens {
		return config.AuthCredentialsStoreEphemeral
	}
	return requested
}

// LoginWithAPIKey writes an auth.json that contains only the API key. Mirrors
// login_with_api_key.
func LoginWithAPIKey(codexHome, apiKey string, mode config.AuthCredentialsStoreMode) error {
	apiKeyCopy := apiKey
	authMode := appserverproto.AuthModeApiKey
	auth := &AuthDotJson{
		AuthMode:     &authMode,
		OpenAIAPIKey: &apiKeyCopy,
	}
	return SaveAuth(codexHome, auth, mode)
}

// LoginWithAccessTokenRecord writes an auth.json for an agent-identity access
// token using a pre-verified record. The caller is responsible for verifying the
// token against the JWKS (see VerifiedAgentIdentityRecord); this records the raw
// JWT into the agent_identity field. Mirrors the persistence half of
// login_with_access_token.
func LoginWithAccessTokenRecord(codexHome, accessToken string, mode config.AuthCredentialsStoreMode) error {
	tokenCopy := accessToken
	authMode := appserverproto.AuthModeAgentIdentity
	auth := &AuthDotJson{
		AuthMode:      &authMode,
		AgentIdentity: &tokenCopy,
	}
	return SaveAuth(codexHome, auth, mode)
}

// SaveAuth persists the provided auth payload using the specified backend.
// Mirrors save_auth.
func SaveAuth(codexHome string, auth *AuthDotJson, mode config.AuthCredentialsStoreMode) error {
	storage := createAuthStorage(codexHome, mode)
	return storage.save(auth)
}

// LoadAuthDotJson loads the raw stored auth payload without applying environment
// overrides, returning nil when no credentials are stored. Mirrors
// load_auth_dot_json.
func LoadAuthDotJson(codexHome string, mode config.AuthCredentialsStoreMode) (*AuthDotJson, error) {
	storage := createAuthStorage(codexHome, mode)
	return storage.load()
}

// Logout deletes the stored auth credentials, returning whether anything was
// removed. Mirrors the free function logout.
func Logout(codexHome string, mode config.AuthCredentialsStoreMode) (bool, error) {
	storage := createAuthStorage(codexHome, mode)
	return storage.delete()
}

// LogoutAllStores clears both the ephemeral and managed stores so a forced
// logout truly removes all active auth. Mirrors logout_all_stores.
func LogoutAllStores(codexHome string, mode config.AuthCredentialsStoreMode) (bool, error) {
	if mode == config.AuthCredentialsStoreEphemeral {
		return Logout(codexHome, config.AuthCredentialsStoreEphemeral)
	}
	removedEphemeral, err := Logout(codexHome, config.AuthCredentialsStoreEphemeral)
	if err != nil {
		return false, err
	}
	removedManaged, err := Logout(codexHome, mode)
	if err != nil {
		return false, err
	}
	return removedEphemeral || removedManaged, nil
}

// now is overridable in tests; defaults to time.Now in UTC.
var now = func() time.Time { return time.Now().UTC() }
