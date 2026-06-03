package client

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRetryOnShouldRetry(t *testing.T) {
	tests := []struct {
		name        string
		on          RetryOn
		err         *TransportError
		attempt     uint64
		maxAttempts uint64
		want        bool
	}{
		{
			name:        "429 retried when enabled",
			on:          RetryOn{Retry429: true},
			err:         &TransportError{Kind: TransportErrorHTTP, Status: http.StatusTooManyRequests},
			attempt:     0,
			maxAttempts: 3,
			want:        true,
		},
		{
			name:        "429 not retried when disabled",
			on:          RetryOn{Retry429: false, Retry5xx: true},
			err:         &TransportError{Kind: TransportErrorHTTP, Status: http.StatusTooManyRequests},
			attempt:     0,
			maxAttempts: 3,
			want:        false,
		},
		{
			name:        "5xx retried",
			on:          RetryOn{Retry5xx: true},
			err:         &TransportError{Kind: TransportErrorHTTP, Status: 503},
			attempt:     0,
			maxAttempts: 3,
			want:        true,
		},
		{
			name:        "4xx not retried",
			on:          RetryOn{Retry5xx: true, Retry429: true},
			err:         &TransportError{Kind: TransportErrorHTTP, Status: 400},
			attempt:     0,
			maxAttempts: 3,
			want:        false,
		},
		{
			name:        "timeout retried when transport enabled",
			on:          RetryOn{RetryTransport: true},
			err:         &TransportError{Kind: TransportErrorTimeout},
			attempt:     0,
			maxAttempts: 3,
			want:        true,
		},
		{
			name:        "network retried when transport enabled",
			on:          RetryOn{RetryTransport: true},
			err:         &TransportError{Kind: TransportErrorNetwork},
			attempt:     0,
			maxAttempts: 3,
			want:        true,
		},
		{
			name:        "attempt at max not retried",
			on:          RetryOn{Retry5xx: true},
			err:         &TransportError{Kind: TransportErrorHTTP, Status: 500},
			attempt:     3,
			maxAttempts: 3,
			want:        false,
		},
		{
			name:        "build error not retried",
			on:          RetryOn{RetryTransport: true},
			err:         &TransportError{Kind: TransportErrorBuild},
			attempt:     0,
			maxAttempts: 3,
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.on.ShouldRetry(tt.err, tt.attempt, tt.maxAttempts); got != tt.want {
				t.Fatalf("ShouldRetry = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBackoffBaseAtAttemptZero(t *testing.T) {
	got := Backoff(200*time.Millisecond, 0, nil)
	if got != 200*time.Millisecond {
		t.Fatalf("backoff(0) = %v, want 200ms", got)
	}
}

func TestBackoffGrowsExponentiallyWithinJitter(t *testing.T) {
	base := 100 * time.Millisecond
	// attempt 1 => base * 2^0 = 100ms, with jitter in [90,110]ms.
	for i := 0; i < 50; i++ {
		got := Backoff(base, 1, nil)
		if got < 90*time.Millisecond || got >= 110*time.Millisecond {
			t.Fatalf("backoff(1) = %v out of jitter range", got)
		}
	}
	// attempt 3 => base * 2^2 = 400ms, jitter [360,440]ms.
	for i := 0; i < 50; i++ {
		got := Backoff(base, 3, nil)
		if got < 360*time.Millisecond || got >= 440*time.Millisecond {
			t.Fatalf("backoff(3) = %v out of jitter range", got)
		}
	}
}

func TestCapRetries(t *testing.T) {
	if CapRetries(5) != 5 {
		t.Fatalf("CapRetries(5) should be 5")
	}
	if CapRetries(150) != MaxRetriesCap {
		t.Fatalf("CapRetries(150) should clamp to %d", MaxRetriesCap)
	}
}

func TestDefaultPolicies(t *testing.T) {
	req := DefaultRequestRetryPolicy()
	if req.MaxAttempts != 4 || req.BaseDelay != 200*time.Millisecond {
		t.Fatalf("unexpected request policy: %+v", req)
	}
	if req.RetryOn.Retry429 || !req.RetryOn.Retry5xx || !req.RetryOn.RetryTransport {
		t.Fatalf("unexpected request retry-on: %+v", req.RetryOn)
	}
	st := DefaultStreamRetryPolicy()
	if st.MaxAttempts != 5 {
		t.Fatalf("unexpected stream policy max attempts: %d", st.MaxAttempts)
	}
}

func TestRunWithRetrySucceedsAfterRetries(t *testing.T) {
	policy := RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		RetryOn:     RetryOn{Retry5xx: true},
	}
	calls := 0
	res, err := RunWithRetry(context.Background(), policy,
		func() Request { return NewRequest(http.MethodGet, "https://x") },
		func(ctx context.Context, req Request, attempt uint64) (int, error) {
			calls++
			if attempt < 2 {
				return 0, &TransportError{Kind: TransportErrorHTTP, Status: 500}
			}
			return 42, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != 42 {
		t.Fatalf("unexpected result: %d", res)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRunWithRetryReturnsErrorWhenNotRetryable(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, RetryOn: RetryOn{Retry5xx: true}}
	calls := 0
	_, err := RunWithRetry(context.Background(), policy,
		func() Request { return NewRequest(http.MethodGet, "https://x") },
		func(ctx context.Context, req Request, attempt uint64) (int, error) {
			calls++
			return 0, &TransportError{Kind: TransportErrorHTTP, Status: 400}
		},
	)
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRunWithRetryExhaustsToRetryLimit(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, RetryOn: RetryOn{Retry5xx: true}}
	_, err := RunWithRetry(context.Background(), policy,
		func() Request { return NewRequest(http.MethodGet, "https://x") },
		func(ctx context.Context, req Request, attempt uint64) (int, error) {
			return 0, &TransportError{Kind: TransportErrorHTTP, Status: 500}
		},
	)
	var te *TransportError
	if err == nil {
		t.Fatalf("expected error")
	}
	if !asTransport(err, &te) || te.Kind != TransportErrorHTTP {
		// On the last attempt ShouldRetry returns false (attempt==max), so the
		// final non-retryable error propagates rather than RetryLimit.
		t.Fatalf("expected http error, got %v", err)
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "120")
	d, ok := ParseRetryAfter(h, time.Now())
	if !ok || d != 120*time.Second {
		t.Fatalf("ParseRetryAfter seconds = %v ok=%v", d, ok)
	}
}

func TestParseRetryAfterDate(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	future := now.Add(30 * time.Second)
	h := http.Header{}
	h.Set("Retry-After", future.Format(http.TimeFormat))
	d, ok := ParseRetryAfter(h, now)
	if !ok || d != 30*time.Second {
		t.Fatalf("ParseRetryAfter date = %v ok=%v", d, ok)
	}
}

func TestParseRetryAfterMissing(t *testing.T) {
	if _, ok := ParseRetryAfter(http.Header{}, time.Now()); ok {
		t.Fatalf("expected missing retry-after")
	}
}

func asTransport(err error, target **TransportError) bool {
	te, ok := err.(*TransportError)
	if ok {
		*target = te
	}
	return ok
}
