package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// maxLineBytes bounds a single newline-delimited JSON-RPC message so a hostile
// or buggy peer cannot exhaust memory. It is generous (16 MiB) to accommodate
// large resume histories.
const maxLineBytes = 16 * 1024 * 1024

// writerSink is an [OutgoingSink] that serializes messages as newline-delimited
// JSON onto an io.Writer. It serializes concurrent writes with a mutex so the
// event forwarder and request handlers never interleave bytes on the wire.
type writerSink struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// newWriterSink wraps w in a buffered, mutex-guarded sink.
func newWriterSink(w io.Writer) *writerSink {
	return &writerSink{w: bufio.NewWriter(w)}
}

// Send marshals msg and writes it as a single newline-terminated line, flushing
// immediately so the peer sees each message promptly.
func (s *writerSink) Send(msg appserverproto.JSONRPCMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(raw); err != nil {
		return err
	}
	if err := s.w.WriteByte('\n'); err != nil {
		return err
	}
	return s.w.Flush()
}

// serveConnection runs the read/dispatch loop for one connection over a
// newline-delimited JSON-RPC byte stream. Each request is dispatched on its own
// goroutine so a long-running turn does not block subsequent requests, mirroring
// the Rust per-request task spawning. Responses, errors, and notifications are
// written through sink.
//
// The loop returns when the reader reaches EOF, ctx is cancelled, or a fatal
// read error occurs. It waits for in-flight request goroutines before returning
// so the processor's forwarders are not left dangling.
func serveConnection(ctx context.Context, proc *Processor, conn *Conn, r io.Reader, sink OutgoingSink) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var wg sync.WaitGroup
	defer wg.Wait()

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Copy the line: the scanner reuses its buffer on the next Scan.
		buf := make([]byte, len(line))
		copy(buf, line)

		var msg appserverproto.JSONRPCMessage
		if err := json.Unmarshal(buf, &msg); err != nil {
			// Malformed JSON cannot be correlated with a request id, so it is logged
			// and skipped, matching the reference (which drops unparseable frames).
			continue
		}

		switch msg.Kind {
		case appserverproto.MessageKindRequest:
			req := *msg.Request
			wg.Add(1)
			go func() {
				defer wg.Done()
				proc.HandleRequest(ctx, conn, req)
			}()
		case appserverproto.MessageKindNotification:
			// Clients are not expected to send notifications; drop them (the Rust
			// process_notification merely logs).
		default:
			// Responses/errors to server-initiated requests are not handled in this
			// subset; drop them.
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
