package exec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserverclient"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// EventSink consumes the JSONL events the session produces and is responsible
// for rendering them (JSONL to stdout, or human-readable text). The session is
// transport- and format-agnostic: it drives the engine and hands every produced
// [ThreadEvent] (plus the final message on shutdown) to the sink.
//
// Implementations must be safe to call from the single session goroutine only.
type EventSink interface {
	// Emit renders one JSONL event.
	Emit(ev ThreadEvent)
	// Warn renders a local (non-engine) warning as the format dictates.
	Warn(message string)
	// Finish is called once after the loop ends with the captured final message
	// (nil when none) and whether it should be persisted/printed.
	Finish(finalMessage *string, emitFinal bool)
}

// client is the subset of the in-process app-server client the session needs.
// Defining it here (where it is used) keeps the dependency surface small and
// makes the session unit-testable against a fake.
type client interface {
	RequestTyped(ctx context.Context, method string, params any, out any) error
	NextEvent(ctx context.Context) (appserverclient.ServerEvent, bool)
	Shutdown(ctx context.Context)
}

// compile-time assertion that the real in-process client satisfies the seam.
var _ client = (*appserverclient.InProcessAppServerClient)(nil)

// SessionConfig parameterizes a single exec session.
type SessionConfig struct {
	// Input is the user input submitted to start the turn (text plus any images).
	Input []appserverproto.UserInput
	// OutputSchema is an optional JSON Schema constraining the model's final
	// response; carried through to turn/start when non-nil.
	OutputSchema json.RawMessage
	// ResumeThreadID resumes an existing thread when non-empty; otherwise a new
	// thread is started.
	ResumeThreadID string
}

// Session drives one non-interactive agent turn against the in-process
// app-server client, transforming the engine event stream into JSONL events
// delivered to the sink. It is the Go analogue of run_exec_session's event loop.
type Session struct {
	client client
	proc   *JSONLProcessor
	sink   EventSink
}

// NewSession constructs a session over the supplied client and sink.
func NewSession(c client, sink EventSink) *Session {
	return &Session{client: c, proc: NewJSONLProcessor(), sink: sink}
}

// Run executes the session: handshake, thread start/resume, turn start, then the
// event loop until the turn reaches a terminal state. It returns the run outcome
// (used to derive the process exit code) or an error for setup failures.
func (s *Session) Run(ctx context.Context, cfg SessionConfig) (Outcome, error) {
	if err := s.initialize(ctx); err != nil {
		return Outcome{}, err
	}

	threadID, err := s.startOrResume(ctx, cfg.ResumeThreadID)
	if err != nil {
		return Outcome{}, err
	}

	if err := s.startTurn(ctx, threadID, cfg); err != nil {
		return Outcome{}, err
	}

	errSeen := s.loop(ctx)

	s.client.Shutdown(context.WithoutCancel(ctx))
	s.sink.Finish(s.proc.FinalMessage(), s.proc.EmitFinalOnExit())

	return Outcome{ErrorSeen: errSeen, FinalMessage: s.proc.FinalMessage()}, nil
}

// initialize performs the required initialize handshake.
func (s *Session) initialize(ctx context.Context) error {
	var resp appserverproto.InitializeResponse
	err := s.client.RequestTyped(ctx, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: clientName, Version: clientVersion},
	}, &resp)
	if err != nil {
		return fmt.Errorf("exec: initialize: %w", err)
	}
	return nil
}

// startOrResume starts a fresh thread or resumes an existing one, emitting the
// thread.started event from the returned thread aggregate.
func (s *Session) startOrResume(ctx context.Context, resumeID string) (string, error) {
	var threadAgg json.RawMessage
	if resumeID != "" {
		var resp appserverproto.ThreadResumeResponse
		if err := s.client.RequestTyped(ctx, "thread/resume", appserverproto.ThreadResumeParams{
			ThreadID: resumeID,
		}, &resp); err != nil {
			return "", fmt.Errorf("exec: thread/resume: %w", err)
		}
		threadAgg = resp.Thread
	} else {
		var resp appserverproto.ThreadStartResponse
		if err := s.client.RequestTyped(ctx, "thread/start", appserverproto.ThreadStartParams{}, &resp); err != nil {
			return "", fmt.Errorf("exec: thread/start: %w", err)
		}
		threadAgg = resp.Thread
	}

	threadID, err := threadIDFromAggregate(threadAgg)
	if err != nil {
		return "", err
	}
	s.sink.Emit(ThreadEvent{
		Kind:          ThreadEventKindThreadStarted,
		ThreadStarted: &ThreadStartedEvent{ThreadID: threadID},
	})
	return threadID, nil
}

// startTurn submits the user input, driving the model turn.
func (s *Session) startTurn(ctx context.Context, threadID string, cfg SessionConfig) error {
	var resp appserverproto.TurnStartResponse
	err := s.client.RequestTyped(ctx, "turn/start", appserverproto.TurnStartParams{
		ThreadID:     threadID,
		Input:        cfg.Input,
		OutputSchema: cfg.OutputSchema,
	}, &resp)
	if err != nil {
		return fmt.Errorf("exec: turn/start: %w", err)
	}
	return nil
}

// loop consumes the engine event stream, transforming each `codex/event` into
// JSONL events until the turn reaches a terminal state or the stream closes. It
// returns whether a fatal error or failed/interrupted turn was observed.
func (s *Session) loop(ctx context.Context) bool {
	errSeen := false
	for {
		ev, ok := s.client.NextEvent(ctx)
		if !ok {
			return errSeen
		}
		if !ev.IsCodexEvent() || ev.Notification == nil {
			continue
		}
		var coreEvent protocol.Event
		if err := json.Unmarshal(ev.Notification.Params, &coreEvent); err != nil {
			// A malformed engine event is surfaced as a warning rather than
			// crashing the run; the loop continues draining.
			s.sink.Warn(fmt.Sprintf("failed to decode engine event: %v", err))
			continue
		}

		if isTerminalError(coreEvent.Msg) {
			errSeen = true
		}

		collected := s.proc.Collect(coreEvent)
		for _, out := range collected.Events {
			s.sink.Emit(out)
		}
		if collected.Status == StatusInitiateShutdown {
			return errSeen
		}
	}
}

// isTerminalError reports whether an engine event signals a non-recoverable
// failure that should drive a non-zero exit code, mirroring the Rust error_seen
// bookkeeping (fatal error or failed/aborted turn).
func isTerminalError(msg protocol.EventMsg) bool {
	switch msg.Type {
	case protocol.EventMsgKindError:
		return true
	case protocol.EventMsgKindTurnAborted:
		return msg.TurnAborted == nil || msg.TurnAborted.Reason != protocol.TurnAbortReasonInterrupted
	default:
		return false
	}
}

// Outcome reports the result of a session run.
type Outcome struct {
	// ErrorSeen is true when a fatal error or failed turn was observed; the CLI
	// maps it to a non-zero exit code.
	ErrorSeen bool
	// FinalMessage is the agent's last message, if any.
	FinalMessage *string
}

// threadIDFromAggregate extracts the "id" field from a thread aggregate payload.
func threadIDFromAggregate(raw json.RawMessage) (string, error) {
	var agg struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &agg); err != nil {
		return "", fmt.Errorf("exec: decode thread aggregate: %w", err)
	}
	if agg.ID == "" {
		return "", fmt.Errorf("exec: thread aggregate has empty id")
	}
	return agg.ID, nil
}

const (
	// clientName / clientVersion identify this exec client in the handshake.
	clientName    = "codex_exec"
	clientVersion = "0.136.0"
)
