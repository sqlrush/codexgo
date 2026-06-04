package exec

import (
	"strings"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// CodexStatus signals whether the run loop should keep running or begin
// shutdown after a notification is processed. It mirrors the Rust
// event_processor::CodexStatus.
type CodexStatus int

const (
	// StatusRunning means the loop should continue consuming events.
	StatusRunning CodexStatus = iota
	// StatusInitiateShutdown means the active turn has reached a terminal state
	// and the loop should request shutdown.
	StatusInitiateShutdown
)

// CollectedThreadEvents bundles the JSONL events produced from a single source
// notification with the resulting loop status.
type CollectedThreadEvents struct {
	Events []ThreadEvent
	Status CodexStatus
}

// runningTodoList tracks the currently-open todo list item so plan updates can
// be reconciled into a single item.started / item.updated / item.completed
// lifecycle, mirroring the Rust RunningTodoList.
type runningTodoList struct {
	itemID string
	items  []TodoItem
}

// JSONLProcessor transforms the engine's v1 [protocol.EventMsg] stream (forwarded
// as `codex/event` notifications) into the exec [ThreadEvent] JSONL vocabulary.
//
// It is the Go analogue of EventProcessorWithJsonOutput. The Go app-server
// forwards core engine events in the v1 EventMsg wire format rather than the v2
// typed ServerNotification union, so this processor maps directly from EventMsg.
// The emitted ThreadEvents are byte-for-byte identical to the Rust JSONL output.
//
// A JSONLProcessor is not safe for concurrent use; the run loop drives it from a
// single goroutine. The next-item-id counter is atomic only so id allocation is
// well-defined under the -race detector when an external caller probes it.
type JSONLProcessor struct {
	nextID atomic.Uint64

	// rawToExecID maps an engine item id to the exec item id allocated when the
	// item first started, so the completed event reuses the same id.
	rawToExecID map[string]string

	runningTodo *runningTodoList

	lastTotalUsage  *protocol.TokenUsage
	lastCritical    *ThreadErrorEvent
	finalMessage    *string
	emitFinalOnExit bool
}

// NewJSONLProcessor constructs an empty processor.
func NewJSONLProcessor() *JSONLProcessor {
	return &JSONLProcessor{rawToExecID: make(map[string]string)}
}

// FinalMessage returns the agent's final message captured for the turn, if any.
func (p *JSONLProcessor) FinalMessage() *string { return p.finalMessage }

// EmitFinalOnExit reports whether a final message should be written to the
// --output-last-message file on shutdown (only true after a successful turn).
func (p *JSONLProcessor) EmitFinalOnExit() bool { return p.emitFinalOnExit }

// nextItemID allocates a fresh exec item id ("item_N").
func (p *JSONLProcessor) nextItemID() string {
	return "item_" + itoa(p.nextID.Add(1)-1)
}

// startedItemID returns the exec id for a started raw id, allocating and
// recording one on first sight.
func (p *JSONLProcessor) startedItemID(rawID string) string {
	if existing, ok := p.rawToExecID[rawID]; ok {
		return existing
	}
	id := p.nextItemID()
	p.rawToExecID[rawID] = id
	return id
}

// completedItemID returns the exec id recorded for a started raw id (consuming
// it), or a fresh id when the item completed without a prior start.
func (p *JSONLProcessor) completedItemID(rawID string) string {
	if existing, ok := p.rawToExecID[rawID]; ok {
		delete(p.rawToExecID, rawID)
		return existing
	}
	return p.nextItemID()
}

// ThreadStartedEvent builds the first JSONL event from a session-configured ack.
func (p *JSONLProcessor) ThreadStartedEvent(sc *protocol.SessionConfiguredEvent) ThreadEvent {
	return ThreadEvent{
		Kind:          ThreadEventKindThreadStarted,
		ThreadStarted: &ThreadStartedEvent{ThreadID: sc.ThreadID.String()},
	}
}

// CollectWarning maps a local exec warning to a non-fatal error item, mirroring
// EventProcessorWithJsonOutput::collect_warning.
func (p *JSONLProcessor) CollectWarning(message string) CollectedThreadEvents {
	return CollectedThreadEvents{
		Events: []ThreadEvent{p.errorItemEvent(message)},
		Status: StatusRunning,
	}
}

// Collect transforms a single engine event into zero or more JSONL events plus
// the resulting loop status. It is the Go analogue of collect_thread_events.
func (p *JSONLProcessor) Collect(ev protocol.Event) CollectedThreadEvents {
	msg := ev.Msg
	switch msg.Type {
	case protocol.EventMsgKindTurnStarted:
		return CollectedThreadEvents{
			Events: []ThreadEvent{{Kind: ThreadEventKindTurnStarted, TurnStarted: &TurnStartedEvent{}}},
			Status: StatusRunning,
		}
	case protocol.EventMsgKindItemStarted:
		return p.collectItemStarted(msg.ItemStarted)
	case protocol.EventMsgKindItemCompleted:
		return p.collectItemCompleted(msg.ItemCompleted)
	case protocol.EventMsgKindExecCommandBegin:
		return p.collectExecCommandBegin(msg.ExecCommandBegin)
	case protocol.EventMsgKindExecCommandEnd:
		return p.collectExecCommandEnd(msg.ExecCommandEnd)
	case protocol.EventMsgKindPlanUpdate:
		return p.collectPlanUpdate(msg.PlanUpdate)
	case protocol.EventMsgKindAgentMessage:
		// The agent_message event carries the canonical final text; record it so
		// the --output-last-message file and human fallback can use it even when
		// no item_completed AgentMessage arrives.
		if msg.AgentMessage != nil {
			text := msg.AgentMessage.Message
			p.finalMessage = &text
		}
		return CollectedThreadEvents{Status: StatusRunning}
	case protocol.EventMsgKindTokenCount:
		if msg.TokenCount != nil && msg.TokenCount.Info != nil {
			u := msg.TokenCount.Info.TotalTokenUsage
			p.lastTotalUsage = &u
		}
		return CollectedThreadEvents{Status: StatusRunning}
	case protocol.EventMsgKindError:
		return p.collectError(msg.Error)
	case protocol.EventMsgKindStreamError:
		return p.collectStreamError(msg.StreamError)
	case protocol.EventMsgKindDeprecationNotice:
		return p.collectDeprecation(msg.DeprecationNotice)
	case protocol.EventMsgKindTurnComplete:
		return p.collectTurnComplete(msg.TurnComplete)
	case protocol.EventMsgKindTurnAborted:
		return p.collectTurnAborted(msg.TurnAborted)
	default:
		return CollectedThreadEvents{Status: StatusRunning}
	}
}

// collectItemStarted maps an engine item_started to an item.started event,
// skipping agent-message and reasoning items (which are emitted only on
// completion, matching the Rust map_started_item).
func (p *JSONLProcessor) collectItemStarted(n *protocol.ItemStartedEvent) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	switch n.Item.Type {
	case protocol.TurnItemKindAgentMessage, protocol.TurnItemKindReasoning:
		return CollectedThreadEvents{Status: StatusRunning}
	}
	rawID := n.Item.ID()
	item, ok := p.mapItem(n.Item, p.startedItemID(rawID))
	if !ok {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	return CollectedThreadEvents{
		Events: []ThreadEvent{{Kind: ThreadEventKindItemStarted, ItemStarted: &ItemEvent{Item: item}}},
		Status: StatusRunning,
	}
}

// collectItemCompleted maps an engine item_completed to an item.completed event.
func (p *JSONLProcessor) collectItemCompleted(n *protocol.ItemCompletedEvent) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	item, ok := p.mapCompletedItem(n.Item)
	if !ok {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	if item.Details.Kind == ThreadItemDetailKindAgentMessage {
		text := item.Details.AgentMessage.Text
		p.finalMessage = &text
	}
	return CollectedThreadEvents{
		Events: []ThreadEvent{{Kind: ThreadEventKindItemCompleted, ItemCompleted: &ItemEvent{Item: item}}},
		Status: StatusRunning,
	}
}

// mapCompletedItem allocates the completed item id (a fresh id for message and
// reasoning items, the started id for everything else) and maps the payload.
func (p *JSONLProcessor) mapCompletedItem(it protocol.TurnItem) (ThreadItem, bool) {
	switch it.Type {
	case protocol.TurnItemKindAgentMessage, protocol.TurnItemKindReasoning:
		return p.mapItem(it, p.nextItemID())
	default:
		return p.mapItem(it, p.completedItemID(it.ID()))
	}
}

// collectPlanUpdate maps an engine plan_update to a todo_list item lifecycle.
func (p *JSONLProcessor) collectPlanUpdate(n *protocol.UpdatePlanArgs) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	items := mapTodoItems(n.Plan)
	if p.runningTodo != nil {
		p.runningTodo.items = items
		return CollectedThreadEvents{
			Events: []ThreadEvent{{
				Kind:        ThreadEventKindItemUpdated,
				ItemUpdated: &ItemEvent{Item: todoItem(p.runningTodo.itemID, items)},
			}},
			Status: StatusRunning,
		}
	}
	id := p.nextItemID()
	p.runningTodo = &runningTodoList{itemID: id, items: items}
	return CollectedThreadEvents{
		Events: []ThreadEvent{{
			Kind:        ThreadEventKindItemStarted,
			ItemStarted: &ItemEvent{Item: todoItem(id, items)},
		}},
		Status: StatusRunning,
	}
}

// collectError maps a fatal error event to a stream error and records it as the
// last critical error so a later turn failure can reuse the message.
func (p *JSONLProcessor) collectError(n *protocol.ErrorEvent) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	errEv := ThreadErrorEvent{Message: n.Message}
	p.lastCritical = &errEv
	return CollectedThreadEvents{
		Events: []ThreadEvent{{Kind: ThreadEventKindError, Error: &errEv}},
		Status: StatusRunning,
	}
}

// collectStreamError maps a recoverable stream error to a stream error event,
// folding in any additional details exactly as the Rust ServerNotification::Error
// mapping does.
func (p *JSONLProcessor) collectStreamError(n *protocol.StreamErrorEvent) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	message := n.Message
	if n.AdditionalDetails != nil && *n.AdditionalDetails != "" {
		message = n.Message + " (" + *n.AdditionalDetails + ")"
	}
	errEv := ThreadErrorEvent{Message: message}
	p.lastCritical = &errEv
	return CollectedThreadEvents{
		Events: []ThreadEvent{{Kind: ThreadEventKindError, Error: &errEv}},
		Status: StatusRunning,
	}
}

// collectDeprecation maps a deprecation notice to a non-fatal error item.
func (p *JSONLProcessor) collectDeprecation(n *protocol.DeprecationNoticeEvent) CollectedThreadEvents {
	if n == nil {
		return CollectedThreadEvents{Status: StatusRunning}
	}
	message := n.Summary
	if n.Details != nil && *n.Details != "" {
		message = n.Summary + " (" + *n.Details + ")"
	}
	return CollectedThreadEvents{
		Events: []ThreadEvent{p.errorItemEvent(message)},
		Status: StatusRunning,
	}
}

// collectTurnComplete flushes any open todo list and emits the terminal
// turn.completed event, capturing the final agent message when present.
func (p *JSONLProcessor) collectTurnComplete(n *protocol.TurnCompleteEvent) CollectedThreadEvents {
	var events []ThreadEvent
	if running := p.runningTodo; running != nil {
		p.runningTodo = nil
		events = append(events, ThreadEvent{
			Kind:          ThreadEventKindItemCompleted,
			ItemCompleted: &ItemEvent{Item: todoItem(running.itemID, running.items)},
		})
	}
	if n != nil && n.LastAgentMessage != nil {
		msg := *n.LastAgentMessage
		p.finalMessage = &msg
	}
	p.emitFinalOnExit = true
	// When a critical error occurred during the turn, codex emits a terminal
	// turn.failed (carrying the error) rather than turn.completed. Mirror that.
	if p.lastCritical != nil {
		events = append(events, ThreadEvent{
			Kind:       ThreadEventKindTurnFailed,
			TurnFailed: &TurnFailedEvent{Error: *p.lastCritical},
		})
		return CollectedThreadEvents{Events: events, Status: StatusInitiateShutdown}
	}
	events = append(events, ThreadEvent{
		Kind:          ThreadEventKindTurnCompleted,
		TurnCompleted: &TurnCompletedEvent{Usage: p.usageFromLastTotal()},
	})
	return CollectedThreadEvents{Events: events, Status: StatusInitiateShutdown}
}

// collectTurnAborted handles a turn abort: an interrupt yields no failure event,
// while any other reason is reported as a turn.failed using the last critical
// error or a generic message.
func (p *JSONLProcessor) collectTurnAborted(n *protocol.TurnAbortedEvent) CollectedThreadEvents {
	p.finalMessage = nil
	p.emitFinalOnExit = false
	if n != nil && n.Reason == protocol.TurnAbortReasonInterrupted {
		return CollectedThreadEvents{Status: StatusInitiateShutdown}
	}
	errEv := ThreadErrorEvent{Message: "turn failed"}
	if p.lastCritical != nil {
		errEv = *p.lastCritical
	}
	return CollectedThreadEvents{
		Events: []ThreadEvent{{Kind: ThreadEventKindTurnFailed, TurnFailed: &TurnFailedEvent{Error: errEv}}},
		Status: StatusInitiateShutdown,
	}
}

// errorItemEvent builds an item.completed event carrying a non-fatal error item.
func (p *JSONLProcessor) errorItemEvent(message string) ThreadEvent {
	return ThreadEvent{
		Kind: ThreadEventKindItemCompleted,
		ItemCompleted: &ItemEvent{Item: ThreadItem{
			ID: p.nextItemID(),
			Details: ThreadItemDetails{
				Kind:  ThreadItemDetailKindError,
				Error: &ErrorItem{Message: message},
			},
		}},
	}
}

// usageFromLastTotal converts the last recorded token usage into a [Usage].
func (p *JSONLProcessor) usageFromLastTotal() Usage {
	if p.lastTotalUsage == nil {
		return Usage{}
	}
	u := p.lastTotalUsage
	return Usage{
		InputTokens:           u.InputTokens,
		CachedInputTokens:     u.CachedInputTokens,
		OutputTokens:          u.OutputTokens,
		ReasoningOutputTokens: u.ReasoningOutputTokens,
	}
}

// todoItem builds a todo_list ThreadItem with the given id and items.
func todoItem(id string, items []TodoItem) ThreadItem {
	return ThreadItem{
		ID: id,
		Details: ThreadItemDetails{
			Kind:     ThreadItemDetailKindTodoList,
			TodoList: &TodoListItem{Items: items},
		},
	}
}

// mapTodoItems converts engine plan steps into todo items.
func mapTodoItems(steps []protocol.PlanItemArg) []TodoItem {
	out := make([]TodoItem, 0, len(steps))
	for _, s := range steps {
		out = append(out, TodoItem{
			Text:      s.Step,
			Completed: s.Status == protocol.StepStatusCompleted,
		})
	}
	return out
}

// itoa renders a uint64 without importing strconv at every call site; kept tiny
// and allocation-light for the hot id path.
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// reasoningText joins reasoning summary fragments, returning "" when empty after
// trimming (so empty reasoning items are dropped, matching the Rust check).
func reasoningText(summary []string) string {
	joined := strings.Join(summary, "\n")
	if strings.TrimSpace(joined) == "" {
		return ""
	}
	return joined
}
