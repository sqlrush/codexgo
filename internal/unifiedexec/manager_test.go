package unifiedexec

import (
	"testing"
	"time"
)

func TestProcessIDToPruneFromMeta(t *testing.T) {
	now := time.Now()
	at := func(secsAgo int) time.Time { return now.Add(-time.Duration(secsAgo) * time.Second) }

	tests := []struct {
		name    string
		meta    []pruneMeta
		want    int
		wantOK  bool
		comment string
	}{
		{
			name: "prefers exited outside recently used",
			meta: []pruneMeta{
				{1, at(40), false}, {2, at(30), true}, {3, at(20), false}, {4, at(19), false},
				{5, at(18), false}, {6, at(17), false}, {7, at(16), false}, {8, at(15), false},
				{9, at(14), false}, {10, at(13), false},
			},
			want: 2, wantOK: true,
		},
		{
			name: "falls back to LRU when no exited",
			meta: []pruneMeta{
				{1, at(40), false}, {2, at(30), false}, {3, at(20), false}, {4, at(19), false},
				{5, at(18), false}, {6, at(17), false}, {7, at(16), false}, {8, at(15), false},
				{9, at(14), false}, {10, at(13), false},
			},
			want: 1, wantOK: true,
		},
		{
			name: "protects recent processes even if exited",
			meta: []pruneMeta{
				{1, at(40), false}, {2, at(30), false}, {3, at(20), true}, {4, at(19), false},
				{5, at(18), false}, {6, at(17), false}, {7, at(16), false}, {8, at(15), false},
				{9, at(14), false}, {10, at(13), true},
			},
			want: 1, wantOK: true,
		},
		{
			name:   "empty meta returns false",
			meta:   nil,
			want:   0,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := processIDToPruneFromMeta(tt.meta)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("prune candidate = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAllocateProcessIDDeterministic(t *testing.T) {
	m := NewDefaultProcessManager()
	m.SetDeterministicProcessIDs(true)

	first := m.AllocateProcessID()
	if first != 1000 {
		t.Fatalf("first id = %d, want 1000", first)
	}
	second := m.AllocateProcessID()
	if second != 1001 {
		t.Fatalf("second id = %d, want 1001", second)
	}

	m.ReleaseProcessID(first)
	// After releasing 1000, the next deterministic id is based on the max
	// remaining reservation (1001) + 1.
	third := m.AllocateProcessID()
	if third != 1002 {
		t.Fatalf("third id = %d, want 1002", third)
	}
}

func TestResolveWriteYieldTime(t *testing.T) {
	m := NewProcessManager(20_000, SandboxSpawner{})

	tests := []struct {
		name  string
		input string
		yield uint64
		want  uint64
	}{
		{"empty poll floors to min empty", "", 100, MinEmptyYieldTimeMS},
		{"empty poll caps to manager max", "", 999_999, 20_000},
		{"non-empty floors to min yield", "x", 10, MinYieldTimeMS},
		{"non-empty caps to max yield", "x", 999_999, MaxYieldTimeMS},
		{"non-empty middle passes through", "x", 5_000, 5_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &WriteStdinRequest{Input: tt.input, YieldTimeMS: tt.yield}
			if got := m.resolveWriteYieldTime(req); got != tt.want {
				t.Fatalf("resolveWriteYieldTime = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRefreshProcessStateUnknown(t *testing.T) {
	m := NewDefaultProcessManager()
	if status := m.refreshProcessState(9999); status.kind != statusUnknown {
		t.Fatalf("status.kind = %d, want statusUnknown", status.kind)
	}
}

func TestUnknownProcessOperations(t *testing.T) {
	m := NewDefaultProcessManager()
	if err := m.Kill(123); err == nil {
		t.Fatalf("Kill unknown returned nil error")
	} else if e, ok := err.(*Error); !ok || e.Kind != ErrUnknownProcessID {
		t.Fatalf("Kill error = %v, want ErrUnknownProcessID", err)
	}
	if err := m.Resize(&ResizeRequest{ProcessID: 123, Rows: 10, Cols: 20}); err == nil {
		t.Fatalf("Resize unknown returned nil error")
	}
}
