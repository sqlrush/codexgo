package pty

import (
	"sort"
	"testing"
	"time"
)

func TestCombineOutput(t *testing.T) {
	stdout := make(chan []byte, 2)
	stderr := make(chan []byte, 2)
	stdout <- []byte("a")
	stdout <- []byte("b")
	stderr <- []byte("c")
	close(stdout)
	close(stderr)

	combined := CombineOutput(stdout, stderr)
	var got []string
	timeout := time.After(2 * time.Second)
	for combined != nil {
		select {
		case chunk, ok := <-combined:
			if !ok {
				combined = nil
				continue
			}
			got = append(got, string(chunk))
		case <-timeout:
			t.Fatal("timed out collecting combined output")
		}
	}
	sort.Strings(got)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDefaultTerminalSize(t *testing.T) {
	got := DefaultTerminalSize()
	if got.Rows != 24 || got.Cols != 80 {
		t.Fatalf("DefaultTerminalSize() = %+v, want {24 80}", got)
	}
}

func TestEnvSlice(t *testing.T) {
	got := envSlice(map[string]string{"B": "2", "A": "1"})
	want := []string{"A=1", "B=2"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestExitCodeFromErr(t *testing.T) {
	if got := exitCodeFromErr(nil); got != 0 {
		t.Fatalf("exitCodeFromErr(nil) = %d, want 0", got)
	}
}
