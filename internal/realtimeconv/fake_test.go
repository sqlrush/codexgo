package realtimeconv

import (
	"context"
	"fmt"
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// sentKind tags an outbound message recorded by fakeWriter.
type sentKind string

const (
	sentAudio   sentKind = "audio"
	sentItem    sentKind = "item_create"
	sentFnOut   sentKind = "function_call_output"
	sentCreate  sentKind = "response_create"
	sentPayload sentKind = "payload"
)

// sentMessage is one recorded outbound message.
type sentMessage struct {
	kind    sentKind
	text    string
	callID  string
	output  string
	payload string
	frame   protocol.RealtimeAudioFrame
}

// fakeWriter records outbound writer calls and can be primed to fail a given
// method, mirroring the failure paths exercised in the Rust tests.
type fakeWriter struct {
	mu   sync.Mutex
	sent []sentMessage

	// failOn maps a sentKind to the error returned for the next matching call.
	failOn map[sentKind]error
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{failOn: map[sentKind]error{}}
}

func (w *fakeWriter) fail(kind sentKind, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failOn[kind] = err
}

func (w *fakeWriter) record(m sentMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err, ok := w.failOn[m.kind]; ok {
		delete(w.failOn, m.kind)
		return err
	}
	w.sent = append(w.sent, m)
	return nil
}

func (w *fakeWriter) messages() []sentMessage {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]sentMessage, len(w.sent))
	copy(out, w.sent)
	return out
}

func (w *fakeWriter) countOf(kind sentKind) int {
	n := 0
	for _, m := range w.messages() {
		if m.kind == kind {
			n++
		}
	}
	return n
}

func (w *fakeWriter) SendAudioFrame(_ context.Context, frame protocol.RealtimeAudioFrame) error {
	return w.record(sentMessage{kind: sentAudio, frame: frame})
}

func (w *fakeWriter) SendConversationItemCreate(_ context.Context, text string) error {
	return w.record(sentMessage{kind: sentItem, text: text})
}

func (w *fakeWriter) SendConversationFunctionCallOutput(_ context.Context, callID, outputText string) error {
	return w.record(sentMessage{kind: sentFnOut, callID: callID, output: outputText})
}

func (w *fakeWriter) SendResponseCreate(_ context.Context) error {
	return w.record(sentMessage{kind: sentCreate})
}

func (w *fakeWriter) SendPayload(_ context.Context, payload string) error {
	return w.record(sentMessage{kind: sentPayload, payload: payload})
}

// fakeEvents replays a scripted sequence of server events, then signals
// end-of-stream (nil event, nil error). A non-nil err entry simulates a
// transport failure.
type fakeEvents struct {
	mu     sync.Mutex
	queue  []serverScript
	closed bool
}

type serverScript struct {
	event *Event
	err   error
}

func newFakeEvents(scripts ...serverScript) *fakeEvents {
	return &fakeEvents{queue: scripts}
}

func (e *fakeEvents) NextEvent(ctx context.Context) (*Event, error) {
	e.mu.Lock()
	if len(e.queue) == 0 {
		e.mu.Unlock()
		// Block until ctx is cancelled to model a quiescent stream.
		<-ctx.Done()
		return nil, nil
	}
	next := e.queue[0]
	e.queue = e.queue[1:]
	e.mu.Unlock()
	return next.event, next.err
}

// fakeConn bundles a fakeWriter and fakeEvents into a Connection.
type fakeConn struct {
	writer *fakeWriter
	events *fakeEvents
}

func newFakeConn(events *fakeEvents) *fakeConn {
	return &fakeConn{writer: newFakeWriter(), events: events}
}

func (c *fakeConn) Writer() Writer { return c.writer }
func (c *fakeConn) Events() Events { return c.events }

// strPtr is a small helper for building optional string fields in tests.
func strPtr(s string) *string { return &s }

// errString returns a deterministic error for priming fakeWriter failures.
func errString(s string) error { return fmt.Errorf("%s", s) }
