package networkproxy

import (
	"context"
	"encoding/json"
)

const (
	maxBlockedEvents             = 200
	networkPolicyViolationPrefix = "CODEXGO_NETWORK_POLICY_VIOLATION"
)

// BlockedRequest records a single policy violation for telemetry. JSON tags
// match codex so violation log lines are byte-compatible.
type BlockedRequest struct {
	Host      string       `json:"host"`
	Reason    string       `json:"reason"`
	Client    *string      `json:"client"`
	Method    *string      `json:"method"`
	Mode      *NetworkMode `json:"mode"`
	Protocol  string       `json:"protocol"`
	Decision  *string      `json:"decision,omitempty"`
	Source    *string      `json:"source,omitempty"`
	Port      *uint16      `json:"port,omitempty"`
	Timestamp int64        `json:"timestamp"`
}

// BlockedRequestArgs are the inputs for constructing a BlockedRequest. The
// timestamp is filled in at construction time.
type BlockedRequestArgs struct {
	Host     string
	Reason   string
	Client   *string
	Method   *string
	Mode     *NetworkMode
	Protocol string
	Decision *string
	Source   *string
	Port     *uint16
}

// BlockedRequestObserver is notified for each blocked request, for logging
// policy violations. Implementations must be safe for concurrent use.
type BlockedRequestObserver interface {
	OnBlockedRequest(ctx context.Context, request BlockedRequest)
}

// BlockedRequestObserverFunc adapts a function to BlockedRequestObserver.
type BlockedRequestObserverFunc func(ctx context.Context, request BlockedRequest)

// OnBlockedRequest implements BlockedRequestObserver.
func (f BlockedRequestObserverFunc) OnBlockedRequest(ctx context.Context, request BlockedRequest) {
	f(ctx, request)
}

// blockedRequestViolationLogLine renders the canonical violation log line,
// matching Rust's format exactly.
func blockedRequestViolationLogLine(entry BlockedRequest) string {
	data, err := json.Marshal(entry)
	if err != nil {
		host := entry.Host
		reason := entry.Reason
		return networkPolicyViolationPrefix + " host=" + host + " reason=" + reason
	}
	return networkPolicyViolationPrefix + " " + string(data)
}

func strPtr(s string) *string { return &s }
