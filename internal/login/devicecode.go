package login

// This file is a faithful Go port of codex-rs/login/src/device_code_auth.rs: the
// OAuth device-authorization login flow used by `codex login --device-auth` on
// headless/remote machines.
//
// Rust mirrors:
//   - request_user_code        -> requestUserCode
//   - request_device_code      -> RequestDeviceCode
//   - poll_for_token           -> pollForToken
//   - print_device_code_prompt -> PrintDeviceCodePrompt
//   - complete_device_code_login -> CompleteDeviceCodeLogin
//   - run_device_code_login    -> RunDeviceCodeLogin
//
// The endpoint base ({issuer}/api/accounts), the user_code/token request bodies,
// the polling state machine (success / 403|404 retry on interval with a 15-minute
// cap / other -> error), and the user-facing strings are byte-identical to codex.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ANSI color codes used in the device-code prompt. Mirror device_code_auth.rs's
// ANSI_BLUE / ANSI_GRAY / ANSI_RESET verbatim.
const (
	ansiBlue  = "\x1b[94m"
	ansiGray  = "\x1b[90m"
	ansiReset = "\x1b[0m"
)

// deviceCodeMaxWait bounds the polling loop. Mirrors max_wait = 15 * 60s.
const deviceCodeMaxWait = 15 * time.Minute

// DeviceCode holds the data needed to prompt the user and poll for completion.
// Mirrors device_code_auth::DeviceCode.
type DeviceCode struct {
	VerificationURL string
	UserCode        string
	deviceAuthID    string
	interval        uint64
}

// userCodeResp mirrors device_code_auth::UserCodeResp. The interval is sent as a
// string and parsed (deserialize_interval); user_code accepts the "usercode"
// alias too.
type userCodeResp struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	UserCodeAlt  string `json:"usercode"`
	Interval     string `json:"interval"`
}

// codeSuccessResp mirrors device_code_auth::CodeSuccessResp.
type codeSuccessResp struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

// deviceAuthPoller abstracts the sleep between polls so tests can drive the state
// machine without real time. The default sleeps with context cancellation.
type deviceAuthPoller func(ctx context.Context, d time.Duration) error

func realSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// requestUserCode requests the device user code and polling interval. Mirrors
// request_user_code: POST {apiBaseURL}/deviceauth/usercode with a JSON
// {"client_id": ...} body. A 404 maps to the "not enabled" message; other
// non-2xx map to "device code request failed with status {status}".
func requestUserCode(ctx context.Context, httpClient *http.Client, apiBaseURL, clientID string) (userCodeResp, error) {
	url := apiBaseURL + "/deviceauth/usercode"
	body, err := json.Marshal(struct {
		ClientID string `json:"client_id"`
	}{ClientID: clientID})
	if err != nil {
		return userCodeResp{}, fmt.Errorf("encode usercode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return userCodeResp{}, fmt.Errorf("build usercode request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return userCodeResp{}, fmt.Errorf("usercode transport failure: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return userCodeResp{}, &deviceCodeNotEnabledError{}
		}
		return userCodeResp{}, fmt.Errorf("device code request failed with status %s", statusString(resp.StatusCode))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return userCodeResp{}, fmt.Errorf("read usercode response: %w", err)
	}
	var parsed userCodeResp
	if err := json.Unmarshal(data, &parsed); err != nil {
		return userCodeResp{}, fmt.Errorf("decode usercode response: %w", err)
	}
	return parsed, nil
}

// userCode returns the user code, honoring the "usercode" alias.
func (r userCodeResp) userCode() string {
	if r.UserCode != "" {
		return r.UserCode
	}
	return r.UserCodeAlt
}

// intervalSeconds parses the string interval (defaulting to 0 when empty, as
// serde's #[serde(default)] would). Mirrors deserialize_interval.
func (r userCodeResp) intervalSeconds() (uint64, error) {
	trimmed := strings.TrimSpace(r.Interval)
	if trimmed == "" {
		return 0, nil
	}
	v, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid interval %q: %w", r.Interval, err)
	}
	return v, nil
}

// pollForToken polls the token endpoint until a code is issued or the 15-minute
// cap is hit. Mirrors poll_for_token: success -> CodeSuccessResp; 403 or 404 ->
// sleep min(interval, remaining) and retry; anything else -> error. On timeout
// it returns "device auth timed out after 15 minutes".
func pollForToken(ctx context.Context, httpClient *http.Client, apiBaseURL, deviceAuthID, userCode string, interval uint64, sleep deviceAuthPoller, start time.Time, now func() time.Time) (codeSuccessResp, error) {
	url := apiBaseURL + "/deviceauth/token"
	for {
		body, err := json.Marshal(struct {
			DeviceAuthID string `json:"device_auth_id"`
			UserCode     string `json:"user_code"`
		}{DeviceAuthID: deviceAuthID, UserCode: userCode})
		if err != nil {
			return codeSuccessResp{}, fmt.Errorf("encode token request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
		if err != nil {
			return codeSuccessResp{}, fmt.Errorf("build token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return codeSuccessResp{}, fmt.Errorf("token transport failure: %w", err)
		}
		status := resp.StatusCode

		if status >= 200 && status < 300 {
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return codeSuccessResp{}, fmt.Errorf("read token response: %w", readErr)
			}
			var parsed codeSuccessResp
			if err := json.Unmarshal(data, &parsed); err != nil {
				return codeSuccessResp{}, fmt.Errorf("decode token response: %w", err)
			}
			return parsed, nil
		}
		resp.Body.Close()

		if status == http.StatusForbidden || status == http.StatusNotFound {
			elapsed := now().Sub(start)
			if elapsed >= deviceCodeMaxWait {
				return codeSuccessResp{}, fmt.Errorf("device auth timed out after 15 minutes")
			}
			sleepFor := time.Duration(interval) * time.Second
			if remaining := deviceCodeMaxWait - elapsed; sleepFor > remaining {
				sleepFor = remaining
			}
			if err := sleep(ctx, sleepFor); err != nil {
				return codeSuccessResp{}, err
			}
			continue
		}

		return codeSuccessResp{}, fmt.Errorf("device auth failed with status %s", statusString(status))
	}
}

// PrintDeviceCodePrompt prints the sign-in instructions. Mirrors
// print_device_code_prompt byte-for-byte, including ANSI codes and the embedded
// CLI version (env!("CARGO_PKG_VERSION")).
func PrintDeviceCodePrompt(w io.Writer, verificationURL, code, version string) {
	fmt.Fprintf(w,
		"\nWelcome to Codex [v%s%s%s]\n%sOpenAI's command-line coding agent%s\n"+
			"\nFollow these steps to sign in with ChatGPT using device code authorization:\n"+
			"\n1. Open this link in your browser and sign in to your account\n   %s%s%s\n"+
			"\n2. Enter this one-time code %s(expires in 15 minutes)%s\n   %s%s%s\n"+
			"\n%sDevice codes are a common phishing target. Never share this code.%s\n",
		ansiGray, version, ansiReset, ansiGray, ansiReset,
		ansiBlue, verificationURL, ansiReset,
		ansiGray, ansiReset, ansiBlue, code, ansiReset,
		ansiGray, ansiReset,
	)
}

// RequestDeviceCode requests a device code from the issuer. Mirrors
// request_device_code: it derives the api base ({issuer}/api/accounts), requests
// the user code, and returns the verification URL ({issuer}/codex/device).
func RequestDeviceCode(ctx context.Context, httpClient *http.Client, opts ServerOptions) (DeviceCode, error) {
	baseURL := trimTrailingSlash(opts.Issuer)
	apiBaseURL := baseURL + "/api/accounts"
	uc, err := requestUserCode(ctx, httpClient, apiBaseURL, opts.ClientID)
	if err != nil {
		return DeviceCode{}, err
	}
	interval, err := uc.intervalSeconds()
	if err != nil {
		return DeviceCode{}, err
	}
	return DeviceCode{
		VerificationURL: baseURL + "/codex/device",
		UserCode:        uc.userCode(),
		deviceAuthID:    uc.DeviceAuthID,
		interval:        interval,
	}, nil
}

// CompleteDeviceCodeLogin polls for the authorization code, exchanges it for
// tokens, validates the workspace restriction, and persists credentials. Mirrors
// complete_device_code_login. The redirect URI is {issuer}/deviceauth/callback.
func CompleteDeviceCodeLogin(ctx context.Context, httpClient *http.Client, opts ServerOptions, deviceCode DeviceCode) error {
	baseURL := trimTrailingSlash(opts.Issuer)
	apiBaseURL := baseURL + "/api/accounts"

	codeResp, err := pollForToken(ctx, httpClient, apiBaseURL, deviceCode.deviceAuthID, deviceCode.UserCode, deviceCode.interval, realSleep, time.Now(), time.Now)
	if err != nil {
		return err
	}

	pkce := PkceCodes{
		CodeVerifier:  codeResp.CodeVerifier,
		CodeChallenge: codeResp.CodeChallenge,
	}
	redirectURI := baseURL + "/deviceauth/callback"

	tokens, err := ExchangeCodeForTokens(ctx, httpClient, baseURL, opts.ClientID, redirectURI, pkce, codeResp.AuthorizationCode)
	if err != nil {
		return fmt.Errorf("device code exchange failed: %w", err)
	}

	if err := EnsureWorkspaceAllowed(opts.ForcedChatgptWorkspaceID, tokens.IDToken); err != nil {
		return &deviceCodePermissionError{message: err.Error()}
	}

	return PersistTokens(ctx, httpClient, opts.CodexHome, nil, tokens.IDToken, tokens.AccessToken, tokens.RefreshToken, opts.CLIAuthCredentialsStoreMode)
}

// RunDeviceCodeLogin runs the full device-code flow: request the code, print the
// prompt to w, then poll-and-persist. Mirrors run_device_code_login.
func RunDeviceCodeLogin(ctx context.Context, httpClient *http.Client, opts ServerOptions, promptWriter io.Writer, version string) error {
	deviceCode, err := RequestDeviceCode(ctx, httpClient, opts)
	if err != nil {
		return err
	}
	PrintDeviceCodePrompt(promptWriter, deviceCode.VerificationURL, deviceCode.UserCode, version)
	return CompleteDeviceCodeLogin(ctx, httpClient, opts, deviceCode)
}

// statusString renders an HTTP status the way reqwest's StatusCode Display does:
// "{code} {canonical reason}" (e.g. "503 Service Unavailable"). The Rust error
// strings interpolate this Display form.
func statusString(code int) string {
	reason := http.StatusText(code)
	if reason == "" {
		return strconv.Itoa(code)
	}
	return strconv.Itoa(code) + " " + reason
}

// deviceCodeNotEnabledError mirrors the io::ErrorKind::NotFound branch of
// request_user_code. The CLI fallback path keys off this (Rust matches
// ErrorKind::NotFound); IsDeviceCodeNotEnabled exposes the same signal.
type deviceCodeNotEnabledError struct{}

func (e *deviceCodeNotEnabledError) Error() string {
	return "device code login is not enabled for this Codex server. Use the browser login or verify the server URL."
}

// IsDeviceCodeNotEnabled reports whether err is the "device code not enabled"
// signal (the analogue of Rust's err.kind() == ErrorKind::NotFound check used by
// run_login_with_device_code_fallback_to_browser).
func IsDeviceCodeNotEnabled(err error) bool {
	var target *deviceCodeNotEnabledError
	return errors.As(err, &target)
}

// deviceCodePermissionError mirrors the io::ErrorKind::PermissionDenied branch of
// complete_device_code_login (workspace restriction violation).
type deviceCodePermissionError struct{ message string }

func (e *deviceCodePermissionError) Error() string { return e.message }

// IsDeviceCodePermissionDenied reports whether err is the workspace-restriction
// permission error.
func IsDeviceCodePermissionDenied(err error) bool {
	var target *deviceCodePermissionError
	return errors.As(err, &target)
}
