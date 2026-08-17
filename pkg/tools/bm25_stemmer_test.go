package tools

import (
	"bufio"
	"os"
	"testing"
)

// TestEnglishStemSnowballVocabulary validates the Snowball English (Porter2)
// stemmer against the rust-stemmers v1.2.0 test vocabulary (test_data/voc_en.txt
// -> res_en.txt). rust-stemmers Algorithm::English is what bm25 v2.3.2 uses, so
// matching this vocabulary byte-for-byte locks the tool_search BM25 ranking to
// codex. Note: rust-stemmers v1.2.0 ships an older Porter2 variant than the
// current snowballstem reference (e.g. it undoubles short stems "added" -> "ad"
// and stems "dyed" -> "dy"), so this data — not snowball-data — is the truth.
func TestEnglishStemSnowballVocabulary(t *testing.T) {
	voc := readLines(t, "testdata/rust_stemmers_en_voc.txt")
	out := readLines(t, "testdata/rust_stemmers_en_res.txt")
	if len(voc) != len(out) {
		t.Fatalf("vocab/output length mismatch: %d vs %d", len(voc), len(out))
	}

	mismatches := 0
	for i := range voc {
		got := englishStem(voc[i])
		if got != out[i] {
			mismatches++
			if mismatches <= 30 {
				t.Errorf("englishStem(%q) = %q, want %q", voc[i], got, out[i])
			}
		}
	}
	if mismatches > 0 {
		t.Errorf("total stemmer mismatches: %d / %d", mismatches, len(voc))
	}
}

// TestEnglishStemSearchTokens spot-checks the tokens that appear in the
// tool_search search texts and the query.
func TestEnglishStemSearchTokens(t *testing.T) {
	cases := []struct{ in, want string }{
		{"spawn", "spawn"},
		{"agent", "agent"},
		{"agents", "agent"},
		{"subagent", "subag"},
		{"delegate", "deleg"},
		{"delegation", "deleg"},
		{"parallel", "parallel"},
		{"worker", "worker"},
		{"explorer", "explor"},
		{"reasoning", "reason"},
		{"message", "messag"},
		{"existing", "exist"},
		{"interrupt", "interrupt"},
		{"redirect", "redirect"},
		{"queue", "queue"},
		{"target", "target"},
		{"targets", "target"},
		{"resume", "resum"},
		{"reopen", "reopen"},
		{"closed", "close"},
		{"complete", "complet"},
		{"timeout", "timeout"},
		{"status", "status"},
	}
	for _, c := range cases {
		if got := englishStem(c.in); got != c.want {
			t.Errorf("englishStem(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return lines
}
