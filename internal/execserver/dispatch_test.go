package execserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestDispatchProcessUnknownMethod(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	_, rpcErr := DispatchProcessMethod(context.Background(), backend, "process/bogus", nil)
	if rpcErr == nil || rpcErr.Code != codeMethodNotFound {
		t.Fatalf("expected method-not-found, got %+v", rpcErr)
	}
}

func TestDispatchProcessInvalidParams(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	_, rpcErr := DispatchProcessMethod(context.Background(), backend, ExecMethod, json.RawMessage(`"not-an-object"`))
	if rpcErr == nil || rpcErr.Code != codeInvalidParams {
		t.Fatalf("expected invalid-params, got %+v", rpcErr)
	}
}

func TestDispatchProcessTerminateUnknown(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	params, _ := json.Marshal(TerminateParams{ProcessID: NewProcessId("ghost")})
	result, rpcErr := DispatchProcessMethod(context.Background(), backend, ExecTerminateMethod, params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %+v", rpcErr)
	}
	var resp TerminateResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Running {
		t.Fatalf("expected running=false")
	}
}

func TestDispatchFsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")

	writeParams, _ := json.Marshal(FsWriteFileParams{
		Path:       protocol.AbsolutePath(file),
		DataBase64: base64.StdEncoding.EncodeToString([]byte("data")),
	})
	if _, rpcErr := DispatchFsMethod(context.Background(), FsWriteFileMethod, writeParams); rpcErr != nil {
		t.Fatalf("write dispatch failed: %+v", rpcErr)
	}

	readParams, _ := json.Marshal(FsReadFileParams{Path: protocol.AbsolutePath(file)})
	result, rpcErr := DispatchFsMethod(context.Background(), FsReadFileMethod, readParams)
	if rpcErr != nil {
		t.Fatalf("read dispatch failed: %+v", rpcErr)
	}
	var payload FsHelperPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	rf, perr := payload.ReadFile()
	if perr != nil {
		t.Fatalf("read file: %+v", perr)
	}
	decoded, _ := base64.StdEncoding.DecodeString(rf.DataBase64)
	if string(decoded) != "data" {
		t.Fatalf("content mismatch: %q", decoded)
	}
}

func TestDispatchFsUnknownMethod(t *testing.T) {
	_, rpcErr := DispatchFsMethod(context.Background(), "fs/bogus", nil)
	if rpcErr == nil || rpcErr.Code != codeMethodNotFound {
		t.Fatalf("expected method-not-found, got %+v", rpcErr)
	}
}
