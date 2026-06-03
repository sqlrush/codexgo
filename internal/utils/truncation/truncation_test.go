package truncation

import (
	"strings"
	"testing"
)

func detailPtr(d ImageDetail) *ImageDetail {
	return &d
}

func TestFormattedTruncateText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		policy  TruncationPolicy
		want    string
	}{
		{
			name:    "bytes less than placeholder returns placeholder",
			content: "example output",
			policy:  BytesPolicy(1),
			want:    "Total output lines: 1\n\n…13 chars truncated…t",
		},
		{
			name:    "tokens less than placeholder returns placeholder",
			content: "example output",
			policy:  TokensPolicy(1),
			want:    "Total output lines: 1\n\nex…3 tokens truncated…ut",
		},
		{
			name:    "tokens under limit returns original",
			content: "example output",
			policy:  TokensPolicy(10),
			want:    "example output",
		},
		{
			name:    "bytes under limit returns original",
			content: "example output",
			policy:  BytesPolicy(20),
			want:    "example output",
		},
		{
			name:    "tokens over limit returns truncated",
			content: "this is an example of a long output that should be truncated",
			policy:  TokensPolicy(5),
			want:    "Total output lines: 1\n\nthis is an…10 tokens truncated… truncated",
		},
		{
			name:    "bytes over limit returns truncated",
			content: "this is an example of a long output that should be truncated",
			policy:  BytesPolicy(30),
			want:    "Total output lines: 1\n\nthis is an exam…30 chars truncated…ld be truncated",
		},
		{
			name:    "bytes reports original line count when truncated",
			content: "this is an example of a long output that should be truncated\nalso some other line",
			policy:  BytesPolicy(30),
			want:    "Total output lines: 2\n\nthis is an exam…51 chars truncated…some other line",
		},
		{
			name:    "tokens reports original line count when truncated",
			content: "this is an example of a long output that should be truncated\nalso some other line",
			policy:  TokensPolicy(10),
			want:    "Total output lines: 2\n\nthis is an example o…11 tokens truncated…also some other line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormattedTruncateText(tt.content, tt.policy)
			if got != tt.want {
				t.Errorf("FormattedTruncateText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		policy  TruncationPolicy
		want    string
	}{
		{
			name:    "bytes handles utf8 content",
			content: "😀😀😀😀😀😀😀😀😀😀\nsecond line with text\n",
			policy:  BytesPolicy(20),
			want:    "😀😀…21 chars truncated…with text\n",
		},
		{
			name:    "tokens handles utf8 content",
			content: "😀😀😀😀😀😀😀😀😀😀\nsecond line with text\n",
			policy:  TokensPolicy(8),
			want:    "😀😀😀😀…8 tokens truncated… line with text\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TruncateText(tt.content, tt.policy)
			if got != tt.want {
				t.Errorf("TruncateText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateMiddleWithTokenBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		content      string
		maxTokens    int
		wantOut      string
		wantOriginal uint64
		wantOK       bool
	}{
		{
			name:      "under limit returns original",
			content:   "short output",
			maxTokens: 100,
			wantOut:   "short output",
			wantOK:    false,
		},
		{
			name:         "reports truncation at zero limit",
			content:      "abcdef",
			maxTokens:    0,
			wantOut:      "…2 tokens truncated…",
			wantOriginal: 2,
			wantOK:       true,
		},
		{
			name:         "handles utf8 content",
			content:      "😀😀😀😀😀😀😀😀😀😀\nsecond line with text\n",
			maxTokens:    8,
			wantOut:      "😀😀😀😀…8 tokens truncated… line with text\n",
			wantOriginal: 16,
			wantOK:       true,
		},
		{
			name:      "empty input",
			content:   "",
			maxTokens: 0,
			wantOut:   "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, original, ok := TruncateMiddleWithTokenBudget(tt.content, tt.maxTokens)
			if out != tt.wantOut {
				t.Errorf("out = %q, want %q", out, tt.wantOut)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && original != tt.wantOriginal {
				t.Errorf("original = %d, want %d", original, tt.wantOriginal)
			}
		})
	}
}

func TestSplitString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		s              string
		beginningBytes int
		endBytes       int
		wantRemoved    int
		wantBefore     string
		wantAfter      string
	}{
		{"basic", "hello world", 5, 5, 1, "hello", "world"},
		{"all removed", "abc", 0, 0, 3, "", ""},
		{"empty string", "", 4, 4, 0, "", ""},
		{"only prefix when tail budget zero", "abcdef", 3, 0, 3, "abc", ""},
		{"only suffix when prefix budget zero", "abcdef", 0, 3, 3, "", "def"},
		{"overlapping budgets without removal", "abcdef", 4, 4, 0, "abcd", "ef"},
		{"utf8 boundaries small", "😀abc😀", 5, 5, 1, "😀a", "c😀"},
		{"utf8 all removed tiny budget", "😀😀😀😀😀", 1, 1, 5, "", ""},
		{"utf8 keeps one each side", "😀😀😀😀😀", 7, 7, 3, "😀", "😀"},
		{"utf8 keeps two each side", "😀😀😀😀😀", 8, 8, 1, "😀😀", "😀😀"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			removed, before, after := splitString(tt.s, tt.beginningBytes, tt.endBytes)
			if removed != tt.wantRemoved || before != tt.wantBefore || after != tt.wantAfter {
				t.Errorf("splitString(%q, %d, %d) = (%d, %q, %q), want (%d, %q, %q)",
					tt.s, tt.beginningBytes, tt.endBytes,
					removed, before, after,
					tt.wantRemoved, tt.wantBefore, tt.wantAfter)
			}
		})
	}
}

func TestTruncateFunctionOutputItemsWithPolicy_AcrossMultiple(t *testing.T) {
	t.Parallel()

	chunk := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau upsilon phi chi psi omega.\n"
	chunkTokens := ApproxTokenCount(chunk)
	if chunkTokens <= 0 {
		t.Fatalf("chunk must consume tokens")
	}
	limit := chunkTokens * 3
	t1 := chunk
	t2 := chunk
	t3 := strings.Repeat(chunk, 10)
	t4 := chunk
	t5 := chunk

	items := []FunctionCallOutputContentItem{
		NewInputText(t1),
		NewInputText(t2),
		NewInputImage("img:mid", detailPtr(DefaultImageDetail)),
		NewInputText(t3),
		NewInputText(t4),
		NewInputText(t5),
	}

	output := TruncateFunctionOutputItemsWithPolicy(items, TokensPolicy(limit))

	if len(output) != 5 {
		t.Fatalf("len(output) = %d, want 5", len(output))
	}
	if output[0].Kind != KindInputText || output[0].Text != t1 {
		t.Errorf("output[0] = %+v, want input_text %q", output[0], t1)
	}
	if output[1].Kind != KindInputText || output[1].Text != t2 {
		t.Errorf("output[1] = %+v, want input_text %q", output[1], t2)
	}
	wantImage := NewInputImage("img:mid", detailPtr(DefaultImageDetail))
	if !output[2].Equal(wantImage) {
		t.Errorf("output[2] = %+v, want %+v", output[2], wantImage)
	}
	if output[3].Kind != KindInputText || !strings.Contains(output[3].Text, "tokens truncated") {
		t.Errorf("output[3] = %+v, want truncated marker", output[3])
	}
	if output[4].Kind != KindInputText || !strings.Contains(output[4].Text, "omitted 2 text items") {
		t.Errorf("output[4] = %+v, want omitted summary", output[4])
	}
}

func TestTruncateFunctionOutputItemsWithPolicy_PreservesEncrypted(t *testing.T) {
	t.Parallel()

	items := []FunctionCallOutputContentItem{
		NewInputText("abcdefgh"),
		NewEncryptedContent("enc_opaque"),
	}

	output := TruncateFunctionOutputItemsWithPolicy(items, BytesPolicy(2))

	want := []FunctionCallOutputContentItem{
		NewInputText("a…6 chars truncated…h"),
		NewEncryptedContent("enc_opaque"),
	}
	if !itemsEqual(output, want) {
		t.Errorf("output = %+v, want %+v", output, want)
	}
}

func TestFormattedTruncateTextContentItemsWithPolicy(t *testing.T) {
	t.Parallel()

	t.Run("returns original under limit", func(t *testing.T) {
		t.Parallel()
		items := []FunctionCallOutputContentItem{
			NewInputText("alpha"),
			NewInputText(""),
			NewInputText("beta"),
		}
		output, original, ok := FormattedTruncateTextContentItemsWithPolicy(items, BytesPolicy(32))
		if !itemsEqual(output, items) {
			t.Errorf("output = %+v, want %+v", output, items)
		}
		if ok {
			t.Errorf("ok = true (original=%d), want false", original)
		}
	})

	t.Run("preserves empty leading text behavior", func(t *testing.T) {
		t.Parallel()
		items := []FunctionCallOutputContentItem{
			NewInputText(""),
			NewInputText("abc"),
		}
		output, original, ok := FormattedTruncateTextContentItemsWithPolicy(items, BytesPolicy(0))
		want := []FunctionCallOutputContentItem{
			NewInputText("Total output lines: 1\n\n…3 chars truncated…"),
		}
		if !itemsEqual(output, want) {
			t.Errorf("output = %+v, want %+v", output, want)
		}
		if !ok || original != 1 {
			t.Errorf("(original, ok) = (%d, %v), want (1, true)", original, ok)
		}
	})

	t.Run("merges text and appends images", func(t *testing.T) {
		t.Parallel()
		items := []FunctionCallOutputContentItem{
			NewInputText("abcd"),
			NewInputImage("img:one", detailPtr(DefaultImageDetail)),
			NewInputText("efgh"),
			NewInputText("ijkl"),
			NewInputImage("img:two", detailPtr(DefaultImageDetail)),
		}
		output, original, ok := FormattedTruncateTextContentItemsWithPolicy(items, BytesPolicy(8))
		want := []FunctionCallOutputContentItem{
			NewInputText("Total output lines: 3\n\nabcd…6 chars truncated…ijkl"),
			NewInputImage("img:one", detailPtr(DefaultImageDetail)),
			NewInputImage("img:two", detailPtr(DefaultImageDetail)),
		}
		if !itemsEqual(output, want) {
			t.Errorf("output = %+v, want %+v", output, want)
		}
		if !ok || original != 4 {
			t.Errorf("(original, ok) = (%d, %v), want (4, true)", original, ok)
		}
	})

	t.Run("preserves encrypted content", func(t *testing.T) {
		t.Parallel()
		items := []FunctionCallOutputContentItem{
			NewInputText("abcdefgh"),
			NewEncryptedContent("enc_opaque"),
		}
		output, original, ok := FormattedTruncateTextContentItemsWithPolicy(items, BytesPolicy(2))
		want := []FunctionCallOutputContentItem{
			NewInputText("Total output lines: 1\n\na…6 chars truncated…h"),
			NewEncryptedContent("enc_opaque"),
		}
		if !itemsEqual(output, want) {
			t.Errorf("output = %+v, want %+v", output, want)
		}
		if !ok || original != 2 {
			t.Errorf("(original, ok) = (%d, %v), want (2, true)", original, ok)
		}
	})

	t.Run("merges all text for token budget", func(t *testing.T) {
		t.Parallel()
		items := []FunctionCallOutputContentItem{
			NewInputText("abcdefgh"),
			NewInputText("ijklmnop"),
		}
		output, original, ok := FormattedTruncateTextContentItemsWithPolicy(items, TokensPolicy(2))
		want := []FunctionCallOutputContentItem{
			NewInputText("Total output lines: 2\n\nabcd…3 tokens truncated…mnop"),
		}
		if !itemsEqual(output, want) {
			t.Errorf("output = %+v, want %+v", output, want)
		}
		if !ok || original != 5 {
			t.Errorf("(original, ok) = (%d, %v), want (5, true)", original, ok)
		}
	})
}

func TestApproxTokensFromByteCountI64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		bytes int64
		want  int64
	}{
		{-1, 0},
		{0, 0},
		{5, 2},
	}
	for _, tt := range tests {
		got := ApproxTokensFromByteCountI64(tt.bytes)
		if got != tt.want {
			t.Errorf("ApproxTokensFromByteCountI64(%d) = %d, want %d", tt.bytes, got, tt.want)
		}
	}
}

func TestApproxHelpers(t *testing.T) {
	t.Parallel()

	if got := ApproxTokenCount("abcdef"); got != 2 {
		t.Errorf("ApproxTokenCount(6 bytes) = %d, want 2", got)
	}
	if got := ApproxTokenCount(""); got != 0 {
		t.Errorf("ApproxTokenCount(empty) = %d, want 0", got)
	}
	if got := ApproxBytesForTokens(3); got != 12 {
		t.Errorf("ApproxBytesForTokens(3) = %d, want 12", got)
	}
	if got := ApproxTokensFromByteCount(5); got != 2 {
		t.Errorf("ApproxTokensFromByteCount(5) = %d, want 2", got)
	}
}

func TestPolicyBudgets(t *testing.T) {
	t.Parallel()

	bp := BytesPolicy(10)
	if bp.ByteBudget() != 10 {
		t.Errorf("BytesPolicy(10).ByteBudget() = %d, want 10", bp.ByteBudget())
	}
	if bp.TokenBudget() != 3 {
		t.Errorf("BytesPolicy(10).TokenBudget() = %d, want 3", bp.TokenBudget())
	}

	tp := TokensPolicy(7)
	if tp.TokenBudget() != 7 {
		t.Errorf("TokensPolicy(7).TokenBudget() = %d, want 7", tp.TokenBudget())
	}
	if tp.ByteBudget() != 28 {
		t.Errorf("TokensPolicy(7).ByteBudget() = %d, want 28", tp.ByteBudget())
	}
}

func TestCountLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"one", 1},
		{"one\n", 1},
		{"one\ntwo", 2},
		{"one\ntwo\n", 2},
		{"\n", 1},
		{"\n\n", 2},
	}
	for _, tt := range tests {
		if got := countLines(tt.s); got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}
}

func TestContentItemJSONRoundTrip(t *testing.T) {
	t.Parallel()

	items := []FunctionCallOutputContentItem{
		NewInputText("hello"),
		NewInputImage("img:one", detailPtr(ImageDetailHigh)),
		NewInputImage("img:two", nil),
		NewEncryptedContent("enc"),
	}
	for _, it := range items {
		data, err := it.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON(%+v) error: %v", it, err)
		}
		var back FunctionCallOutputContentItem
		if err := back.UnmarshalJSON(data); err != nil {
			t.Fatalf("UnmarshalJSON(%s) error: %v", data, err)
		}
		if !back.Equal(it) {
			t.Errorf("round trip mismatch: got %+v, want %+v (json=%s)", back, it, data)
		}
	}
}

func TestContentItemJSONShape(t *testing.T) {
	t.Parallel()

	data, err := NewInputImage("img:two", nil).MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "detail") {
		t.Errorf("nil detail should be omitted, got %s", got)
	}
	if !strings.Contains(got, `"type":"input_image"`) {
		t.Errorf("missing type discriminator, got %s", got)
	}
}

// TestImmutability verifies that the public helpers do not mutate the
// caller-provided slice or its items.
func TestImmutability(t *testing.T) {
	t.Parallel()

	detail := DefaultImageDetail
	items := []FunctionCallOutputContentItem{
		NewInputText("abcdefgh"),
		NewInputImage("img", &detail),
		NewEncryptedContent("enc"),
	}
	snapshot := cloneItems(items)

	_, _, _ = FormattedTruncateTextContentItemsWithPolicy(items, BytesPolicy(2))
	_ = TruncateFunctionOutputItemsWithPolicy(items, BytesPolicy(2))

	if !itemsEqual(items, snapshot) {
		t.Errorf("input items were mutated: got %+v, want %+v", items, snapshot)
	}
	// The original detail value must be untouched.
	if detail != DefaultImageDetail {
		t.Errorf("original detail value was mutated to %v", detail)
	}
}
