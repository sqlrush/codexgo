package system

import (
	"errors"
	"sync"
	"testing"

	gokeyring "github.com/zalando/go-keyring"

	"github.com/sqlrush/codexgo/internal/keyring"
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
				var se *keyring.StoreError
				if !errors.As(err, &se) {
					t.Fatalf("error %v is not a *StoreError", err)
				}
				if tc.wantNotAvail && !errors.Is(err, keyring.ErrNotAvailable) {
					t.Fatalf("expected keyring.ErrNotAvailable, got %v", err)
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
				var se *keyring.StoreError
				if !errors.As(err, &se) {
					t.Fatalf("error %v is not a *StoreError", err)
				}
				if tc.notAvail && !errors.Is(err, keyring.ErrNotAvailable) {
					t.Fatalf("expected keyring.ErrNotAvailable, got %v", err)
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
				if tc.notAvail && !errors.Is(err, keyring.ErrNotAvailable) {
					t.Fatalf("expected keyring.ErrNotAvailable, got %v", err)
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
