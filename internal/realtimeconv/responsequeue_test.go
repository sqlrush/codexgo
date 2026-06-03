package realtimeconv

import (
	"context"
	"testing"
)

func TestResponseCreateQueue(t *testing.T) {
	ctx := context.Background()

	t.Run("immediate create when idle", func(t *testing.T) {
		w := newFakeWriter()
		eventsTx := make(chan Event, 1)
		var q responseCreateQueue
		if err := q.requestCreate(ctx, w, eventsTx, "x"); err != nil {
			t.Fatalf("err=%v", err)
		}
		if w.countOf(sentCreate) != 1 {
			t.Fatalf("expected immediate create")
		}
		if !q.activeDefaultResponse {
			t.Fatalf("expected active response set")
		}
	})

	t.Run("defers while active then flushes on finish", func(t *testing.T) {
		w := newFakeWriter()
		eventsTx := make(chan Event, 1)
		q := responseCreateQueue{activeDefaultResponse: true}
		if err := q.requestCreate(ctx, w, eventsTx, "x"); err != nil {
			t.Fatalf("err=%v", err)
		}
		if w.countOf(sentCreate) != 0 {
			t.Fatalf("should defer while active")
		}
		if !q.pendingCreate {
			t.Fatalf("expected pending create")
		}
		if err := q.markFinished(ctx, w, eventsTx, "deferred"); err != nil {
			t.Fatalf("err=%v", err)
		}
		if w.countOf(sentCreate) != 1 {
			t.Fatalf("expected deferred create to flush")
		}
		if q.pendingCreate {
			t.Fatalf("pending create should be cleared")
		}
	})

	t.Run("finish without pending is noop", func(t *testing.T) {
		w := newFakeWriter()
		eventsTx := make(chan Event, 1)
		q := responseCreateQueue{activeDefaultResponse: true}
		if err := q.markFinished(ctx, w, eventsTx, "deferred"); err != nil {
			t.Fatalf("err=%v", err)
		}
		if w.countOf(sentCreate) != 0 {
			t.Fatalf("expected no create")
		}
		if q.activeDefaultResponse {
			t.Fatalf("expected inactive after finish")
		}
	})

	t.Run("active response race re-defers", func(t *testing.T) {
		w := newFakeWriter()
		w.fail(sentCreate, errString(activeResponseErrorPrefix+" resp_123"))
		eventsTx := make(chan Event, 1)
		var q responseCreateQueue
		if err := q.requestCreate(ctx, w, eventsTx, "x"); err != nil {
			t.Fatalf("race should be treated as success, got %v", err)
		}
		if !q.activeDefaultResponse || !q.pendingCreate {
			t.Fatalf("expected re-deferred state, got %+v", q)
		}
		select {
		case <-eventsTx:
			t.Fatalf("race should not emit an error event")
		default:
		}
	})

	t.Run("other send error surfaces and emits event", func(t *testing.T) {
		w := newFakeWriter()
		w.fail(sentCreate, errString("network down"))
		eventsTx := make(chan Event, 1)
		var q responseCreateQueue
		if err := q.requestCreate(ctx, w, eventsTx, "x"); err == nil {
			t.Fatalf("expected error to surface")
		}
		select {
		case ev := <-eventsTx:
			if !ev.IsError() || ev.ErrorMessage != "network down" {
				t.Fatalf("unexpected event %+v", ev)
			}
		default:
			t.Fatalf("expected error event emitted")
		}
	})
}
