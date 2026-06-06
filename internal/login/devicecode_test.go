package login

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
)

const (
	workspaceIDAllowed    = "123e4567-e89b-42d3-a456-426614174000"
	workspaceIDDisallowed = "123e4567-e89b-42d3-a456-426614174002"
)

// deviceJWT builds a JWT carrying the given auth-namespace claims, mirroring the
// make_jwt helper in device_code_login.rs.
func deviceJWT(t *testing.T, authClaims map[string]any) string {
	t.Helper()
	payload := map[string]any{}
	if authClaims != nil {
		payload["https://api.openai.com/auth"] = authClaims
	}
	return fakeJWT(t, payload)
}

// deviceTestServer wires the three device-auth endpoints onto an httptest server:
// usercode, token (driven by a per-call handler), and the OAuth token exchange.
type deviceTestServer struct {
	server      *httptest.Server
	tokenCalls  atomic.Int64
	tokenHandle func(call int64, w http.ResponseWriter, r *http.Request)
}

func newDeviceTestServer(t *testing.T, usercode func(http.ResponseWriter, *http.Request), oauthJWT string) *deviceTestServer {
	t.Helper()
	d := &deviceTestServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/usercode", usercode)
	mux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, r *http.Request) {
		call := d.tokenCalls.Add(1) - 1
		if d.tokenHandle != nil {
			d.tokenHandle(call, w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id_token":      oauthJWT,
			"access_token":  "access-token-123",
			"refresh_token": "refresh-token-123",
		})
	})
	d.server = httptest.NewServer(mux)
	t.Cleanup(d.server.Close)
	return d
}

func usercodeSuccess(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"device_auth_id": "device-auth-123",
		"user_code":      "CODE-12345",
		"interval":       "0",
	})
}

func deviceOpts(home, issuer string) ServerOptions {
	return ServerOptions{
		CodexHome:                   home,
		ClientID:                    "client-id",
		Issuer:                      issuer,
		CLIAuthCredentialsStoreMode: config.AuthCredentialsStoreFile,
	}
}

// TestDeviceCodeLoginSucceeds mirrors device_code_login_integration_succeeds: a
// 404-then-200 token poll yields tokens persisted to auth.json.
func TestDeviceCodeLoginSucceeds(t *testing.T) {
	home := t.TempDir()
	jwt := deviceJWT(t, map[string]any{"chatgpt_account_id": workspaceIDAllowed})
	d := newDeviceTestServer(t, usercodeSuccess, jwt)
	d.tokenHandle = twoStepTokenHandler(http.StatusNotFound)

	opts := deviceOpts(home, d.server.URL)
	if err := RunDeviceCodeLogin(context.Background(), d.server.Client(), opts, &bytes.Buffer{}, "0.136.0"); err != nil {
		t.Fatalf("RunDeviceCodeLogin: %v", err)
	}

	auth, err := LoadAuthDotJson(home, config.AuthCredentialsStoreFile)
	if err != nil {
		t.Fatalf("LoadAuthDotJson: %v", err)
	}
	if auth == nil || auth.Tokens == nil {
		t.Fatal("auth.json not written")
	}
	if auth.Tokens.AccessToken != "access-token-123" || auth.Tokens.RefreshToken != "refresh-token-123" {
		t.Errorf("tokens = %+v", auth.Tokens)
	}
	if auth.Tokens.IDToken.RawJWT != jwt {
		t.Errorf("id token raw = %q, want %q", auth.Tokens.IDToken.RawJWT, jwt)
	}
	if auth.Tokens.AccountID == nil || *auth.Tokens.AccountID != workspaceIDAllowed {
		t.Errorf("account id = %v, want %s", auth.Tokens.AccountID, workspaceIDAllowed)
	}
	// Device flow never exchanges for an API key (persist_tokens_async api_key=None).
	if auth.OpenAIAPIKey != nil {
		t.Errorf("openai_api_key = %v, want nil", auth.OpenAIAPIKey)
	}
	if d.tokenCalls.Load() != 2 {
		t.Errorf("token polled %d times, want 2", d.tokenCalls.Load())
	}
}

// TestDeviceCodeLoginRejectsWorkspaceMismatch mirrors
// device_code_login_rejects_workspace_mismatch.
func TestDeviceCodeLoginRejectsWorkspaceMismatch(t *testing.T) {
	home := t.TempDir()
	jwt := deviceJWT(t, map[string]any{
		"chatgpt_account_id": workspaceIDDisallowed,
		"organization_id":    workspaceIDDisallowed,
	})
	d := newDeviceTestServer(t, usercodeSuccess, jwt)
	d.tokenHandle = twoStepTokenHandler(http.StatusNotFound)

	opts := deviceOpts(home, d.server.URL)
	opts.ForcedChatgptWorkspaceID = []string{workspaceIDAllowed}

	err := RunDeviceCodeLogin(context.Background(), d.server.Client(), opts, &bytes.Buffer{}, "0.136.0")
	if err == nil {
		t.Fatal("expected workspace-mismatch error")
	}
	if !IsDeviceCodePermissionDenied(err) {
		t.Errorf("err = %v, want permission-denied", err)
	}
	auth, _ := LoadAuthDotJson(home, config.AuthCredentialsStoreFile)
	if auth != nil {
		t.Error("auth.json should not be created when workspace validation fails")
	}
}

// TestDeviceCodeLoginUsercodeHTTPFailure mirrors
// device_code_login_integration_handles_usercode_http_failure.
func TestDeviceCodeLoginUsercodeHTTPFailure(t *testing.T) {
	home := t.TempDir()
	d := newDeviceTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}, "")

	opts := deviceOpts(home, d.server.URL)
	err := RunDeviceCodeLogin(context.Background(), d.server.Client(), opts, &bytes.Buffer{}, "0.136.0")
	if err == nil || !strings.Contains(err.Error(), "device code request failed with status") {
		t.Fatalf("err = %v, want 'device code request failed with status'", err)
	}
	if auth, _ := LoadAuthDotJson(home, config.AuthCredentialsStoreFile); auth != nil {
		t.Error("auth.json should not be created when login fails")
	}
}

// TestDeviceCodeLoginNotEnabled covers the 404-on-usercode branch -> the "not
// enabled" message and the IsDeviceCodeNotEnabled signal used by the browser
// fallback.
func TestDeviceCodeLoginNotEnabled(t *testing.T) {
	home := t.TempDir()
	d := newDeviceTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}, "")

	opts := deviceOpts(home, d.server.URL)
	err := RunDeviceCodeLogin(context.Background(), d.server.Client(), opts, &bytes.Buffer{}, "0.136.0")
	if err == nil || !IsDeviceCodeNotEnabled(err) {
		t.Fatalf("err = %v, want device-code-not-enabled", err)
	}
	want := "device code login is not enabled for this Codex server. Use the browser login or verify the server URL."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// TestDeviceCodeLoginPersistsWithoutAPIKey mirrors
// device_code_login_integration_persists_without_api_key_on_exchange_failure: an
// empty-auth JWT still persists tokens with no API key.
func TestDeviceCodeLoginPersistsWithoutAPIKey(t *testing.T) {
	home := t.TempDir()
	jwt := deviceJWT(t, nil)
	d := newDeviceTestServer(t, usercodeSuccess, jwt)
	d.tokenHandle = twoStepTokenHandler(http.StatusNotFound)

	opts := deviceOpts(home, d.server.URL)
	if err := RunDeviceCodeLogin(context.Background(), d.server.Client(), opts, &bytes.Buffer{}, "0.136.0"); err != nil {
		t.Fatalf("RunDeviceCodeLogin: %v", err)
	}
	auth, _ := LoadAuthDotJson(home, config.AuthCredentialsStoreFile)
	if auth == nil || auth.Tokens == nil {
		t.Fatal("auth.json not written")
	}
	if auth.OpenAIAPIKey != nil {
		t.Errorf("openai_api_key = %v, want nil", auth.OpenAIAPIKey)
	}
	if auth.Tokens.AccessToken != "access-token-123" {
		t.Errorf("access token = %q", auth.Tokens.AccessToken)
	}
}

// TestDeviceCodeLoginErrorPayload mirrors
// device_code_login_integration_handles_error_payload: a 401 with an error body
// surfaces a failure and writes no auth.json.
func TestDeviceCodeLoginErrorPayload(t *testing.T) {
	home := t.TempDir()
	d := newDeviceTestServer(t, usercodeSuccess, "")
	d.tokenHandle = func(_ int64, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"authorization_declined","error_description":"Denied"}`))
	}

	opts := deviceOpts(home, d.server.URL)
	err := RunDeviceCodeLogin(context.Background(), d.server.Client(), opts, &bytes.Buffer{}, "0.136.0")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want a 401 device-auth failure", err)
	}
	if auth, _ := LoadAuthDotJson(home, config.AuthCredentialsStoreFile); auth != nil {
		t.Error("auth.json should not be created when device auth fails")
	}
}

// twoStepTokenHandler returns firstStatus on the first poll and a success body on
// the second, mirroring mock_poll_token_two_step.
func twoStepTokenHandler(firstStatus int) func(int64, http.ResponseWriter, *http.Request) {
	return func(call int64, w http.ResponseWriter, _ *http.Request) {
		if call == 0 {
			w.WriteHeader(firstStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"authorization_code": "poll-code-321",
			"code_challenge":     "code-challenge-321",
			"code_verifier":      "code-verifier-321",
		})
	}
}

// --- Polling state-machine unit tests (pollForToken directly) ---

// fakeClock advances a virtual clock by the requested sleep so the timeout branch
// is reachable without real waiting.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	return nil
}

func TestPollForTokenSuccessImmediate(t *testing.T) {
	base, _ := pollBase(t, func(call int64, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_code":"ac","code_challenge":"ch","code_verifier":"cv"}`))
	})
	clock := &fakeClock{t: time.Unix(0, 0)}
	resp, err := pollForToken(context.Background(), http.DefaultClient, base, "id", "code", 5, clock.sleep, clock.t, clock.now)
	if err != nil {
		t.Fatalf("pollForToken: %v", err)
	}
	if resp.AuthorizationCode != "ac" {
		t.Errorf("authorization_code = %q, want ac", resp.AuthorizationCode)
	}
}

func TestPollForTokenPendingRetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int64
	base, _ := pollBase(t, func(call int64, w http.ResponseWriter) {
		if calls.Add(1) <= 3 {
			w.WriteHeader(http.StatusForbidden) // pending via 403
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_code":"ac","code_challenge":"ch","code_verifier":"cv"}`))
	})
	clock := &fakeClock{t: time.Unix(0, 0)}
	resp, err := pollForToken(context.Background(), http.DefaultClient, base, "id", "code", 1, clock.sleep, clock.t, clock.now)
	if err != nil {
		t.Fatalf("pollForToken: %v", err)
	}
	if resp.AuthorizationCode != "ac" {
		t.Errorf("authorization_code = %q, want ac", resp.AuthorizationCode)
	}
}

func TestPollForTokenDeniedNonRetryable(t *testing.T) {
	base, _ := pollBase(t, func(call int64, w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	clock := &fakeClock{t: time.Unix(0, 0)}
	_, err := pollForToken(context.Background(), http.DefaultClient, base, "id", "code", 1, clock.sleep, clock.t, clock.now)
	if err == nil || !strings.Contains(err.Error(), "device auth failed with status") {
		t.Fatalf("err = %v, want 'device auth failed with status'", err)
	}
}

func TestPollForTokenExpires(t *testing.T) {
	base, _ := pollBase(t, func(call int64, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound) // always pending
	})
	clock := &fakeClock{t: time.Unix(0, 0)}
	_, err := pollForToken(context.Background(), http.DefaultClient, base, "id", "code", 60, clock.sleep, clock.t, clock.now)
	if err == nil || !strings.Contains(err.Error(), "device auth timed out after 15 minutes") {
		t.Fatalf("err = %v, want timeout error", err)
	}
}

// pollBase spins up a server serving only the token endpoint and returns the api
// base URL ({server}/api/accounts).
func pollBase(t *testing.T, handle func(call int64, w http.ResponseWriter)) (string, *httptest.Server) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handle(calls.Add(1)-1, w)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api/accounts", srv
}

// TestPrintDeviceCodePrompt asserts the prompt text is byte-identical to
// print_device_code_prompt.
func TestPrintDeviceCodePrompt(t *testing.T) {
	var buf bytes.Buffer
	PrintDeviceCodePrompt(&buf, "https://auth.example/codex/device", "CODE-12345", "0.136.0")
	want := "\nWelcome to Codex [v\x1b[90m0.136.0\x1b[0m]\n\x1b[90mOpenAI's command-line coding agent\x1b[0m\n" +
		"\nFollow these steps to sign in with ChatGPT using device code authorization:\n" +
		"\n1. Open this link in your browser and sign in to your account\n   \x1b[94mhttps://auth.example/codex/device\x1b[0m\n" +
		"\n2. Enter this one-time code \x1b[90m(expires in 15 minutes)\x1b[0m\n   \x1b[94mCODE-12345\x1b[0m\n" +
		"\n\x1b[90mDevice codes are a common phishing target. Never share this code.\x1b[0m\n"
	if buf.String() != want {
		t.Errorf("prompt mismatch:\n got %q\nwant %q", buf.String(), want)
	}
}
