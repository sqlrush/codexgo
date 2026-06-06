package networkproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// mitmTestState builds a policy state for MITM tests, mirroring
// network_proxy_state_for_policy used in mitm_tests.rs.
func mitmTestState(t *testing.T, network NetworkProxySettings) *NetworkProxyState {
	t.Helper()
	network.Enabled = true
	cs, err := BuildConfigState(NetworkProxyConfig{Network: network}, NetworkProxyConstraints{})
	if err != nil {
		t.Fatalf("BuildConfigState: %v", err)
	}
	state := NewNetworkProxyState(cs)
	state.SetLookupFunc(publicLookup)
	return state
}

func githubWriteHook() MitmHookConfig {
	bearer := "Bearer "
	envVar := "CODEX_GITHUB_TOKEN"
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
				SecretEnvVar: &envVar,
				Prefix:       &bearer,
			}},
		},
	}
}

func mitmReq(t *testing.T, method, target, host string, headers map[string]string) *http.Request {
	t.Helper()
	u, err := url.ParseRequestURI(target)
	if err != nil {
		t.Fatalf("parse target %q: %v", target, err)
	}
	req := &http.Request{Method: method, URL: u, Header: http.Header{}, Host: host}
	req.Header.Set("Host", host)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// TestMitmPolicyBlocksDisallowedMethod mirrors
// mitm_policy_blocks_disallowed_method_and_records_telemetry.
func TestMitmPolicyBlocksDisallowedMethod(t *testing.T) {
	state := mitmTestState(t, DefaultNetworkProxySettings().WithAllowedDomains([]string{"example.com"}))
	h := &mitmHandler{state: state, targetHost: "example.com", targetPort: 443, mode: NetworkModeLimited}

	req := mitmReq(t, http.MethodPost, "/v1/responses?api_key=secret", "example.com", nil)
	outcome := h.evaluateMitmPolicy(context.Background(), req)

	if !outcome.block || outcome.status != http.StatusForbidden {
		t.Fatalf("outcome = %+v, want forbidden block", outcome)
	}
	if got := blockedHeaderValue(outcome.reason); got != "blocked-by-method-policy" {
		t.Errorf("x-proxy-error = %q, want blocked-by-method-policy", got)
	}
	blocked := state.DrainBlocked()
	if len(blocked) != 1 || blocked[0].Reason != reasonMethodNotAllowed {
		t.Fatalf("blocked = %+v, want one method_not_allowed", blocked)
	}
	if blocked[0].Method == nil || *blocked[0].Method != "POST" || blocked[0].Host != "example.com" {
		t.Errorf("blocked[0] = %+v, want POST/example.com", blocked[0])
	}
	if blocked[0].Port == nil || *blocked[0].Port != 443 {
		t.Errorf("blocked port = %v, want 443", blocked[0].Port)
	}
}

// TestMitmPolicyRejectsHostMismatch mirrors mitm_policy_rejects_host_mismatch.
func TestMitmPolicyRejectsHostMismatch(t *testing.T) {
	state := mitmTestState(t, DefaultNetworkProxySettings().WithAllowedDomains([]string{"example.com"}))
	h := &mitmHandler{state: state, targetHost: "example.com", targetPort: 443, mode: NetworkModeFull}

	req := mitmReq(t, http.MethodGet, "/", "evil.example", nil)
	outcome := h.evaluateMitmPolicy(context.Background(), req)

	if !outcome.block || outcome.status != http.StatusBadRequest {
		t.Fatalf("outcome = %+v, want 400 block", outcome)
	}
	if outcome.reason != "" {
		t.Errorf("reason = %q, want plain text_response (no x-proxy-error)", outcome.reason)
	}
	if got := state.BlockedSnapshot(); len(got) != 0 {
		t.Errorf("blocked = %+v, want none recorded for host mismatch", got)
	}
}

// TestMitmPolicyRechecksLocalAfterConnect mirrors
// mitm_policy_rechecks_local_private_target_after_connect.
func TestMitmPolicyRechecksLocalAfterConnect(t *testing.T) {
	network := DefaultNetworkProxySettings().WithAllowedDomains([]string{"example.com"})
	network.AllowLocalBinding = false
	state := mitmTestState(t, network)
	h := &mitmHandler{state: state, targetHost: "10.0.0.1", targetPort: 443, mode: NetworkModeFull}

	req := mitmReq(t, http.MethodGet, "/health?token=secret", "10.0.0.1", nil)
	outcome := h.evaluateMitmPolicy(context.Background(), req)

	if !outcome.block || outcome.status != http.StatusForbidden {
		t.Fatalf("outcome = %+v, want forbidden block", outcome)
	}
	blocked := state.DrainBlocked()
	if len(blocked) != 1 || blocked[0].Reason != reasonNotAllowedLocal {
		t.Fatalf("blocked = %+v, want one not_allowed_local", blocked)
	}
	if blocked[0].Host != "10.0.0.1" || blocked[0].Port == nil || *blocked[0].Port != 443 {
		t.Errorf("blocked[0] = %+v, want 10.0.0.1:443", blocked[0])
	}
}

// TestMitmPolicyAllowsMatchingHookedWriteFullMode mirrors
// mitm_policy_allows_matching_hooked_write_in_full_mode.
func TestMitmPolicyAllowsMatchingHookedWriteFullMode(t *testing.T) {
	secretFile := writeTempSecret(t, "ghp-secret\n")
	hook := githubWriteHook()
	hook.Actions.InjectRequestHeaders[0].SecretEnvVar = nil
	hook.Actions.InjectRequestHeaders[0].SecretFile = &secretFile

	network := DefaultNetworkProxySettings().WithAllowedDomains([]string{"api.github.com"})
	network.Mitm = true
	network.Mode = NetworkModeFull
	network.MitmHooks = []MitmHookConfig{hook}
	state := mitmTestState(t, network)
	h := &mitmHandler{state: state, targetHost: "api.github.com", targetPort: 443, mode: NetworkModeFull}

	req := mitmReq(t, http.MethodPost, "/repos/openai/codex/issues", "api.github.com", nil)
	outcome := h.evaluateMitmPolicy(context.Background(), req)

	if outcome.block {
		t.Fatalf("matching hook should bypass method clamp, got block %+v", outcome)
	}
	if outcome.hookActions == nil {
		t.Fatal("expected hook actions to be returned")
	}
	if got := state.BlockedSnapshot(); len(got) != 0 {
		t.Errorf("blocked = %+v, want none", got)
	}
}

// TestMitmPolicyBlocksMatchingHookedWriteLimitedMode mirrors
// mitm_policy_blocks_matching_hooked_write_in_limited_mode.
func TestMitmPolicyBlocksMatchingHookedWriteLimitedMode(t *testing.T) {
	hook := githubWriteHook()
	hook.Actions.InjectRequestHeaders = nil

	network := DefaultNetworkProxySettings().WithAllowedDomains([]string{"api.github.com"})
	network.Mitm = true
	network.Mode = NetworkModeLimited
	network.MitmHooks = []MitmHookConfig{hook}
	state := mitmTestState(t, network)
	h := &mitmHandler{state: state, targetHost: "api.github.com", targetPort: 443, mode: NetworkModeLimited}

	req := mitmReq(t, http.MethodPost, "/repos/openai/codex/issues", "api.github.com", nil)
	outcome := h.evaluateMitmPolicy(context.Background(), req)

	if !outcome.block || outcome.status != http.StatusForbidden {
		t.Fatalf("outcome = %+v, want forbidden", outcome)
	}
	if got := blockedHeaderValue(outcome.reason); got != "blocked-by-method-policy" {
		t.Errorf("x-proxy-error = %q, want blocked-by-method-policy", got)
	}
	blocked := state.DrainBlocked()
	if len(blocked) != 1 || blocked[0].Reason != reasonMethodNotAllowed {
		t.Fatalf("blocked = %+v, want one method_not_allowed", blocked)
	}
}

// TestMitmPolicyBlocksHookMissForHookedHost mirrors
// mitm_policy_blocks_hook_miss_for_hooked_host_and_records_telemetry_in_full_mode.
func TestMitmPolicyBlocksHookMissForHookedHost(t *testing.T) {
	secretFile := writeTempSecret(t, "ghp-secret\n")
	hook := githubWriteHook()
	hook.Actions.InjectRequestHeaders[0].SecretEnvVar = nil
	hook.Actions.InjectRequestHeaders[0].SecretFile = &secretFile

	network := DefaultNetworkProxySettings().WithAllowedDomains([]string{"api.github.com"})
	network.Mitm = true
	network.Mode = NetworkModeFull
	network.MitmHooks = []MitmHookConfig{hook}
	state := mitmTestState(t, network)
	h := &mitmHandler{state: state, targetHost: "api.github.com", targetPort: 443, mode: NetworkModeFull}

	// GET does not match the POST/PUT hook -> hooked-host-no-match -> denied.
	req := mitmReq(t, http.MethodGet, "/repos/openai/codex/issues?token=secret", "api.github.com",
		map[string]string{"authorization": "Bearer user-supplied"})
	outcome := h.evaluateMitmPolicy(context.Background(), req)

	if !outcome.block || outcome.status != http.StatusForbidden {
		t.Fatalf("outcome = %+v, want forbidden", outcome)
	}
	if got := blockedHeaderValue(outcome.reason); got != "blocked-by-mitm-hook" {
		t.Errorf("x-proxy-error = %q, want blocked-by-mitm-hook", got)
	}
	blocked := state.DrainBlocked()
	if len(blocked) != 1 || blocked[0].Reason != reasonMitmHookDenied {
		t.Fatalf("blocked = %+v, want one mitm_hook_denied", blocked)
	}
	if blocked[0].Method == nil || *blocked[0].Method != "GET" {
		t.Errorf("blocked method = %v, want GET", blocked[0].Method)
	}
}

// TestApplyMitmHookActions mirrors apply_mitm_hook_actions_replaces_authorization_header.
func TestApplyMitmHookActions(t *testing.T) {
	headers := http.Header{}
	headers.Add("authorization", "Bearer user-supplied")
	headers.Add("x-request-id", "req_123")

	actions := &mitmHookActions{
		stripRequestHeaders: []string{"Authorization"},
		injectRequestHeaders: []resolvedInjectedHeader{{
			name:  "Authorization",
			value: "Bearer secret-token",
		}},
	}
	applyMitmHookActions(headers, actions)

	if got := headers.Get("authorization"); got != "Bearer secret-token" {
		t.Errorf("authorization = %q, want Bearer secret-token", got)
	}
	if got := headers.Get("x-request-id"); got != "req_123" {
		t.Errorf("x-request-id = %q, want req_123", got)
	}
}

func TestAuthorityHeaderValue(t *testing.T) {
	cases := []struct {
		host string
		port uint16
		want string
	}{
		{"example.com", 443, "example.com"},
		{"example.com", 8443, "example.com:8443"},
		{"::1", 443, "[::1]"},
		{"::1", 8443, "[::1]:8443"},
	}
	for _, tc := range cases {
		if got := authorityHeaderValue(tc.host, tc.port); got != tc.want {
			t.Errorf("authorityHeaderValue(%q,%d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

// --- End-to-end TLS termination test ---

// TestMitmConnectEndToEnd drives a real HTTPS client through a CONNECT MITM
// tunnel: the client trusts the proxy CA, an allowed GET reaches the upstream,
// and the upstream sees the rewritten Host header. This exercises the full
// data path: CONNECT -> TLS terminate with minted leaf -> policy -> forward.
//
// We use 127.0.0.1 as the policy host throughout so the proxy's upstream dial
// reaches the loopback test server without depending on ambient DNS.
func TestMitmConnectEndToEnd(t *testing.T) {
	ca, _, _ := newTestCA(t)

	// Upstream HTTPS server presenting a cert minted for "127.0.0.1" so the
	// proxy's forwarding transport (which trusts our CA) can reach it.
	upstreamTLS, err := ca.TLSConfigForHost("127.0.0.1")
	if err != nil {
		t.Fatalf("upstream tls: %v", err)
	}
	var gotHost string
	upstream := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHost = r.Host
			_, _ = io.WriteString(w, "ok-from-upstream:"+r.Method)
		}),
		TLSConfig: upstreamTLS,
	}
	upLn, err := tls.Listen("tcp", "127.0.0.1:0", upstreamTLS)
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	go func() { _ = upstream.Serve(upLn) }()
	defer upstream.Close()
	upPort := uint16(upLn.Addr().(*net.TCPAddr).Port)

	// Allow 127.0.0.1 + local binding so the proxy may dial loopback. MITM is on
	// and a hook on this host forces CONNECT to be MITM-terminated even in full
	// mode.
	network := DefaultNetworkProxySettings().WithAllowedDomains([]string{"127.0.0.1"})
	network.Mitm = true
	network.Mode = NetworkModeFull
	network.AllowLocalBinding = true
	network.MitmHooks = []MitmHookConfig{loopbackGetHook()}
	state := mitmTestState(t, network)

	upstreamRoots := x509.NewCertPool()
	upstreamRoots.AppendCertsFromPEM(ca.CertificatePEM())

	srv := newHTTPProxyServer(state, nil)
	srv.mitmCAFactory = func() (*ManagedMitmCA, error) { return ca, nil }
	srv.mitmUpstreamTLS = &tls.Config{RootCAs: upstreamRoots}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	defer proxyLn.Close()
	httpSrv := &http.Server{Handler: srv}
	go func() { _ = httpSrv.Serve(proxyLn) }()
	defer httpSrv.Close()
	proxyAddr := proxyLn.Addr().String()

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca.CertificatePEM()) {
		t.Fatal("append CA")
	}
	proxyURL := &url.URL{Scheme: "http", Host: proxyAddr}
	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{RootCAs: roots},
		},
	}

	// Allowed GET: matches the loopback hook, succeeds, upstream sees rewritten Host.
	resp, err := client.Get(targetURL("127.0.0.1", upPort, "/data"))
	if err != nil {
		t.Fatalf("allowed GET through MITM: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ok-from-upstream:GET") {
		t.Fatalf("status=%d body=%q, want 200 ok-from-upstream:GET", resp.StatusCode, body)
	}
	wantAuthority := authorityHeaderValue("127.0.0.1", upPort)
	if gotHost != wantAuthority {
		t.Errorf("upstream Host = %q, want %q", gotHost, wantAuthority)
	}
}

// TestMitmConnectDeniedMethodLimitedMode drives a real client through the MITM
// tunnel and asserts a disallowed POST gets the Rust-shaped denial.
func TestMitmConnectDeniedMethodLimitedMode(t *testing.T) {
	ca, _, _ := newTestCA(t)
	upstreamTLS, _ := ca.TLSConfigForHost("127.0.0.1")
	upstream := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should-not-reach")
	}), TLSConfig: upstreamTLS}
	upLn, err := tls.Listen("tcp", "127.0.0.1:0", upstreamTLS)
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	go func() { _ = upstream.Serve(upLn) }()
	defer upstream.Close()
	upPort := uint16(upLn.Addr().(*net.TCPAddr).Port)

	// Limited mode forces CONNECT to be MITM-terminated for inner-method clamping.
	network := DefaultNetworkProxySettings().WithAllowedDomains([]string{"127.0.0.1"})
	network.Mitm = true
	network.Mode = NetworkModeLimited
	network.AllowLocalBinding = true
	state := mitmTestState(t, network)

	srv := newHTTPProxyServer(state, nil)
	srv.mitmCAFactory = func() (*ManagedMitmCA, error) { return ca, nil }

	proxyLn, _ := net.Listen("tcp", "127.0.0.1:0")
	defer proxyLn.Close()
	httpSrv := &http.Server{Handler: srv}
	go func() { _ = httpSrv.Serve(proxyLn) }()
	defer httpSrv.Close()

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca.CertificatePEM())
	proxyURL := &url.URL{Scheme: "http", Host: proxyLn.Addr().String()}
	client := &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}},
	}

	resp, err := client.Post(targetURL("127.0.0.1", upPort, "/write"), "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("POST through MITM: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if got := resp.Header.Get("x-proxy-error"); got != "blocked-by-method-policy" {
		t.Errorf("x-proxy-error = %q, want blocked-by-method-policy", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != blockedMessage(reasonMethodNotAllowed) {
		t.Errorf("body = %q, want %q", body, blockedMessage(reasonMethodNotAllowed))
	}
	if blocked := state.DrainBlocked(); len(blocked) != 1 || blocked[0].Reason != reasonMethodNotAllowed {
		t.Errorf("blocked = %+v, want one method_not_allowed", blocked)
	}
}

func writeTempSecret(t *testing.T, contents string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

// targetURL builds an https URL for host:port/path.
func targetURL(host string, port uint16, path string) string {
	return (&url.URL{Scheme: "https", Host: net.JoinHostPort(host, strconvUint(port)), Path: path}).String()
}

func strconvUint(p uint16) string {
	return strconv.Itoa(int(p))
}

// loopbackGetHook matches any GET on 127.0.0.1 so CONNECT is MITM-terminated in
// full mode (a hooked host forces termination).
func loopbackGetHook() MitmHookConfig {
	return MitmHookConfig{
		Host: "127.0.0.1",
		Matcher: MitmHookMatchConfig{
			Methods:      []string{"GET"},
			PathPrefixes: []string{"/"},
		},
	}
}
