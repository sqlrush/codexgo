package mcp

import (
	"net/http"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
)

// fakeEnv builds an EnvLookup from a fixed map.
func fakeEnv(m map[string]string) EnvLookup {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

func TestBuildHTTPHeaders(t *testing.T) {
	t.Parallel()
	env := fakeEnv(map[string]string{
		"TOKEN_VAR": "secret",
		"BLANK_VAR": "   ",
		"AUTH_VAR":  "Bearer xyz",
	})

	headers := BuildHTTPHeaders(
		map[string]string{
			"X-Static":     "value",
			"Bad Name":     "ignored", // invalid token char (space)
			"X-Bad\nValue": "ignored", // invalid name
			"X-Ctrl":       "a\x01b",  // invalid value (control char)
		},
		map[string]string{
			"X-From-Env":    "TOKEN_VAR",
			"X-Blank":       "BLANK_VAR", // blank -> skipped
			"X-Missing":     "ABSENT",    // missing -> skipped
			"Authorization": "AUTH_VAR",
		},
		env,
	)

	if got := headers.Get("X-Static"); got != "value" {
		t.Errorf("X-Static=%q", got)
	}
	if got := headers.Get("X-From-Env"); got != "secret" {
		t.Errorf("X-From-Env=%q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer xyz" {
		t.Errorf("Authorization=%q", got)
	}
	for _, skipped := range []string{"X-Blank", "X-Missing", "X-Ctrl"} {
		if got := headers.Get(skipped); got != "" {
			t.Errorf("%s should be skipped, got %q", skipped, got)
		}
	}
}

func TestValidHeaderName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{name: "X-Token", want: true},
		{name: "Content-Type", want: true},
		{name: "", want: false},
		{name: "has space", want: false},
		{name: "tab\there", want: false},
		{name: "unicodeé", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := validHeaderName(tc.name); got != tc.want {
				t.Fatalf("validHeaderName(%q)=%v want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestValidHeaderValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  bool
	}{
		{value: "plain", want: true},
		{value: "with\ttab", want: true},
		{value: "newline\nbad", want: false},
		{value: "ctrl\x01bad", want: false},
		{value: "del\x7f", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			if got := validHeaderValue(tc.value); got != tc.want {
				t.Fatalf("validHeaderValue(%q)=%v want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestBuildStdioEnv(t *testing.T) {
	t.Parallel()
	env := fakeEnv(map[string]string{
		"PATH":      "/usr/bin",
		"HOME":      "/home/me",
		"MY_TOKEN":  "abc",
		"NOT_FOUND": "", // present but not requested
	})

	out, err := BuildStdioEnv(
		map[string]string{"EXTRA": "literal"},
		[]config.McpServerEnvVar{{Name: "MY_TOKEN"}},
		env,
	)
	if err != nil {
		t.Fatalf("BuildStdioEnv: %v", err)
	}
	// Allowlisted vars present in env are forwarded.
	if out["PATH"] != "/usr/bin" {
		t.Errorf("PATH=%q", out["PATH"])
	}
	// Explicitly requested env_vars are forwarded.
	if out["MY_TOKEN"] != "abc" {
		t.Errorf("MY_TOKEN=%q", out["MY_TOKEN"])
	}
	// Literal extras override / add.
	if out["EXTRA"] != "literal" {
		t.Errorf("EXTRA=%q", out["EXTRA"])
	}
}

func TestBuildStdioEnvLiteralOverrides(t *testing.T) {
	t.Parallel()
	env := fakeEnv(map[string]string{"PATH": "/inherited"})
	out, err := BuildStdioEnv(map[string]string{"PATH": "/override"}, nil, env)
	if err != nil {
		t.Fatalf("BuildStdioEnv: %v", err)
	}
	if out["PATH"] != "/override" {
		t.Fatalf("literal extra must override inherited: PATH=%q", out["PATH"])
	}
}

func TestBuildStdioEnvRejectsRemoteSource(t *testing.T) {
	t.Parallel()
	remote := "remote"
	_, err := BuildStdioEnv(nil, []config.McpServerEnvVar{{Name: "X", Source: &remote}}, fakeEnv(nil))
	if err == nil {
		t.Fatal("expected error for remote env_var source")
	}
}

func TestResolveBearerToken(t *testing.T) {
	t.Parallel()
	varName := "TOKEN"
	tests := []struct {
		name    string
		envVar  *string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "nil env var", envVar: nil, want: ""},
		{name: "present", envVar: &varName, env: map[string]string{"TOKEN": "secret"}, want: "secret"},
		{name: "missing", envVar: &varName, env: map[string]string{}, wantErr: true},
		{name: "empty", envVar: &varName, env: map[string]string{"TOKEN": ""}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveBearerToken("srv", tc.envVar, fakeEnv(tc.env))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestValidateServerName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "github", wantErr: false},
		{name: "my_server-1", wantErr: false},
		{name: "", wantErr: true},
		{name: "has space", wantErr: true},
		{name: "has.dot", wantErr: true},
		{name: "slash/x", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateServerName(tc.name)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateServerName(%q) err=%v wantErr=%v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestTimeoutResolution(t *testing.T) {
	t.Parallel()
	def := config.McpServerConfig{}
	if got := startupTimeoutFor(def); got != DefaultStartupTimeout {
		t.Errorf("default startup=%v", got)
	}
	if got := toolTimeoutFor(def); got != DefaultToolTimeout {
		t.Errorf("default tool=%v", got)
	}

	startup := 2.5
	tool := 0.5
	custom := config.McpServerConfig{StartupTimeoutSec: &startup, ToolTimeoutSec: &tool}
	if got := startupTimeoutFor(custom); got.Seconds() != 2.5 {
		t.Errorf("custom startup=%v", got)
	}
	if got := toolTimeoutFor(custom); got.Seconds() != 0.5 {
		t.Errorf("custom tool=%v", got)
	}
}

func TestApplyHeadersBearerPrecedence(t *testing.T) {
	t.Parallel()
	// A bearer token must not override an existing Authorization header.
	tr := &httpTransport{
		bearerToken:    "from-oauth",
		defaultHeaders: http.Header{"Authorization": []string{"Bearer explicit"}},
	}
	req, _ := http.NewRequest(http.MethodPost, "https://x", nil)
	tr.applyHeaders(req)
	if got := req.Header.Get("Authorization"); got != "Bearer explicit" {
		t.Fatalf("explicit Authorization must win, got %q", got)
	}

	// Without a pre-set header, the bearer token is applied.
	tr2 := &httpTransport{bearerToken: "tok", defaultHeaders: http.Header{}}
	req2, _ := http.NewRequest(http.MethodPost, "https://x", nil)
	tr2.applyHeaders(req2)
	if got := req2.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("bearer token not applied, got %q", got)
	}
}
