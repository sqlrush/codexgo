package networkproxy

import (
	"context"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/execpolicy"
)

// compileExecPolicy parses an execpolicy file fixture and returns its compiled
// allow/deny domain lists, failing the test on a parse error.
func compileExecPolicy(t *testing.T, identifier, src string) (allowed, denied []string) {
	t.Helper()
	parser := execpolicy.NewPolicyParser()
	if err := parser.Parse(identifier, src); err != nil {
		t.Fatalf("parse exec policy %s: %v", identifier, err)
	}
	return parser.Build().CompiledNetworkDomains()
}

// TestApplyExecPolicyNetworkRulesPermitsAllowedHost proves that an
// `network_rule(decision = "allow")` declared in an execpolicy file ends up
// permitting that host through the proxy policy, while a denied host is blocked.
func TestApplyExecPolicyNetworkRulesPermitsAllowedHost(t *testing.T) {
	allowed, denied := compileExecPolicy(t, "network.policy", `
network_rule(host = "api.github.com", protocol = "https", decision = "allow")
network_rule(host = "blocked.example.com", protocol = "https", decision = "deny")
`)

	cfg := NetworkProxyConfig{Network: DefaultNetworkProxySettings()}
	cfg.Network.Enabled = true
	cfg = ApplyExecPolicyNetworkRules(cfg, allowed, denied)

	if got := cfg.Network.AllowedDomains(); !reflect.DeepEqual(got, []string{"api.github.com"}) {
		t.Fatalf("allowed domains: got %v, want [api.github.com]", got)
	}
	if got := cfg.Network.DeniedDomains(); !reflect.DeepEqual(got, []string{"blocked.example.com"}) {
		t.Fatalf("denied domains: got %v, want [blocked.example.com]", got)
	}

	cs, err := BuildConfigState(cfg, NetworkProxyConstraints{})
	if err != nil {
		t.Fatalf("BuildConfigState: %v", err)
	}
	state := NewNetworkProxyState(cs)
	state.SetLookupFunc(publicLookup)

	if got := state.hostBlocked(context.Background(), "api.github.com", 443); got != (hostBlockDecision{allowed: true}) {
		t.Fatalf("allowed host: got %+v, want {allowed:true}", got)
	}
	if got := state.hostBlocked(context.Background(), "blocked.example.com", 443); got != (hostBlockDecision{reason: hostBlockNotAllowed}) {
		// blocked.example.com is on the denylist but the allowlist does not
		// include it, so the allowlist-miss path fires first.
		if got != (hostBlockDecision{reason: hostBlockDenied}) {
			t.Fatalf("denied host: got %+v, want denied or not-allowed", got)
		}
	}
}

// TestApplyExecPolicyNetworkRulesOverridesConfigEntry verifies precedence:
// exec-policy rules are upserted after config-sourced entries, so an exec-policy
// deny for a host that the config allows flips that host to denied.
func TestApplyExecPolicyNetworkRulesOverridesConfigEntry(t *testing.T) {
	cfg := NetworkProxyConfig{Network: DefaultNetworkProxySettings().WithAllowedDomains([]string{"example.com"})}

	allowed, denied := compileExecPolicy(t, "override.policy", `
network_rule(host = "example.com", protocol = "https", decision = "deny")
`)
	cfg = ApplyExecPolicyNetworkRules(cfg, allowed, denied)

	if got := cfg.Network.AllowedDomains(); got != nil {
		t.Fatalf("allowed domains: got %v, want nil (exec-policy deny removed config allow)", got)
	}
	if got := cfg.Network.DeniedDomains(); !reflect.DeepEqual(got, []string{"example.com"}) {
		t.Fatalf("denied domains: got %v, want [example.com]", got)
	}
}

// TestApplyExecPolicyNetworkRulesDeduplicatesNormalizedHosts verifies that a
// repeated normalized host only upserts once (the Rust HashSet guard).
func TestApplyExecPolicyNetworkRulesDeduplicatesNormalizedHosts(t *testing.T) {
	cfg := NetworkProxyConfig{Network: DefaultNetworkProxySettings()}

	// Two distinct verbatim hosts that normalize to the same value both pass
	// the verbatim-string dedup guard; the upsert then collapses them on the
	// normalized host, so the last one wins (mirroring Rust).
	cfg = ApplyExecPolicyNetworkRules(cfg, []string{"Example.COM", "example.com"}, nil)
	if got := cfg.Network.AllowedDomains(); !reflect.DeepEqual(got, []string{"example.com"}) {
		t.Fatalf("allowed domains: got %v, want [example.com]", got)
	}

	// A truly identical verbatim host is skipped by the dedup guard.
	cfg2 := NetworkProxyConfig{Network: DefaultNetworkProxySettings()}
	cfg2 = ApplyExecPolicyNetworkRules(cfg2, []string{"example.com", "example.com"}, nil)
	if got := cfg2.Network.AllowedDomains(); !reflect.DeepEqual(got, []string{"example.com"}) {
		t.Fatalf("allowed domains (identical dup): got %v, want [example.com]", got)
	}
}
