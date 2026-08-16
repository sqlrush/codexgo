package engine

import (
	"fmt"
	"math"
	"time"

	"github.com/dop251/goja"
)

// setTimeoutCallback mirrors codex's `set_timeout_callback`/`schedule_timeout`.
// It retains the JS callback, spawns a goroutine that sleeps for the requested
// delay, and then posts a TimeoutFired command back into the runtime loop. The
// callback itself runs on the runtime goroutine when the command is processed,
// preserving single-threaded JS execution.
func (s *runtimeState) setTimeoutCallback(call goja.FunctionCall) goja.Value {
	callbackVal := call.Argument(0)
	callback, ok := goja.AssertFunction(callbackVal)
	if !ok {
		s.throwTypeError("setTimeout expects a function callback")
	}

	delayMS := normalizeDelayMS(numberValue(call.Argument(1)))

	timeoutID := s.nextTimeoutID
	s.nextTimeoutID = saturatingAdd(s.nextTimeoutID)
	cancel := make(chan struct{})
	s.pendingTimeouts[timeoutID] = scheduledTimeout{callback: callback, cancel: cancel}

	fired := s.timeoutFired
	s.timers.add()
	go func() {
		defer s.timers.done()
		timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			select {
			case fired <- runtimeCommand{Kind: runtimeCmdTimeoutFired, TimeoutID: timeoutID}:
			case <-cancel:
			}
		case <-cancel:
		}
	}()

	return s.rt.ToValue(float64(timeoutID))
}

// clearTimeoutCallback mirrors codex's `clear_timeout_callback`/`clear_timeout`.
func (s *runtimeState) clearTimeoutCallback(call goja.FunctionCall) goja.Value {
	timeoutID, ok, err := timeoutIDFromArgs(call)
	if err != nil {
		s.throwTypeError(err.Error())
	}
	if !ok {
		return goja.Undefined()
	}
	if scheduled, present := s.pendingTimeouts[timeoutID]; present {
		close(scheduled.cancel)
		delete(s.pendingTimeouts, timeoutID)
	}
	return goja.Undefined()
}

// invokeTimeoutCallback runs a fired timeout's callback on the runtime goroutine,
// mirroring codex's `invoke_timeout_callback`. A non-nil error carries the JS
// exception text so the runtime loop can terminate the cell with that error.
func (s *runtimeState) invokeTimeoutCallback(timeoutID uint64) error {
	scheduled, ok := s.pendingTimeouts[timeoutID]
	if !ok {
		return nil
	}
	delete(s.pendingTimeouts, timeoutID)

	if _, err := scheduled.callback(goja.Undefined()); err != nil {
		return fmt.Errorf("%s", valueToErrorText(s.rt, err))
	}
	return nil
}

// timeoutIDFromArgs mirrors codex's `timeout_id_from_args`. The boolean reports
// whether a clearable id was supplied (false means a no-op clearTimeout).
func timeoutIDFromArgs(call goja.FunctionCall) (uint64, bool, error) {
	if len(call.Arguments) == 0 {
		return 0, false, nil
	}
	arg := call.Arguments[0]
	if goja.IsNull(arg) || goja.IsUndefined(arg) {
		return 0, false, nil
	}
	number, ok := asNumber(arg)
	if !ok {
		return 0, false, fmt.Errorf("clearTimeout expects a numeric timeout id")
	}
	if math.IsInf(number, 0) || math.IsNaN(number) || number <= 0 {
		return 0, false, nil
	}
	return uint64(math.Min(math.Trunc(number), float64(^uint64(0)))), true, nil
}

// normalizeDelayMS mirrors codex's `normalize_delay_ms`.
func normalizeDelayMS(delayMS float64) uint64 {
	if math.IsInf(delayMS, 0) || math.IsNaN(delayMS) || delayMS <= 0 {
		return 0
	}
	return uint64(math.Min(math.Trunc(delayMS), float64(^uint64(0))))
}

// numberValue coerces a goja value to a float64 the way V8's number_value does,
// returning 0 for non-numeric inputs (which normalizeDelayMS then clamps).
func numberValue(value goja.Value) float64 {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return 0
	}
	n, ok := asNumber(value)
	if !ok {
		return 0
	}
	return n
}

// asNumber reports whether a goja value carries a numeric export and returns it.
func asNumber(value goja.Value) (float64, bool) {
	if value == nil {
		return 0, false
	}
	switch n := value.Export().(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return value.ToFloat(), !math.IsNaN(value.ToFloat())
}
