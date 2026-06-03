package streamparser

const (
	proposedPlanOpenTag  = "<proposed_plan>"
	proposedPlanCloseTag = "</proposed_plan>"
)

// planTag is the single tag kind tracked by [ProposedPlanParser].
type planTag int

const (
	planTagProposedPlan planTag = iota
)

// ProposedPlanSegmentKind discriminates the variants of [ProposedPlanSegment].
type ProposedPlanSegmentKind int

const (
	// ProposedPlanNormal is ordinary assistant text outside any plan block.
	ProposedPlanNormal ProposedPlanSegmentKind = iota
	// ProposedPlanStart marks the beginning of a "<proposed_plan>" block.
	ProposedPlanStart
	// ProposedPlanDelta is a chunk of text inside a plan block.
	ProposedPlanDelta
	// ProposedPlanEnd marks the end of a plan block.
	ProposedPlanEnd
)

// ProposedPlanSegment is one ordered output element from [ProposedPlanParser].
//
// For Normal and Delta segments, Text holds the associated text; for Start and
// End segments, Text is empty.
type ProposedPlanSegment struct {
	Kind ProposedPlanSegmentKind
	Text string
}

// NewProposedPlanNormal builds a Normal plan segment carrying the given text.
func NewProposedPlanNormal(text string) ProposedPlanSegment {
	return ProposedPlanSegment{Kind: ProposedPlanNormal, Text: text}
}

// NewProposedPlanStart builds a plan-block start segment.
func NewProposedPlanStart() ProposedPlanSegment {
	return ProposedPlanSegment{Kind: ProposedPlanStart}
}

// NewProposedPlanDelta builds a plan-block delta segment carrying the given text.
func NewProposedPlanDelta(text string) ProposedPlanSegment {
	return ProposedPlanSegment{Kind: ProposedPlanDelta, Text: text}
}

// NewProposedPlanEnd builds a plan-block end segment.
func NewProposedPlanEnd() ProposedPlanSegment {
	return ProposedPlanSegment{Kind: ProposedPlanEnd}
}

// ProposedPlanParser parses "<proposed_plan>" blocks emitted in plan mode.
//
// It implements [StreamTextParser] so callers can consume:
//   - VisibleText: normal assistant text with plan blocks removed.
//   - Extracted: ordered plan segments (includes Normal segments for ordering fidelity).
type ProposedPlanParser struct {
	parser *taggedLineParser[planTag]
}

// NewProposedPlanParser creates a ready-to-use proposed-plan parser.
func NewProposedPlanParser() *ProposedPlanParser {
	return &ProposedPlanParser{
		parser: newTaggedLineParser([]tagSpec[planTag]{{
			open:  proposedPlanOpenTag,
			close: proposedPlanCloseTag,
			tag:   planTagProposedPlan,
		}}),
	}
}

// PushStr feeds a new text chunk and returns visible text plus ordered plan
// segments.
func (p *ProposedPlanParser) PushStr(chunk string) StreamTextChunk[ProposedPlanSegment] {
	return mapPlanSegments(p.parser.parse(chunk))
}

// Finish flushes buffered state, closing any unterminated plan block.
func (p *ProposedPlanParser) Finish() StreamTextChunk[ProposedPlanSegment] {
	return mapPlanSegments(p.parser.finish())
}

// mapPlanSegments converts low-level tagged-line segments into
// [ProposedPlanSegment] values and accumulates the visible text from Normal
// segments. It never mutates its input.
func mapPlanSegments(segments []taggedLineSegment[planTag]) StreamTextChunk[ProposedPlanSegment] {
	out := StreamTextChunk[ProposedPlanSegment]{}
	for _, seg := range segments {
		var mapped ProposedPlanSegment
		switch seg.kind {
		case segNormal:
			mapped = NewProposedPlanNormal(seg.text)
		case segTagStart:
			mapped = NewProposedPlanStart()
		case segTagDelta:
			mapped = NewProposedPlanDelta(seg.text)
		case segTagEnd:
			mapped = NewProposedPlanEnd()
		}
		if mapped.Kind == ProposedPlanNormal {
			out.VisibleText += mapped.Text
		}
		out.Extracted = append(out.Extracted, mapped)
	}
	return out
}

// StripProposedPlanBlocks removes "<proposed_plan>" blocks from a complete
// string and returns the remaining visible text.
func StripProposedPlanBlocks(text string) string {
	parser := NewProposedPlanParser()
	out := parser.PushStr(text).VisibleText
	out += parser.Finish().VisibleText
	return out
}

// ExtractProposedPlanText returns the concatenated plan-block text from a
// complete string. The second return value is false when no plan block was
// present; when true, the first value is the plan text (possibly empty). If
// multiple plan blocks are present, only the last one's text is returned,
// matching the Rust implementation.
func ExtractProposedPlanText(text string) (string, bool) {
	parser := NewProposedPlanParser()
	planText := ""
	sawPlanBlock := false

	segments := parser.PushStr(text).Extracted
	segments = append(segments, parser.Finish().Extracted...)
	for _, seg := range segments {
		switch seg.Kind {
		case ProposedPlanStart:
			sawPlanBlock = true
			planText = ""
		case ProposedPlanDelta:
			planText += seg.Text
		case ProposedPlanEnd, ProposedPlanNormal:
			// no-op
		}
	}

	if !sawPlanBlock {
		return "", false
	}
	return planText, true
}
