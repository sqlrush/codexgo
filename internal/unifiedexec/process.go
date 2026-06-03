package unifiedexec

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sqlrush/codexgo/internal/execserver"
	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

// earlyExitGracePeriod is how long from_spawned waits for an immediate exit
// before declaring the process alive. Mirrors EARLY_EXIT_GRACE_PERIOD.
const earlyExitGracePeriod = 150 * time.Millisecond

// outputHandles is the shared output state exposed to polling and streaming
// consumers. Mirrors OutputHandles in process.rs.
type outputHandles struct {
	outputBuffer       *HeadTailBuffer
	outputBufferMu     *sync.Mutex
	outputNotify       *notify
	outputClosed       *atomic.Bool
	outputClosedNotify *notify
	cancellation       *cancelToken
}

// outputSubscriber is a single live consumer of broadcast output chunks.
type outputSubscriber struct {
	ch chan []byte
}

// Process is a unified wrapper over a directly-spawned PTY session and an
// exec-server-backed process. It buffers output with a head/tail cap, fans out
// live chunks to subscribers, and tracks exit/failure state. Mirrors
// UnifiedExecProcess in process.rs. All methods are safe for concurrent use.
type Process struct {
	local       *pty.Process
	remote      execserver.ExecProcess
	sandboxType sandbox.SandboxType

	outputBuffer       *HeadTailBuffer
	outputBufferMu     sync.Mutex
	outputNotify       *notify
	outputClosed       atomic.Bool
	outputClosedNotify *notify
	cancellation       *cancelToken

	stateMu sync.Mutex
	state   processState

	subMu sync.Mutex
	subs  map[*outputSubscriber]struct{}

	writer chan<- []byte
}

// newProcess builds an empty Process around a transport. Exactly one of local /
// remote is non-nil. Mirrors UnifiedExecProcess::new.
func newProcess(sandboxType sandbox.SandboxType) *Process {
	return &Process{
		sandboxType:        sandboxType,
		outputBuffer:       NewDefaultHeadTailBuffer(),
		outputNotify:       newNotify(),
		outputClosedNotify: newNotify(),
		cancellation:       newCancelToken(),
		subs:               make(map[*outputSubscriber]struct{}),
	}
}

// outputHandles returns the shared output state. Mirrors output_handles.
func (p *Process) outputHandles() outputHandles {
	return outputHandles{
		outputBuffer:       p.outputBuffer,
		outputBufferMu:     &p.outputBufferMu,
		outputNotify:       p.outputNotify,
		outputClosed:       &p.outputClosed,
		outputClosedNotify: p.outputClosedNotify,
		cancellation:       p.cancellation,
	}
}

// SubscribeOutput returns a channel of broadcast output chunks for streaming
// consumers, plus an unsubscribe func. The channel is closed when output ends or
// the subscriber is removed. Mirrors output_receiver (a broadcast subscription).
func (p *Process) SubscribeOutput() (<-chan []byte, func()) {
	sub := &outputSubscriber{ch: make(chan []byte, 64)}
	p.subMu.Lock()
	p.subs[sub] = struct{}{}
	p.subMu.Unlock()
	return sub.ch, func() { p.removeSubscriber(sub) }
}

func (p *Process) removeSubscriber(sub *outputSubscriber) {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	if _, ok := p.subs[sub]; ok {
		delete(p.subs, sub)
		close(sub.ch)
	}
}

// broadcast fans a chunk out to live subscribers, dropping any that have fallen
// behind their bounded channel (they recover via the retained buffer). Mirrors
// broadcast::Sender::send + Lagged recovery.
func (p *Process) broadcast(chunk []byte) {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	for sub := range p.subs {
		select {
		case sub.ch <- cloneBytes(chunk):
		default:
			delete(p.subs, sub)
			close(sub.ch)
		}
	}
}

// closeSubscribers closes all live subscriber channels. Used when output ends.
func (p *Process) closeSubscribers() {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	for sub := range p.subs {
		delete(p.subs, sub)
		close(sub.ch)
	}
}

// HasExited reports whether the process has exited. Mirrors has_exited.
func (p *Process) HasExited() bool {
	p.stateMu.Lock()
	exited := p.state.hasExited
	p.stateMu.Unlock()
	if exited {
		return true
	}
	if p.local != nil {
		return p.local.HasExited()
	}
	return false
}

// ExitCode returns the exit code and whether it is known. Mirrors exit_code
// (Option<i32>).
func (p *Process) ExitCode() (int, bool) {
	p.stateMu.Lock()
	code := p.state.exitCode
	p.stateMu.Unlock()
	if code != nil {
		return *code, true
	}
	if p.local != nil {
		return p.local.ExitCode()
	}
	return 0, false
}

// SandboxType returns the sandbox type the process was spawned under. Mirrors
// sandbox_type.
func (p *Process) SandboxType() sandbox.SandboxType { return p.sandboxType }

// FailureMessage returns the recorded failure message, if any. Mirrors
// failure_message.
func (p *Process) FailureMessage() (string, bool) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.state.failureMessage == nil {
		return "", false
	}
	return *p.state.failureMessage, true
}

// Write sends bytes to the child's stdin. Mirrors write.
func (p *Process) Write(ctx context.Context, data []byte) error {
	if p.local != nil {
		if p.writer == nil {
			return newWriteToStdinError()
		}
		select {
		case p.writer <- cloneBytes(data):
			return nil
		case <-ctx.Done():
			return newWriteToStdinError()
		case <-p.cancellation.cancelled():
			return newWriteToStdinError()
		}
	}

	resp, err := p.remote.Write(ctx, cloneBytes(data))
	if err != nil {
		return newProcessFailedError(err.Error())
	}
	switch resp.Status {
	case execserver.WriteStatusAccepted:
		return nil
	case execserver.WriteStatusUnknownProcess, execserver.WriteStatusStdinClosed:
		p.markExited(p.snapshotExitCode())
		p.cancellation.cancel()
		return newWriteToStdinError()
	default: // Starting
		return newWriteToStdinError()
	}
}

// Resize resizes the PTY for a local interactive session. It returns an error
// for non-PTY sessions (matching pty.Process.Resize) and is a no-op for
// exec-server processes, which do not expose a resize control.
func (p *Process) Resize(size pty.TerminalSize) error {
	if p.local != nil {
		return p.local.Resize(size)
	}
	return nil
}

// Terminate kills the child and closes output. It is idempotent. Mirrors
// terminate.
func (p *Process) Terminate() {
	p.outputClosed.Store(true)
	p.outputClosedNotify.notifyWaiters()
	if p.local != nil {
		p.local.Terminate()
	}
	if p.remote != nil {
		remote := p.remote
		go func() { _ = remote.Terminate(context.Background()) }()
	}
	p.cancellation.cancel()
	p.closeSubscribers()
}

// FailAndTerminate records a failure message (if none is set yet) and then
// terminates the process. Mirrors fail_and_terminate.
func (p *Process) FailAndTerminate(message string) {
	p.stateMu.Lock()
	if p.state.failureMessage == nil {
		p.state = p.state.failed(message)
	}
	p.stateMu.Unlock()
	p.Terminate()
}

// markExited records the exit code and marks the process exited, then cancels.
// Mirrors signal_exit.
func (p *Process) markExited(exitCode *int) {
	p.stateMu.Lock()
	p.state = p.state.exited(exitCode)
	p.stateMu.Unlock()
}

// snapshotExitCode reads the current exit code without falling back to the
// transport, used when synthesizing an exited state on write failure.
func (p *Process) snapshotExitCode() *int {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.state.exitCode
}

// recordFailure records a failure message unconditionally and cancels output.
func (p *Process) recordFailure(message string) {
	p.stateMu.Lock()
	p.state = p.state.failed(message)
	p.stateMu.Unlock()
	p.outputClosed.Store(true)
	p.outputClosedNotify.notifyWaiters()
	p.cancellation.cancel()
}

// pushOutput appends a chunk to the retained buffer, fans it out to subscribers,
// and wakes pollers. Used by the transport output tasks.
func (p *Process) pushOutput(chunk []byte) {
	p.outputBufferMu.Lock()
	p.outputBuffer.PushChunk(chunk)
	p.outputBufferMu.Unlock()
	p.broadcast(chunk)
	p.outputNotify.notifyWaiters()
}

// fromSpawned builds a Process from a directly-spawned PTY session, draining its
// combined output into the buffer and watching for exit. Mirrors from_spawned.
func fromSpawned(spawned *pty.SpawnedProcess, sandboxType sandbox.SandboxType) (*Process, error) {
	managed := newProcess(sandboxType)
	managed.local = spawned.Process
	managed.writer = spawned.Process.Stdin()

	combined := pty.CombineOutput(spawned.Stdout, spawned.Stderr)
	go managed.runLocalOutputTask(combined)

	// Wait briefly for an immediate exit so short-lived commands report their
	// exit code synchronously, matching from_spawned's early-exit grace window.
	select {
	case code, ok := <-spawned.Exit:
		if ok {
			c := code
			managed.markExited(&c)
		} else {
			managed.markExited(nil)
		}
		managed.cancellation.cancel()
		return managed, nil
	case <-time.After(earlyExitGracePeriod):
	}

	go func() {
		code, ok := <-spawned.Exit
		if ok {
			c := code
			managed.markExited(&c)
		} else {
			managed.markExited(nil)
		}
		managed.cancellation.cancel()
	}()

	return managed, nil
}

// runLocalOutputTask drains the combined PTY output channel into the buffer.
// Mirrors spawn_local_output_task.
func (p *Process) runLocalOutputTask(combined <-chan []byte) {
	for chunk := range combined {
		p.pushOutput(chunk)
	}
	p.outputClosed.Store(true)
	p.outputClosedNotify.notifyWaiters()
	p.closeSubscribers()
}

// fromExecServerStarted builds a Process from an exec-server-started process,
// draining its event stream into the buffer and watching for exit/failure.
// Mirrors from_exec_server_started (adapted to the Go event-receiver model).
func fromExecServerStarted(started execserver.StartedExecProcess, sandboxType sandbox.SandboxType) (*Process, error) {
	managed := newProcess(sandboxType)
	managed.remote = started.Process

	receiver := started.Process.SubscribeEvents()
	go managed.runExecServerOutputTask(started.Process, receiver)

	// Wait briefly for an early exit/failure so short-lived commands report
	// synchronously, matching the from_exec_server_started grace window.
	deadline := time.After(earlyExitGracePeriod)
	for {
		if managed.HasExited() {
			break
		}
		if _, failed := managed.FailureMessage(); failed {
			break
		}
		select {
		case <-deadline:
			return managed, nil
		case <-time.After(5 * time.Millisecond):
		}
	}
	return managed, nil
}

// runExecServerOutputTask consumes pushed exec-server events into the buffer and
// updates exit/failure state. Mirrors spawn_exec_server_output_task adapted to
// the Go ExecProcessEventReceiver.
func (p *Process) runExecServerOutputTask(proc execserver.ExecProcess, receiver *execserver.ExecProcessEventReceiver) {
	defer receiver.Close()
	ctx := context.Background()
	for {
		event, ok := receiver.Recv(ctx)
		if !ok {
			// Live stream closed (lagged or ended); recover via a final read so
			// no retained output is lost, then close.
			p.drainRemoteRead(ctx, proc)
			p.outputClosed.Store(true)
			p.outputClosedNotify.notifyWaiters()
			p.cancellation.cancel()
			p.closeSubscribers()
			return
		}
		switch event.Kind {
		case execserver.ExecProcessEventOutput:
			p.pushOutput(event.Output.Chunk)
		case execserver.ExecProcessEventExited:
			code := event.ExitCode
			p.markExited(&code)
		case execserver.ExecProcessEventClosed:
			p.outputClosed.Store(true)
			p.outputClosedNotify.notifyWaiters()
			p.cancellation.cancel()
			p.closeSubscribers()
			return
		case execserver.ExecProcessEventFailed:
			p.recordFailure(event.Failure)
			p.closeSubscribers()
			return
		}
	}
}

// checkForSandboxDenialWithText checks whether an exited sandboxed process was
// likely denied, building a denial error when so. Mirrors
// check_for_sandbox_denial_with_text. The core is_likely_sandbox_denied
// heuristic (exit code + output inspection) is not in this package's surface, so
// this port applies the load-bearing rule: a sandboxed process that exited
// non-zero is treated as a likely denial.
func (p *Process) checkForSandboxDenialWithText(text string) error {
	if p.sandboxType == sandbox.SandboxTypeNone || !p.HasExited() {
		return nil
	}
	exitCode := -1
	if code, ok := p.ExitCode(); ok {
		exitCode = code
	}
	if exitCode == 0 {
		return nil
	}
	snippet := truncation.FormattedTruncateText(text, truncation.TokensPolicy(UnifiedExecOutputMaxTokens))
	message := snippet
	if message == "" {
		message = fmt.Sprintf("Process exited with code %d", exitCode)
	}
	return newSandboxDeniedError(message, DeniedOutput{ExitCode: exitCode, AggregatedText: text})
}

// drainRemoteRead performs a final non-blocking read so any retained output the
// event stream missed (after a lag) is still captured.
func (p *Process) drainRemoteRead(ctx context.Context, proc execserver.ExecProcess) {
	wait := uint64(0)
	resp, err := proc.Read(ctx, nil, nil, &wait)
	if err != nil {
		return
	}
	for _, chunk := range resp.Chunks {
		p.pushOutput(chunk.Chunk)
	}
	if resp.Exited {
		p.markExited(resp.ExitCode)
	}
}
