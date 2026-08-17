package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/client"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the sampling-request retry loop of codex-core
// (`run_sampling_request` + `responses_retry.rs`, upstream 0.147; spec 50
// D0.5): a dropped or failed model stream is retried with exponential backoff
// up to the provider's stream_max_retries, each retry surfacing a
// `stream_error` "Reconnecting... n/max" event so clients do not stare at a
// frozen turn. Errors the upstream classifies as non-retryable (invalid
// request, context window exceeded, quota/usage limits, 429 retry limit,
// server overloaded, cancellation) return immediately.

const (
	// DefaultStreamMaxRetries mirrors model-provider-info DEFAULT_STREAM_MAX_RETRIES.
	DefaultStreamMaxRetries uint64 = 5
	// MaxStreamMaxRetries mirrors MAX_STREAM_MAX_RETRIES (the configured cap).
	MaxStreamMaxRetries uint64 = 100
	// samplingBackoffInitial / samplingBackoffFactor mirror core util::backoff
	// (INITIAL_DELAY_MS = 200, BACKOFF_FACTOR = 2.0, jitter 0.9..1.1).
	samplingBackoffInitial = 200 * time.Millisecond
	samplingBackoffFactor  = 2.0
)

// ErrStreamDisconnected reports a model stream that closed before
// `response.completed`; it is retryable (Rust `CodexErr::Stream`).
var ErrStreamDisconnected = errors.New("core: stream disconnected before completion")

// samplingRetryable reports whether err should be retried by the sampling
// loop and the server-suggested delay when one was supplied. It mirrors
// `codex_api::map_api_error` + `CodexErr::is_retryable`.
func samplingRetryable(err error) (retryable bool, delay *time.Duration) {
	if err == nil {
		return false, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTurnAborted) {
		return false, nil
	}
	if errors.Is(err, ErrStreamDisconnected) {
		return true, nil
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return false, nil
	}
	switch apiErr.Kind {
	case api.APIErrorRetryable:
		return true, apiErr.Delay
	case api.APIErrorStream, api.APIErrorAPI:
		// Stream → CodexErr::Stream; Api{status} → UnexpectedStatus. Both retryable.
		return true, nil
	case api.APIErrorTransport:
		return transportRetryable(apiErr.Transport), nil
	default:
		// ContextWindowExceeded, QuotaExceeded, UsageNotIncluded, RateLimit
		// (429 → RetryLimit / UsageLimitReached), InvalidRequest, CyberPolicy,
		// ServerOverloaded.
		return false, nil
	}
}

// transportRetryable classifies a transport failure like map_api_error's
// Transport arm: timeouts and network faults retry (Timeout / ConnectionFailed);
// HTTP 500 retries (InternalServerError); 400 (InvalidRequest), 429
// (RetryLimit / usage limits) and exhausted client-side retries do not; other
// statuses map to UnexpectedStatus, which is retryable.
func transportRetryable(te *client.TransportError) bool {
	if te == nil {
		return false
	}
	switch te.Kind {
	case client.TransportErrorTimeout, client.TransportErrorNetwork:
		return true
	case client.TransportErrorHTTP:
		switch te.Status {
		case http.StatusBadRequest, http.StatusTooManyRequests:
			return false
		default:
			return true
		}
	default: // RetryLimit, Build
		return false
	}
}

// samplingBackoff mirrors core util::backoff: 200ms * 2^(attempt-1) with
// uniform jitter in [0.9, 1.1).
func samplingBackoff(attempt uint64) time.Duration {
	exp := 1.0
	for i := uint64(1); i < attempt; i++ {
		exp *= samplingBackoffFactor
	}
	base := float64(samplingBackoffInitial/time.Millisecond) * exp
	jitter := 0.9 + rand.Float64()*0.2 //nolint:gosec // jitter only
	return time.Duration(base*jitter) * time.Millisecond
}

// streamMaxRetries resolves the turn's retry budget: the configured value
// (capped) or the default.
func streamMaxRetries(tc *TurnContext) uint64 {
	if tc == nil || tc.StreamMaxRetries == nil {
		return DefaultStreamMaxRetries
	}
	if *tc.StreamMaxRetries > MaxStreamMaxRetries {
		return MaxStreamMaxRetries
	}
	return *tc.StreamMaxRetries
}

// handleRetryableSamplingError is the Go analogue of
// `handle_retryable_response_stream_error` for the Sampling request kind
// (there is no websocket fallback transport in codexgo). It returns nil when
// the caller should retry (after sleeping the delay) and the original error
// once the budget is exhausted.
func handleRetryableSamplingError(ctx context.Context, sess *Session, tc *TurnContext, retries *uint64, maxRetries uint64, err error, delay *time.Duration) error {
	if *retries >= maxRetries {
		return err
	}
	*retries++
	wait := samplingBackoff(*retries)
	if delay != nil {
		wait = *delay
	}
	slog.Warn("stream disconnected - retrying sampling request",
		"turn_id", tc.SubID, "retries", *retries, "max_retries", maxRetries,
		"delay", wait, "sampling_error", err)
	// Surface retry information to any UI/front-end so the user understands
	// what is happening instead of staring at a seemingly frozen screen
	// (Rust notify_stream_error; codexgo has no websocket transport, so the
	// first retry is reported too).
	msg := err.Error()
	sess.SendEvent(tc.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindStreamError,
		StreamError: &protocol.StreamErrorEvent{
			Message:           fmt.Sprintf("Reconnecting... %d/%d", *retries, maxRetries),
			AdditionalDetails: &msg,
		},
	})
	return samplingSleep(ctx, wait)
}

// samplingSleep waits for d or until ctx is done; tests override it to avoid
// real backoff delays.
var samplingSleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ErrTurnAborted
	case <-timer.C:
		return nil
	}
}
