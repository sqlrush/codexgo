package execserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestExecServerErrorMessage(t *testing.T) {
	err := &ExecServerError{Code: -32600, Message: "bad"}
	want := "exec-server rejected request (-32600): bad"
	if err.Error() != want {
		t.Fatalf("got %q want %q", err.Error(), want)
	}
}

func TestMapHandlerError(t *testing.T) {
	if mapHandlerError(nil) != nil {
		t.Fatalf("nil handler error should map to nil")
	}
	mapped := mapHandlerError(&rpcErr{body: invalidRequest("nope")})
	var serverErr *ExecServerError
	if !asExecServerError(mapped, &serverErr) {
		t.Fatalf("expected *ExecServerError, got %T", mapped)
	}
	if serverErr.Code != codeInvalidRequest || serverErr.Message != "nope" {
		t.Fatalf("unexpected mapped error: %+v", serverErr)
	}
}

func asExecServerError(err error, target **ExecServerError) bool {
	e, ok := err.(*ExecServerError)
	if ok {
		*target = e
	}
	return ok
}

func TestPopulateEnvCustomExclude(t *testing.T) {
	vars := map[string]string{"FOO_BAR": "1", "BAZ": "2", "FOO_QUX": "3"}
	policy := ExecEnvPolicy{
		Inherit:               protocol.ShellEnvironmentPolicyInheritAll,
		IgnoreDefaultExcludes: true,
		Exclude:               []string{"FOO_*"},
	}
	got := populateEnv(vars, policy, nil)
	want := map[string]string{"BAZ": "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("populateEnv got %v want %v", got, want)
	}
}

func TestPopulateEnvInheritNoneEmpty(t *testing.T) {
	vars := map[string]string{"X": "1"}
	policy := ExecEnvPolicy{
		Inherit:               protocol.ShellEnvironmentPolicyInheritNone,
		IgnoreDefaultExcludes: true,
	}
	got := populateEnv(vars, policy, nil)
	if len(got) != 0 {
		t.Fatalf("expected empty env, got %v", got)
	}
}

func TestPopulateEnvUnknownInheritDefaultsToAll(t *testing.T) {
	vars := map[string]string{"X": "1"}
	policy := ExecEnvPolicy{
		Inherit:               protocol.ShellEnvironmentPolicyInherit("bogus"),
		IgnoreDefaultExcludes: true,
	}
	got := populateEnv(vars, policy, nil)
	if !reflect.DeepEqual(got, vars) {
		t.Fatalf("unknown inherit should default to All: got %v", got)
	}
}

func TestRunDirectRequestReadDirectoryRemoveCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// read directory
	rdPayload, rpcErr := RunDirectRequest(context.Background(), NewReadDirectoryRequest(FsReadDirectoryParams{Path: protocol.AbsolutePath(dir)}))
	if rpcErr != nil {
		t.Fatalf("read directory: %+v", rpcErr)
	}
	rd, perr := rdPayload.ReadDirectory()
	if perr != nil {
		t.Fatalf("expect read directory: %+v", perr)
	}
	if len(rd.Entries) != 1 || rd.Entries[0].FileName != "src.txt" {
		t.Fatalf("unexpected directory entries: %+v", rd.Entries)
	}

	// copy
	cpPayload, rpcErr := RunDirectRequest(context.Background(), NewCopyRequest(FsCopyParams{
		SourcePath:      protocol.AbsolutePath(src),
		DestinationPath: protocol.AbsolutePath(dst),
		Recursive:       false,
	}))
	if rpcErr != nil {
		t.Fatalf("copy: %+v", rpcErr)
	}
	if _, perr := cpPayload.Copy(); perr != nil {
		t.Fatalf("expect copy: %+v", perr)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("copy did not create destination: %v", err)
	}

	// remove
	rmPayload, rpcErr := RunDirectRequest(context.Background(), NewRemoveRequest(FsRemoveParams{Path: protocol.AbsolutePath(dst)}))
	if rpcErr != nil {
		t.Fatalf("remove: %+v", rpcErr)
	}
	if _, perr := rmPayload.Remove(); perr != nil {
		t.Fatalf("expect remove: %+v", perr)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("remove did not delete destination")
	}
}

func TestRunDirectRequestBadBase64(t *testing.T) {
	dir := t.TempDir()
	req := NewWriteFileRequest(FsWriteFileParams{
		Path:       protocol.AbsolutePath(filepath.Join(dir, "f")),
		DataBase64: "!!!not base64!!!",
	})
	_, rpcErr := RunDirectRequest(context.Background(), req)
	if rpcErr == nil || rpcErr.Code != codeInvalidRequest {
		t.Fatalf("expected invalid-request for bad base64, got %+v", rpcErr)
	}
}

func TestFsHelperPayloadOperationAndMismatches(t *testing.T) {
	payload := newCreateDirectoryPayload(FsCreateDirectoryResponse{})
	if payload.Operation() != FsCreateDirectoryMethod {
		t.Fatalf("operation mismatch: %s", payload.Operation())
	}
	if _, err := payload.CreateDirectory(); err != nil {
		t.Fatalf("CreateDirectory: %+v", err)
	}
	if _, err := payload.ReadDirectory(); err == nil {
		t.Fatalf("expected mismatch for ReadDirectory")
	}
	if _, err := payload.GetMetadata(); err == nil {
		t.Fatalf("expected mismatch for GetMetadata")
	}
	if _, err := payload.Remove(); err == nil {
		t.Fatalf("expected mismatch for Remove")
	}
	if _, err := payload.Copy(); err == nil {
		t.Fatalf("expected mismatch for Copy")
	}
}

func TestFsHelperPayloadRoundTripAllOperations(t *testing.T) {
	payloads := []FsHelperPayload{
		newReadFilePayload(FsReadFileResponse{DataBase64: "aGk="}),
		newWriteFilePayload(FsWriteFileResponse{}),
		newCreateDirectoryPayload(FsCreateDirectoryResponse{}),
		newGetMetadataPayload(FsGetMetadataResponse{IsFile: true}),
		newReadDirectoryPayload(FsReadDirectoryResponse{Entries: []FsReadDirectoryEntry{{FileName: "x"}}}),
		newRemovePayload(FsRemoveResponse{}),
		newCopyPayload(FsCopyResponse{}),
	}
	for _, p := range payloads {
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal %s: %v", p.operation, err)
		}
		var back FsHelperPayload
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", p.operation, err)
		}
		if back.operation != p.operation {
			t.Fatalf("operation mismatch: got %s want %s", back.operation, p.operation)
		}
	}
}

func TestReadMaxBytesTruncation(t *testing.T) {
	skipOnWindows(t)
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()

	id := NewProcessId("printf-1")
	if _, rpcErr := backend.Exec(context.Background(), ExecParams{
		ProcessID: id,
		Argv:      []string{"/bin/sh", "-c", "printf 'aaaabbbbcccc'"},
		Cwd:       "/tmp",
		Env:       map[string]string{},
	}); rpcErr != nil {
		t.Fatalf("Exec failed: %+v", rpcErr)
	}

	// Wait for output and close so all bytes are retained.
	readUntil(t, backend, id, func(r ReadResponse) bool { return r.Closed })

	// Read with a small max_bytes; at least one chunk must come back but capped.
	maxBytes := 1
	resp, rpcErr := backend.ExecRead(context.Background(), ReadParams{
		ProcessID: id,
		MaxBytes:  &maxBytes,
	})
	if rpcErr != nil {
		t.Fatalf("ExecRead failed: %+v", rpcErr)
	}
	if len(resp.Chunks) == 0 {
		t.Fatalf("expected at least one chunk with max_bytes set")
	}
	// The first chunk is always returned even if it exceeds max_bytes, but no
	// further chunks beyond the cap.
	total := 0
	for _, c := range resp.Chunks {
		total += len(c.Chunk)
	}
	if len(resp.Chunks) > 1 && total > maxBytes {
		t.Fatalf("expected truncation to first chunk, got %d chunks totaling %d bytes", len(resp.Chunks), total)
	}
}

func TestReadUnknownProcess(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	_, rpcErr := backend.ExecRead(context.Background(), ReadParams{ProcessID: NewProcessId("ghost")})
	if rpcErr == nil || rpcErr.Code != codeInvalidRequest {
		t.Fatalf("expected invalid-request for unknown process, got %+v", rpcErr)
	}
}

func TestReadWaitTimesOutWithoutOutput(t *testing.T) {
	skipOnWindows(t)
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	id := NewProcessId("sleep-wait")
	if _, rpcErr := backend.Exec(context.Background(), ExecParams{
		ProcessID: id,
		Argv:      []string{"/bin/sleep", "30"},
		Cwd:       "/tmp",
		Env:       map[string]string{},
	}); rpcErr != nil {
		t.Fatalf("Exec failed: %+v", rpcErr)
	}
	wait := uint64(100)
	start := time.Now()
	resp, rpcErr := backend.ExecRead(context.Background(), ReadParams{ProcessID: id, WaitMs: &wait})
	if rpcErr != nil {
		t.Fatalf("ExecRead failed: %+v", rpcErr)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("read returned too quickly: %v", elapsed)
	}
	if len(resp.Chunks) != 0 || resp.Exited {
		t.Fatalf("expected empty non-exited read, got %+v", resp)
	}
	_, _ = backend.Terminate(TerminateParams{ProcessID: id})
}

func TestDispatchFsAllMethods(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "d")

	createParams, _ := json.Marshal(FsCreateDirectoryParams{Path: protocol.AbsolutePath(sub)})
	if _, rpcErr := DispatchFsMethod(context.Background(), FsCreateDirectoryMethod, createParams); rpcErr != nil {
		t.Fatalf("createDirectory dispatch: %+v", rpcErr)
	}

	mdParams, _ := json.Marshal(FsGetMetadataParams{Path: protocol.AbsolutePath(sub)})
	if _, rpcErr := DispatchFsMethod(context.Background(), FsGetMetadataMethod, mdParams); rpcErr != nil {
		t.Fatalf("getMetadata dispatch: %+v", rpcErr)
	}

	rdParams, _ := json.Marshal(FsReadDirectoryParams{Path: protocol.AbsolutePath(dir)})
	if _, rpcErr := DispatchFsMethod(context.Background(), FsReadDirectoryMethod, rdParams); rpcErr != nil {
		t.Fatalf("readDirectory dispatch: %+v", rpcErr)
	}

	rmParams, _ := json.Marshal(FsRemoveParams{Path: protocol.AbsolutePath(sub)})
	if _, rpcErr := DispatchFsMethod(context.Background(), FsRemoveMethod, rmParams); rpcErr != nil {
		t.Fatalf("remove dispatch: %+v", rpcErr)
	}

	src := filepath.Join(dir, "s")
	if err := os.WriteFile(src, []byte("y"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cpParams, _ := json.Marshal(FsCopyParams{
		SourcePath:      protocol.AbsolutePath(src),
		DestinationPath: protocol.AbsolutePath(filepath.Join(dir, "s2")),
	})
	if _, rpcErr := DispatchFsMethod(context.Background(), FsCopyMethod, cpParams); rpcErr != nil {
		t.Fatalf("copy dispatch: %+v", rpcErr)
	}
}

func TestSetNotificationSender(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	sender := &recordingSender{}
	backend.SetNotificationSender(sender)
	if backend.notificationSender() != sender {
		t.Fatalf("sender not updated")
	}
	backend.SetNotificationSender(nil)
	if _, ok := backend.notificationSender().(discardNotificationSender); !ok {
		t.Fatalf("nil sender should restore discard sender")
	}
}

func TestDecodeParamsEmptyObject(t *testing.T) {
	var p TerminateParams
	// An empty object should fail to populate a required-field struct but the
	// retry path handles unit-shaped params; TerminateParams has a required
	// field so this simply yields a zero value (processId empty string).
	if err := decodeParams(json.RawMessage("{}"), &p); err != nil {
		t.Fatalf("decodeParams empty object: %v", err)
	}
	if p.ProcessID.String() != "" {
		t.Fatalf("expected empty process id, got %q", p.ProcessID)
	}
}

func TestByteChunkInvalidBase64(t *testing.T) {
	var b ByteChunk
	if err := b.UnmarshalJSON([]byte(`"!!!"`)); err == nil {
		t.Fatalf("expected base64 decode error")
	}
	if err := b.UnmarshalJSON([]byte(`123`)); err == nil {
		t.Fatalf("expected type error for non-string")
	}
}

func TestRunFsHelperMainErrorResponse(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	req := NewReadFileRequest(FsReadFileParams{Path: protocol.AbsolutePath(missing)})
	input, _ := json.Marshal(req)
	var out, errOut bytes.Buffer
	code := RunFsHelperMain(context.Background(), bytes.NewReader(input), &out, &errOut)
	if code != 0 {
		t.Fatalf("helper should exit 0 even for fs error, got %d", code)
	}
	var resp FsHelperResponse
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	body, ok := resp.Err()
	if !ok {
		t.Fatalf("expected error response for missing file")
	}
	if body.Code != codeNotFound {
		t.Fatalf("expected not-found code, got %d", body.Code)
	}
}
