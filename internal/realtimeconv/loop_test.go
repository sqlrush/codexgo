package realtimeconv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestApplyServerEventEffects(t *testing.T) {
	ctx := context.Background()

	t.Run("v2 audio out accumulates output state", func(t *testing.T) {
		w := newFakeWriter()
		var outputAudio *outputAudioState
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)

		id := "item-x"
		ev := Event{Kind: EventKindAudioOut, AudioOut: &protocol.RealtimeAudioFrame{
			SampleRate: 1000, SamplesPerChannel: u32(1000), ItemID: &id,
		}}
		stop, err := applyServerEventEffects(ctx, ev, w, eventsTx, st, SessionKindV2, &outputAudio, &q)
		if err != nil || stop {
			t.Fatalf("stop=%v err=%v", stop, err)
		}
		if outputAudio == nil || outputAudio.itemID != "item-x" || outputAudio.audioEndMS != 1000 {
			t.Fatalf("unexpected output audio state %+v", outputAudio)
		}
	})

	t.Run("v1 audio out does not track output state", func(t *testing.T) {
		w := newFakeWriter()
		var outputAudio *outputAudioState
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV1)
		eventsTx := make(chan Event, 1)
		id := "item-x"
		ev := Event{Kind: EventKindAudioOut, AudioOut: &protocol.RealtimeAudioFrame{
			SampleRate: 1000, SamplesPerChannel: u32(1000), ItemID: &id,
		}}
		if _, err := applyServerEventEffects(ctx, ev, w, eventsTx, st, SessionKindV1, &outputAudio, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if outputAudio != nil {
			t.Fatalf("expected no output audio state, got %+v", outputAudio)
		}
	})

	t.Run("v2 speech started truncates", func(t *testing.T) {
		w := newFakeWriter()
		outputAudio := &outputAudioState{itemID: "item-1", audioEndMS: 250}
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)

		ev := Event{Kind: EventKindInputAudioSpeechStarted, InputAudioSpeechStarted: &InputAudioSpeechStarted{ItemID: strPtr("item-1")}}
		if _, err := applyServerEventEffects(ctx, ev, w, eventsTx, st, SessionKindV2, &outputAudio, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if outputAudio != nil {
			t.Fatalf("expected output audio cleared")
		}
		msgs := w.messages()
		if len(msgs) != 1 || msgs[0].kind != sentPayload {
			t.Fatalf("expected one truncate payload, got %+v", msgs)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(msgs[0].payload), &payload); err != nil {
			t.Fatalf("payload not JSON: %v", err)
		}
		if payload["type"] != "conversation.item.truncate" || payload["item_id"] != "item-1" {
			t.Fatalf("unexpected truncate payload %v", payload)
		}
	})

	t.Run("v2 speech started for other item does not truncate", func(t *testing.T) {
		w := newFakeWriter()
		outputAudio := &outputAudioState{itemID: "item-1", audioEndMS: 250}
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)
		ev := Event{Kind: EventKindInputAudioSpeechStarted, InputAudioSpeechStarted: &InputAudioSpeechStarted{ItemID: strPtr("other")}}
		if _, err := applyServerEventEffects(ctx, ev, w, eventsTx, st, SessionKindV2, &outputAudio, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if outputAudio != nil {
			t.Fatalf("expected output audio cleared even when item differs")
		}
		if w.countOf(sentPayload) != 0 {
			t.Fatalf("expected no truncate for differing item")
		}
	})

	t.Run("v2 response created marks queue started", func(t *testing.T) {
		w := newFakeWriter()
		var outputAudio *outputAudioState
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)
		ev := Event{Kind: EventKindResponseCreated, Response: &ResponseLifecycle{}}
		if _, err := applyServerEventEffects(ctx, ev, w, eventsTx, st, SessionKindV2, &outputAudio, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if !q.activeDefaultResponse {
			t.Fatalf("expected active response after ResponseCreated")
		}
	})

	t.Run("v2 response done flushes deferred create", func(t *testing.T) {
		w := newFakeWriter()
		var outputAudio *outputAudioState
		q := responseCreateQueue{activeDefaultResponse: true, pendingCreate: true}
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)
		ev := Event{Kind: EventKindResponseDone, Response: &ResponseLifecycle{}}
		if _, err := applyServerEventEffects(ctx, ev, w, eventsTx, st, SessionKindV2, &outputAudio, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if w.countOf(sentCreate) != 1 {
			t.Fatalf("expected deferred response.create to flush")
		}
		if !q.activeDefaultResponse {
			t.Fatalf("expected new active response")
		}
	})

	t.Run("v2 noop acknowledged", func(t *testing.T) {
		w := newFakeWriter()
		var outputAudio *outputAudioState
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)
		ev := Event{Kind: EventKindNoopRequested, Noop: &NoopRequested{CallID: "c1"}}
		if _, err := applyServerEventEffects(ctx, ev, w, eventsTx, st, SessionKindV2, &outputAudio, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		msgs := w.messages()
		if len(msgs) != 1 || msgs[0].kind != sentFnOut || msgs[0].callID != "c1" || msgs[0].output != "" {
			t.Fatalf("unexpected noop ack %+v", msgs)
		}
	})

	t.Run("error event signals stop", func(t *testing.T) {
		w := newFakeWriter()
		var outputAudio *outputAudioState
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)
		stop, err := applyServerEventEffects(ctx, NewError("x"), w, eventsTx, st, SessionKindV2, &outputAudio, &q)
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if !stop {
			t.Fatalf("expected stop=true for error event")
		}
	})
}

func TestApplyHandoffRequested(t *testing.T) {
	ctx := context.Background()

	t.Run("v1 records active handoff", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV1)
		eventsTx := make(chan Event, 1)
		err := applyHandoffRequested(ctx, &HandoffRequested{HandoffID: "h1"}, w, eventsTx, st, SessionKindV1, &q)
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if id, ok := st.activeID(); !ok || id != "h1" {
			t.Fatalf("expected active handoff h1, got %q ok=%v", id, ok)
		}
	})

	t.Run("v2 first handoff records active", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		eventsTx := make(chan Event, 1)
		if err := applyHandoffRequested(ctx, &HandoffRequested{HandoffID: "h1"}, w, eventsTx, st, SessionKindV2, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if id, ok := st.activeID(); !ok || id != "h1" {
			t.Fatalf("expected active handoff h1")
		}
		if w.countOf(sentFnOut) != 0 {
			t.Fatalf("first handoff should not send steering ack")
		}
	})

	t.Run("v2 second handoff steers", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		st.setActive(strPtr("h1"))
		eventsTx := make(chan Event, 1)
		if err := applyHandoffRequested(ctx, &HandoffRequested{HandoffID: "h2"}, w, eventsTx, st, SessionKindV2, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		msgs := w.messages()
		if len(msgs) == 0 || msgs[0].kind != sentFnOut || msgs[0].callID != "h2" || msgs[0].output != v2SteerAck {
			t.Fatalf("expected steering ack, got %+v", msgs)
		}
		// Active handoff must remain h1 (steering does not replace it).
		if id, _ := st.activeID(); id != "h1" {
			t.Fatalf("active handoff changed to %q", id)
		}
		if w.countOf(sentCreate) != 1 {
			t.Fatalf("expected response.create after steering")
		}
	})
}

func TestHandleHandoffOutput(t *testing.T) {
	ctx := context.Background()

	t.Run("channel closed ends loop", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV1)
		eventsTx := make(chan Event, 1)
		err := handleHandoffOutput(ctx, handoffOutput{}, false, w, eventsTx, st, SessionKindV1, &q)
		if err == nil {
			t.Fatalf("expected error on closed channel")
		}
	})

	t.Run("v1 progress resolves function output", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV1)
		eventsTx := make(chan Event, 1)
		out := handoffOutput{kind: handoffProgress, handoffID: "h1", outputText: "partial"}
		if err := handleHandoffOutput(ctx, out, true, w, eventsTx, st, SessionKindV1, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		msgs := w.messages()
		if len(msgs) != 1 || msgs[0].kind != sentFnOut || msgs[0].output != "partial" {
			t.Fatalf("unexpected v1 output %+v", msgs)
		}
	})

	t.Run("v2 progress for active handoff sends item", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		st.setActive(strPtr("h1"))
		eventsTx := make(chan Event, 1)
		out := handoffOutput{kind: handoffProgress, handoffID: "h1", outputText: "[BACKEND] hi"}
		if err := handleHandoffOutput(ctx, out, true, w, eventsTx, st, SessionKindV2, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if w.countOf(sentItem) != 1 {
			t.Fatalf("expected conversation item create")
		}
	})

	t.Run("v2 progress for stale handoff dropped", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		st.setActive(strPtr("current"))
		eventsTx := make(chan Event, 1)
		out := handoffOutput{kind: handoffProgress, handoffID: "stale", outputText: "x"}
		if err := handleHandoffOutput(ctx, out, true, w, eventsTx, st, SessionKindV2, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		if len(w.messages()) != 0 {
			t.Fatalf("stale handoff should be dropped, got %+v", w.messages())
		}
	})

	t.Run("v2 final sends ack and requests create", func(t *testing.T) {
		w := newFakeWriter()
		var q responseCreateQueue
		st := newHandoffState(make(chan handoffOutput, 1), SessionKindV2)
		st.setActive(strPtr("h1"))
		eventsTx := make(chan Event, 1)
		out := handoffOutput{kind: handoffFinal, handoffID: "h1", outputText: "ignored"}
		if err := handleHandoffOutput(ctx, out, true, w, eventsTx, st, SessionKindV2, &q); err != nil {
			t.Fatalf("err=%v", err)
		}
		msgs := w.messages()
		if w.countOf(sentFnOut) != 1 || msgs[0].output != v2HandoffCompleteAck {
			t.Fatalf("expected completion ack, got %+v", msgs)
		}
		if w.countOf(sentCreate) != 1 {
			t.Fatalf("expected response.create after final handoff")
		}
	})
}
