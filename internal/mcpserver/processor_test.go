package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestInitializeReturnsServerInfo(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)

	result, errObj, ok := w.responseFor(t, 0)
	if !ok || errObj != nil {
		t.Fatalf("initialize failed: ok=%v err=%v", ok, errObj)
	}
	var pv string
	if err := json.Unmarshal(result["protocolVersion"], &pv); err != nil || pv != "2025-06-18" {
		t.Fatalf("protocolVersion = %q (err=%v)", pv, err)
	}
	var si map[string]any
	if err := json.Unmarshal(result["serverInfo"], &si); err != nil {
		t.Fatalf("decode serverInfo: %v", err)
	}
	if si["name"] != "codex-mcp-server" {
		t.Fatalf("serverInfo.name = %v", si["name"])
	}
	if si["user_agent"] != "codex-mcp/0.0.0-test" {
		t.Fatalf("serverInfo.user_agent = %v", si["user_agent"])
	}
	// Capabilities advertise the tools listChanged capability.
	var caps map[string]json.RawMessage
	if err := json.Unmarshal(result["capabilities"], &caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if _, ok := caps["tools"]; !ok {
		t.Fatalf("capabilities missing tools: %v", caps)
	}
}

func TestInitializeTwiceFails(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)
	proc.processFrame(context.Background(), request(1, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "c", "version": "1"},
	}))
	_, errObj, ok := w.responseFor(t, 1)
	if !ok || errObj == nil {
		t.Fatalf("second initialize should fail: ok=%v err=%v", ok, errObj)
	}
	var code int64
	_ = json.Unmarshal(errObj["code"], &code)
	if code != codeInvalidRequest {
		t.Fatalf("error code = %d, want %d", code, codeInvalidRequest)
	}
}

func TestPing(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)
	proc.processFrame(context.Background(), request(1, "ping", nil))
	result, errObj, ok := w.responseFor(t, 1)
	if !ok || errObj != nil {
		t.Fatalf("ping failed: ok=%v err=%v", ok, errObj)
	}
	if len(result) != 0 {
		t.Fatalf("ping result should be empty object, got %v", result)
	}
}

func TestListTools(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)
	proc.processFrame(context.Background(), request(1, "tools/list", nil))

	result, errObj, ok := w.responseFor(t, 1)
	if !ok || errObj != nil {
		t.Fatalf("tools/list failed: ok=%v err=%v", ok, errObj)
	}
	var tools []toolDescriptor
	if err := json.Unmarshal(result["tools"], &tools); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		if tool.OutputSchema == nil {
			t.Fatalf("tool %q missing output schema", tool.Name)
		}
	}
	if !names[toolNameCodex] || !names[toolNameCodexReply] {
		t.Fatalf("missing expected tools: %v", names)
	}
}

func TestCallCodexStreamsEventsAndReturnsResult(t *testing.T) {
	proc, w := newTestProcessor(t, completedTurn("hello from codex"))
	initialize(t, proc)

	// handleCallCodex spawns its own goroutine, so processFrame returns
	// immediately; the response and event stream arrive asynchronously.
	proc.processFrame(context.Background(), request(7, "tools/call", map[string]any{
		"name":      toolNameCodex,
		"arguments": map[string]any{"prompt": "hi"},
	}))

	result := waitForResponse(t, w, 7)

	// The content array's first text block mirrors the agent's last message.
	var blocks []contentBlock
	if err := json.Unmarshal(result["content"], &blocks); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if len(blocks) == 0 || blocks[0].Text != "hello from codex" {
		t.Fatalf("content blocks = %v, want first text 'hello from codex'", blocks)
	}

	// structuredContent must carry threadId + content mirror.
	var structured map[string]any
	if err := json.Unmarshal(result["structuredContent"], &structured); err != nil {
		t.Fatalf("decode structuredContent: %v", err)
	}
	if structured["content"] != "hello from codex" {
		t.Fatalf("structuredContent.content = %v, want hello from codex", structured["content"])
	}
	tid, _ := structured["threadId"].(string)
	if tid == "" {
		t.Fatalf("structuredContent.threadId is empty")
	}

	// The codex/event stream must include the SessionConfigured event with _meta
	// carrying the threadId and the originating requestId.
	notes := w.notificationsByMethod("codex/event")
	if len(notes) == 0 {
		t.Fatal("no codex/event notifications emitted")
	}
	var sawSessionConfigured, sawMetaThread bool
	for _, n := range notes {
		var params map[string]json.RawMessage
		if err := json.Unmarshal(n["params"], &params); err != nil {
			continue
		}
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(params["msg"], &msg); err == nil {
			var typ string
			_ = json.Unmarshal(msg["type"], &typ)
			if typ == "session_configured" {
				sawSessionConfigured = true
			}
		}
		if metaRaw, ok := params["_meta"]; ok {
			var meta map[string]any
			if err := json.Unmarshal(metaRaw, &meta); err == nil {
				if meta["threadId"] == tid && meta["requestId"] != nil {
					sawMetaThread = true
				}
			}
		}
	}
	if !sawSessionConfigured {
		t.Fatal("codex/event stream missing session_configured")
	}
	if !sawMetaThread {
		t.Fatal("codex/event _meta missing threadId/requestId correlation")
	}
}

func TestCallCodexReplyContinuesThread(t *testing.T) {
	// Two scripted turns: the first for the codex tool, the second for the reply.
	proc, w := newTestProcessor(t, completedTurn("first"), completedTurn("second"))
	initialize(t, proc)

	proc.processFrame(context.Background(), request(20, "tools/call", map[string]any{
		"name":      toolNameCodex,
		"arguments": map[string]any{"prompt": "hi"},
	}))
	first := waitForResponse(t, w, 20)
	var structured map[string]any
	if err := json.Unmarshal(first["structuredContent"], &structured); err != nil {
		t.Fatalf("decode structuredContent: %v", err)
	}
	threadID, _ := structured["threadId"].(string)
	if threadID == "" {
		t.Fatal("first call produced no threadId")
	}

	// Continue the same thread via codex-reply.
	proc.processFrame(context.Background(), request(21, "tools/call", map[string]any{
		"name":      toolNameCodexReply,
		"arguments": map[string]any{"threadId": threadID, "prompt": "more"},
	}))
	second := waitForResponse(t, w, 21)
	var structured2 map[string]any
	if err := json.Unmarshal(second["structuredContent"], &structured2); err != nil {
		t.Fatalf("decode reply structuredContent: %v", err)
	}
	if structured2["threadId"] != threadID {
		t.Fatalf("reply threadId = %v, want %v", structured2["threadId"], threadID)
	}
	if structured2["content"] != "second" {
		t.Fatalf("reply content = %v, want 'second'", structured2["content"])
	}
}

func TestCallCodexReplyUnknownThread(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)

	proc.processFrame(context.Background(), request(3, "tools/call", map[string]any{
		"name":      toolNameCodexReply,
		"arguments": map[string]any{"threadId": "no-such-thread", "prompt": "again"},
	}))

	result := waitForResponse(t, w, 3)
	var isErr bool
	if err := json.Unmarshal(result["isError"], &isErr); err != nil || !isErr {
		t.Fatalf("expected isError true, got %v (err=%v)", isErr, err)
	}
	var structured map[string]any
	if err := json.Unmarshal(result["structuredContent"], &structured); err != nil {
		t.Fatalf("decode structuredContent: %v", err)
	}
	if structured["threadId"] != "no-such-thread" {
		t.Fatalf("structuredContent.threadId = %v", structured["threadId"])
	}
}

func TestCallUnknownTool(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)
	proc.processFrame(context.Background(), request(4, "tools/call", map[string]any{
		"name": "bogus",
	}))
	result := waitForResponse(t, w, 4)
	var isErr bool
	_ = json.Unmarshal(result["isError"], &isErr)
	if !isErr {
		t.Fatalf("unknown tool should be an error result: %v", result)
	}
}

func TestCallCodexMissingPrompt(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)
	proc.processFrame(context.Background(), request(5, "tools/call", map[string]any{
		"name":      toolNameCodex,
		"arguments": map[string]any{"cwd": "/x"},
	}))
	result := waitForResponse(t, w, 5)
	var isErr bool
	_ = json.Unmarshal(result["isError"], &isErr)
	if !isErr {
		t.Fatalf("missing prompt should error: %v", result)
	}
}

func TestCallCodexRejectsUnknownField(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)
	proc.processFrame(context.Background(), request(6, "tools/call", map[string]any{
		"name":      toolNameCodex,
		"arguments": map[string]any{"prompt": "hi", "profile": "work"},
	}))
	result := waitForResponse(t, w, 6)
	var isErr bool
	_ = json.Unmarshal(result["isError"], &isErr)
	if !isErr {
		t.Fatalf("unknown field should error: %v", result)
	}
}

func TestDelegatesThreadStartToAppServer(t *testing.T) {
	proc, w := newTestProcessor(t, completedTurn("ok"))
	initialize(t, proc)

	proc.processFrame(context.Background(), request(10, "thread/start", map[string]any{}))

	result, errObj, ok := w.responseFor(t, 10)
	if !ok {
		t.Fatal("no response for thread/start")
	}
	if errObj != nil {
		t.Fatalf("thread/start errored: %v", errObj)
	}
	if _, hasThread := result["thread"]; !hasThread {
		t.Fatalf("thread/start response missing thread: %v", result)
	}
}

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	proc, w := newTestProcessor(t)
	initialize(t, proc)
	proc.processFrame(context.Background(), request(11, "no/such/method", nil))
	_, errObj, ok := w.responseFor(t, 11)
	if !ok || errObj == nil {
		t.Fatalf("unknown method should error: ok=%v err=%v", ok, errObj)
	}
	var code int64
	_ = json.Unmarshal(errObj["code"], &code)
	if code != codeMethodNotFound {
		t.Fatalf("error code = %d, want method-not-found %d", code, codeMethodNotFound)
	}
}

func TestPreInitializeDelegatedMethodRejected(t *testing.T) {
	proc, w := newTestProcessor(t)
	// No initialize. A v2 method must be rejected by the app-server's not-
	// initialized gate.
	proc.processFrame(context.Background(), request(12, "thread/start", map[string]any{}))
	_, errObj, ok := w.responseFor(t, 12)
	if !ok || errObj == nil {
		t.Fatalf("pre-initialize delegated method should error: ok=%v err=%v", ok, errObj)
	}
}

func waitForResponse(t *testing.T, w *captureWriter, id int64) map[string]json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result, errObj, ok := w.responseFor(t, id)
		if ok {
			if errObj != nil {
				t.Fatalf("unexpected error response for id %d: %v", id, errObj)
			}
			return result
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no response for id %d after polling", id)
	return nil
}
