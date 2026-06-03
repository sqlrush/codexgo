package execserver

import (
	"context"
	"testing"
	"time"
)

func TestEventHistoryReplayIsBoundedByRetainedBytes(t *testing.T) {
	log := newExecProcessEventLog(8 /*eventCapacity*/, 3 /*byteCapacity*/)

	log.publish(NewOutputEvent(ProcessOutputChunk{
		Seq:    1,
		Stream: ExecOutputStreamStdout,
		Chunk:  []byte("large"),
	}))
	log.publish(NewExitedEvent(2, 0))
	log.publish(NewClosedEvent(3))

	receiver := log.subscribe()
	defer receiver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	first, ok := receiver.Recv(ctx)
	if !ok {
		t.Fatalf("expected exited replay event")
	}
	if first.Kind != ExecProcessEventExited || first.SeqNum != 2 {
		t.Fatalf("unexpected first replay event: %+v", first)
	}
	second, ok := receiver.Recv(ctx)
	if !ok {
		t.Fatalf("expected closed replay event")
	}
	if second.Kind != ExecProcessEventClosed || second.SeqNum != 3 {
		t.Fatalf("unexpected second replay event: %+v", second)
	}
}

func TestEventLogLiveFanOut(t *testing.T) {
	log := newExecProcessEventLog(8, 1024)
	receiver := log.subscribe()
	defer receiver.Close()

	log.publish(NewOutputEvent(ProcessOutputChunk{Seq: 1, Stream: ExecOutputStreamStdout, Chunk: []byte("a")}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, ok := receiver.Recv(ctx)
	if !ok {
		t.Fatalf("expected live event")
	}
	if event.Kind != ExecProcessEventOutput || string(event.Output.Chunk) != "a" {
		t.Fatalf("unexpected live event: %+v", event)
	}
}

func TestEmptyEventReceiver(t *testing.T) {
	receiver := EmptyEventReceiver()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := receiver.Recv(ctx); ok {
		t.Fatalf("empty receiver should not yield events")
	}
}

func TestExecProcessEventSeq(t *testing.T) {
	tests := []struct {
		event ExecProcessEvent
		seq   uint64
		ok    bool
	}{
		{NewOutputEvent(ProcessOutputChunk{Seq: 5}), 5, true},
		{NewExitedEvent(6, 0), 6, true},
		{NewClosedEvent(7), 7, true},
		{NewFailedEvent("x"), 0, false},
	}
	for _, tt := range tests {
		seq, ok := tt.event.Seq()
		if seq != tt.seq || ok != tt.ok {
			t.Errorf("Seq() = (%d, %v), want (%d, %v) for %+v", seq, ok, tt.seq, tt.ok, tt.event)
		}
	}
}
