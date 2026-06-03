package networkproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

const dnsLookupTimeout = 2 * time.Second

// hostBlockReason is the baseline-policy reason for a blocked host.
type hostBlockReason int

const (
	hostBlockDenied hostBlockReason = iota
	hostBlockNotAllowed
	hostBlockNotAllowedLocal
)

func (r hostBlockReason) asStr() string {
	switch r {
	case hostBlockDenied:
		return reasonDenied
	case hostBlockNotAllowed:
		return reasonNotAllowed
	case hostBlockNotAllowedLocal:
		return reasonNotAllowedLocal
	default:
		return reasonNotAllowed
	}
}

// hostBlockDecision is the result of host_blocked: allowed, or blocked with a
// reason.
type hostBlockDecision struct {
	allowed bool
	reason  hostBlockReason
}

// ConfigState is the compiled, immutable policy state derived from a config.
type ConfigState struct {
	Config       NetworkProxyConfig
	allowSet     domainGlobSet
	denySet      domainGlobSet
	mitmEnabled  bool
	mitmHooks    mitmHooksByHost
	constraints  NetworkProxyConstraints
	blocked      []BlockedRequest
	blockedTotal uint64
}

// BuildConfigState compiles a config and constraints into a ConfigState,
// validating domain patterns, MITM hooks, and unix-socket paths. Mirrors Rust's
// `build_config_state`.
func BuildConfigState(config NetworkProxyConfig, constraints NetworkProxyConstraints) (ConfigState, error) {
	if err := validateUnixSocketAllowlistPaths(config); err != nil {
		return ConfigState{}, err
	}
	allowedDomains := config.Network.AllowedDomains()
	deniedDomains := config.Network.DeniedDomains()
	if err := validateNonGlobalWildcardDomainPatterns("network.denied_domains", deniedDomains); err != nil {
		return ConfigState{}, err
	}
	denySet, err := compileDenylistGlobset(deniedDomains)
	if err != nil {
		return ConfigState{}, fmt.Errorf("compile denylist: %w", err)
	}
	allowSet, err := compileAllowlistGlobset(allowedDomains)
	if err != nil {
		return ConfigState{}, fmt.Errorf("compile allowlist: %w", err)
	}
	hooks, err := compileMitmHooks(config)
	if err != nil {
		return ConfigState{}, fmt.Errorf("compile mitm hooks: %w", err)
	}
	return ConfigState{
		Config:      config,
		allowSet:    allowSet,
		denySet:     denySet,
		mitmEnabled: config.Network.Mitm,
		mitmHooks:   hooks,
		constraints: constraints,
	}, nil
}

// hostLookupFunc resolves a host:port to a set of IP addresses. It is injectable
// for testing.
type hostLookupFunc func(ctx context.Context, host string, port uint16) ([]netip.Addr, error)

// NetworkProxyState is a live, concurrency-safe view of compiled policy. It
// exposes policy queries (host_blocked, method_allowed, unix-socket checks),
// blocked-request telemetry, and the audit metadata.
type NetworkProxyState struct {
	mu        sync.RWMutex
	state     ConfigState
	observer  BlockedRequestObserver
	auditSink AuditSink
	metadata  NetworkProxyAuditMetadata
	lookup    hostLookupFunc
}

// NewNetworkProxyState constructs a state from a compiled ConfigState.
func NewNetworkProxyState(state ConfigState) *NetworkProxyState {
	return &NetworkProxyState{
		state:  state,
		lookup: defaultHostLookup,
	}
}

// WithAuditMetadata sets the audit metadata. Returns the receiver for chaining.
func (s *NetworkProxyState) WithAuditMetadata(metadata NetworkProxyAuditMetadata) *NetworkProxyState {
	s.metadata = metadata
	return s
}

// WithAuditSink sets the audit sink. Returns the receiver for chaining.
func (s *NetworkProxyState) WithAuditSink(sink AuditSink) *NetworkProxyState {
	s.auditSink = sink
	return s
}

// WithBlockedRequestObserver sets the blocked-request observer. Returns the
// receiver for chaining.
func (s *NetworkProxyState) WithBlockedRequestObserver(observer BlockedRequestObserver) *NetworkProxyState {
	s.SetBlockedRequestObserver(observer)
	return s
}

// SetBlockedRequestObserver atomically replaces the blocked-request observer.
func (s *NetworkProxyState) SetBlockedRequestObserver(observer BlockedRequestObserver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observer = observer
}

// AuditMetadata returns the audit metadata.
func (s *NetworkProxyState) AuditMetadata() NetworkProxyAuditMetadata {
	return s.metadata
}

// CurrentConfig returns a copy of the current config.
func (s *NetworkProxyState) CurrentConfig() NetworkProxyConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Config
}

// CurrentPatterns returns the current allow and deny domain patterns.
func (s *NetworkProxyState) CurrentPatterns() (allowed, denied []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Config.Network.AllowedDomains(), s.state.Config.Network.DeniedDomains()
}

// Enabled reports whether the proxy is enabled.
func (s *NetworkProxyState) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Config.Network.Enabled
}

// NetworkMode returns the current mode.
func (s *NetworkProxyState) NetworkMode() NetworkMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Config.Network.Mode.normalized()
}

// MethodAllowed reports whether the method is permitted under the current mode.
func (s *NetworkProxyState) MethodAllowed(method string) bool {
	return s.NetworkMode().AllowsMethod(method)
}

// AllowUpstreamProxy reports whether upstream HTTP(S) proxies are honored.
func (s *NetworkProxyState) AllowUpstreamProxy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Config.Network.AllowUpstreamProxy
}

// AllowLocalBinding reports whether local/private targets are permitted.
func (s *NetworkProxyState) AllowLocalBinding() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Config.Network.AllowLocalBinding
}

// SetNetworkMode updates the mode after validating against constraints.
func (s *NetworkProxyState) SetNetworkMode(mode NetworkMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.state.Config
	candidate.Network.Mode = mode
	if err := ValidatePolicyAgainstConstraints(candidate, s.state.constraints); err != nil {
		return fmt.Errorf("network.mode constrained by managed config: %w", err)
	}
	s.state.Config.Network.Mode = mode
	return nil
}

// SetLookupFunc overrides the DNS resolver (used in tests).
func (s *NetworkProxyState) SetLookupFunc(lookup hostLookupFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lookup == nil {
		lookup = defaultHostLookup
	}
	s.lookup = lookup
}

// HostHasMitmHooks reports whether any MITM hooks are configured for the host.
func (s *NetworkProxyState) HostHasMitmHooks(host string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.state.mitmHooks[NormalizeHost(host)]
	return ok
}

// MitmEnabled reports whether MITM termination is enabled.
func (s *NetworkProxyState) MitmEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.mitmEnabled
}

// EvaluateMitmHookRequest evaluates hooks for an HTTP request against the host.
func (s *NetworkProxyState) evaluateMitmHookRequest(host string, req *http.Request) hookEvaluation {
	s.mu.RLock()
	hooks := s.state.mitmHooks
	s.mu.RUnlock()
	return evaluateMitmHooks(hooks, host, req)
}

// hostBlocked evaluates whether a host:port is blocked by baseline policy.
// Faithful port of Rust's `host_blocked`.
func (s *NetworkProxyState) hostBlocked(ctx context.Context, host string, port uint16) hostBlockDecision {
	normalized, ok := parseHost(host)
	if !ok {
		return hostBlockDecision{reason: hostBlockNotAllowed}
	}

	s.mu.RLock()
	denySet := s.state.denySet
	allowSet := s.state.allowSet
	allowLocalBinding := s.state.Config.Network.AllowLocalBinding
	allowedDomains := s.state.Config.Network.AllowedDomains()
	lookup := s.lookup
	s.mu.RUnlock()

	allowedDomainsEmpty := len(allowedDomains) == 0

	// 1) explicit deny always wins.
	if globsetMatchesHostOrUnscoped(denySet, normalized) {
		return hostBlockDecision{reason: hostBlockDenied}
	}

	isAllowlisted := globsetMatchesHostOrUnscoped(allowSet, normalized)

	if !allowLocalBinding {
		hostNoScope := normalized
		if ip, ok := unscopedIPLiteral(normalized); ok {
			hostNoScope = ip
		}
		localLiteral := false
		if isLoopbackHost(normalized) {
			localLiteral = true
		} else if addr, err := netip.ParseAddr(hostNoScope); err == nil {
			localLiteral = isNonPublicIP(addr)
		}

		if localLiteral {
			if !isExplicitLocalAllowlisted(allowedDomains, normalized) {
				return hostBlockDecision{reason: hostBlockNotAllowedLocal}
			}
		} else if hostResolvesToNonPublicIP(ctx, normalized, port, dnsLookupTimeout, lookup) {
			return hostBlockDecision{reason: hostBlockNotAllowedLocal}
		}
	}

	if allowedDomainsEmpty || !isAllowlisted {
		return hostBlockDecision{reason: hostBlockNotAllowed}
	}
	return hostBlockDecision{allowed: true}
}

// RecordBlocked appends a blocked-request entry, bounds the buffer, and notifies
// the observer.
func (s *NetworkProxyState) RecordBlocked(ctx context.Context, entry BlockedRequest) {
	s.mu.Lock()
	observer := s.observer
	s.state.blocked = append(s.state.blocked, entry)
	s.state.blockedTotal++
	for len(s.state.blocked) > maxBlockedEvents {
		s.state.blocked = s.state.blocked[1:]
	}
	s.mu.Unlock()

	if observer != nil {
		observer.OnBlockedRequest(ctx, entry)
	}
}

// BlockedSnapshot returns a copy of the buffered blocked-request entries.
func (s *NetworkProxyState) BlockedSnapshot() []BlockedRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]BlockedRequest, len(s.state.blocked))
	copy(out, s.state.blocked)
	return out
}

// DrainBlocked removes and returns the buffered blocked-request entries.
func (s *NetworkProxyState) DrainBlocked() []BlockedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.state.blocked
	s.state.blocked = nil
	return out
}

// AddAllowedDomain adds a host to the allowlist (and removes a matching deny).
func (s *NetworkProxyState) AddAllowedDomain(host string) error {
	return s.updateDomainList(host, DomainPermissionAllow)
}

// AddDeniedDomain adds a host to the denylist (and removes a matching allow).
func (s *NetworkProxyState) AddDeniedDomain(host string) error {
	return s.updateDomainList(host, DomainPermissionDeny)
}

func (s *NetworkProxyState) updateDomainList(host string, permission NetworkDomainPermission) error {
	normalized, ok := parseHost(host)
	if !ok {
		return fmt.Errorf("invalid network host: %q", host)
	}
	listName := "allowlist"
	constraintField := "network.allowed_domains"
	if permission == DomainPermissionDeny {
		listName = "denylist"
		constraintField = "network.denied_domains"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.state.Config
	targetEntries := current.Network.AllowedDomains()
	oppositeEntries := current.Network.DeniedDomains()
	if permission == DomainPermissionDeny {
		targetEntries, oppositeEntries = oppositeEntries, targetEntries
	}
	targetContains := containsNormalized(targetEntries, normalized)
	oppositeContains := containsNormalized(oppositeEntries, normalized)
	if targetContains && !oppositeContains {
		return nil
	}

	candidate := current
	candidate.Network = current.Network.WithUpsertDomainPermission(normalized, permission)
	if err := ValidatePolicyAgainstConstraints(candidate, s.state.constraints); err != nil {
		return fmt.Errorf("%s constrained by managed config: %w", constraintField, err)
	}
	newState, err := BuildConfigState(candidate, s.state.constraints)
	if err != nil {
		return fmt.Errorf("failed to compile updated network %s: %w", listName, err)
	}
	newState.blocked = s.state.blocked
	newState.blockedTotal = s.state.blockedTotal
	s.state = newState
	return nil
}

// IsUnixSocketAllowed reports whether proxying to the socket path is permitted.
// Faithful port of Rust's `is_unix_socket_allowed` (macOS-only, absolute-path).
func (s *NetworkProxyState) IsUnixSocketAllowed(path string) bool {
	if !unixSocketPermissionsSupported() {
		return false
	}
	if !filepath.IsAbs(path) {
		return false
	}

	s.mu.RLock()
	dangerouslyAllowAll := s.state.Config.Network.DangerouslyAllowAllUnixSockets
	allowList := s.state.Config.Network.AllowUnixSockets()
	s.mu.RUnlock()

	if dangerouslyAllowAll {
		return true
	}

	requestedAbs, err := abspath.FromAbsolutePath(path)
	if err != nil {
		return false
	}
	requestedCanonical, _ := filepath.EvalSymlinks(requestedAbs.Path())

	for _, allowed := range allowList {
		validated, err := parseValidatedUnixSocketPath(allowed)
		if err != nil || validated.kind != unixPathNative {
			continue
		}
		if validated.native.Path() == requestedAbs.Path() {
			return true
		}
		if requestedCanonical == "" {
			continue
		}
		if allowedCanonical, err := filepath.EvalSymlinks(validated.native.Path()); err == nil && allowedCanonical == requestedCanonical {
			return true
		}
	}
	return false
}

func containsNormalized(entries []string, normalized string) bool {
	for _, e := range entries {
		if NormalizeHost(e) == normalized {
			return true
		}
	}
	return false
}

// isExplicitLocalAllowlisted reports whether a local literal is explicitly (not
// via wildcard) allowlisted. Faithful port of Rust's
// `is_explicit_local_allowlisted`.
func isExplicitLocalAllowlisted(allowedDomains []string, host string) bool {
	unscoped, hasUnscoped := unscopedIPLiteral(host)
	for _, pattern := range allowedDomains {
		pattern = strings.TrimSpace(pattern)
		if pattern == "*" || strings.HasPrefix(pattern, "*.") || strings.HasPrefix(pattern, "**.") {
			continue
		}
		if strings.ContainsAny(pattern, "*?") {
			continue
		}
		normalizedPattern := NormalizeHost(pattern)
		if normalizedPattern == host {
			return true
		}
		if hasUnscoped && normalizedPattern == unscoped {
			return true
		}
	}
	return false
}

// hostResolvesToNonPublicIP reports whether the host resolves (or is) a
// non-public IP. A DNS failure or timeout blocks (returns true). Faithful port
// of Rust's `host_resolves_to_non_public_ip`.
func hostResolvesToNonPublicIP(ctx context.Context, host string, port uint16, timeout time.Duration, lookup hostLookupFunc) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return isNonPublicIP(addr)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := lookup(lookupCtx, host, port)
	if err != nil {
		return true
	}
	for _, addr := range addrs {
		if isNonPublicIP(addr) {
			return true
		}
	}
	return false
}

func defaultHostLookup(ctx context.Context, host string, port uint16) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", host, err)
	}
	return ips, nil
}

func unixTimestamp() int64 {
	return time.Now().UTC().Unix()
}

// newBlockedRequest constructs a BlockedRequest with the current timestamp.
func newBlockedRequest(args BlockedRequestArgs) BlockedRequest {
	return BlockedRequest{
		Host:      args.Host,
		Reason:    args.Reason,
		Client:    args.Client,
		Method:    args.Method,
		Mode:      args.Mode,
		Protocol:  args.Protocol,
		Decision:  args.Decision,
		Source:    args.Source,
		Port:      args.Port,
		Timestamp: unixTimestamp(),
	}
}

// evaluateHostPolicy runs baseline host policy, consulting the decider only for
// not_allowed misses, and emits a domain-scope audit event. Faithful port of
// Rust's `evaluate_host_policy`.
func evaluateHostPolicy(ctx context.Context, state *NetworkProxyState, decider NetworkPolicyDecider, request NetworkPolicyRequest) NetworkDecision {
	hostDecision := state.hostBlocked(ctx, request.Host, request.Port)

	var (
		decision       NetworkDecision
		policyOverride bool
	)
	switch {
	case hostDecision.allowed:
		decision, policyOverride = Allow(), false
	case hostDecision.reason == hostBlockNotAllowed:
		if decider != nil {
			deciderDecision := mapDeciderDecision(decider.Decide(ctx, request))
			policyOverride = deciderDecision.Kind == DecisionAllow
			decision = deciderDecision
		} else {
			decision = DenyWithSource(hostBlockNotAllowed.asStr(), protocol.NetworkDecisionSourceBaselinePolicy)
		}
	default:
		decision = DenyWithSource(hostDecision.reason.asStr(), protocol.NetworkDecisionSourceBaselinePolicy)
	}

	var (
		auditDecision string
		auditSource   protocol.NetworkDecisionSource
		auditReason   string
	)
	if decision.Kind == DecisionAllow {
		auditDecision = auditDecisionAllow
		if policyOverride {
			auditSource = protocol.NetworkDecisionSourceDecider
			auditReason = hostBlockNotAllowed.asStr()
		} else {
			auditSource = protocol.NetworkDecisionSourceBaselinePolicy
			auditReason = auditReasonAllow
		}
	} else {
		auditDecision = string(decision.Decision)
		auditSource = decision.Source
		auditReason = decision.Reason
	}

	emitPolicyAuditEvent(state.auditSink, state.metadata, policyAuditArgs{
		scope:         auditScopeDomain,
		decision:      auditDecision,
		source:        string(auditSource),
		reason:        auditReason,
		protocol:      request.Protocol,
		serverAddress: request.Host,
		serverPort:    request.Port,
		method:        request.Method,
		hasMethod:     request.Method != "",
		clientAddr:    request.ClientAddr,
		hasClient:     request.ClientAddr != "",
		override:      policyOverride,
	})

	return decision
}
