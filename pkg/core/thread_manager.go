package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
	"github.com/sqlrush/codexgo/pkg/threadstore"
)

// ErrThreadNotFound is returned when a requested thread id is not tracked by the
// manager. It mirrors the Rust `CodexErr::ThreadNotFound`.
var ErrThreadNotFound = errors.New("core: thread not found")

// ErrThreadAlreadyRunning is returned when a spawn races with an already-running
// thread of the same id. It mirrors the Rust `CodexErr::InvalidRequest` "thread
// ... is already running" path.
var ErrThreadAlreadyRunning = errors.New("core: thread is already running")

// ErrSessionConfiguredNotFirst is returned when the first event emitted by a
// freshly spawned thread is not a SessionConfigured ack. It mirrors the Rust
// `CodexErr::SessionConfiguredNotFirstEvent`.
var ErrSessionConfiguredNotFirst = errors.New("core: first event was not SessionConfigured")

// ErrThreadStoreInvalidRequest is returned when a thread-store read rejects the
// request as malformed (e.g. an unknown rollout path). It mirrors the Rust
// `CodexErr::InvalidRequest` mapping in `thread_store_rollout_read_error`.
var ErrThreadStoreInvalidRequest = errors.New("core: invalid thread-store request")

// ThreadServicesFactory builds the per-thread [SessionServices] for a new thread.
// It is the dependency-injection seam that keeps [ThreadManager] free of the
// concrete managers (auth, models, environment, MCP, skills, plugins, hooks,
// exec, rollout). Callers (area agents, app-server, tests) provide a factory that
// constructs the appropriate concrete implementations for the given thread id and
// configuration.
//
// The factory is invoked once per spawn, before [Spawn]. It receives the resolved
// session configuration so it can, for example, wire a rollout recorder rooted at
// the configuration's CodexHome.
type ThreadServicesFactory func(ctx context.Context, threadID protocol.ThreadID, cfg SessionConfiguration) (SessionServices, error)

// ThreadIDFactory mints a fresh, unique [protocol.ThreadID] for a new thread. The
// Rust engine uses UUIDv7; callers inject a generator so core does not take a
// UUID dependency.
type ThreadIDFactory func() protocol.ThreadID

// ThreadManagerConfig bundles the dependencies for a [ThreadManager]. All fields
// are required unless documented otherwise.
type ThreadManagerConfig struct {
	// Store is the thread persistence boundary used for resume/fork/read.
	Store threadstore.ThreadStore
	// ServicesFactory builds per-thread services (dependency injection).
	ServicesFactory ThreadServicesFactory
	// NewThreadID mints fresh thread ids; required.
	NewThreadID ThreadIDFactory
	// SessionSource is the default origin classification for spawned threads.
	SessionSource rollout.SessionSource
	// InstallationID is the stable installation identifier carried into spawns.
	InstallationID string
	// Originator is recorded in session metadata (e.g. "codex_cli_rs"). Optional.
	Originator string
	// CliVersion is recorded in session metadata. Optional.
	CliVersion string
	// Now returns the current time; defaults to time.Now when nil (testability).
	Now func() time.Time
}

// StartThreadOptions controls a single thread spawn. It is a reduced port of the
// Rust `StartThreadOptions`.
type StartThreadOptions struct {
	// Configuration is the resolved session configuration for the new thread.
	Configuration SessionConfiguration
	// InitialHistory is the history the thread starts with (New/Resumed/Forked).
	InitialHistory rollout.InitialHistory
	// SessionSource overrides the manager's default source for this thread.
	SessionSource *rollout.SessionSource
	// ThreadSource is the optional analytics source classification.
	ThreadSource *rollout.ThreadSource
	// DynamicTools are the thread-defined dynamic tool specs.
	DynamicTools []protocol.DynamicToolSpec
	// PersistExtendedHistory selects extended vs. limited event persistence.
	PersistExtendedHistory bool
	// ThreadID, when set, is the id for a brand-new thread instead of one minted
	// by the manager's ThreadIDFactory. Hosts that create the thread row in their
	// store before the first turn (airush: pgstore CreateThread → later spawn)
	// pass it so the session, its rollout and the row share one identity.
	// Ignored for resumed history (the id comes from the history).
	ThreadID *protocol.ThreadID
}

// NewThread is the result of a successful spawn: the live [CodexThread], its id,
// and the SessionConfigured ack observed at startup. Mirrors the Rust `NewThread`.
type NewThread struct {
	ThreadID          protocol.ThreadID
	Thread            *CodexThread
	SessionConfigured protocol.SessionConfiguredEvent
}

// ThreadShutdownReport partitions the threads attempted during a bounded
// shutdown, mirroring the Rust `ThreadShutdownReport`.
type ThreadShutdownReport struct {
	Completed    []protocol.ThreadID
	SubmitFailed []protocol.ThreadID
	TimedOut     []protocol.ThreadID
}

// ThreadManager creates threads and maintains them in memory. It is a reduced,
// faithful port of the Rust `thread_manager::ThreadManager`: the create / fork /
// resume / rollback lifecycle over [threadstore.ThreadStore] and the rollout
// model, built around the dependency-injected [ThreadServicesFactory].
//
// STUB: the Rust ThreadManager also owns the concrete auth/models/environment/
// extension managers, analytics, attestation, the state DB, sub-agent spawn
// edges, and thread-created broadcast subscriptions. Those flows are deferred to
// the area agents that port goals/multi_agents/apps; core exposes only the
// turn-running lifecycle and the in-memory thread registry.
type ThreadManager struct {
	store           threadstore.ThreadStore
	servicesFactory ThreadServicesFactory
	newThreadID     ThreadIDFactory
	sessionSource   rollout.SessionSource
	installationID  string
	originator      string
	cliVersion      string
	now             func() time.Time

	mu      sync.Mutex
	threads map[protocol.ThreadID]*CodexThread
}

// NewThreadManager constructs a [ThreadManager], validating required fields at the
// system boundary.
func NewThreadManager(cfg ThreadManagerConfig) (*ThreadManager, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("core: ThreadManagerConfig.Store is required")
	}
	if cfg.ServicesFactory == nil {
		return nil, fmt.Errorf("core: ThreadManagerConfig.ServicesFactory is required")
	}
	if cfg.NewThreadID == nil {
		return nil, fmt.Errorf("core: ThreadManagerConfig.NewThreadID is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	source := cfg.SessionSource
	if source.Kind == "" {
		source = rollout.DefaultSessionSource()
	}
	return &ThreadManager{
		store:           cfg.Store,
		servicesFactory: cfg.ServicesFactory,
		newThreadID:     cfg.NewThreadID,
		sessionSource:   source,
		installationID:  cfg.InstallationID,
		originator:      cfg.Originator,
		cliVersion:      cfg.CliVersion,
		now:             now,
		threads:         make(map[protocol.ThreadID]*CodexThread),
	}, nil
}

// SessionSource returns the manager's default session source.
func (m *ThreadManager) SessionSource() rollout.SessionSource { return m.sessionSource }

// StartThread spawns a brand-new thread with no history, mirroring the Rust
// `ThreadManager::start_thread`.
func (m *ThreadManager) StartThread(ctx context.Context, cfg SessionConfiguration) (NewThread, error) {
	return m.StartThreadWithOptions(ctx, StartThreadOptions{
		Configuration:  cfg,
		InitialHistory: rollout.NewInitialHistory(),
	})
}

// StartThreadWithOptions spawns a new thread with the supplied options, mirroring
// the Rust `ThreadManager::start_thread_with_options`.
func (m *ThreadManager) StartThreadWithOptions(ctx context.Context, options StartThreadOptions) (NewThread, error) {
	return m.spawnThread(ctx, options, nil)
}

// ResumeThreadFromRollout resumes a thread by reading its persisted history from
// the rollout path and replaying it, mirroring the Rust
// `ThreadManager::resume_thread_from_rollout`.
func (m *ThreadManager) ResumeThreadFromRollout(ctx context.Context, cfg SessionConfiguration, rolloutPath string) (NewThread, error) {
	history, err := m.initialHistoryFromRolloutPath(ctx, rolloutPath)
	if err != nil {
		return NewThread{}, err
	}
	return m.ResumeThreadWithHistory(ctx, cfg, history)
}

// ResumeThreadByID resumes a thread by reading its persisted history from the
// store keyed by thread id (rather than rollout path), then replaying it under
// the supplied session source. It is the resume-by-id path the multi-agent
// control plane uses to reopen a closed sub-agent, mirroring the Rust
// `resume_single_agent_from_rollout` store read by `ThreadId`.
func (m *ThreadManager) ResumeThreadByID(ctx context.Context, cfg SessionConfiguration, threadID protocol.ThreadID, source rollout.SessionSource) (NewThread, error) {
	stored, err := m.store.ReadThread(ctx, threadstore.ReadThreadParams{
		ThreadID:        threadID,
		IncludeArchived: true,
		IncludeHistory:  true,
	})
	if err != nil {
		return NewThread{}, mapStoreReadError(err)
	}
	history, err := storedThreadToInitialHistory(stored, nil)
	if err != nil {
		return NewThread{}, err
	}
	options := StartThreadOptions{
		Configuration:  cfg,
		InitialHistory: history,
		SessionSource:  &source,
		ThreadSource:   subagentThreadSourceForResume(source),
	}
	return m.spawnThread(ctx, options, nil)
}

// subagentThreadSourceForResume returns the subagent analytics source for a
// thread-spawn resume, matching the spawn path's classification.
func subagentThreadSourceForResume(source rollout.SessionSource) *rollout.ThreadSource {
	if source.Kind == rollout.SessionSourceKindSubAgent {
		ts := rollout.ThreadSourceSubagent
		return &ts
	}
	return nil
}

// ResumeThreadWithHistory resumes a thread from already-loaded history, mirroring
// the Rust `ThreadManager::resume_thread_with_history`.
func (m *ThreadManager) ResumeThreadWithHistory(ctx context.Context, cfg SessionConfiguration, history rollout.InitialHistory) (NewThread, error) {
	source := m.sessionSource
	if rs := resumedSessionSource(history); rs != nil {
		source = *rs
	}
	threadSource := history.GetResumedThreadSource()
	options := StartThreadOptions{
		Configuration:  cfg,
		InitialHistory: history,
		SessionSource:  &source,
		ThreadSource:   threadSource,
	}
	return m.spawnThread(ctx, options, nil)
}

// ForkThread forks an existing thread by reading its persisted history from the
// rollout path, snapshotting it per snapshot, and spawning a new thread with a
// fresh id, mirroring the Rust `ThreadManager::fork_thread`.
func (m *ThreadManager) ForkThread(ctx context.Context, snapshot ForkSnapshot, cfg SessionConfiguration, rolloutPath string, threadSource *rollout.ThreadSource, persistExtendedHistory bool) (NewThread, error) {
	history, err := m.initialHistoryFromRolloutPath(ctx, rolloutPath)
	if err != nil {
		return NewThread{}, err
	}
	return m.ForkThreadFromHistory(ctx, snapshot, cfg, history, threadSource, persistExtendedHistory)
}

// ForkThreadFromHistory forks an existing thread from already-loaded store
// history, mirroring the Rust `ThreadManager::fork_thread_from_history`.
func (m *ThreadManager) ForkThreadFromHistory(ctx context.Context, snapshot ForkSnapshot, cfg SessionConfiguration, history rollout.InitialHistory, threadSource *rollout.ThreadSource, persistExtendedHistory bool) (NewThread, error) {
	// forkedFromID describes this history's existing lineage. When forking a
	// resumed thread, the child copies the resumed thread itself; when forking an
	// already-forked history, it copies that history's forked_from_id. Mirrors the
	// Rust `fork_thread_with_initial_history`.
	var forkedFromID *protocol.ThreadID
	switch history.Kind {
	case rollout.InitialHistoryKindResumed:
		if history.Resumed != nil {
			id := history.Resumed.ConversationID
			forkedFromID = &id
		}
	case rollout.InitialHistoryKindForked:
		forkedFromID = history.ForkedFromID()
	}

	forked := forkHistoryFromSnapshot(snapshot, history)
	options := StartThreadOptions{
		Configuration:          cfg,
		InitialHistory:         forked,
		ThreadSource:           threadSource,
		PersistExtendedHistory: persistExtendedHistory,
	}
	return m.spawnThread(ctx, options, forkedFromID)
}

// Rollback removes the last numTurns user turns from a thread's persisted history
// and resumes the thread on the truncated prefix, returning the new live thread.
// It is the core of the Rust `Op::ThreadRollback` flow (here surfaced as a
// manager method).
//
// numTurns is the number of trailing user-message boundaries to drop. A value of
// zero is a no-op resume of the full history.
//
// STUB: the Rust rollback additionally rewrites the live rollout in place and
// emits a ThreadRolledBack event; here rollback is modeled as a read + truncate +
// resume on a forked prefix, which yields an equivalent in-memory history.
func (m *ThreadManager) Rollback(ctx context.Context, cfg SessionConfiguration, rolloutPath string, numTurns uint32) (NewThread, error) {
	history, err := m.initialHistoryFromRolloutPath(ctx, rolloutPath)
	if err != nil {
		return NewThread{}, err
	}
	truncated := rollbackHistory(history, int(numTurns))
	return m.ResumeThreadWithHistory(ctx, cfg, truncated)
}

// rollbackHistory drops the trailing numTurns user-message boundaries from a
// resumed history, returning the truncated prefix as a forked history. When
// numTurns is zero or out of range the full history is returned unchanged.
func rollbackHistory(history rollout.InitialHistory, numTurns int) rollout.InitialHistory {
	if numTurns <= 0 {
		return history
	}
	items := history.GetRolloutItems()
	positions := userMessagePositionsInRollout(items)
	if len(positions) == 0 {
		return history
	}
	target := len(positions) - numTurns
	if target <= 0 {
		return rollout.NewInitialHistory()
	}
	cut := positions[target]
	rolled := append([]rollout.RolloutItem(nil), items[:cut]...)
	if len(rolled) == 0 {
		return rollout.NewInitialHistory()
	}
	if history.Kind == rollout.InitialHistoryKindResumed && history.Resumed != nil {
		return rollout.InitialHistory{
			Kind: rollout.InitialHistoryKindResumed,
			Resumed: &rollout.ResumedHistory{
				ConversationID: history.Resumed.ConversationID,
				History:        rolled,
				RolloutPath:    clonePtr(history.Resumed.RolloutPath),
			},
		}
	}
	return rollout.InitialHistory{Kind: rollout.InitialHistoryKindForked, Forked: rolled}
}

// GetThread returns the tracked thread for id, or [ErrThreadNotFound], mirroring
// the Rust `ThreadManager::get_thread`. Internal-source threads are hidden.
func (m *ThreadManager) GetThread(id protocol.ThreadID) (*CodexThread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	thread, ok := m.threads[id]
	if !ok || thread.sessionSource.IsInternal() {
		return nil, fmt.Errorf("%w: %s", ErrThreadNotFound, id)
	}
	return thread, nil
}

// RemoveThread removes id from the in-memory registry, returning the thread when
// present, mirroring the Rust `ThreadManager::remove_thread`.
func (m *ThreadManager) RemoveThread(id protocol.ThreadID) *CodexThread {
	m.mu.Lock()
	defer m.mu.Unlock()
	thread := m.threads[id]
	delete(m.threads, id)
	return thread
}

// ListThreadIDs returns the ids of all tracked, externally-visible threads,
// mirroring the Rust `ThreadManager::list_thread_ids`.
func (m *ThreadManager) ListThreadIDs() []protocol.ThreadID {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]protocol.ThreadID, 0, len(m.threads))
	for id, thread := range m.threads {
		if thread.sessionSource.IsInternal() {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids
}

// ShutdownAllThreadsBounded attempts to shut down every tracked thread within
// timeout, removing completed threads and partitioning the outcome. It mirrors
// the Rust `ThreadManager::shutdown_all_threads_bounded`.
func (m *ThreadManager) ShutdownAllThreadsBounded(ctx context.Context, timeout time.Duration) ThreadShutdownReport {
	m.mu.Lock()
	pending := make(map[protocol.ThreadID]*CodexThread, len(m.threads))
	for id, thread := range m.threads {
		pending[id] = thread
	}
	m.mu.Unlock()

	report := ThreadShutdownReport{}
	for id, thread := range pending {
		shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
		err := thread.ShutdownAndWait(shutdownCtx)
		cancel()
		switch {
		case err == nil:
			report.Completed = append(report.Completed, id)
		case errors.Is(err, context.DeadlineExceeded):
			report.TimedOut = append(report.TimedOut, id)
		default:
			report.SubmitFailed = append(report.SubmitFailed, id)
		}
	}

	m.mu.Lock()
	for _, id := range report.Completed {
		delete(m.threads, id)
	}
	m.mu.Unlock()

	sortThreadIDs(report.Completed)
	sortThreadIDs(report.SubmitFailed)
	sortThreadIDs(report.TimedOut)
	return report
}

// spawnThread is the shared spawn path: it resolves the thread id and source,
// builds the per-thread services, records the session_meta + resumed/forked
// history into the rollout, spawns the [Codex], and registers the resulting
// [CodexThread]. Mirrors the Rust `ThreadManagerState::spawn_thread_with_source`
// + `finalize_thread_spawn` (turn-running subset).
func (m *ThreadManager) spawnThread(ctx context.Context, options StartThreadOptions, forkedFromID *protocol.ThreadID) (NewThread, error) {
	// Resumed-thread short-circuit: if the conversation is already tracked and
	// running, return the existing thread, mirroring the Rust resumed branch.
	if existing, ok := m.resumedRunningThread(options.InitialHistory); ok {
		return NewThread{
			ThreadID:          existing.ThreadID(),
			Thread:            existing,
			SessionConfigured: existing.SessionConfigured(),
		}, nil
	}

	threadID, isResume := m.resolveThreadID(options.InitialHistory)
	if !isResume && options.ThreadID != nil {
		threadID = *options.ThreadID
	}

	source := m.sessionSource
	if options.SessionSource != nil {
		source = *options.SessionSource
	}

	cfg := m.applyThreadIdentity(options.Configuration, forkedFromID)

	services, err := m.servicesFactory(ctx, threadID, cfg)
	if err != nil {
		return NewThread{}, fmt.Errorf("core: build thread services: %w", err)
	}

	rolloutPath, err := m.recordThreadStartup(ctx, services.RolloutRecorder, threadID, forkedFromID, source, options, cfg, isResume)
	if err != nil {
		return NewThread{}, err
	}

	spawned, err := Spawn(ctx, CodexSpawnArgs{
		ThreadID:            threadID,
		SessionID:           threadID.ToSessionID(),
		InstallationID:      m.installationID,
		Configuration:       cfg,
		Services:            services,
		ConversationHistory: options.InitialHistory,
		RolloutPath:         rolloutPath,
	})
	if err != nil {
		return NewThread{}, fmt.Errorf("core: spawn thread: %w", err)
	}

	configured, err := awaitSessionConfigured(ctx, spawned.Codex)
	if err != nil {
		_ = spawned.Codex.Shutdown(ctx)
		return NewThread{}, err
	}

	thread := newCodexThread(spawned.Codex, configured, rolloutPath, source)
	if err := m.registerThread(ctx, threadID, thread, spawned.Codex); err != nil {
		return NewThread{}, err
	}

	return NewThread{
		ThreadID:          threadID,
		Thread:            thread,
		SessionConfigured: configured,
	}, nil
}

// resumedRunningThread returns the already-tracked, running thread for a resumed
// history, mirroring the Rust resumed-thread short-circuit. It validates that a
// requested rollout path matches the tracked one.
func (m *ThreadManager) resumedRunningThread(history rollout.InitialHistory) (*CodexThread, bool) {
	if history.Kind != rollout.InitialHistoryKindResumed || history.Resumed == nil {
		return nil, false
	}
	id := history.Resumed.ConversationID
	m.mu.Lock()
	thread, ok := m.threads[id]
	m.mu.Unlock()
	if !ok {
		return nil, false
	}
	if !thread.IsRunning() {
		m.RemoveThread(id)
		return nil, false
	}
	return thread, true
}

// resolveThreadID returns the thread id to spawn under and whether this is a
// resume. A resumed history reuses its recorded conversation id; everything else
// mints a fresh id.
func (m *ThreadManager) resolveThreadID(history rollout.InitialHistory) (protocol.ThreadID, bool) {
	if history.Kind == rollout.InitialHistoryKindResumed && history.Resumed != nil {
		return history.Resumed.ConversationID, true
	}
	return m.newThreadID(), false
}

// applyThreadIdentity returns a configuration carrying the forked-from lineage
// and any thread-source classification, without mutating the input (immutability).
func (m *ThreadManager) applyThreadIdentity(cfg SessionConfiguration, forkedFromID *protocol.ThreadID) SessionConfiguration {
	if forkedFromID == nil {
		return cfg
	}
	next := cfg.clone()
	next.ForkedFromThreadID = clonePtr(forkedFromID)
	return next
}

// registerThread inserts the thread into the registry, shutting it down on a
// duplicate-id race, mirroring the Rust `finalize_thread_spawn` vacancy check.
func (m *ThreadManager) registerThread(ctx context.Context, id protocol.ThreadID, thread *CodexThread, codex *Codex) error {
	m.mu.Lock()
	if _, exists := m.threads[id]; exists {
		m.mu.Unlock()
		_ = codex.Shutdown(ctx)
		return fmt.Errorf("%w: %s", ErrThreadAlreadyRunning, id)
	}
	m.threads[id] = thread
	m.mu.Unlock()
	return nil
}

// recordThreadStartup writes the session_meta line and any resumed/forked history
// into the rollout (when a recorder is present), returning the rollout path
// reported to the SessionConfigured event. It mirrors the Rust session-startup
// rollout writes that prepend the session_meta before the first turn.
func (m *ThreadManager) recordThreadStartup(
	ctx context.Context,
	recorder RolloutRecorder,
	threadID protocol.ThreadID,
	forkedFromID *protocol.ThreadID,
	source rollout.SessionSource,
	options StartThreadOptions,
	cfg SessionConfiguration,
	isResume bool,
) (*string, error) {
	if recorder == nil {
		return rolloutPathFromHistory(options.InitialHistory), nil
	}

	// A resumed thread already has a session_meta line in its rollout; replaying
	// it would duplicate the start record. Only write fresh metadata for new and
	// forked threads, mirroring the Rust resume-vs-create split.
	if isResume {
		if err := recorder.Flush(ctx); err != nil {
			return nil, fmt.Errorf("core: flush resumed rollout: %w", err)
		}
		return rolloutPathFromHistory(options.InitialHistory), nil
	}

	meta := m.buildSessionMeta(threadID, forkedFromID, source, options, cfg)
	items := []rollout.RolloutItem{rollout.NewSessionMetaItem(meta)}
	if forked := options.InitialHistory.GetRolloutItems(); len(forked) > 0 {
		items = append(items, forked...)
	}
	if err := recorder.Record(ctx, items); err != nil {
		return nil, fmt.Errorf("core: record session metadata: %w", err)
	}
	if err := recorder.Flush(ctx); err != nil {
		return nil, fmt.Errorf("core: flush session metadata: %w", err)
	}
	return rolloutPathFromHistory(options.InitialHistory), nil
}

// buildSessionMeta constructs the session_meta line recorded at thread start,
// mirroring the Rust session-metadata construction (turn-running subset).
func (m *ThreadManager) buildSessionMeta(
	threadID protocol.ThreadID,
	forkedFromID *protocol.ThreadID,
	source rollout.SessionSource,
	options StartThreadOptions,
	cfg SessionConfiguration,
) rollout.SessionMetaLine {
	provider := cfg.ProviderID
	var baseInstructions *rollout.BaseInstructions
	if cfg.BaseInstructions != "" {
		baseInstructions = &rollout.BaseInstructions{Text: cfg.BaseInstructions}
	}
	var dynamicTools *[]protocol.DynamicToolSpec
	if len(options.DynamicTools) > 0 {
		tools := append([]protocol.DynamicToolSpec(nil), options.DynamicTools...)
		dynamicTools = &tools
	}
	meta := rollout.SessionMeta{
		ID:               threadID,
		ForkedFromID:     clonePtr(forkedFromID),
		Timestamp:        m.now().UTC().Format(time.RFC3339),
		Cwd:              cfg.Cwd,
		Originator:       m.originator,
		CliVersion:       m.cliVersion,
		Source:           source,
		ThreadSource:     options.ThreadSource,
		ModelProvider:    &provider,
		BaseInstructions: baseInstructions,
		DynamicTools:     dynamicTools,
	}
	return rollout.SessionMetaLine{Meta: meta}
}

// initialHistoryFromRolloutPath reads a stored thread by rollout path and converts
// it into a resumed initial history, mirroring the Rust
// `ThreadManager::initial_history_from_rollout_path`.
func (m *ThreadManager) initialHistoryFromRolloutPath(ctx context.Context, rolloutPath string) (rollout.InitialHistory, error) {
	stored, err := m.store.ReadThreadByRolloutPath(ctx, threadstore.ReadThreadByRolloutPathParams{
		RolloutPath:     rolloutPath,
		IncludeArchived: true,
		IncludeHistory:  true,
	})
	if err != nil {
		return rollout.InitialHistory{}, mapStoreReadError(err)
	}
	return storedThreadToInitialHistory(stored, &rolloutPath)
}

// storedThreadToInitialHistory converts a stored thread into a resumed initial
// history, mirroring the Rust `stored_thread_to_initial_history`.
func storedThreadToInitialHistory(stored threadstore.StoredThread, rolloutPath *string) (rollout.InitialHistory, error) {
	if stored.History == nil {
		return rollout.InitialHistory{}, fmt.Errorf("core: thread %s did not include persisted history", stored.ThreadID)
	}
	path := rolloutPath
	if path == nil {
		path = stored.RolloutPath
	}
	return rollout.InitialHistory{
		Kind: rollout.InitialHistoryKindResumed,
		Resumed: &rollout.ResumedHistory{
			ConversationID: stored.ThreadID,
			History:        append([]rollout.RolloutItem(nil), stored.History.Items...),
			RolloutPath:    clonePtr(path),
		},
	}, nil
}

// awaitSessionConfigured consumes the first event from a freshly spawned codex
// and asserts it is the SessionConfigured ack, mirroring the Rust
// `finalize_thread_spawn` event check.
func awaitSessionConfigured(ctx context.Context, codex *Codex) (protocol.SessionConfiguredEvent, error) {
	ev, err := codex.NextEvent(ctx)
	if err != nil {
		return protocol.SessionConfiguredEvent{}, fmt.Errorf("core: await SessionConfigured: %w", err)
	}
	if ev.ID != InitialSubmitID || ev.Msg.Type != protocol.EventMsgKindSessionConfigured || ev.Msg.SessionConfigured == nil {
		return protocol.SessionConfiguredEvent{}, ErrSessionConfiguredNotFirst
	}
	return *ev.Msg.SessionConfigured, nil
}

// resumedSessionSource returns the session source recorded in a resumed history's
// session metadata, if any.
func resumedSessionSource(history rollout.InitialHistory) *rollout.SessionSource {
	meta := history.GetResumedSessionMeta()
	if meta == nil {
		return nil
	}
	src := meta.Source
	return &src
}

// rolloutPathFromHistory extracts the rollout path recorded in a resumed history,
// when present.
func rolloutPathFromHistory(history rollout.InitialHistory) *string {
	if history.Kind == rollout.InitialHistoryKindResumed && history.Resumed != nil {
		return clonePtr(history.Resumed.RolloutPath)
	}
	return nil
}

// mapStoreReadError maps thread-store read failures onto core errors, mirroring
// the Rust `thread_store_rollout_read_error` three-way mapping: ThreadNotFound
// and InvalidRequest are surfaced as their dedicated sentinels, while every
// other failure is wrapped as a fatal read error.
func mapStoreReadError(err error) error {
	var storeErr *threadstore.Error
	if errors.As(err, &storeErr) {
		switch storeErr.Kind {
		case threadstore.ErrorKindThreadNotFound:
			return fmt.Errorf("%w: %s", ErrThreadNotFound, storeErr.ThreadID)
		case threadstore.ErrorKindInvalidRequest:
			return fmt.Errorf("%w: %s", ErrThreadStoreInvalidRequest, storeErr.Message)
		}
	}
	return fmt.Errorf("core: read thread by rollout path: %w", err)
}

// sortThreadIDs sorts thread ids by their string form for stable reporting.
func sortThreadIDs(ids []protocol.ThreadID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
}
