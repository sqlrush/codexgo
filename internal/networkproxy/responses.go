package networkproxy

import "github.com/sqlrush/codexgo/internal/protocol"

// policyDecisionDetails carries the policy context for a blocked response.
type policyDecisionDetails struct {
	decision protocol.NetworkPolicyDecision
	reason   string
	source   protocol.NetworkDecisionSource
	protocol NetworkProtocol
	host     string
	port     uint16
}

// blockedHeaderValue maps a reason code to the `x-proxy-error` header value,
// matching codex exactly.
func blockedHeaderValue(reason string) string {
	switch reason {
	case reasonNotAllowed, reasonNotAllowedLocal:
		return "blocked-by-allowlist"
	case reasonDenied:
		return "blocked-by-denylist"
	case reasonMethodNotAllowed:
		return "blocked-by-method-policy"
	case reasonMitmHookDenied:
		return "blocked-by-mitm-hook"
	case reasonMitmRequired:
		return "blocked-by-mitm-required"
	default:
		return "blocked-by-policy"
	}
}

// blockedMessage maps a reason code to a human-readable message, matching codex.
func blockedMessage(reason string) string {
	switch reason {
	case reasonNotAllowed:
		return "Domain not in allowlist."
	case reasonNotAllowedLocal:
		return "Sandbox policy blocks local/private network addresses."
	case reasonDenied:
		return "Domain denied by the sandbox policy."
	case reasonMethodNotAllowed:
		return "Method not allowed in limited mode."
	case reasonMitmHookDenied:
		return "HTTPS request denied by MITM hook policy."
	case reasonMitmRequired:
		return "MITM required for limited HTTPS."
	case reasonProxyDisabled:
		return "network proxy is disabled"
	default:
		return "Request blocked by network policy."
	}
}

// blockedResponseBody is the JSON shape returned for blocked HTTP requests.
type blockedResponseBody struct {
	Status   string  `json:"status"`
	Host     string  `json:"host"`
	Reason   string  `json:"reason"`
	Decision *string `json:"decision,omitempty"`
	Source   *string `json:"source,omitempty"`
	Protocol *string `json:"protocol,omitempty"`
	Port     *uint16 `json:"port,omitempty"`
	Message  *string `json:"message,omitempty"`
}
