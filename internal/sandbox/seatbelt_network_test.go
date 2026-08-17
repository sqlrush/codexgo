package sandbox

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// TestDynamicNetworkPolicyRoutesThroughProxyPorts mirrors
// create_seatbelt_args_routes_network_through_proxy_ports.
func TestDynamicNetworkPolicyRoutesThroughProxyPorts(t *testing.T) {
	t.Parallel()
	policy := dynamicNetworkPolicyForNetwork(
		protocol.NetworkSandboxPolicyRestricted,
		false,
		proxyPolicyInputs{ports: []uint16{43128, 48081}, hasProxyConfig: true},
	)

	wants := []string{
		`(allow network-outbound (remote ip "localhost:43128"))`,
		`(allow network-outbound (remote ip "localhost:48081"))`,
	}
	for _, w := range wants {
		if !strings.Contains(policy, w) {
			t.Errorf("policy missing %q:\n%s", w, policy)
		}
	}
	unwanted := []string{
		"\n(allow network-outbound)\n",
		`(allow network-bind (local ip "*:*"))`,
		`(allow network-inbound (local ip "localhost:*"))`,
		`(allow network-outbound (remote ip "*:53"))`,
	}
	for _, u := range unwanted {
		if strings.Contains(policy, u) {
			t.Errorf("policy unexpectedly contains %q:\n%s", u, policy)
		}
	}
}

// TestDynamicNetworkPolicyAllowsLocalBinding mirrors
// create_seatbelt_args_allows_local_binding_when_explicitly_enabled.
func TestDynamicNetworkPolicyAllowsLocalBinding(t *testing.T) {
	t.Parallel()
	policy := dynamicNetworkPolicyForNetwork(
		protocol.NetworkSandboxPolicyRestricted,
		false,
		proxyPolicyInputs{ports: []uint16{43128}, hasProxyConfig: true, allowLocalBinding: true},
	)

	wants := []string{
		`(allow network-bind (local ip "*:*"))`,
		`(allow network-inbound (local ip "localhost:*"))`,
		`(allow network-outbound (remote ip "localhost:*"))`,
		`(allow network-outbound (remote ip "*:53"))`,
		`(allow network-outbound (remote ip "localhost:43128"))`,
	}
	for _, w := range wants {
		if !strings.Contains(policy, w) {
			t.Errorf("policy missing %q:\n%s", w, policy)
		}
	}
}

// TestDynamicNetworkPolicyFailsClosedWithProxyConfigNoPorts mirrors
// dynamic_network_policy_preserves_restricted_policy_when_proxy_config_without_ports.
func TestDynamicNetworkPolicyFailsClosedWithProxyConfigNoPorts(t *testing.T) {
	t.Parallel()
	policy := dynamicNetworkPolicyForNetwork(
		protocol.NetworkSandboxPolicyRestricted,
		false,
		proxyPolicyInputs{hasProxyConfig: true},
	)
	if policy != "" {
		t.Fatalf("expected empty (fail-closed) policy, got:\n%s", policy)
	}
}

// TestDynamicNetworkPolicyManagedNetworkFailsClosed mirrors
// dynamic_network_policy_preserves_restricted_policy_for_managed_network_without_proxy_config.
func TestDynamicNetworkPolicyManagedNetworkFailsClosed(t *testing.T) {
	t.Parallel()
	policy := dynamicNetworkPolicyForNetwork(
		protocol.NetworkSandboxPolicyRestricted,
		true,
		proxyPolicyInputs{},
	)
	if policy != "" {
		t.Fatalf("expected empty (fail-closed) policy, got:\n%s", policy)
	}
}

// TestDynamicNetworkPolicyAllowlistsUnixSockets mirrors
// create_seatbelt_args_allowlists_unix_socket_paths.
func TestDynamicNetworkPolicyAllowlistsUnixSockets(t *testing.T) {
	t.Parallel()
	policy := dynamicNetworkPolicyForNetwork(
		protocol.NetworkSandboxPolicyRestricted,
		false,
		proxyPolicyInputs{
			ports:          []uint16{43128},
			hasProxyConfig: true,
			udsKind:        unixDomainSocketRestricted,
			udsAllowed:     []string{"/tmp/example.sock"},
		},
	)

	wants := []string{
		"(allow system-socket (socket-domain AF_UNIX))",
		`(allow network-bind (local unix-socket (subpath (param "UNIX_SOCKET_PATH_0"))))`,
		`(allow network-outbound (remote unix-socket (subpath (param "UNIX_SOCKET_PATH_0"))))`,
	}
	for _, w := range wants {
		if !strings.Contains(policy, w) {
			t.Errorf("policy missing %q:\n%s", w, policy)
		}
	}
	if strings.Contains(policy, "(allow network* (subpath") {
		t.Errorf("policy should not use generic subpath unix-socket rules:\n%s", policy)
	}
}

// TestDynamicNetworkPolicyAllowsAllUnixSockets mirrors
// create_seatbelt_args_allows_all_unix_sockets_when_enabled.
func TestDynamicNetworkPolicyAllowsAllUnixSockets(t *testing.T) {
	t.Parallel()
	policy := dynamicNetworkPolicyForNetwork(
		protocol.NetworkSandboxPolicyRestricted,
		false,
		proxyPolicyInputs{udsKind: unixDomainSocketAllowAll},
	)

	wants := []string{
		"(allow system-socket (socket-domain AF_UNIX))",
		"(allow network-bind (local unix-socket))",
		"(allow network-outbound (remote unix-socket))",
	}
	for _, w := range wants {
		if !strings.Contains(policy, w) {
			t.Errorf("policy missing %q:\n%s", w, policy)
		}
	}
}

// TestDynamicNetworkPolicyFullNetworkUnixSockets mirrors
// create_seatbelt_args_preserves_full_network_with_explicit_unix_socket_paths.
func TestDynamicNetworkPolicyFullNetworkUnixSockets(t *testing.T) {
	t.Parallel()
	policy := dynamicNetworkPolicyForNetwork(
		protocol.NetworkSandboxPolicyEnabled,
		false,
		proxyPolicyInputs{udsKind: unixDomainSocketRestricted, udsAllowed: []string{"/tmp/example.sock"}},
	)

	wants := []string{
		"(allow network-outbound)\n",
		"(allow network-inbound)\n",
		"(allow system-socket (socket-domain AF_UNIX))",
	}
	for _, w := range wants {
		if !strings.Contains(policy, w) {
			t.Errorf("policy missing %q:\n%s", w, policy)
		}
	}
}

// TestUnixSocketPolicyNonEmptyOutputIsNewlineTerminated mirrors
// unix_socket_policy_non_empty_output_is_newline_terminated.
func TestUnixSocketPolicyNonEmptyOutputIsNewlineTerminated(t *testing.T) {
	t.Parallel()
	proxy := proxyPolicyInputs{udsKind: unixDomainSocketRestricted, udsAllowed: []string{"/tmp/example.sock"}}
	got := proxy.unixSocketPolicy()
	if got == "" {
		t.Fatal("expected non-empty unix socket policy")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected newline-terminated output, got %q", got)
	}
}

// TestUnixSocketDirParamsUseStableParamNames mirrors
// unix_socket_dir_params_use_stable_param_names: dedup + sorted-by-path indexing.
func TestUnixSocketDirParamsUseStableParamNames(t *testing.T) {
	t.Parallel()
	proxy := proxyPolicyInputs{
		udsKind:    unixDomainSocketRestricted,
		udsAllowed: []string{"/tmp/b.sock", "/tmp/a.sock", "/tmp/b.sock"},
	}
	params := proxy.unixSocketDirParams()
	want := []dirParam{
		{key: "UNIX_SOCKET_PATH_0", path: "/tmp/a.sock"},
		{key: "UNIX_SOCKET_PATH_1", path: "/tmp/b.sock"},
	}
	if len(params) != len(want) {
		t.Fatalf("params = %#v, want %#v", params, want)
	}
	for i := range want {
		if params[i] != want[i] {
			t.Fatalf("param[%d] = %#v, want %#v", i, params[i], want[i])
		}
	}
}

// TestProxyLoopbackPortsFromEnv exercises proxy_loopback_ports_from_env across
// scheme defaults, explicit ports, non-loopback rejection and dedup/sorting.
func TestProxyLoopbackPortsFromEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want []uint16
	}{
		{"explicit ports sorted+deduped", map[string]string{
			"HTTP_PROXY":  "http://127.0.0.1:48081",
			"HTTPS_PROXY": "http://localhost:43128",
			"ALL_PROXY":   "http://127.0.0.1:48081",
		}, []uint16{43128, 48081}},
		{"https default port", map[string]string{"HTTPS_PROXY": "https://localhost"}, []uint16{443}},
		{"socks default port", map[string]string{"ALL_PROXY": "socks5h://127.0.0.1"}, []uint16{1080}},
		{"http default port", map[string]string{"HTTP_PROXY": "127.0.0.1"}, []uint16{80}},
		{"non-loopback rejected", map[string]string{"HTTP_PROXY": "http://example.com:3128"}, nil},
		{"empty value ignored", map[string]string{"HTTP_PROXY": "   "}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proxyLoopbackPortsFromEnv(tt.env)
			if len(got) != len(tt.want) {
				t.Fatalf("ports = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("ports = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
