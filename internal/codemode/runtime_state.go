package codemode

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// runtimeState mirrors codex's `RuntimeState`. It owns the per-cell mutable
// runtime bookkeeping: pending nested-tool promise resolvers, scheduled timeout
// callbacks, the session-scoped stored-value snapshot plus the writes this cell
// has performed, and the monotonic id counters.
//
// A runtimeState is confined to a single goroutine (the cell runtime loop), so
// its maps need no locking. The only field touched from other goroutines is the
// event channel, which is itself safe for concurrent sends.
type runtimeState struct {
	rt      *goja.Runtime
	eventCh chan<- runtimeEvent

	pendingToolCalls map[string]promiseResolver
	pendingTimeouts  map[uint64]scheduledTimeout

	storedValues      map[string]any
	storedValueWrites map[string]any

	enabledTools []EnabledToolMetadata

	nextToolCallID uint64
	nextTimeoutID  uint64

	toolCallID string

	// timeoutFired delivers TimeoutFired commands back into the runtime loop. It
	// mirrors codex's runtime_command_tx (timers send TimeoutFired to the same
	// command channel the host uses).
	timeoutFired chan<- runtimeCommand

	// timers tracks live setTimeout goroutines so the engine can drain them on
	// shutdown.
	timers *timerWaitGroup

	exitRequested bool
}

// promiseResolver bundles a goja promise's resolve/reject callbacks so a pending
// nested tool call can be settled later from the runtime loop.
type promiseResolver struct {
	resolve func(any) error
	reject  func(any) error
}

// scheduledTimeout mirrors codex's `ScheduledTimeout`: a retained callback plus
// the goroutine cancellation channel used by clearTimeout.
type scheduledTimeout struct {
	callback goja.Callable
	cancel   chan struct{}
}

// newRuntimeState constructs a runtimeState seeded with a clone of the session's
// stored values. The clone mirrors codex cloning `stored_values` at cell start so
// concurrent cells observe a consistent snapshot.
func newRuntimeState(
	rt *goja.Runtime,
	eventCh chan<- runtimeEvent,
	timeoutFired chan<- runtimeCommand,
	storedValues map[string]any,
	enabledTools []EnabledToolMetadata,
	toolCallID string,
) *runtimeState {
	clone := make(map[string]any, len(storedValues))
	for k, v := range storedValues {
		clone[k] = v
	}
	return &runtimeState{
		rt:                rt,
		eventCh:           eventCh,
		pendingToolCalls:  map[string]promiseResolver{},
		pendingTimeouts:   map[uint64]scheduledTimeout{},
		storedValues:      clone,
		storedValueWrites: map[string]any{},
		enabledTools:      enabledTools,
		nextToolCallID:    1,
		nextTimeoutID:     1,
		toolCallID:        toolCallID,
		timeoutFired:      timeoutFired,
		timers:            &timerWaitGroup{},
	}
}

// emit forwards a runtime event to the host control loop. Sends never block the
// runtime goroutine beyond channel buffering; the channel is unbounded in spirit
// (large buffer) so emit is effectively non-blocking for well-behaved cells.
func (s *runtimeState) emit(event runtimeEvent) {
	s.eventCh <- event
}

// installGlobals wires the code-mode host functions onto the runtime's global
// object, mirroring codex's `globals::install_globals`. It also removes the V8
// globals codex deletes (console, Atomics, SharedArrayBuffer, WebAssembly) where
// goja exposes an equivalent.
func (s *runtimeState) installGlobals() error {
	rt := s.rt
	global := rt.GlobalObject()

	for _, name := range []string{"console", "Atomics", "SharedArrayBuffer", "WebAssembly"} {
		if err := global.Delete(name); err != nil {
			return fmt.Errorf("failed to remove global %q: %w", name, err)
		}
	}

	toolsObj, err := s.buildToolsObject()
	if err != nil {
		return err
	}
	allTools, err := s.buildAllToolsValue()
	if err != nil {
		return err
	}

	bindings := map[string]any{
		"tools":        toolsObj,
		"ALL_TOOLS":    allTools,
		"clearTimeout": s.clearTimeoutCallback,
		"setTimeout":   s.setTimeoutCallback,
		"text":         s.textCallback,
		"image":        s.imageCallback,
		"store":        s.storeCallback,
		"load":         s.loadCallback,
		"notify":       s.notifyCallback,
		"yield_control": func(goja.FunctionCall) goja.Value {
			s.emit(runtimeEvent{Kind: runtimeEventYieldRequested})
			return goja.Undefined()
		},
		"exit": s.exitCallback,
	}
	for name, value := range bindings {
		if err := rt.Set(name, value); err != nil {
			return fmt.Errorf("failed to set global %q: %w", name, err)
		}
	}
	return nil
}

// buildToolsObject builds the global `tools` object, one normalized method per
// enabled tool, mirroring codex's `build_tools_object`.
func (s *runtimeState) buildToolsObject() (*goja.Object, error) {
	tools := s.rt.NewObject()
	for index, tool := range s.enabledTools {
		index := index
		if err := tools.Set(tool.GlobalName, s.makeToolFunction(index)); err != nil {
			return nil, fmt.Errorf("failed to set tool %q: %w", tool.GlobalName, err)
		}
	}
	return tools, nil
}

// buildAllToolsValue builds the global `ALL_TOOLS` array of {name, description}
// metadata, mirroring codex's `build_all_tools_value`.
func (s *runtimeState) buildAllToolsValue() (goja.Value, error) {
	items := make([]any, 0, len(s.enabledTools))
	for _, tool := range s.enabledTools {
		item := s.rt.NewObject()
		if err := item.Set("name", tool.GlobalName); err != nil {
			return nil, fmt.Errorf("failed to set ALL_TOOLS name: %w", err)
		}
		if err := item.Set("description", tool.Description); err != nil {
			return nil, fmt.Errorf("failed to set ALL_TOOLS description: %w", err)
		}
		items = append(items, item)
	}
	return s.rt.ToValue(items), nil
}

// makeToolFunction returns the goja callback for one enabled tool. It mirrors
// codex's `tool_callback`: it decodes the JS argument to JSON, allocates a
// promise, registers its resolver, and emits a ToolCall event whose response the
// host control loop later resolves via resolveToolResponse.
func (s *runtimeState) makeToolFunction(toolIndex int) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		var input any
		if len(call.Arguments) > 0 {
			decoded, ok, err := jsValueToJSON(s.rt, call.Arguments[0])
			if err != nil {
				s.throwTypeError(err.Error())
			}
			if ok {
				input = decoded
			}
		}

		tool := s.enabledTools[toolIndex]
		promise, resolve, reject := s.rt.NewPromise()
		id := fmt.Sprintf("tool-%d", s.nextToolCallID)
		s.nextToolCallID = saturatingAdd(s.nextToolCallID)
		s.pendingToolCalls[id] = promiseResolver{resolve: resolve, reject: reject}
		s.emit(runtimeEvent{
			Kind:     runtimeEventToolCall,
			ID:       id,
			ToolName: tool.ToolName,
			ToolKind: tool.Kind,
			Input:    input,
		})
		return s.rt.ToValue(promise)
	}
}

// textCallback mirrors codex's `text_callback`.
func (s *runtimeState) textCallback(call goja.FunctionCall) goja.Value {
	value := argOrUndefined(call, 0)
	text, err := serializeOutputText(s.rt, value)
	if err != nil {
		s.throwTypeError(err.Error())
	}
	s.emit(runtimeEvent{Kind: runtimeEventContentItem, ContentItem: NewTextItem(text)})
	return goja.Undefined()
}

// imageCallback mirrors codex's `image_callback`.
func (s *runtimeState) imageCallback(call goja.FunctionCall) goja.Value {
	value := argOrUndefined(call, 0)
	var detailOverride *string
	if len(call.Arguments) >= 2 {
		detail := call.Arguments[1]
		switch {
		case isStringValue(detail):
			str := detail.String()
			detailOverride = &str
		case goja.IsNull(detail) || goja.IsUndefined(detail):
			detailOverride = nil
		default:
			s.throwTypeError("image detail must be a string when provided")
		}
	}
	item, err := normalizeOutputImage(s.rt, value, detailOverride)
	if err != nil {
		s.throwTypeError(err.Error())
	}
	s.emit(runtimeEvent{Kind: runtimeEventContentItem, ContentItem: item})
	return goja.Undefined()
}

// storeCallback mirrors codex's `store_callback`.
func (s *runtimeState) storeCallback(call goja.FunctionCall) goja.Value {
	key := call.Argument(0).String()
	serialized, ok, err := jsValueToJSON(s.rt, call.Argument(1))
	if err != nil {
		s.throwTypeError(err.Error())
	}
	if !ok {
		s.throwTypeError(fmt.Sprintf(
			"Unable to store %q. Only plain serializable objects can be stored.", key,
		))
	}
	s.storedValues[key] = serialized
	s.storedValueWrites[key] = serialized
	return goja.Undefined()
}

// loadCallback mirrors codex's `load_callback`.
func (s *runtimeState) loadCallback(call goja.FunctionCall) goja.Value {
	key := call.Argument(0).String()
	value, ok := s.storedValues[key]
	if !ok {
		return goja.Undefined()
	}
	js, err := jsonToJSValue(s.rt, value)
	if err != nil {
		s.throwTypeError("failed to load stored value")
	}
	return js
}

// notifyCallback mirrors codex's `notify_callback`.
func (s *runtimeState) notifyCallback(call goja.FunctionCall) goja.Value {
	value := argOrUndefined(call, 0)
	text, err := serializeOutputText(s.rt, value)
	if err != nil {
		s.throwTypeError(err.Error())
	}
	if strings.TrimSpace(text) == "" {
		s.throwTypeError("notify expects non-empty text")
	}
	s.emit(runtimeEvent{Kind: runtimeEventNotify, CallID: s.toolCallID, Text: text})
	return goja.Undefined()
}

// exitCallback mirrors codex's `exit_callback`: it sets the exit flag and throws
// the sentinel string so the surrounding async body unwinds.
func (s *runtimeState) exitCallback(goja.FunctionCall) goja.Value {
	s.exitRequested = true
	panic(s.rt.ToValue(exitSentinel))
}

// throwTypeError throws a bare JS string exception, mirroring codex's
// `throw_type_error` (which does `scope.throw_exception(message.into())` with a
// v8::String, NOT a TypeError object). Throwing a string keeps the surfaced
// error_text equal to the message, without a goja stack-trace prefix.
func (s *runtimeState) throwTypeError(message string) {
	panic(s.rt.ToValue(message))
}

// argOrUndefined returns the call's nth argument or undefined when absent,
// matching codex's `args.length() == 0` checks.
func argOrUndefined(call goja.FunctionCall, n int) goja.Value {
	if len(call.Arguments) <= n {
		return goja.Undefined()
	}
	return call.Arguments[n]
}

// isStringValue reports whether a goja value is a JS string.
func isStringValue(value goja.Value) bool {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return false
	}
	_, ok := value.Export().(string)
	return ok
}

// saturatingAdd increments an id counter, saturating at uint64 max to mirror
// codex's saturating_add.
func saturatingAdd(v uint64) uint64 {
	if v == ^uint64(0) {
		return v
	}
	return v + 1
}

// timerWaitGroup tracks live timeout goroutines so a runtime can drain them on
// shutdown without leaking. It is package-private and used by the engine.
type timerWaitGroup struct {
	wg sync.WaitGroup
}

func (t *timerWaitGroup) add()  { t.wg.Add(1) }
func (t *timerWaitGroup) done() { t.wg.Done() }
func (t *timerWaitGroup) wait() { t.wg.Wait() }
