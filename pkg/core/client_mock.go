package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/sqlrush/codexgo/internal/api"
)

// MockModelClient is a [ModelClient] that replays scripted responses. It is the
// Go analogue of the Rust test fakes used to drive the turn runner without a
// live provider. Each call to Stream pops the next scripted turn and replays its
// events on the returned channel.
//
// MockModelClient is safe for concurrent use; the scripted-turn cursor is
// guarded by a mutex. It records the prompts it received so tests can assert on
// the request the engine built.
type MockModelClient struct {
	mu sync.Mutex

	// slug / contextWindow back the ModelClient metadata methods.
	slug          string
	contextWindow *int64

	// turns is the queue of scripted turns. Each turn is the ordered set of
	// events to replay for one Stream call.
	turns [][]api.ResponseEvent
	// errs[i], when non-nil, makes the i-th Stream call return that error before
	// any events are produced.
	errs []error
	// cursor is the index of the next scripted turn to replay.
	cursor int

	// receivedPrompts records every Prompt passed to Stream, in order.
	receivedPrompts []Prompt
}

// compile-time assertion that MockModelClient satisfies the ModelClient
// interface.
var _ ModelClient = (*MockModelClient)(nil)

// MockTurn is one scripted turn: the events to replay and an optional terminal
// error returned by Stream before any events.
type MockTurn struct {
	// Events are replayed in order on the Stream channel.
	Events []api.ResponseEvent
	// StreamErr, when non-nil, is returned by Stream instead of producing events.
	StreamErr error
}

// NewMockModelClient builds a mock client that replays the given turns in order.
// slug and contextWindow back the metadata accessors.
func NewMockModelClient(slug string, contextWindow *int64, turns ...MockTurn) *MockModelClient {
	m := &MockModelClient{slug: slug, contextWindow: contextWindow}
	for _, t := range turns {
		m.turns = append(m.turns, t.Events)
		m.errs = append(m.errs, t.StreamErr)
	}
	return m
}

// ModelSlug returns the configured slug.
func (m *MockModelClient) ModelSlug() string { return m.slug }

// ContextWindow returns the configured context window.
func (m *MockModelClient) ContextWindow() *int64 { return m.contextWindow }

// Stream replays the next scripted turn. When the script is exhausted it returns
// an error so a runaway turn loop is surfaced rather than silently producing an
// empty stream.
func (m *MockModelClient) Stream(ctx context.Context, prompt Prompt) (<-chan api.ResponseEvent, error) {
	m.mu.Lock()
	m.receivedPrompts = append(m.receivedPrompts, prompt)
	if m.cursor >= len(m.turns) {
		m.mu.Unlock()
		return nil, fmt.Errorf("core: MockModelClient: no scripted turn for call %d", m.cursor)
	}
	idx := m.cursor
	m.cursor++
	events := m.turns[idx]
	err := m.errs[idx]
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	out := make(chan api.ResponseEvent, len(events))
	go func() {
		defer close(out)
		for _, ev := range events {
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
		}
	}()
	return out, nil
}

// StreamWithErrors replays the next scripted turn on the error-aware [StreamItem]
// channel, mirroring [ResponsesModelClient.StreamWithErrors]. A scripted
// StreamErr is surfaced as a terminal [StreamItem.Err] (after exhausting the
// turn's events) rather than as a Stream-level error, so the turn runner can
// exercise its mid-stream error handling against the mock.
//
// Note: because the foundation [ModelClient] interface only requires Stream, this
// method is available exclusively on the concrete *MockModelClient. Callers that
// need the error-aware path against a mock should accept the concrete type or a
// local interface that includes StreamWithErrors.
func (m *MockModelClient) StreamWithErrors(ctx context.Context, prompt Prompt) (<-chan StreamItem, error) {
	m.mu.Lock()
	m.receivedPrompts = append(m.receivedPrompts, prompt)
	if m.cursor >= len(m.turns) {
		m.mu.Unlock()
		return nil, fmt.Errorf("core: MockModelClient: no scripted turn for call %d", m.cursor)
	}
	idx := m.cursor
	m.cursor++
	events := m.turns[idx]
	streamErr := m.errs[idx]
	m.mu.Unlock()

	out := make(chan StreamItem, len(events)+1)
	go func() {
		defer close(out)
		for _, ev := range events {
			ev := ev
			select {
			case <-ctx.Done():
				return
			case out <- StreamItem{Event: &ev}:
			}
		}
		if streamErr != nil {
			select {
			case <-ctx.Done():
			case out <- StreamItem{Err: streamErr}:
			}
		}
	}()
	return out, nil
}

// ReceivedPrompts returns a copy of the prompts the mock has been asked to
// stream, in call order.
func (m *MockModelClient) ReceivedPrompts() []Prompt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Prompt(nil), m.receivedPrompts...)
}

// LastPrompt returns the most recent prompt passed to Stream/StreamWithErrors, or
// the zero Prompt when none have been recorded. It is a convenience for tests
// asserting on the latest request the engine built.
func (m *MockModelClient) LastPrompt() (Prompt, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.receivedPrompts) == 0 {
		return Prompt{}, false
	}
	return m.receivedPrompts[len(m.receivedPrompts)-1], true
}

// Reset rewinds the scripted-turn cursor and clears the recorded prompts so the
// same mock can be reused across sub-tests.
func (m *MockModelClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cursor = 0
	m.receivedPrompts = nil
}

// CallCount returns how many times Stream has been invoked.
func (m *MockModelClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.receivedPrompts)
}
