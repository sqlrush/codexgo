package unifiedexec

import (
	"context"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

// ptyTerminalSize builds a pty.TerminalSize from rows/cols.
func ptyTerminalSize(rows, cols uint16) pty.TerminalSize {
	return pty.TerminalSize{Rows: rows, Cols: cols}
}

// postExitCloseWaitCap bounds how long output collection lingers after the exit
// signal, waiting for the output channel to close. Mirrors
// POST_EXIT_CLOSE_WAIT_CAP.
const postExitCloseWaitCap = 50 * time.Millisecond

// processEntry is a live session retained by the manager. Mirrors ProcessEntry.
type processEntry struct {
	process     *Process
	callID      string
	processID   int
	hookCommand string
	tty         bool
	lastUsed    time.Time
}

// processStore holds live sessions plus reserved (allocated-but-not-yet-stored)
// process ids. Mirrors ProcessStore.
type processStore struct {
	processes map[int]*processEntry
	reserved  map[int]struct{}
}

// remove deletes the entry and reservation for a process id, returning the entry
// when present. Mirrors ProcessStore::remove.
func (s *processStore) remove(processID int) (*processEntry, bool) {
	delete(s.reserved, processID)
	entry, ok := s.processes[processID]
	if ok {
		delete(s.processes, processID)
	}
	return entry, ok
}

// ProcessManager keeps interactive PTY/exec-server sessions alive across tool
// calls. It allocates logical process ids, opens sessions through a [Spawner],
// reuses them via write-stdin/poll, prunes least-recently-used sessions when the
// cap is reached, and tears everything down on shutdown. Mirrors
// UnifiedExecProcessManager. All methods are safe for concurrent use.
type ProcessManager struct {
	mu                       sync.Mutex
	store                    processStore
	maxWriteStdinYieldTimeMS uint64
	spawner                  Spawner

	deterministicIDs bool

	// sessionStoredHook, when set, is invoked each time a live session is
	// persisted so the core layer can arm the background streaming/exit watcher
	// (the Rust store_process calls start_streaming_output + spawn_exit_watcher).
	sessionStoredHook SessionStoredHook
}

// StoredSession is the context handed to a [SessionStoredHook] when a live
// session is persisted. It bundles the live process, the shared transcript the
// watcher accumulates into, and the originating-call metadata the late exec-end
// carries. Mirrors the arguments store_process threads into spawn_exit_watcher.
type StoredSession struct {
	// Process is the live unified-exec process; the watcher subscribes to its
	// output and waits on its exit signal.
	Process *Process
	// Transcript is the shared buffer the streaming watcher appends to and the
	// exit watcher reads for the aggregated output. TranscriptMu guards it.
	Transcript   *HeadTailBuffer
	TranscriptMu *sync.Mutex
	// CallID, TurnID, Command, Cwd, ProcessID, StartedAt carry the
	// originating-call context for the late exec-end event.
	CallID    string
	TurnID    string
	Command   []string
	Cwd       string
	ProcessID int
	StartedAt time.Time
}

// SessionStoredHook is invoked when a live session is persisted. It hands the
// caller the live process and originating-call context so it can arm the
// background streaming/exit watcher without this package depending on core.
type SessionStoredHook func(StoredSession)

// SetSessionStoredHook installs the hook fired when a live session is persisted.
// It is safe to call before any session is opened. Passing nil clears the hook.
func (m *ProcessManager) SetSessionStoredHook(hook SessionStoredHook) {
	m.mu.Lock()
	m.sessionStoredHook = hook
	m.mu.Unlock()
}

// NewProcessManager builds a manager with the given empty-poll yield cap and
// spawner. The cap is floored at MinEmptyYieldTimeMS. Mirrors
// UnifiedExecProcessManager::new.
func NewProcessManager(maxWriteStdinYieldTimeMS uint64, spawner Spawner) *ProcessManager {
	yieldCap := maxWriteStdinYieldTimeMS
	if yieldCap < MinEmptyYieldTimeMS {
		yieldCap = MinEmptyYieldTimeMS
	}
	if spawner == nil {
		spawner = SandboxSpawner{}
	}
	return &ProcessManager{
		store: processStore{
			processes: make(map[int]*processEntry),
			reserved:  make(map[int]struct{}),
		},
		maxWriteStdinYieldTimeMS: yieldCap,
		spawner:                  spawner,
	}
}

// NewDefaultProcessManager builds a manager with the default background-terminal
// timeout and the sandbox spawner. Mirrors UnifiedExecProcessManager::default.
func NewDefaultProcessManager() *ProcessManager {
	return NewProcessManager(DefaultMaxBackgroundTerminalTimeoutMS, SandboxSpawner{})
}

// SetDeterministicProcessIDs forces deterministic, monotonically-increasing
// process ids (for tests). Mirrors set_deterministic_process_ids_for_tests.
func (m *ProcessManager) SetDeterministicProcessIDs(enabled bool) {
	m.mu.Lock()
	m.deterministicIDs = enabled
	m.mu.Unlock()
}

// AllocateProcessID reserves and returns a fresh logical process id. Mirrors
// allocate_process_id.
func (m *ProcessManager) AllocateProcessID() int {
	for {
		m.mu.Lock()
		var processID int
		if m.deterministicIDs {
			maxID := 999
			for id := range m.store.reserved {
				if id > maxID {
					maxID = id
				}
			}
			processID = maxID + 1
		} else {
			processID = rand.Intn(100_000-1_000) + 1_000
		}
		if _, taken := m.store.reserved[processID]; taken {
			m.mu.Unlock()
			continue
		}
		m.store.reserved[processID] = struct{}{}
		m.mu.Unlock()
		return processID
	}
}

// ReleaseProcessID removes a session and its reservation. Mirrors
// release_process_id.
func (m *ProcessManager) ReleaseProcessID(processID int) {
	m.mu.Lock()
	m.store.remove(processID)
	m.mu.Unlock()
}

// ExecCommand opens a new session, collects an initial output snapshot up to the
// yield deadline, persists the session when it is still alive, and returns the
// captured output. Mirrors exec_command (without the core approval/event layers,
// which the caller drives). The supplied context cancels the spawn.
func (m *ProcessManager) ExecCommand(ctx context.Context, req *ExecCommandRequest) (*Output, error) {
	process, err := m.spawner.Spawn(ctx, req.ProcessID, req)
	if err != nil {
		m.ReleaseProcessID(req.ProcessID)
		return nil, err
	}

	start := time.Now()
	// Persist live sessions before the yield wait so a cancellation cannot drop
	// the last reference and terminate the background process.
	_, exitKnown := process.ExitCode()
	processStartedAlive := !process.HasExited() && !exitKnown
	if processStartedAlive {
		m.storeProcess(process, req, start)
	}

	yieldTimeMS := clampYieldTime(req.YieldTimeMS)
	handles := process.outputHandles()
	deadline := start.Add(time.Duration(yieldTimeMS) * time.Millisecond)
	collected := collectOutputUntilDeadline(handles, deadline)
	wallTime := time.Since(start)

	text := string(collected)
	chunkID := generateChunkID()

	if message, failed := process.FailureMessage(); failed {
		m.ReleaseProcessID(req.ProcessID)
		return nil, newProcessFailedError(message)
	}

	var responseProcessID *int
	var exitCode *int
	if processStartedAlive {
		switch status := m.refreshProcessState(req.ProcessID); status.kind {
		case statusAlive:
			pid := status.processID
			responseProcessID = &pid
			exitCode = status.exitCode
		case statusExited:
			exitCode = status.exitCode
			if derr := process.checkForSandboxDenialWithText(text); derr != nil {
				return nil, derr
			}
		case statusUnknown:
			return nil, newUnknownProcessIDError(req.ProcessID)
		}
	} else {
		// Short-lived command: it already exited within the spawn grace window.
		code, ok := process.ExitCode()
		if ok {
			exitCode = &code
		}
		m.ReleaseProcessID(req.ProcessID)
		if derr := process.checkForSandboxDenialWithText(text); derr != nil {
			return nil, derr
		}
	}

	return &Output{
		ChunkID:            chunkID,
		EventCallID:        req.CallID,
		WallTime:           int64(wallTime),
		RawOutput:          collected,
		MaxOutputTokens:    req.MaxOutputTokens,
		ProcessID:          responseProcessID,
		ExitCode:           exitCode,
		OriginalTokenCount: truncation.ApproxTokenCount(text),
		HookCommand:        req.HookCommand,
	}, nil
}

// WriteStdin writes input to a live session (or polls when input is empty),
// collects output up to the yield deadline, and returns the captured output.
// Mirrors write_stdin. The supplied context cancels the write.
func (m *ProcessManager) WriteStdin(ctx context.Context, req *WriteStdinRequest) (*Output, error) {
	prepared, err := m.prepareProcessHandles(req.ProcessID)
	if err != nil {
		return nil, err
	}
	process := prepared.process

	var statusAfterWrite *processStatus
	if req.Input != "" {
		if !prepared.tty {
			return nil, newStdinClosedError()
		}
		if werr := process.Write(ctx, []byte(req.Input)); werr != nil {
			status := m.refreshProcessState(req.ProcessID)
			if status.kind == statusExited {
				s := status
				statusAfterWrite = &s
			} else if e, ok := werr.(*Error); ok && e.Kind == ErrProcessFailed {
				process.Terminate()
				m.ReleaseProcessID(req.ProcessID)
				return nil, werr
			} else {
				return nil, werr
			}
		} else {
			// Give the child a brief window to react so we capture its output.
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
			}
		}
	}

	yieldTimeMS := m.resolveWriteYieldTime(req)
	start := time.Now()
	deadline := start.Add(time.Duration(yieldTimeMS) * time.Millisecond)
	collected := collectOutputUntilDeadline(prepared.handles, deadline)
	wallTime := time.Since(start)

	text := string(collected)
	chunkID := generateChunkID()

	if message, failed := process.FailureMessage(); failed {
		m.ReleaseProcessID(req.ProcessID)
		return nil, newProcessFailedError(message)
	}

	var status processStatus
	if statusAfterWrite != nil {
		status = *statusAfterWrite
	} else {
		status = m.refreshProcessState(req.ProcessID)
	}

	var responseProcessID *int
	var exitCode *int
	switch status.kind {
	case statusAlive:
		pid := status.processID
		responseProcessID = &pid
		exitCode = status.exitCode
	case statusExited:
		exitCode = status.exitCode
	case statusUnknown:
		return nil, newUnknownProcessIDError(req.ProcessID)
	}

	return &Output{
		ChunkID:            chunkID,
		EventCallID:        prepared.callID,
		WallTime:           int64(wallTime),
		RawOutput:          collected,
		MaxOutputTokens:    req.MaxOutputTokens,
		ProcessID:          responseProcessID,
		ExitCode:           exitCode,
		OriginalTokenCount: truncation.ApproxTokenCount(text),
		HookCommand:        prepared.hookCommand,
	}, nil
}

// resolveWriteYieldTime computes the output-collection window for a write/poll.
// Empty polls use the configurable background bounds; non-empty writes keep a
// fixed max cap. Mirrors the yield_time_ms block in write_stdin.
func (m *ProcessManager) resolveWriteYieldTime(req *WriteStdinRequest) uint64 {
	timeMS := req.YieldTimeMS
	if timeMS < MinYieldTimeMS {
		timeMS = MinYieldTimeMS
	}
	if req.Input == "" {
		if timeMS < MinEmptyYieldTimeMS {
			timeMS = MinEmptyYieldTimeMS
		}
		if timeMS > m.maxWriteStdinYieldTimeMS {
			timeMS = m.maxWriteStdinYieldTimeMS
		}
		return timeMS
	}
	if timeMS > MaxYieldTimeMS {
		timeMS = MaxYieldTimeMS
	}
	return timeMS
}

// Resize resizes the PTY for a live interactive session. It is a unified-exec
// extension over the Rust subsystem, which exposes resize through the pty crate;
// it returns ErrUnknownProcessID for unknown ids.
func (m *ProcessManager) Resize(req *ResizeRequest) error {
	m.mu.Lock()
	entry, ok := m.store.processes[req.ProcessID]
	if ok {
		entry.lastUsed = time.Now()
	}
	m.mu.Unlock()
	if !ok {
		return newUnknownProcessIDError(req.ProcessID)
	}
	return entry.process.Resize(ptyTerminalSize(req.Rows, req.Cols))
}

// Kill terminates a live session and removes it from the store. It returns
// ErrUnknownProcessID for unknown ids.
func (m *ProcessManager) Kill(processID int) error {
	m.mu.Lock()
	entry, ok := m.store.remove(processID)
	m.mu.Unlock()
	if !ok {
		return newUnknownProcessIDError(processID)
	}
	entry.process.Terminate()
	return nil
}

// TerminateAllProcesses tears down every live session. Mirrors
// terminate_all_processes.
func (m *ProcessManager) TerminateAllProcesses() {
	m.mu.Lock()
	entries := make([]*processEntry, 0, len(m.store.processes))
	for _, entry := range m.store.processes {
		entries = append(entries, entry)
	}
	m.store.processes = make(map[int]*processEntry)
	m.store.reserved = make(map[int]struct{})
	m.mu.Unlock()

	for _, entry := range entries {
		entry.process.Terminate()
	}
}

// storeProcess inserts a live session, pruning an old one first when at the cap,
// then arms the background streaming/exit watcher via the session-stored hook
// (the core layer supplies the event sink). Mirrors store_process, which calls
// spawn_exit_watcher after inserting the entry.
func (m *ProcessManager) storeProcess(process *Process, req *ExecCommandRequest, startedAt time.Time) {
	entry := &processEntry{
		process:     process,
		callID:      req.CallID,
		processID:   req.ProcessID,
		hookCommand: req.HookCommand,
		tty:         req.TTY,
		lastUsed:    startedAt,
	}
	m.mu.Lock()
	pruned := pruneProcessesIfNeeded(&m.store)
	m.store.processes[req.ProcessID] = entry
	hook := m.sessionStoredHook
	m.mu.Unlock()
	if pruned != nil {
		pruned.process.Terminate()
	}

	if hook != nil {
		// Mirror the shared Arc<Mutex<HeadTailBuffer>> the Rust watcher threads
		// through start_streaming_output and spawn_exit_watcher.
		command := append([]string(nil), req.Command...)
		hook(StoredSession{
			Process:      process,
			Transcript:   NewDefaultHeadTailBuffer(),
			TranscriptMu: &sync.Mutex{},
			CallID:       req.CallID,
			TurnID:       req.TurnID,
			Command:      command,
			Cwd:          req.Cwd,
			ProcessID:    req.ProcessID,
			StartedAt:    startedAt,
		})
	}
}

// prepared bundles the handles and metadata needed for a write/poll. Mirrors
// PreparedProcessHandles.
type prepared struct {
	process     *Process
	handles     outputHandles
	callID      string
	hookCommand string
	tty         bool
}

// prepareProcessHandles looks up a live session, refreshes last-used, and returns
// its handles. Mirrors prepare_process_handles.
func (m *ProcessManager) prepareProcessHandles(processID int) (*prepared, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.store.processes[processID]
	if !ok {
		return nil, newUnknownProcessIDError(processID)
	}
	entry.lastUsed = time.Now()
	return &prepared{
		process:     entry.process,
		handles:     entry.process.outputHandles(),
		callID:      entry.callID,
		hookCommand: entry.hookCommand,
		tty:         entry.tty,
	}, nil
}

// processStatusKind tags a processStatus.
type processStatusKind int

const (
	statusAlive processStatusKind = iota
	statusExited
	statusUnknown
)

// processStatus reports whether a session is still alive or has exited (and been
// removed). Mirrors the ProcessStatus enum.
type processStatus struct {
	kind      processStatusKind
	exitCode  *int
	processID int
}

// refreshProcessState reports the current status of a session, removing it from
// the store when it has exited. Mirrors refresh_process_state.
func (m *ProcessManager) refreshProcessState(processID int) processStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.store.processes[processID]
	if !ok {
		return processStatus{kind: statusUnknown}
	}
	var exitCode *int
	if code, known := entry.process.ExitCode(); known {
		c := code
		exitCode = &c
	}
	if entry.process.HasExited() {
		m.store.remove(processID)
		return processStatus{kind: statusExited, exitCode: exitCode, processID: entry.processID}
	}
	return processStatus{kind: statusAlive, exitCode: exitCode, processID: entry.processID}
}

// pruneProcessesIfNeeded evicts one session when the store is at capacity,
// returning the evicted entry. Mirrors prune_processes_if_needed.
func pruneProcessesIfNeeded(store *processStore) *processEntry {
	if len(store.processes) < MaxUnifiedExecProcesses {
		return nil
	}
	meta := make([]pruneMeta, 0, len(store.processes))
	for id, entry := range store.processes {
		meta = append(meta, pruneMeta{id: id, lastUsed: entry.lastUsed, exited: entry.process.HasExited()})
	}
	if id, ok := processIDToPruneFromMeta(meta); ok {
		entry, _ := store.remove(id)
		return entry
	}
	return nil
}

// pruneMeta is the per-process metadata the pruning policy operates on.
type pruneMeta struct {
	id       int
	lastUsed time.Time
	exited   bool
}

// processIDToPruneFromMeta selects which process to evict: prefer an exited
// process outside the 8 most-recently-used, otherwise the overall LRU outside
// that protected set. Mirrors process_id_to_prune_from_meta.
func processIDToPruneFromMeta(meta []pruneMeta) (int, bool) {
	if len(meta) == 0 {
		return 0, false
	}

	byRecency := make([]pruneMeta, len(meta))
	copy(byRecency, meta)
	sort.SliceStable(byRecency, func(i, j int) bool {
		return byRecency[i].lastUsed.After(byRecency[j].lastUsed)
	})
	protected := make(map[int]struct{})
	for i := 0; i < len(byRecency) && i < 8; i++ {
		protected[byRecency[i].id] = struct{}{}
	}

	lru := make([]pruneMeta, len(meta))
	copy(lru, meta)
	sort.SliceStable(lru, func(i, j int) bool {
		return lru[i].lastUsed.Before(lru[j].lastUsed)
	})

	for _, m := range lru {
		if _, isProtected := protected[m.id]; !isProtected && m.exited {
			return m.id, true
		}
	}
	for _, m := range lru {
		if _, isProtected := protected[m.id]; !isProtected {
			return m.id, true
		}
	}
	return 0, false
}
