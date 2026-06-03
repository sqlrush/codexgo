package applypatch

import "testing"

func TestSeekSequence(t *testing.T) {
	tests := []struct {
		name    string
		lines   []string
		pattern []string
		start   int
		eof     bool
		wantIdx int
		wantOK  bool
	}{
		{
			name:    "exact match finds sequence",
			lines:   []string{"foo", "bar", "baz"},
			pattern: []string{"bar", "baz"},
			wantIdx: 1, wantOK: true,
		},
		{
			name:    "rstrip match ignores trailing whitespace",
			lines:   []string{"foo   ", "bar\t\t"},
			pattern: []string{"foo", "bar"},
			wantIdx: 0, wantOK: true,
		},
		{
			name:    "trim match ignores leading and trailing whitespace",
			lines:   []string{"    foo   ", "   bar\t"},
			pattern: []string{"foo", "bar"},
			wantIdx: 0, wantOK: true,
		},
		{
			name:    "pattern longer than input returns none",
			lines:   []string{"just one line"},
			pattern: []string{"too", "many", "lines"},
			wantOK:  false,
		},
		{
			name:    "empty pattern returns start",
			lines:   []string{"a", "b"},
			pattern: nil,
			start:   1,
			wantIdx: 1, wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idx, ok := seekSequence(tc.lines, tc.pattern, tc.start, tc.eof)
			if ok != tc.wantOK {
				t.Fatalf("ok got %v, want %v", ok, tc.wantOK)
			}
			if ok && idx != tc.wantIdx {
				t.Fatalf("idx got %d, want %d", idx, tc.wantIdx)
			}
		})
	}
}

// TestSeekSequenceUnicodeNormalization exercises the most permissive matching
// pass: an ASCII-authored pattern should match a source line containing
// typographic Unicode punctuation.
func TestSeekSequenceUnicodeNormalization(t *testing.T) {
	// EN DASH (U+2013) and NON-BREAKING HYPHEN (U+2011) in the source.
	source := "import asyncio  # local import – avoids top‑level dep"
	pattern := "import asyncio  # local import - avoids top-level dep"
	idx, ok := seekSequence([]string{source}, []string{pattern}, 0, false)
	if !ok || idx != 0 {
		t.Fatalf("got (%d, %v), want (0, true)", idx, ok)
	}
}
