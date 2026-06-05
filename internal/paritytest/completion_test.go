package paritytest

// Completion parity: assert codexgo's `completion <shell>` output is byte-for-byte
// identical to the real codex binary for every clap_complete shell. Env-gated on
// CODEX_PARITY_BIN like the other differential tests. Run locally:
//
//	CODEX_PARITY_BIN=/path/to/codex go test ./internal/paritytest/ -run TestParityCompletion -v

import (
	"bytes"
	"os/exec"
	"testing"
)

// runStdout runs bin with args and returns stdout and stderr separately plus the
// exit code. Unlike runCmd, it keeps the streams apart so completion-script bytes
// (stdout) can be compared without diagnostic noise from stderr.
func runStdout(bin string, args ...string) (stdout, stderr string, code int) {
	cmd := exec.Command(bin, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	switch e := err.(type) {
	case nil:
		return out.String(), errBuf.String(), 0
	case *exec.ExitError:
		return out.String(), errBuf.String(), e.ExitCode()
	default:
		return out.String(), errBuf.String(), -1
	}
}

// TestParityCompletion asserts every shell's completion script is byte-identical
// between codex and codexgo. The default (no SHELL arg) must also match (bash).
func TestParityCompletion(t *testing.T) {
	ref := referenceBin(t)
	cgo := buildCodexgo(t)

	shells := []string{"bash", "elvish", "fish", "powershell", "zsh"}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			refOut, refErr, refCode := runStdout(ref, "completion", shell)
			cgoOut, cgoErr, cgoCode := runStdout(cgo, "completion", shell)
			if refCode != 0 || cgoCode != 0 {
				t.Fatalf("exit codes: codex=%d (%q) codexgo=%d (%q)", refCode, refErr, cgoCode, cgoErr)
			}
			if refOut != cgoOut {
				t.Errorf("%s completion mismatch: codex=%d bytes codexgo=%d bytes; first diff at %d",
					shell, len(refOut), len(cgoOut), firstDiff(refOut, cgoOut))
			}
		})
	}

	t.Run("default-is-bash", func(t *testing.T) {
		refOut, _, _ := runStdout(ref, "completion")
		cgoOut, _, _ := runStdout(cgo, "completion")
		if refOut != cgoOut {
			t.Errorf("default completion mismatch: first diff at %d", firstDiff(refOut, cgoOut))
		}
	})
}

// firstDiff returns the index of the first byte that differs between a and b, or
// -1 when one is a prefix of the other (lengths differ) or they are equal.
func firstDiff(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}
