package keyring

import (
	"errors"
	"sync"
	"testing"

	gokeyring "github.com/zalando/go-keyring"
)

// fakeBackend is an in-memory backend used to drive DefaultStore in tests
// without touching the real system keyring or the zalando global provider.
type fakeBackend struct {
	mu     sync.Mutex
	store  map[string]map[string]string
	getErr error
	setErr error
	delErr error
	getCnt int
	setCnt int
	delCnt int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{store: make(map[string]map[string]string)}
}

func (f *fakeBackend) Get(service, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCnt++
	if f.getErr != nil {
		return "", f.getErr
	}
	if m, ok := f.store[service]; ok {
		if v, ok := m[user]; ok {
			return v, nil
		}
	}
	return "", gokeyring.ErrNotFound
}

func (f *fakeBackend) Set(service, user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCnt++
	if f.setErr != nil {
		return f.setErr
	}
	if f.store[service] == nil {
		f.store[service] = make(map[string]string)
	}
	f.store[service][user] = password
	return nil
}

func (f *fakeBackend) Delete(service, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delCnt++
	if f.delErr != nil {
		return f.delErr
	}
	if m, ok := f.store[service]; ok {
		if _, ok := m[user]; ok {
			delete(m, user)
			return nil
		}
	}
	return gokeyring.ErrNotFound
}

func ptr(s string) *string { return &s }

func TestDefaultStoreLoad(t *testing.T) {
	boom := errors.New("boom")

	tests := []struct {
		name         string
		seed         map[string]string // service "svc" entries
		getErr       error
		want         *string
		wantErr      bool
		wantNotAvail bool
	}{
		{
			name: "found",
			seed: map[string]string{"acct": "secret"},
			want: ptr("secret"),
		},
		{
			name: "not found returns nil nil",
			want: nil,
		},
		{
			name:    "backend error wrapped",
			getErr:  boom,
			wantErr: true,
		},
		{
			name:         "unsupported platform maps to not available",
			getErr:       gokeyring.ErrUnsupportedPlatform,
			wantErr:      true,
			wantNotAvail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := newFakeBackend()
			for k, v := range tc.seed {
				if fb.store["svc"] == nil {
					fb.store["svc"] = map[string]string{}
				}
				fb.store["svc"][k] = v
			}
			fb.getErr = tc.getErr
			s := &DefaultStore{backend: fb}

			got, err := s.Load("svc", "acct")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var se *StoreError
				if !errors.As(err, &se) {
					t.Fatalf("error %v is not a *StoreError", err)
				}
				if tc.wantNotAvail && !errors.Is(err, ErrNotAvailable) {
					t.Fatalf("expected ErrNotAvailable, got %v", err)
				}
				if !tc.wantNotAvail && !errors.Is(err, tc.getErr) {
					t.Fatalf("expected wrapped %v, got %v", tc.getErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("expected nil, got %q", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("expected %q, got nil", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("expected %q, got %q", *tc.want, *got)
			}
		})
	}
}

func TestDefaultStoreSave(t *testing.T) {
	boom := errors.New("write failed")

	tests := []struct {
		name     string
		setErr   error
		wantErr  bool
		notAvail bool
	}{
		{name: "success"},
		{name: "error wrapped", setErr: boom, wantErr: true},
		{name: "unsupported", setErr: gokeyring.ErrUnsupportedPlatform, wantErr: true, notAvail: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := newFakeBackend()
			fb.setErr = tc.setErr
			s := &DefaultStore{backend: fb}

			err := s.Save("svc", "acct", "value")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var se *StoreError
				if !errors.As(err, &se) {
					t.Fatalf("error %v is not a *StoreError", err)
				}
				if tc.notAvail && !errors.Is(err, ErrNotAvailable) {
					t.Fatalf("expected ErrNotAvailable, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := fb.store["svc"]["acct"]; got != "value" {
				t.Fatalf("expected stored value %q, got %q", "value", got)
			}
		})
	}
}

func TestDefaultStoreDelete(t *testing.T) {
	boom := errors.New("delete failed")

	tests := []struct {
		name     string
		seed     bool
		delErr   error
		want     bool
		wantErr  bool
		notAvail bool
	}{
		{name: "existing returns true", seed: true, want: true},
		{name: "missing returns false no error", seed: false, want: false},
		{name: "error wrapped", seed: true, delErr: boom, wantErr: true},
		{name: "unsupported", seed: true, delErr: gokeyring.ErrUnsupportedPlatform, wantErr: true, notAvail: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := newFakeBackend()
			if tc.seed {
				fb.store["svc"] = map[string]string{"acct": "secret"}
			}
			fb.delErr = tc.delErr
			s := &DefaultStore{backend: fb}

			got, err := s.Delete("svc", "acct")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.notAvail && !errors.Is(err, ErrNotAvailable) {
					t.Fatalf("expected ErrNotAvailable, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected removed=%v, got %v", tc.want, got)
			}
		})
	}
}

// TestDefaultStoreZeroValue verifies the zero value uses the system backend
// without panicking on method dispatch (we only check it does not panic and
// returns a non-nil store via store()).
func TestDefaultStoreZeroValue(t *testing.T) {
	var s DefaultStore
	if s.store() == nil {
		t.Fatalf("zero-value store() returned nil backend")
	}
}

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
	_ Store = (*DefaultStore)(nil)
	_ Store = (*MemoryStore)(nil)
)
