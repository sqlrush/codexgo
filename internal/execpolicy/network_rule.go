package execpolicy

import (
	"fmt"
	"strings"
)

// NetworkRuleProtocol enumerates the transport protocols a [NetworkRule] may
// target. It mirrors the Rust `NetworkRuleProtocol` enum.
type NetworkRuleProtocol int

const (
	// NetworkRuleProtocolHTTP corresponds to Rust `NetworkRuleProtocol::Http`.
	NetworkRuleProtocolHTTP NetworkRuleProtocol = iota
	// NetworkRuleProtocolHTTPS corresponds to Rust `NetworkRuleProtocol::Https`.
	NetworkRuleProtocolHTTPS
	// NetworkRuleProtocolSocks5TCP corresponds to Rust
	// `NetworkRuleProtocol::Socks5Tcp`.
	NetworkRuleProtocolSocks5TCP
	// NetworkRuleProtocolSocks5UDP corresponds to Rust
	// `NetworkRuleProtocol::Socks5Udp`.
	NetworkRuleProtocolSocks5UDP
)

// ParseNetworkRuleProtocol parses a policy `protocol` string, mirroring Rust's
// `NetworkRuleProtocol::parse`. The "https_connect"/"http-connect" aliases map
// to HTTPS, matching the Rust accepted spellings.
func ParseNetworkRuleProtocol(raw string) (NetworkRuleProtocol, error) {
	switch raw {
	case "http":
		return NetworkRuleProtocolHTTP, nil
	case "https", "https_connect", "http-connect":
		return NetworkRuleProtocolHTTPS, nil
	case "socks5_tcp":
		return NetworkRuleProtocolSocks5TCP, nil
	case "socks5_udp":
		return NetworkRuleProtocolSocks5UDP, nil
	default:
		return 0, &Error{
			Kind: ErrInvalidRule,
			Message: fmt.Sprintf(
				"network_rule protocol must be one of http, https, socks5_tcp, socks5_udp (got %s)",
				raw,
			),
		}
	}
}

// AsPolicyString returns the canonical policy spelling of the protocol,
// mirroring Rust's `NetworkRuleProtocol::as_policy_string`.
func (p NetworkRuleProtocol) AsPolicyString() string {
	switch p {
	case NetworkRuleProtocolHTTP:
		return "http"
	case NetworkRuleProtocolHTTPS:
		return "https"
	case NetworkRuleProtocolSocks5TCP:
		return "socks5_tcp"
	case NetworkRuleProtocolSocks5UDP:
		return "socks5_udp"
	default:
		return fmt.Sprintf("NetworkRuleProtocol(%d)", int(p))
	}
}

// NetworkRule registers a network-policy decision for a specific host and
// protocol. It mirrors the Rust `NetworkRule` struct. Values are immutable.
type NetworkRule struct {
	Host     string
	Protocol NetworkRuleProtocol
	Decision Decision
	// Justification is the optional rationale. HasJustification distinguishes an
	// absent justification (Rust `None`) from an empty one; empty justifications
	// are rejected at parse time.
	Justification    string
	HasJustification bool
}

// parseNetworkRuleDecision parses a network_rule `decision` string, mirroring
// Rust's `parse_network_rule_decision`. It accepts the "deny" alias (mapped to
// Forbidden) in addition to the standard decision spellings.
func parseNetworkRuleDecision(raw string) (Decision, error) {
	if raw == "deny" {
		return DecisionForbidden, nil
	}
	return ParseDecision(raw)
}

// normalizeNetworkRuleHost validates and canonicalizes a network_rule host,
// mirroring Rust's `normalize_network_rule_host`. It strips an optional port
// suffix (including bracketed IPv6 literals), trims trailing dots, lowercases,
// and rejects empty hosts, schemes/paths, wildcards, and whitespace.
func normalizeNetworkRuleHost(raw string) (string, error) {
	host := strings.TrimSpace(raw)
	if host == "" {
		return "", &Error{Kind: ErrInvalidRule, Message: "network_rule host cannot be empty"}
	}
	if strings.Contains(host, "://") || strings.Contains(host, "/") ||
		strings.Contains(host, "?") || strings.Contains(host, "#") {
		return "", &Error{
			Kind:    ErrInvalidRule,
			Message: "network_rule host must be a hostname or IP literal (without scheme or path)",
		}
	}

	if stripped, ok := strings.CutPrefix(host, "["); ok {
		inside, rest, found := strings.Cut(stripped, "]")
		if !found {
			return "", &Error{
				Kind:    ErrInvalidRule,
				Message: "network_rule host has an invalid bracketed IPv6 literal",
			}
		}
		portOK := false
		if port, hasColon := strings.CutPrefix(rest, ":"); hasColon {
			portOK = port != "" && allASCIIDigits(port)
		}
		if rest != "" && !portOK {
			return "", &Error{
				Kind:    ErrInvalidRule,
				Message: fmt.Sprintf("network_rule host contains an unsupported suffix: %s", raw),
			}
		}
		host = inside
	} else if strings.Count(host, ":") == 1 {
		if candidate, port, found := strings.Cut(host, ":"); found &&
			candidate != "" && port != "" && allASCIIDigits(port) {
			host = candidate
		}
	}

	normalized := strings.ToLower(strings.TrimSpace(strings.TrimRight(host, ".")))
	if normalized == "" {
		return "", &Error{Kind: ErrInvalidRule, Message: "network_rule host cannot be empty"}
	}
	if strings.Contains(normalized, "*") {
		return "", &Error{
			Kind:    ErrInvalidRule,
			Message: "network_rule host must be a specific host; wildcards are not allowed",
		}
	}
	if containsWhitespace(normalized) {
		return "", &Error{
			Kind:    ErrInvalidRule,
			Message: "network_rule host cannot contain whitespace",
		}
	}

	return normalized, nil
}

// allASCIIDigits reports whether s is non-empty and consists solely of ASCII
// digits, matching Rust's `port.chars().all(|c| c.is_ascii_digit())`.
func allASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// containsWhitespace reports whether s contains any whitespace rune, matching
// Rust's `chars().any(char::is_whitespace)`.
func containsWhitespace(s string) bool {
	for _, r := range s {
		if isWhitespace(r) {
			return true
		}
	}
	return false
}
