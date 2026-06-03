package streamparser

// citationTag is the single tag kind tracked by [CitationStreamParser].
type citationTag int

const (
	citationTagCitation citationTag = iota
)

const (
	citationOpen  = "<oai-mem-citation>"
	citationClose = "</oai-mem-citation>"
)

// CitationStreamParser is a stream parser for
// "<oai-mem-citation>...</oai-mem-citation>" tags.
//
// It is a thin convenience wrapper around [InlineHiddenTagParser]. It returns
// citation bodies as plain strings and omits the citation tags from visible
// text.
//
// Matching is literal and non-nested. If EOF is reached before a closing
// "</oai-mem-citation>", the parser auto-closes the tag and returns the buffered
// body as an extracted citation.
type CitationStreamParser struct {
	inner *InlineHiddenTagParser[citationTag]
}

// NewCitationStreamParser creates a ready-to-use citation parser.
func NewCitationStreamParser() *CitationStreamParser {
	return &CitationStreamParser{
		inner: NewInlineHiddenTagParser([]InlineTagSpec[citationTag]{{
			Tag:   citationTagCitation,
			Open:  citationOpen,
			Close: citationClose,
		}}),
	}
}

// PushStr feeds a new text chunk and returns visible text plus any complete
// citation bodies extracted from it.
func (c *CitationStreamParser) PushStr(chunk string) StreamTextChunk[string] {
	inner := c.inner.PushStr(chunk)
	return mapExtractedToContent(inner)
}

// Finish flushes buffered state, auto-closing any unterminated citation tag.
func (c *CitationStreamParser) Finish() StreamTextChunk[string] {
	inner := c.inner.Finish()
	return mapExtractedToContent(inner)
}

// mapExtractedToContent converts extracted inline tags into their string bodies,
// preserving order. It allocates a new slice and never mutates the input.
func mapExtractedToContent(in StreamTextChunk[ExtractedInlineTag[citationTag]]) StreamTextChunk[string] {
	out := StreamTextChunk[string]{VisibleText: in.VisibleText}
	if len(in.Extracted) > 0 {
		out.Extracted = make([]string, 0, len(in.Extracted))
		for _, tag := range in.Extracted {
			out.Extracted = append(out.Extracted, tag.Content)
		}
	}
	return out
}

// StripCitations removes citation tags from a complete string and returns the
// visible text together with the extracted citation bodies in order.
//
// It uses [CitationStreamParser] internally, so it inherits the same semantics:
// literal, non-nested matching and auto-closing unterminated citations at EOF.
func StripCitations(text string) (visible string, citations []string) {
	parser := NewCitationStreamParser()
	out := parser.PushStr(text)
	tail := parser.Finish()
	out.VisibleText += tail.VisibleText
	out.Extracted = append(out.Extracted, tail.Extracted...)
	return out.VisibleText, out.Extracted
}
