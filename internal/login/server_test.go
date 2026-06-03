package login

import (
	"strings"
	"testing"
)

// TestURLEncode locks in byte-for-byte parity with the Rust `urlencoding::encode`
// crate: spaces become "%20" (not "+"), and all bytes outside the RFC 3986
// unreserved set are percent-encoded with uppercase hex.
func TestURLEncode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"abcXYZ012-_.~", "abcXYZ012-_.~"},
		{"a b", "a%20b"},
		{
			"openid profile email offline_access api.connectors.read api.connectors.invoke",
			"openid%20profile%20email%20offline_access%20api.connectors.read%20api.connectors.invoke",
		},
		{"urn:ietf:params:oauth:grant-type:token-exchange", "urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange"},
		{"a+b/c?d=e&f", "a%2Bb%2Fc%3Fd%3De%26f"},
		{"café", "caf%C3%A9"},
	}
	for _, c := range cases {
		if got := urlEncode(c.in); got != c.want {
			t.Errorf("urlEncode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	pkce := PkceCodes{CodeVerifier: "ver ifier", CodeChallenge: "chal lenge"}
	got := BuildAuthorizeURL(DefaultIssuer, ClientID, redirectURIForPort(DefaultPort), pkce, "the state", nil)

	wantPrefix := DefaultIssuer + "/oauth/authorize?"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("authorize url missing prefix: %q", got)
	}
	// Scope must be percent-encoded with %20 (parity with urlencoding::encode).
	if !strings.Contains(got, "scope=openid%20profile%20email%20offline_access%20api.connectors.read%20api.connectors.invoke") {
		t.Errorf("scope not encoded with %%20: %q", got)
	}
	// State spaces become %20, not +.
	if !strings.Contains(got, "state=the%20state") {
		t.Errorf("state not encoded with %%20: %q", got)
	}
	// Required fixed params in declared order.
	for _, want := range []string{
		"response_type=code",
		"client_id=" + ClientID,
		"code_challenge=chal%20lenge",
		"code_challenge_method=S256",
		"id_token_add_organizations=true",
		"codex_cli_simplified_flow=true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("authorize url missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "allowed_workspace_id") {
		t.Errorf("allowed_workspace_id should be absent when no workspace ids: %q", got)
	}

	// With forced workspace ids the parameter is appended last.
	withWs := BuildAuthorizeURL(DefaultIssuer, ClientID, redirectURIForPort(DefaultPort), pkce, "s", []string{"ws-1", "ws-2"})
	if !strings.HasSuffix(withWs, "&allowed_workspace_id=ws-1%2Cws-2") {
		t.Errorf("allowed_workspace_id not appended correctly: %q", withWs)
	}
}

func TestComposeSuccessURL(t *testing.T) {
	idToken := fakeJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"organization_id":               "org 1",
			"project_id":                    "proj_1",
			"completed_platform_onboarding": false,
			"is_org_owner":                  true,
		},
	})
	accessToken := fakeJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "pro",
		},
	})

	got := ComposeSuccessURL(DefaultPort, DefaultIssuer, idToken, accessToken, false)
	if !strings.HasPrefix(got, "http://localhost:1455/success?") {
		t.Fatalf("success url prefix mismatch: %q", got)
	}
	// needs_setup = !completed && is_org_owner = true.
	if !strings.Contains(got, "needs_setup=true") {
		t.Errorf("needs_setup missing/incorrect: %q", got)
	}
	// org_id space must encode as %20.
	if !strings.Contains(got, "org_id=org%201") {
		t.Errorf("org_id not encoded with %%20: %q", got)
	}
	if !strings.Contains(got, "plan_type=pro") {
		t.Errorf("plan_type missing: %q", got)
	}
	if !strings.Contains(got, "platform_url=https%3A%2F%2Fplatform.openai.com") {
		t.Errorf("platform_url for default issuer incorrect: %q", got)
	}
	if strings.Contains(got, "codex_streamlined_login") {
		t.Errorf("codex_streamlined_login should be absent: %q", got)
	}

	// Non-default issuer switches the platform url; streamlined flag is appended.
	got = ComposeSuccessURL(DefaultPort, "https://auth.example.com", idToken, accessToken, true)
	if !strings.Contains(got, "platform_url=https%3A%2F%2Fplatform.api.openai.org") {
		t.Errorf("platform_url for custom issuer incorrect: %q", got)
	}
	if !strings.HasSuffix(got, "&codex_streamlined_login=true") {
		t.Errorf("codex_streamlined_login not appended: %q", got)
	}
}

func TestEnsureWorkspaceAllowed(t *testing.T) {
	idToken := fakeJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "ws-2"},
	})
	missing := fakeJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{},
	})

	if err := EnsureWorkspaceAllowed(nil, idToken); err != nil {
		t.Errorf("nil restriction should allow: %v", err)
	}
	if err := EnsureWorkspaceAllowed([]string{"ws-1", "ws-2"}, idToken); err != nil {
		t.Errorf("matching workspace should allow: %v", err)
	}
	if err := EnsureWorkspaceAllowed([]string{"ws-9"}, idToken); err == nil {
		t.Errorf("non-matching workspace should be rejected")
	}
	if err := EnsureWorkspaceAllowed([]string{"ws-1"}, missing); err == nil {
		t.Errorf("missing chatgpt_account_id claim should be rejected")
	}
}

func TestGeneratePKCE(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	// 64 random bytes -> 86-char unpadded base64url verifier.
	if len(pkce.CodeVerifier) != 86 {
		t.Errorf("verifier length = %d, want 86", len(pkce.CodeVerifier))
	}
	// SHA-256 digest -> 43-char unpadded base64url challenge.
	if len(pkce.CodeChallenge) != 43 {
		t.Errorf("challenge length = %d, want 43", len(pkce.CodeChallenge))
	}
	if strings.ContainsAny(pkce.CodeVerifier, "+/=") || strings.ContainsAny(pkce.CodeChallenge, "+/=") {
		t.Errorf("pkce codes must be url-safe without padding")
	}

	other, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if other.CodeVerifier == pkce.CodeVerifier {
		t.Errorf("expected distinct verifiers across calls")
	}
}

func TestGenerateState(t *testing.T) {
	state, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	// 32 random bytes -> 43-char unpadded base64url.
	if len(state) != 43 {
		t.Errorf("state length = %d, want 43", len(state))
	}
	if strings.ContainsAny(state, "+/=") {
		t.Errorf("state must be url-safe without padding: %q", state)
	}
}

func TestSelectBindPort(t *testing.T) {
	cases := []struct {
		name      string
		preferred int
		free      map[int]bool
		want      int
		wantErr   bool
	}{
		{"preferred free", DefaultPort, map[int]bool{DefaultPort: true}, DefaultPort, false},
		{"fallback when default busy", DefaultPort, map[int]bool{FallbackPort: true}, FallbackPort, false},
		{"both busy", DefaultPort, map[int]bool{}, 0, true},
		{"non-default has no fallback", 9000, map[int]bool{FallbackPort: true}, 0, true},
		{"non-default free", 9000, map[int]bool{9000: true}, 9000, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SelectBindPort(c.preferred, func(p int) bool { return c.free[p] })
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error, got port %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectBindPort: %v", err)
			}
			if got != c.want {
				t.Errorf("port = %d, want %d", got, c.want)
			}
		})
	}
}

func TestOAuthCallbackErrorMessage(t *testing.T) {
	missingEntitlement := "the workspace is missing_codex_entitlement for codex"
	if got := oauthCallbackErrorMessage("access_denied", &missingEntitlement); !strings.Contains(got, "Codex is not enabled") {
		t.Errorf("missing entitlement message = %q", got)
	}
	desc := "user closed the window"
	if got := oauthCallbackErrorMessage("access_denied", &desc); got != "Sign-in failed: user closed the window" {
		t.Errorf("desc message = %q", got)
	}
	if got := oauthCallbackErrorMessage("server_error", nil); got != "Sign-in failed: server_error" {
		t.Errorf("code-only message = %q", got)
	}
}

func TestParseTokenEndpointError(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty", "", "unknown error"},
		{"error_description", `{"error":"invalid_grant","error_description":"bad code"}`, "bad code"},
		{"error object message", `{"error":{"code":"x","message":"nested message"}}`, "nested message"},
		{"error code only", `{"error":"invalid_grant"}`, "invalid_grant"},
		{"non json", "plain text failure", "plain text failure"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseTokenEndpointError(c.body).displayMessage; got != c.want {
				t.Errorf("displayMessage = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHTMLEscape(t *testing.T) {
	in := `<a href="x">&'</a>`
	want := "&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;"
	if got := htmlEscape(in); got != want {
		t.Errorf("htmlEscape = %q, want %q", got, want)
	}
}
