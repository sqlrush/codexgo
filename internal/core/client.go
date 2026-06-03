package core

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports codex-core's client.rs: the ModelClient that bridges the
// engine to internal/api's Responses streaming layer. It builds the Responses
// request from the conversation history + tools + instructions carried on a
// [Prompt], attaches turn-metadata / routing headers, streams events, and maps
// api.ResponseEvent values onto the engine-facing channel.
//
// The exported [ResponsesModelClient] satisfies the foundation [ModelClient]
// interface declared in managers.go. A [MockModelClient] replays scripted
// responses for tests.
//
// STUB (deferred from client.rs): the Responses-over-WebSocket transport
// (stream_responses_websocket / prewarm / sticky-routing reconnect), the
// HTTP<->WS fallback state machine, attestation headers, window-generation
// tracking + advance, unauthorized auth-recovery retry loops, compaction
// (/responses/compact) and memories (/memories/trace_summarize) endpoints,
// realtime call setup, request-compression negotiation, incremental WS input
// deltas, and OpenTelemetry inference-trace spans. The synchronous HTTP
// Responses path (stream_responses_api) is implemented faithfully.

// Header names used on Responses requests, mirroring the Rust client.rs
// constants. Only the turn-running subset is implemented; see the file STUB note.
const (
	headerOpenAIBeta             = "OpenAI-Beta"
	headerCodexInstallationID    = "x-codex-installation-id"
	headerCodexTurnState         = "x-codex-turn-state"
	headerCodexTurnMetadata      = "x-codex-turn-metadata"
	headerCodexParentThreadID    = "x-codex-parent-thread-id"
	headerCodexWindowID          = "x-codex-window-id"
	headerOpenAISubagent         = "x-openai-subagent"
	headerCodexBetaFeatures      = "x-codex-beta-features"
	headerResponsesAPITiming     = "x-responsesapi-include-timing-metrics"
	headerClientRequestID        = "x-client-request-id"
	responsesWebsocketsV2Beta    = "responses_websockets=2026-02-06"
	includeReasoningEncrypted    = "reasoning.encrypted_content"
	clientMetadataInstallationID = headerCodexInstallationID
)

// ModelClientConfig is the session-scoped, immutable configuration for a
// [ResponsesModelClient]. It is the Go analogue of the stable fields the Rust
// `ModelClientState` carries (the per-turn values arrive through the [Prompt]
// and the configured [TurnContext]).
//
// All fields are treated as read-only after construction; the client never
// mutates the config (immutability contract).
type ModelClientConfig struct {
	// SessionID / ThreadID are the stable identities reported in routing headers.
	SessionID protocol.SessionID
	ThreadID  protocol.ThreadID
	// InstallationID is the stable installation identifier sent in the
	// x-codex-installation-id header and client metadata.
	InstallationID string

	// Provider is the resolved HTTP endpoint configuration.
	Provider api.Provider
	// Auth applies authentication to outbound requests.
	Auth api.AuthProvider
	// Transport executes requests; when nil a default *http.Client transport is
	// constructed lazily.
	Transport client.HTTPTransport

	// ModelInfo is the resolved model metadata for the targeted model. It drives
	// the slug, reasoning support, verbosity support, and service-tier filtering.
	ModelInfo modelsmanager.ModelInfo

	// ReasoningEffort / ReasoningSummary configure the reasoning block. Effort is
	// optional (nil falls back to the model default).
	ReasoningEffort  *protocol.ReasoningEffort
	ReasoningSummary protocol.ReasoningSummary
	// ServiceTier is the optional requested service tier (filtered by ModelInfo).
	ServiceTier *string

	// ModelVerbosity overrides the model's default verbosity when supported.
	ModelVerbosity *protocol.Verbosity

	// SessionSource influences the subagent / parent-thread routing headers.
	SessionSource *api.SessionSource

	// BetaFeaturesHeader is the comma-separated beta feature list, if any.
	BetaFeaturesHeader string
	// IncludeTimingMetrics requests server-side timing metrics.
	IncludeTimingMetrics bool

	// PromptCacheKeyOverride overrides the default prompt cache key (the thread
	// id). Mirrors with_prompt_cache_key_override.
	PromptCacheKeyOverride *string

	// ParallelToolCalls permits parallel tool calls for requests. Mirrors the
	// Rust Prompt.parallel_tool_calls (defaults to false).
	ParallelToolCalls bool
	// OutputSchemaStrict toggles strict JSON-schema validation of the output
	// schema. Mirrors the Rust Prompt.output_schema_strict (defaults to true).
	OutputSchemaStrict bool

	// RequestTelemetry / SSETelemetry are optional observability hooks forwarded
	// to the api client.
	RequestTelemetry client.RequestTelemetry
	SSETelemetry     api.SSETelemetry
}

// ResponsesModelClient is the concrete [ModelClient] that streams a turn over
// the HTTP Responses API. It is the turn-running port of the Rust `ModelClient`
// / `ModelClientSession`, holding session-scoped state and exposing the
// engine-facing Stream method.
//
// A ResponsesModelClient is safe for concurrent use; the only mutable state is
// the atomic window generation and the sticky-routing turn-state token guarded
// by turnStateMu.
type ResponsesModelClient struct {
	cfg ModelClientConfig

	// windowGeneration backs the x-codex-window-id header, mirroring the Rust
	// AtomicU64 window_generation.
	windowGeneration atomic.Uint64

	// turnStateMu guards turnState, the sticky-routing x-codex-turn-state token
	// captured from a prior request in the same turn (set via SetTurnState).
	turnStateMu sync.Mutex
	turnState   string

	// responsesClient is the lazily-built api streaming client.
	once            sync.Once
	responsesClient *api.ResponsesClient
	buildErr        error
}

// compile-time assertion that ResponsesModelClient satisfies the foundation
// ModelClient interface.
var _ ModelClient = (*ResponsesModelClient)(nil)

// NewResponsesModelClient constructs a session-scoped ResponsesModelClient. The
// returned client does not perform any network I/O until Stream is called.
//
// Required: cfg.Provider must be populated and cfg.Auth non-nil. When
// cfg.Transport is nil a default transport over http.DefaultClient is used.
func NewResponsesModelClient(cfg ModelClientConfig) (*ResponsesModelClient, error) {
	if cfg.Auth == nil {
		return nil, fmt.Errorf("core: ModelClientConfig.Auth is required")
	}
	if cfg.Provider.BaseURL == "" {
		return nil, fmt.Errorf("core: ModelClientConfig.Provider.BaseURL is required")
	}
	return &ResponsesModelClient{cfg: cfg}, nil
}

// ModelSlug returns the canonical slug of the targeted model.
func (c *ResponsesModelClient) ModelSlug() string { return c.cfg.ModelInfo.Slug }

// ContextWindow reports the model's effective context window in tokens, if known.
func (c *ResponsesModelClient) ContextWindow() *int64 { return c.cfg.ModelInfo.ContextWindow }

// SetWindowGeneration sets the window generation used in the x-codex-window-id
// header, mirroring set_window_generation (without the cached-websocket reset,
// which is part of the deferred WS transport).
func (c *ResponsesModelClient) SetWindowGeneration(gen uint64) {
	c.windowGeneration.Store(gen)
}

// AdvanceWindowGeneration increments the window generation, mirroring
// advance_window_generation.
func (c *ResponsesModelClient) AdvanceWindowGeneration() {
	c.windowGeneration.Add(1)
}

// currentWindowID returns the "<thread_id>:<generation>" window id, mirroring
// current_window_id.
func (c *ResponsesModelClient) currentWindowID() string {
	return fmt.Sprintf("%s:%d", c.cfg.ThreadID.String(), c.windowGeneration.Load())
}

// SetTurnState records the sticky-routing token captured from a prior request
// in this turn. Subsequent requests replay it via the x-codex-turn-state header.
func (c *ResponsesModelClient) SetTurnState(state string) {
	c.turnStateMu.Lock()
	c.turnState = state
	c.turnStateMu.Unlock()
}

// currentTurnState returns the captured sticky-routing token, if any.
func (c *ResponsesModelClient) currentTurnState() string {
	c.turnStateMu.Lock()
	defer c.turnStateMu.Unlock()
	return c.turnState
}

// promptCacheKey returns the request prompt cache key, mirroring prompt_cache_key
// (override, else the thread id).
func (c *ResponsesModelClient) promptCacheKey() string {
	if c.cfg.PromptCacheKeyOverride != nil {
		return *c.cfg.PromptCacheKeyOverride
	}
	return c.cfg.ThreadID.String()
}

// Stream issues a single Responses API request built from prompt and yields its
// response events on the returned channel. It mirrors the HTTP path of the Rust
// `ModelClientSession::stream` -> `stream_responses_api`.
//
// The returned channel is closed when the stream terminates. Cancellation is
// driven through ctx. A terminal stream error is surfaced as an error event by
// the api layer; transport/build failures are returned directly.
func (c *ResponsesModelClient) Stream(ctx context.Context, prompt Prompt) (<-chan api.ResponseEvent, error) {
	apiClient, err := c.client()
	if err != nil {
		return nil, err
	}

	request, err := c.buildResponsesRequest(prompt)
	if err != nil {
		return nil, fmt.Errorf("core: build responses request: %w", err)
	}

	options := c.buildResponsesOptions()

	stream, apiErr := apiClient.StreamRequest(ctx, request, options)
	if apiErr != nil {
		return nil, fmt.Errorf("core: responses stream request: %w", apiErr)
	}

	out := make(chan api.ResponseEvent, streamForwardBuffer)
	go forwardResponseStream(ctx, stream, out)
	return out, nil
}

// streamForwardBuffer bounds the engine-facing event channel, matching the
// api-side RESPONSE_STREAM_CHANNEL_CAPACITY.
const streamForwardBuffer = 1600

// forwardResponseStream copies events from an api.ResponseStream onto the
// engine-facing channel, honoring ctx, and closes out when the source is
// drained. A stream error is forwarded as a Completed sentinel is NOT
// synthesized here; instead the error is dropped after best-effort logging,
// matching the foundation interface which carries only api.ResponseEvent values.
//
// STUB: the foundation ModelClient.Stream signature yields only
// api.ResponseEvent (no per-item error). Terminal stream errors from the api
// layer are surfaced by closing the channel early; richer error propagation is
// deferred to a future signature change that area agents may introduce.
func forwardResponseStream(ctx context.Context, stream api.ResponseStream, out chan<- api.ResponseEvent) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case res, ok := <-stream.Events:
			if !ok {
				return
			}
			if res.Err != nil {
				// Terminal error: stop forwarding. Closing out signals the
				// consumer that the stream ended.
				return
			}
			if res.Event == nil {
				continue
			}
			select {
			case out <- *res.Event:
			case <-ctx.Done():
				return
			}
		}
	}
}

// StreamWithErrors is an error-aware variant of Stream that preserves the
// api-layer error channel. It is the recommended entry point for the turn
// runner, which needs to observe terminal stream errors. The returned channel
// yields [StreamItem] values and is closed when the stream terminates.
func (c *ResponsesModelClient) StreamWithErrors(ctx context.Context, prompt Prompt) (<-chan StreamItem, error) {
	apiClient, err := c.client()
	if err != nil {
		return nil, err
	}
	request, err := c.buildResponsesRequest(prompt)
	if err != nil {
		return nil, fmt.Errorf("core: build responses request: %w", err)
	}
	options := c.buildResponsesOptions()
	stream, apiErr := apiClient.StreamRequest(ctx, request, options)
	if apiErr != nil {
		return nil, fmt.Errorf("core: responses stream request: %w", apiErr)
	}
	out := make(chan StreamItem, streamForwardBuffer)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case res, ok := <-stream.Events:
				if !ok {
					return
				}
				item := StreamItem{}
				if res.Err != nil {
					item.Err = fmt.Errorf("core: responses stream: %w", res.Err)
				} else if res.Event != nil {
					ev := *res.Event
					item.Event = &ev
				} else {
					continue
				}
				select {
				case out <- item:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// StreamItem is one item from [ResponsesModelClient.StreamWithErrors]: an event
// or a terminal error.
type StreamItem struct {
	Event *api.ResponseEvent
	Err   error
}

// client lazily builds and caches the api streaming client.
func (c *ResponsesModelClient) client() (*api.ResponsesClient, error) {
	c.once.Do(func() {
		transport := c.cfg.Transport
		if transport == nil {
			transport = client.NewHTTPClientTransport(http.DefaultClient)
		}
		ac := api.NewResponsesClient(transport, c.cfg.Provider, c.cfg.Auth)
		if c.cfg.RequestTelemetry != nil || c.cfg.SSETelemetry != nil {
			ac = ac.WithTelemetry(c.cfg.RequestTelemetry, c.cfg.SSETelemetry)
		}
		c.responsesClient = ac
	})
	return c.responsesClient, c.buildErr
}
