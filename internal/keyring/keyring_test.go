package keyring

import (
	"errors"
	"sync"
	"testing"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	s := NewMemoryStore()

	if v, err := s.Load("svc", "acct"); err != nil || v != nil {
		t.Fatalf("expected (nil,nil) for missing, got (%v,%v)", v, err)
	}
	if removed, err := s.Delete("svc", "acct"); err != nil || removed {
		t.Fatalf("expected (false,nil) deleting missing, got (%v,%v)", removed, err)
	}

	if err := s.Save("svc", "acct", "secret"); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if !s.Contains("acct") {
		t.Fatalf("expected Contains to be true after save")
	}
	if v, ok := s.SavedValue("acct"); !ok || v != "secret" {
		t.Fatalf("expected SavedValue secret/true, got %q/%v", v, ok)
	}

	v, err := s.Load("svc", "acct")
	if err != nil || v == nil || *v != "secret" {
		t.Fatalf("expected load secret, got (%v,%v)", v, err)
	}

	// Overwrite.
	if err := s.Save("svc", "acct", "new"); err != nil {
		t.Fatalf("overwrite save failed: %v", err)
	}
	v, err = s.Load("svc", "acct")
	if err != nil || v == nil || *v != "new" {
		t.Fatalf("expected load new, got (%v,%v)", v, err)
	}

	removed, err := s.Delete("svc", "acct")
	if err != nil || !removed {
		t.Fatalf("expected (true,nil) deleting existing, got (%v,%v)", removed, err)
	}
	if s.Contains("acct") {
		t.Fatalf("expected Contains false after delete")
	}
}

func TestMemoryStoreInjectedError(t *testing.T) {
	s := NewMemoryStore()
	injected := errors.New("locked")
	s.SetError("acct", injected)

	if _, err := s.Load("svc", "acct"); !errors.Is(err, injected) {
		t.Fatalf("Load: expected injected error, got %v", err)
	}
	if err := s.Save("svc", "acct", "x"); !errors.Is(err, injected) {
		t.Fatalf("Save: expected injected error, got %v", err)
	}
	if _, err := s.Delete("svc", "acct"); !errors.Is(err, injected) {
		t.Fatalf("Delete: expected injected error, got %v", err)
	}

	// Errors must be wrapped as *StoreError.
	_, err := s.Load("svc", "acct")
	var se *StoreError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StoreError, got %T", err)
	}

	// Clearing the error restores normal operation.
	s.SetError("acct", nil)
	if err := s.Save("svc", "acct", "ok"); err != nil {
		t.Fatalf("expected save to succeed after clearing error, got %v", err)
	}
}

func TestMemoryStoreZeroValue(t *testing.T) {
	var s MemoryStore
	if err := s.Save("svc", "acct", "v"); err != nil {
		t.Fatalf("zero-value Save failed: %v", err)
	}
	v, err := s.Load("svc", "acct")
	if err != nil || v == nil || *v != "v" {
		t.Fatalf("zero-value Load: got (%v,%v)", v, err)
	}
}

func TestMemoryStoreConcurrent(t *testing.T) {
	s := NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Save("svc", "acct", "v")
			_, _ = s.Load("svc", "acct")
			_, _ = s.Delete("svc", "acct")
			_ = s.Contains("acct")
			_, _ = s.SavedValue("acct")
		}()
	}
	wg.Wait()
}

func TestStoreError(t *testing.T) {
	t.Run("nil pointer is safe", func(t *testing.T) {
		var se *StoreError
		if se.Error() == "" {
			t.Fatalf("expected non-empty message for nil receiver")
		}
		if se.Message() != "" {
			t.Fatalf("expected empty Message for nil receiver, got %q", se.Message())
		}
		if se.Unwrap() != nil {
			t.Fatalf("expected nil Unwrap for nil receiver")
		}
	})

	t.Run("wraps and reports message", func(t *testing.T) {
		inner := errors.New("inner failure")
		se := &StoreError{Err: inner}
		if se.Message() != "inner failure" {
			t.Fatalf("expected message %q, got %q", "inner failure", se.Message())
		}
		if !errors.Is(se, inner) {
			t.Fatalf("expected errors.Is to find inner")
		}
		if se.Unwrap() != inner {
			t.Fatalf("expected Unwrap to return inner")
		}
	})

	t.Run("newStoreError nil passthrough", func(t *testing.T) {
		if err := newStoreError(nil); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

// Compile-time checks that the implementations satisfy the Store interface.
var (
	_ Store = (*MemoryStore)(nil)
)
