//go:build linux || android

package prochard

import "testing"

func TestDisableProcessDumping(t *testing.T) {
	// PR_SET_DUMPABLE is permitted for the calling process under normal
	// circumstances, so this should succeed in the test environment.
	if err := DisableProcessDumping(); err != nil {
		t.Fatalf("DisableProcessDumping() failed: %v", err)
	}
}
