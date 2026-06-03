package login

import (
	"os"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestClassifyRefreshTokenFailure(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantReason protocol.RefreshTokenFailedReason
		wantMsg    string
	}{
		{
			name:       "expired (string error)",
			body:       `{"error":"refresh_token_expired"}`,
			wantReason: protocol.RefreshTokenFailedExpired,
			wantMsg:    refreshTokenExpiredMessage,
		},
		{
			name:       "reused -> exhausted (object error)",
			body:       `{"error":{"code":"refresh_token_reused"}}`,
			wantReason: protocol.RefreshTokenFailedExhausted,
			wantMsg:    refreshTokenReusedMessage,
		},
		{
			name:       "invalidated -> revoked (top-level code)",
			body:       `{"code":"refresh_token_invalidated"}`,
			wantReason: protocol.RefreshTokenFailedRevoked,
			wantMsg:    refreshTokenInvalidatedMessage,
		},
		{
			name:       "uppercase normalized",
			body:       `{"error":"REFRESH_TOKEN_EXPIRED"}`,
			wantReason: protocol.RefreshTokenFailedExpired,
			wantMsg:    refreshTokenExpiredMessage,
		},
		{
			name:       "unknown code",
			body:       `{"error":"something_else"}`,
			wantReason: protocol.RefreshTokenFailedOther,
			wantMsg:    refreshTokenUnknownMessage,
		},
		{
			name:       "empty body",
			body:       "",
			wantReason: protocol.RefreshTokenFailedOther,
			wantMsg:    refreshTokenUnknownMessage,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyRefreshTokenFailure(c.body)
			if got.Reason != c.wantReason {
				t.Errorf("reason = %v, want %v", got.Reason, c.wantReason)
			}
			if got.Message != c.wantMsg {
				t.Errorf("message = %q, want %q", got.Message, c.wantMsg)
			}
		})
	}
}

func TestExtractRefreshTokenErrorCode(t *testing.T) {
	cases := []struct {
		body string
		want *string
	}{
		{`{"error":"x"}`, strp("x")},
		{`{"error":{"code":"y"}}`, strp("y")},
		{`{"code":"z"}`, strp("z")},
		{`{"other":"q"}`, nil},
		{`not json`, nil},
		{``, nil},
	}
	for _, c := range cases {
		got := extractRefreshTokenErrorCode(c.body)
		if !eqStrPtr(got, c.want) {
			t.Errorf("extractRefreshTokenErrorCode(%q) = %v, want %v", c.body, derefStr(got), derefStr(c.want))
		}
	}
}

func TestRefreshTokenErrorFailedReason(t *testing.T) {
	perm := permanentRefreshError(protocol.RefreshTokenFailedExpired, "msg")
	if perm.FailedReason() == nil || *perm.FailedReason() != protocol.RefreshTokenFailedExpired {
		t.Errorf("permanent FailedReason mismatch: %v", perm.FailedReason())
	}
	trans := transientRefreshError(errStub("boom"))
	if trans.FailedReason() != nil {
		t.Errorf("transient FailedReason should be nil")
	}
	if trans.Error() != "boom" {
		t.Errorf("transient error = %q", trans.Error())
	}
}

type errStub string

func (e errStub) Error() string { return string(e) }

func TestRevokeTokenEndpoint(t *testing.T) {
	// Ensure a clean starting environment; restore on exit.
	withUnset(t, RevokeTokenURLOverrideEnvVar)
	withUnset(t, RefreshTokenURLOverrideEnvVar)

	if got := revokeTokenEndpoint(); got != revokeTokenURL {
		t.Errorf("default revoke endpoint = %q, want %q", got, revokeTokenURL)
	}

	// Revoke override wins.
	t.Setenv(RevokeTokenURLOverrideEnvVar, "https://revoke.example/x")
	if got := revokeTokenEndpoint(); got != "https://revoke.example/x" {
		t.Errorf("override revoke endpoint = %q", got)
	}
	withUnset(t, RevokeTokenURLOverrideEnvVar)

	// Derived from refresh override.
	t.Setenv(RefreshTokenURLOverrideEnvVar, "https://auth.example/oauth/token?x=1")
	if got := revokeTokenEndpoint(); got != "https://auth.example/oauth/revoke" {
		t.Errorf("derived revoke endpoint = %q, want https://auth.example/oauth/revoke", got)
	}
}

func TestRefreshTokenEndpoint(t *testing.T) {
	withUnset(t, RefreshTokenURLOverrideEnvVar)
	if got := refreshTokenEndpoint(); got != refreshTokenURL {
		t.Errorf("default refresh endpoint = %q, want %q", got, refreshTokenURL)
	}
	t.Setenv(RefreshTokenURLOverrideEnvVar, "https://refresh.example/token")
	if got := refreshTokenEndpoint(); got != "https://refresh.example/token" {
		t.Errorf("override refresh endpoint = %q", got)
	}
}

// withUnset removes an environment variable for the duration of the test,
// restoring its previous value afterwards. t.Setenv cannot unset, so this
// helper covers the "variable absent" cases.
func withUnset(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestDeriveRevokeTokenEndpoint(t *testing.T) {
	got, ok := deriveRevokeTokenEndpoint("https://auth.example.com/oauth/token?foo=bar")
	if !ok {
		t.Fatalf("derive returned not ok")
	}
	if got != "https://auth.example.com/oauth/revoke" {
		t.Errorf("derived = %q", got)
	}
}

func TestShouldRevokeAuthTokens(t *testing.T) {
	chat := appserverproto.AuthModeChatgpt
	prev := &AuthDotJson{
		AuthMode: &chat,
		Tokens:   &TokenData{AccessToken: "a-old", RefreshToken: "r-old"},
	}

	// No replacement tokens -> revoke.
	if !shouldRevokeAuthTokens(prev, nil) {
		t.Errorf("expected revoke when replacement is nil")
	}

	// Replacement reuses the refresh token -> do not revoke.
	same := &AuthDotJson{
		AuthMode: &chat,
		Tokens:   &TokenData{AccessToken: "a-new", RefreshToken: "r-old"},
	}
	if shouldRevokeAuthTokens(prev, same) {
		t.Errorf("should not revoke when refresh token unchanged")
	}

	// Replacement has a different refresh token -> revoke.
	diff := &AuthDotJson{
		AuthMode: &chat,
		Tokens:   &TokenData{AccessToken: "a-new", RefreshToken: "r-new"},
	}
	if !shouldRevokeAuthTokens(prev, diff) {
		t.Errorf("should revoke when refresh token changed")
	}

	// API-key prev auth is not revocable.
	apiMode := appserverproto.AuthModeApiKey
	key := "sk"
	if shouldRevokeAuthTokens(&AuthDotJson{AuthMode: &apiMode, OpenAIAPIKey: &key}, diff) {
		t.Errorf("api-key auth should not be revoked")
	}
}

func TestRevocableTokenPrefersRefresh(t *testing.T) {
	chat := appserverproto.AuthModeChatgpt
	auth := &AuthDotJson{AuthMode: &chat, Tokens: &TokenData{AccessToken: "a", RefreshToken: "r"}}
	tok, kind, ok := revocableToken(auth)
	if !ok || tok != "r" || kind != revokeRefresh {
		t.Errorf("revocableToken = (%q,%v,%v), want (r,refresh,true)", tok, kind, ok)
	}
	// Only access token present.
	auth = &AuthDotJson{AuthMode: &chat, Tokens: &TokenData{AccessToken: "a"}}
	tok, kind, ok = revocableToken(auth)
	if !ok || tok != "a" || kind != revokeAccess {
		t.Errorf("revocableToken (access only) = (%q,%v,%v), want (a,access,true)", tok, kind, ok)
	}
}
