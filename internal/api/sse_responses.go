package api

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// Header names consulted on the stream response, mirroring the Rust constants.
const (
	xReasoningIncludedHeader            = "X-Reasoning-Included"
	openAIModelHeader                   = "OpenAI-Model"
	requestIDHeader                     = "X-Request-Id"
	xModelsEtagHeader                   = "X-Models-Etag"
	xCodexTurnStateHeader               = "X-Codex-Turn-State"
	trustedAccessForCyberVerification   = "trusted_access_for_cyber"
	streamClosedBeforeCompletedMessage  = "stream closed before response.completed"
	idleTimeoutWaitingForSSEMessage     = "idle timeout waiting for SSE"
	cyberPolicyFallbackMessageForStream = "This request has been flagged for possible cybersecurity risk."
)

// ResponseStream is the consumer-facing handle for a streaming response. It
// mirrors the Rust `ResponseStream`: Events yields parsed events or errors, and
// UpstreamRequestID carries the server's x-request-id header when present.
type ResponseStream struct {
	// Events is closed when the stream ends.
	Events <-chan ResponseResult
	// UpstreamRequestID is the server-assigned x-request-id, when present.
	UpstreamRequestID *string
}

// ResponseResult is one item from a ResponseStream: an event or an error.
type ResponseResult struct {
	Event *ResponseEvent
	Err   *APIError
}

// SpawnResponseStream wraps a transport StreamResponse, emits header-derived
// events first, then parses the SSE body. It mirrors the Rust
// `spawn_response_stream`. turnState, when non-nil, receives the
// x-codex-turn-state header value via the provided callback.
func SpawnResponseStream(
	ctx context.Context,
	streamResponse client.StreamResponse,
	idleTimeout time.Duration,
	telemetry SSETelemetry,
	setTurnState func(string),
) ResponseStream {
	headers := streamResponse.Headers
	rateLimitSnapshots := ParseAllRateLimits(headers)

	var modelsEtag string
	if v := headers.Get(xModelsEtagHeader); v != "" {
		modelsEtag = v
	}
	var serverModel string
	if v := headers.Get(openAIModelHeader); v != "" {
		serverModel = v
	}
	reasoningIncluded := headers.Get(xReasoningIncludedHeader) != ""

	var upstreamRequestID *string
	if v := headers.Get(requestIDHeader); v != "" {
		id := v
		upstreamRequestID = &id
	}
	if setTurnState != nil {
		if v := headers.Get(xCodexTurnStateHeader); v != "" {
			setTurnState(v)
		}
	}

	out := make(chan ResponseResult, 1600)
	go func() {
		defer close(out)
		send := func(res ResponseResult) bool {
			select {
			case <-ctx.Done():
				return false
			case out <- res:
				return true
			}
		}

		if serverModel != "" {
			if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventServerModel, Model: serverModel}}) {
				return
			}
		}
		for i := range rateLimitSnapshots {
			snap := rateLimitSnapshots[i]
			if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventRateLimits, RateLimits: &snap}}) {
				return
			}
		}
		if modelsEtag != "" {
			if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventModelsEtag, ModelsEtag: modelsEtag}}) {
				return
			}
		}
		if reasoningIncluded {
			if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventServerReasoningIncluded, ReasoningIncluded: true}}) {
				return
			}
		}
		processSSE(ctx, streamResponse.Bytes, send, idleTimeout, telemetry)
	}()

	return ResponseStream{Events: out, UpstreamRequestID: upstreamRequestID}
}

// streamError is the deserialized error object from a response.failed event.
type streamError struct {
	Type     *string `json:"type"`
	Code     *string `json:"code"`
	Message  *string `json:"message"`
	PlanType *string `json:"plan_type"`
	ResetsAt *int64  `json:"resets_at"`
}

// responseCompleted is the deserialized `response.completed` payload.
type responseCompleted struct {
	ID      string                  `json:"id"`
	Usage   *responseCompletedUsage `json:"usage"`
	EndTurn *bool                   `json:"end_turn"`
}

type responseCompletedUsage struct {
	InputTokens         int64                          `json:"input_tokens"`
	InputTokensDetails  *responseCompletedInputTokens  `json:"input_tokens_details"`
	OutputTokens        int64                          `json:"output_tokens"`
	OutputTokensDetails *responseCompletedOutputTokens `json:"output_tokens_details"`
	TotalTokens         int64                          `json:"total_tokens"`
}

type responseCompletedInputTokens struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type responseCompletedOutputTokens struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

// toTokenUsage converts the parsed usage into a protocol.TokenUsage, mirroring
// the Rust `From<ResponseCompletedUsage>`.
func (u responseCompletedUsage) toTokenUsage() protocol.TokenUsage {
	var cached, reasoning int64
	if u.InputTokensDetails != nil {
		cached = u.InputTokensDetails.CachedTokens
	}
	if u.OutputTokensDetails != nil {
		reasoning = u.OutputTokensDetails.ReasoningTokens
	}
	return protocol.TokenUsage{
		InputTokens:           u.InputTokens,
		CachedInputTokens:     cached,
		OutputTokens:          u.OutputTokens,
		ReasoningOutputTokens: reasoning,
		TotalTokens:           u.TotalTokens,
	}
}

// ResponsesStreamEvent is the raw deserialized SSE event. It mirrors the Rust
// `ResponsesStreamEvent`.
type ResponsesStreamEvent struct {
	Kind         string          `json:"type"`
	Headers      json.RawMessage `json:"headers"`
	Metadata     json.RawMessage `json:"metadata"`
	Response     json.RawMessage `json:"response"`
	Item         json.RawMessage `json:"item"`
	ItemID       *string         `json:"item_id"`
	CallID       *string         `json:"call_id"`
	Delta        *string         `json:"delta"`
	SummaryIndex *int64          `json:"summary_index"`
	ContentIndex *int64          `json:"content_index"`
}

// EventKind returns the event's type discriminator.
func (e *ResponsesStreamEvent) EventKind() string { return e.Kind }

// ResponseModel returns the effective server model from the event, preferring the
// model under `response.headers` over top-level `headers`. It mirrors the Rust
// `response_model`.
func (e *ResponsesStreamEvent) ResponseModel() string {
	if len(e.Response) > 0 {
		var resp map[string]json.RawMessage
		if json.Unmarshal(e.Response, &resp) == nil {
			if h, ok := resp["headers"]; ok {
				if model := headerOpenAIModelValueFromJSON(h); model != "" {
					return model
				}
			}
		}
	}
	if len(e.Headers) > 0 {
		return headerOpenAIModelValueFromJSON(e.Headers)
	}
	return ""
}

// ModelVerifications returns the recommended verifications for a
// `response.metadata` event. It mirrors the Rust `model_verifications`.
func (e *ResponsesStreamEvent) ModelVerifications() []protocol.ModelVerification {
	if e.Kind != "response.metadata" || len(e.Metadata) == 0 {
		return nil
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(e.Metadata, &meta) != nil {
		return nil
	}
	raw, ok := meta["openai_verification_recommendation"]
	if !ok {
		return nil
	}
	return modelVerificationsFromJSON(raw)
}

func headerOpenAIModelValueFromJSON(raw json.RawMessage) string {
	var headers map[string]json.RawMessage
	if json.Unmarshal(raw, &headers) != nil {
		return ""
	}
	for name, value := range headers {
		if strings.EqualFold(name, "openai-model") || strings.EqualFold(name, "x-openai-model") {
			if s := jsonValueAsString(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func modelVerificationsFromJSON(raw json.RawMessage) []protocol.ModelVerification {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	var out []protocol.ModelVerification
	for _, item := range arr {
		var s string
		if json.Unmarshal(item, &s) != nil {
			continue
		}
		v, ok := parseModelVerification(s)
		if !ok {
			continue
		}
		if !containsVerification(out, v) {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func containsVerification(list []protocol.ModelVerification, v protocol.ModelVerification) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func parseModelVerification(value string) (protocol.ModelVerification, bool) {
	if value == trustedAccessForCyberVerification {
		return protocol.ModelVerificationTrustedAccessForCyber, true
	}
	return "", false
}

// jsonValueAsString extracts a string from a JSON value: a string maps directly,
// and an array maps to its first string element. It mirrors the Rust
// `json_value_as_string`.
func jsonValueAsString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		return jsonValueAsString(arr[0])
	}
	return ""
}

// ProcessResponsesEvent maps a raw stream event to a ResponseEvent (or nil), or
// to an APIError for terminal failure events. It mirrors the Rust
// `process_responses_event`. The returned (nil, nil) means "ignore this event".
func ProcessResponsesEvent(event ResponsesStreamEvent) (*ResponseEvent, *APIError) {
	switch event.Kind {
	case "response.output_item.done":
		if len(event.Item) > 0 {
			var item protocol.ResponseItem
			if json.Unmarshal(event.Item, &item) == nil {
				return &ResponseEvent{Kind: ResponseEventOutputItemDone, Item: &item}, nil
			}
		}
	case "response.output_text.delta":
		if event.Delta != nil {
			return &ResponseEvent{Kind: ResponseEventOutputTextDelta, Delta: *event.Delta}, nil
		}
	case "response.custom_tool_call_input.delta":
		itemID := event.ItemID
		if itemID == nil {
			itemID = event.CallID
		}
		if event.Delta != nil && itemID != nil {
			return &ResponseEvent{
				Kind:   ResponseEventToolCallInputDelta,
				ItemID: *itemID,
				CallID: event.CallID,
				Delta:  *event.Delta,
			}, nil
		}
	case "response.reasoning_summary_text.delta":
		if event.Delta != nil && event.SummaryIndex != nil {
			return &ResponseEvent{
				Kind:         ResponseEventReasoningSummaryDelta,
				Delta:        *event.Delta,
				SummaryIndex: *event.SummaryIndex,
			}, nil
		}
	case "response.reasoning_text.delta":
		if event.Delta != nil && event.ContentIndex != nil {
			return &ResponseEvent{
				Kind:         ResponseEventReasoningContentDelta,
				Delta:        *event.Delta,
				ContentIndex: *event.ContentIndex,
			}, nil
		}
	case "response.created":
		if len(event.Response) > 0 {
			return &ResponseEvent{Kind: ResponseEventCreated}, nil
		}
	case "response.failed":
		return nil, mapResponseFailed(event)
	case "response.incomplete":
		reason := "unknown"
		if len(event.Response) > 0 {
			var resp map[string]json.RawMessage
			if json.Unmarshal(event.Response, &resp) == nil {
				if details, ok := resp["incomplete_details"]; ok {
					var d map[string]json.RawMessage
					if json.Unmarshal(details, &d) == nil {
						if r, ok := d["reason"]; ok {
							var s string
							if json.Unmarshal(r, &s) == nil && s != "" {
								reason = s
							}
						}
					}
				}
			}
		}
		return nil, NewStreamError(fmt.Sprintf("Incomplete response returned, reason: %s", reason))
	case "response.completed":
		if len(event.Response) > 0 {
			var resp responseCompleted
			if err := json.Unmarshal(event.Response, &resp); err != nil {
				return nil, NewStreamError(fmt.Sprintf("failed to parse ResponseCompleted: %v", err))
			}
			ev := &ResponseEvent{
				Kind:       ResponseEventCompleted,
				ResponseID: resp.ID,
				EndTurn:    resp.EndTurn,
			}
			if resp.Usage != nil {
				usage := resp.Usage.toTokenUsage()
				ev.TokenUsage = &usage
			}
			return ev, nil
		}
	case "response.output_item.added":
		if len(event.Item) > 0 {
			var item protocol.ResponseItem
			if json.Unmarshal(event.Item, &item) == nil {
				return &ResponseEvent{Kind: ResponseEventOutputItemAdded, Item: &item}, nil
			}
		}
	case "response.reasoning_summary_part.added":
		if event.SummaryIndex != nil {
			return &ResponseEvent{Kind: ResponseEventReasoningSummaryPartAdded, SummaryIndex: *event.SummaryIndex}, nil
		}
	}
	return nil, nil
}

// mapResponseFailed maps a `response.failed` event to a terminal APIError. It
// mirrors the error classification ladder in the Rust process_responses_event.
func mapResponseFailed(event ResponsesStreamEvent) *APIError {
	if len(event.Response) == 0 {
		return NewStreamError("response.failed event received")
	}
	var resp map[string]json.RawMessage
	if json.Unmarshal(event.Response, &resp) != nil {
		return NewStreamError("response.failed event received")
	}
	errRaw, ok := resp["error"]
	if !ok {
		return NewStreamError("response.failed event received")
	}
	var e streamError
	if json.Unmarshal(errRaw, &e) != nil {
		return NewStreamError("response.failed event received")
	}

	switch {
	case isContextWindowError(e):
		return &APIError{Kind: APIErrorContextWindowExceeded}
	case isQuotaExceededError(e):
		return &APIError{Kind: APIErrorQuotaExceeded}
	case isUsageNotIncluded(e):
		return &APIError{Kind: APIErrorUsageNotIncluded}
	case isCyberPolicyError(e):
		return &APIError{Kind: APIErrorCyberPolicy, Message: cyberPolicyMessage(e.Message)}
	case isInvalidPromptError(e):
		msg := "Invalid request."
		if e.Message != nil {
			msg = *e.Message
		}
		return &APIError{Kind: APIErrorInvalidRequest, Message: msg}
	case isServerOverloadedError(e):
		return &APIError{Kind: APIErrorServerOverloaded}
	default:
		delay := tryParseRetryAfter(e)
		msg := ""
		if e.Message != nil {
			msg = *e.Message
		}
		return NewRetryableError(msg, delay)
	}
}

// processSSE parses the SSE byte stream and emits ResponseResult values via send.
// It mirrors the Rust `process_sse`, including emitting ServerModel /
// ModelVerifications dedup logic, retaining a deferred response error, and
// stopping after `response.completed`.
func processSSE(
	ctx context.Context,
	stream client.ByteStream,
	send func(ResponseResult) bool,
	idleTimeout time.Duration,
	telemetry SSETelemetry,
) {
	events := client.SSEStream(ctx, stream, idleTimeout)
	var responseError *APIError
	var lastServerModel string

	for {
		res, ok := <-events
		start := time.Now()
		if telemetry != nil {
			telemetry.OnSSEPoll(res.Event != nil, res.Err, time.Since(start))
		}
		if !ok {
			// Channel closed without an explicit terminal item.
			err := responseError
			if err == nil {
				err = NewStreamError(streamClosedBeforeCompletedMessage)
			}
			send(ResponseResult{Err: err})
			return
		}
		if res.Err != nil {
			if res.Err.Kind == client.StreamErrorTimeout {
				send(ResponseResult{Err: NewStreamError(idleTimeoutWaitingForSSEMessage)})
				return
			}
			// The SSE helper emits "stream closed before completion" on EOF;
			// translate it to a deferred response error if one is pending.
			if isStreamClosedSentinel(res.Err) {
				err := responseError
				if err == nil {
					err = NewStreamError(streamClosedBeforeCompletedMessage)
				}
				send(ResponseResult{Err: err})
				return
			}
			send(ResponseResult{Err: NewStreamError(res.Err.Message)})
			return
		}

		var event ResponsesStreamEvent
		if json.Unmarshal([]byte(res.Event.Data), &event) != nil {
			continue
		}

		modelVerifications := event.ModelVerifications()

		if model := event.ResponseModel(); model != "" && model != lastServerModel {
			if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventServerModel, Model: model}}) {
				return
			}
			lastServerModel = model
		}
		if len(modelVerifications) > 0 {
			if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventModelVerifications, Verifications: modelVerifications}}) {
				return
			}
		}

		ev, apiErr := ProcessResponsesEvent(event)
		switch {
		case apiErr != nil:
			responseError = apiErr
		case ev != nil:
			isCompleted := ev.Kind == ResponseEventCompleted
			if !send(ResponseResult{Event: ev}) {
				return
			}
			if isCompleted {
				return
			}
		}
	}
}

// isStreamClosedSentinel reports whether a StreamError is the SSE helper's
// "stream closed before completion" sentinel.
func isStreamClosedSentinel(err *client.StreamError) bool {
	return err.Kind == client.StreamErrorStream && err.Message == "stream closed before completion"
}

var rateLimitRegex = regexp.MustCompile(`(?i)try again in\s*(\d+(?:\.\d+)?)\s*(s|ms|seconds?)`)

// tryParseRetryAfter extracts a retry delay from a rate-limit error message. It
// mirrors the Rust `try_parse_retry_after`: it only applies to
// `rate_limit_exceeded` errors and parses a "try again in N s|ms|seconds"
// phrase.
func tryParseRetryAfter(e streamError) *time.Duration {
	if e.Code == nil || *e.Code != "rate_limit_exceeded" || e.Message == nil {
		return nil
	}
	matches := rateLimitRegex.FindStringSubmatch(*e.Message)
	if matches == nil {
		return nil
	}
	value, err := parseFloat(matches[1])
	if err != nil {
		return nil
	}
	unit := strings.ToLower(matches[2])
	var d time.Duration
	switch {
	case unit == "s" || strings.HasPrefix(unit, "second"):
		d = time.Duration(value * float64(time.Second))
	case unit == "ms":
		d = time.Duration(int64(value)) * time.Millisecond
	default:
		return nil
	}
	return &d
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%g", &f)
	return f, err
}

func isContextWindowError(e streamError) bool { return codeIs(e, "context_length_exceeded") }
func isQuotaExceededError(e streamError) bool { return codeIs(e, "insufficient_quota") }
func isUsageNotIncluded(e streamError) bool   { return codeIs(e, "usage_not_included") }
func isInvalidPromptError(e streamError) bool { return codeIs(e, "invalid_prompt") }
func isCyberPolicyError(e streamError) bool   { return codeIs(e, "cyber_policy") }
func isServerOverloadedError(e streamError) bool {
	return codeIs(e, "server_is_overloaded") || codeIs(e, "slow_down")
}

func codeIs(e streamError, code string) bool {
	return e.Code != nil && *e.Code == code
}

// cyberPolicyMessage returns the cyber-policy message or the fallback when empty,
// mirroring the Rust `cyber_policy_message`.
func cyberPolicyMessage(message *string) string {
	if message != nil && strings.TrimSpace(*message) != "" {
		return *message
	}
	return cyberPolicyFallbackMessageForStream
}
