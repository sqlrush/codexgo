package streamparser

// AssistantTextChunk is the result of parsing one chunk of assistant text.
type AssistantTextChunk struct {
	// VisibleText is text safe to render immediately, with citation tags and (in
	// plan mode) plan blocks removed.
	VisibleText string
	// Citations holds citation bodies extracted from the chunk, in order.
	Citations []string
	// PlanSegments holds ordered plan segments. It is always empty when plan
	// mode is disabled.
	PlanSegments []ProposedPlanSegment
}

// IsEmpty reports whether no visible text, citations, or plan segments were
// produced.
func (c AssistantTextChunk) IsEmpty() bool {
	return c.VisibleText == "" && len(c.Citations) == 0 && len(c.PlanSegments) == 0
}

// AssistantTextStreamParser parses assistant text streaming markup in one pass:
//   - strips "<oai-mem-citation>" tags and extracts citation payloads;
//   - in plan mode, also strips "<proposed_plan>" blocks and emits plan segments.
//
// Citations are stripped first, then the resulting visible text is fed to the
// plan parser, so a citation split across a plan-block boundary is handled
// correctly.
type AssistantTextStreamParser struct {
	planMode  bool
	citations *CitationStreamParser
	plan      *ProposedPlanParser
}

// NewAssistantTextStreamParser creates a parser. When planMode is false, plan
// blocks are left untouched and PlanSegments is always empty.
func NewAssistantTextStreamParser(planMode bool) *AssistantTextStreamParser {
	return &AssistantTextStreamParser{
		planMode:  planMode,
		citations: NewCitationStreamParser(),
		plan:      NewProposedPlanParser(),
	}
}

// PushStr feeds a new chunk of assistant text.
func (a *AssistantTextStreamParser) PushStr(chunk string) AssistantTextChunk {
	citationChunk := a.citations.PushStr(chunk)
	out := a.parseVisibleText(citationChunk.VisibleText)
	out.Citations = citationChunk.Extracted
	return out
}

// Finish flushes buffered state from both the citation and plan parsers.
func (a *AssistantTextStreamParser) Finish() AssistantTextChunk {
	citationChunk := a.citations.Finish()
	out := a.parseVisibleText(citationChunk.VisibleText)
	if a.planMode {
		tail := a.plan.Finish()
		tailChunk := AssistantTextChunk{
			VisibleText:  tail.VisibleText,
			PlanSegments: tail.Extracted,
		}
		if !tailChunk.IsEmpty() {
			out.VisibleText += tailChunk.VisibleText
			out.PlanSegments = append(out.PlanSegments, tailChunk.PlanSegments...)
		}
	}
	out.Citations = citationChunk.Extracted
	return out
}

// parseVisibleText routes citation-stripped text through the plan parser when in
// plan mode, or returns it directly otherwise.
func (a *AssistantTextStreamParser) parseVisibleText(visibleText string) AssistantTextChunk {
	if !a.planMode {
		return AssistantTextChunk{VisibleText: visibleText}
	}
	planChunk := a.plan.PushStr(visibleText)
	return AssistantTextChunk{
		VisibleText:  planChunk.VisibleText,
		PlanSegments: planChunk.Extracted,
	}
}
