package tui

import "testing"

func TestComputeLayoutBasic(t *testing.T) {
	l := ComputeLayout(80, 24, 3)
	if l.Transcript != (Rect{X: 0, Y: 0, Width: 80, Height: 21}) {
		t.Fatalf("transcript = %+v", l.Transcript)
	}
	if l.Bottom != (Rect{X: 0, Y: 21, Width: 80, Height: 3}) {
		t.Fatalf("bottom = %+v", l.Bottom)
	}
	// Regions tile the height exactly.
	if l.Transcript.Height+l.Bottom.Height != 24 {
		t.Fatalf("heights do not sum to terminal height")
	}
}

func TestComputeLayoutClampsBottom(t *testing.T) {
	// A bottom pane taller than the screen is clamped so >=1 transcript row stays.
	l := ComputeLayout(80, 10, 100)
	if l.Transcript.Height < 1 {
		t.Fatalf("expected at least one transcript row, got %d", l.Transcript.Height)
	}
	if l.Bottom.Height != 9 {
		t.Fatalf("bottom clamped height = %d, want 9", l.Bottom.Height)
	}
}

func TestComputeLayoutNegativeInputs(t *testing.T) {
	l := ComputeLayout(-5, -5, -5)
	if !l.Transcript.IsEmpty() || !l.Bottom.IsEmpty() {
		t.Fatalf("negative inputs should yield empty regions: %+v", l)
	}
}

func TestComputeLayoutSingleRow(t *testing.T) {
	// With height 1 the bottom pane may take the whole screen (no reserve).
	l := ComputeLayout(80, 1, 1)
	if l.Bottom.Height != 1 {
		t.Fatalf("single-row bottom height = %d, want 1", l.Bottom.Height)
	}
	if l.Transcript.Height != 0 {
		t.Fatalf("single-row transcript height = %d, want 0", l.Transcript.Height)
	}
}

func TestRectHelpers(t *testing.T) {
	r := Rect{X: 2, Y: 3, Width: 10, Height: 4}
	if r.Right() != 12 {
		t.Fatalf("Right = %d, want 12", r.Right())
	}
	if r.Bottom() != 7 {
		t.Fatalf("Bottom = %d, want 7", r.Bottom())
	}
	if r.IsEmpty() {
		t.Fatal("rect with area should not be empty")
	}
	if !(Rect{Width: 0, Height: 5}).IsEmpty() {
		t.Fatal("zero-width rect should be empty")
	}
}
