package networkproxy

import (
	"net/http"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func baseMitmConfig() NetworkProxyConfig {
	s := DefaultNetworkProxySettings()
	s.Mitm = true
	s.Mode = NetworkModeLimited
	return NetworkProxyConfig{Network: s}
}

func githubHook() MitmHookConfig {
	env := "CODEX_GITHUB_TOKEN"
	prefix := "Bearer "
	return MitmHookConfig{
		Host: "api.github.com",
		Matcher: MitmHookMatchConfig{
			Methods:      []string{"POST", "PUT"},
			PathPrefixes: []string{"/repos/openai/"},
		},
		Actions: MitmHookActionsConfig{
			StripRequestHeaders: []string{"authorization"},
			InjectRequestHeaders: []InjectedHeaderConfig{{
				Name:         "authorization",
				SecretEnvVar: &env,
				Prefix:       &prefix,
			}},
		},
	}
}

func TestValidateRequiresMitmForHooks(t *testing.T) {
	cfg := baseMitmConfig()
	cfg.Network.Mitm = false
	cfg.Network.MitmHooks = []MitmHookConfig{githubHook()}
	if err := validateMitmHookConfig(cfg); err == nil {
		t.Fatal("expected error when hooks configured without mitm")
	}
}

func TestValidateRejectsBodyMatchers(t *testing.T) {
	cfg := baseMitmConfig()
	hook := githubHook()
	hook.Matcher.Body = &MitmHookBodyConfig{Raw: map[string]any{"repository": "openai/codex"}}
	cfg.Network.MitmHooks = []MitmHookConfig{hook}
	if err := validateMitmHookConfig(cfg); err == nil {
		t.Fatal("expected body matchers to be rejected")
	}
}

func TestValidateRejectsRelativeSecretFile(t *testing.T) {
	cfg := baseMitmConfig()
	hook := githubHook()
	rel := "token.txt"
	hook.Actions.InjectRequestHeaders[0].SecretEnvVar = nil
	hook.Actions.InjectRequestHeaders[0].SecretFile = &rel
	cfg.Network.MitmHooks = []MitmHookConfig{hook}
	if err := validateMitmHookConfig(cfg); err == nil {
		t.Fatal("expected relative secret file to be rejected")
	}
}

func TestValidateRejectsDualSecretSources(t *testing.T) {
	cfg := baseMitmConfig()
	hook := githubHook()
	file := "/tmp/github-token"
	hook.Actions.InjectRequestHeaders[0].SecretFile = &file
	cfg.Network.MitmHooks = []MitmHookConfig{hook}
	if err := validateMitmHookConfig(cfg); err == nil {
		t.Fatal("expected dual secret sources to be rejected")
	}
}

func TestCompileResolvesEnvBackedInjectedHeaders(t *testing.T) {
	cfg := baseMitmConfig()
	cfg.Network.MitmHooks = []MitmHookConfig{githubHook()}
	hooks, err := compileMitmHooksWithResolvers(cfg,
		func(name string) (string, bool) {
			return "ghp-secret", name == "CODEX_GITHUB_TOKEN"
		},
		func(abspath.AbsolutePathBuf) (string, error) { return "", nil },
	)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	compiled := hooks["api.github.com"]
	if len(compiled) != 1 {
		t.Fatalf("got %d compiled hooks, want 1", len(compiled))
	}
	header := compiled[0].actions.injectRequestHeaders[0]
	if header.sourceKind != secretSourceEnvVar || header.source != "CODEX_GITHUB_TOKEN" {
		t.Errorf("source = %v/%q, want env CODEX_GITHUB_TOKEN", header.sourceKind, header.source)
	}
	if header.value != "Bearer ghp-secret" {
		t.Errorf("value = %q, want Bearer ghp-secret", header.value)
	}
}

func compileTestHooks(t *testing.T, cfg NetworkProxyConfig) mitmHooksByHost {
	t.Helper()
	hooks, err := compileMitmHooksWithResolvers(cfg,
		func(string) (string, bool) { return "abc", true },
		func(abspath.AbsolutePathBuf) (string, error) { return "", nil },
	)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return hooks
}

func TestEvaluateReturnsFirstMatchingHook(t *testing.T) {
	cfg := baseMitmConfig()
	first := githubHook()
	second := githubHook()
	tokenPrefix := "Token "
	second.Actions.InjectRequestHeaders[0].Prefix = &tokenPrefix
	cfg.Network.MitmHooks = []MitmHookConfig{first, second}
	hooks := compileTestHooks(t, cfg)

	req, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/codex/issues", nil)
	req.Header.Set("x-trace", "1")
	eval := evaluateMitmHooks(hooks, "api.github.com", req)
	if eval.kind != hookMatched {
		t.Fatalf("expected match, got %v", eval.kind)
	}
	if eval.actions.injectRequestHeaders[0].value != "Bearer abc" {
		t.Errorf("value = %q, want Bearer abc (first hook)", eval.actions.injectRequestHeaders[0].value)
	}
}

func TestEvaluateQueryAndHeaderConstraints(t *testing.T) {
	cfg := baseMitmConfig()
	hook := githubHook()
	hook.Matcher.Query = map[string][]string{"state": {"open", "triage"}}
	hook.Matcher.Headers = map[string][]string{"x-github-api-version": {"2022-11-28"}}
	cfg.Network.MitmHooks = []MitmHookConfig{hook}
	hooks := compileTestHooks(t, cfg)

	req, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/codex/issues?state=open&per_page=10", nil)
	req.Header.Set("x-github-api-version", "2022-11-28")
	if eval := evaluateMitmHooks(hooks, "api.github.com", req); eval.kind != hookMatched {
		t.Errorf("expected match, got %v", eval.kind)
	}

	reqMiss, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/codex/issues?state=closed", nil)
	if eval := evaluateMitmHooks(hooks, "api.github.com", reqMiss); eval.kind != hookedHostNoMatch {
		t.Errorf("expected no match for closed state, got %v", eval.kind)
	}
}

func TestEvaluatePathWildcardDoesNotCrossSegments(t *testing.T) {
	cfg := baseMitmConfig()
	hook := githubHook()
	hook.Matcher.PathPrefixes = []string{"pattern:/repos/*/codex/issues*"}
	cfg.Network.MitmHooks = []MitmHookConfig{hook}
	hooks := compileTestHooks(t, cfg)

	nested, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/private/codex/issues", nil)
	if eval := evaluateMitmHooks(hooks, "api.github.com", nested); eval.kind != hookedHostNoMatch {
		t.Errorf("nested path should not match, got %v", eval.kind)
	}
	direct, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/codex/issues", nil)
	if eval := evaluateMitmHooks(hooks, "api.github.com", direct); eval.kind != hookMatched {
		t.Errorf("direct path should match, got %v", eval.kind)
	}
}

func TestEvaluateNoHooksForUnconfiguredHost(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/codex/issues", nil)
	if eval := evaluateMitmHooks(mitmHooksByHost{}, "api.github.com", req); eval.kind != hookNoHooksForHost {
		t.Errorf("expected no hooks for host, got %v", eval.kind)
	}
}

func TestLiteralValuesWithReservedPrefixes(t *testing.T) {
	cfg := baseMitmConfig()
	hook := githubHook()
	hook.Matcher.Headers = map[string][]string{"x-github-api-version": {"literal:pattern:*"}}
	cfg.Network.MitmHooks = []MitmHookConfig{hook}
	hooks := compileTestHooks(t, cfg)

	exact, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/codex/issues", nil)
	exact.Header.Set("x-github-api-version", "pattern:*")
	if eval := evaluateMitmHooks(hooks, "api.github.com", exact); eval.kind != hookMatched {
		t.Errorf("literal value should match exactly, got %v", eval.kind)
	}
	other, _ := http.NewRequest("POST", "http://api.github.com/repos/openai/codex/issues", nil)
	other.Header.Set("x-github-api-version", "pattern:preview")
	if eval := evaluateMitmHooks(hooks, "api.github.com", other); eval.kind != hookedHostNoMatch {
		t.Errorf("non-literal value should not match, got %v", eval.kind)
	}
}
