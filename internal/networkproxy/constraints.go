package networkproxy

import (
	"fmt"
	"strings"
)

// NetworkProxyConstraints expresses managed-config bounds that user policy may
// not exceed. Nil fields impose no constraint. Faithful port of Rust's
// `NetworkProxyConstraints`.
type NetworkProxyConstraints struct {
	Enabled                          *bool
	Mode                             *NetworkMode
	AllowUpstreamProxy               *bool
	DangerouslyAllowNonLoopbackProxy *bool
	DangerouslyAllowAllUnixSockets   *bool
	AllowedDomains                   *[]string
	AllowlistExpansionEnabled        *bool
	DeniedDomains                    *[]string
	DenylistExpansionEnabled         *bool
	AllowUnixSockets                 *[]string
	AllowLocalBinding                *bool
}

// NetworkProxyConstraintError describes a policy value that violates a managed
// constraint.
type NetworkProxyConstraintError struct {
	FieldName string
	Candidate string
	Allowed   string
}

func (e NetworkProxyConstraintError) Error() string {
	return fmt.Sprintf("invalid value for %s: %s (allowed %s)", e.FieldName, e.Candidate, e.Allowed)
}

func invalidValue(fieldName, candidate, allowed string) error {
	return NetworkProxyConstraintError{FieldName: fieldName, Candidate: candidate, Allowed: allowed}
}

func networkModeRank(mode NetworkMode) int {
	if mode.normalized() == NetworkModeLimited {
		return 0
	}
	return 1
}

// ValidatePolicyAgainstConstraints validates a config against managed
// constraints. Faithful port of Rust's `validate_policy_against_constraints`.
func ValidatePolicyAgainstConstraints(config NetworkProxyConfig, constraints NetworkProxyConstraints) error {
	enabled := config.Network.Enabled
	configAllowedDomains := config.Network.AllowedDomains()
	configDeniedDomains := config.Network.DeniedDomains()
	deniedDomainOverrides := lowercaseSet(configDeniedDomains)
	configAllowUnixSockets := config.Network.AllowUnixSockets()

	if err := validateMitmHookConfig(config); err != nil {
		return invalidValue("network.mitm_hooks", err.Error(), "valid MITM hook configuration")
	}
	if err := validateNonGlobalWildcardDomainPatterns("network.denied_domains", configDeniedDomains); err != nil {
		return err
	}

	if constraints.Enabled != nil {
		if enabled && !*constraints.Enabled {
			return invalidValue("network.enabled", "true", "false (disabled by managed config)")
		}
	}

	if constraints.Mode != nil {
		if networkModeRank(config.Network.Mode) > networkModeRank(*constraints.Mode) {
			return invalidValue("network.mode", string(config.Network.Mode.normalized()),
				string(constraints.Mode.normalized())+" or more restrictive")
		}
	}

	if constraints.AllowUpstreamProxy != nil && !*constraints.AllowUpstreamProxy {
		if config.Network.AllowUpstreamProxy {
			return invalidValue("network.allow_upstream_proxy", "true", "false (disabled by managed config)")
		}
	}

	if constraints.DangerouslyAllowNonLoopbackProxy != nil && !*constraints.DangerouslyAllowNonLoopbackProxy {
		if config.Network.DangerouslyAllowNonLoopbackProxy {
			return invalidValue("network.dangerously_allow_non_loopback_proxy", "true", "false (disabled by managed config)")
		}
	}

	// dangerously_allow_all_unix_sockets defaults to (allow_unix_sockets is unset).
	allowAllUnixSockets := constraints.AllowUnixSockets == nil
	if constraints.DangerouslyAllowAllUnixSockets != nil {
		allowAllUnixSockets = *constraints.DangerouslyAllowAllUnixSockets
	}
	if config.Network.DangerouslyAllowAllUnixSockets && !allowAllUnixSockets {
		return invalidValue("network.dangerously_allow_all_unix_sockets", "true", "false (disabled by managed config)")
	}

	if constraints.AllowLocalBinding != nil && !*constraints.AllowLocalBinding {
		if config.Network.AllowLocalBinding {
			return invalidValue("network.allow_local_binding", "true", "false (disabled by managed config)")
		}
	}

	if constraints.AllowedDomains != nil {
		if err := validateAllowedDomainsConstraint(*constraints.AllowedDomains, constraints.AllowlistExpansionEnabled, configAllowedDomains, deniedDomainOverrides); err != nil {
			return err
		}
	}

	if constraints.DeniedDomains != nil {
		if err := validateDeniedDomainsConstraint(*constraints.DeniedDomains, constraints.DenylistExpansionEnabled, configDeniedDomains); err != nil {
			return err
		}
	}

	if constraints.AllowUnixSockets != nil {
		allowed := lowercaseSet(*constraints.AllowUnixSockets)
		var invalid []string
		for _, entry := range configAllowUnixSockets {
			if _, ok := allowed[strings.ToLower(entry)]; !ok {
				invalid = append(invalid, entry)
			}
		}
		if len(invalid) > 0 {
			return invalidValue("network.allow_unix_sockets", fmt.Sprintf("%q", invalid), "subset of managed allow_unix_sockets")
		}
	}

	return nil
}

func validateAllowedDomainsConstraint(managed []string, expansion *bool, candidate []string, deniedOverrides map[string]struct{}) error {
	if err := validateNonGlobalWildcardDomainPatterns("network.allowed_domains", managed); err != nil {
		return err
	}
	requiredSet := lowercaseSet(managed)
	switch {
	case expansion != nil && *expansion:
		candidateSet := lowercaseSet(candidate)
		var missing []string
		for entry := range requiredSet {
			_, inCandidate := candidateSet[entry]
			_, inDenied := deniedOverrides[entry]
			if !inCandidate && !inDenied {
				missing = append(missing, entry)
			}
		}
		if len(missing) > 0 {
			return invalidValue("network.allowed_domains", "missing managed allowed_domains entries", fmt.Sprintf("%q", missing))
		}
	case expansion != nil && !*expansion:
		candidateSet := lowercaseSet(candidate)
		expectedSet := make(map[string]struct{})
		for entry := range requiredSet {
			if _, denied := deniedOverrides[entry]; !denied {
				expectedSet[entry] = struct{}{}
			}
		}
		if !setsEqual(candidateSet, expectedSet) {
			return invalidValue("network.allowed_domains", fmt.Sprintf("%q", candidate), "must match managed allowed_domains")
		}
	default:
		managedPatterns := make([]domainPattern, 0, len(managed))
		for _, entry := range managed {
			managedPatterns = append(managedPatterns, parseDomainPatternForConstraints(entry))
		}
		var invalid []string
		for _, entry := range candidate {
			candidatePattern := parseDomainPatternForConstraints(entry)
			allowed := false
			for _, m := range managedPatterns {
				if m.allows(candidatePattern) {
					allowed = true
					break
				}
			}
			if !allowed {
				invalid = append(invalid, entry)
			}
		}
		if len(invalid) > 0 {
			return invalidValue("network.allowed_domains", fmt.Sprintf("%q", invalid), "subset of managed allowed_domains")
		}
	}
	return nil
}

func validateDeniedDomainsConstraint(managed []string, expansion *bool, candidate []string) error {
	if err := validateNonGlobalWildcardDomainPatterns("network.denied_domains", managed); err != nil {
		return err
	}
	requiredSet := lowercaseSet(managed)
	if expansion != nil && !*expansion {
		candidateSet := lowercaseSet(candidate)
		if !setsEqual(candidateSet, requiredSet) {
			return invalidValue("network.denied_domains", fmt.Sprintf("%q", candidate), "must match managed denied_domains")
		}
		return nil
	}
	candidateSet := lowercaseSet(candidate)
	var missing []string
	for entry := range requiredSet {
		if _, ok := candidateSet[entry]; !ok {
			missing = append(missing, entry)
		}
	}
	if len(missing) > 0 {
		return invalidValue("network.denied_domains", "missing managed denied_domains entries", fmt.Sprintf("%q", missing))
	}
	return nil
}

func validateNonGlobalWildcardDomainPatterns(fieldName string, patterns []string) error {
	for _, pattern := range patterns {
		if isGlobalWildcardDomainPattern(pattern) {
			return NetworkProxyConstraintError{
				FieldName: fieldName,
				Candidate: strings.TrimSpace(pattern),
				Allowed:   "exact hosts or scoped wildcards like *.example.com or **.example.com",
			}
		}
	}
	return nil
}

func lowercaseSet(entries []string) map[string]struct{} {
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[strings.ToLower(entry)] = struct{}{}
	}
	return out
}

func setsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
