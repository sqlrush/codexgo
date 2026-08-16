package core

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// noSleep makes the retry loop skip its backoff for the duration of a test.
func noSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	prev := samplingSleep
	samplingSleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	t.Cleanup(func() { samplingSleep = prev })
	return &waits
}

func retryableAPIErr(msg string) error {
	return api.NewRetryableError(msg, nil)
}

func TestSamplingRetryable(t *testing.T) {
	delay := 3 * time.Second
	cases := []struct {
		name  string
		err   error
		want  bool
		delay *time.Duration
	}{
		{"nil", nil, false, nil},
		{"disconnected", ErrStreamDisconnected, true, nil},
		{"canceled", context.Canceled, false, nil},
		{"turn aborted", ErrTurnAborted, false, nil},
		{"retryable with delay", &api.APIError{Kind: api.APIErrorRetryable, Delay: &delay}, true, &delay},
		{"stream", &api.APIError{Kind: api.APIErrorStream, Message: "eof"}, true, nil},
		{"api status", &api.APIError{Kind: api.APIErrorAPI, Status: 502}, true, nil},
		{"context window", &api.APIError{Kind: api.APIErrorContextWindowExceeded}, false, nil},
		{"quota", &api.APIError{Kind: api.APIErrorQuotaExceeded}, false, nil},
		{"invalid request", &api.APIError{Kind: api.APIErrorInvalidRequest}, false, nil},
		{"server overloaded", &api.APIError{Kind: api.APIErrorServerOverloaded}, false, nil},
		{"transport timeout", &api.APIError{Kind: api.APIErrorTransport, Transport: &client.TransportError{Kind: client.TransportErrorTimeout}}, true, nil},
		{"transport network", &api.APIError{Kind: api.APIErrorTransport, Transport: &client.TransportError{Kind: client.TransportErrorNetwork}}, true, nil},
		{"transport 500", &api.APIError{Kind: api.APIErrorTransport, Transport: &client.TransportError{Kind: client.TransportErrorHTTP, Status: http.StatusInternalServerError}}, true, nil},
		{"transport 400", &api.APIError{Kind: api.APIErrorTransport, Transport: &client.TransportError{Kind: client.TransportErrorHTTP, Status: http.StatusBadRequest}}, false, nil},
		{"transport 429", &api.APIError{Kind: api.APIErrorTransport, Transport: &client.TransportError{Kind: client.TransportErrorHTTP, Status: http.StatusTooManyRequests}}, false, nil},
		{"transport retry limit", &api.APIError{Kind: api.APIErrorTransport, Transport: &client.TransportError{Kind: client.TransportErrorRetryLimit}}, false, nil},
		{"plain error", errors.New("boom"), false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotDelay := samplingRetryable(tc.err)
			if got != tc.want {
				t.Fatalf("retryable = %v, want %v", got, tc.want)
			}
			if (gotDelay == nil) != (tc.delay == nil) || (gotDelay != nil && *gotDelay != *tc.delay) {
				t.Fatalf("delay = %v, want %v", gotDelay, tc.delay)
			}
		})
	}
}

// TestRunSamplingRequestRetriesRetryableStreamError asserts the loop retries a
// retryable failure, emits the "Reconnecting... n/max" stream_error, and then
// completes on the next attempt.
func TestRunSamplingRequestRetriesRetryableStreamError(t *testing.T) {
	waits := noSleep(t)
	mc := NewMockModelClient("gpt-test", nil,
		MockTurn{StreamErr: retryableAPIErr("upstream hiccup")},
		MockTurn{Events: []api.ResponseEvent{evCreated(), evMessageDone("m1", "ok"), evCompleted(true, nil)}},
	)
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	res, err := runSamplingRequest(sess.ctx, sess, tc)
	if err != nil {
		t.Fatalf("runSamplingRequest: %v", err)
	}
	if res.LastAgentMessage == nil || *res.LastAgentMessage != "ok" {
		t.Fatalf("expected the retried attempt's message, got %+v", res)
	}
	events := drainEvents(evCh)
	ev, ok := firstEvent(events, protocol.EventMsgKindStreamError)
	if !ok {
		t.Fatalf("expected a stream_error retry notice, got %v", eventsByKind(events))
	}
	if ev.Msg.StreamError.Message != "Reconnecting... 1/5" {
		t.Fatalf("stream_error message = %q", ev.Msg.StreamError.Message)
	}
	if len(*waits) != 1 || (*waits)[0] < 180*time.Millisecond || (*waits)[0] > 220*time.Millisecond {
		t.Fatalf("first backoff = %v, want ~200ms", *waits)
	}
}

// TestRunSamplingRequestRetryBudgetExhausted asserts the original error comes
// back once stream_max_retries is spent, and the configured budget is honored.
func TestRunSamplingRequestRetryBudgetExhausted(t *testing.T) {
	waits := noSleep(t)
	two := uint64(2)
	boom := retryableAPIErr("still down")
	mc := NewMockModelClient("gpt-test", nil,
		MockTurn{StreamErr: boom}, MockTurn{StreamErr: boom}, MockTurn{StreamErr: boom},
	)
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	tc.StreamMaxRetries = &two
	_, err := runSamplingRequest(sess.ctx, sess, tc)
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected the exhausted retryable error, got %v", err)
	}
	if len(*waits) != 2 {
		t.Fatalf("retries = %d, want 2", len(*waits))
	}
	notices := 0
	for _, ev := range drainEvents(evCh) {
		if ev.Msg.Type == protocol.EventMsgKindStreamError {
			notices++
		}
	}
	if notices != 2 {
		t.Fatalf("stream_error notices = %d, want 2", notices)
	}
}

// TestRunSamplingRequestNonRetryableFailsFast asserts non-retryable failures
// are returned without any retry or notice.
func TestRunSamplingRequestNonRetryableFailsFast(t *testing.T) {
	waits := noSleep(t)
	fatal := &api.APIError{Kind: api.APIErrorContextWindowExceeded}
	mc := NewMockModelClient("gpt-test", nil, MockTurn{StreamErr: fatal}, MockTurn{StreamErr: fatal})
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	_, err := runSamplingRequest(sess.ctx, sess, tc)
	if err == nil || !errors.Is(err, fatal) {
		t.Fatalf("expected the fatal error, got %v", err)
	}
	if len(*waits) != 0 {
		t.Fatalf("non-retryable error must not back off, got %v", *waits)
	}
	if _, ok := firstEvent(drainEvents(evCh), protocol.EventMsgKindStreamError); ok {
		t.Fatalf("non-retryable error must not emit a retry notice")
	}
}

func TestSamplingBackoffGrowsAndCaps(t *testing.T) {
	first := samplingBackoff(1)
	third := samplingBackoff(3)
	if first < 180*time.Millisecond || first > 220*time.Millisecond {
		t.Fatalf("attempt 1 = %v, want ~200ms", first)
	}
	if third < 720*time.Millisecond || third > 880*time.Millisecond {
		t.Fatalf("attempt 3 = %v, want ~800ms", third)
	}
	huge := uint64(1000)
	if got := streamMaxRetries(&TurnContext{StreamMaxRetries: &huge}); got != MaxStreamMaxRetries {
		t.Fatalf("configured budget should cap at %d, got %d", MaxStreamMaxRetries, got)
	}
	if got := streamMaxRetries(nil); got != DefaultStreamMaxRetries {
		t.Fatalf("nil turn should default to %d, got %d", DefaultStreamMaxRetries, got)
	}
}
