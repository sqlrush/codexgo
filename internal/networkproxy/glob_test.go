package networkproxy

import "testing"

func TestCompileDenylistGlobset(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		matches  map[string]bool
	}{
		{
			name:     "normalizes trailing dots",
			patterns: []string{"Example.COM."},
			matches:  map[string]bool{"example.com": true, "api.example.com": false},
		},
		{
			name:     "subdomain wildcard excludes apex",
			patterns: []string{"*.Example.COM."},
			matches:  map[string]bool{"api.example.com": true, "example.com": false},
		},
		{
			name:     "mid-label wildcards",
			patterns: []string{"region*.v2.argotunnel.com"},
			matches: map[string]bool{
				"region1.v2.argotunnel.com":     true,
				"region.v2.argotunnel.com":      true,
				"xregion1.v2.argotunnel.com":    false,
				"foo.region1.v2.argotunnel.com": false,
			},
		},
		{
			name:     "double wildcard matches apex and subdomains",
			patterns: []string{"**.Example.COM."},
			matches:  map[string]bool{"example.com": true, "api.example.com": true},
		},
		{
			name:     "bracketed ipv6 literal",
			patterns: []string{"[::1]"},
			matches:  map[string]bool{"::1": true},
		},
		{
			name:     "scoped ipv6 literal preserved",
			patterns: []string{"[fe80::1%25lo0]"},
			matches:  map[string]bool{"fe80::1%lo0": true, "fe80::1%lo1": false, "fe80::1": false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := compileDenylistGlobset(tt.patterns)
			if err != nil {
				t.Fatalf("compileDenylistGlobset: %v", err)
			}
			for host, want := range tt.matches {
				if got := set.match(host); got != want {
					t.Errorf("match(%q) = %v, want %v", host, got, want)
				}
			}
		})
	}
}

func TestCompileDenylistRejectsGlobalWildcard(t *testing.T) {
	if _, err := compileDenylistGlobset([]string{"*"}); err == nil {
		t.Fatal("expected global wildcard to be rejected in denylist")
	}
	if _, err := compileDenylistGlobset([]string{"[*]"}); err == nil {
		t.Fatal("expected bracketed global wildcard to be rejected in denylist")
	}
}

func TestCompileAllowlistAllowsGlobalWildcard(t *testing.T) {
	set, err := compileAllowlistGlobset([]string{"*"})
	if err != nil {
		t.Fatalf("compileAllowlistGlobset: %v", err)
	}
	if !set.match("anything.example") {
		t.Error("global wildcard allowlist should match any host")
	}
}

func TestGlobMatchLiteralSeparator(t *testing.T) {
	// With literalSeparator, '*' does not cross '/'.
	if globMatch("/repos/*/codex/issues*", "/repos/openai/private/codex/issues", true) {
		t.Error("wildcard should not cross path segment boundary")
	}
	if !globMatch("/repos/*/codex/issues*", "/repos/openai/codex/issues", true) {
		t.Error("wildcard should match single path segment")
	}
}
