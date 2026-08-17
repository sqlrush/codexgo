package client

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// Codex default retry parameters, mirrored from the Rust workspace.
//
// DEFAULT_REQUEST_MAX_RETRIES / DEFAULT_STREAM_MAX_RETRIES and the shared cap
// come from codex-rs/model-provider-info, while the 200ms base delay comes from
// the ApiRetryConfig built in the same crate.
const (
	// DefaultRequestMaxRetries is the codex default for unary request retries.
	DefaultRequestMaxRetries uint64 = 4
	// DefaultStreamMaxRetries is the codex default for stream reconnection
	// attempts.
	DefaultStreamMaxRetries uint64 = 5
	// MaxRetriesCap is the hard cap applied to user-configured retry counts.
	MaxRetriesCap uint64 = 100
	// DefaultBaseDelay is the codex default initial backoff delay.
	DefaultBaseDelay = 200 * time.Millisecond
)

// CapRetries clamps a configured retry count to the codex hard cap of 100,
// matching the `.min(MAX_..._MAX_RETRIES)` calls in model-provider-info.
func CapRetries(n uint64) uint64 {
	if n > MaxRetriesCap {
		return MaxRetriesCap
	}
	return n
}

// RetryOn describes which error classes are retryable. It mirrors the Rust
// `RetryOn` struct.
type RetryOn struct {
	Retry429       bool
	Retry5xx       bool
	RetryTransport bool
}

// ShouldRetry reports whether the given error should be retried on the given
// attempt. It mirrors the Rust `RetryOn::should_retry`.
func (r RetryOn) ShouldRetry(err *TransportError, attempt, maxAttempts uint64) bool {
	if attempt >= maxAttempts {
		return false
	}
	if err == nil {
		return false
	}
	switch err.Kind {
	case TransportErrorHTTP:
		return (r.Retry429 && err.Status == http.StatusTooManyRequests) ||
			(r.Retry5xx && err.Status >= 500 && err.Status <= 599)
	case TransportErrorTimeout, TransportErrorNetwork:
		return r.RetryTransport
	default:
		return false
	}
}

// RetryPolicy configures the retry loop. It mirrors the Rust `RetryPolicy`.
type RetryPolicy struct {
	MaxAttempts uint64
	BaseDelay   time.Duration
	RetryOn     RetryOn
}

// DefaultRequestRetryPolicy returns the codex default policy for unary requests:
// up to 4 attempts, 200ms base delay, retry on 5xx and transport errors (but not
// on 429), matching the ApiRetryConfig assembled in model-provider-info.
func DefaultRequestRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: DefaultRequestMaxRetries,
		BaseDelay:   DefaultBaseDelay,
		RetryOn: RetryOn{
			Retry429:       false,
			Retry5xx:       true,
			RetryTransport: true,
		},
	}
}

// DefaultStreamRetryPolicy returns the codex default policy for streaming
// requests: up to 5 attempts with the same per-error retry rules as unary
// requests.
func DefaultStreamRetryPolicy() RetryPolicy {
	policy := DefaultRequestRetryPolicy()
	policy.MaxAttempts = DefaultStreamMaxRetries
	return policy
}

// Backoff computes the delay before the given (1-based) attempt using
// exponential backoff with +/-10% jitter. It mirrors the Rust `backoff(base,
// attempt)` in codex-client: attempt 0 returns the base delay, and attempt N
// returns base * 2^(N-1) scaled by a uniform jitter factor in [0.9, 1.1).
//
// The optional rng allows deterministic testing; pass nil to use the default
// source.
func Backoff(base time.Duration, attempt uint64, rng *rand.Rand) time.Duration {
	if attempt == 0 {
		return base
	}
	exp := saturatingPow2(attempt - 1)
	millis := uint64(base / time.Millisecond)
	raw := saturatingMul(millis, exp)
	jitter := jitterFactor(rng)
	return time.Duration(float64(raw)*jitter) * time.Millisecond
}

// jitterFactor returns a uniform value in [0.9, 1.1).
func jitterFactor(rng *rand.Rand) float64 {
	if rng == nil {
		return 0.9 + rand.Float64()*0.2
	}
	return 0.9 + rng.Float64()*0.2
}

// saturatingPow2 returns 2^n, saturating at the max uint64 value.
func saturatingPow2(n uint64) uint64 {
	if n >= 64 {
		return math.MaxUint64
	}
	return uint64(1) << n
}

// saturatingMul multiplies a*b, saturating at the max uint64 value.
func saturatingMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxUint64/b {
		return math.MaxUint64
	}
	return a * b
}

// RetryOp executes a single attempt. attempt is the 0-based attempt index.
type RetryOp[T any] func(ctx context.Context, req Request, attempt uint64) (T, error)

// MakeRequest produces a fresh Request for each attempt, mirroring the Rust
// `make_req` closure (which can re-read mutable state per attempt).
type MakeRequest func() Request

// RunWithRetry drives an operation under a retry policy. It mirrors the Rust
// `run_with_retry`: it loops attempts 0..=MaxAttempts, retries transport errors
// the policy classifies as retryable (sleeping for the backoff delay before the
// next attempt), and otherwise returns the error immediately. If all attempts
// are exhausted it returns a RetryLimit TransportError.
//
// Unlike the Rust version this honors context cancellation while sleeping and
// during attempts.
func RunWithRetry[T any](
	ctx context.Context,
	policy RetryPolicy,
	makeReq MakeRequest,
	op RetryOp[T],
) (T, error) {
	var zero T
	for attempt := uint64(0); attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		req := makeReq()
		result, err := op(ctx, req, attempt)
		if err == nil {
			return result, nil
		}
		var transportErr *TransportError
		if errors.As(err, &transportErr) && policy.RetryOn.ShouldRetry(transportErr, attempt, policy.MaxAttempts) {
			delay := Backoff(policy.BaseDelay, attempt+1, nil)
			if serr := sleepCtx(ctx, delay); serr != nil {
				return zero, serr
			}
			continue
		}
		return zero, err
	}
	return zero, NewRetryLimitError()
}

// sleepCtx sleeps for d or returns the context error if the context is cancelled
// first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ParseRetryAfter parses an HTTP `Retry-After` header value. It supports both
// the delta-seconds form (e.g. "120") and the HTTP-date form (e.g. an RFC1123
// timestamp), returning the delay from now. It returns (0, false) when the value
// is absent or unparseable.
//
// The codex client retry loop does not itself consult Retry-After; this helper
// exists so callers that want to honor a server-provided Retry-After (for
// example when classifying an HTTP 429) can compute the requested delay.
func ParseRetryAfter(headers http.Header, now time.Time) (time.Duration, bool) {
	if headers == nil {
		return 0, false
	}
	value := headers.Get("Retry-After")
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	if t, err := http.ParseTime(value); err == nil {
		delay := t.Sub(now)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}
