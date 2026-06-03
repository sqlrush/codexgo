package login

import (
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// chatgptAuthFromPayload builds a managed-ChatGPT CodexAuth backed by an
// AuthDotJson whose id_token carries the supplied auth claims.
func chatgptAuthFromPayload(t *testing.T, authClaims map[string]any, lastRefresh *time.Time) CodexAuth {
	t.Helper()
	payload := map[string]any{"https://api.openai.com/auth": authClaims}
	if email, ok := authClaims["__email"]; ok {
		payload["email"] = email
		delete(authClaims, "__email")
	}
	jwt := fakeJWT(t, payload)
	info, err := ParseChatgptJWTClaims(jwt)
	if err != nil {
		t.Fatalf("parse jwt: %v", err)
	}
	mode := appserverproto.AuthModeChatgpt
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if lastRefresh != nil {
		ts = *lastRefresh
	}
	return chatgptAuth(&AuthDotJson{
		AuthMode:    &mode,
		Tokens:      &TokenData{IDToken: info, AccessToken: "acc", RefreshToken: "ref", AccountID: strp("acct")},
		LastRefresh: &ts,
	})
}

func TestCodexAuthAPIKeyAccessors(t *testing.T) {
	auth := FromAPIKey("sk-xyz")
	if !auth.IsAPIKeyAuth() {
		t.Errorf("IsAPIKeyAuth = false")
	}
	if auth.IsChatgptAuth() || auth.UsesCodexBackend() {
		t.Errorf("api key auth should not be chatgpt/codex-backend")
	}
	if auth.AuthMode() != appserverproto.AuthModeApiKey {
		t.Errorf("AuthMode = %q", auth.AuthMode())
	}
	if auth.APIAuthMode() != appserverproto.AuthModeApiKey {
		t.Errorf("APIAuthMode = %q", auth.APIAuthMode())
	}
	if k := auth.APIKey(); k == nil || *k != "sk-xyz" {
		t.Errorf("APIKey = %v", k)
	}
	tok, err := auth.GetToken()
	if err != nil || tok != "sk-xyz" {
		t.Errorf("GetToken = (%q,%v)", tok, err)
	}
	if auth.GetAccountID() != nil || auth.GetAccountEmail() != nil {
		t.Errorf("api key auth should have no account id/email")
	}
}

func TestCodexAuthChatgptAccessors(t *testing.T) {
	auth := chatgptAuthFromPayload(t, map[string]any{
		"__email":                    "user@example.com",
		"chatgpt_account_id":         "acct",
		"chatgpt_user_id":            "uid-1",
		"chatgpt_plan_type":          "pro",
		"chatgpt_account_is_fedramp": true,
	}, nil)

	if !auth.IsChatgptAuth() || !auth.UsesCodexBackend() {
		t.Errorf("chatgpt auth flags wrong")
	}
	if auth.AuthMode() != appserverproto.AuthModeChatgpt {
		t.Errorf("AuthMode = %q", auth.AuthMode())
	}
	if auth.APIAuthMode() != appserverproto.AuthModeChatgpt {
		t.Errorf("APIAuthMode = %q", auth.APIAuthMode())
	}
	if id := auth.GetAccountID(); id == nil || *id != "acct" {
		t.Errorf("GetAccountID = %v", id)
	}
	if email := auth.GetAccountEmail(); email == nil || *email != "user@example.com" {
		t.Errorf("GetAccountEmail = %v", email)
	}
	if uid := auth.GetChatgptUserID(); uid == nil || *uid != "uid-1" {
		t.Errorf("GetChatgptUserID = %v", uid)
	}
	if !auth.IsFedrampAccount() {
		t.Errorf("IsFedrampAccount = false")
	}
	plan := auth.AccountPlanType()
	if plan == nil || *plan != protocol.PlanTypePro {
		t.Errorf("AccountPlanType = %v, want pro", plan)
	}
	tok, err := auth.GetToken()
	if err != nil || tok != "acc" {
		t.Errorf("GetToken = (%q,%v)", tok, err)
	}
}

func TestCodexAuthAgentIdentityAccessors(t *testing.T) {
	record := AgentIdentityAuthRecord{
		AgentRuntimeID:          "rt",
		AccountID:               "acct-ai",
		ChatgptUserID:           "uid-ai",
		Email:                   "agent@example.com",
		PlanType:                protocol.PlanTypeEnterprise,
		ChatgptAccountIsFedramp: true,
	}
	auth := agentIdentityAuth(record)
	if auth.AuthMode() != appserverproto.AuthModeAgentIdentity {
		t.Errorf("AuthMode = %q", auth.AuthMode())
	}
	if !auth.UsesCodexBackend() || auth.IsChatgptAuth() {
		t.Errorf("agent identity backend flags wrong")
	}
	if id := auth.GetAccountID(); id == nil || *id != "acct-ai" {
		t.Errorf("GetAccountID = %v", id)
	}
	if email := auth.GetAccountEmail(); email == nil || *email != "agent@example.com" {
		t.Errorf("GetAccountEmail = %v", email)
	}
	if !auth.IsFedrampAccount() {
		t.Errorf("IsFedrampAccount = false")
	}
	plan := auth.AccountPlanType()
	if plan == nil || *plan != protocol.PlanTypeEnterprise {
		t.Errorf("AccountPlanType = %v, want enterprise", plan)
	}
	if !auth.IsWorkspaceAccount() {
		t.Errorf("enterprise should be a workspace account")
	}
	if _, err := auth.GetToken(); err == nil {
		t.Errorf("agent identity GetToken should error")
	}
	if auth.Record() == nil || auth.Record().AgentRuntimeID != "rt" {
		t.Errorf("Record mismatch")
	}
}

func TestCodexAuthGetTokenDataRequiresLastRefresh(t *testing.T) {
	mode := appserverproto.AuthModeChatgpt
	// Tokens present but no last_refresh -> unavailable.
	auth := chatgptAuth(&AuthDotJson{AuthMode: &mode, Tokens: &TokenData{AccessToken: "a"}})
	if _, err := auth.GetTokenData(); err == nil {
		t.Errorf("expected ErrTokenDataUnavailable without last_refresh")
	}
}

func TestShouldRefreshProactively(t *testing.T) {
	fixedNow := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	restore := now
	now = func() time.Time { return fixedNow }
	t.Cleanup(func() { now = restore })

	// Access token expiring within the 5-minute window -> refresh.
	expiringSoon := fixedNow.Add(2 * time.Minute)
	authSoon := buildChatgptAuthWithAccessExp(t, expiringSoon, fixedNow)
	if !authSoon.ShouldRefreshProactively() {
		t.Errorf("expected proactive refresh for soon-expiring token")
	}

	// Access token far in the future -> no refresh.
	expiringLater := fixedNow.Add(2 * time.Hour)
	authLater := buildChatgptAuthWithAccessExp(t, expiringLater, fixedNow)
	if authLater.ShouldRefreshProactively() {
		t.Errorf("did not expect refresh for far-future token")
	}

	// External-tokens variant never proactively refreshes.
	mode := appserverproto.AuthModeChatgptAuthTokens
	ts := fixedNow.Add(-30 * 24 * time.Hour)
	external := chatgptAuthTokens(&AuthDotJson{AuthMode: &mode, LastRefresh: &ts})
	if external.ShouldRefreshProactively() {
		t.Errorf("external tokens should not proactively refresh")
	}

	// No parseable exp, stale last_refresh older than 8 days -> refresh.
	staleTs := fixedNow.Add(-9 * 24 * time.Hour)
	jwt := fakeJWT(t, map[string]any{"sub": "x"})
	info, err := ParseChatgptJWTClaims(jwt)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	chat := appserverproto.AuthModeChatgpt
	stale := chatgptAuth(&AuthDotJson{
		AuthMode:    &chat,
		Tokens:      &TokenData{IDToken: info, AccessToken: jwt},
		LastRefresh: &staleTs,
	})
	if !stale.ShouldRefreshProactively() {
		t.Errorf("expected refresh for stale last_refresh")
	}
}

// buildChatgptAuthWithAccessExp builds a managed-ChatGPT CodexAuth whose access
// token JWT carries the given expiration.
func buildChatgptAuthWithAccessExp(t *testing.T, exp, lastRefresh time.Time) CodexAuth {
	t.Helper()
	access := fakeJWT(t, map[string]any{"exp": exp.Unix()})
	idJWT := fakeJWT(t, map[string]any{"email": "u@example.com"})
	info, err := ParseChatgptJWTClaims(idJWT)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mode := appserverproto.AuthModeChatgpt
	return chatgptAuth(&AuthDotJson{
		AuthMode:    &mode,
		Tokens:      &TokenData{IDToken: info, AccessToken: access, RefreshToken: "ref"},
		LastRefresh: &lastRefresh,
	})
}

func TestFromAuthDotJsonAPIKey(t *testing.T) {
	key := "sk-direct"
	apiMode := appserverproto.AuthModeApiKey
	auth, err := FromAuthDotJson(nil, nil, t.TempDir(), &AuthDotJson{AuthMode: &apiMode, OpenAIAPIKey: &key}, config.AuthCredentialsStoreFile, nil)
	if err != nil {
		t.Fatalf("FromAuthDotJson: %v", err)
	}
	if !auth.IsAPIKeyAuth() {
		t.Errorf("expected api key auth")
	}
}

func TestFromAuthDotJsonAPIKeyMissingKeyErrors(t *testing.T) {
	apiMode := appserverproto.AuthModeApiKey
	if _, err := FromAuthDotJson(nil, nil, t.TempDir(), &AuthDotJson{AuthMode: &apiMode}, config.AuthCredentialsStoreFile, nil); err == nil {
		t.Errorf("expected error for api key auth without key")
	}
}
