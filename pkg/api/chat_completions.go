package api

// OpenAI-compatible chat-completions streaming client.
//
// This is a codexgo EXTENSION: upstream codex 0.136 removed the chat wire
// protocol entirely (wire_api = "chat" became a migration error), but codexgo
// deliberately supports third-party OpenAI-compatible backends (GLM, DeepSeek,
// Kimi, local vLLM/Ollama gateways, …) that expose only /chat/completions.
// See DEVIATIONS.md "wire_api chat".
//
// The stream parser aggregates chat chunk deltas and re-emits them as the SAME
// api.ResponseEvent sequence the Responses SSE layer produces, so the core
// turn runner consumes both wire protocols identically:
//
//	Created
//	→ (OutputItemAdded(reasoning) → ReasoningContentDelta*)        [optional]
//	→ (OutputItemAdded(message)  → OutputTextDelta* → OutputItemDone(message))
//	→ (OutputItemAdded(function_call) → OutputItemDone(function_call))*
//	→ Completed{EndTurn: no tool calls}
//
// Vendor extensions handled: `reasoning_content` deltas (GLM / DeepSeek
// reasoning models) stream as ReasoningContentDelta but are NOT persisted as a
// reasoning history item (chat backends do not accept reasoning items back).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/pkg/client"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// chatCompletionsPath is the endpoint path joined onto the provider base URL.
const chatCompletionsPath = "chat/completions"

// ChatMessage is one chat-completions message.
type ChatMessage struct {
	Role string `json:"role"`
	// Content is a plain string (or null for assistant tool-call messages).
	Content *string `json:"content"`
	// ToolCalls is set on assistant messages that invoked tools.
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
	// ToolCallID correlates a role:"tool" result with its call.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ChatToolCall is an assistant-invoked tool call.
type ChatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"` // always "function"
	Function ChatToolCallFunction `json:"function"`
}

// ChatToolCallFunction carries the called function name and JSON arguments.
type ChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionsRequest is the request body for POST /chat/completions.
type ChatCompletionsRequest struct {
	Model      string            `json:"model"`
	Messages   []ChatMessage     `json:"messages"`
	Tools      []json.RawMessage `json:"tools,omitempty"`
	ToolChoice string            `json:"tool_choice,omitempty"`
	Stream     bool              `json:"stream"`
}

// ChatCompletionsClient streams chat-completions requests for one provider.
type ChatCompletionsClient struct {
	session *endpointSession
}

// NewChatCompletionsClient builds a client over a transport, provider, and auth
// source (the same trio the Responses client takes).
func NewChatCompletionsClient(transport client.HTTPTransport, provider Provider, auth AuthProvider) *ChatCompletionsClient {
	return &ChatCompletionsClient{session: newEndpointSession(transport, provider, auth)}
}

// StreamRequest issues one streaming chat-completions request and returns the
// aggregated ResponseStream.
func (c *ChatCompletionsClient) StreamRequest(ctx context.Context, request ChatCompletionsRequest, extraHeaders http.Header) (ResponseStream, *APIError) {
	body, err := json.Marshal(request)
	if err != nil {
		return ResponseStream{}, NewStreamError(fmt.Sprintf("failed to encode chat request: %v", err))
	}

	streamResponse, apiErr := c.session.streamWith(
		ctx,
		http.MethodPost,
		chatCompletionsPath,
		cloneHeader(extraHeaders),
		body,
		func(req *client.Request) {
			req.Headers.Set("Accept", "text/event-stream")
		},
	)
	if apiErr != nil {
		return ResponseStream{}, apiErr
	}

	return spawnChatCompletionsStream(ctx, streamResponse, c.session.provider.StreamIdleTimeout), nil
}

// chatChunk is one streamed chat.completion.chunk payload.
type chatChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Role             string  `json:"role"`
			Content          *string `json:"content"`
			ReasoningContent *string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function *struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage"`
}

// chatUsage is the (optionally streamed) token accounting block.
type chatUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// toTokenUsage converts chat usage to the protocol TokenUsage shape.
func (u chatUsage) toTokenUsage() protocol.TokenUsage {
	var cached int64
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	return protocol.TokenUsage{
		InputTokens:       u.PromptTokens,
		CachedInputTokens: cached,
		OutputTokens:      u.CompletionTokens,
		TotalTokens:       u.TotalTokens,
	}
}

// chatToolCallAcc accumulates one tool call's streamed fragments.
type chatToolCallAcc struct {
	id   string
	name string
	args strings.Builder
}

// spawnChatCompletionsStream parses the chat SSE body and re-emits the
// aggregated ResponseEvent sequence documented in the file header.
func spawnChatCompletionsStream(ctx context.Context, streamResponse client.StreamResponse, idleTimeout time.Duration) ResponseStream {
	var upstreamRequestID *string
	if v := streamResponse.Headers.Get(requestIDHeader); v != "" {
		id := v
		upstreamRequestID = &id
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

		if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventCreated}}) {
			return
		}

		var (
			responseID     string
			text           strings.Builder
			messageAdded   bool
			reasoningAdded bool
			toolCalls      = map[int]*chatToolCallAcc{}
			usage          *protocol.TokenUsage
			sawFinish      bool
			sawDone        bool
		)

		// finalize flushes the aggregated items and the Completed sentinel.
		finalize := func() {
			if messageAdded {
				full := text.String()
				item := protocol.ResponseItem{
					Type:    protocol.ResponseItemKindMessage,
					Role:    "assistant",
					Content: []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: full}},
				}
				if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventOutputItemDone, Item: &item}}) {
					return
				}
			}
			indices := make([]int, 0, len(toolCalls))
			for idx := range toolCalls {
				indices = append(indices, idx)
			}
			sort.Ints(indices)
			for n, idx := range indices {
				acc := toolCalls[idx]
				callID := acc.id
				if callID == "" {
					callID = fmt.Sprintf("call_%d", n)
				}
				item := protocol.ResponseItem{
					Type:      protocol.ResponseItemKindFunctionCall,
					Name:      acc.name,
					Arguments: acc.args.String(),
					CallID:    callID,
				}
				added := item
				if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventOutputItemAdded, Item: &added}}) {
					return
				}
				if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventOutputItemDone, Item: &item}}) {
					return
				}
			}
			endTurn := len(toolCalls) == 0
			send(ResponseResult{Event: &ResponseEvent{
				Kind:       ResponseEventCompleted,
				ResponseID: responseID,
				TokenUsage: usage,
				EndTurn:    &endTurn,
			}})
		}

		events := client.SSEStream(ctx, streamResponse.Bytes, idleTimeout)
		for res := range events {
			if res.Err != nil {
				if res.Err.Kind == client.StreamErrorTimeout {
					send(ResponseResult{Err: NewStreamError(idleTimeoutWaitingForSSEMessage)})
					return
				}
				// EOF: some vendors close without a [DONE] sentinel. Treat the
				// close as terminal success when the model already finished.
				if isStreamClosedSentinel(res.Err) && (sawDone || sawFinish) {
					finalize()
					return
				}
				send(ResponseResult{Err: NewStreamError(res.Err.Message)})
				return
			}
			data := strings.TrimSpace(res.Event.Data)
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				sawDone = true
				finalize()
				return
			}

			var chunk chatChunk
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}
			if chunk.ID != "" {
				responseID = chunk.ID
			}
			if chunk.Usage != nil {
				u := chunk.Usage.toTokenUsage()
				usage = &u
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				sawFinish = true
			}

			if rc := choice.Delta.ReasoningContent; rc != nil && *rc != "" {
				if !reasoningAdded {
					reasoningAdded = true
					skeleton := protocol.ResponseItem{
						Type:    protocol.ResponseItemKindReasoning,
						Summary: []protocol.ReasoningItemReasoningSummary{},
					}
					if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventOutputItemAdded, Item: &skeleton}}) {
						return
					}
				}
				if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventReasoningContentDelta, Delta: *rc, ContentIndex: 0}}) {
					return
				}
			}

			if content := choice.Delta.Content; content != nil && *content != "" {
				if !messageAdded {
					messageAdded = true
					skeleton := protocol.ResponseItem{
						Type:    protocol.ResponseItemKindMessage,
						Role:    "assistant",
						Content: []protocol.ContentItem{},
					}
					if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventOutputItemAdded, Item: &skeleton}}) {
						return
					}
				}
				text.WriteString(*content)
				if !send(ResponseResult{Event: &ResponseEvent{Kind: ResponseEventOutputTextDelta, Delta: *content}}) {
					return
				}
			}

			for _, tc := range choice.Delta.ToolCalls {
				acc, ok := toolCalls[tc.Index]
				if !ok {
					acc = &chatToolCallAcc{}
					toolCalls[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function != nil {
					if tc.Function.Name != "" {
						acc.name += tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						acc.args.WriteString(tc.Function.Arguments)
					}
				}
			}
		}

		// Channel closed without DONE/EOF sentinel: finalize when finished,
		// otherwise report the truncation like the Responses parser does.
		if sawDone || sawFinish {
			finalize()
			return
		}
		send(ResponseResult{Err: NewStreamError(streamClosedBeforeCompletedMessage)})
	}()

	return ResponseStream{Events: out, UpstreamRequestID: upstreamRequestID}
}
