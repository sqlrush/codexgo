package engine

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// CodeModeSession mirrors codex's `CodeModeSession` trait. A session is a durable
// code-mode execution context owned by one Codex thread: cells in the same
// session share stored values, while separate sessions keep them isolated.
type CodeModeSession interface {
	// Execute starts a cell and returns a StartedCell whose InitialResponse
	// resolves to the cell's first RuntimeResponse (a yield or a terminal result).
	Execute(ctx context.Context, request ExecuteRequest) (*StartedCell, error)
	// Wait resumes a live cell for a bounded window, returning the new output or
	// the cell's terminal/missing response.
	Wait(ctx context.Context, request WaitRequest) (WaitOutcome, error)
	// Terminate stops a live cell and returns its terminal response.
	Terminate(ctx context.Context, cellID CellID) (WaitOutcome, error)
	// Shutdown terminates every live cell and blocks until they are gone.
	Shutdown(ctx context.Context) error
}

// StartedCell mirrors codex's `StartedCell`. It carries the allocated cell id and
// a channel delivering the cell's first RuntimeResponse.
type StartedCell struct {
	CellID          CellID
	initialResponse <-chan RuntimeResponse
}

// InitialResponse blocks until the cell yields or terminates, mirroring codex's
// `StartedCell::initial_response`. It returns an error if the runtime ends before
// producing a response (or ctx is cancelled).
func (c *StartedCell) InitialResponse(ctx context.Context) (RuntimeResponse, error) {
	select {
	case response, ok := <-c.initialResponse:
		if !ok {
			return RuntimeResponse{}, fmt.Errorf("exec runtime ended unexpectedly")
		}
		return response, nil
	case <-ctx.Done():
		return RuntimeResponse{}, fmt.Errorf("exec wait cancelled: %w", ctx.Err())
	}
}

// CodeModeService mirrors codex's `CodeModeService`. It is the in-process
// CodeModeSession implementation: it owns the session-scoped stored values and
// the live cell registry, and drives each cell through a control loop.
type CodeModeService struct {
	mu           sync.Mutex
	storedValues map[string]any
	cells        map[CellID]*cellHandle
	delegate     CodeModeSessionDelegate
	shuttingDown atomic.Bool
	nextCellID   atomic.Uint64
	// closed tracks when each cell's control loop has fully exited, enabling
	// Shutdown to wait without busy-looping.
	cellDone map[CellID]chan struct{}
}

// NewCodeModeService constructs a service backed by the noop delegate, mirroring
// codex's `CodeModeService::new`.
func NewCodeModeService() *CodeModeService {
	return NewCodeModeServiceWithDelegate(NoopCodeModeSessionDelegate{})
}

// NewCodeModeServiceWithDelegate constructs a service with the given host bridge,
// mirroring codex's `CodeModeService::with_delegate`.
func NewCodeModeServiceWithDelegate(delegate CodeModeSessionDelegate) *CodeModeService {
	s := &CodeModeService{
		storedValues: map[string]any{},
		cells:        map[CellID]*cellHandle{},
		cellDone:     map[CellID]chan struct{}{},
		delegate:     delegate,
	}
	s.nextCellID.Store(1)
	return s
}

// allocateCellID mirrors codex's `allocate_cell_id`.
func (s *CodeModeService) allocateCellID() CellID {
	return NewCellID(strconv.FormatUint(s.nextCellID.Add(1)-1, 10))
}

// cellHandle mirrors codex's `CellHandle`. It bundles the channels and cancel
// func used to drive one live cell's control loop. terminateRuntime is the goja
// analog of v8::IsolateHandle::terminate_execution: it interrupts a CPU-bound
// runtime goroutine.
type cellHandle struct {
	controlCh        chan cellControlCommand
	commandCh        chan<- runtimeCommand
	cancel           context.CancelFunc
	terminateRuntime func()
}

// cellControlCommandKind enumerates control commands the service delivers to a
// cell's control loop, mirroring codex's `CellControlCommand`.
type cellControlCommandKind int

const (
	cellControlPoll cellControlCommandKind = iota
	cellControlTerminate
)

// cellControlCommand mirrors codex's `CellControlCommand`. responseCh carries the
// resulting RuntimeResponse back to the waiting caller.
type cellControlCommand struct {
	Kind        cellControlCommandKind
	YieldTimeMS uint64
	responseCh  chan<- RuntimeResponse
}

// Execute mirrors codex's `CodeModeService::execute`.
func (s *CodeModeService) Execute(ctx context.Context, request ExecuteRequest) (*StartedCell, error) {
	if s.shuttingDown.Load() {
		return nil, fmt.Errorf("code mode session is shutting down")
	}
	initialYield := DefaultExecYieldTimeMS
	if request.YieldTimeMS != nil {
		initialYield = *request.YieldTimeMS
	}
	responseCh := make(chan RuntimeResponse, 1)
	cellID := s.allocateCellID()
	if err := s.startCell(cellID, request, cellResponseSender{runtimeCh: responseCh}, &initialYield, pendingModeContinue); err != nil {
		return nil, err
	}
	return &StartedCell{CellID: cellID, initialResponse: responseCh}, nil
}

// cellResponseSender mirrors codex's `CellResponseSender`. Exactly one channel is
// set: runtimeCh for Execute/Wait/Terminate callers.
type cellResponseSender struct {
	runtimeCh chan<- RuntimeResponse
}

func (c cellResponseSender) isSet() bool { return c.runtimeCh != nil }

func (c cellResponseSender) send(response RuntimeResponse) {
	if c.runtimeCh != nil {
		c.runtimeCh <- response
	}
}

// startCell registers a new cell and launches its control loop, mirroring codex's
// `start_cell`.
func (s *CodeModeService) startCell(
	cellID CellID,
	request ExecuteRequest,
	initialResponse cellResponseSender,
	initialYieldMS *uint64,
	pendingMode pendingRuntimeMode,
) error {
	eventCh := make(chan runtimeEvent, 1024)
	controlCh := make(chan cellControlCommand, 16)
	// Cells are session-scoped, not request-scoped: they outlive the Execute call
	// and are cancelled by Terminate/Shutdown, so they derive from Background.
	ctx, cancel := context.WithCancel(context.Background())

	s.mu.Lock()
	if s.shuttingDown.Load() {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("code mode session is shutting down")
	}
	if _, exists := s.cells[cellID]; exists {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("exec cell %s already exists", cellID)
	}
	stored := cloneStringMap(s.storedValues)
	handle, err := spawnRuntime(stored, request, eventCh, pendingMode)
	if err != nil {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("spawn code mode runtime: %w", err)
	}
	done := make(chan struct{})
	s.cells[cellID] = &cellHandle{
		controlCh:        controlCh,
		commandCh:        handle.commandCh,
		cancel:           cancel,
		terminateRuntime: handle.terminate,
	}
	s.cellDone[cellID] = done
	s.mu.Unlock()

	go s.runCellControl(ctx, cancel, cellControlContext{
		cellID:      cellID,
		runtime:     handle,
		pendingMode: pendingMode,
	}, eventCh, controlCh, initialResponse, initialYieldMS, done)

	return nil
}

// Wait mirrors codex's `CodeModeService::wait`.
func (s *CodeModeService) Wait(ctx context.Context, request WaitRequest) (WaitOutcome, error) {
	handle := s.lookupCell(request.CellID)
	if handle == nil {
		return WaitOutcome{Kind: WaitOutcomeMissingCell, Response: missingCellResponse(request.CellID)}, nil
	}
	responseCh := make(chan RuntimeResponse, 1)
	command := cellControlCommand{Kind: cellControlPoll, YieldTimeMS: request.YieldTimeMS, responseCh: responseCh}
	if !sendControl(handle, command) {
		return WaitOutcome{Kind: WaitOutcomeMissingCell, Response: missingCellResponse(request.CellID)}, nil
	}
	select {
	case response := <-responseCh:
		return WaitOutcome{Kind: WaitOutcomeLiveCell, Response: response}, nil
	case <-ctx.Done():
		return WaitOutcome{Kind: WaitOutcomeMissingCell, Response: missingCellResponse(request.CellID)}, nil
	}
}

// Terminate mirrors codex's `CodeModeService::terminate`.
func (s *CodeModeService) Terminate(ctx context.Context, cellID CellID) (WaitOutcome, error) {
	handle := s.lookupCell(cellID)
	if handle == nil {
		return WaitOutcome{Kind: WaitOutcomeMissingCell, Response: missingCellResponse(cellID)}, nil
	}
	responseCh := make(chan RuntimeResponse, 1)
	command := cellControlCommand{Kind: cellControlTerminate, responseCh: responseCh}
	if !sendControl(handle, command) {
		return WaitOutcome{Kind: WaitOutcomeMissingCell, Response: missingCellResponse(cellID)}, nil
	}
	select {
	case response := <-responseCh:
		return WaitOutcome{Kind: WaitOutcomeLiveCell, Response: response}, nil
	case <-ctx.Done():
		return WaitOutcome{Kind: WaitOutcomeMissingCell, Response: missingCellResponse(cellID)}, nil
	}
}

// Shutdown mirrors codex's `CodeModeService::shutdown`. It marks the session
// shutting down, terminates every live cell, and waits for their control loops to
// exit.
func (s *CodeModeService) Shutdown(ctx context.Context) error {
	s.shuttingDown.Store(true)

	s.mu.Lock()
	handles := make([]*cellHandle, 0, len(s.cells))
	dones := make([]chan struct{}, 0, len(s.cellDone))
	for id, handle := range s.cells {
		handles = append(handles, handle)
		dones = append(dones, s.cellDone[id])
	}
	s.mu.Unlock()

	for _, handle := range handles {
		handle.cancel()
		// Terminate the cell's control loop; the loop interrupts the runtime and
		// drains its terminate path before exiting.
		responseCh := make(chan RuntimeResponse, 1)
		sendControl(handle, cellControlCommand{Kind: cellControlTerminate, responseCh: responseCh})
		// Interrupt a CPU-bound runtime directly so a tight loop unwinds even if
		// it never yields back to the control loop.
		if handle.terminateRuntime != nil {
			handle.terminateRuntime()
		}
	}

	for _, done := range dones {
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("code mode shutdown cancelled: %w", ctx.Err())
		}
	}
	return nil
}

// lookupCell returns a live cell's handle, or nil when absent.
func (s *CodeModeService) lookupCell(cellID CellID) *cellHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cells[cellID]
}

// removeCell drops a cell from the registry and signals its done channel.
func (s *CodeModeService) removeCell(cellID CellID) {
	s.mu.Lock()
	delete(s.cells, cellID)
	done := s.cellDone[cellID]
	delete(s.cellDone, cellID)
	s.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// extendStoredValues merges a cell's stored-value writes into the session store,
// mirroring codex extending `stored_values` on a terminal Result.
func (s *CodeModeService) extendStoredValues(writes map[string]any) {
	if len(writes) == 0 {
		return
	}
	s.mu.Lock()
	for k, v := range writes {
		s.storedValues[k] = v
	}
	s.mu.Unlock()
}

// sendControl forwards a control command, reporting false when the loop has gone.
func sendControl(handle *cellHandle, command cellControlCommand) bool {
	defer func() { _ = recover() }() // closed channel send => loop already exited
	handle.controlCh <- command
	return true
}

// missingCellResponse mirrors codex's `missing_cell_response`.
func missingCellResponse(cellID CellID) RuntimeResponse {
	return newResultResponse(cellID, nil, strPtrOf(fmt.Sprintf("exec cell %s not found", cellID)))
}
