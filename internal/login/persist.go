package login

import (
	"context"
	"net/http"

	"github.com/sqlrush/codexgo/pkg/config"
)

// persistTokens persists refreshed tokens into auth storage and updates
// last_refresh. Mirrors manager::persist_tokens: it loads the current auth,
// applies any provided id/access/refresh token, sets last_refresh to now, and
// saves. Returns the updated AuthDotJson.
func persistTokens(storage authStorageBackend, idToken, accessToken, refreshToken *string) (*AuthDotJson, error) {
	authDotJson, err := storage.load()
	if err != nil {
		return nil, err
	}
	if authDotJson == nil {
		return nil, ErrTokenDataUnavailable
	}

	if authDotJson.Tokens == nil {
		authDotJson.Tokens = &TokenData{}
	}
	if idToken != nil {
		info, err := ParseChatgptJWTClaims(*idToken)
		if err != nil {
			return nil, err
		}
		authDotJson.Tokens.IDToken = info
	}
	if accessToken != nil {
		authDotJson.Tokens.AccessToken = *accessToken
	}
	if refreshToken != nil {
		authDotJson.Tokens.RefreshToken = *refreshToken
	}
	nowTs := now()
	authDotJson.LastRefresh = &nowTs
	if err := storage.save(authDotJson); err != nil {
		return nil, err
	}
	return authDotJson, nil
}

// RefreshChatgptToken requests refreshed ChatGPT OAuth tokens using the stored
// refresh token, persists them, and returns the updated AuthDotJson. It mirrors
// the refresh half of manager::refresh_and_persist_chatgpt_token. The caller
// supplies the codex home, store mode, and an HTTP client.
func RefreshChatgptToken(ctx context.Context, httpClient *http.Client, codexHome string, mode config.AuthCredentialsStoreMode) (*AuthDotJson, *RefreshTokenError) {
	storage := createAuthStorage(codexHome, mode)
	authDotJson, err := storage.load()
	if err != nil {
		return nil, transientRefreshError(err)
	}
	if authDotJson == nil || authDotJson.Tokens == nil {
		return nil, transientRefreshError(ErrTokenDataUnavailable)
	}

	resp, refreshErr := requestChatgptTokenRefresh(ctx, httpClient, authDotJson.Tokens.RefreshToken)
	if refreshErr != nil {
		return nil, refreshErr
	}

	updated, err := persistTokens(storage, resp.IDToken, resp.AccessToken, resp.RefreshToken)
	if err != nil {
		return nil, transientRefreshError(err)
	}
	return updated, nil
}
