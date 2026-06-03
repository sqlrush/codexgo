package strutil

import "testing"

func TestSplitString(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		beginning   int
		end         int
		wantRemoved int
		wantBefore  string
		wantAfter   string
	}{
		{"works", "hello world", 5, 5, 1, "hello", "world"},
		{"zero budgets", "abc", 0, 0, 3, "", ""},
		{"empty string", "", 4, 4, 0, "", ""},
		{"tail budget zero", "abcdef", 3, 0, 3, "abc", ""},
		{"prefix budget zero", "abcdef", 0, 3, 3, "", "def"},
		{"overlapping no removal", "abcdef", 4, 4, 0, "abcd", "ef"},
		{"utf8 boundaries 1", "😀abc😀", 5, 5, 1, "😀a", "c😀"},
		{"utf8 boundaries small", "😀😀😀😀😀", 1, 1, 5, "", ""},
		{"utf8 boundaries 7", "😀😀😀😀😀", 7, 7, 3, "😀", "😀"},
		{"utf8 boundaries 8", "😀😀😀😀😀", 8, 8, 1, "😀😀", "😀😀"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed, before, after := splitString(tt.input, tt.beginning, tt.end)
			if removed != tt.wantRemoved || before != tt.wantBefore || after != tt.wantAfter {
				t.Fatalf("splitString(%q, %d, %d) = (%d, %q, %q), want (%d, %q, %q)",
					tt.input, tt.beginning, tt.end,
					removed, before, after,
					tt.wantRemoved, tt.wantBefore, tt.wantAfter)
			}
		})
	}
}

func TestTruncateMiddleWithTokenBudget(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		maxTokens     int
		wantOut       string
		wantTokens    uint64
		wantTruncated bool
	}{
		{
			name:          "under limit returns original",
			input:         "short output",
			maxTokens:     100,
			wantOut:       "short output",
			wantTokens:    0,
			wantTruncated: false,
		},
		{
			name:          "zero limit reports truncation",
			input:         "abcdef",
			maxTokens:     0,
			wantOut:       "…2 tokens truncated…",
			wantTokens:    2,
			wantTruncated: true,
		},
		{
			name:          "utf8 content",
			input:         "😀😀😀😀😀😀😀😀😀😀\nsecond line with text\n",
			maxTokens:     8,
			wantOut:       "😀😀😀😀…8 tokens truncated… line with text\n",
			wantTokens:    16,
			wantTruncated: true,
		},
		{
			name:          "empty input",
			input:         "",
			maxTokens:     10,
			wantOut:       "",
			wantTokens:    0,
			wantTruncated: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, tokens, truncated := TruncateMiddleWithTokenBudget(tt.input, tt.maxTokens)
			if out != tt.wantOut || tokens != tt.wantTokens || truncated != tt.wantTruncated {
				t.Fatalf("TruncateMiddleWithTokenBudget(%q, %d) = (%q, %d, %v), want (%q, %d, %v)",
					tt.input, tt.maxTokens,
					out, tokens, truncated,
					tt.wantOut, tt.wantTokens, tt.wantTruncated)
			}
		})
	}
}

func TestTruncateMiddleChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{
			name:     "utf8 content",
			input:    "😀😀😀😀😀😀😀😀😀😀\nsecond line with text\n",
			maxBytes: 20,
			want:     "😀😀…21 chars truncated…with text\n",
		},
		{
			name:     "fits unchanged",
			input:    "hello",
			maxBytes: 100,
			want:     "hello",
		},
		{
			name:     "empty",
			input:    "",
			maxBytes: 10,
			want:     "",
		},
		{
			name:     "zero budget marks all removed",
			input:    "abc",
			maxBytes: 0,
			want:     "…3 chars truncated…",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateMiddleChars(tt.input, tt.maxBytes); got != tt.want {
				t.Fatalf("TruncateMiddleChars(%q, %d) = %q, want %q", tt.input, tt.maxBytes, got, tt.want)
			}
		})
	}
}

func TestApproxTokenCount(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
	}
	for _, tt := range tests {
		if got := ApproxTokenCount(tt.input); got != tt.want {
			t.Fatalf("ApproxTokenCount(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestApproxBytesForTokens(t *testing.T) {
	tests := []struct {
		tokens int
		want   int
	}{
		{0, 0},
		{1, 4},
		{10, 40},
	}
	for _, tt := range tests {
		if got := ApproxBytesForTokens(tt.tokens); got != tt.want {
			t.Fatalf("ApproxBytesForTokens(%d) = %d, want %d", tt.tokens, got, tt.want)
		}
	}
}

func TestApproxTokensFromByteCount(t *testing.T) {
	tests := []struct {
		bytes int
		want  uint64
	}{
		{0, 0},
		{1, 1},
		{4, 1},
		{5, 2},
		{8, 2},
	}
	for _, tt := range tests {
		if got := ApproxTokensFromByteCount(tt.bytes); got != tt.want {
			t.Fatalf("ApproxTokensFromByteCount(%d) = %d, want %d", tt.bytes, got, tt.want)
		}
	}
}
