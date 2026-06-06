package networkproxy

// This file is a faithful Go port of codex-rs/network-proxy/src/mitm.rs's policy
// + forwarding data path. After a CONNECT is terminated with a minted leaf cert
// (see mitmca.go), each decrypted inner HTTP request flows through here:
//
//   - evaluateMitmPolicy mirrors mitm.rs::evaluate_mitm_policy (host-mismatch
//     guard, DNS-rebinding recheck, hook evaluation, limited-mode method clamp).
//   - applyMitmHookActions mirrors mitm.rs::apply_mitm_hook_actions.
//   - mitmHandler.forward mirrors mitm.rs::forward_request (rewrite authority +
//     Host, strip hop-by-hop, stream bodies both ways).
//
// Rust's MITM_INSPECT_BODIES is false in 0.136.0, so the body-inspection logging
// path is intentionally not reproduced (it is a no-op pass-through there).

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// mitmHandler serves decrypted inner HTTPS requests for a single CONNECT target.
// It mirrors mitm.rs::MitmRequestContext + MitmPolicyContext fused into one
// http.Handler (Go terminates TLS with crypto/tls and dispatches through an
// http.Server, so the policy context travels with the handler rather than with
// per-request extensions).
type mitmHandler struct {
	state      *NetworkProxyState
	targetHost string
	targetPort uint16
	mode       NetworkMode
	clientAddr string
	transport  http.RoundTripper
}

// mitmPolicyOutcome is the result of evaluateMitmPolicy: either allow (with
// optional hook actions) or block (with a fully-formed denial to write back).
// Mirrors mitm.rs::MitmPolicyDecision.
type mitmPolicyOutcome struct {
	block       bool
	hookActions *mitmHookActions
	// block fields:
	status int
	reason string // "" when this is a plain text_response (no x-proxy-error)
	body   string
}

// ServeHTTP enforces policy on the decrypted request and forwards it upstream.
// Mirrors mitm.rs::handle_mitm_request -> forward_request.
func (h *mitmHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	outcome := h.evaluateMitmPolicy(r.Context(), r)
	if outcome.block {
		h.writeBlock(w, outcome)
		return
	}
	if err := h.forward(w, r, outcome.hookActions); err != nil {
		// Mirrors handle_mitm_request's BAD_GATEWAY fallback.
		writeTextResponse(w, http.StatusBadGateway, "mitm upstream error")
	}
}

// evaluateMitmPolicy mirrors mitm.rs::evaluate_mitm_policy exactly, including the
// telemetry recorded for each blocking branch and the ordering of checks.
func (h *mitmHandler) evaluateMitmPolicy(ctx context.Context, r *http.Request) mitmPolicyOutcome {
	if r.Method == http.MethodConnect {
		return mitmPolicyOutcome{
			block:  true,
			status: http.StatusMethodNotAllowed,
			body:   "CONNECT not supported inside MITM",
		}
	}

	method := r.Method

	// Host-mismatch guard: the inner request's Host (or authority) must match the
	// CONNECT target. Defends against a client smuggling a different host inside
	// the tunnel.
	if requestHost := extractRequestHost(r); requestHost != "" {
		normalized := NormalizeHost(requestHost)
		if normalized != "" && normalized != h.targetHost {
			return mitmPolicyOutcome{
				block:  true,
				status: http.StatusBadRequest,
				body:   "host mismatch",
			}
		}
	}

	// DNS-rebinding recheck: CONNECT already enforced allow/deny + decider policy,
	// but re-check local/private resolution here in case DNS changed between
	// CONNECT and the inner request.
	if decision := h.state.hostBlocked(ctx, h.targetHost, h.targetPort); !decision.allowed &&
		decision.reason == hostBlockNotAllowedLocal {
		reason := hostBlockNotAllowedLocal.asStr()
		h.recordBlocked(ctx, reason, method)
		return mitmPolicyOutcome{block: true, status: http.StatusForbidden, reason: reason, body: blockedMessage(reason)}
	}

	// MITM hook evaluation. A hooked host with no matching hook is denied; a host
	// with no hooks falls through to the method clamp.
	var hookActions *mitmHookActions
	switch eval := h.state.evaluateMitmHookRequest(h.targetHost, r); eval.kind {
	case hookMatched:
		actions := eval.actions
		hookActions = &actions
	case hookedHostNoMatch:
		h.recordBlocked(ctx, reasonMitmHookDenied, method)
		return mitmPolicyOutcome{block: true, status: http.StatusForbidden, reason: reasonMitmHookDenied, body: blockedMessage(reasonMitmHookDenied)}
	case hookNoHooksForHost:
		hookActions = nil
	}

	// Limited-mode method clamp: a matching hook bypasses this (the hook explicitly
	// authorizes the write), but an unhooked write in limited mode is denied.
	if !h.mode.AllowsMethod(method) {
		h.recordBlocked(ctx, reasonMethodNotAllowed, method)
		return mitmPolicyOutcome{block: true, status: http.StatusForbidden, reason: reasonMethodNotAllowed, body: blockedMessage(reasonMethodNotAllowed)}
	}

	return mitmPolicyOutcome{hookActions: hookActions}
}

// recordBlocked records a blocked inner-HTTPS request. Mirrors the
// record_blocked calls in evaluate_mitm_policy (protocol "https", mode set).
// evaluate_mitm_policy only records to the blocked buffer here; it does not emit
// a separate audit decision event, so neither do we.
func (h *mitmHandler) recordBlocked(ctx context.Context, reason, method string) {
	mode := h.mode
	port := h.targetPort
	h.state.RecordBlocked(ctx, newBlockedRequest(BlockedRequestArgs{
		Host:     h.targetHost,
		Reason:   reason,
		Client:   optStr(h.clientAddr),
		Method:   optStr(method),
		Mode:     &mode,
		Protocol: "https",
		Port:     &port,
	}))
}

// writeBlock writes a denial response. When reason is set it carries the
// x-proxy-error header (blocked_text_response); otherwise it is a plain
// text_response (host-mismatch / CONNECT-in-MITM / method-not-allowed-in-MITM).
func (h *mitmHandler) writeBlock(w http.ResponseWriter, outcome mitmPolicyOutcome) {
	if outcome.reason != "" {
		w.Header().Set("content-type", "text/plain")
		w.Header().Set("x-proxy-error", blockedHeaderValue(outcome.reason))
		w.WriteHeader(outcome.status)
		_, _ = w.Write([]byte(outcome.body))
		return
	}
	writeTextResponse(w, outcome.status, outcome.body)
}

// forward rewrites the request for the upstream HTTPS target and proxies it,
// streaming the request and response bodies. Mirrors mitm.rs::forward_request.
func (h *mitmHandler) forward(w http.ResponseWriter, r *http.Request, hookActions *mitmHookActions) error {
	authority := authorityHeaderValue(h.targetHost, h.targetPort)
	path := pathAndQuery(r.URL)

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, "https://"+authority+path, r.Body)
	if err != nil {
		return fmt.Errorf("build upstream request: %w", err)
	}
	copyHeaders(outReq.Header, r.Header)
	removeHopByHopHeaders(outReq.Header)
	applyMitmHookActions(outReq.Header, hookActions)
	outReq.Host = authority
	outReq.Header.Set("Host", authority)
	outReq.ContentLength = r.ContentLength

	resp, err := h.transport.RoundTrip(outReq)
	if err != nil {
		return fmt.Errorf("mitm upstream round trip: %w", err)
	}
	defer resp.Body.Close()

	respHeader := w.Header()
	copyHeaders(respHeader, resp.Header)
	removeHopByHopHeaders(respHeader)
	w.WriteHeader(resp.StatusCode)
	if err := streamBody(w, resp.Body); err != nil {
		return fmt.Errorf("mitm stream response body: %w", err)
	}
	return nil
}

// applyMitmHookActions strips then injects the configured headers. Mirrors
// mitm.rs::apply_mitm_hook_actions (strip-all-then-insert ordering).
func applyMitmHookActions(headers http.Header, actions *mitmHookActions) {
	if actions == nil {
		return
	}
	for _, name := range actions.stripRequestHeaders {
		headers.Del(name)
	}
	for _, injected := range actions.injectRequestHeaders {
		headers.Set(injected.name, injected.value)
	}
}

// extractRequestHost returns the inner request's Host header, falling back to the
// URI authority. Mirrors mitm.rs::extract_request_host.
func extractRequestHost(r *http.Request) string {
	if host := r.Header.Get("Host"); host != "" {
		return host
	}
	if r.Host != "" {
		return r.Host
	}
	if r.URL != nil {
		return r.URL.Host
	}
	return ""
}

// authorityHeaderValue formats the Host header / URI authority for an upstream
// HTTPS request. Mirrors mitm.rs::authority_header_value, including the IPv6
// bracketing and the default-443 elision.
func authorityHeaderValue(host string, port uint16) string {
	if strings.Contains(host, ":") {
		if port == 443 {
			return "[" + host + "]"
		}
		return "[" + host + "]:" + strconv.Itoa(int(port))
	}
	if port == 443 {
		return host
	}
	return host + ":" + strconv.Itoa(int(port))
}

// pathAndQuery returns the request-target path (and query), defaulting to "/".
// Mirrors mitm.rs::path_and_query.
func pathAndQuery(u *url.URL) string {
	if u == nil {
		return "/"
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		return p + "?" + u.RawQuery
	}
	return p
}

// copyHeaders copies all header values from src into dst.
func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// streamBody copies the upstream response body to the client, flushing after each
// chunk so streaming responses are not buffered (mirrors rama's streaming body
// forwarding).
func streamBody(w http.ResponseWriter, body io.Reader) error {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// newMitmTransport builds the upstream RoundTripper for MITM-forwarded HTTPS
// requests. It enforces the local-target policy on dial (SSRF defense, matching
// the CONNECT path), honors a custom CA bundle (CODEXGO_CA_CERTIFICATE /
// SSL_CERT_FILE, mirroring the Rust UpstreamClient's TLS config), and optionally
// honors an upstream HTTP(S) proxy.
func newMitmTransport(s *httpProxyServer, allowUpstream bool) http.RoundTripper {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split address %q: %w", address, err)
			}
			p, _ := strconv.ParseUint(portStr, 10, 16)
			return s.dialChecked(ctx, host, uint16(p))
		},
		ForceAttemptHTTP2: true,
		TLSClientConfig:   s.upstreamTLSConfig(),
	}
	if allowUpstream {
		if proxy := proxyForHTTP(); proxy != nil {
			tr.Proxy = http.ProxyURL(proxy)
		}
	}
	return tr
}
