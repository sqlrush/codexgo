package extensionapi

import (
	"context"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ExtensionRegistryBuilder is the mutable registry used while hosts register
// typed runtime contributions. Mirrors the Rust `ExtensionRegistryBuilder<C>`.
//
// The type parameter C is the host configuration type carried by the
// config-aware contributors.
type ExtensionRegistryBuilder[C any] struct {
	eventSink                   ExtensionEventSink
	threadLifecycleContributors []ThreadLifecycleContributor[C]
	turnLifecycleContributors   []TurnLifecycleContributor
	configContributors          []ConfigContributor[C]
	tokenUsageContributors      []TokenUsageContributor
	contextContributors         []ContextContributor
	toolContributors            []ToolContributor
	toolLifecycleContributors   []ToolLifecycleContributor
	turnItemContributors        []TurnItemContributor
	approvalReviewContributors  []ApprovalReviewContributor
}

// NewExtensionRegistryBuilder creates an empty registry builder. Mirrors Rust
// `ExtensionRegistryBuilder::new`.
func NewExtensionRegistryBuilder[C any]() *ExtensionRegistryBuilder[C] {
	return &ExtensionRegistryBuilder[C]{eventSink: NoopExtensionEventSink{}}
}

// NewExtensionRegistryBuilderWithEventSink creates an empty registry builder
// with a host-provided event sink. Mirrors Rust
// `ExtensionRegistryBuilder::with_event_sink`.
func NewExtensionRegistryBuilderWithEventSink[C any](eventSink ExtensionEventSink) *ExtensionRegistryBuilder[C] {
	if eventSink == nil {
		eventSink = NoopExtensionEventSink{}
	}
	return &ExtensionRegistryBuilder[C]{eventSink: eventSink}
}

// EventSink returns the host event sink to pass into extension constructors.
// Mirrors Rust `ExtensionRegistryBuilder::event_sink`.
func (b *ExtensionRegistryBuilder[C]) EventSink() ExtensionEventSink {
	return b.eventSink
}

// AddApprovalReviewContributor registers one approval-review contributor.
// Mirrors Rust `approval_review_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddApprovalReviewContributor(contributor ApprovalReviewContributor) {
	b.approvalReviewContributors = append(b.approvalReviewContributors, contributor)
}

// AddThreadLifecycleContributor registers one thread-lifecycle contributor.
// Mirrors Rust `thread_lifecycle_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddThreadLifecycleContributor(contributor ThreadLifecycleContributor[C]) {
	b.threadLifecycleContributors = append(b.threadLifecycleContributors, contributor)
}

// AddTurnLifecycleContributor registers one turn-lifecycle contributor. Mirrors
// Rust `turn_lifecycle_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddTurnLifecycleContributor(contributor TurnLifecycleContributor) {
	b.turnLifecycleContributors = append(b.turnLifecycleContributors, contributor)
}

// AddConfigContributor registers one config contributor. Mirrors Rust
// `config_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddConfigContributor(contributor ConfigContributor[C]) {
	b.configContributors = append(b.configContributors, contributor)
}

// AddTokenUsageContributor registers one token-usage contributor. Mirrors Rust
// `token_usage_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddTokenUsageContributor(contributor TokenUsageContributor) {
	b.tokenUsageContributors = append(b.tokenUsageContributors, contributor)
}

// AddPromptContributor registers one prompt contributor. Mirrors Rust
// `prompt_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddPromptContributor(contributor ContextContributor) {
	b.contextContributors = append(b.contextContributors, contributor)
}

// AddToolContributor registers one native tool contributor. Mirrors Rust
// `tool_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddToolContributor(contributor ToolContributor) {
	b.toolContributors = append(b.toolContributors, contributor)
}

// AddToolLifecycleContributor registers one tool-lifecycle contributor. Mirrors
// Rust `tool_lifecycle_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddToolLifecycleContributor(contributor ToolLifecycleContributor) {
	b.toolLifecycleContributors = append(b.toolLifecycleContributors, contributor)
}

// AddTurnItemContributor registers one ordered turn-item contributor. Mirrors
// Rust `turn_item_contributor`.
func (b *ExtensionRegistryBuilder[C]) AddTurnItemContributor(contributor TurnItemContributor) {
	b.turnItemContributors = append(b.turnItemContributors, contributor)
}

// Build finishes construction and returns the immutable registry. Mirrors Rust
// `ExtensionRegistryBuilder::build`.
func (b *ExtensionRegistryBuilder[C]) Build() *ExtensionRegistry[C] {
	return &ExtensionRegistry[C]{
		eventSink:                   b.eventSink,
		threadLifecycleContributors: cloneSlice(b.threadLifecycleContributors),
		turnLifecycleContributors:   cloneSlice(b.turnLifecycleContributors),
		configContributors:          cloneSlice(b.configContributors),
		tokenUsageContributors:      cloneSlice(b.tokenUsageContributors),
		contextContributors:         cloneSlice(b.contextContributors),
		toolContributors:            cloneSlice(b.toolContributors),
		toolLifecycleContributors:   cloneSlice(b.toolLifecycleContributors),
		turnItemContributors:        cloneSlice(b.turnItemContributors),
		approvalReviewContributors:  cloneSlice(b.approvalReviewContributors),
	}
}

// ExtensionRegistry is the immutable typed registry produced after extensions
// are installed. Mirrors the Rust `ExtensionRegistry<C>`.
type ExtensionRegistry[C any] struct {
	eventSink                   ExtensionEventSink
	threadLifecycleContributors []ThreadLifecycleContributor[C]
	turnLifecycleContributors   []TurnLifecycleContributor
	configContributors          []ConfigContributor[C]
	tokenUsageContributors      []TokenUsageContributor
	contextContributors         []ContextContributor
	toolContributors            []ToolContributor
	toolLifecycleContributors   []ToolLifecycleContributor
	turnItemContributors        []TurnItemContributor
	approvalReviewContributors  []ApprovalReviewContributor
}

// EventSink returns the host event sink retained by this registry. Mirrors Rust
// `ExtensionRegistry::event_sink`.
func (r *ExtensionRegistry[C]) EventSink() ExtensionEventSink {
	return r.eventSink
}

// ThreadLifecycleContributors returns the registered thread-lifecycle
// contributors. Mirrors Rust `thread_lifecycle_contributors`.
func (r *ExtensionRegistry[C]) ThreadLifecycleContributors() []ThreadLifecycleContributor[C] {
	return r.threadLifecycleContributors
}

// TurnLifecycleContributors returns the registered turn-lifecycle contributors.
// Mirrors Rust `turn_lifecycle_contributors`.
func (r *ExtensionRegistry[C]) TurnLifecycleContributors() []TurnLifecycleContributor {
	return r.turnLifecycleContributors
}

// ConfigContributors returns the registered config contributors. Mirrors Rust
// `config_contributors`.
func (r *ExtensionRegistry[C]) ConfigContributors() []ConfigContributor[C] {
	return r.configContributors
}

// TokenUsageContributors returns the registered token-usage contributors.
// Mirrors Rust `token_usage_contributors`.
func (r *ExtensionRegistry[C]) TokenUsageContributors() []TokenUsageContributor {
	return r.tokenUsageContributors
}

// ApprovalReview claims the first rendered approval-review prompt accepted by an
// installed contributor. Mirrors Rust `ExtensionRegistry::approval_review`.
func (r *ExtensionRegistry[C]) ApprovalReview(ctx context.Context, sessionStore, threadStore *ExtensionData, prompt string) (protocol.ReviewDecision, bool) {
	for _, contributor := range r.approvalReviewContributors {
		if decision, ok := contributor.Contribute(ctx, sessionStore, threadStore, prompt); ok {
			return decision, true
		}
	}
	return protocol.ReviewDecision{}, false
}

// ContextContributors returns the registered prompt contributors. Mirrors Rust
// `context_contributors`.
func (r *ExtensionRegistry[C]) ContextContributors() []ContextContributor {
	return r.contextContributors
}

// ToolContributors returns the registered native tool contributors. Mirrors
// Rust `tool_contributors`.
func (r *ExtensionRegistry[C]) ToolContributors() []ToolContributor {
	return r.toolContributors
}

// ToolLifecycleContributors returns the registered tool-lifecycle contributors.
// Mirrors Rust `tool_lifecycle_contributors`.
func (r *ExtensionRegistry[C]) ToolLifecycleContributors() []ToolLifecycleContributor {
	return r.toolLifecycleContributors
}

// TurnItemContributors returns the registered ordered turn-item contributors.
// Mirrors Rust `turn_item_contributors`.
func (r *ExtensionRegistry[C]) TurnItemContributors() []TurnItemContributor {
	return r.turnItemContributors
}

// EmptyExtensionRegistry creates an empty registry for hosts that do not
// register contributions. Mirrors Rust `empty_extension_registry`.
func EmptyExtensionRegistry[C any]() *ExtensionRegistry[C] {
	return NewExtensionRegistryBuilder[C]().Build()
}

// cloneSlice returns a shallow copy of s, preserving the nil-vs-empty
// distinction so an unset registry slice stays nil.
func cloneSlice[T any](s []T) []T {
	if s == nil {
		return nil
	}
	out := make([]T, len(s))
	copy(out, s)
	return out
}
