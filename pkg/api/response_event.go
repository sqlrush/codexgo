package api

import "github.com/sqlrush/codexgo/pkg/protocol"

// ResponseEventKind discriminates the variants of ResponseEvent, mirroring the
// Rust `ResponseEvent` enum.
type ResponseEventKind int

const (
	// ResponseEventCreated corresponds to `response.created`.
	ResponseEventCreated ResponseEventKind = iota
	// ResponseEventOutputItemDone corresponds to `response.output_item.done`.
	ResponseEventOutputItemDone
	// ResponseEventOutputItemAdded corresponds to `response.output_item.added`.
	ResponseEventOutputItemAdded
	// ResponseEventServerModel carries the server-selected model.
	ResponseEventServerModel
	// ResponseEventModelVerifications carries recommended account verifications.
	ResponseEventModelVerifications
	// ResponseEventServerReasoningIncluded carries the reasoning-included flag.
	ResponseEventServerReasoningIncluded
	// ResponseEventCompleted corresponds to `response.completed`.
	ResponseEventCompleted
	// ResponseEventOutputTextDelta corresponds to `response.output_text.delta`.
	ResponseEventOutputTextDelta
	// ResponseEventToolCallInputDelta corresponds to custom-tool-call input deltas.
	ResponseEventToolCallInputDelta
	// ResponseEventReasoningSummaryDelta corresponds to reasoning summary deltas.
	ResponseEventReasoningSummaryDelta
	// ResponseEventReasoningContentDelta corresponds to reasoning text deltas.
	ResponseEventReasoningContentDelta
	// ResponseEventReasoningSummaryPartAdded corresponds to summary part additions.
	ResponseEventReasoningSummaryPartAdded
	// ResponseEventRateLimits carries a parsed rate-limit snapshot.
	ResponseEventRateLimits
	// ResponseEventModelsEtag carries the model catalog ETag.
	ResponseEventModelsEtag
)

// ResponseEvent is a single event emitted by the Responses stream. It mirrors the
// Rust `ResponseEvent` enum, using a Kind discriminator with per-variant fields.
type ResponseEvent struct {
	Kind ResponseEventKind

	// Item is set for OutputItemDone / OutputItemAdded.
	Item *protocol.ResponseItem

	// Model is set for ServerModel.
	Model string

	// Verifications is set for ModelVerifications.
	Verifications []protocol.ModelVerification

	// ReasoningIncluded is set for ServerReasoningIncluded.
	ReasoningIncluded bool

	// Completed fields.
	ResponseID string
	TokenUsage *protocol.TokenUsage
	EndTurn    *bool

	// Delta is set for OutputTextDelta, ToolCallInputDelta,
	// ReasoningSummaryDelta, and ReasoningContentDelta.
	Delta string

	// ToolCallInputDelta fields.
	ItemID string
	CallID *string

	// SummaryIndex is set for ReasoningSummaryDelta / ReasoningSummaryPartAdded.
	SummaryIndex int64
	// ContentIndex is set for ReasoningContentDelta.
	ContentIndex int64

	// RateLimits is set for RateLimits.
	RateLimits *protocol.RateLimitSnapshot

	// ModelsEtag is set for ModelsEtag.
	ModelsEtag string
}
