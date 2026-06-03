package fuzzymatch

import (
	"math"
	"reflect"
	"testing"
)

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		name        string
		haystack    string
		needle      string
		wantOK      bool
		wantIndices []int
		wantScore   int
	}{
		{
			// ASCII: 'h' at 0, 'l' at 2 -> window 1; start-of-string bonus (-100).
			name:        "ascii basic indices",
			haystack:    "hello",
			needle:      "hl",
			wantOK:      true,
			wantIndices: []int{0, 2},
			wantScore:   -99,
		},
		{
			// İ lowercases to "i" + combining dot above, so 's' lands at lowered
			// position 2 -> window 1; start-of-string bonus applies.
			name:        "unicode dotted i istanbul highlighting",
			haystack:    "İstanbul",
			needle:      "is",
			wantOK:      true,
			wantIndices: []int{0, 1},
			wantScore:   -99,
		},
		{
			// ß lowercases to itself, so "strasse" cannot match "straße".
			name:     "unicode german sharp s casefold",
			haystack: "straße",
			needle:   "strasse",
			wantOK:   false,
		},
		{
			// Contiguous window -> 0; start-of-string bonus -> -100.
			name:        "prefer contiguous match contiguous",
			haystack:    "abc",
			needle:      "abc",
			wantOK:      true,
			wantIndices: []int{0, 1, 2},
			wantScore:   -100,
		},
		{
			// Spread over 5 chars for a 3-letter needle -> window 2; bonus -> -98.
			name:        "prefer contiguous match spread",
			haystack:    "a-b-c",
			needle:      "abc",
			wantOK:      true,
			wantIndices: []int{0, 2, 4},
			wantScore:   -98,
		},
		{
			// Start-of-string contiguous -> window 0; bonus -> -100.
			name:        "start of string bonus prefix",
			haystack:    "file_name",
			needle:      "file",
			wantOK:      true,
			wantIndices: []int{0, 1, 2, 3},
			wantScore:   -100,
		},
		{
			// Non-prefix contiguous -> window 0; no bonus -> 0.
			name:        "start of string bonus non prefix",
			haystack:    "my_file_name",
			needle:      "file",
			wantOK:      true,
			wantIndices: []int{3, 4, 5, 6},
			wantScore:   0,
		},
		{
			name:        "empty needle matches with max score and no indices",
			haystack:    "anything",
			needle:      "",
			wantOK:      true,
			wantIndices: []int{},
			wantScore:   math.MaxInt32,
		},
		{
			// Case-insensitive contiguous prefix match -> window 0 with bonus.
			name:        "case insensitive matching basic",
			haystack:    "FooBar",
			needle:      "foO",
			wantOK:      true,
			wantIndices: []int{0, 1, 2},
			wantScore:   -100,
		},
		{
			// Needle "i" + combining dot above; lowercasing İ expands to two
			// chars; contiguous prefix -> window 0 with bonus; indices deduped.
			name:        "indices deduped for multichar lowercase expansion",
			haystack:    "İ",
			needle:      "i̇",
			wantOK:      true,
			wantIndices: []int{0},
			wantScore:   -100,
		},
		{
			name:     "no match returns false",
			haystack: "hello",
			needle:   "xyz",
			wantOK:   false,
		},
		{
			name:     "needle longer than haystack",
			haystack: "ab",
			needle:   "abc",
			wantOK:   false,
		},
		{
			name:     "empty haystack non empty needle",
			haystack: "",
			needle:   "a",
			wantOK:   false,
		},
		{
			name:        "empty haystack empty needle",
			haystack:    "",
			needle:      "",
			wantOK:      true,
			wantIndices: []int{},
			wantScore:   math.MaxInt32,
		},
		{
			// Single character match at the start: window 0, prefix bonus.
			name:        "single char prefix",
			haystack:    "hello",
			needle:      "h",
			wantOK:      true,
			wantIndices: []int{0},
			wantScore:   -100,
		},
		{
			// Single character match not at the start: window 0, no bonus.
			name:        "single char non prefix",
			haystack:    "hello",
			needle:      "o",
			wantOK:      true,
			wantIndices: []int{4},
			wantScore:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FuzzyMatch(tt.haystack, tt.needle)
			if ok != tt.wantOK {
				t.Fatalf("FuzzyMatch(%q, %q) ok = %v, want %v", tt.haystack, tt.needle, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if !reflect.DeepEqual(got.Indices, tt.wantIndices) {
				t.Errorf("indices = %v, want %v", got.Indices, tt.wantIndices)
			}
			if got.Score != tt.wantScore {
				t.Errorf("score = %d, want %d", got.Score, tt.wantScore)
			}
		})
	}
}

// TestContiguousBeatsSpread documents that a contiguous match scores strictly
// better (smaller) than a spread one, matching the Rust assertion.
func TestContiguousBeatsSpread(t *testing.T) {
	a, okA := FuzzyMatch("abc", "abc")
	b, okB := FuzzyMatch("a-b-c", "abc")
	if !okA || !okB {
		t.Fatalf("expected both to match: okA=%v okB=%v", okA, okB)
	}
	if !(a.Score < b.Score) {
		t.Errorf("contiguous score %d should be < spread score %d", a.Score, b.Score)
	}
}

// TestPrefixBeatsNonPrefix documents that a start-of-string match scores
// strictly better than the same match later in the string.
func TestPrefixBeatsNonPrefix(t *testing.T) {
	a, okA := FuzzyMatch("file_name", "file")
	b, okB := FuzzyMatch("my_file_name", "file")
	if !okA || !okB {
		t.Fatalf("expected both to match: okA=%v okB=%v", okA, okB)
	}
	if !(a.Score < b.Score) {
		t.Errorf("prefix score %d should be < non-prefix score %d", a.Score, b.Score)
	}
}

// TestFuzzyMatchDoesNotMutateInputsOrShareState verifies immutability: repeated
// calls return independent slices and the returned slice can be mutated without
// affecting subsequent calls.
func TestFuzzyMatchDoesNotShareReturnedSlice(t *testing.T) {
	m1, ok := FuzzyMatch("abc", "abc")
	if !ok {
		t.Fatal("expected a match")
	}
	m1.Indices[0] = 999
	m2, ok := FuzzyMatch("abc", "abc")
	if !ok {
		t.Fatal("expected a match")
	}
	if m2.Indices[0] != 0 {
		t.Errorf("mutation of returned slice leaked into later call: got %v", m2.Indices)
	}
}

func TestLowerString(t *testing.T) {
	tests := []struct {
		in   string
		want []rune
	}{
		{"ABC", []rune{'a', 'b', 'c'}},
		{"İ", []rune{'i', '̇'}},
		{"straße", []rune{'s', 't', 'r', 'a', 'ß', 'e'}},
		{"", []rune{}},
	}
	for _, tt := range tests {
		got := lowerString(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("lowerString(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
