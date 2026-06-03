package realtimeconv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// inputTask bundles everything the conversation loop needs to drive one realtime
// session. It mirrors the Rust RealtimeInputTask.
type inputTask struct {
	writer   Writer
	events   Events
	userText <-chan string
	handoff  <-chan handoffOutput
	audio    <-chan protocol.RealtimeAudioFrame
	eventsTx chan<- Event

	handoffState *handoffState
	sessionKind  SessionKind
}

// runInputTask is the conversation loop: it multiplexes user text, background
// agent handoff output, inbound realtime server events, and microphone audio
// until any source closes or a handler returns an error. It mirrors the Rust
// run_realtime_input_task tokio::select! loop.
//
// The loop exits when ctx is cancelled, an input channel closes, the server
// event stream ends, or a handler reports a fatal error.
func runInputTask(ctx context.Context, t inputTask) {
	var outputAudio *outputAudioState
	var queue responseCreateQueue

	// Inbound server events are read on a dedicated goroutine so the select can
	// treat them as just another channel, matching the Rust events.next_event()
	// select arm. serverErr distinguishes a transport error from end-of-stream.
	type serverEvent struct {
		event *Event
		err   error
	}
	serverCh := make(chan serverEvent, 1)
	readerCtx, cancelReader := context.WithCancel(ctx)
	defer cancelReader()
	go func() {
		for {
			ev, err := t.events.NextEvent(readerCtx)
			select {
			case serverCh <- serverEvent{event: ev, err: err}:
			case <-readerCtx.Done():
				return
			}
			if err != nil || ev == nil {
				return
			}
		}
	}()

	for {
		var err error
		select {
		case <-ctx.Done():
			return
		case text, ok := <-t.userText:
			err = handleUserText(ctx, text, ok, t.writer, t.eventsTx)
		case out, ok := <-t.handoff:
			err = handleHandoffOutput(ctx, out, ok, t.writer, t.eventsTx, t.handoffState, t.sessionKind, &queue)
		case se := <-serverCh:
			err = handleServerEvent(ctx, se.event, se.err, t.writer, t.eventsTx, t.handoffState, t.sessionKind, &outputAudio, &queue)
		case frame, ok := <-t.audio:
			err = handleUserAudio(ctx, frame, ok, t.writer, t.eventsTx)
		}
		if err != nil {
			return
		}
	}
}

// handleUserText forwards user-typed text into realtime. Mirrors the Rust
// handle_user_text_input. A closed channel ends the loop.
func handleUserText(ctx context.Context, text string, ok bool, writer Writer, eventsTx chan<- Event) error {
	if !ok {
		return errors.New("user text input channel closed")
	}
	if err := writer.SendConversationItemCreate(ctx, text); err != nil {
		sendEvent(ctx, eventsTx, NewError(err.Error()))
		return fmt.Errorf("send input text: %w", err)
	}
	return nil
}

// handleUserAudio forwards a captured microphone frame into realtime. Mirrors
// the Rust handle_user_audio_input. A closed channel ends the loop.
func handleUserAudio(ctx context.Context, frame protocol.RealtimeAudioFrame, ok bool, writer Writer, eventsTx chan<- Event) error {
	if !ok {
		return errors.New("user audio input channel closed")
	}
	if err := writer.SendAudioFrame(ctx, frame); err != nil {
		sendEvent(ctx, eventsTx, NewError(err.Error()))
		return fmt.Errorf("send input audio: %w", err)
	}
	return nil
}

// handleHandoffOutput forwards a background-agent progress/final update into
// realtime, applying the V1/V2 protocol differences. Mirrors the Rust
// handle_handoff_output. A closed channel ends the loop.
func handleHandoffOutput(
	ctx context.Context,
	out handoffOutput,
	ok bool,
	writer Writer,
	eventsTx chan<- Event,
	state *handoffState,
	kind SessionKind,
	queue *responseCreateQueue,
) error {
	if !ok {
		return errors.New("handoff output channel closed")
	}

	var sendErr error
	switch kind {
	case SessionKindV1:
		// Both progress and final updates resolve the function call output.
		sendErr = writer.SendConversationFunctionCallOutput(ctx, out.handoffID, out.outputText)
	case SessionKindV2:
		switch out.kind {
		case handoffProgress:
			active, has := state.activeID()
			if !has || active != out.handoffID {
				// Stale update for a handoff that is no longer active; drop it.
				return nil
			}
			sendErr = writer.SendConversationItemCreate(ctx, out.outputText)
		case handoffFinal:
			if err := writer.SendConversationFunctionCallOutput(ctx, out.handoffID, v2HandoffCompleteAck); err != nil {
				sendErr = err
				break
			}
			return queue.requestCreate(ctx, writer, eventsTx, "handoff")
		}
	}

	if sendErr != nil {
		sendEvent(ctx, eventsTx, NewError(sendErr.Error()))
		return fmt.Errorf("send handoff output: %w", sendErr)
	}
	return nil
}

// handleServerEvent processes one inbound realtime server event: it performs any
// protocol side effects (audio truncation, response-queue bookkeeping, handoff
// steering, noop acknowledgement), forwards the event to the output channel, and
// reports whether the loop should stop. Mirrors the Rust
// handle_realtime_server_event.
func handleServerEvent(
	ctx context.Context,
	event *Event,
	streamErr error,
	writer Writer,
	eventsTx chan<- Event,
	state *handoffState,
	kind SessionKind,
	outputAudio **outputAudioState,
	queue *responseCreateQueue,
) error {
	if streamErr != nil {
		msg := streamErr.Error()
		sendEvent(ctx, eventsTx, NewError(msg))
		return fmt.Errorf("realtime stream closed: %w", streamErr)
	}
	if event == nil {
		return errors.New("realtime event stream ended")
	}

	shouldStop, err := applyServerEventEffects(ctx, *event, writer, eventsTx, state, kind, outputAudio, queue)
	if err != nil {
		return err
	}

	sendEvent(ctx, eventsTx, *event)
	if shouldStop {
		return errors.New("realtime stream error event received")
	}
	return nil
}

// applyServerEventEffects performs the per-variant side effects for an inbound
// event and reports whether it is a terminal error. It mirrors the body of the
// Rust handle_realtime_server_event match prior to forwarding the event.
func applyServerEventEffects(
	ctx context.Context,
	event Event,
	writer Writer,
	eventsTx chan<- Event,
	state *handoffState,
	kind SessionKind,
	outputAudio **outputAudioState,
	queue *responseCreateQueue,
) (bool, error) {
	switch event.Kind {
	case EventKindAudioOut:
		if kind == SessionKindV2 {
			*outputAudio = updateOutputAudioState(*outputAudio, event.AudioOut)
		}
	case EventKindInputAudioSpeechStarted:
		if kind == SessionKindV2 {
			truncateOnSpeechStarted(ctx, writer, event.InputAudioSpeechStarted, outputAudio)
		}
	case EventKindResponseCreated:
		if kind == SessionKindV2 {
			queue.markStarted()
		}
	case EventKindResponseCancelled, EventKindResponseDone:
		*outputAudio = nil
		if kind == SessionKindV2 {
			if err := queue.markFinished(ctx, writer, eventsTx, "deferred"); err != nil {
				return false, err
			}
		}
	case EventKindHandoffRequested:
		*outputAudio = nil
		if err := applyHandoffRequested(ctx, event.Handoff, writer, eventsTx, state, kind, queue); err != nil {
			return false, err
		}
	case EventKindNoopRequested:
		*outputAudio = nil
		if kind == SessionKindV2 && event.Noop != nil {
			if err := writer.SendConversationFunctionCallOutput(ctx, event.Noop.CallID, ""); err != nil {
				sendEvent(ctx, eventsTx, NewError(err.Error()))
				return false, fmt.Errorf("send realtime noop function output: %w", err)
			}
		}
	case EventKindError:
		return true, nil
	case EventKindSessionUpdated,
		EventKindInputTranscriptDelta,
		EventKindInputTranscriptDone,
		EventKindOutputTranscriptDelta,
		EventKindOutputTranscriptDone,
		EventKindConversationItemAdded,
		EventKindConversationItemDone:
		// No side effects; event is forwarded unchanged.
	}
	return false, nil
}

// truncateOnSpeechStarted truncates the in-flight model audio when the user
// barges in, matching the V2 branch of the Rust InputAudioSpeechStarted arm. The
// truncate is best-effort: a send failure is swallowed (the Rust code only
// warns) so the loop continues.
func truncateOnSpeechStarted(
	ctx context.Context,
	writer Writer,
	speech *InputAudioSpeechStarted,
	outputAudio **outputAudioState,
) {
	state := *outputAudio
	if state == nil {
		return
	}
	*outputAudio = nil

	if speech != nil && speech.ItemID != nil && *speech.ItemID != state.itemID {
		// Speech belongs to a different item; nothing to truncate.
		return
	}

	payload := map[string]any{
		"type":          "conversation.item.truncate",
		"item_id":       state.itemID,
		"content_index": 0,
		"audio_end_ms":  state.audioEndMS,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	// Best-effort, mirroring the Rust code which only logs a warning on failure.
	_ = writer.SendPayload(ctx, string(encoded))
}

// applyHandoffRequested records a new handoff or, in V2, steers an in-flight one.
// Mirrors the HandoffRequested arm of the Rust handle_realtime_server_event.
func applyHandoffRequested(
	ctx context.Context,
	handoff *HandoffRequested,
	writer Writer,
	eventsTx chan<- Event,
	state *handoffState,
	kind SessionKind,
	queue *responseCreateQueue,
) error {
	if handoff == nil {
		return nil
	}
	switch kind {
	case SessionKindV1:
		state.setLastOutput(nil)
		id := handoff.HandoffID
		state.setActive(&id)
	case SessionKindV2:
		if _, active := state.activeID(); active {
			if err := writer.SendConversationFunctionCallOutput(ctx, handoff.HandoffID, v2SteerAck); err != nil {
				sendEvent(ctx, eventsTx, NewError(err.Error()))
				return fmt.Errorf("send handoff steering acknowledgement: %w", err)
			}
			return queue.requestCreate(ctx, writer, eventsTx, "handoff steering")
		}
		state.setLastOutput(nil)
		id := handoff.HandoffID
		state.setActive(&id)
	}
	return nil
}

// sendEvent forwards an event to the output channel without blocking the caller
// on ctx cancellation. It mirrors the best-effort events_tx.send used throughout
// the Rust loop (which ignores send errors on the warning paths).
func sendEvent(ctx context.Context, eventsTx chan<- Event, event Event) {
	select {
	case eventsTx <- event:
	case <-ctx.Done():
	}
}
