package readiness

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSubscribeAndMarkReadyRoundtrip(t *testing.T) {
	f := New()
	token, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}

	ok, err := f.MarkReady(context.Background(), token)
	if err != nil {
		t.Fatalf("MarkReady returned error: %v", err)
	}
	if !ok {
		t.Fatalf("MarkReady = false, want true")
	}
	if !f.IsReady() {
		t.Fatalf("IsReady = false after MarkReady, want true")
	}
}

func TestSubscribeAfterReadyReturnsError(t *testing.T) {
	f := New()
	token, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if ok, err := f.MarkReady(context.Background(), token); err != nil || !ok {
		t.Fatalf("MarkReady = (%v, %v), want (true, nil)", ok, err)
	}

	_, err = f.Subscribe(context.Background())
	if !errors.Is(err, ErrFlagAlreadyReady) {
		t.Fatalf("Subscribe after ready = %v, want ErrFlagAlreadyReady", err)
	}
}

func TestMarkReadyRejectsUnknownToken(t *testing.T) {
	f := New()

	ok, err := f.MarkReady(context.Background(), Token{id: 42})
	if err != nil {
		t.Fatalf("MarkReady returned error: %v", err)
	}
	if ok {
		t.Fatalf("MarkReady with unknown token = true, want false")
	}
	if f.loadReady() {
		t.Fatalf("loadReady = true after rejected MarkReady, want false")
	}
	// IsReady promotes the flag because there are no subscribers, mirroring
	// the Rust test mark_ready_rejects_unknown_token.
	if !f.IsReady() {
		t.Fatalf("IsReady = false with no subscribers, want true")
	}
}

func TestWaitReadyUnblocksAfterMarkReady(t *testing.T) {
	f := New()
	token, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	waitErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		waitErr <- f.WaitReady(context.Background())
	}()

	if ok, err := f.MarkReady(context.Background(), token); err != nil || !ok {
		t.Fatalf("MarkReady = (%v, %v), want (true, nil)", ok, err)
	}

	wg.Wait()
	if err := <-waitErr; err != nil {
		t.Fatalf("WaitReady returned error: %v", err)
	}
}

func TestMarkReadyTwiceUsesSingleToken(t *testing.T) {
	f := New()
	token, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}

	if ok, err := f.MarkReady(context.Background(), token); err != nil || !ok {
		t.Fatalf("first MarkReady = (%v, %v), want (true, nil)", ok, err)
	}
	ok, err := f.MarkReady(context.Background(), token)
	if err != nil {
		t.Fatalf("second MarkReady returned error: %v", err)
	}
	if ok {
		t.Fatalf("second MarkReady = true, want false")
	}
}

func TestIsReadyWithoutSubscribersMarksFlagReady(t *testing.T) {
	f := New()

	if !f.IsReady() {
		t.Fatalf("first IsReady = false, want true")
	}
	if !f.IsReady() {
		t.Fatalf("second IsReady = false, want true")
	}
	_, err := f.Subscribe(context.Background())
	if !errors.Is(err, ErrFlagAlreadyReady) {
		t.Fatalf("Subscribe after IsReady = %v, want ErrFlagAlreadyReady", err)
	}
}

func TestSubscribeReturnsErrorWhenLockHeld(t *testing.T) {
	f := New()

	// Hold the internal lock so Subscribe cannot acquire it within the
	// timeout. We shorten the wait by cancelling the context, which the timed
	// mutex treats the same as a timeout (ErrTokenLockFailed).
	if !f.lock.tryLock() {
		t.Fatalf("could not acquire internal lock for test setup")
	}
	defer f.lock.unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := f.Subscribe(ctx)
	if !errors.Is(err, ErrTokenLockFailed) {
		t.Fatalf("contended Subscribe = %v, want ErrTokenLockFailed", err)
	}
}

func TestSubscribeSkipsZeroToken(t *testing.T) {
	f := New()
	f.nextID.Store(0)

	token, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if token.id == 0 {
		t.Fatalf("Subscribe returned reserved zero token")
	}
	if ok, err := f.MarkReady(context.Background(), token); err != nil || !ok {
		t.Fatalf("MarkReady = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestSubscribeAvoidsDuplicateTokens(t *testing.T) {
	f := New()
	token, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	// Rewind the counter so the next id collides with the existing token.
	f.nextID.Store(token.id)

	token2, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("second Subscribe returned error: %v", err)
	}
	if token2 == token {
		t.Fatalf("Subscribe returned duplicate token %v", token2)
	}
}

func TestWaitReadyRespectsContextCancellation(t *testing.T) {
	f := New()
	// Keep a subscriber outstanding so IsReady will not auto-promote.
	if _, err := f.Subscribe(context.Background()); err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := f.WaitReady(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitReady = %v, want context.DeadlineExceeded", err)
	}
}

func TestWaitReadyReturnsImmediatelyWhenReady(t *testing.T) {
	f := New()
	token, err := f.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if ok, err := f.MarkReady(context.Background(), token); err != nil || !ok {
		t.Fatalf("MarkReady = (%v, %v), want (true, nil)", ok, err)
	}

	done := make(chan struct{})
	go func() {
		_ = f.WaitReady(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("WaitReady did not return promptly for a ready flag")
	}
}

func TestStringFormat(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*Flag)
		want    string
	}{
		{
			name:    "not ready",
			prepare: func(*Flag) {},
			want:    "ReadinessFlag { ready: false }",
		},
		{
			name: "ready",
			prepare: func(f *Flag) {
				tok, err := f.Subscribe(context.Background())
				if err != nil {
					t.Fatalf("Subscribe: %v", err)
				}
				if _, err := f.MarkReady(context.Background(), tok); err != nil {
					t.Fatalf("MarkReady: %v", err)
				}
			},
			want: "ReadinessFlag { ready: true }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			tt.prepare(f)
			if got := f.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConcurrentMarkReadyOnlyOneSucceeds(t *testing.T) {
	f := New()
	const n = 16
	tokens := make([]Token, 0, n)
	for range n {
		tok, err := f.Subscribe(context.Background())
		if err != nil {
			t.Fatalf("Subscribe returned error: %v", err)
		}
		tokens = append(tokens, tok)
	}

	var wg sync.WaitGroup
	results := make([]bool, n)
	for i, tok := range tokens {
		wg.Add(1)
		go func(i int, tok Token) {
			defer wg.Done()
			ok, err := f.MarkReady(context.Background(), tok)
			if err != nil {
				t.Errorf("MarkReady returned error: %v", err)
			}
			results[i] = ok
		}(i, tok)
	}
	wg.Wait()

	successes := 0
	for _, ok := range results {
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful MarkReady calls, want exactly 1", successes)
	}
	if !f.IsReady() {
		t.Fatalf("IsReady = false after MarkReady, want true")
	}
}

func TestTimedMutexTryLock(t *testing.T) {
	m := newTimedMutex()
	if !m.tryLock() {
		t.Fatalf("tryLock on free mutex = false, want true")
	}
	if m.tryLock() {
		t.Fatalf("tryLock on held mutex = true, want false")
	}
	m.unlock()
	if !m.tryLock() {
		t.Fatalf("tryLock after unlock = false, want true")
	}
}

func TestTimedMutexLockTimeout(t *testing.T) {
	m := newTimedMutex()
	if !m.tryLock() {
		t.Fatalf("setup tryLock = false, want true")
	}
	defer m.unlock()

	start := time.Now()
	err := m.lock(context.Background(), 30*time.Millisecond)
	if !errors.Is(err, ErrTokenLockFailed) {
		t.Fatalf("lock on held mutex = %v, want ErrTokenLockFailed", err)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Fatalf("lock returned after %v, expected to wait near the timeout", elapsed)
	}
}
