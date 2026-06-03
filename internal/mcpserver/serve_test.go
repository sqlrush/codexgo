package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/core"
)

// newServeAssembly builds an assembly with a completing mock turn for serve
// tests.
func newServeAssembly(t *testing.T, turns ...core.MockTurn) *appserver.Assembly {
	t.Helper()
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: mockClientFactory(turns...),
		CodexHome:          "/home/.codex",
		DefaultModel:       "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return asm
}

// encodeFrame marshals an incoming frame to a single JSON line.
func encodeFrame(t *testing.T, id int64, method string, params any) string {
	t.Helper()
	frame := request(id, method, params)
	b, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      RequestID       `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{
		JSONRPC: jsonRPCVersion,
		ID:      *frame.ID,
		Method:  method,
		Params:  frame.Params,
	})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return string(b) + "\n"
}

func TestServeStdioInitialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	asm := newServeAssembly(t, completedTurn("hi"))

	input := encodeFrame(t, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo":      map[string]any{"name": "stdio-test", "version": "1.0"},
	})
	r := strings.NewReader(input)
	pr, pw := io.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- ServeStdio(ctx, asm, appserver.Defaults{
			Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "stdio-agent",
		}, r, pw)
		_ = pw.Close()
	}()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	if !scanner.Scan() {
		t.Fatalf("no response line: %v", scanner.Err())
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The first frame must be the initialize response carrying jsonrpc 2.0.
	var version string
	if err := json.Unmarshal(frame["jsonrpc"], &version); err != nil || version != "2.0" {
		t.Fatalf("jsonrpc = %q (err=%v)", version, err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(frame["result"], &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	var si map[string]any
	if err := json.Unmarshal(result["serverInfo"], &si); err != nil {
		t.Fatalf("decode serverInfo: %v", err)
	}
	if si["user_agent"] != "stdio-agent" {
		t.Fatalf("serverInfo.user_agent = %v", si["user_agent"])
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serve loop did not exit after cancel")
	}
}

func TestServeStdioToolCallRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	asm := newServeAssembly(t, completedTurn("done"))

	// Drive input through a pipe that stays open: a real MCP client holds the
	// connection while a tool call runs. Closing stdin (EOF) would tear down the
	// engine, matching a client disconnect. The output pipe is synchronous, so
	// the reader must run concurrently with the writer to avoid a deadlock.
	inR, inW := io.Pipe()
	pr, pw := io.Pipe()
	go func() {
		_ = ServeStdio(ctx, asm, appserver.Defaults{Model: "gpt-test", ProviderID: "openai", UserAgent: "agent"}, inR, pw)
		_ = pw.Close()
	}()
	defer inW.Close()

	// Reader goroutine: scan frames until the tools/call response (id 2) arrives.
	resultCh := make(chan map[string]json.RawMessage, 1)
	go func() {
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 4096), 1<<20)
		for scanner.Scan() {
			var frame map[string]json.RawMessage
			if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
				continue
			}
			idRaw, ok := frame["id"]
			if !ok {
				continue
			}
			var id int64
			if err := json.Unmarshal(idRaw, &id); err != nil || id != 2 {
				continue
			}
			if resRaw, ok := frame["result"]; ok {
				var res map[string]json.RawMessage
				_ = json.Unmarshal(resRaw, &res)
				resultCh <- res
				return
			}
		}
		resultCh <- nil
	}()

	// Writer goroutine: feed the initialize and tools/call frames.
	go func() {
		_, _ = io.WriteString(inW, encodeFrame(t, 1, "initialize", map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo":      map[string]any{"name": "stdio-test", "version": "1.0"},
		}))
		_, _ = io.WriteString(inW, encodeFrame(t, 2, "tools/call", map[string]any{
			"name":      toolNameCodex,
			"arguments": map[string]any{"prompt": "hi"},
		}))
	}()

	var toolResult map[string]json.RawMessage
	select {
	case toolResult = <-resultCh:
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for tools/call response")
	}
	if toolResult == nil {
		t.Fatal("never saw tools/call response")
	}
	var structured map[string]any
	if err := json.Unmarshal(toolResult["structuredContent"], &structured); err != nil {
		t.Fatalf("decode structuredContent: %v", err)
	}
	if structured["content"] != "done" {
		t.Fatalf("structuredContent.content = %v, want done", structured["content"])
	}
}
