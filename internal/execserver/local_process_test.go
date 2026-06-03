package execserver

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

// recordingSender captures notifications for assertions.
type recordingSender struct {
	mu     sync.Mutex
	method []string
}

func (s *recordingSender) Notify(_ context.Context, method string, _ any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.method = append(s.method, method)
	return nil
}

func (s *recordingSender) methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.method))
	copy(out, s.method)
	return out
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell commands not available on Windows")
	}
}

// readUntil polls process/read until the predicate is satisfied or the deadline
// passes, accumulating chunks across reads.
func readUntil(t *testing.T, b *LocalProcess, id ProcessId, done func(ReadResponse) bool) ReadResponse {
	t.Helper()
	var afterSeq *uint64
	wait := uint64(500)
	deadline := time.Now().Add(5 * time.Second)
	var last ReadResponse
	var collected []ProcessOutputChunk
	for time.Now().Before(deadline) {
		resp, rpcErr := b.ExecRead(context.Background(), ReadParams{
			ProcessID: id,
			AfterSeq:  afterSeq,
			WaitMs:    &wait,
		})
		if rpcErr != nil {
			t.Fatalf("ExecRead failed: %+v", rpcErr)
		}
		collected = append(collected, resp.Chunks...)
		resp.Chunks = collected
		last = resp
		if done(resp) {
			return resp
		}
		next := resp.NextSeq
		if next > 0 {
			n := next - 1
			afterSeq = &n
		}
	}
	return last
}

func TestLocalProcessEchoLifecycle(t *testing.T) {
	skipOnWindows(t)
	sender := &recordingSender{}
	backend := NewLocalProcess(sender, nil)
	defer backend.Shutdown()

	id := NewProcessId("echo-1")
	resp, rpcErr := backend.Exec(context.Background(), ExecParams{
		ProcessID: id,
		Argv:      []string{"/bin/echo", "hello world"},
		Cwd:       "/tmp",
		Env:       map[string]string{},
	})
	if rpcErr != nil {
		t.Fatalf("Exec failed: %+v", rpcErr)
	}
	if resp.ProcessID.String() != "echo-1" {
		t.Fatalf("unexpected process id: %s", resp.ProcessID)
	}

	final := readUntil(t, backend, id, func(r ReadResponse) bool { return r.Closed })
	if !final.Closed {
		t.Fatalf("process did not close: %+v", final)
	}
	if !final.Exited || final.ExitCode == nil || *final.ExitCode != 0 {
		t.Fatalf("unexpected exit: exited=%v code=%v", final.Exited, final.ExitCode)
	}
	var combined string
	for _, chunk := range final.Chunks {
		combined += string(chunk.Chunk)
	}
	if want := "hello world\n"; combined != want {
		t.Fatalf("output mismatch: got %q want %q", combined, want)
	}

	// The closed notification is delivered after the reader is woken, so poll
	// briefly for both lifecycle notifications to arrive.
	deadline := time.Now().Add(2 * time.Second)
	for {
		methods := sender.methods()
		hasExited, hasClosed := false, false
		for _, m := range methods {
			if m == ExecExitedMethod {
				hasExited = true
			}
			if m == ExecClosedMethod {
				hasClosed = true
			}
		}
		if hasExited && hasClosed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing lifecycle notifications: %v", methods)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLocalProcessDuplicateIDRejected(t *testing.T) {
	skipOnWindows(t)
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()

	id := NewProcessId("dup")
	params := ExecParams{ProcessID: id, Argv: []string{"/bin/sleep", "5"}, Cwd: "/tmp", Env: map[string]string{}}
	if _, rpcErr := backend.Exec(context.Background(), params); rpcErr != nil {
		t.Fatalf("first Exec failed: %+v", rpcErr)
	}
	_, rpcErr := backend.Exec(context.Background(), params)
	if rpcErr == nil {
		t.Fatalf("expected duplicate process error")
	}
	if rpcErr.Code != codeInvalidRequest {
		t.Fatalf("expected invalid-request code, got %d", rpcErr.Code)
	}
}

func TestLocalProcessEmptyArgvRejected(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	_, rpcErr := backend.Exec(context.Background(), ExecParams{
		ProcessID: NewProcessId("empty"),
		Argv:      nil,
		Cwd:       "/tmp",
		Env:       map[string]string{},
	})
	if rpcErr == nil || rpcErr.Code != codeInvalidParams {
		t.Fatalf("expected invalid-params for empty argv, got %+v", rpcErr)
	}
}

func TestLocalProcessWriteStdin(t *testing.T) {
	skipOnWindows(t)
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()

	id := NewProcessId("cat-1")
	if _, rpcErr := backend.Exec(context.Background(), ExecParams{
		ProcessID: id,
		Argv:      []string{"/bin/cat"},
		Cwd:       "/tmp",
		Env:       map[string]string{},
		PipeStdin: true,
	}); rpcErr != nil {
		t.Fatalf("Exec failed: %+v", rpcErr)
	}

	writeResp, rpcErr := backend.ExecWrite(context.Background(), WriteParams{
		ProcessID: id,
		Chunk:     []byte("ping\n"),
	})
	if rpcErr != nil {
		t.Fatalf("ExecWrite failed: %+v", rpcErr)
	}
	if writeResp.Status != WriteStatusAccepted {
		t.Fatalf("expected accepted write, got %s", writeResp.Status)
	}

	got := readUntil(t, backend, id, func(r ReadResponse) bool {
		var combined string
		for _, c := range r.Chunks {
			combined += string(c.Chunk)
		}
		return combined == "ping\n"
	})
	var combined string
	for _, c := range got.Chunks {
		combined += string(c.Chunk)
	}
	if combined != "ping\n" {
		t.Fatalf("stdin echo mismatch: got %q", combined)
	}

	if _, rpcErr := backend.Terminate(TerminateParams{ProcessID: id}); rpcErr != nil {
		t.Fatalf("Terminate failed: %+v", rpcErr)
	}
}

func TestLocalProcessWriteUnknown(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	resp, rpcErr := backend.ExecWrite(context.Background(), WriteParams{
		ProcessID: NewProcessId("nope"),
		Chunk:     []byte("x"),
	})
	if rpcErr != nil {
		t.Fatalf("ExecWrite returned error: %+v", rpcErr)
	}
	if resp.Status != WriteStatusUnknownProcess {
		t.Fatalf("expected unknownProcess, got %s", resp.Status)
	}
}

func TestLocalProcessWriteStdinClosed(t *testing.T) {
	skipOnWindows(t)
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	id := NewProcessId("sleep-nostdin")
	if _, rpcErr := backend.Exec(context.Background(), ExecParams{
		ProcessID: id,
		Argv:      []string{"/bin/sleep", "5"},
		Cwd:       "/tmp",
		Env:       map[string]string{},
	}); rpcErr != nil {
		t.Fatalf("Exec failed: %+v", rpcErr)
	}
	resp, rpcErr := backend.ExecWrite(context.Background(), WriteParams{ProcessID: id, Chunk: []byte("x")})
	if rpcErr != nil {
		t.Fatalf("ExecWrite error: %+v", rpcErr)
	}
	if resp.Status != WriteStatusStdinClosed {
		t.Fatalf("expected stdinClosed, got %s", resp.Status)
	}
	if _, rpcErr := backend.Terminate(TerminateParams{ProcessID: id}); rpcErr != nil {
		t.Fatalf("Terminate failed: %+v", rpcErr)
	}
}

func TestLocalProcessTerminateRunning(t *testing.T) {
	skipOnWindows(t)
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	id := NewProcessId("sleep-term")
	if _, rpcErr := backend.Exec(context.Background(), ExecParams{
		ProcessID: id,
		Argv:      []string{"/bin/sleep", "30"},
		Cwd:       "/tmp",
		Env:       map[string]string{},
	}); rpcErr != nil {
		t.Fatalf("Exec failed: %+v", rpcErr)
	}
	resp, rpcErr := backend.Terminate(TerminateParams{ProcessID: id})
	if rpcErr != nil {
		t.Fatalf("Terminate failed: %+v", rpcErr)
	}
	if !resp.Running {
		t.Fatalf("expected running=true for a live process")
	}

	final := readUntil(t, backend, id, func(r ReadResponse) bool { return r.Exited })
	if !final.Exited {
		t.Fatalf("terminated process should report exited")
	}
}

func TestLocalProcessTerminateUnknown(t *testing.T) {
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()
	resp, rpcErr := backend.Terminate(TerminateParams{ProcessID: NewProcessId("ghost")})
	if rpcErr != nil {
		t.Fatalf("Terminate error: %+v", rpcErr)
	}
	if resp.Running {
		t.Fatalf("expected running=false for unknown process")
	}
}

func TestLocalProcessStartViaBackend(t *testing.T) {
	skipOnWindows(t)
	backend := NewLocalProcess(nil, nil)
	defer backend.Shutdown()

	var eb ExecBackend = backend
	started, err := eb.Start(context.Background(), ExecParams{
		ProcessID: NewProcessId("backend-echo"),
		Argv:      []string{"/bin/echo", "hi"},
		Cwd:       "/tmp",
		Env:       map[string]string{},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if started.Process.ProcessID().String() != "backend-echo" {
		t.Fatalf("unexpected process id: %s", started.Process.ProcessID())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	waitMs := uint64(2000)
	var combined string
	var afterSeq *uint64
	for {
		resp, rerr := started.Process.Read(ctx, afterSeq, nil, &waitMs)
		if rerr != nil {
			t.Fatalf("Read failed: %v", rerr)
		}
		for _, c := range resp.Chunks {
			combined += string(c.Chunk)
		}
		if resp.Closed {
			break
		}
		if resp.NextSeq > 0 {
			n := resp.NextSeq - 1
			afterSeq = &n
		}
		if ctx.Err() != nil {
			t.Fatalf("timed out reading process output, got %q", combined)
		}
	}
	if combined != "hi\n" {
		t.Fatalf("output mismatch: got %q", combined)
	}
}
