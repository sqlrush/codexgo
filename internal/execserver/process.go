package execserver

import (
	"context"
	"sync"
)

// StartedExecProcess bundles a started process handle.
//
// Rust: `StartedExecProcess { process: Arc<dyn ExecProcess> }`.
type StartedExecProcess struct {
	Process ExecProcess
}

// ExecProcessEventKind tags an [ExecProcessEvent].
type ExecProcessEventKind int

const (
	// ExecProcessEventOutput carries stdout, stderr, or pty bytes.
	ExecProcessEventOutput ExecProcessEventKind = iota
	// ExecProcessEventExited reports the process exit status.
	ExecProcessEventExited
	// ExecProcessEventClosed means all output streams have ended.
	ExecProcessEventClosed
	// ExecProcessEventFailed means the process session cannot continue.
	ExecProcessEventFailed
)

// ExecProcessEvent is a pushed process event for consumers that follow output as
// it arrives instead of polling retained output with [ExecProcess.Read].
//
// Rust: the `ExecProcessEvent` enum. Output events carry an output chunk; Exited
// carries seq + exit code; Closed carries seq; Failed carries a message and is
// intentionally unsequenced because it is synthesized by the client on session
// or transport failure.
type ExecProcessEvent struct {
	Kind     ExecProcessEventKind
	Output   ProcessOutputChunk
	SeqNum   uint64
	ExitCode int
	Failure  string
}

// NewOutputEvent builds an Output event.
func NewOutputEvent(chunk ProcessOutputChunk) ExecProcessEvent {
	return ExecProcessEvent{Kind: ExecProcessEventOutput, Output: chunk, SeqNum: chunk.Seq}
}

// NewExitedEvent builds an Exited event.
func NewExitedEvent(seq uint64, exitCode int) ExecProcessEvent {
	return ExecProcessEvent{Kind: ExecProcessEventExited, SeqNum: seq, ExitCode: exitCode}
}

// NewClosedEvent builds a Closed event.
func NewClosedEvent(seq uint64) ExecProcessEvent {
	return ExecProcessEvent{Kind: ExecProcessEventClosed, SeqNum: seq}
}

// NewFailedEvent builds a Failed event.
func NewFailedEvent(message string) ExecProcessEvent {
	return ExecProcessEvent{Kind: ExecProcessEventFailed, Failure: message}
}

// Seq returns the ordering sequence number, or (0, false) for Failed events.
//
// Rust: `ExecProcessEvent::seq` returning Option<u64>; Failed is unsequenced.
func (e ExecProcessEvent) Seq() (uint64, bool) {
	switch e.Kind {
	case ExecProcessEventOutput, ExecProcessEventExited, ExecProcessEventClosed:
		return e.SeqNum, true
	default:
		return 0, false
	}
}

// retainedLen reports the bytes this event occupies in the bounded history.
//
// Rust: `ExecProcessEvent::retained_len`.
func (e ExecProcessEvent) retainedLen() int {
	switch e.Kind {
	case ExecProcessEventOutput:
		return len(e.Output.Chunk)
	case ExecProcessEventFailed:
		return len(e.Failure)
	default:
		return 0
	}
}

// execProcessEventLog is a bounded replay buffer plus live fan-out for pushed
// process events. New subscribers first drain the replay history, then continue
// on the live stream. History is bounded by event count and retained bytes.
//
// Rust: `ExecProcessEventLog` over a tokio broadcast channel. The Go port uses a
// mutex-guarded history plus a set of per-subscriber channels, which preserves
// the same observable contract for the in-process backend.
type execProcessEventLog struct {
	mu            sync.Mutex
	events        []ExecProcessEvent
	retainedBytes int
	eventCapacity int
	byteCapacity  int
	subscribers   map[*execEventSubscriber]struct{}
}

type execEventSubscriber struct {
	ch chan ExecProcessEvent
}

// newExecProcessEventLog builds a log bounded by the given event and byte caps.
func newExecProcessEventLog(eventCapacity, byteCapacity int) *execProcessEventLog {
	return &execProcessEventLog{
		eventCapacity: eventCapacity,
		byteCapacity:  byteCapacity,
		subscribers:   make(map[*execEventSubscriber]struct{}),
	}
}

// publish records the event in the bounded history and fans it out to live
// subscribers. Mirrors `ExecProcessEventLog::publish`.
func (l *execProcessEventLog) publish(event ExecProcessEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.retainedBytes += event.retainedLen()
	l.events = append(l.events, event)
	for len(l.events) > l.eventCapacity || l.retainedBytes > l.byteCapacity {
		if len(l.events) == 0 {
			break
		}
		evicted := l.events[0]
		l.events = l.events[1:]
		l.retainedBytes -= evicted.retainedLen()
		if l.retainedBytes < 0 {
			l.retainedBytes = 0
		}
	}

	for sub := range l.subscribers {
		select {
		case sub.ch <- event:
		default:
			// Subscriber fell behind the bounded live channel; drop the
			// subscriber so its receiver observes a closed channel and
			// recovers through ExecProcess.Read, matching the Rust Lagged
			// recovery contract.
			close(sub.ch)
			delete(l.subscribers, sub)
		}
	}
}

// subscribe returns a receiver that first drains a snapshot of the replay
// history and then continues on the live stream. Mirrors
// `ExecProcessEventLog::subscribe`.
func (l *execProcessEventLog) subscribe() *ExecProcessEventReceiver {
	l.mu.Lock()
	defer l.mu.Unlock()

	replay := make([]ExecProcessEvent, len(l.events))
	copy(replay, l.events)
	sub := &execEventSubscriber{ch: make(chan ExecProcessEvent, l.eventCapacity)}
	l.subscribers[sub] = struct{}{}

	return &ExecProcessEventReceiver{
		log:    l,
		sub:    sub,
		replay: replay,
	}
}

// removeSubscriber detaches sub from the live fan-out.
func (l *execProcessEventLog) removeSubscriber(sub *execEventSubscriber) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.subscribers[sub]; ok {
		delete(l.subscribers, sub)
		close(sub.ch)
	}
}

// ExecProcessEventReceiver delivers replayed then live process events.
//
// Rust: `ExecProcessEventReceiver` (replay VecDeque + broadcast Receiver).
type ExecProcessEventReceiver struct {
	log    *execProcessEventLog
	sub    *execEventSubscriber
	replay []ExecProcessEvent
}

// EmptyEventReceiver returns a receiver that yields no events.
//
// Rust: `ExecProcessEventReceiver::empty`.
func EmptyEventReceiver() *ExecProcessEventReceiver {
	return &ExecProcessEventReceiver{}
}

// Recv returns the next replayed or live event. It returns (event, true) when an
// event is available, and (zero, false) when the live stream has closed (the
// subscriber lagged or the log is empty); callers recover through
// [ExecProcess.Read]. The context cancels the wait.
//
// Rust: `ExecProcessEventReceiver::recv` returning Result<_, RecvError>.
func (r *ExecProcessEventReceiver) Recv(ctx context.Context) (ExecProcessEvent, bool) {
	if len(r.replay) > 0 {
		event := r.replay[0]
		r.replay = r.replay[1:]
		return event, true
	}
	if r.sub == nil {
		return ExecProcessEvent{}, false
	}
	select {
	case <-ctx.Done():
		return ExecProcessEvent{}, false
	case event, ok := <-r.sub.ch:
		if !ok {
			return ExecProcessEvent{}, false
		}
		return event, true
	}
}

// Close detaches the receiver from the live fan-out. It is safe to call once.
func (r *ExecProcessEventReceiver) Close() {
	if r.log != nil && r.sub != nil {
		r.log.removeSubscriber(r.sub)
	}
}

// ExecProcess is a handle for an executor-managed process. Implementations must
// support both retained-output reads ([ExecProcess.Read]) and pushed events
// ([ExecProcess.SubscribeEvents]).
//
// Rust: the `ExecProcess` async trait. The Go port adds a leading
// [context.Context] to each method for cancellation, in place of tokio task
// cancellation.
type ExecProcess interface {
	// ProcessID returns the logical process handle.
	ProcessID() ProcessId

	// SubscribeEvents returns a receiver for pushed output and lifecycle events.
	SubscribeEvents() *ExecProcessEventReceiver

	// Read pages through retained output. afterSeq/maxBytes/waitMs are optional.
	Read(ctx context.Context, afterSeq *uint64, maxBytes *int, waitMs *uint64) (ReadResponse, error)

	// Write sends bytes to the child's stdin.
	Write(ctx context.Context, chunk []byte) (WriteResponse, error)

	// Terminate requests termination of the child.
	Terminate(ctx context.Context) error
}

// ExecBackend starts processes.
//
// Rust: the `ExecBackend` async trait.
type ExecBackend interface {
	// Start spawns the process described by params.
	Start(ctx context.Context, params ExecParams) (StartedExecProcess, error)
}
