package appserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// captureSink records every outgoing message for assertions. It is safe for
// concurrent use, matching the OutgoingSink contract.
type captureSink struct {
	mu   sync.Mutex
	msgs []appserverproto.JSONRPCMessage
}

func (s *captureSink) Send(msg appserverproto.JSONRPCMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
	return nil
}

func (s *captureSink) snapshot() []appserverproto.JSONRPCMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]appserverproto.JSONRPCMessage(nil), s.msgs...)
}

// newTestProcessor builds a processor wired to a capture sink over a minimal
// assembly.
func newTestProcessor(t *testing.T) (*Processor, *captureSink) {
	t.Helper()
	asm, err := Assemble(AssemblyConfig{
		ModelClientFactory: mockClientFactory(completedTurn("hi")),
		CodexHome:          "/home/.codex",
		DefaultModel:       "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	sink := &captureSink{}
	proc := NewProcessor(ProcessorConfig{
		Assembly: asm,
		Defaults: Defaults{Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "codex-go/0.0.0-test"},
		Sink:     sink,
	})
	return proc, sink
}

// requestEnvelope builds a JSON-RPC request with marshaled params.
func requestEnvelope(t *testing.T, id int64, method string, params any) appserverproto.JSONRPCRequest {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		raw = b
	}
	return appserverproto.JSONRPCRequest{
		ID:     appserverproto.NewIntegerRequestId(id),
		Method: method,
		Params: raw,
	}
}

// findResponse returns the first response/error for the given integer id.
func findResponse(msgs []appserverproto.JSONRPCMessage, id int64) (json.RawMessage, *appserverproto.JSONRPCErrorBody, bool) {
	for _, m := range msgs {
		switch m.Kind {
		case appserverproto.MessageKindResponse:
			if v, ok := m.Response.ID.Integer(); ok && v == id {
				return m.Response.Result, nil, true
			}
		case appserverproto.MessageKindError:
			if v, ok := m.Error.ID.Integer(); ok && v == id {
				body := m.Error.Error
				return nil, &body, true
			}
		}
	}
	return nil, nil, false
}

func TestProcessorRequiresInitialize(t *testing.T) {
	proc, sink := newTestProcessor(t)
	conn := NewConn()

	// A thread/start before initialize must fail with invalid-request.
	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 1, "thread/start", appserverproto.ThreadStartParams{}))

	_, errBody, ok := findResponse(sink.snapshot(), 1)
	if !ok {
		t.Fatal("no response for pre-initialize request")
	}
	if errBody == nil || errBody.Code != InvalidRequestCode {
		t.Fatalf("want invalid-request error, got %+v", errBody)
	}
}

func TestProcessorInitialize(t *testing.T) {
	proc, sink := newTestProcessor(t)
	conn := NewConn()

	params := appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "test-client", Version: "1.2.3"},
	}
	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 1, "initialize", params))

	result, errBody, ok := findResponse(sink.snapshot(), 1)
	if !ok || errBody != nil {
		t.Fatalf("initialize failed: ok=%v err=%+v", ok, errBody)
	}
	var resp appserverproto.InitializeResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if resp.CodexHome != protocol.AbsolutePath("/home/.codex") {
		t.Fatalf("CodexHome = %q", resp.CodexHome)
	}
	if resp.UserAgent != "codex-go/0.0.0-test" {
		t.Fatalf("UserAgent = %q", resp.UserAgent)
	}
	if !conn.isInitialized() {
		t.Fatal("connection not marked initialized")
	}

	// A second initialize is rejected.
	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 2, "initialize", params))
	_, errBody2, ok := findResponse(sink.snapshot(), 2)
	if !ok || errBody2 == nil {
		t.Fatalf("double initialize should fail: ok=%v err=%+v", ok, errBody2)
	}
}

func TestProcessorUnknownMethod(t *testing.T) {
	proc, sink := newTestProcessor(t)
	conn := NewConn()
	conn.markInitialized("c", "1", false)

	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 1, "no/such/method", nil))
	_, errBody, ok := findResponse(sink.snapshot(), 1)
	if !ok || errBody == nil || errBody.Code != MethodNotFoundCode {
		t.Fatalf("want method-not-found, got ok=%v err=%+v", ok, errBody)
	}
}

func TestProcessorFsRoundTrip(t *testing.T) {
	proc, sink := newTestProcessor(t)
	conn := NewConn()
	conn.markInitialized("c", "1", false)

	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	content := []byte("hello fs")

	// fs/writeFile
	writeParams := appserverproto.FsWriteFileParams{
		Path:       protocol.AbsolutePath(path),
		DataBase64: base64.StdEncoding.EncodeToString(content),
	}
	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 1, "fs/writeFile", writeParams))
	if _, errBody, ok := findResponse(sink.snapshot(), 1); !ok || errBody != nil {
		t.Fatalf("fs/writeFile failed: ok=%v err=%+v", ok, errBody)
	}

	// fs/readFile
	readParams := appserverproto.FsReadFileParams{Path: protocol.AbsolutePath(path)}
	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 2, "fs/readFile", readParams))
	result, errBody, ok := findResponse(sink.snapshot(), 2)
	if !ok || errBody != nil {
		t.Fatalf("fs/readFile failed: ok=%v err=%+v", ok, errBody)
	}
	var resp appserverproto.FsReadFileResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("decode read response: %v", err)
	}
	got, err := base64.StdEncoding.DecodeString(resp.DataBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("read content = %q, want %q", got, content)
	}

	// fs/getMetadata reports a file.
	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 3, "fs/getMetadata", appserverproto.FsGetMetadataParams{Path: protocol.AbsolutePath(path)}))
	mResult, mErr, mOk := findResponse(sink.snapshot(), 3)
	if !mOk || mErr != nil {
		t.Fatalf("fs/getMetadata failed: ok=%v err=%+v", mOk, mErr)
	}
	var meta appserverproto.FsGetMetadataResponse
	if err := json.Unmarshal(mResult, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if !meta.IsFile {
		t.Fatalf("metadata IsFile = false for %q", path)
	}
}

func TestProcessorFsReadMissingFileMapsError(t *testing.T) {
	proc, sink := newTestProcessor(t)
	conn := NewConn()
	conn.markInitialized("c", "1", false)

	missing := filepath.Join(t.TempDir(), "nope.txt")
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("test setup: file should not exist")
	}
	proc.HandleRequest(context.Background(), conn, requestEnvelope(t, 1, "fs/readFile", appserverproto.FsReadFileParams{Path: protocol.AbsolutePath(missing)}))
	_, errBody, ok := findResponse(sink.snapshot(), 1)
	if !ok || errBody == nil {
		t.Fatalf("reading a missing file should error: ok=%v err=%+v", ok, errBody)
	}
	if errBody.Code != InternalErrorCode {
		t.Fatalf("missing-file error code = %d, want internal", errBody.Code)
	}
}
