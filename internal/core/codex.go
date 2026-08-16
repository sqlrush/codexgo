package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// InitialSubmitID is the submission id used for events emitted before any client
// submission (e.g. SessionConfigured). Mirrors the Rust `INITIAL_SUBMIT_ID`
// (the empty string).
const InitialSubmitID = ""

// SubmissionChannelCapacity bounds the submission queue, mirroring the Rust
// `SUBMISSION_CHANNEL_CAPACITY`.
const SubmissionChannelCapacity = 512

// ErrInternalAgentDied is returned when the SQ/EQ loop has terminated and can no
// longer accept submissions or produce events. It mirrors the Rust
// `CodexErr::InternalAgentDied`.
var ErrInternalAgentDied = errors.New("core: internal agent loop has terminated")

// CodexSpawnArgs bundles everything needed to spawn a [Codex]. It is the Go
// analogue of the Rust `CodexSpawnArgs`, reduced to the turn-running subset and
// built around the injected manager interfaces (dependency injection).
//
// Required fields: ThreadID, SessionID, Configuration, and Services. The
// remaining fields default to sensible zero values.
type CodexSpawnArgs struct {
	// ThreadID is the identity for the new thread. If zero, the caller is
	// expected to have generated one; core does not mint UUIDs itself in this
	// foundation (STUB: id generation is the caller's responsibility).
	ThreadID protocol.ThreadID
	// SessionID is the shared identity; when zero it is derived from ThreadID.
	SessionID protocol.SessionID

	// InstallationID is the stable installation identifier.
	InstallationID string

	// Configuration is the initial session configuration.
	Configuration SessionConfiguration

	// Services holds the injected manager dependencies.
	Services SessionServices

	// ConversationHistory is the initial history (new, resumed, or forked).
	ConversationHistory rollout.InitialHistory

	// RolloutPath, when non-nil, is reported in the SessionConfigured event.
	RolloutPath *string
}

// CodexSpawnOk is returned by [Spawn]: the live [Codex] handle plus the thread
// id assigned to it. Mirrors the Rust `CodexSpawnOk`.
type CodexSpawnOk struct {
	Codex    *Codex
	ThreadID protocol.ThreadID
}

// Codex is the high-level interface to the agent engine. It operates as a queue
// pair: callers push [protocol.Op] submissions and receive [protocol.Event]
// values. Mirrors the Rust `Codex`.
type Codex struct {
	txSub   chan protocol.Submission
	rxEvent chan protocol.Event
	session *Session

	// loopDone is closed when the submission loop exits, so multiple callers can
	// wait for shutdown (mirrors the shared session-loop termination future).
	loopDone chan struct{}

	// closeOnce guards txSub closure on shutdown.
	closeOnce sync.Once
	closed    bool
	closeMu   sync.Mutex
}

// Spawn creates a new [Codex] and initializes its session, then starts the
// background submission loop. It mirrors the Rust `Codex::spawn`.
//
// The returned handle's submission loop runs until an [protocol.OpShutdown] is
// submitted (or the supplied context is cancelled).
func Spawn(ctx context.Context, args CodexSpawnArgs) (CodexSpawnOk, error) {
	if err := validateSpawnArgs(args); err != nil {
		return CodexSpawnOk{}, err
	}

	sessionID := args.SessionID
	if sessionID == (protocol.SessionID{}) {
		sessionID = args.ThreadID.ToSessionID()
	}

	txSub := make(chan protocol.Submission, SubmissionChannelCapacity)
	// The event queue is unbounded in Rust (async_channel::unbounded). We use a
	// generously buffered channel and a non-blocking-with-cancellation send in
	// Session.SendEvent so the engine never deadlocks on a slow consumer.
	rxEvent := make(chan protocol.Event, SubmissionChannelCapacity)

	sessionCtx, cancel := context.WithCancel(ctx)

	sess := &Session{
		threadID:       args.ThreadID,
		sessionID:      sessionID,
		installationID: args.InstallationID,
		rolloutPath:    clonePtr(args.RolloutPath),
		services:       args.Services,
		txEvent:        rxEvent,
		state:          NewSessionState(args.Configuration),
		agentStatus:    protocol.AgentStatus{Kind: protocol.AgentStatusPendingInit},
		ctx:            sessionCtx,
		cancel:         cancel,
	}

	// Let executors that need the live session (e.g. the unified-exec background
	// watcher in core/localexec, async_watcher.rs) attach to it now that both the
	// router and the session exist.
	armSessionExecutors(sess)

	// Seed initial history into the shared store (resumed/forked) before the
	// SessionConfigured event so resume UIs see consistent state.
	seedInitialHistory(sess, args.ConversationHistory)

	// For a fresh thread (no resumed/forked history), seed the codex initial
	// context — the `<permissions instructions>` (+ `<skills_instructions>`)
	// developer message and the `<environment_context>` user message — so the
	// model sees the active sandbox/approval, available skills, and cwd from
	// turn one, like the reference binary. Gated on IncludeEnvironmentContext
	// (set by the real binary's assembly) so direct-construction unit tests are
	// unaffected.
	if args.Configuration.IncludeEnvironmentContext && len(sess.HistoryItems()) == 0 {
		var skillsInstructions *string
		if renderer, ok := args.Services.SkillsManager.(InitialSkillsRenderer); ok {
			var contextWindow *int64
			if mc := args.Services.ModelClient; mc != nil {
				contextWindow = mc.ContextWindow()
			}
			if text, ok := renderer.RenderInitialSkillsInstructions(sessionCtx, args.Configuration.Cwd, contextWindow); ok {
				skillsInstructions = &text
			}
		}
		sess.RecordItems(buildSessionInitialContext(args.Configuration, skillsInstructions))
	}

	codex := &Codex{
		txSub:    txSub,
		rxEvent:  rxEvent,
		session:  sess,
		loopDone: make(chan struct{}),
	}

	// Emit SessionConfigured first, mirroring the Rust ordering.
	emitSessionConfigured(sess, args)

	go codex.runSubmissionLoop()

	return CodexSpawnOk{Codex: codex, ThreadID: args.ThreadID}, nil
}

// validateSpawnArgs enforces the required-field contract at the system boundary.
func validateSpawnArgs(args CodexSpawnArgs) error {
	if args.ThreadID == (protocol.ThreadID{}) {
		return fmt.Errorf("core: CodexSpawnArgs.ThreadID is required")
	}
	svc := args.Services
	if svc.ModelClient == nil {
		return fmt.Errorf("core: CodexSpawnArgs.Services.ModelClient is required")
	}
	if svc.ToolRouter == nil {
		return fmt.Errorf("core: CodexSpawnArgs.Services.ToolRouter is required")
	}
	return nil
}

// seedInitialHistory loads resumed/forked rollout items into the shared history.
//
// STUB: full rollout reconstruction (turn-context replay, token-info seeding,
// previous-turn-settings derivation) is owned by the rollout-reconstruction
// area. Here we extract only the ResponseItem records so the model sees prior
// turns.
func seedInitialHistory(sess *Session, hist rollout.InitialHistory) {
	items := hist.GetRolloutItems()
	if len(items) == 0 {
		return
	}
	var responses []protocol.ResponseItem
	for _, it := range items {
		if it.Kind == rollout.RolloutItemKindResponseItem && it.ResponseItem != nil {
			responses = append(responses, *it.ResponseItem)
		}
	}
	if len(responses) > 0 {
		sess.RecordItems(responses)
	}
}

// emitSessionConfigured publishes the initial SessionConfigured event.
func emitSessionConfigured(sess *Session, args CodexSpawnArgs) {
	cfg := args.Configuration
	var forkedFrom *protocol.ThreadID
	if cfg.ForkedFromThreadID != nil {
		v := *cfg.ForkedFromThreadID
		forkedFrom = &v
	}
	ev := protocol.SessionConfiguredEvent{
		SessionID:         sess.sessionID,
		ThreadID:          sess.threadID,
		ForkedFromID:      forkedFrom,
		ThreadName:        cfg.ThreadName,
		Model:             cfg.Model(),
		ModelProviderID:   cfg.ProviderID,
		ServiceTier:       cfg.ServiceTier,
		ApprovalPolicy:    cfg.ApprovalPolicy,
		ApprovalsReviewer: cfg.ApprovalsReviewer,
		Cwd:               protocol.AbsolutePath(cfg.Cwd),
		RolloutPath:       args.RolloutPath,
	}
	if msgs := args.ConversationHistory.GetEventMsgs(); len(msgs) > 0 {
		ev.InitialMessages = &msgs
	}
	sess.SendEvent(InitialSubmitID, protocol.EventMsg{
		Type:              protocol.EventMsgKindSessionConfigured,
		SessionConfigured: &ev,
	})
}

// Session returns the underlying session (primarily for tests and area agents
// that need read access to shared state).
func (c *Codex) Session() *Session { return c.session }

// Submit wraps op in a [protocol.Submission] with a fresh id and enqueues it,
// returning the generated id. Mirrors the Rust `Codex::submit`.
func (c *Codex) Submit(op protocol.Op) (string, error) {
	id := newSubmissionID()
	sub := protocol.Submission{ID: id, Op: op}
	if err := c.SubmitWithID(sub); err != nil {
		return "", err
	}
	return id, nil
}

// SubmitWithID enqueues a pre-built submission. Prefer [Codex.Submit] so core
// owns id generation. Mirrors the Rust `Codex::submit_with_id`.
func (c *Codex) SubmitWithID(sub protocol.Submission) error {
	c.closeMu.Lock()
	closed := c.closed
	c.closeMu.Unlock()
	if closed {
		return ErrInternalAgentDied
	}
	select {
	case c.txSub <- sub:
		return nil
	case <-c.loopDone:
		return ErrInternalAgentDied
	case <-c.session.ctx.Done():
		return ErrInternalAgentDied
	}
}

// NextEvent blocks until the next event is available, honoring ctx for
// cancellation. It returns [ErrInternalAgentDied] once the loop has terminated
// and the event queue is drained. Mirrors the Rust `Codex::next_event`.
func (c *Codex) NextEvent(ctx context.Context) (protocol.Event, error) {
	select {
	case ev, ok := <-c.rxEvent:
		if !ok {
			return protocol.Event{}, ErrInternalAgentDied
		}
		return ev, nil
	case <-ctx.Done():
		return protocol.Event{}, ctx.Err()
	}
}

// Shutdown submits an [protocol.OpShutdown] and waits for the submission loop to
// terminate. It is safe to call multiple times. Mirrors the Rust
// `Codex::shutdown_and_wait`.
func (c *Codex) Shutdown(ctx context.Context) error {
	err := c.SubmitWithID(protocol.Submission{ID: newSubmissionID(), Op: protocol.Op{Type: protocol.OpShutdown}})
	if err != nil && !errors.Is(err, ErrInternalAgentDied) {
		return err
	}
	select {
	case <-c.loopDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// AgentStatus returns the latest known agent status.
func (c *Codex) AgentStatus() protocol.AgentStatus { return c.session.AgentStatus() }

// SubscribeAgentStatus subscribes to this thread's agent-status transitions,
// forwarding to [Session.SubscribeAgentStatus]. Mirrors the watch-channel
// subscription `AgentControl` reaches via the thread.
func (c *Codex) SubscribeAgentStatus() (protocol.AgentStatus, <-chan protocol.AgentStatus, func()) {
	return c.session.SubscribeAgentStatus()
}

// isClosed reports whether the submission queue has been closed (i.e. the loop
// has been shut down). It mirrors the Rust `tx_sub.is_closed()` check used by
// `CodexThread::is_running`.
func (c *Codex) isClosed() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return true
	}
	select {
	case <-c.loopDone:
		return true
	default:
		return false
	}
}

// newSubmissionID generates a unique submission id. The Rust engine uses
// UUIDv7; here we use a monotonic counter combined with a process-unique prefix.
// STUB: swap for a real UUIDv7 generator when one lands in a shared util.
func newSubmissionID() string {
	n := submissionCounter.Add(1)
	return "sub-" + strconv.FormatUint(n, 10)
}

// itoa formats a uint64 as a decimal string (small helper to avoid importing
// strconv where only this is needed).
func itoa(n uint64) string { return strconv.FormatUint(n, 10) }
