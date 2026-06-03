package client

import (
	"context"
	"testing"
	"time"
)

// chunksToStream builds a ByteStream from a sequence of byte slices.
func chunksToStream(chunks ...[]byte) ByteStream {
	ch := make(chan ByteChunk, len(chunks))
	for _, c := range chunks {
		ch <- ByteChunk{Data: c}
	}
	close(ch)
	return ch
}

func TestSSEStreamParsesDataFrames(t *testing.T) {
	stream := chunksToStream(
		[]byte("event: a\ndata: hello\n\n"),
		[]byte("event: b\ndata: world\n\n"),
	)
	results := SSEStream(context.Background(), stream, time.Second)

	var datas []string
	var sawClosed bool
	for r := range results {
		if r.Event != nil {
			datas = append(datas, r.Event.Data)
		}
		if r.Err != nil {
			sawClosed = true
		}
	}
	if len(datas) != 2 || datas[0] != "hello" || datas[1] != "world" {
		t.Fatalf("unexpected datas: %v", datas)
	}
	if !sawClosed {
		t.Fatalf("expected a closed-before-completion error after events")
	}
}

func TestSSEStreamSplitAcrossChunks(t *testing.T) {
	stream := chunksToStream(
		[]byte("event: a\nda"),
		[]byte("ta: split"),
		[]byte("value\n\n"),
	)
	results := SSEStream(context.Background(), stream, time.Second)
	var data string
	for r := range results {
		if r.Event != nil {
			data = r.Event.Data
		}
	}
	if data != "splitvalue" {
		t.Fatalf("unexpected data %q", data)
	}
}

func TestSSEStreamIdleTimeout(t *testing.T) {
	// Never-closing stream that emits nothing.
	ch := make(chan ByteChunk)
	results := SSEStream(context.Background(), ch, 30*time.Millisecond)
	var gotTimeout bool
	for r := range results {
		if r.Err != nil && r.Err.Kind == StreamErrorTimeout {
			gotTimeout = true
		}
	}
	if !gotTimeout {
		t.Fatalf("expected idle timeout error")
	}
	close(ch)
}

func TestSSEStreamForwardsStreamError(t *testing.T) {
	ch := make(chan ByteChunk, 1)
	ch <- ByteChunk{Err: NewNetworkError("boom")}
	close(ch)
	results := SSEStream(context.Background(), ch, time.Second)
	var gotErr bool
	for r := range results {
		if r.Err != nil && r.Err.Kind == StreamErrorStream {
			gotErr = true
		}
	}
	if !gotErr {
		t.Fatalf("expected stream error forwarded")
	}
}

func TestParseEventBlockNoDataReturnsNil(t *testing.T) {
	if ev := parseEventBlock([]byte("event: ping")); ev != nil {
		t.Fatalf("expected nil for data-less event, got %+v", ev)
	}
}

func TestParseEventBlockMultilineData(t *testing.T) {
	ev := parseEventBlock([]byte("data: line1\ndata: line2"))
	if ev == nil || ev.Data != "line1\nline2" {
		t.Fatalf("unexpected event %+v", ev)
	}
}
