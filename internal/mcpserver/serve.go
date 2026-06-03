package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/sqlrush/codexgo/internal/appserver"
)

// maxLineBytes bounds a single newline-delimited JSON-RPC message so a hostile
// or buggy peer cannot exhaust memory. It is generous (16 MiB).
const maxLineBytes = 16 * 1024 * 1024

// lineWriter is a [frameWriter] that serializes each frame as one
// newline-terminated JSON line onto an io.Writer. Concurrent writes are
// serialized with a mutex so approval tasks, event forwarders, and request
// handlers never interleave bytes on the wire. It mirrors the Rust stdout writer
// task (one line per outgoing message, flushed immediately).
type lineWriter struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// newLineWriter wraps w in a buffered, mutex-guarded frame writer.
func newLineWriter(w io.Writer) *lineWriter {
	return &lineWriter{w: bufio.NewWriter(w)}
}

// WriteFrame marshals v and writes it as one newline-terminated line.
func (l *lineWriter) WriteFrame(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.w.Write(raw); err != nil {
		return err
	}
	if err := l.w.WriteByte('\n'); err != nil {
		return err
	}
	return l.w.Flush()
}

// ServeStdio runs the Codex MCP server over a newline-delimited JSON-RPC byte
// stream (the standard MCP stdio transport). It reads frames from r and writes
// responses, errors, and notifications to w. The connection is single-client.
//
// ServeStdio blocks until r reaches EOF, ctx is cancelled, or a fatal read error
// occurs. On return it shuts down any running thread forwarders. It mirrors the
// Rust run_main task topology (stdin reader + processor + stdout writer) reduced
// to a single serial read loop where long-running tool calls run on their own
// goroutines.
func ServeStdio(ctx context.Context, assembly *appserver.Assembly, defaults appserver.Defaults, r io.Reader, w io.Writer) error {
	writer := newLineWriter(w)
	proc := NewMessageProcessor(ProcessorConfig{
		Assembly:  assembly,
		Defaults:  defaults,
		Writer:    writer,
		UserAgent: defaults.UserAgent,
	})
	err := serveConnection(ctx, proc, r)
	proc.Shutdown(context.Background())
	return err
}

// serveConnection runs the read/dispatch loop for one connection. Each frame is
// decoded and dispatched to the processor. Long-running tool calls spawn their
// own goroutines inside the processor, so the loop is never blocked.
func serveConnection(ctx context.Context, proc *MessageProcessor, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		buf := make([]byte, len(line))
		copy(buf, line)

		var msg incomingMessage
		if err := json.Unmarshal(buf, &msg); err != nil {
			// Malformed JSON cannot be correlated with a request id; the reference
			// logs and skips. Drop the frame.
			continue
		}
		proc.processFrame(ctx, msg)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
