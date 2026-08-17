package mcpserver

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// captureWriter records every outgoing frame for assertions. It is safe for
// concurrent use, matching the frameWriter contract.
type captureWriter struct {
	mu     sync.Mutex
	frames []map[string]json.RawMessage
}

func (c *captureWriter) WriteFrame(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	c.mu.Lock()
	c.frames = append(c.frames, obj)
	c.mu.Unlock()
	return nil
}

func (c *captureWriter) snapshot() []map[string]json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]map[string]json.RawMessage, len(c.frames))
	copy(out, c.frames)
	return out
}

// responseFor returns the result object of the response/error frame for id.
func (c *captureWriter) responseFor(t *testing.T, id int64) (result map[string]json.RawMessage, errObj map[string]json.RawMessage, found bool) {
	t.Helper()
	for _, f := range c.snapshot() {
		raw, ok := f["id"]
		if !ok {
			continue
		}
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil || n != id {
			continue
		}
		if eb, ok := f["error"]; ok {
			var e map[string]json.RawMessage
			_ = json.Unmarshal(eb, &e)
			return nil, e, true
		}
		if rb, ok := f["result"]; ok {
			var r map[string]json.RawMessage
			_ = json.Unmarshal(rb, &r)
			return r, nil, true
		}
	}
	return nil, nil, false
}

// notificationsByMethod returns every notification frame with the given method.
func (c *captureWriter) notificationsByMethod(method string) []map[string]json.RawMessage {
	var out []map[string]json.RawMessage
	for _, f := range c.snapshot() {
		if _, hasID := f["id"]; hasID {
			continue
		}
		m, ok := f["method"]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(m, &s); err != nil || s != method {
			continue
		}
		out = append(out, f)
	}
	return out
}

// serverRequestsByMethod returns every server->client request frame (id+method)
// with the given method.
func (c *captureWriter) serverRequestsByMethod(method string) []map[string]json.RawMessage {
	var out []map[string]json.RawMessage
	for _, f := range c.snapshot() {
		if _, hasID := f["id"]; !hasID {
			continue
		}
		if _, hasResult := f["result"]; hasResult {
			continue
		}
		if _, hasErr := f["error"]; hasErr {
			continue
		}
		m, ok := f["method"]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(m, &s); err != nil || s != method {
			continue
		}
		out = append(out, f)
	}
	return out
}

// mockClientFactory hands out a fresh MockModelClient replaying the given turns
// for every spawned thread.
func mockClientFactory(turns ...core.MockTurn) appserver.ModelClientFactory {
	return func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
		slug := cfg.Model()
		if slug == "" {
			slug = "gpt-test"
		}
		return core.NewMockModelClient(slug, nil, turns...), nil
	}
}

// completedTurn builds a one-shot scripted assistant turn streaming a single
// message and ending the turn.
func completedTurn(text string) core.MockTurn {
	mid := "m1"
	return core.MockTurn{Events: []api.ResponseEvent{
		{Kind: api.ResponseEventCreated},
		{
			Kind: api.ResponseEventOutputItemDone,
			Item: &protocol.ResponseItem{
				Type:      protocol.ResponseItemKindMessage,
				Role:      "assistant",
				MessageID: &mid,
				Content:   []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
			},
		},
		{Kind: api.ResponseEventCompleted, EndTurn: boolPtr(true)},
	}}
}

func boolPtr(b bool) *bool { return &b }

// newTestProcessor builds a MessageProcessor over a minimal assembly wired to a
// capture writer.
func newTestProcessor(t *testing.T, turns ...core.MockTurn) (*MessageProcessor, *captureWriter) {
	t.Helper()
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: mockClientFactory(turns...),
		CodexHome:          "/home/.codex",
		DefaultModel:       "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	w := &captureWriter{}
	proc := NewMessageProcessor(ProcessorConfig{
		Assembly:      asm,
		Defaults:      appserver.Defaults{Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "codex-mcp/0.0.0-test"},
		Writer:        w,
		ServerVersion: "0.0.0-test",
		UserAgent:     "codex-mcp/0.0.0-test",
	})
	return proc, w
}

// request builds an incomingMessage for a client request.
func request(id int64, method string, params any) incomingMessage {
	mid := NewIntRequestID(id)
	return incomingMessage{
		JSONRPC: jsonRPCVersion,
		ID:      &mid,
		Method:  method,
		Params:  mustMarshal(params),
	}
}

// notification builds an incomingMessage for a client notification.
func notification(method string, params any) incomingMessage {
	return incomingMessage{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  mustMarshal(params),
	}
}

func mustMarshal(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// initialize drives the MCP initialize handshake on proc.
func initialize(t *testing.T, proc *MessageProcessor) {
	t.Helper()
	proc.processFrame(context.Background(), request(0, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
	}))
}
