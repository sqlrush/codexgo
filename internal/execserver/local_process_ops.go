package execserver

import (
	"context"
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// rpcErr wraps a JSON-RPC error body produced by the in-process handlers.
type rpcErr struct {
	body *appserverproto.JSONRPCErrorBody
}

func (e *rpcErr) Error() string { return e.body.Message }

// mapHandlerError converts a handler error into an [ExecServerError], matching
// the Rust `map_handler_error` which produces `ExecServerError::Server`.
func mapHandlerError(err *rpcErr) error {
	if err == nil {
		return nil
	}
	return &ExecServerError{Code: err.body.Code, Message: err.body.Message}
}

// Exec spawns a process and returns its protocol response. Mirrors
// `LocalProcess::exec`.
func (b *LocalProcess) Exec(ctx context.Context, params ExecParams) (ExecResponse, *appserverproto.JSONRPCErrorBody) {
	if _, err := b.startProcess(ctx, params); err != nil {
		return ExecResponse{}, err.body
	}
	return ExecResponse{ProcessID: params.ProcessID}, nil
}

// ExecRead returns the public read response. Mirrors `LocalProcess::exec_read`.
func (b *LocalProcess) ExecRead(ctx context.Context, params ReadParams) (ReadResponse, *appserverproto.JSONRPCErrorBody) {
	resp, err := b.execRead(ctx, params)
	if err != nil {
		return ReadResponse{}, err.body
	}
	return resp, nil
}

// ExecWrite writes to process stdin. Mirrors `LocalProcess::exec_write`.
func (b *LocalProcess) ExecWrite(ctx context.Context, params WriteParams) (WriteResponse, *appserverproto.JSONRPCErrorBody) {
	resp, err := b.execWrite(ctx, params)
	if err != nil {
		return WriteResponse{}, err.body
	}
	return resp, nil
}

// Terminate terminates a process. Mirrors `LocalProcess::terminate_process`.
func (b *LocalProcess) Terminate(params TerminateParams) (TerminateResponse, *appserverproto.JSONRPCErrorBody) {
	resp, err := b.terminateProcess(params)
	if err != nil {
		return TerminateResponse{}, err.body
	}
	return resp, nil
}

// execRead pages through retained output, optionally waiting up to waitMs for new
// output, an exit, or a close. Mirrors `LocalProcess::exec_read`.
func (b *LocalProcess) execRead(ctx context.Context, params ReadParams) (ReadResponse, *rpcErr) {
	var afterSeq uint64
	if params.AfterSeq != nil {
		afterSeq = *params.AfterSeq
	}
	maxBytes := -1
	if params.MaxBytes != nil {
		maxBytes = *params.MaxBytes
	}
	var wait time.Duration
	if params.WaitMs != nil {
		wait = time.Duration(*params.WaitMs) * time.Millisecond
	}
	deadline := time.Now().Add(wait)

	for {
		resp, wake, rerr := b.snapshotRead(params.ProcessID, afterSeq, maxBytes)
		if rerr != nil {
			return ReadResponse{}, rerr
		}

		hasNewTerminalEvent := resp.Exited && afterSeq < saturatingSub(resp.NextSeq, 1)
		if len(resp.Chunks) > 0 || resp.Closed || hasNewTerminalEvent || !time.Now().Before(deadline) {
			return resp, nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return resp, nil
		}

		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return resp, nil
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// snapshotRead builds a single read response from the current retained output.
func (b *LocalProcess) snapshotRead(id ProcessId, afterSeq uint64, maxBytes int) (ReadResponse, <-chan struct{}, *rpcErr) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, ok := b.processes[id.String()]
	if !ok {
		return ReadResponse{}, nil, &rpcErr{body: invalidRequest(fmt.Sprintf("unknown process id %s", id))}
	}
	if entry.running == nil {
		return ReadResponse{}, nil, &rpcErr{body: invalidRequest(fmt.Sprintf("process id %s is starting", id))}
	}
	rp := entry.running

	chunks := make([]ProcessOutputChunk, 0)
	totalBytes := 0
	nextSeq := rp.nextSeq
	for _, retained := range rp.output {
		if retained.seq <= afterSeq {
			continue
		}
		chunkLen := len(retained.chunk)
		if len(chunks) > 0 && maxBytes >= 0 && totalBytes+chunkLen > maxBytes {
			break
		}
		totalBytes += chunkLen
		cp := make([]byte, chunkLen)
		copy(cp, retained.chunk)
		chunks = append(chunks, ProcessOutputChunk{
			Seq:    retained.seq,
			Stream: retained.stream,
			Chunk:  cp,
		})
		nextSeq = retained.seq + 1
		if maxBytes >= 0 && totalBytes >= maxBytes {
			break
		}
	}

	var exitCode *int
	if rp.exitCode != nil {
		code := *rp.exitCode
		exitCode = &code
	}

	return ReadResponse{
		Chunks:   chunks,
		NextSeq:  nextSeq,
		Exited:   rp.exitCode != nil,
		ExitCode: exitCode,
		Closed:   rp.closed,
		Failure:  nil,
	}, rp.wake, nil
}

// execWrite sends bytes to a process's stdin. Mirrors
// `LocalProcess::exec_write`.
func (b *LocalProcess) execWrite(ctx context.Context, params WriteParams) (WriteResponse, *rpcErr) {
	b.mu.Lock()
	entry, ok := b.processes[params.ProcessID.String()]
	if !ok {
		b.mu.Unlock()
		return WriteResponse{Status: WriteStatusUnknownProcess}, nil
	}
	if entry.running == nil {
		b.mu.Unlock()
		return WriteResponse{Status: WriteStatusStarting}, nil
	}
	rp := entry.running
	if !rp.tty && !rp.pipeStdin {
		b.mu.Unlock()
		return WriteResponse{Status: WriteStatusStdinClosed}, nil
	}
	stdin := rp.process.Stdin()
	b.mu.Unlock()

	if stdin == nil {
		return WriteResponse{Status: WriteStatusStdinClosed}, nil
	}

	cp := make([]byte, len(params.Chunk))
	copy(cp, params.Chunk)
	select {
	case stdin <- cp:
		return WriteResponse{Status: WriteStatusAccepted}, nil
	case <-ctx.Done():
		return WriteResponse{}, &rpcErr{body: internalError("failed to write to process stdin")}
	}
}

// terminateProcess kills a running process and reports whether it was running.
// Mirrors `LocalProcess::terminate_process`.
func (b *LocalProcess) terminateProcess(params TerminateParams) (TerminateResponse, *rpcErr) {
	b.mu.Lock()
	entry, ok := b.processes[params.ProcessID.String()]
	if !ok || entry.running == nil {
		b.mu.Unlock()
		return TerminateResponse{Running: false}, nil
	}
	rp := entry.running
	if rp.exitCode != nil {
		b.mu.Unlock()
		return TerminateResponse{Running: false}, nil
	}
	process := rp.process
	b.mu.Unlock()

	process.Terminate()
	return TerminateResponse{Running: true}, nil
}

// saturatingSub returns a-b, clamped at 0.
func saturatingSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}
