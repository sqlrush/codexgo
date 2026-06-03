package client

import (
	"bytes"
	"context"
	"strings"
	"time"
)

// SSEvent is a parsed Server-Sent Event. Only the fields Codex consumes are
// modeled; the `data` field is the concatenation of all `data:` lines.
type SSEvent struct {
	Event string
	Data  string
	ID    string
}

// SSEResult is one item emitted by the SSE parser: either an event or a stream
// error.
type SSEResult struct {
	Event *SSEvent
	Err   *StreamError
}

// SSEStream parses an SSE byte stream and forwards each event's data over the
// returned channel. It mirrors the Rust `sse_stream`: it applies an idle timeout
// between events, and on idle timeout or stream error it emits a final error
// result and stops. When the underlying stream closes before completion it emits
// a "stream closed before completion" error, matching Rust.
//
// The returned channel is closed when the stream ends. The goroutine respects
// ctx cancellation.
func SSEStream(ctx context.Context, stream ByteStream, idleTimeout time.Duration) <-chan SSEResult {
	out := make(chan SSEResult, 16)
	go func() {
		defer close(out)
		events := parseSSE(ctx, stream, idleTimeout)
		for res := range events {
			select {
			case <-ctx.Done():
				return
			case out <- res:
			}
			if res.Err != nil {
				return
			}
		}
	}()
	return out
}

// parseSSE turns a raw ByteStream into a stream of SSEResult values, applying the
// idle timeout between successfully-parsed events. This is the shared SSE framing
// used by both the client SSE helper and the API responses parser.
func parseSSE(ctx context.Context, stream ByteStream, idleTimeout time.Duration) <-chan SSEResult {
	out := make(chan SSEResult, 16)
	go func() {
		defer close(out)

		var buf bytes.Buffer
		// pending collects parsed-but-undelivered events extracted from buf.
		emit := func(res SSEResult) bool {
			select {
			case <-ctx.Done():
				return false
			case out <- res:
				return true
			}
		}

		// readNext reads the next byte chunk subject to the idle timeout.
		readNext := func() (ByteChunk, bool, bool) {
			var timer *time.Timer
			var timeoutCh <-chan time.Time
			if idleTimeout > 0 {
				timer = time.NewTimer(idleTimeout)
				timeoutCh = timer.C
				defer timer.Stop()
			}
			select {
			case <-ctx.Done():
				return ByteChunk{}, false, false
			case <-timeoutCh:
				return ByteChunk{}, false, true
			case chunk, ok := <-stream:
				if !ok {
					return ByteChunk{}, false, false
				}
				return chunk, true, false
			}
		}

		for {
			// Drain any complete events already buffered.
			for {
				event, rest, found := extractEvent(buf.Bytes())
				if !found {
					break
				}
				buf.Reset()
				buf.Write(rest)
				if event != nil {
					if !emit(SSEResult{Event: event}) {
						return
					}
				}
			}

			chunk, ok, timedOut := readNext()
			if timedOut {
				emit(SSEResult{Err: NewStreamTimeoutError()})
				return
			}
			if !ok {
				// Context cancelled or stream closed. Flush a trailing event
				// if the buffer holds one without a terminating blank line.
				if event := flushEvent(buf.Bytes()); event != nil {
					if !emit(SSEResult{Event: event}) {
						return
					}
				}
				emit(SSEResult{Err: NewStreamError("stream closed before completion")})
				return
			}
			if chunk.Err != nil {
				emit(SSEResult{Err: NewStreamError(chunk.Err.Error())})
				return
			}
			buf.Write(chunk.Data)
		}
	}()
	return out
}

// extractEvent extracts the first complete SSE event (terminated by a blank
// line) from data. It returns the parsed event (nil if the block had no data),
// the remaining bytes, and whether a complete block was found.
func extractEvent(data []byte) (*SSEvent, []byte, bool) {
	// SSE events are separated by a blank line. Support both "\n\n" and
	// "\r\n\r\n" separators.
	idx, sepLen := findBlankLine(data)
	if idx < 0 {
		return nil, data, false
	}
	block := data[:idx]
	rest := data[idx+sepLen:]
	return parseEventBlock(block), rest, true
}

// flushEvent parses a trailing event block that was not terminated by a blank
// line, used when the stream ends.
func flushEvent(data []byte) *SSEvent {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return parseEventBlock(data)
}

// findBlankLine returns the index of the blank-line separator and its length, or
// (-1, 0) if no blank line is present.
func findBlankLine(data []byte) (int, int) {
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return i, 4
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i, 2
	}
	return -1, 0
}

// parseEventBlock parses a single SSE event block into an SSEvent. It returns nil
// when the block carries no data field, matching eventsource semantics where
// data-less events are not dispatched.
func parseEventBlock(block []byte) *SSEvent {
	lines := strings.Split(strings.ReplaceAll(string(block), "\r\n", "\n"), "\n")
	var (
		eventName string
		id        string
		dataLines []string
		hasData   bool
	)
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		field, value := splitField(line)
		switch field {
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, value)
			hasData = true
		case "id":
			id = value
		}
	}
	if !hasData {
		return nil
	}
	return &SSEvent{
		Event: eventName,
		Data:  strings.Join(dataLines, "\n"),
		ID:    id,
	}
}

// splitField splits an SSE field line into its name and value, stripping a single
// leading space from the value per the SSE spec.
func splitField(line string) (string, string) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return line, ""
	}
	field := line[:idx]
	value := line[idx+1:]
	value = strings.TrimPrefix(value, " ")
	return field, value
}
