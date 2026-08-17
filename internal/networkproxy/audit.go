package networkproxy

import (
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

const (
	auditPolicyDecisionEventName = "codex.network_proxy.policy_decision"
	auditScopeDomain             = "domain"
	auditScopeNonDomain          = "non_domain"
	auditDecisionAllow           = "allow"
	auditDecisionDeny            = "deny"
	auditReasonAllow             = "allow"
	auditDefaultMethod           = "none"
	auditDefaultClientAddress    = "unknown"
)

// NetworkProxyAuditMetadata carries identity/context fields attached to every
// policy-decision audit event. All fields are optional.
type NetworkProxyAuditMetadata struct {
	ConversationID string
	AppVersion     string
	UserAccountID  string
	AuthMode       string
	Originator     string
	UserEmail      string
	TerminalType   string
	Model          string
	Slug           string
}

// PolicyDecisionAuditEvent is a structured audit event emitted on every policy
// decision. Field names mirror the OTEL-compatible keys documented by codex.
type PolicyDecisionAuditEvent struct {
	EventName string
	Timestamp string // RFC3339 UTC, millisecond precision
	Metadata  NetworkProxyAuditMetadata

	Scope    string // "domain" or "non_domain"
	Decision string // "allow", "deny", or "ask"
	Source   string
	Reason   string
	Protocol string
	// ServerAddress / ServerPort use sentinel values ("unix-socket", 0) for
	// unix-socket block paths.
	ServerAddress string
	ServerPort    uint16
	Method        string
	ClientAddress string
	Override      bool
}

// AuditSink receives policy-decision audit events. Implementations must be safe
// for concurrent use. A nil sink disables auditing.
type AuditSink interface {
	EmitPolicyDecision(event PolicyDecisionAuditEvent)
}

// AuditSinkFunc adapts a function to the AuditSink interface.
type AuditSinkFunc func(event PolicyDecisionAuditEvent)

// EmitPolicyDecision implements AuditSink.
func (f AuditSinkFunc) EmitPolicyDecision(event PolicyDecisionAuditEvent) { f(event) }

func auditTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// blockDecisionAuditArgs is the input for non-domain (mode-guard/proxy-state)
// audit events.
type blockDecisionAuditArgs struct {
	source        protocol.NetworkDecisionSource
	reason        string
	protocol      NetworkProtocol
	serverAddress string
	serverPort    uint16
	method        string // empty -> "none"
	hasMethod     bool
	clientAddr    string // empty -> "unknown"
	hasClient     bool
}

type policyAuditArgs struct {
	scope         string
	decision      string
	source        string
	reason        string
	protocol      NetworkProtocol
	serverAddress string
	serverPort    uint16
	method        string
	hasMethod     bool
	clientAddr    string
	hasClient     bool
	override      bool
}

func emitBlockDecisionAuditEvent(sink AuditSink, metadata NetworkProxyAuditMetadata, args blockDecisionAuditArgs) {
	emitNonDomainAuditEvent(sink, metadata, args, auditDecisionDeny)
}

func emitAllowDecisionAuditEvent(sink AuditSink, metadata NetworkProxyAuditMetadata, args blockDecisionAuditArgs) {
	emitNonDomainAuditEvent(sink, metadata, args, auditDecisionAllow)
}

func emitNonDomainAuditEvent(sink AuditSink, metadata NetworkProxyAuditMetadata, args blockDecisionAuditArgs, decision string) {
	emitPolicyAuditEvent(sink, metadata, policyAuditArgs{
		scope:         auditScopeNonDomain,
		decision:      decision,
		source:        string(args.source),
		reason:        args.reason,
		protocol:      args.protocol,
		serverAddress: args.serverAddress,
		serverPort:    args.serverPort,
		method:        args.method,
		hasMethod:     args.hasMethod,
		clientAddr:    args.clientAddr,
		hasClient:     args.hasClient,
		override:      false,
	})
}

func emitPolicyAuditEvent(sink AuditSink, metadata NetworkProxyAuditMetadata, args policyAuditArgs) {
	if sink == nil {
		return
	}
	method := auditDefaultMethod
	if args.hasMethod {
		method = args.method
	}
	client := auditDefaultClientAddress
	if args.hasClient {
		client = args.clientAddr
	}
	sink.EmitPolicyDecision(PolicyDecisionAuditEvent{
		EventName:     auditPolicyDecisionEventName,
		Timestamp:     auditTimestamp(),
		Metadata:      metadata,
		Scope:         args.scope,
		Decision:      args.decision,
		Source:        args.source,
		Reason:        args.reason,
		Protocol:      args.protocol.PolicyProtocol(),
		ServerAddress: args.serverAddress,
		ServerPort:    args.serverPort,
		Method:        method,
		ClientAddress: client,
		Override:      args.override,
	})
}
