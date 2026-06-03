package unifiedexec

import "testing"

func TestProcessState(t *testing.T) {
	t.Run("exited sets fields and preserves failure", func(t *testing.T) {
		msg := "boom"
		base := processState{failureMessage: &msg}
		code := 7
		next := base.exited(&code)
		if !next.hasExited {
			t.Fatalf("hasExited = false, want true")
		}
		if next.exitCode == nil || *next.exitCode != 7 {
			t.Fatalf("exitCode = %v, want 7", next.exitCode)
		}
		if next.failureMessage == nil || *next.failureMessage != "boom" {
			t.Fatalf("failureMessage = %v, want boom", next.failureMessage)
		}
		// base is unchanged (immutability).
		if base.hasExited {
			t.Fatalf("base mutated: hasExited = true")
		}
	})

	t.Run("failed sets message and preserves exit code", func(t *testing.T) {
		code := 3
		base := processState{exitCode: &code}
		next := base.failed("denied")
		if !next.hasExited {
			t.Fatalf("hasExited = false, want true")
		}
		if next.exitCode == nil || *next.exitCode != 3 {
			t.Fatalf("exitCode = %v, want 3", next.exitCode)
		}
		if next.failureMessage == nil || *next.failureMessage != "denied" {
			t.Fatalf("failureMessage = %v, want denied", next.failureMessage)
		}
		if base.failureMessage != nil {
			t.Fatalf("base mutated: failureMessage = %v", base.failureMessage)
		}
	})
}
