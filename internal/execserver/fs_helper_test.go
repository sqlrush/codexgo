package execserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestFsHelperRequestUsesFsMethodNames(t *testing.T) {
	req := NewWriteFileRequest(FsWriteFileParams{
		Path:       protocol.AbsolutePath("/tmp/file"),
		DataBase64: "",
	})
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if string(raw["operation"]) != `"`+FsWriteFileMethod+`"` {
		t.Fatalf("operation mismatch: %s", raw["operation"])
	}
	if _, ok := raw["params"]; !ok {
		t.Fatalf("missing params content: %s", data)
	}
}

func TestFsHelperRequestRoundTrip(t *testing.T) {
	tests := []FsHelperRequest{
		NewReadFileRequest(FsReadFileParams{Path: protocol.AbsolutePath("/a")}),
		NewWriteFileRequest(FsWriteFileParams{Path: protocol.AbsolutePath("/a"), DataBase64: "aGk="}),
		NewCreateDirectoryRequest(FsCreateDirectoryParams{Path: protocol.AbsolutePath("/a")}),
		NewGetMetadataRequest(FsGetMetadataParams{Path: protocol.AbsolutePath("/a")}),
		NewReadDirectoryRequest(FsReadDirectoryParams{Path: protocol.AbsolutePath("/a")}),
		NewRemoveRequest(FsRemoveParams{Path: protocol.AbsolutePath("/a")}),
		NewCopyRequest(FsCopyParams{SourcePath: protocol.AbsolutePath("/a"), DestinationPath: protocol.AbsolutePath("/b"), Recursive: true}),
	}
	for _, req := range tests {
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal %s: %v", req.operation, err)
		}
		var back FsHelperRequest
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", req.operation, err)
		}
		if back.operation != req.operation {
			t.Fatalf("operation mismatch: got %s want %s", back.operation, req.operation)
		}
	}
}

func TestFsHelperResponseStatusTags(t *testing.T) {
	okResp := NewOkResponse(newWriteFilePayload(FsWriteFileResponse{}))
	data, err := json.Marshal(okResp)
	if err != nil {
		t.Fatalf("marshal ok: %v", err)
	}
	if !strings.Contains(string(data), `"status":"ok"`) {
		t.Fatalf("ok response missing status: %s", data)
	}
	if !strings.Contains(string(data), `"payload"`) {
		t.Fatalf("ok response missing payload: %s", data)
	}

	errResp := NewErrorResponse(internalError("boom"))
	data, err = json.Marshal(errResp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(data), `"status":"error"`) {
		t.Fatalf("error response missing status: %s", data)
	}

	var back FsHelperResponse
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	body, ok := back.Err()
	if !ok || body.Message != "boom" || body.Code != codeInternalError {
		t.Fatalf("error round-trip mismatch: %+v ok=%v", body, ok)
	}
}

func TestFsHelperPayloadExpectMismatch(t *testing.T) {
	payload := newReadFilePayload(FsReadFileResponse{DataBase64: "aGk="})
	if _, err := payload.WriteFile(); err == nil {
		t.Fatalf("expected mismatch error for WriteFile on ReadFile payload")
	}
	got, err := payload.ReadFile()
	if err != nil {
		t.Fatalf("ReadFile: %+v", err)
	}
	if got.DataBase64 != "aGk=" {
		t.Fatalf("ReadFile data mismatch: %s", got.DataBase64)
	}
}

func TestRunDirectRequestReadWrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")

	writeReq := NewWriteFileRequest(FsWriteFileParams{
		Path:       protocol.AbsolutePath(file),
		DataBase64: base64.StdEncoding.EncodeToString([]byte("contents")),
	})
	if _, rpcErr := RunDirectRequest(context.Background(), writeReq); rpcErr != nil {
		t.Fatalf("write request failed: %+v", rpcErr)
	}

	readReq := NewReadFileRequest(FsReadFileParams{Path: protocol.AbsolutePath(file)})
	payload, rpcErr := RunDirectRequest(context.Background(), readReq)
	if rpcErr != nil {
		t.Fatalf("read request failed: %+v", rpcErr)
	}
	resp, err := payload.ReadFile()
	if err != nil {
		t.Fatalf("expect read file: %+v", err)
	}
	decoded, derr := base64.StdEncoding.DecodeString(resp.DataBase64)
	if derr != nil {
		t.Fatalf("decode: %v", derr)
	}
	if string(decoded) != "contents" {
		t.Fatalf("read content mismatch: %q", decoded)
	}
}

func TestRunDirectRequestNotFound(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	req := NewReadFileRequest(FsReadFileParams{Path: protocol.AbsolutePath(missing)})
	_, rpcErr := RunDirectRequest(context.Background(), req)
	if rpcErr == nil {
		t.Fatalf("expected not-found error")
	}
	if rpcErr.Code != codeNotFound {
		t.Fatalf("expected not-found code %d, got %d", codeNotFound, rpcErr.Code)
	}
}

func TestRunDirectRequestCreateDirectoryAndMetadata(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	createReq := NewCreateDirectoryRequest(FsCreateDirectoryParams{Path: protocol.AbsolutePath(sub)})
	if _, rpcErr := RunDirectRequest(context.Background(), createReq); rpcErr != nil {
		t.Fatalf("create directory failed: %+v", rpcErr)
	}
	mdReq := NewGetMetadataRequest(FsGetMetadataParams{Path: protocol.AbsolutePath(sub)})
	payload, rpcErr := RunDirectRequest(context.Background(), mdReq)
	if rpcErr != nil {
		t.Fatalf("metadata failed: %+v", rpcErr)
	}
	md, err := payload.GetMetadata()
	if err != nil {
		t.Fatalf("expect metadata: %+v", err)
	}
	if !md.IsDirectory {
		t.Fatalf("expected directory metadata, got %+v", md)
	}
}

func TestRunFsHelperMain(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	req := NewReadFileRequest(FsReadFileParams{Path: protocol.AbsolutePath(file)})
	input, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := RunFsHelperMain(context.Background(), bytes.NewReader(input), &out, &errOut)
	if code != 0 {
		t.Fatalf("helper exit code %d, stderr=%s", code, errOut.String())
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("output should end with newline: %q", out.String())
	}
	var resp FsHelperResponse
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	payload, ok := resp.Ok()
	if !ok {
		t.Fatalf("expected ok response: %s", out.String())
	}
	rf, perr := payload.ReadFile()
	if perr != nil {
		t.Fatalf("read file payload: %+v", perr)
	}
	decoded, _ := base64.StdEncoding.DecodeString(rf.DataBase64)
	if string(decoded) != "hi" {
		t.Fatalf("content mismatch: %q", decoded)
	}
}

func TestRunFsHelperMainBadInput(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunFsHelperMain(context.Background(), strings.NewReader("not json"), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected diagnostic on stderr")
	}
}
