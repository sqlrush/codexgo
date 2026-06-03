package streamparser

import "strings"

// ExtractedInlineTag is one hidden inline tag extracted by [InlineHiddenTagParser].
type ExtractedInlineTag[T comparable] struct {
	Tag     T
	Content string
}

// InlineTagSpec is a literal tag specification used by [InlineHiddenTagParser].
//
// Open and Close are matched literally and case-sensitively. Both must be
// non-empty.
type InlineTagSpec[T comparable] struct {
	Tag   T
	Open  string
	Close string
}

// activeTag tracks the currently open tag while its body is being buffered.
type activeTag[T comparable] struct {
	tag     T
	close   string
	content string
}

// InlineHiddenTagParser is a generic streaming parser that hides configured
// inline tags and extracts their contents.
//
// Example:
//   - input: "hello <oai-mem-citation>doc A</oai-mem-citation> world"
//   - visible output: "hello  world"
//   - extracted: ["doc A"]
//
// Matching is literal and non-nested. If EOF is reached while a tag is still
// open, the parser auto-closes it and returns the buffered content as extracted
// data.
type InlineHiddenTagParser[T comparable] struct {
	specs   []InlineTagSpec[T]
	pending string
	active  *activeTag[T]
}

// NewInlineHiddenTagParser creates a parser for one or more hidden inline tags.
//
// It panics if specs is empty or if any spec has an empty Open or Close
// delimiter, mirroring the assertions in the Rust implementation. The provided
// slice is defensively copied so the caller's slice is never mutated.
func NewInlineHiddenTagParser[T comparable](specs []InlineTagSpec[T]) *InlineHiddenTagParser[T] {
	if len(specs) == 0 {
		panic("InlineHiddenTagParser requires at least one tag spec")
	}
	for _, spec := range specs {
		if spec.Open == "" {
			panic("InlineHiddenTagParser requires non-empty open delimiters")
		}
		if spec.Close == "" {
			panic("InlineHiddenTagParser requires non-empty close delimiters")
		}
	}
	copied := make([]InlineTagSpec[T], len(specs))
	copy(copied, specs)
	return &InlineHiddenTagParser[T]{specs: copied}
}

// findNextOpen returns the byte offset of the earliest opening delimiter found
// in the pending buffer and the index of the matching spec, along with whether a
// match was found.
//
// Ties are broken to match the Rust ordering: earliest position first; then the
// longest opener; then the lowest spec index.
func (p *InlineHiddenTagParser[T]) findNextOpen() (pos int, specIdx int, found bool) {
	bestPos := -1
	bestLen := 0
	bestIdx := -1
	for idx, spec := range p.specs {
		at := strings.Index(p.pending, spec.Open)
		if at < 0 {
			continue
		}
		candLen := len(spec.Open)
		if bestPos == -1 || at < bestPos ||
			(at == bestPos && candLen > bestLen) ||
			(at == bestPos && candLen == bestLen && idx < bestIdx) {
			bestPos = at
			bestLen = candLen
			bestIdx = idx
		}
	}
	if bestPos == -1 {
		return 0, 0, false
	}
	return bestPos, bestIdx, true
}

// maxOpenPrefixSuffixLen returns the longest number of trailing bytes of the
// pending buffer that form a prefix of any opening delimiter. These bytes must
// be kept buffered because they might complete an opener on the next chunk.
func (p *InlineHiddenTagParser[T]) maxOpenPrefixSuffixLen() int {
	max := 0
	for _, spec := range p.specs {
		if l := longestSuffixPrefixLen(p.pending, spec.Open); l > max {
			max = l
		}
	}
	return max
}

// drainVisibleToSuffixMatch emits as visible text everything in the pending
// buffer except the trailing keepSuffixLen bytes, then drops the emitted prefix.
func (p *InlineHiddenTagParser[T]) drainVisibleToSuffixMatch(out *StreamTextChunk[ExtractedInlineTag[T]], keepSuffixLen int) {
	take := len(p.pending) - keepSuffixLen
	if take <= 0 {
		return
	}
	if prefix := p.pending[:take]; prefix != "" {
		out.VisibleText += prefix
	}
	p.pending = p.pending[take:]
}

// PushStr feeds a new text chunk, returning visible text plus any extracted tags
// that can be emitted given the parser's current state. Bytes that might be part
// of a split delimiter are buffered for the next call.
func (p *InlineHiddenTagParser[T]) PushStr(chunk string) StreamTextChunk[ExtractedInlineTag[T]] {
	p.pending += chunk
	out := StreamTextChunk[ExtractedInlineTag[T]]{}

	for {
		if p.active != nil {
			closeTag := p.active.close
			if closeIdx := strings.Index(p.pending, closeTag); closeIdx >= 0 {
				active := p.active
				p.active = nil
				active.content += p.pending[:closeIdx]
				out.Extracted = append(out.Extracted, ExtractedInlineTag[T]{
					Tag:     active.tag,
					Content: active.content,
				})
				p.pending = p.pending[closeIdx+len(closeTag):]
				continue
			}

			keep := longestSuffixPrefixLen(p.pending, closeTag)
			take := len(p.pending) - keep
			if take > 0 {
				p.active.content += p.pending[:take]
				p.pending = p.pending[take:]
			}
			break
		}

		if openIdx, specIdx, found := p.findNextOpen(); found {
			if prefix := p.pending[:openIdx]; prefix != "" {
				out.VisibleText += prefix
			}
			spec := p.specs[specIdx]
			p.pending = p.pending[openIdx+len(spec.Open):]
			p.active = &activeTag[T]{
				tag:   spec.Tag,
				close: spec.Close,
			}
			continue
		}

		keep := p.maxOpenPrefixSuffixLen()
		p.drainVisibleToSuffixMatch(&out, keep)
		break
	}

	return out
}

// Finish flushes any buffered state at end-of-stream. If a tag is still open its
// buffered content is auto-closed and returned as extracted data; otherwise any
// remaining pending bytes are emitted as visible text.
func (p *InlineHiddenTagParser[T]) Finish() StreamTextChunk[ExtractedInlineTag[T]] {
	out := StreamTextChunk[ExtractedInlineTag[T]]{}

	if p.active != nil {
		active := p.active
		p.active = nil
		if p.pending != "" {
			active.content += p.pending
			p.pending = ""
		}
		out.Extracted = append(out.Extracted, ExtractedInlineTag[T]{
			Tag:     active.tag,
			Content: active.content,
		})
		return out
	}

	if p.pending != "" {
		out.VisibleText += p.pending
		p.pending = ""
	}

	return out
}

// longestSuffixPrefixLen returns the length, in bytes, of the longest non-empty
// suffix of s that is also a prefix of needle, considering only lengths k where
// needle[:k] ends on a UTF-8 character boundary of needle. A return value of 0
// means there is no such overlap.
//
// This mirrors the Rust helper, where suffix lengths are capped at len(needle)-1
// and rejected unless they fall on a char boundary of needle.
func longestSuffixPrefixLen(s, needle string) int {
	max := len(s)
	if cap := len(needle) - 1; cap < max {
		max = cap
	}
	for k := max; k >= 1; k-- {
		if isCharBoundary(needle, k) && strings.HasSuffix(s, needle[:k]) {
			return k
		}
	}
	return 0
}

// isCharBoundary reports whether index i lies on a UTF-8 character boundary of
// s. Indices 0 and len(s) are always boundaries. An interior index is a
// boundary when the byte at that position is not a UTF-8 continuation byte
// (0b10xxxxxx). This matches Rust's str::is_char_boundary.
func isCharBoundary(s string, i int) bool {
	if i == 0 || i == len(s) {
		return true
	}
	if i < 0 || i > len(s) {
		return false
	}
	return s[i]&0xC0 != 0x80
}
