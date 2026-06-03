package networkproxy

import (
	"context"
	"net/netip"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// publicLookup resolves every hostname to a fixed public IP so policy tests do
// not depend on ambient DNS.
func publicLookup(_ context.Context, _ string, _ uint16) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

// failLookup always errors, simulating an unresolvable hostname.
func failLookup(_ context.Context, host string, _ uint16) ([]netip.Addr, error) {
	return nil, context.DeadlineExceeded
}

func stateForPolicy(t *testing.T, network NetworkProxySettings) *NetworkProxyState {
	t.Helper()
	network.Enabled = true
	cfg := NetworkProxyConfig{Network: network}
	cs, err := BuildConfigState(cfg, NetworkProxyConstraints{})
	if err != nil {
		t.Fatalf("BuildConfigState: %v", err)
	}
	state := NewNetworkProxyState(cs)
	state.SetLookupFunc(publicLookup)
	return state
}

func settingsWithDomains(allowed, denied []string) NetworkProxySettings {
	s := DefaultNetworkProxySettings()
	if len(allowed) > 0 {
		s = s.WithAllowedDomains(allowed)
	}
	if len(denied) > 0 {
		s = s.WithDeniedDomains(denied)
	}
	return s
}

func TestHostBlocked(t *testing.T) {
	tests := []struct {
		name       string
		settings   NetworkProxySettings
		host       string
		port       uint16
		allowLocal bool
		lookup     hostLookupFunc
		want       hostBlockDecision
	}{
		{
			name:     "denied wins over allowed",
			settings: settingsWithDomains([]string{"example.com"}, []string{"example.com"}),
			host:     "example.com",
			want:     hostBlockDecision{reason: hostBlockDenied},
		},
		{
			name:     "allowlist match allows",
			settings: settingsWithDomains([]string{"example.com"}, nil),
			host:     "example.com",
			want:     hostBlockDecision{allowed: true},
		},
		{
			name:     "allowlist miss blocks",
			settings: settingsWithDomains([]string{"example.com"}, nil),
			host:     "8.8.8.8",
			want:     hostBlockDecision{reason: hostBlockNotAllowed},
		},
		{
			name:     "subdomain wildcard excludes apex",
			settings: settingsWithDomains([]string{"*.openai.com"}, nil),
			host:     "openai.com",
			want:     hostBlockDecision{reason: hostBlockNotAllowed},
		},
		{
			name:     "subdomain wildcard allows subdomain",
			settings: settingsWithDomains([]string{"*.openai.com"}, nil),
			host:     "api.openai.com",
			want:     hostBlockDecision{allowed: true},
		},
		{
			name:     "global wildcard allows public except denylist",
			settings: settingsWithDomains([]string{"*"}, []string{"evil.example"}),
			host:     "example.com",
			want:     hostBlockDecision{allowed: true},
		},
		{
			name:     "global wildcard denies denylisted",
			settings: settingsWithDomains([]string{"*"}, []string{"evil.example"}),
			host:     "evil.example",
			want:     hostBlockDecision{reason: hostBlockDenied},
		},
		{
			name:     "loopback blocked when local binding disabled",
			settings: settingsWithDomains([]string{"example.com"}, nil),
			host:     "127.0.0.1",
			want:     hostBlockDecision{reason: hostBlockNotAllowedLocal},
		},
		{
			name:     "localhost blocked when local binding disabled",
			settings: settingsWithDomains([]string{"example.com"}, nil),
			host:     "localhost",
			want:     hostBlockDecision{reason: hostBlockNotAllowedLocal},
		},
		{
			name:     "loopback allowed when explicitly allowlisted",
			settings: settingsWithDomains([]string{"localhost"}, nil),
			host:     "localhost",
			want:     hostBlockDecision{allowed: true},
		},
		{
			name:     "private ip literal allowed when explicitly allowlisted",
			settings: settingsWithDomains([]string{"10.0.0.1"}, nil),
			host:     "10.0.0.1",
			want:     hostBlockDecision{allowed: true},
		},
		{
			name:     "scoped ipv6 blocked when not allowlisted",
			settings: settingsWithDomains([]string{"example.com"}, nil),
			host:     "fe80::1%lo0",
			want:     hostBlockDecision{reason: hostBlockNotAllowedLocal},
		},
		{
			name:     "scoped ipv6 allowed when allowlisted",
			settings: settingsWithDomains([]string{"fe80::1"}, nil),
			host:     "fe80::1%lo0",
			want:     hostBlockDecision{allowed: true},
		},
		{
			name:     "loopback blocked when allowlist empty",
			settings: DefaultNetworkProxySettings(),
			host:     "127.0.0.1",
			want:     hostBlockDecision{reason: hostBlockNotAllowedLocal},
		},
		{
			name:     "allowlisted hostname blocked when dns fails",
			settings: settingsWithDomains([]string{"does-not-resolve.invalid"}, nil),
			host:     "does-not-resolve.invalid",
			lookup:   failLookup,
			want:     hostBlockDecision{reason: hostBlockNotAllowedLocal},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := stateForPolicy(t, tt.settings)
			if tt.lookup != nil {
				state.SetLookupFunc(tt.lookup)
			}
			port := tt.port
			if port == 0 {
				port = 80
			}
			got := state.hostBlocked(context.Background(), tt.host, port)
			if got != tt.want {
				t.Errorf("hostBlocked(%q) = %+v, want %+v", tt.host, got, tt.want)
			}
		})
	}
}

func TestEvaluateHostPolicyBaselineDeny(t *testing.T) {
	state := stateForPolicy(t, settingsWithDomains([]string{"example.com"}, []string{"blocked.com"}))
	req := NetworkPolicyRequest{Protocol: ProtocolHTTP, Host: "blocked.com", Port: 80, Method: "GET"}
	decision := evaluateHostPolicy(context.Background(), state, nil, req)
	if decision.Kind != DecisionDeny {
		t.Fatalf("expected deny, got %+v", decision)
	}
	if decision.Reason != reasonDenied {
		t.Errorf("reason = %q, want %q", decision.Reason, reasonDenied)
	}
	if decision.Source != protocol.NetworkDecisionSourceBaselinePolicy {
		t.Errorf("source = %q, want baseline_policy", decision.Source)
	}
	if decision.Decision != protocol.NetworkPolicyDecisionDeny {
		t.Errorf("decision = %q, want deny", decision.Decision)
	}
}

func TestEvaluateHostPolicyDeciderAllowOverride(t *testing.T) {
	state := stateForPolicy(t, DefaultNetworkProxySettings())
	calls := 0
	decider := DeciderFunc(func(_ context.Context, _ NetworkPolicyRequest) NetworkDecision {
		calls++
		return Allow()
	})
	req := NetworkPolicyRequest{Protocol: ProtocolHTTP, Host: "example.com", Port: 80}
	decision := evaluateHostPolicy(context.Background(), state, decider, req)
	if decision.Kind != DecisionAllow {
		t.Fatalf("expected allow override, got %+v", decision)
	}
	if calls != 1 {
		t.Errorf("decider called %d times, want 1", calls)
	}
}

func TestEvaluateHostPolicyDeciderAsk(t *testing.T) {
	state := stateForPolicy(t, DefaultNetworkProxySettings())
	decider := DeciderFunc(func(_ context.Context, _ NetworkPolicyRequest) NetworkDecision {
		return Ask(reasonNotAllowed)
	})
	req := NetworkPolicyRequest{Protocol: ProtocolHTTP, Host: "example.com", Port: 80, Method: "GET"}
	decision := evaluateHostPolicy(context.Background(), state, decider, req)
	if decision.Kind != DecisionDeny {
		t.Fatalf("expected deny(ask), got %+v", decision)
	}
	if decision.Decision != protocol.NetworkPolicyDecisionAsk {
		t.Errorf("decision = %q, want ask", decision.Decision)
	}
	if decision.Source != protocol.NetworkDecisionSourceDecider {
		t.Errorf("source = %q, want decider", decision.Source)
	}
}

func TestEvaluateHostPolicyNotAllowedLocalNotOverridable(t *testing.T) {
	settings := settingsWithDomains([]string{"example.com"}, nil)
	state := stateForPolicy(t, settings)
	deciderCalled := false
	decider := DeciderFunc(func(_ context.Context, _ NetworkPolicyRequest) NetworkDecision {
		deciderCalled = true
		return Allow()
	})
	req := NetworkPolicyRequest{Protocol: ProtocolHTTP, Host: "127.0.0.1", Port: 80, Method: "GET"}
	decision := evaluateHostPolicy(context.Background(), state, decider, req)
	if decision.Kind != DecisionDeny {
		t.Fatalf("expected not_allowed_local deny, got %+v", decision)
	}
	if decision.Reason != reasonNotAllowedLocal {
		t.Errorf("reason = %q, want %q", decision.Reason, reasonNotAllowedLocal)
	}
	if deciderCalled {
		t.Error("decider must not be consulted for not_allowed_local")
	}
}

func TestEvaluateHostPolicyEmitsAuditEvent(t *testing.T) {
	state := stateForPolicy(t, DefaultNetworkProxySettings())
	var events []PolicyDecisionAuditEvent
	state.WithAuditSink(AuditSinkFunc(func(e PolicyDecisionAuditEvent) {
		events = append(events, e)
	}))
	decider := DeciderFunc(func(_ context.Context, _ NetworkPolicyRequest) NetworkDecision {
		return Allow()
	})
	req := NetworkPolicyRequest{Protocol: ProtocolHTTP, Host: "example.com", Port: 80}
	evaluateHostPolicy(context.Background(), state, decider, req)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	e := events[0]
	if e.Scope != auditScopeDomain {
		t.Errorf("scope = %q, want domain", e.Scope)
	}
	if e.Decision != auditDecisionAllow {
		t.Errorf("decision = %q, want allow", e.Decision)
	}
	if e.Source != string(protocol.NetworkDecisionSourceDecider) {
		t.Errorf("source = %q, want decider", e.Source)
	}
	if e.Reason != reasonNotAllowed {
		t.Errorf("reason = %q, want %q", e.Reason, reasonNotAllowed)
	}
	if !e.Override {
		t.Error("override should be true for decider allow")
	}
	if e.Method != auditDefaultMethod {
		t.Errorf("method = %q, want %q", e.Method, auditDefaultMethod)
	}
	if e.ClientAddress != auditDefaultClientAddress {
		t.Errorf("client = %q, want %q", e.ClientAddress, auditDefaultClientAddress)
	}
}

func TestHostResolvesToNonPublicIP(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		lookup hostLookupFunc
		want   bool
	}{
		{"public resolution allowed", "public.example", publicLookup, false},
		{"dns error blocks", "error.example", failLookup, true},
		{
			name: "private resolution blocks",
			host: "local.example",
			lookup: func(_ context.Context, _ string, _ uint16) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostResolvesToNonPublicIP(context.Background(), tt.host, 80, dnsLookupTimeout, tt.lookup)
			if got != tt.want {
				t.Errorf("hostResolvesToNonPublicIP(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestAddDomainMutations(t *testing.T) {
	t.Run("add allowed removes matching deny", func(t *testing.T) {
		state := stateForPolicy(t, settingsWithDomains(nil, []string{"example.com"}))
		if err := state.AddAllowedDomain("ExAmPlE.CoM"); err != nil {
			t.Fatalf("AddAllowedDomain: %v", err)
		}
		allowed, denied := state.CurrentPatterns()
		if len(allowed) != 1 || allowed[0] != "example.com" {
			t.Errorf("allowed = %v, want [example.com]", allowed)
		}
		if len(denied) != 0 {
			t.Errorf("denied = %v, want empty", denied)
		}
	})
	t.Run("add denied removes matching allow", func(t *testing.T) {
		state := stateForPolicy(t, settingsWithDomains([]string{"example.com"}, nil))
		if err := state.AddDeniedDomain("EXAMPLE.COM"); err != nil {
			t.Fatalf("AddDeniedDomain: %v", err)
		}
		allowed, denied := state.CurrentPatterns()
		if len(allowed) != 0 {
			t.Errorf("allowed = %v, want empty", allowed)
		}
		if len(denied) != 1 || denied[0] != "example.com" {
			t.Errorf("denied = %v, want [example.com]", denied)
		}
	})
}
