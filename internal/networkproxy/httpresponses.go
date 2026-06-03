package networkproxy

import (
	"encoding/json"
	"net/http"
)

// writeTextResponse writes a plain-text response.
func writeTextResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("content-type", "text/plain")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// writeJSONBlocked writes the JSON 403 blocked response with the x-proxy-error
// header, matching codex's `json_blocked`.
func writeJSONBlocked(w http.ResponseWriter, host, reason string, details *policyDecisionDetails) {
	body := blockedResponseBody{Status: "blocked", Host: host, Reason: reason}
	if details != nil {
		msg := blockedMessage(reason)
		decision := string(details.decision)
		source := string(details.source)
		proto := details.protocol.PolicyProtocol()
		port := details.port
		body.Decision = &decision
		body.Source = &source
		body.Protocol = &proto
		body.Port = &port
		body.Message = &msg
	}
	data, err := json.Marshal(body)
	if err != nil {
		data = []byte("{}")
	}
	w.Header().Set("content-type", "application/json")
	w.Header().Set("x-proxy-error", blockedHeaderValue(reason))
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write(data)
}

// writeBlockedText writes a plain-text 403 with the x-proxy-error header,
// matching codex's `blocked_text_response_with_policy`.
func writeBlockedText(w http.ResponseWriter, reason string) {
	w.Header().Set("content-type", "text/plain")
	w.Header().Set("x-proxy-error", blockedHeaderValue(reason))
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(blockedMessage(reason)))
}
