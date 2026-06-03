package appserver

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/uds"
)

// ProcessorFactory builds a fresh [Processor] bound to the given [OutgoingSink].
// Each accepted connection gets its own processor (and thus its own per-thread
// forwarders), mirroring the Rust per-connection MessageProcessor wiring. The
// factory typically captures a shared [Assembly] so all connections drive the
// same engine.
type ProcessorFactory func(sink OutgoingSink) *Processor

// NewProcessorFactory returns a [ProcessorFactory] that builds processors over a
// shared assembly and defaults.
func NewProcessorFactory(assembly *Assembly, defaults Defaults) ProcessorFactory {
	return func(sink OutgoingSink) *Processor {
		return NewProcessor(ProcessorConfig{
			Assembly: assembly,
			Defaults: defaults,
			Sink:     sink,
		})
	}
}

// ServeUDS binds a Unix domain socket at socketPath and serves each accepted
// connection with a processor built by factory. It blocks until ctx is
// cancelled or the listener fails. Each connection is handled in its own
// goroutine; the loop returns when ctx is cancelled (closing the listener).
//
// On unsupported platforms (e.g. constrained sandboxes), Bind may fail; the
// error is wrapped and returned.
func ServeUDS(ctx context.Context, factory ProcessorFactory, socketPath string) error {
	listener, err := uds.Bind(socketPath)
	if err != nil {
		return fmt.Errorf("appserver: bind uds %q: %w", socketPath, err)
	}

	// Close the listener when ctx is cancelled so Accept unblocks.
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		stream, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("appserver: uds accept: %w", acceptErr)
		}
		go serveUDSConnection(ctx, factory, stream)
	}
}

// serveUDSConnection serves one accepted UDS connection. The stream is both the
// reader and the writer; a writerSink serializes outbound frames.
func serveUDSConnection(ctx context.Context, factory ProcessorFactory, stream *uds.UnixStream) {
	defer stream.Close()
	sink := newWriterSink(stream)
	proc := factory(sink)
	conn := newConnectionState()
	_ = serveConnection(ctx, proc, conn, stream, sink)
	proc.Shutdown(context.Background())
}
