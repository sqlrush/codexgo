package paritytest

// Output-last-message parity: `codex exec -o/--output-last-message <FILE>` writes
// the final agent message to FILE. CI integrations consume this file, so byte-level
// drop-in matters. This drives both the real codex binary and codexgo through the
// same single-message fake /v1/responses turn (reusing deterministicSSE) and
// asserts the written file is byte-identical. Env-gated on CODEX_PARITY_BIN.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runOutputLastTurn runs `bin exec --json -o <outfile>` against srv in a fresh
// CODEX_HOME, returning the contents of the output-last-message file (and whether
// it was created).
func runOutputLastTurn(t *testing.T, who, bin, serverURL string) ([]byte, bool) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	writeParityConfig(t, home, serverURL)
	outFile := filepath.Join(work, "last-message.txt")

	cmd := exec.Command(bin, "exec", "--json", "--skip-git-repo-check", "-C", work,
		"-o", outFile, turnPrompt)
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+home,
		fakeEnvKey+"="+fakeAPIKey,
		"OPENAI_API_KEY=",
		"CODEX_API_KEY=",
		"CODEX_ACCESS_TOKEN=",
	)
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s exec -o failed: %v\nstdout:\n%s\nstderr:\n%s", who, err, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		t.Fatalf("%s read output-last-message file: %v", who, err)
	}
	return data, true
}

// TestParityOutputLastMessage asserts codexgo writes the SAME -o/--output-last-message
// file as real codex for a single-message turn ("Hello from parity").
func TestParityOutputLastMessage(t *testing.T) {
	refBin := referenceBin(t)
	cgoBin := buildCodexgo(t)

	run := func(who, bin string) []byte {
		srv := newResponsesServer(t)
		defer srv.Close()
		data, ok := runOutputLastTurn(t, who, bin, srv.URL)
		if !ok {
			t.Fatalf("%s did not create the output-last-message file", who)
		}
		return data
	}
	ref := run("codex", refBin)
	cgo := run("codexgo", cgoBin)

	// The reference must capture the final agent message.
	if !bytes.Contains(ref, []byte("Hello from parity")) {
		t.Fatalf("codex output-last-message did not contain the final message: %q", ref)
	}
	if !bytes.Equal(ref, cgo) {
		t.Errorf("output-last-message file mismatch\n codex:   %q\n codexgo: %q", ref, cgo)
	}
}
