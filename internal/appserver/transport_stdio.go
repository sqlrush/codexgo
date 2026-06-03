package appserver

import (
	"context"
	"io"
)

// ServeStdio runs the app-server over a newline-delimited JSON-RPC byte stream
// (the primary transport). It reads requests from r and writes responses,
// errors, and notifications to w. The connection is single-client: one
// [connectionState] is created and the initialize handshake gates all other
// methods.
//
// ServeStdio blocks until r reaches EOF, ctx is cancelled, or a fatal I/O error
// occurs. On return it shuts down any running thread forwarders.
func ServeStdio(ctx context.Context, proc *Processor, r io.Reader, w io.Writer) error {
	sink := newWriterSink(w)
	conn := newConnectionState()
	err := serveConnection(ctx, proc, conn, r, sink)
	proc.Shutdown(context.Background())
	return err
}

// ServeStdioWithProcessor is a convenience wrapper that builds a [Processor]
// bound to the stdio sink, then serves it. It is the typical entry point for a
// `codex app-server` binary: assemble the engine, then call this.
func ServeStdioWithProcessor(ctx context.Context, assembly *Assembly, defaults Defaults, r io.Reader, w io.Writer) error {
	sink := newWriterSink(w)
	proc := NewProcessor(ProcessorConfig{
		Assembly: assembly,
		Defaults: defaults,
		Sink:     sink,
	})
	conn := newConnectionState()
	err := serveConnection(ctx, proc, conn, r, sink)
	proc.Shutdown(context.Background())
	return err
}
