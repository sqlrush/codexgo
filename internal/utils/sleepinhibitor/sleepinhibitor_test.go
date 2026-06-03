package sleepinhibitor

import (
	"bytes"
	"testing"
)

// recordingBackend is a test double that counts acquire/release calls so we can
// assert on the externally observable backend interactions without depending on
// any real OS sleep-prevention mechanism.
type recordingBackend struct {
	acquires int
	releases int
	active   bool
}

func (r *recordingBackend) acquire() {
	r.acquires++
	r.active = true
}

func (r *recordingBackend) release() {
	r.releases++
	r.active = false
}

// newWithBackend builds a SleepInhibitor wired to a test backend, bypassing the
// platform selection so the tests are deterministic on every OS.
func newWithBackend(enabled bool, b backend) *SleepInhibitor {
	return &SleepInhibitor{enabled: enabled, platform: b}
}

func TestSetTurnRunning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		enabled      bool
		sequence     []bool
		wantState    bool
		wantAcquires int
		wantReleases int
		wantActive   bool
	}{
		{
			name:         "enabled toggles on then off",
			enabled:      true,
			sequence:     []bool{true, false},
			wantState:    false,
			wantAcquires: 1,
			wantReleases: 1,
			wantActive:   false,
		},
		{
			name:         "enabled single on",
			enabled:      true,
			sequence:     []bool{true},
			wantState:    true,
			wantAcquires: 1,
			wantReleases: 0,
			wantActive:   true,
		},
		{
			name:         "disabled never acquires",
			enabled:      false,
			sequence:     []bool{true, false},
			wantState:    false,
			wantAcquires: 0,
			wantReleases: 2,
			wantActive:   false,
		},
		{
			name:         "disabled records turn state but stays released",
			enabled:      false,
			sequence:     []bool{true},
			wantState:    true,
			wantAcquires: 0,
			wantReleases: 1,
			wantActive:   false,
		},
		{
			name:         "enabled repeated true acquires each call",
			enabled:      true,
			sequence:     []bool{true, true, true, false},
			wantState:    false,
			wantAcquires: 3,
			wantReleases: 1,
			wantActive:   false,
		},
		{
			name:         "enabled toggles multiple times",
			enabled:      true,
			sequence:     []bool{true, false, true, false},
			wantState:    false,
			wantAcquires: 2,
			wantReleases: 2,
			wantActive:   false,
		},
		{
			name:         "enabled ends active after odd toggles",
			enabled:      true,
			sequence:     []bool{true, false, true},
			wantState:    true,
			wantAcquires: 2,
			wantReleases: 1,
			wantActive:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rb := &recordingBackend{}
			inhibitor := newWithBackend(tc.enabled, rb)

			for _, v := range tc.sequence {
				inhibitor.SetTurnRunning(v)
			}

			if got := inhibitor.IsTurnRunning(); got != tc.wantState {
				t.Errorf("IsTurnRunning() = %v, want %v", got, tc.wantState)
			}
			if rb.acquires != tc.wantAcquires {
				t.Errorf("acquires = %d, want %d", rb.acquires, tc.wantAcquires)
			}
			if rb.releases != tc.wantReleases {
				t.Errorf("releases = %d, want %d", rb.releases, tc.wantReleases)
			}
			if rb.active != tc.wantActive {
				t.Errorf("backend active = %v, want %v", rb.active, tc.wantActive)
			}
		})
	}
}

func TestNewInitialState(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{true, false} {
		inhibitor := New(enabled)
		if inhibitor == nil {
			t.Fatalf("New(%v) returned nil", enabled)
		}
		if inhibitor.IsTurnRunning() {
			t.Errorf("New(%v): IsTurnRunning() = true, want false", enabled)
		}
		if inhibitor.platform == nil {
			t.Errorf("New(%v): platform backend is nil", enabled)
		}
	}
}

func TestRealBackendDoesNotPanic(t *testing.T) {
	t.Parallel()

	// Exercise the actual platform backend selected for this OS to ensure the
	// public API never panics, matching the Rust crate's smoke tests. The
	// effect (whether the machine truly stays awake) is environment-dependent
	// and intentionally not asserted.
	inhibitor := New(true)
	inhibitor.SetTurnRunning(true)
	if !inhibitor.IsTurnRunning() {
		t.Fatal("expected turn running after SetTurnRunning(true)")
	}
	inhibitor.SetTurnRunning(false)
	if inhibitor.IsTurnRunning() {
		t.Fatal("expected turn not running after SetTurnRunning(false)")
	}
	if err := inhibitor.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
	// Close is safe to call repeatedly.
	if err := inhibitor.Close(); err != nil {
		t.Fatalf("second Close() returned error: %v", err)
	}
}

func TestCloseReleasesBackend(t *testing.T) {
	t.Parallel()

	rb := &recordingBackend{}
	inhibitor := newWithBackend(true, rb)
	inhibitor.SetTurnRunning(true)
	if !rb.active {
		t.Fatal("expected backend active after acquiring")
	}
	if err := inhibitor.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
	if rb.active {
		t.Error("expected backend released after Close()")
	}
}

func TestSetLogger(t *testing.T) {
	// Not parallel: mutates the package-level logger.
	original := logger
	t.Cleanup(func() { logger = original })

	var buf bytes.Buffer
	SetLogger(&buf)
	warn("hello %s", "world")
	if got := buf.String(); got == "" {
		t.Fatal("expected warning to be written to the custom logger")
	}

	// A nil writer must discard without panicking.
	SetLogger(nil)
	warn("should be discarded")
}
