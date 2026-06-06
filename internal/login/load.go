package login

import (
	"context"
	"errors"
	"net/http"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/config"
)

// FromAuthDotJson converts a stored AuthDotJson into a CodexAuth. Mirrors
// CodexAuth::from_auth_dot_json. The agent-identity path verifies the JWT against
// the JWKS at chatgptBaseURL (defaulting to the ChatGPT backend) and requires
// network access. The httpClient is used for that verification only.
func FromAuthDotJson(ctx context.Context, httpClient *http.Client, codexHome string, auth *AuthDotJson, mode config.AuthCredentialsStoreMode, chatgptBaseURL *string) (CodexAuth, error) {
	authMode := auth.resolvedMode()

	if authMode == appserverproto.AuthModeApiKey {
		if auth.OpenAIAPIKey == nil {
			return CodexAuth{}, errors.New("API key auth is missing a key.")
		}
		return FromAPIKey(*auth.OpenAIAPIKey), nil
	}

	if authMode == appserverproto.AuthModeAgentIdentity {
		if auth.AgentIdentity == nil {
			return CodexAuth{}, errors.New("agent identity auth is missing an agent identity token.")
		}
		return FromAgentIdentityJWT(ctx, httpClient, *auth.AgentIdentity, chatgptBaseURL)
	}

	switch authMode {
	case appserverproto.AuthModeChatgpt:
		return chatgptAuth(auth), nil
	case appserverproto.AuthModeChatgptAuthTokens:
		return chatgptAuthTokens(auth), nil
	default:
		// resolvedMode never returns ApiKey/AgentIdentity here (handled above).
		return chatgptAuth(auth), nil
	}
}

// FromAgentIdentityJWT verifies an agent-identity JWT and returns a record-backed
// CodexAuth. Mirrors CodexAuth::from_agent_identity_jwt, minus the agent-task
// registration side effect (which is a separate network concern).
func FromAgentIdentityJWT(ctx context.Context, httpClient *http.Client, jwt string, chatgptBaseURL *string) (CodexAuth, error) {
	baseURL := DefaultChatgptBackendBaseURL
	if chatgptBaseURL != nil {
		baseURL = *chatgptBaseURL
	}
	baseURL = trimTrailingSlash(baseURL)
	record, err := VerifiedAgentIdentityRecord(ctx, httpClient, jwt, baseURL)
	if err != nil {
		return CodexAuth{}, err
	}
	return agentIdentityAuth(record), nil
}

// LoadAuth resolves credentials with the same precedence as manager::load_auth:
//  1. CODEXGO_API_KEY env var (only when enableCodexAPIKeyEnv is set)
//  2. external ChatGPT tokens in the ephemeral store
//  3. (unless mode is Ephemeral) CODEXGO_ACCESS_TOKEN agent-identity env var
//  4. the configured persistent store (file/keyring/auto)
//
// Returns (nil, nil) when no credentials are found.
func LoadAuth(ctx context.Context, httpClient *http.Client, codexHome string, enableCodexAPIKeyEnv bool, mode config.AuthCredentialsStoreMode, chatgptBaseURL *string) (*CodexAuth, error) {
	if enableCodexAPIKeyEnv {
		if apiKey := ReadCodexAPIKeyFromEnv(); apiKey != nil {
			auth := FromAPIKey(*apiKey)
			return &auth, nil
		}
	}

	// External ChatGPT auth tokens live in the in-memory (ephemeral) store and
	// take precedence over any persisted credentials.
	ephemeralStorage := createAuthStorage(codexHome, config.AuthCredentialsStoreEphemeral)
	ephemeralAuth, err := ephemeralStorage.load()
	if err != nil {
		return nil, err
	}
	if ephemeralAuth != nil {
		auth, err := FromAuthDotJson(ctx, httpClient, codexHome, ephemeralAuth, config.AuthCredentialsStoreEphemeral, chatgptBaseURL)
		if err != nil {
			return nil, err
		}
		return &auth, nil
	}

	if mode == config.AuthCredentialsStoreEphemeral {
		return nil, nil
	}

	if token := ReadCodexAccessTokenFromEnv(); token != nil {
		auth, err := FromAgentIdentityJWT(ctx, httpClient, *token, chatgptBaseURL)
		if err != nil {
			return nil, err
		}
		return &auth, nil
	}

	storage := createAuthStorage(codexHome, mode)
	authDotJson, err := storage.load()
	if err != nil {
		return nil, err
	}
	if authDotJson == nil {
		return nil, nil
	}
	auth, err := FromAuthDotJson(ctx, httpClient, codexHome, authDotJson, mode, chatgptBaseURL)
	if err != nil {
		return nil, err
	}
	return &auth, nil
}
