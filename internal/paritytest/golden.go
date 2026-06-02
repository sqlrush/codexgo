// Package paritytest provides the differential / golden-file harness that codexgo
// uses to prove byte, protocol, and behavioral parity with the frozen reference
// build of OpenAI Codex (0.136.0). See docs/specs/00-project-foundation.md.
//
// Two assertion styles are offered:
//
//   - AssertBytes      — for artifacts whose exact bytes Codex emits must be
//     reproduced (e.g. apply_patch output, generated policy files).
//   - AssertJSONEqual  — for JSON where Go's encoding/json key ordering makes
//     literal byte equality impractical; equality is checked after
//     canonicalization. Every use implies an entry in DEVIATIONS.md explaining
//     why byte parity was not feasible.
package paritytest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// GoldenDir is the path, relative to a test's package directory, that callers
// typically join against; fixtures captured from the reference binary live under
// the repository's top-level testdata/golden tree.
const GoldenDir = "testdata/golden"

// Load reads a golden fixture at the given path, failing the test if it is missing.
func Load(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("paritytest: load golden %q: %v", path, err)
	}
	return b
}

// AssertBytes fails unless got is byte-identical to the golden file at path.
func AssertBytes(t *testing.T, path string, got []byte) {
	t.Helper()
	want := Load(t, path)
	if !bytes.Equal(want, got) {
		t.Errorf("paritytest: byte mismatch for %s\n--- want (%d bytes) ---\n%s\n--- got (%d bytes) ---\n%s",
			filepath.Base(path), len(want), want, len(got), got)
	}
}

// AssertJSONEqual fails unless got is semantically equal to the golden file at
// path after canonicalization (sorted object keys, normalized whitespace).
func AssertJSONEqual(t *testing.T, path string, got []byte) {
	t.Helper()
	wantC, err := CanonicalizeJSON(Load(t, path))
	if err != nil {
		t.Fatalf("paritytest: canonicalize want %q: %v", path, err)
	}
	gotC, err := CanonicalizeJSON(got)
	if err != nil {
		t.Fatalf("paritytest: canonicalize got: %v", err)
	}
	if !bytes.Equal(wantC, gotC) {
		t.Errorf("paritytest: json mismatch for %s\n--- want ---\n%s\n--- got ---\n%s",
			filepath.Base(path), wantC, gotC)
	}
}

// CanonicalizeJSON returns a stable representation of arbitrary JSON. Decoding
// into any and re-encoding sorts object keys (encoding/json marshals
// map[string]any in sorted key order) and strips insignificant whitespace, so
// semantically equal documents compare byte-equal.
func CanonicalizeJSON(b []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
