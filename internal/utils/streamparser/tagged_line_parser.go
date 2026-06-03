package streamparser

import (
	"strings"
	"unicode"
)

// Line-based tag block parsing for streamed text.
//
// The parser buffers each line until it can disprove that the line is a tag,
// which is required for tags that must appear alone on a line.

// tagSpec describes a line-tag block: an open delimiter line, a close delimiter
// line, and the associated tag value.
type tagSpec[T comparable] struct {
	open  string
	close string
	tag   T
}

// taggedLineSegmentKind discriminates the variants of [taggedLineSegment].
type taggedLineSegmentKind int

const (
	segNormal taggedLineSegmentKind = iota
	segTagStart
	segTagDelta
	segTagEnd
)

// taggedLineSegment is one ordered output element of [taggedLineParser]: either
// normal text, the start/end of a tag block, or a delta of text inside a block.
type taggedLineSegment[T comparable] struct {
	kind taggedLineSegmentKind
	tag  T
	text string
}

// taggedLineParser is a stateful line parser that splits input into normal text
// versus tag blocks.
type taggedLineParser[T comparable] struct {
	specs      []tagSpec[T]
	activeTag  *T
	detectTag  bool
	lineBuffer string
}

// newTaggedLineParser creates a parser for the given line-tag specs. The slice
// is defensively copied so the caller's slice is never mutated.
func newTaggedLineParser[T comparable](specs []tagSpec[T]) *taggedLineParser[T] {
	copied := make([]tagSpec[T], len(specs))
	copy(copied, specs)
	return &taggedLineParser[T]{
		specs:     copied,
		detectTag: true,
	}
}

// parse consumes a delta of streamed text and returns the segments that can be
// emitted given the parser's current state. Partial lines are buffered until a
// newline arrives or the line is proven not to be a tag.
func (p *taggedLineParser[T]) parse(delta string) []taggedLineSegment[T] {
	var segments []taggedLineSegment[T]
	var run strings.Builder

	for _, ch := range delta {
		if p.detectTag {
			if run.Len() > 0 {
				p.pushText(run.String(), &segments)
				run.Reset()
			}
			p.lineBuffer += string(ch)
			if ch == '\n' {
				p.finishLine(&segments)
				continue
			}
			slug := trimStart(p.lineBuffer)
			if slug == "" || p.isTagPrefix(slug) {
				continue
			}
			buffered := p.lineBuffer
			p.lineBuffer = ""
			p.detectTag = false
			p.pushText(buffered, &segments)
			continue
		}

		run.WriteRune(ch)
		if ch == '\n' {
			p.pushText(run.String(), &segments)
			run.Reset()
			p.detectTag = true
		}
	}

	if run.Len() > 0 {
		p.pushText(run.String(), &segments)
	}

	return segments
}

// finish flushes any buffered line at end-of-stream, deciding whether the
// trailing line is a tag delimiter or plain text, and auto-closes an open block.
func (p *taggedLineParser[T]) finish() []taggedLineSegment[T] {
	var segments []taggedLineSegment[T]
	if p.lineBuffer != "" {
		buffered := p.lineBuffer
		p.lineBuffer = ""
		withoutNewline := strings.TrimSuffix(buffered, "\n")
		slug := trimEnd(trimStart(withoutNewline))

		if tag, ok := p.matchOpen(slug); ok && p.activeTag == nil {
			pushSegment(&segments, taggedLineSegment[T]{kind: segTagStart, tag: tag})
			t := tag
			p.activeTag = &t
		} else if tag, ok := p.matchClose(slug); ok && p.activeTag != nil && *p.activeTag == tag {
			pushSegment(&segments, taggedLineSegment[T]{kind: segTagEnd, tag: tag})
			p.activeTag = nil
		} else {
			p.pushText(buffered, &segments)
		}
	}
	if p.activeTag != nil {
		tag := *p.activeTag
		p.activeTag = nil
		pushSegment(&segments, taggedLineSegment[T]{kind: segTagEnd, tag: tag})
	}
	p.detectTag = true
	return segments
}

// finishLine handles a completed line (one ending in '\n') during streaming.
func (p *taggedLineParser[T]) finishLine(segments *[]taggedLineSegment[T]) {
	line := p.lineBuffer
	p.lineBuffer = ""
	withoutNewline := strings.TrimSuffix(line, "\n")
	slug := trimEnd(trimStart(withoutNewline))

	if tag, ok := p.matchOpen(slug); ok && p.activeTag == nil {
		pushSegment(segments, taggedLineSegment[T]{kind: segTagStart, tag: tag})
		t := tag
		p.activeTag = &t
		p.detectTag = true
		return
	}

	if tag, ok := p.matchClose(slug); ok && p.activeTag != nil && *p.activeTag == tag {
		pushSegment(segments, taggedLineSegment[T]{kind: segTagEnd, tag: tag})
		p.activeTag = nil
		p.detectTag = true
		return
	}

	p.detectTag = true
	p.pushText(line, segments)
}

// pushText emits text either as normal text or as a delta inside the active tag
// block, depending on whether a block is currently open.
func (p *taggedLineParser[T]) pushText(text string, segments *[]taggedLineSegment[T]) {
	if p.activeTag != nil {
		pushSegment(segments, taggedLineSegment[T]{kind: segTagDelta, tag: *p.activeTag, text: text})
	} else {
		pushSegment(segments, taggedLineSegment[T]{kind: segNormal, text: text})
	}
}

// isTagPrefix reports whether slug could still grow into a tag delimiter, i.e.
// it is a prefix of some open or close delimiter.
func (p *taggedLineParser[T]) isTagPrefix(slug string) bool {
	slug = trimEnd(slug)
	for _, spec := range p.specs {
		if strings.HasPrefix(spec.open, slug) || strings.HasPrefix(spec.close, slug) {
			return true
		}
	}
	return false
}

// matchOpen returns the tag whose open delimiter equals slug, if any.
func (p *taggedLineParser[T]) matchOpen(slug string) (T, bool) {
	for _, spec := range p.specs {
		if spec.open == slug {
			return spec.tag, true
		}
	}
	var zero T
	return zero, false
}

// matchClose returns the tag whose close delimiter equals slug, if any.
func (p *taggedLineParser[T]) matchClose(slug string) (T, bool) {
	for _, spec := range p.specs {
		if spec.close == slug {
			return spec.tag, true
		}
	}
	var zero T
	return zero, false
}

// pushSegment appends a segment, coalescing consecutive Normal segments and
// consecutive TagDelta segments of the same tag, and dropping empty text
// segments. This mirrors the Rust push_segment behavior exactly.
func pushSegment[T comparable](segments *[]taggedLineSegment[T], seg taggedLineSegment[T]) {
	switch seg.kind {
	case segNormal:
		if seg.text == "" {
			return
		}
		if n := len(*segments); n > 0 && (*segments)[n-1].kind == segNormal {
			(*segments)[n-1].text += seg.text
			return
		}
		*segments = append(*segments, seg)
	case segTagDelta:
		if seg.text == "" {
			return
		}
		if n := len(*segments); n > 0 {
			last := &(*segments)[n-1]
			if last.kind == segTagDelta && last.tag == seg.tag {
				last.text += seg.text
				return
			}
		}
		*segments = append(*segments, seg)
	default:
		*segments = append(*segments, seg)
	}
}

// trimStart removes leading Unicode whitespace, matching Rust's str::trim_start
// (which uses char::is_whitespace, the Unicode White_Space property).
func trimStart(s string) string {
	return strings.TrimLeftFunc(s, unicode.IsSpace)
}

// trimEnd removes trailing Unicode whitespace, matching Rust's str::trim_end.
func trimEnd(s string) string {
	return strings.TrimRightFunc(s, unicode.IsSpace)
}
