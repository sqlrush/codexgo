package responsesproxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// proxyHandler implements [http.Handler] for the proxy. It mirrors the request
// loop of codex's `run_main` + `forward_request`.
type proxyHandler struct {
	authHeader   string
	config       *forwardConfig
	dumper       *exchangeDumper
	client       *http.Client
	httpShutdown bool
	exit         Exiter
}

// ServeHTTP routes shutdown requests, forwards `POST /v1/responses`, and rejects
// everything else with 403.
func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.httpShutdown && r.Method == http.MethodGet && r.URL.Path == "/shutdown" && r.URL.RawQuery == "" {
		w.WriteHeader(http.StatusOK)
		if h.exit != nil {
			h.exit(0)
		}
		return
	}

	if err := h.forwardRequest(w, r); err != nil {
		fmt.Fprintf(os.Stderr, "forwarding error: %v\n", err)
	}
}

// forwardRequest forwards an allowed request to the upstream and relays the
// response. It mirrors codex's `forward_request`.
func (h *proxyHandler) forwardRequest(w http.ResponseWriter, r *http.Request) error {
	// Only allow POST /v1/responses exactly, with no query string.
	urlPath := requestURLPath(r)
	allow := r.Method == http.MethodPost && urlPath == "/v1/responses"
	if !allow {
		w.WriteHeader(http.StatusForbidden)
		return nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("reading request body: %w", err)
	}

	var pendingDump *exchangeDump
	if h.dumper != nil {
		dumpHeaders := dumpHeadersFromHeader(r.Header)
		pendingDump, err = h.dumper.dumpRequest(r.Method, urlPath, dumpHeaders, body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "responses-api-proxy failed to dump request: %v\n", err)
			pendingDump = nil
		}
	}

	upstreamReq, err := http.NewRequest(http.MethodPost, h.config.upstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building upstream request: %w", err)
	}
	applyUpstreamHeaders(upstreamReq, r.Header, h.authHeader, h.config.hostHeader)

	upstreamResp, err := h.client.Do(upstreamReq)
	if err != nil {
		return fmt.Errorf("forwarding request to upstream: %w", err)
	}
	defer upstreamResp.Body.Close()

	relayResponseHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)

	var responseBody io.Reader = upstreamResp.Body
	if pendingDump != nil {
		dumpHeaders := dumpHeadersFromResponse(upstreamResp.Header)
		responseBody = pendingDump.teeResponseBody(uint16(upstreamResp.StatusCode), dumpHeaders, upstreamResp.Body)
	}

	if _, err := io.Copy(w, responseBody); err != nil {
		return fmt.Errorf("relaying upstream response: %w", err)
	}
	return nil
}

// requestURLPath returns the request path including any raw query string, so the
// exact-match check against "/v1/responses" rejects requests with a query, as in
// codex (which compares tiny_http's url, i.e. path+query).
func requestURLPath(r *http.Request) string {
	if r.URL.RawQuery != "" {
		return r.URL.Path + "?" + r.URL.RawQuery
	}
	return r.URL.Path
}

// applyUpstreamHeaders copies all incoming headers except Authorization and Host
// (compared case-insensitively), then injects the Bearer Authorization header and
// the upstream Host header. It mirrors the header construction in codex's
// `forward_request`.
func applyUpstreamHeaders(req *http.Request, incoming http.Header, authHeader, hostHeader string) {
	for name, values := range incoming {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "host" {
			continue
		}
		for _, v := range values {
			req.Header.Add(lower, v)
		}
	}
	req.Header.Set("Authorization", authHeader)
	req.Host = hostHeader
	req.Header.Set("Host", hostHeader)
}

// relayResponseHeaders copies upstream response headers to the client, skipping
// hop-by-hop headers that the Go server manages. It mirrors the response header
// relay in codex's `forward_request`.
func relayResponseHeaders(dst, src http.Header) {
	for name, values := range src {
		if _, skip := hopByHopResponseHeaders[strings.ToLower(name)]; skip {
			continue
		}
		for _, v := range values {
			dst.Add(name, v)
		}
	}
}

// dumpHeadersFromResponse builds redacted dump headers from response headers.
// reqwest lowercases header names, so the response dump uses lowercase names to
// match codex's behavior.
func dumpHeadersFromResponse(h http.Header) []headerDump {
	out := make([]headerDump, 0, len(h))
	for name, values := range h {
		for _, v := range values {
			out = append(out, newHeaderDump(strings.ToLower(name), v))
		}
	}
	return out
}

// dumpHeadersFromHeader builds dump headers preserving header-name casing.
func dumpHeadersFromHeader(h http.Header) []headerDump {
	out := make([]headerDump, 0, len(h))
	for name, values := range h {
		for _, v := range values {
			out = append(out, newHeaderDump(name, v))
		}
	}
	return out
}
