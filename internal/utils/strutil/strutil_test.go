package strutil

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestTakeBytesAtCharBoundary(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		maxByte int
		want    string
	}{
		{"fits exactly", "hello", 5, "hello"},
		{"fits under", "hi", 10, "hi"},
		{"ascii prefix", "hello world", 5, "hello"},
		{"empty", "", 0, ""},
		{"zero budget", "abc", 0, ""},
		{"utf8 boundary cuts whole rune", "😀abc", 4, "😀"},
		{"utf8 budget splits rune mid-byte", "😀abc", 3, ""},
		{"utf8 keeps rune plus ascii", "😀abc", 5, "😀a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TakeBytesAtCharBoundary(tt.input, tt.maxByte); got != tt.want {
				t.Fatalf("TakeBytesAtCharBoundary(%q, %d) = %q, want %q", tt.input, tt.maxByte, got, tt.want)
			}
		})
	}
}

func TestSanitizeMetricTagValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"trims and fills unspecified", "///", "unspecified"},
		{"replaces invalid chars", "bad value!", "bad_value"},
		{"all underscores", "___", "unspecified"},
		{"empty", "", "unspecified"},
		{"allowed punctuation kept", "a.b-c_d/e", "a.b-c_d/e"},
		{"leading trailing underscores trimmed", "_abc_", "abc"},
		{"non-ascii becomes underscore", "café", "caf"},
		{"digits ok", "v1.2.3", "v1.2.3"},
		{"keeps internal underscores", "a__b", "a__b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeMetricTagValue(tt.input); got != tt.want {
				t.Fatalf("SanitizeMetricTagValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeMetricTagValueCapsAt256(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	got := SanitizeMetricTagValue(string(long))
	if len(got) != 256 {
		t.Fatalf("expected length 256, got %d", len(got))
	}
}

func TestFindUUIDs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "finds multiple",
			input: "x 00112233-4455-6677-8899-aabbccddeeff-k y 12345678-90ab-cdef-0123-456789abcdef",
			want: []string{
				"00112233-4455-6677-8899-aabbccddeeff",
				"12345678-90ab-cdef-0123-456789abcdef",
			},
		},
		{
			name:  "ignores invalid",
			input: "not-a-uuid-1234-5678-9abc-def0-123456789abc",
			want:  []string{},
		},
		{
			name:  "non-ascii without overlap",
			input: "🙂 55e5d6f7-8a7f-4d2a-8d88-123456789012abc",
			want:  []string{"55e5d6f7-8a7f-4d2a-8d88-123456789012"},
		},
		{
			name:  "empty input",
			input: "",
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindUUIDs(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("FindUUIDs(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeMarkdownHashLocationSuffix(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{"single location", "#L74C3", ":74:3", true},
		{"range", "#L74C3-L76C9", ":74:3-76:9", true},
		{"line only", "#L10", ":10", true},
		{"range line only", "#L10-L20", ":10-20", true},
		{"mixed column presence", "#L10-L20C5", ":10-20:5", true},
		{"missing hash prefix", "L74C3", "", false},
		{"missing L prefix", "#74C3", "", false},
		{"bad end point", "#L10-20", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeMarkdownHashLocationSuffix(tt.input)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("NormalizeMarkdownHashLocationSuffix(%q) = (%q, %v), want (%q, %v)",
					tt.input, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestToASCIIJSONStringEscapesNonASCII(t *testing.T) {
	type workspace struct {
		Label string `json:"label"`
		Emoji string `json:"emoji"`
	}
	type payload struct {
		Workspaces map[string]workspace `json:"workspaces"`
	}

	value := payload{
		Workspaces: map[string]workspace{
			"/tmp/東京": {Label: "Agentlarım", Emoji: "🚀"},
		},
	}

	got, err := ToASCIIJSONString(value)
	if err != nil {
		t.Fatalf("ToASCIIJSONString returned error: %v", err)
	}

	// Matches serde_json's escaped, ASCII-safe compact output exactly. The
	// doubled backslashes make the literal contain the escape sequences
	// "東" (東), "京" (京), "ı" (ı), and the surrogate pair for 🚀.
	want := "{\"workspaces\":{\"/tmp/\\u6771\\u4eac\":{\"label\":\"Agentlar\\u0131m\",\"emoji\":\"\\ud83d\\ude80\"}}}"
	if got != want {
		t.Fatalf("ToASCIIJSONString = %q, want %q", got, want)
	}

	if !isASCIIString(got) {
		t.Fatalf("result is not pure ASCII: %q", got)
	}

	// Must round-trip back to the same JSON value.
	var reparsed any
	if err := json.Unmarshal([]byte(got), &reparsed); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	expected := map[string]any{
		"workspaces": map[string]any{
			"/tmp/東京": map[string]any{
				"label": "Agentlarım",
				"emoji": "🚀",
			},
		},
	}
	if !reflect.DeepEqual(reparsed, expected) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", reparsed, expected)
	}
}

func TestToASCIIJSONStringDoesNotHTMLEscape(t *testing.T) {
	got, err := ToASCIIJSONString("<a> & </a>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `"<a> & </a>"`
	if got != want {
		t.Fatalf("ToASCIIJSONString = %q, want %q", got, want)
	}
}

func TestToASCIIJSONStringPlainValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"int", 42, "42"},
		{"bool", true, "true"},
		{"nil", nil, "null"},
		{"ascii string", "hello", `"hello"`},
		{"slice", []int{1, 2, 3}, "[1,2,3]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToASCIIJSONString(tt.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ToASCIIJSONString(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestToASCIIJSONStringErrorsOnUnsupported(t *testing.T) {
	if _, err := ToASCIIJSONString(make(chan int)); err == nil {
		t.Fatal("expected error for unsupported type, got nil")
	}
}
