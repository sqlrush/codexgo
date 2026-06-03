package template

// segmentKind distinguishes literal text from a placeholder reference within a
// parsed template.
type segmentKind int

const (
	// segmentLiteral is verbatim text emitted as-is during rendering.
	segmentLiteral segmentKind = iota
	// segmentPlaceholder is a variable reference resolved during rendering.
	segmentPlaceholder
)

// segment is one parsed piece of a template: either literal text or a named
// placeholder. It is unexported because callers interact with templates only
// through the Template type.
type segment struct {
	kind segmentKind
	// text holds the literal content for segmentLiteral, or the (trimmed)
	// placeholder name for segmentPlaceholder.
	text string
}

// appendLiteral appends literal text to segments, coalescing it with a trailing
// literal segment when possible. Empty input is ignored. The returned slice is
// the (possibly grown) segments slice; callers must use the return value, in
// keeping with append semantics. This mirrors the Rust push_literal helper.
func appendLiteral(segments []segment, literal string) []segment {
	if literal == "" {
		return segments
	}
	if n := len(segments); n > 0 && segments[n-1].kind == segmentLiteral {
		// Build a new segment value rather than mutating in place so the
		// operation has no hidden side effects on aliased slices.
		merged := segment{kind: segmentLiteral, text: segments[n-1].text + literal}
		out := make([]segment, n)
		copy(out, segments)
		out[n-1] = merged
		return out
	}
	return append(segments, segment{kind: segmentLiteral, text: literal})
}
