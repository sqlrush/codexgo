package execpolicy

import (
	"reflect"
	"strings"
	"testing"
)

// TestNetworkRulesCompileIntoDomainLists ports the Rust
// `network_rules_compile_into_domain_lists` test (execpolicy/tests/basic.rs).
func TestNetworkRulesCompileIntoDomainLists(t *testing.T) {
	policy := parsePolicy(t, "network.rules", `
network_rule(host = "google.com", protocol = "http", decision = "allow")
network_rule(host = "api.github.com", protocol = "https", decision = "allow")
network_rule(host = "blocked.example.com", protocol = "https", decision = "deny")
network_rule(host = "prompt-only.example.com", protocol = "https", decision = "prompt")
`)

	rules := policy.NetworkRules()
	if len(rules) != 4 {
		t.Fatalf("network rules len: got %d, want 4", len(rules))
	}
	if rules[1].Protocol != NetworkRuleProtocolHTTPS {
		t.Fatalf("rule[1] protocol: got %v, want https", rules[1].Protocol)
	}

	allowed, denied := policy.CompiledNetworkDomains()
	wantAllowed := []string{"google.com", "api.github.com"}
	if !reflect.DeepEqual(allowed, wantAllowed) {
		t.Fatalf("allowed: got %v, want %v", allowed, wantAllowed)
	}
	wantDenied := []string{"blocked.example.com"}
	if !reflect.DeepEqual(denied, wantDenied) {
		t.Fatalf("denied: got %v, want %v", denied, wantDenied)
	}
}

// TestNetworkRuleRejectsWildcardHosts ports the Rust
// `network_rule_rejects_wildcard_hosts` test.
func TestNetworkRuleRejectsWildcardHosts(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("network.rules", `network_rule(host="*", protocol="http", decision="allow")`)
	if err == nil {
		t.Fatal("expected wildcard network_rule host to fail")
	}
	if !strings.Contains(err.Error(), "wildcards are not allowed") {
		t.Fatalf("error: got %q, want substring %q", err.Error(), "wildcards are not allowed")
	}
}

// TestNetworkRuleProtocolParse covers ParseNetworkRuleProtocol including the
// Rust aliases (https_connect/http-connect -> https) and the error message.
func TestNetworkRuleProtocolParse(t *testing.T) {
	cases := []struct {
		raw  string
		want NetworkRuleProtocol
	}{
		{"http", NetworkRuleProtocolHTTP},
		{"https", NetworkRuleProtocolHTTPS},
		{"https_connect", NetworkRuleProtocolHTTPS},
		{"http-connect", NetworkRuleProtocolHTTPS},
		{"socks5_tcp", NetworkRuleProtocolSocks5TCP},
		{"socks5_udp", NetworkRuleProtocolSocks5UDP},
	}
	for _, tc := range cases {
		got, err := ParseNetworkRuleProtocol(tc.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("parse %q: got %v, want %v", tc.raw, got, tc.want)
		}
	}

	_, err := ParseNetworkRuleProtocol("ftp")
	if err == nil {
		t.Fatal("expected unknown protocol to fail")
	}
	want := "network_rule protocol must be one of http, https, socks5_tcp, socks5_udp (got ftp)"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error: got %q, want substring %q", err.Error(), want)
	}
}

// TestNetworkRuleProtocolAsPolicyString covers the canonical spelling round-trip.
func TestNetworkRuleProtocolAsPolicyString(t *testing.T) {
	cases := map[NetworkRuleProtocol]string{
		NetworkRuleProtocolHTTP:      "http",
		NetworkRuleProtocolHTTPS:     "https",
		NetworkRuleProtocolSocks5TCP: "socks5_tcp",
		NetworkRuleProtocolSocks5UDP: "socks5_udp",
	}
	for proto, want := range cases {
		if got := proto.AsPolicyString(); got != want {
			t.Fatalf("AsPolicyString(%v): got %q, want %q", proto, got, want)
		}
	}
}

// TestNormalizeNetworkRuleHost ports the host-normalization branches from the
// Rust `normalize_network_rule_host`.
func TestNormalizeNetworkRuleHost(t *testing.T) {
	ok := []struct {
		raw  string
		want string
	}{
		{"Example.COM", "example.com"},
		{"  api.github.com  ", "api.github.com"},
		{"example.com.", "example.com"},
		{"example.com:8080", "example.com"},
		{"[2001:db8::1]", "2001:db8::1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"127.0.0.1:443", "127.0.0.1"},
	}
	for _, tc := range ok {
		got, err := normalizeNetworkRuleHost(tc.raw)
		if err != nil {
			t.Fatalf("normalize %q: %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("normalize %q: got %q, want %q", tc.raw, got, tc.want)
		}
	}

	bad := []struct {
		raw     string
		wantSub string
	}{
		{"", "cannot be empty"},
		{"   ", "cannot be empty"},
		{"https://example.com", "without scheme or path"},
		{"example.com/path", "without scheme or path"},
		{"example.com?x=1", "without scheme or path"},
		{"example.com#frag", "without scheme or path"},
		{"[2001:db8::1", "invalid bracketed IPv6 literal"},
		{"[2001:db8::1]:abc", "unsupported suffix"},
		{"*", "wildcards are not allowed"},
		{"exa*mple.com", "wildcards are not allowed"},
	}
	for _, tc := range bad {
		_, err := normalizeNetworkRuleHost(tc.raw)
		if err == nil {
			t.Fatalf("normalize %q: expected error", tc.raw)
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Fatalf("normalize %q: got %q, want substring %q", tc.raw, err.Error(), tc.wantSub)
		}
	}
}

// TestNetworkRuleJustification verifies justification handling: a present value
// is stored, and an explicit blank value is rejected.
func TestNetworkRuleJustification(t *testing.T) {
	policy := parsePolicy(t, "network.rules", `
network_rule(host = "api.github.com", protocol = "https", decision = "allow", justification = "Allow API access")
`)
	rules := policy.NetworkRules()
	if len(rules) != 1 {
		t.Fatalf("rules len: got %d, want 1", len(rules))
	}
	if !rules[0].HasJustification || rules[0].Justification != "Allow API access" {
		t.Fatalf("justification: got (%v, %q)", rules[0].HasJustification, rules[0].Justification)
	}

	parser := NewPolicyParser()
	err := parser.Parse("network.rules", `network_rule(host="api.github.com", protocol="https", decision="allow", justification="   ")`)
	if err == nil {
		t.Fatal("expected blank justification to fail")
	}
	if !strings.Contains(err.Error(), "justification cannot be empty") {
		t.Fatalf("error: got %q", err.Error())
	}
}

// TestNetworkRuleInvalidDecision verifies that an unrecognized decision string
// is rejected (while "deny" is accepted as an alias for forbidden).
func TestNetworkRuleInvalidDecision(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("network.rules", `network_rule(host="api.github.com", protocol="https", decision="reject")`)
	if err == nil {
		t.Fatal("expected invalid decision to fail")
	}
	if !strings.Contains(err.Error(), "invalid decision: reject") {
		t.Fatalf("error: got %q", err.Error())
	}

	// "deny" maps to forbidden.
	policy := parsePolicy(t, "network.rules", `network_rule(host="b.example.com", protocol="https", decision="deny")`)
	if got := policy.NetworkRules()[0].Decision; got != DecisionForbidden {
		t.Fatalf("deny decision: got %v, want forbidden", got)
	}
}

// TestCompiledNetworkDomainsAllowDenyTransitions verifies that later rules move
// a host between the allow and deny lists, keeping each list deduplicated and
// preserving most-recent order (mirrors Rust upsert_domain/retain semantics).
func TestCompiledNetworkDomainsAllowDenyTransitions(t *testing.T) {
	policy := parsePolicy(t, "network.rules", `
network_rule(host = "a.com", protocol = "https", decision = "allow")
network_rule(host = "b.com", protocol = "https", decision = "deny")
network_rule(host = "a.com", protocol = "https", decision = "deny")
network_rule(host = "b.com", protocol = "https", decision = "allow")
`)
	allowed, denied := policy.CompiledNetworkDomains()
	if !reflect.DeepEqual(allowed, []string{"b.com"}) {
		t.Fatalf("allowed: got %v, want [b.com]", allowed)
	}
	if !reflect.DeepEqual(denied, []string{"a.com"}) {
		t.Fatalf("denied: got %v, want [a.com]", denied)
	}
}

// TestAddNetworkRuleExtendsPolicy verifies the immutable AddNetworkRule helper.
func TestAddNetworkRuleExtendsPolicy(t *testing.T) {
	base := EmptyPolicy()
	just := "manual allow"
	extended, err := base.AddNetworkRule("Example.COM:443", NetworkRuleProtocolHTTPS, DecisionAllow, &just)
	if err != nil {
		t.Fatalf("AddNetworkRule: %v", err)
	}
	if len(base.NetworkRules()) != 0 {
		t.Fatalf("base mutated: got %d rules", len(base.NetworkRules()))
	}
	rules := extended.NetworkRules()
	if len(rules) != 1 {
		t.Fatalf("extended rules len: got %d, want 1", len(rules))
	}
	if rules[0].Host != "example.com" || !rules[0].HasJustification || rules[0].Justification != just {
		t.Fatalf("rule: got %+v", rules[0])
	}

	// Blank justification is rejected.
	blank := "  "
	if _, err := base.AddNetworkRule("example.com", NetworkRuleProtocolHTTP, DecisionAllow, &blank); err == nil {
		t.Fatal("expected blank justification to fail")
	}
}

// TestMergeOverlayCombinesNetworkRules verifies that MergeOverlay appends the
// overlay's network rules after the base's.
func TestMergeOverlayCombinesNetworkRules(t *testing.T) {
	base := parsePolicy(t, "base.rules", `network_rule(host="a.com", protocol="https", decision="allow")`)
	overlay := parsePolicy(t, "overlay.rules", `network_rule(host="b.com", protocol="https", decision="deny")`)
	merged := base.MergeOverlay(overlay)

	rules := merged.NetworkRules()
	if len(rules) != 2 {
		t.Fatalf("merged rules len: got %d, want 2", len(rules))
	}
	if rules[0].Host != "a.com" || rules[1].Host != "b.com" {
		t.Fatalf("merged order: got %q, %q", rules[0].Host, rules[1].Host)
	}
	// Base is unchanged.
	if len(base.NetworkRules()) != 1 {
		t.Fatalf("base mutated: got %d rules", len(base.NetworkRules()))
	}
}

// TestNetworkRuleMissingRequiredArgs verifies host/protocol/decision are
// required arguments.
func TestNetworkRuleMissingRequiredArgs(t *testing.T) {
	parser := NewPolicyParser()
	if err := parser.Parse("network.rules", `network_rule(protocol="https", decision="allow")`); err == nil {
		t.Fatal("expected missing host to fail")
	}
}
