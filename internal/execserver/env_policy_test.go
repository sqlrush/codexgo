package execserver

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestWildMatch(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"*key*", "openai_api_key", true},
		{"*key*", "keychain", true},
		{"*key*", "monkeys", true},
		{"*key*", "value", false},
		{"path", "path", true},
		{"path", "paths", false},
		{"p?th", "path", true},
		{"p?th", "pth", false},
		{"*", "anything", true},
		{"*", "", true},
		{"", "", true},
		{"", "x", false},
		{"a*b", "ab", true},
		{"a*b", "axxxb", true},
		{"a*b", "axxx", false},
	}
	for _, tt := range tests {
		if got := wildMatch(tt.pattern, tt.text); got != tt.want {
			t.Errorf("wildMatch(%q, %q) = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
	}
}

func TestEnvPatternCaseInsensitive(t *testing.T) {
	p := newCaseInsensitivePattern("*SECRET*")
	for _, name := range []string{"my_secret", "MY_SECRET", "AwsSecretKey"} {
		if !p.matches(name) {
			t.Errorf("pattern *SECRET* should match %q", name)
		}
	}
	if p.matches("PATH") {
		t.Errorf("pattern *SECRET* should not match PATH")
	}
}

func TestChildEnvDefaultsToExactEnv(t *testing.T) {
	params := ExecParams{
		Argv: []string{"true"},
		Env:  map[string]string{"ONLY_THIS": "1"},
	}
	got := childEnv(params, map[string]string{"INHERITED": "x"})
	want := map[string]string{"ONLY_THIS": "1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("childEnv got %v want %v", got, want)
	}
}

func TestChildEnvAppliesPolicyThenOverlay(t *testing.T) {
	params := ExecParams{
		Argv: []string{"true"},
		Env: map[string]string{
			"OVERLAY":    "overlay",
			"POLICY_SET": "overlay-wins",
		},
		EnvPolicy: &ExecEnvPolicy{
			Inherit:               protocol.ShellEnvironmentPolicyInheritNone,
			IgnoreDefaultExcludes: true,
			Set:                   map[string]string{"POLICY_SET": "policy"},
		},
	}
	got := childEnv(params, map[string]string{"INHERITED": "x", "OPENAI_API_KEY": "secret"})
	want := map[string]string{
		"OVERLAY":    "overlay",
		"POLICY_SET": "overlay-wins",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("childEnv got %v want %v", got, want)
	}
}

func TestPopulateEnvDefaultExcludes(t *testing.T) {
	vars := map[string]string{
		"PATH":           "/usr/bin",
		"OPENAI_API_KEY": "k",
		"MY_SECRET":      "s",
		"AUTH_TOKEN":     "t",
		"SAFE":           "ok",
	}
	policy := ExecEnvPolicy{
		Inherit:               protocol.ShellEnvironmentPolicyInheritAll,
		IgnoreDefaultExcludes: false,
	}
	got := populateEnv(vars, policy, nil)
	want := map[string]string{"PATH": "/usr/bin", "SAFE": "ok"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("populateEnv got %v want %v", got, want)
	}
}

func TestPopulateEnvCoreInheritCaseInsensitive(t *testing.T) {
	vars := map[string]string{
		"path":           "/usr/bin",
		"home":           "/home/codex",
		"TmpDir":         "/tmp/custom",
		"OPENAI_API_KEY": "secret",
	}
	policy := ExecEnvPolicy{
		Inherit:               protocol.ShellEnvironmentPolicyInheritCore,
		IgnoreDefaultExcludes: true,
	}
	got := populateEnv(vars, policy, nil)
	want := map[string]string{
		"path":   "/usr/bin",
		"home":   "/home/codex",
		"TmpDir": "/tmp/custom",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("populateEnv got %v want %v", got, want)
	}
}

func TestPopulateEnvIncludeOnlyAndThreadID(t *testing.T) {
	vars := map[string]string{"PATH": "/usr/bin", "HOME": "/home", "EXTRA": "x"}
	tid := "thread-123"
	policy := ExecEnvPolicy{
		Inherit:               protocol.ShellEnvironmentPolicyInheritAll,
		IgnoreDefaultExcludes: true,
		IncludeOnly:           []string{"PATH"},
	}
	got := populateEnv(vars, policy, &tid)
	want := map[string]string{"PATH": "/usr/bin", codexThreadIDEnvVar: tid}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("populateEnv got %v want %v", got, want)
	}
}
