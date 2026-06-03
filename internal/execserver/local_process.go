package execserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/pty"
)

// retainedOutputBytesPerProcess bounds the retained per-process output buffer.
//
// Rust: `RETAINED_OUTPUT_BYTES_PER_PROCESS = 1024 * 1024`.
const retainedOutputBytesPerProcess = 1024 * 1024

// processEventChannelCapacity bounds the pushed-event replay/live channel.
//
// Rust: `PROCESS_EVENT_CHANNEL_CAPACITY = 256`.
const processEventChannelCapacity = 256

// exitedProcessRetention is how long a closed process is retained before it is
// evicted from the registry.
//
// Rust: `EXITED_PROCESS_RETENTION` (30s in release builds).
const exitedProcessRetention = 30 * time.Second

// retainedOutputChunk is one buffered output frame.
type retainedOutputChunk struct {
	seq    uint64
	stream ExecOutputStream
	chunk  []byte
}

// runningProcess holds the live state for one spawned process.
//
// Rust: `RunningProcess`.
type runningProcess struct {
	process       *pty.Process
	tty           bool
	pipeStdin     bool
	output        []retainedOutputChunk
	retainedBytes int
	nextSeq       uint64
	exitCode      *int
	events        *execProcessEventLog
	wake          chan struct{}
	openStreams   int
	closed        bool
}

// processEntry is either a process that is starting (placeholder) or running.
//
// Rust: `ProcessEntry { Starting, Running(Box<RunningProcess>) }`.
type processEntry struct {
	running *runningProcess // nil while starting
}

// LocalProcess is the in-process [ExecBackend]: it spawns commands through
// internal/pty, retains and truncates their output, supports stdin writes, and
// emits process/output, process/exited, and process/closed notifications.
//
// Rust: `LocalProcess`. It is safe for concurrent use.
type LocalProcess struct {
	mu        sync.Mutex
	notifMu   sync.RWMutex
	notifier  NotificationSender
	processes map[string]*processEntry
	// inherited is the base environment used when an env policy filters the
	// inherited set. It defaults to the current process environment.
	inherited map[string]string
}

// NewLocalProcess builds a backend that delivers notifications via sender (nil
// means notifications are discarded) and uses the supplied inherited environment
// for policy filtering (nil means the current process environment).
//
// Rust: `LocalProcess::new`.
func NewLocalProcess(sender NotificationSender, inherited map[string]string) *LocalProcess {
	if sender == nil {
		sender = discardNotificationSender{}
	}
	if inherited == nil {
		inherited = currentEnv()
	}
	return &LocalProcess{
		notifier:  sender,
		processes: make(map[string]*processEntry),
		inherited: inherited,
	}
}

// SetNotificationSender swaps the active notification sender. A nil sender
// restores the discarding sender. Mirrors
// `LocalProcess::set_notification_sender`.
func (b *LocalProcess) SetNotificationSender(sender NotificationSender) {
	b.notifMu.Lock()
	defer b.notifMu.Unlock()
	if sender == nil {
		sender = discardNotificationSender{}
	}
	b.notifier = sender
}

// notificationSender returns the active sender.
func (b *LocalProcess) notificationSender() NotificationSender {
	b.notifMu.RLock()
	defer b.notifMu.RUnlock()
	return b.notifier
}

// Shutdown terminates every running process and clears the registry. Mirrors
// `LocalProcess::shutdown`.
func (b *LocalProcess) Shutdown() {
	b.mu.Lock()
	remaining := make([]*runningProcess, 0, len(b.processes))
	for _, entry := range b.processes {
		if entry.running != nil {
			remaining = append(remaining, entry.running)
		}
	}
	b.processes = make(map[string]*processEntry)
	b.mu.Unlock()

	for _, rp := range remaining {
		rp.process.Terminate()
	}
}

// Start spawns the process described by params and returns a handle. Mirrors
// `ExecBackend::start` over `LocalProcess::start_process`.
func (b *LocalProcess) Start(ctx context.Context, params ExecParams) (StartedExecProcess, error) {
	rp, err := b.startProcess(ctx, params)
	if err != nil {
		return StartedExecProcess{}, mapHandlerError(err)
	}
	return StartedExecProcess{
		Process: &localExecProcess{
			processID: params.ProcessID,
			backend:   b,
			events:    rp.events,
		},
	}, nil
}

// startProcess validates and spawns a process, registering it as Running.
//
// Rust: `LocalProcess::start_process`.
func (b *LocalProcess) startProcess(ctx context.Context, params ExecParams) (*runningProcess, *rpcErr) {
	id := params.ProcessID.String()
	if len(params.Argv) == 0 {
		return nil, &rpcErr{body: invalidParams("argv must not be empty")}
	}
	program := params.Argv[0]
	args := params.Argv[1:]

	b.mu.Lock()
	if _, exists := b.processes[id]; exists {
		b.mu.Unlock()
		return nil, &rpcErr{body: invalidRequest(fmt.Sprintf("process %s already exists", id))}
	}
	b.processes[id] = &processEntry{}
	b.mu.Unlock()

	env := childEnv(params, b.inherited)
	var spawned *pty.SpawnedProcess
	var spawnErr error
	switch {
	case params.Tty:
		spawned, spawnErr = pty.SpawnPTY(ctx, program, args, params.Cwd, env, params.Arg0, pty.DefaultTerminalSize())
	case params.PipeStdin:
		spawned, spawnErr = pty.SpawnPipe(ctx, program, args, params.Cwd, env, params.Arg0)
	default:
		spawned, spawnErr = pty.SpawnPipeNoStdin(ctx, program, args, params.Cwd, env, params.Arg0)
	}
	if spawnErr != nil {
		b.mu.Lock()
		if entry, ok := b.processes[id]; ok && entry.running == nil {
			delete(b.processes, id)
		}
		b.mu.Unlock()
		return nil, &rpcErr{body: internalError(spawnErr.Error())}
	}

	events := newExecProcessEventLog(processEventChannelCapacity, retainedOutputBytesPerProcess)
	rp := &runningProcess{
		process:     spawned.Process,
		tty:         params.Tty,
		pipeStdin:   params.PipeStdin,
		nextSeq:     1,
		events:      events,
		wake:        make(chan struct{}),
		openStreams: 2,
	}

	b.mu.Lock()
	b.processes[id] = &processEntry{running: rp}
	b.mu.Unlock()

	stdoutStream := ExecOutputStreamStdout
	stderrStream := ExecOutputStreamStderr
	if params.Tty {
		stdoutStream = ExecOutputStreamPty
		stderrStream = ExecOutputStreamPty
	}

	go b.streamOutput(params.ProcessID, stdoutStream, spawned.Stdout)
	go b.streamOutput(params.ProcessID, stderrStream, spawned.Stderr)
	go b.watchExit(params.ProcessID, spawned.Exit)

	return rp, nil
}

// localExecProcess is the per-handle [ExecProcess] over the shared backend.
//
// Rust: `LocalExecProcess`.
type localExecProcess struct {
	processID ProcessId
	backend   *LocalProcess
	events    *execProcessEventLog
}

func (p *localExecProcess) ProcessID() ProcessId { return p.processID }

func (p *localExecProcess) SubscribeEvents() *ExecProcessEventReceiver {
	return p.events.subscribe()
}

func (p *localExecProcess) Read(ctx context.Context, afterSeq *uint64, maxBytes *int, waitMs *uint64) (ReadResponse, error) {
	resp, err := p.backend.execRead(ctx, ReadParams{
		ProcessID: p.processID,
		AfterSeq:  afterSeq,
		MaxBytes:  maxBytes,
		WaitMs:    waitMs,
	})
	if err != nil {
		return ReadResponse{}, mapHandlerError(err)
	}
	return resp, nil
}

func (p *localExecProcess) Write(ctx context.Context, chunk []byte) (WriteResponse, error) {
	resp, err := p.backend.execWrite(ctx, WriteParams{ProcessID: p.processID, Chunk: chunk})
	if err != nil {
		return WriteResponse{}, mapHandlerError(err)
	}
	return resp, nil
}

func (p *localExecProcess) Terminate(ctx context.Context) error {
	_, err := p.backend.terminateProcess(TerminateParams{ProcessID: p.processID})
	if err != nil {
		return mapHandlerError(err)
	}
	return nil
}
