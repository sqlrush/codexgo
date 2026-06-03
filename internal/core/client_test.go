package core

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func clientStrPtr(s string) *string { return &s }

func i64ptr(v int64) *int64 { return &v }

func effortPtr(e protocol.ReasoningEffort) *protocol.ReasoningEffort { return &e }

func verbosityPtr(v protocol.Verbosity) *protocol.Verbosity { return &v }

// userMessage builds a minimal input message item for prompts.
func userMessage(text string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: text}},
	}
}

// functionTool builds a trivial function ToolSpec for request-building tests.
func functionTool(name string) tools.ToolSpec {
	return tools.ToolSpec{
		Kind: tools.ToolSpecKindFunction,
		Function: &tools.ResponsesApiTool{
			Name:        name,
			Description: "test tool",
			Strict:      false,
			Parameters:  tools.JsonSchema{Properties: map[string]tools.JsonSchema{}},
		},
	}
}

// baseModelInfo returns a model descriptor that supports neither reasoning nor
// verbosity, suitable as a baseline that individual tests can override.
func baseModelInfo(slug string) modelsmanager.ModelInfo {
	info := modelsmanager.ModelInfoFromSlug(slug)
	info.BaseInstructions = "base-instructions"
	info.SupportsReasoningSummary = false
	info.SupportVerbosity = false
	info.ContextWindow = i64ptr(123456)
	return info
}

// testAuth is the static bearer-token AuthProvider used across tests.
func testAuth() api.AuthProvider { return api.NewBearerAuth("test-token") }

// capturingTransport records the streamed request and replays a scripted SSE
// body. It implements client.HTTPTransport.
type capturingTransport struct {
	body    []byte
	headers http.Header
	url     string
	method  string
	chunks  [][]byte
}

func (t *capturingTransport) Execute(_ context.Context, _ client.Request) (client.Response, error) {
	return client.Response{}, nil
}

func (t *capturingTransport) Stream(_ context.Context, req client.Request) (client.StreamResponse, error) {
	prepared, err := req.PrepareBodyForSend()
	if err != nil {
		return client.StreamResponse{}, err
	}
	t.body = prepared.BodyBytes()
	t.headers = req.Headers.Clone()
	t.url = req.URL
	t.method = req.Method

	ch := make(chan client.ByteChunk, len(t.chunks))
	for _, c := range t.chunks {
		ch <- client.ByteChunk{Data: c}
	}
	close(ch)
	return client.StreamResponse{
		Status:  200,
		Headers: http.Header{},
		Bytes:   ch,
	}, nil
}

func sseFrame(kind, data string) []byte {
	return []byte("event: " + kind + "\ndata: " + data + "\n\n")
}

// containsKind reports whether kinds includes want.
func containsKind(kinds []api.ResponseEventKind, want api.ResponseEventKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

// countKind returns how many times want appears in kinds.
func countKind(kinds []api.ResponseEventKind, want api.ResponseEventKind) int {
	n := 0
	for _, k := range kinds {
		if k == want {
			n++
		}
	}
	return n
}

func testProvider() api.Provider {
	return api.Provider{
		Name:              "openai",
		BaseURL:           "https://example.test/v1",
		StreamIdleTimeout: 5 * time.Second,
	}
}

func newTestClient(t *testing.T, mutate func(*ModelClientConfig)) *ResponsesModelClient {
	t.Helper()
	cfg := ModelClientConfig{
		SessionID:      protocol.NewSessionID("sess-1"),
		ThreadID:       protocol.NewThreadID("thread-1"),
		InstallationID: "install-1",
		Provider:       testProvider(),
		Auth:           testAuth(),
		ModelInfo:      baseModelInfo("gpt-test"),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	c, err := NewResponsesModelClient(cfg)
	if err != nil {
		t.Fatalf("NewResponsesModelClient: %v", err)
	}
	return c
}

// ---------------------------------------------------------------------------
// Constructor validation
// ---------------------------------------------------------------------------

func TestNewResponsesModelClientValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     ModelClientConfig
		wantErr string
	}{
		{
			name:    "missing auth",
			cfg:     ModelClientConfig{Provider: testProvider()},
			wantErr: "Auth is required",
		},
		{
			name:    "missing base url",
			cfg:     ModelClientConfig{Auth: testAuth()},
			wantErr: "BaseURL is required",
		},
		{
			name: "valid",
			cfg:  ModelClientConfig{Auth: testAuth(), Provider: testProvider()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewResponsesModelClient(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// build_responses_request
// ---------------------------------------------------------------------------

func TestBuildResponsesRequest(t *testing.T) {
	t.Parallel()

	reasoningModel := func(info *modelsmanager.ModelInfo) {
		info.SupportsReasoningSummary = true
		info.DefaultReasoningLevel = effortPtr(protocol.ReasoningEffortMedium)
	}
	verbosityModel := func(info *modelsmanager.ModelInfo) {
		info.SupportVerbosity = true
		info.DefaultVerbosity = verbosityPtr(protocol.VerbosityMedium)
	}

	tests := []struct {
		name      string
		mutateCfg func(*ModelClientConfig)
		mutateMod func(*modelsmanager.ModelInfo)
		prompt    Prompt
		check     func(t *testing.T, req api.ResponsesApiRequest)
	}{
		{
			name:   "defaults: instructions from model, no reasoning, no verbosity",
			prompt: Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.Instructions != "base-instructions" {
					t.Fatalf("instructions = %q", req.Instructions)
				}
				if req.Model != "gpt-test" {
					t.Fatalf("model = %q", req.Model)
				}
				if req.Reasoning != nil {
					t.Fatalf("reasoning = %+v, want nil", req.Reasoning)
				}
				if len(req.Include) != 0 {
					t.Fatalf("include = %v, want empty", req.Include)
				}
				if req.ToolChoice != "auto" {
					t.Fatalf("tool_choice = %q", req.ToolChoice)
				}
				if !req.Stream {
					t.Fatalf("stream = false, want true")
				}
				if req.ParallelToolCalls {
					t.Fatalf("parallel_tool_calls = true, want false default")
				}
				if req.PromptCacheKey == nil || *req.PromptCacheKey != "thread-1" {
					t.Fatalf("prompt_cache_key = %v, want thread-1", req.PromptCacheKey)
				}
				if req.ClientMetadata[headerCodexInstallationID] != "install-1" {
					t.Fatalf("client_metadata install id = %v", req.ClientMetadata)
				}
				if len(req.Input) != 1 {
					t.Fatalf("input len = %d", len(req.Input))
				}
			},
		},
		{
			name: "base instructions override",
			prompt: Prompt{
				Input:                    []protocol.ResponseItem{userMessage("hi")},
				BaseInstructionsOverride: clientStrPtr("override-instructions"),
			},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.Instructions != "override-instructions" {
					t.Fatalf("instructions = %q, want override", req.Instructions)
				}
			},
		},
		{
			name:      "reasoning model uses default effort + include set",
			mutateMod: reasoningModel,
			prompt:    Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.Reasoning == nil {
					t.Fatalf("reasoning = nil, want present")
				}
				if req.Reasoning.Effort == nil || *req.Reasoning.Effort != protocol.ReasoningEffortMedium {
					t.Fatalf("reasoning effort = %v, want medium default", req.Reasoning.Effort)
				}
				if len(req.Include) != 1 || req.Include[0] != includeReasoningEncrypted {
					t.Fatalf("include = %v, want %q", req.Include, includeReasoningEncrypted)
				}
			},
		},
		{
			name:      "reasoning effort override beats model default",
			mutateMod: reasoningModel,
			mutateCfg: func(c *ModelClientConfig) {
				c.ReasoningEffort = effortPtr(protocol.ReasoningEffortHigh)
				c.ReasoningSummary = protocol.ReasoningSummaryAuto
			},
			prompt: Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.Reasoning == nil || req.Reasoning.Effort == nil || *req.Reasoning.Effort != protocol.ReasoningEffortHigh {
					t.Fatalf("reasoning effort = %v, want high", req.Reasoning)
				}
				if req.Reasoning.Summary == nil || *req.Reasoning.Summary != protocol.ReasoningSummaryAuto {
					t.Fatalf("reasoning summary = %v, want auto", req.Reasoning.Summary)
				}
			},
		},
		{
			name:      "reasoning summary none is dropped",
			mutateMod: reasoningModel,
			mutateCfg: func(c *ModelClientConfig) { c.ReasoningSummary = protocol.ReasoningSummaryNone },
			prompt:    Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.Reasoning == nil {
					t.Fatalf("reasoning = nil")
				}
				if req.Reasoning.Summary != nil {
					t.Fatalf("summary = %v, want nil for None", req.Reasoning.Summary)
				}
			},
		},
		{
			name:      "verbosity from model default when supported",
			mutateMod: verbosityModel,
			prompt:    Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.Text == nil {
					t.Fatalf("text controls = nil, want verbosity present")
				}
			},
		},
		{
			name: "verbosity override ignored when unsupported",
			mutateCfg: func(c *ModelClientConfig) {
				c.ModelVerbosity = verbosityPtr(protocol.VerbosityHigh)
			},
			prompt: Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				// Model does not support verbosity; the override is silently dropped.
				if req.Text != nil && req.Text.Verbosity != nil {
					t.Fatalf("verbosity = %v, want dropped for unsupported model", req.Text.Verbosity)
				}
			},
		},
		{
			name: "prompt cache key override",
			mutateCfg: func(c *ModelClientConfig) {
				c.PromptCacheKeyOverride = clientStrPtr("custom-key")
			},
			prompt: Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.PromptCacheKey == nil || *req.PromptCacheKey != "custom-key" {
					t.Fatalf("prompt_cache_key = %v, want custom-key", req.PromptCacheKey)
				}
			},
		},
		{
			name: "parallel tool calls flag flows through",
			mutateCfg: func(c *ModelClientConfig) {
				c.ParallelToolCalls = true
			},
			prompt: Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if !req.ParallelToolCalls {
					t.Fatalf("parallel_tool_calls = false, want true")
				}
			},
		},
		{
			name:   "tools are encoded",
			prompt: Prompt{Input: []protocol.ResponseItem{userMessage("hi")}, Tools: []tools.ToolSpec{functionTool("do_thing")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if len(req.Tools) != 1 {
					t.Fatalf("tools len = %d, want 1", len(req.Tools))
				}
				var decoded map[string]any
				if err := json.Unmarshal(req.Tools[0], &decoded); err != nil {
					t.Fatalf("tool decode: %v", err)
				}
				if decoded["name"] != "do_thing" {
					t.Fatalf("tool name = %v, want do_thing", decoded["name"])
				}
			},
		},
		{
			name: "service tier filtered to nil when unsupported",
			mutateCfg: func(c *ModelClientConfig) {
				c.ServiceTier = clientStrPtr("flex")
			},
			prompt: Prompt{Input: []protocol.ResponseItem{userMessage("hi")}},
			check: func(t *testing.T, req api.ResponsesApiRequest) {
				if req.ServiceTier != nil {
					t.Fatalf("service_tier = %v, want nil (unsupported)", req.ServiceTier)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(cfg *ModelClientConfig) {
				if tc.mutateMod != nil {
					info := cfg.ModelInfo
					tc.mutateMod(&info)
					cfg.ModelInfo = info
				}
				if tc.mutateCfg != nil {
					tc.mutateCfg(cfg)
				}
			})
			req, err := c.buildResponsesRequest(tc.prompt)
			if err != nil {
				t.Fatalf("buildResponsesRequest: %v", err)
			}
			tc.check(t, req)
		})
	}
}

func TestBuildResponsesRequestStoreOnAzure(t *testing.T) {
	t.Parallel()
	azure := newTestClient(t, func(cfg *ModelClientConfig) {
		cfg.Provider = api.Provider{
			Name:              "azure",
			BaseURL:           "https://my-resource.openai.azure.com/openai/v1",
			StreamIdleTimeout: time.Second,
		}
	})
	req, err := azure.buildResponsesRequest(Prompt{Input: []protocol.ResponseItem{userMessage("hi")}})
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if !req.Store {
		t.Fatalf("store = false on azure endpoint, want true")
	}

	nonAzure := newTestClient(t, nil)
	req2, err := nonAzure.buildResponsesRequest(Prompt{Input: []protocol.ResponseItem{userMessage("hi")}})
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if req2.Store {
		t.Fatalf("store = true on non-azure endpoint, want false")
	}
}

// ---------------------------------------------------------------------------
// Headers / routing
// ---------------------------------------------------------------------------

func TestBuildResponsesHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mutateCfg func(*ModelClientConfig)
		setup     func(*ResponsesModelClient)
		check     func(t *testing.T, h http.Header)
	}{
		{
			name: "installation id and beta marker always set",
			check: func(t *testing.T, h http.Header) {
				if h.Get(headerCodexInstallationID) != "install-1" {
					t.Fatalf("installation id = %q", h.Get(headerCodexInstallationID))
				}
				if h.Get(headerOpenAIBeta) != responsesWebsocketsV2Beta {
					t.Fatalf("beta header = %q", h.Get(headerOpenAIBeta))
				}
			},
		},
		{
			name: "beta features header set when present",
			mutateCfg: func(c *ModelClientConfig) {
				c.BetaFeaturesHeader = "feature_a,feature_b"
			},
			check: func(t *testing.T, h http.Header) {
				if h.Get(headerCodexBetaFeatures) != "feature_a,feature_b" {
					t.Fatalf("beta features = %q", h.Get(headerCodexBetaFeatures))
				}
			},
		},
		{
			name: "beta features omitted when empty",
			check: func(t *testing.T, h http.Header) {
				if _, ok := h[http.CanonicalHeaderKey(headerCodexBetaFeatures)]; ok {
					t.Fatalf("beta features header present but should be omitted")
				}
			},
		},
		{
			name: "timing metrics header set when enabled",
			mutateCfg: func(c *ModelClientConfig) {
				c.IncludeTimingMetrics = true
			},
			check: func(t *testing.T, h http.Header) {
				if h.Get(headerResponsesAPITiming) != "true" {
					t.Fatalf("timing metrics = %q", h.Get(headerResponsesAPITiming))
				}
			},
		},
		{
			name:  "turn state replayed when set",
			setup: func(c *ResponsesModelClient) { c.SetTurnState("sticky-token") },
			check: func(t *testing.T, h http.Header) {
				if h.Get(headerCodexTurnState) != "sticky-token" {
					t.Fatalf("turn state = %q", h.Get(headerCodexTurnState))
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, tc.mutateCfg)
			if tc.setup != nil {
				tc.setup(c)
			}
			tc.check(t, c.buildResponsesHeaders())
		})
	}
}

func TestExtendIdentityHeaders(t *testing.T) {
	t.Parallel()

	t.Run("window id present", func(t *testing.T) {
		c := newTestClient(t, nil)
		h := http.Header{}
		c.extendIdentityHeaders(h)
		if h.Get(headerCodexWindowID) != "thread-1:0" {
			t.Fatalf("window id = %q, want thread-1:0", h.Get(headerCodexWindowID))
		}
	})

	t.Run("subagent header for review session", func(t *testing.T) {
		src := api.NewSubAgentSession(api.SubAgentSource{Kind: api.SubAgentReview})
		c := newTestClient(t, func(cfg *ModelClientConfig) { cfg.SessionSource = &src })
		h := http.Header{}
		c.extendIdentityHeaders(h)
		if h.Get(headerOpenAISubagent) != "review" {
			t.Fatalf("subagent = %q, want review", h.Get(headerOpenAISubagent))
		}
	})

	t.Run("no subagent header for non-subagent source", func(t *testing.T) {
		c := newTestClient(t, nil)
		h := http.Header{}
		c.extendIdentityHeaders(h)
		if _, ok := h[http.CanonicalHeaderKey(headerOpenAISubagent)]; ok {
			t.Fatalf("subagent header present but should be omitted")
		}
	})
}

func TestSubagentHeaderValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  api.SessionSource
		want string
		ok   bool
	}{
		{"review", api.NewSubAgentSession(api.SubAgentSource{Kind: api.SubAgentReview}), "review", true},
		{"compact", api.NewSubAgentSession(api.SubAgentSource{Kind: api.SubAgentCompact}), "compact", true},
		{"memory", api.NewSubAgentSession(api.SubAgentSource{Kind: api.SubAgentMemoryConsolidation}), "memory_consolidation", true},
		{"thread spawn", api.NewSubAgentSession(api.SubAgentSource{Kind: api.SubAgentThreadSpawn}), "collab_spawn", true},
		{"other", api.NewSubAgentSession(api.SubAgentSource{Kind: api.SubAgentOther, Label: "custom-label"}), "custom-label", true},
		{"non-subagent", api.SessionSource{IsSubAgent: false}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src
			c := newTestClient(t, func(cfg *ModelClientConfig) { cfg.SessionSource = &src })
			got, ok := c.subagentHeaderValue()
			if ok != tc.ok || got != tc.want {
				t.Fatalf("subagentHeaderValue = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Window generation + turn state + cache key
// ---------------------------------------------------------------------------

func TestWindowGeneration(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, nil)
	if got := c.currentWindowID(); got != "thread-1:0" {
		t.Fatalf("initial window id = %q, want thread-1:0", got)
	}
	c.AdvanceWindowGeneration()
	if got := c.currentWindowID(); got != "thread-1:1" {
		t.Fatalf("after advance window id = %q, want thread-1:1", got)
	}
	c.SetWindowGeneration(42)
	if got := c.currentWindowID(); got != "thread-1:42" {
		t.Fatalf("after set window id = %q, want thread-1:42", got)
	}
}

func TestPromptCacheKey(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, nil)
	if got := c.promptCacheKey(); got != "thread-1" {
		t.Fatalf("default cache key = %q, want thread-1", got)
	}
	c2 := newTestClient(t, func(cfg *ModelClientConfig) {
		cfg.PromptCacheKeyOverride = clientStrPtr("override-key")
	})
	if got := c2.promptCacheKey(); got != "override-key" {
		t.Fatalf("override cache key = %q, want override-key", got)
	}
}

func TestModelMetadataAccessors(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, nil)
	if c.ModelSlug() != "gpt-test" {
		t.Fatalf("ModelSlug = %q", c.ModelSlug())
	}
	cw := c.ContextWindow()
	if cw == nil || *cw != 123456 {
		t.Fatalf("ContextWindow = %v, want 123456", cw)
	}
}

// ---------------------------------------------------------------------------
// End-to-end stream through a fake transport
// ---------------------------------------------------------------------------

func TestStreamEndToEnd(t *testing.T) {
	t.Parallel()
	itemDone := `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}}`
	completed := `{"type":"response.completed","response":{"id":"resp-xyz"}}`
	transport := &capturingTransport{
		chunks: [][]byte{
			sseFrame("response.output_item.done", itemDone),
			sseFrame("response.completed", completed),
		},
	}
	c := newTestClient(t, func(cfg *ModelClientConfig) {
		cfg.Transport = transport
		cfg.BetaFeaturesHeader = "feat"
	})

	events, err := c.Stream(context.Background(), Prompt{Input: []protocol.ResponseItem{userMessage("hi")}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var kinds []api.ResponseEventKind
	for ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	// The api layer may prepend metadata events (e.g. rate-limit snapshots
	// parsed from headers) ahead of the SSE data events. The client must
	// forward every event the api layer produces, in order, so we assert on the
	// meaningful subsequence rather than an exact count: the output item is
	// delivered, and the stream is terminated by a single Completed event.
	if len(kinds) == 0 {
		t.Fatalf("no events forwarded")
	}
	if kinds[len(kinds)-1] != api.ResponseEventCompleted {
		t.Fatalf("last event = %v, want Completed; full = %v", kinds[len(kinds)-1], kinds)
	}
	if !containsKind(kinds, api.ResponseEventOutputItemDone) {
		t.Fatalf("missing OutputItemDone in forwarded events: %v", kinds)
	}
	// Exactly one terminal Completed event is forwarded.
	if n := countKind(kinds, api.ResponseEventCompleted); n != 1 {
		t.Fatalf("Completed events = %d, want 1; full = %v", n, kinds)
	}

	// Verify the request body the engine built was streamed to the transport.
	if transport.body == nil {
		t.Fatalf("transport captured no body")
	}
	var sent api.ResponsesApiRequest
	if err := json.Unmarshal(transport.body, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Model != "gpt-test" || !sent.Stream {
		t.Fatalf("sent request mismatch: model=%q stream=%v", sent.Model, sent.Stream)
	}
	if transport.headers.Get(headerCodexBetaFeatures) != "feat" {
		t.Fatalf("beta features header not sent: %q", transport.headers.Get(headerCodexBetaFeatures))
	}
	if transport.headers.Get(headerCodexInstallationID) != "install-1" {
		t.Fatalf("installation id header not sent: %q", transport.headers.Get(headerCodexInstallationID))
	}
	if transport.headers.Get(headerCodexWindowID) != "thread-1:0" {
		t.Fatalf("window id header not sent: %q", transport.headers.Get(headerCodexWindowID))
	}
}

func TestStreamWithErrorsTerminalError(t *testing.T) {
	t.Parallel()
	// A failed response (rate limit) should surface as a terminal StreamItem.Err.
	failed := `{"type":"response.failed","response":{"id":"r","error":{"code":"rate_limit_exceeded","message":"Rate limit reached. Please try again in 1s."}}}`
	transport := &capturingTransport{
		chunks: [][]byte{sseFrame("response.failed", failed)},
	}
	c := newTestClient(t, func(cfg *ModelClientConfig) { cfg.Transport = transport })

	items, err := c.StreamWithErrors(context.Background(), Prompt{Input: []protocol.ResponseItem{userMessage("hi")}})
	if err != nil {
		t.Fatalf("StreamWithErrors: %v", err)
	}
	var sawErr bool
	for it := range items {
		if it.Err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("expected a terminal error item from a failed response")
	}
}

// ---------------------------------------------------------------------------
// MockModelClient
// ---------------------------------------------------------------------------

func completedEvent(id string) api.ResponseEvent {
	return api.ResponseEvent{Kind: api.ResponseEventCompleted, ResponseID: id}
}

func TestMockModelClientReplay(t *testing.T) {
	t.Parallel()
	cw := i64ptr(8192)
	mock := NewMockModelClient("mock-slug", cw,
		MockTurn{Events: []api.ResponseEvent{
			{Kind: api.ResponseEventOutputTextDelta, Delta: "a"},
			completedEvent("turn-1"),
		}},
		MockTurn{Events: []api.ResponseEvent{completedEvent("turn-2")}},
	)

	if mock.ModelSlug() != "mock-slug" {
		t.Fatalf("ModelSlug = %q", mock.ModelSlug())
	}
	if got := mock.ContextWindow(); got == nil || *got != 8192 {
		t.Fatalf("ContextWindow = %v", got)
	}

	// First turn.
	ch, err := mock.Stream(context.Background(), Prompt{Input: []protocol.ResponseItem{userMessage("one")}})
	if err != nil {
		t.Fatalf("Stream turn 1: %v", err)
	}
	var got1 []api.ResponseEventKind
	for ev := range ch {
		got1 = append(got1, ev.Kind)
	}
	if len(got1) != 2 || got1[1] != api.ResponseEventCompleted {
		t.Fatalf("turn 1 events = %v", got1)
	}

	// Second turn.
	ch2, err := mock.Stream(context.Background(), Prompt{Input: []protocol.ResponseItem{userMessage("two")}})
	if err != nil {
		t.Fatalf("Stream turn 2: %v", err)
	}
	count := 0
	for range ch2 {
		count++
	}
	if count != 1 {
		t.Fatalf("turn 2 events count = %d, want 1", count)
	}

	// Exhausted script returns an error.
	if _, err := mock.Stream(context.Background(), Prompt{}); err == nil {
		t.Fatalf("expected error after script exhausted")
	}

	// Prompts recorded in order (3 calls, last one is the exhausted call).
	prompts := mock.ReceivedPrompts()
	if len(prompts) != 3 {
		t.Fatalf("recorded prompts = %d, want 3", len(prompts))
	}
	if mock.CallCount() != 3 {
		t.Fatalf("CallCount = %d, want 3", mock.CallCount())
	}
	last, ok := mock.LastPrompt()
	if !ok || len(last.Input) != 0 {
		t.Fatalf("LastPrompt = (%+v, %v)", last, ok)
	}
}

func TestMockModelClientStreamError(t *testing.T) {
	t.Parallel()
	wantErr := context.DeadlineExceeded
	mock := NewMockModelClient("m", nil, MockTurn{StreamErr: wantErr})
	_, err := mock.Stream(context.Background(), Prompt{})
	if err != wantErr {
		t.Fatalf("Stream err = %v, want %v", err, wantErr)
	}
}

func TestMockModelClientStreamWithErrors(t *testing.T) {
	t.Parallel()
	wantErr := context.DeadlineExceeded
	mock := NewMockModelClient("m", nil,
		MockTurn{
			Events:    []api.ResponseEvent{{Kind: api.ResponseEventOutputTextDelta, Delta: "x"}},
			StreamErr: wantErr,
		},
	)
	items, err := mock.StreamWithErrors(context.Background(), Prompt{Input: []protocol.ResponseItem{userMessage("hi")}})
	if err != nil {
		t.Fatalf("StreamWithErrors: %v", err)
	}
	var events int
	var sawErr error
	for it := range items {
		if it.Event != nil {
			events++
		}
		if it.Err != nil {
			sawErr = it.Err
		}
	}
	if events != 1 {
		t.Fatalf("events = %d, want 1", events)
	}
	if sawErr != wantErr {
		t.Fatalf("terminal err = %v, want %v", sawErr, wantErr)
	}
}

func TestMockModelClientReset(t *testing.T) {
	t.Parallel()
	mock := NewMockModelClient("m", nil, MockTurn{Events: []api.ResponseEvent{completedEvent("t1")}})
	if _, err := mock.Stream(context.Background(), Prompt{}); err != nil {
		t.Fatalf("first stream: %v", err)
	}
	mock.Reset()
	if mock.CallCount() != 0 {
		t.Fatalf("CallCount after reset = %d, want 0", mock.CallCount())
	}
	// Script is replayable after reset.
	if _, err := mock.Stream(context.Background(), Prompt{}); err != nil {
		t.Fatalf("stream after reset: %v", err)
	}
}

func TestMockModelClientContextCancellation(t *testing.T) {
	t.Parallel()
	mock := NewMockModelClient("m", nil, MockTurn{Events: []api.ResponseEvent{
		completedEvent("a"), completedEvent("b"), completedEvent("c"),
	}})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := mock.Stream(ctx, Prompt{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	cancel()
	// Drain; the producer must respect cancellation and close the channel.
	for range ch {
	}
}

// compile-time assertions on the local interfaces the turn runner relies on.
var (
	_ ModelClient = (*ResponsesModelClient)(nil)
	_ ModelClient = (*MockModelClient)(nil)
)
