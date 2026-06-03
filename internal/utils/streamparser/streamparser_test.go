package streamparser

import (
	"reflect"
	"testing"
)

// Compile-time assertions that the concrete parsers satisfy StreamTextParser.
var (
	_ StreamTextParser[string]                  = (*CitationStreamParser)(nil)
	_ StreamTextParser[ProposedPlanSegment]     = (*ProposedPlanParser)(nil)
	_ StreamTextParser[ExtractedInlineTag[int]] = (*InlineHiddenTagParser[int])(nil)
)

// collectCitationChunks feeds all chunks then finishes, accumulating output.
func collectCitationChunks(t *testing.T, p *CitationStreamParser, chunks []string) StreamTextChunk[string] {
	t.Helper()
	all := StreamTextChunk[string]{}
	for _, c := range chunks {
		next := p.PushStr(c)
		all.VisibleText += next.VisibleText
		all.Extracted = append(all.Extracted, next.Extracted...)
	}
	tail := p.Finish()
	all.VisibleText += tail.VisibleText
	all.Extracted = append(all.Extracted, tail.Extracted...)
	return all
}

func TestCitationStreamParser(t *testing.T) {
	tests := []struct {
		name         string
		chunks       []string
		wantVisible  string
		wantCitation []string
	}{
		{
			name:         "streams across chunk boundaries",
			chunks:       []string{"Hello <oai-mem-", "citation>source A</oai-mem-", "citation> world"},
			wantVisible:  "Hello  world",
			wantCitation: []string{"source A"},
		},
		{
			name:         "auto closes unterminated tag on finish",
			chunks:       []string{"x<oai-mem-citation>source"},
			wantVisible:  "x",
			wantCitation: []string{"source"},
		},
		{
			name:         "preserves partial open tag at eof if not a full tag",
			chunks:       []string{"hello <oai-mem-"},
			wantVisible:  "hello <oai-mem-",
			wantCitation: nil,
		},
		{
			name:         "single chunk multiple citations",
			chunks:       []string{"a<oai-mem-citation>one</oai-mem-citation>b<oai-mem-citation>two</oai-mem-citation>c"},
			wantVisible:  "abc",
			wantCitation: []string{"one", "two"},
		},
		{
			name:         "does not support nested tags",
			chunks:       []string{"a<oai-mem-citation>x<oai-mem-citation>y</oai-mem-citation>z</oai-mem-citation>b"},
			wantVisible:  "az</oai-mem-citation>b",
			wantCitation: []string{"x<oai-mem-citation>y"},
		},
		{
			name:         "no tags at all",
			chunks:       []string{"plain ", "text"},
			wantVisible:  "plain text",
			wantCitation: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewCitationStreamParser()
			got := collectCitationChunks(t, p, tt.chunks)
			if got.VisibleText != tt.wantVisible {
				t.Errorf("visible = %q, want %q", got.VisibleText, tt.wantVisible)
			}
			if !reflect.DeepEqual(got.Extracted, tt.wantCitation) {
				t.Errorf("citations = %#v, want %#v", got.Extracted, tt.wantCitation)
			}
		})
	}
}

func TestCitationStreamParserBuffersPartialOpenTagPrefix(t *testing.T) {
	p := NewCitationStreamParser()

	first := p.PushStr("abc <oai-mem-")
	if first.VisibleText != "abc " {
		t.Fatalf("first visible = %q, want %q", first.VisibleText, "abc ")
	}
	if len(first.Extracted) != 0 {
		t.Fatalf("first extracted = %#v, want empty", first.Extracted)
	}

	second := p.PushStr("citation>x</oai-mem-citation>z")
	tail := p.Finish()

	if second.VisibleText != "z" {
		t.Errorf("second visible = %q, want %q", second.VisibleText, "z")
	}
	if !reflect.DeepEqual(second.Extracted, []string{"x"}) {
		t.Errorf("second extracted = %#v, want [x]", second.Extracted)
	}
	if !tail.IsEmpty() {
		t.Errorf("tail not empty: %#v", tail)
	}
}

func TestStripCitations(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantVis   string
		wantCites []string
	}{
		{
			name:      "collects all citations",
			input:     "a<oai-mem-citation>one</oai-mem-citation>b<oai-mem-citation>two</oai-mem-citation>c",
			wantVis:   "abc",
			wantCites: []string{"one", "two"},
		},
		{
			name:      "auto closes unterminated citation at eof",
			input:     "x<oai-mem-citation>y",
			wantVis:   "x",
			wantCites: []string{"y"},
		},
		{
			name:      "no citations",
			input:     "just text",
			wantVis:   "just text",
			wantCites: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vis, cites := StripCitations(tt.input)
			if vis != tt.wantVis {
				t.Errorf("visible = %q, want %q", vis, tt.wantVis)
			}
			if !reflect.DeepEqual(cites, tt.wantCites) {
				t.Errorf("citations = %#v, want %#v", cites, tt.wantCites)
			}
		})
	}
}

type genericTag int

const (
	tagA genericTag = iota
	tagB
)

func collectInlineChunks(t *testing.T, p *InlineHiddenTagParser[genericTag], chunks []string) StreamTextChunk[ExtractedInlineTag[genericTag]] {
	t.Helper()
	all := StreamTextChunk[ExtractedInlineTag[genericTag]]{}
	for _, c := range chunks {
		next := p.PushStr(c)
		all.VisibleText += next.VisibleText
		all.Extracted = append(all.Extracted, next.Extracted...)
	}
	tail := p.Finish()
	all.VisibleText += tail.VisibleText
	all.Extracted = append(all.Extracted, tail.Extracted...)
	return all
}

func TestInlineHiddenTagParser(t *testing.T) {
	tests := []struct {
		name        string
		specs       []InlineTagSpec[genericTag]
		chunks      []string
		wantVisible string
		wantTags    []ExtractedInlineTag[genericTag]
	}{
		{
			name: "multiple tag types",
			specs: []InlineTagSpec[genericTag]{
				{Tag: tagA, Open: "<a>", Close: "</a>"},
				{Tag: tagB, Open: "<b>", Close: "</b>"},
			},
			chunks:      []string{"1<a>x</a>2<b>y</b>3"},
			wantVisible: "123",
			wantTags: []ExtractedInlineTag[genericTag]{
				{Tag: tagA, Content: "x"},
				{Tag: tagB, Content: "y"},
			},
		},
		{
			name: "non-ascii tag delimiters split across chunks",
			specs: []InlineTagSpec[genericTag]{
				{Tag: tagA, Open: "<é>", Close: "</é>"},
			},
			chunks:      []string{"a<", "é>中</", "é>b"},
			wantVisible: "ab",
			wantTags: []ExtractedInlineTag[genericTag]{
				{Tag: tagA, Content: "中"},
			},
		},
		{
			name: "prefers longest opener at same offset",
			specs: []InlineTagSpec[genericTag]{
				{Tag: tagA, Open: "<a>", Close: "</a>"},
				{Tag: tagB, Open: "<ab>", Close: "</ab>"},
			},
			chunks:      []string{"x<ab>y</ab>z"},
			wantVisible: "xz",
			wantTags: []ExtractedInlineTag[genericTag]{
				{Tag: tagB, Content: "y"},
			},
		},
		{
			name: "auto closes open tag at eof",
			specs: []InlineTagSpec[genericTag]{
				{Tag: tagA, Open: "<a>", Close: "</a>"},
			},
			chunks:      []string{"hi<a>body"},
			wantVisible: "hi",
			wantTags: []ExtractedInlineTag[genericTag]{
				{Tag: tagA, Content: "body"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewInlineHiddenTagParser(tt.specs)
			got := collectInlineChunks(t, p, tt.chunks)
			if got.VisibleText != tt.wantVisible {
				t.Errorf("visible = %q, want %q", got.VisibleText, tt.wantVisible)
			}
			if !reflect.DeepEqual(got.Extracted, tt.wantTags) {
				t.Errorf("extracted = %#v, want %#v", got.Extracted, tt.wantTags)
			}
		})
	}
}

func TestInlineHiddenTagParserPanics(t *testing.T) {
	tests := []struct {
		name  string
		specs []InlineTagSpec[genericTag]
	}{
		{name: "empty specs", specs: nil},
		{name: "empty open", specs: []InlineTagSpec[genericTag]{{Tag: tagA, Open: "", Close: "</a>"}}},
		{name: "empty close", specs: []InlineTagSpec[genericTag]{{Tag: tagA, Open: "<a>", Close: ""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic, got none")
				}
			}()
			_ = NewInlineHiddenTagParser(tt.specs)
		})
	}
}

func TestInlineHiddenTagParserDoesNotMutateInput(t *testing.T) {
	specs := []InlineTagSpec[genericTag]{
		{Tag: tagA, Open: "<a>", Close: "</a>"},
	}
	before := make([]InlineTagSpec[genericTag], len(specs))
	copy(before, specs)
	p := NewInlineHiddenTagParser(specs)
	_ = p.PushStr("<a>x</a>")
	if !reflect.DeepEqual(specs, before) {
		t.Errorf("input specs mutated: %#v != %#v", specs, before)
	}
}

func collectPlanChunks(t *testing.T, p *ProposedPlanParser, chunks []string) StreamTextChunk[ProposedPlanSegment] {
	t.Helper()
	all := StreamTextChunk[ProposedPlanSegment]{}
	for _, c := range chunks {
		next := p.PushStr(c)
		all.VisibleText += next.VisibleText
		all.Extracted = append(all.Extracted, next.Extracted...)
	}
	tail := p.Finish()
	all.VisibleText += tail.VisibleText
	all.Extracted = append(all.Extracted, tail.Extracted...)
	return all
}

func TestProposedPlanParser(t *testing.T) {
	tests := []struct {
		name        string
		chunks      []string
		wantVisible string
		wantSegs    []ProposedPlanSegment
	}{
		{
			name:        "streams plan segments and visible text",
			chunks:      []string{"Intro text\n<prop", "osed_plan>\n- step 1\n", "</proposed_plan>\nOutro"},
			wantVisible: "Intro text\nOutro",
			wantSegs: []ProposedPlanSegment{
				NewProposedPlanNormal("Intro text\n"),
				NewProposedPlanStart(),
				NewProposedPlanDelta("- step 1\n"),
				NewProposedPlanEnd(),
				NewProposedPlanNormal("Outro"),
			},
		},
		{
			name:        "preserves non-tag lines",
			chunks:      []string{"  <proposed_plan> extra\n"},
			wantVisible: "  <proposed_plan> extra\n",
			wantSegs: []ProposedPlanSegment{
				NewProposedPlanNormal("  <proposed_plan> extra\n"),
			},
		},
		{
			name:        "closes unterminated plan block on finish",
			chunks:      []string{"<proposed_plan>\n- step 1\n"},
			wantVisible: "",
			wantSegs: []ProposedPlanSegment{
				NewProposedPlanStart(),
				NewProposedPlanDelta("- step 1\n"),
				NewProposedPlanEnd(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProposedPlanParser()
			got := collectPlanChunks(t, p, tt.chunks)
			if got.VisibleText != tt.wantVisible {
				t.Errorf("visible = %q, want %q", got.VisibleText, tt.wantVisible)
			}
			if !reflect.DeepEqual(got.Extracted, tt.wantSegs) {
				t.Errorf("segments = %#v, want %#v", got.Extracted, tt.wantSegs)
			}
		})
	}
}

func TestStripProposedPlanBlocks(t *testing.T) {
	text := "before\n<proposed_plan>\n- step\n</proposed_plan>\nafter"
	if got := StripProposedPlanBlocks(text); got != "before\nafter" {
		t.Errorf("StripProposedPlanBlocks = %q, want %q", got, "before\nafter")
	}
}

func TestExtractProposedPlanText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantHad bool
	}{
		{
			name:    "extracts plan text",
			input:   "before\n<proposed_plan>\n- step\n</proposed_plan>\nafter",
			want:    "- step\n",
			wantHad: true,
		},
		{
			name:    "no plan block",
			input:   "just normal text\nmore text\n",
			want:    "",
			wantHad: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, had := ExtractProposedPlanText(tt.input)
			if had != tt.wantHad {
				t.Errorf("had = %v, want %v", had, tt.wantHad)
			}
			if got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssistantTextStreamParserCitationsOnly(t *testing.T) {
	p := NewAssistantTextStreamParser(false)

	seeded := p.PushStr("hello <oai-mem-citation>doc")
	parsed := p.PushStr("1</oai-mem-citation> world")
	tail := p.Finish()

	if seeded.VisibleText != "hello " {
		t.Errorf("seeded visible = %q, want %q", seeded.VisibleText, "hello ")
	}
	if len(seeded.Citations) != 0 {
		t.Errorf("seeded citations = %#v, want empty", seeded.Citations)
	}
	if parsed.VisibleText != " world" {
		t.Errorf("parsed visible = %q, want %q", parsed.VisibleText, " world")
	}
	if !reflect.DeepEqual(parsed.Citations, []string{"doc1"}) {
		t.Errorf("parsed citations = %#v, want [doc1]", parsed.Citations)
	}
	if tail.VisibleText != "" {
		t.Errorf("tail visible = %q, want empty", tail.VisibleText)
	}
	if len(tail.Citations) != 0 {
		t.Errorf("tail citations = %#v, want empty", tail.Citations)
	}
}

func TestAssistantTextStreamParserPlanMode(t *testing.T) {
	p := NewAssistantTextStreamParser(true)

	seeded := p.PushStr("Intro\n<proposed")
	parsed := p.PushStr("_plan>\n- step <oai-mem-citation>doc</oai-mem-citation>\n")
	tail := p.PushStr("</proposed_plan>\nOutro")
	finish := p.Finish()

	if seeded.VisibleText != "Intro\n" {
		t.Errorf("seeded visible = %q, want %q", seeded.VisibleText, "Intro\n")
	}
	if !reflect.DeepEqual(seeded.PlanSegments, []ProposedPlanSegment{NewProposedPlanNormal("Intro\n")}) {
		t.Errorf("seeded segments = %#v", seeded.PlanSegments)
	}

	if parsed.VisibleText != "" {
		t.Errorf("parsed visible = %q, want empty", parsed.VisibleText)
	}
	if !reflect.DeepEqual(parsed.Citations, []string{"doc"}) {
		t.Errorf("parsed citations = %#v, want [doc]", parsed.Citations)
	}
	wantParsedSegs := []ProposedPlanSegment{
		NewProposedPlanStart(),
		NewProposedPlanDelta("- step \n"),
	}
	if !reflect.DeepEqual(parsed.PlanSegments, wantParsedSegs) {
		t.Errorf("parsed segments = %#v, want %#v", parsed.PlanSegments, wantParsedSegs)
	}

	if tail.VisibleText != "Outro" {
		t.Errorf("tail visible = %q, want %q", tail.VisibleText, "Outro")
	}
	wantTailSegs := []ProposedPlanSegment{
		NewProposedPlanEnd(),
		NewProposedPlanNormal("Outro"),
	}
	if !reflect.DeepEqual(tail.PlanSegments, wantTailSegs) {
		t.Errorf("tail segments = %#v, want %#v", tail.PlanSegments, wantTailSegs)
	}

	if !finish.IsEmpty() {
		t.Errorf("finish not empty: %#v", finish)
	}
}

func TestUtf8StreamParserSplitCodePoints(t *testing.T) {
	chunks := [][]byte{
		{'A', 0xC3},
		{0xA9, '<', 'o', 'a', 'i', '-', 'm', 'e', 'm', '-', 'c', 'i', 't', 'a', 't', 'i', 'o', 'n', '>', 0xE4},
		{0xB8, 0xAD, '<', '/', 'o', 'a', 'i', '-', 'm', 'e', 'm', '-', 'c', 'i', 't', 'a', 't', 'i', 'o', 'n', '>', 'Z'},
	}

	p := NewUtf8StreamParser[string](NewCitationStreamParser())
	all := StreamTextChunk[string]{}
	for _, c := range chunks {
		next, err := p.PushBytes(c)
		if err != nil {
			t.Fatalf("PushBytes error: %v", err)
		}
		all.VisibleText += next.VisibleText
		all.Extracted = append(all.Extracted, next.Extracted...)
	}
	tail, err := p.Finish()
	if err != nil {
		t.Fatalf("Finish error: %v", err)
	}
	all.VisibleText += tail.VisibleText
	all.Extracted = append(all.Extracted, tail.Extracted...)

	if all.VisibleText != "AéZ" {
		t.Errorf("visible = %q, want %q", all.VisibleText, "AéZ")
	}
	if !reflect.DeepEqual(all.Extracted, []string{"中"}) {
		t.Errorf("extracted = %#v, want [中]", all.Extracted)
	}
}

func TestUtf8StreamParserRollsBackOnInvalidContinuation(t *testing.T) {
	p := NewUtf8StreamParser[string](NewCitationStreamParser())

	first, err := p.PushBytes([]byte{0xC3})
	if err != nil {
		t.Fatalf("leading byte should be buffered: %v", err)
	}
	if !first.IsEmpty() {
		t.Fatalf("first not empty: %#v", first)
	}

	_, err = p.PushBytes([]byte{0x28})
	gotErr, ok := err.(*Utf8StreamParserError)
	if !ok {
		t.Fatalf("expected Utf8StreamParserError, got %T %v", err, err)
	}
	if gotErr.Kind != Utf8InvalidUtf8 || gotErr.ValidUpTo != 0 || gotErr.ErrorLen != 1 {
		t.Errorf("err = %#v, want InvalidUtf8{validUpTo:0, errorLen:1}", gotErr)
	}

	second, err := p.PushBytes([]byte{0xA9, 'x'})
	if err != nil {
		t.Fatalf("valid continuation should succeed: %v", err)
	}
	tail, err := p.Finish()
	if err != nil {
		t.Fatalf("finish error: %v", err)
	}
	if second.VisibleText != "éx" {
		t.Errorf("second visible = %q, want %q", second.VisibleText, "éx")
	}
	if len(second.Extracted) != 0 {
		t.Errorf("second extracted = %#v, want empty", second.Extracted)
	}
	if !tail.IsEmpty() {
		t.Errorf("tail not empty: %#v", tail)
	}
}

func TestUtf8StreamParserRollsBackEntireChunkOnInvalidByteAfterValidPrefix(t *testing.T) {
	p := NewUtf8StreamParser[string](NewCitationStreamParser())

	_, err := p.PushBytes([]byte("ok\xFF"))
	gotErr, ok := err.(*Utf8StreamParserError)
	if !ok {
		t.Fatalf("expected Utf8StreamParserError, got %T %v", err, err)
	}
	if gotErr.Kind != Utf8InvalidUtf8 || gotErr.ValidUpTo != 2 || gotErr.ErrorLen != 1 {
		t.Errorf("err = %#v, want InvalidUtf8{validUpTo:2, errorLen:1}", gotErr)
	}

	next, err := p.PushBytes([]byte("!"))
	if err != nil {
		t.Fatalf("recover after rollback: %v", err)
	}
	if next.VisibleText != "!" {
		t.Errorf("next visible = %q, want %q", next.VisibleText, "!")
	}
	if len(next.Extracted) != 0 {
		t.Errorf("next extracted = %#v, want empty", next.Extracted)
	}
}

func TestUtf8StreamParserIncompleteAtEof(t *testing.T) {
	p := NewUtf8StreamParser[string](NewCitationStreamParser())

	out, err := p.PushBytes([]byte{0xE2, 0x82})
	if err != nil {
		t.Fatalf("partial code point should be buffered: %v", err)
	}
	if !out.IsEmpty() {
		t.Fatalf("out not empty: %#v", out)
	}

	_, err = p.Finish()
	gotErr, ok := err.(*Utf8StreamParserError)
	if !ok {
		t.Fatalf("expected Utf8StreamParserError, got %T %v", err, err)
	}
	if gotErr.Kind != Utf8IncompleteAtEof {
		t.Errorf("err = %#v, want IncompleteAtEof", gotErr)
	}
}

func TestUtf8StreamParserIntoInner(t *testing.T) {
	t.Run("errors when partial code point buffered", func(t *testing.T) {
		p := NewUtf8StreamParser[string](NewCitationStreamParser())
		out, err := p.PushBytes([]byte{0xC3})
		if err != nil {
			t.Fatalf("partial should be buffered: %v", err)
		}
		if !out.IsEmpty() {
			t.Fatalf("out not empty: %#v", out)
		}
		_, err = p.IntoInner()
		gotErr, ok := err.(*Utf8StreamParserError)
		if !ok || gotErr.Kind != Utf8IncompleteAtEof {
			t.Errorf("err = %v, want IncompleteAtEof", err)
		}
	})

	t.Run("lossy drops buffered partial code point", func(t *testing.T) {
		p := NewUtf8StreamParser[string](NewCitationStreamParser())
		out, err := p.PushBytes([]byte{0xC3})
		if err != nil {
			t.Fatalf("partial should be buffered: %v", err)
		}
		if !out.IsEmpty() {
			t.Fatalf("out not empty: %#v", out)
		}
		inner := p.IntoInnerLossy()
		tail := inner.Finish()
		if !tail.IsEmpty() {
			t.Errorf("tail not empty: %#v", tail)
		}
	})
}

func TestUtf8StreamParserDoesNotMutateInputSlice(t *testing.T) {
	p := NewUtf8StreamParser[string](NewCitationStreamParser())
	input := []byte("hello")
	before := make([]byte, len(input))
	copy(before, input)
	if _, err := p.PushBytes(input); err != nil {
		t.Fatalf("PushBytes error: %v", err)
	}
	if !reflect.DeepEqual(input, before) {
		t.Errorf("input slice mutated: %v != %v", input, before)
	}
}

func TestUtf8Validate(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantOK    bool
		validUpTo int
		errorLen  int
	}{
		{name: "ascii", input: []byte("abc"), wantOK: true, validUpTo: 3},
		{name: "two-byte", input: []byte("é"), wantOK: true, validUpTo: 2},
		{name: "three-byte", input: []byte("中"), wantOK: true, validUpTo: 3},
		{name: "four-byte", input: []byte("😀"), wantOK: true, validUpTo: 4},
		{name: "incomplete two-byte", input: []byte{0xC3}, wantOK: false, validUpTo: 0, errorLen: 0},
		{name: "incomplete three-byte", input: []byte{0xE2, 0x82}, wantOK: false, validUpTo: 0, errorLen: 0},
		{name: "lone continuation", input: []byte{0x80}, wantOK: false, validUpTo: 0, errorLen: 1},
		{name: "invalid continuation after lead", input: []byte{0xC3, 0x28}, wantOK: false, validUpTo: 0, errorLen: 1},
		{name: "valid prefix then invalid", input: []byte("ok\xFF"), wantOK: false, validUpTo: 2, errorLen: 1},
		{name: "overlong rejected (0xC0)", input: []byte{0xC0, 0x80}, wantOK: false, validUpTo: 0, errorLen: 1},
		{name: "surrogate rejected", input: []byte{0xED, 0xA0, 0x80}, wantOK: false, validUpTo: 0, errorLen: 1},
		{name: "bad third byte", input: []byte{0xE4, 0xB8, 0x28}, wantOK: false, validUpTo: 0, errorLen: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateUTF8(tt.input)
			if got.ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", got.ok, tt.wantOK)
			}
			if !got.ok {
				if got.validUpTo != tt.validUpTo {
					t.Errorf("validUpTo = %d, want %d", got.validUpTo, tt.validUpTo)
				}
				if got.errorLen != tt.errorLen {
					t.Errorf("errorLen = %d, want %d", got.errorLen, tt.errorLen)
				}
			}
		})
	}
}

func TestLongestSuffixPrefixLen(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		needle string
		want   int
	}{
		{name: "no overlap", s: "abc", needle: "xyz", want: 0},
		{name: "full overlap capped", s: "<oai-mem-", needle: "<oai-mem-citation>", want: 9},
		{name: "partial overlap", s: "hello <", needle: "<a>", want: 1},
		{name: "non-ascii boundary respected", s: "</", needle: "</é>", want: 2},
		{name: "empty s", s: "", needle: "<a>", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := longestSuffixPrefixLen(tt.s, tt.needle); got != tt.want {
				t.Errorf("longestSuffixPrefixLen(%q, %q) = %d, want %d", tt.s, tt.needle, got, tt.want)
			}
		})
	}
}

func TestProposedPlanParserFinishWithBufferedTagLine(t *testing.T) {
	tests := []struct {
		name        string
		chunks      []string
		wantVisible string
		wantSegs    []ProposedPlanSegment
	}{
		{
			// Open tag on the final line with no trailing newline must be
			// recognized at Finish, then auto-closed.
			name:        "open tag buffered without trailing newline",
			chunks:      []string{"<proposed_plan>"},
			wantVisible: "",
			wantSegs: []ProposedPlanSegment{
				NewProposedPlanStart(),
				NewProposedPlanEnd(),
			},
		},
		{
			// Close tag on the final buffered line, inside an open block.
			name:        "close tag buffered without trailing newline",
			chunks:      []string{"<proposed_plan>\nbody\n", "</proposed_plan>"},
			wantVisible: "",
			wantSegs: []ProposedPlanSegment{
				NewProposedPlanStart(),
				NewProposedPlanDelta("body\n"),
				NewProposedPlanEnd(),
			},
		},
		{
			// Buffered final line that is neither open nor close stays visible.
			name:        "plain trailing line stays visible",
			chunks:      []string{"plain trailing"},
			wantVisible: "plain trailing",
			wantSegs: []ProposedPlanSegment{
				NewProposedPlanNormal("plain trailing"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProposedPlanParser()
			got := collectPlanChunks(t, p, tt.chunks)
			if got.VisibleText != tt.wantVisible {
				t.Errorf("visible = %q, want %q", got.VisibleText, tt.wantVisible)
			}
			if !reflect.DeepEqual(got.Extracted, tt.wantSegs) {
				t.Errorf("segments = %#v, want %#v", got.Extracted, tt.wantSegs)
			}
		})
	}
}

func TestUtf8StreamParserIntoInnerSuccess(t *testing.T) {
	t.Run("no buffered bytes", func(t *testing.T) {
		p := NewUtf8StreamParser[string](NewCitationStreamParser())
		if _, err := p.PushBytes([]byte("hello")); err != nil {
			t.Fatalf("PushBytes error: %v", err)
		}
		inner, err := p.IntoInner()
		if err != nil {
			t.Fatalf("IntoInner error: %v", err)
		}
		if inner == nil {
			t.Fatal("IntoInner returned nil inner")
		}
	})
}

func TestUtf8StreamParserFinishFlushesBufferedValidText(t *testing.T) {
	// The inner citation parser buffers a partial open-tag prefix ("<oai-mem-")
	// that turns out not to be a full tag. Finish must flush it back out as
	// visible text. The valid prefix "hello " is emitted on the PushBytes call,
	// and the buffered "<oai-mem-" is emitted on Finish.
	p := NewUtf8StreamParser[string](NewCitationStreamParser())

	first, err := p.PushBytes([]byte("hello <oai-mem-"))
	if err != nil {
		t.Fatalf("PushBytes error: %v", err)
	}
	tail, err := p.Finish()
	if err != nil {
		t.Fatalf("Finish error: %v", err)
	}
	combined := first.VisibleText + tail.VisibleText
	if combined != "hello <oai-mem-" {
		t.Errorf("combined visible = %q, want %q", combined, "hello <oai-mem-")
	}
}

func TestUtf8StreamParserErrorMessages(t *testing.T) {
	invalid := &Utf8StreamParserError{Kind: Utf8InvalidUtf8, ValidUpTo: 2, ErrorLen: 1}
	if got := invalid.Error(); got != "invalid UTF-8 in streamed bytes at offset 2 (error length 1)" {
		t.Errorf("invalid error message = %q", got)
	}
	incomplete := &Utf8StreamParserError{Kind: Utf8IncompleteAtEof}
	if got := incomplete.Error(); got != "incomplete UTF-8 code point at end of stream" {
		t.Errorf("incomplete error message = %q", got)
	}
}
