// Package streamparser provides small, dependency-free utilities for parsing
// streamed text incrementally.
//
// It is a faithful Go port of the codex-utils-stream-parser Rust crate. Some
// model outputs arrive as a stream and may contain hidden markup (for example
// "<oai-mem-citation>...</oai-mem-citation>") split across chunk boundaries.
// Parsing each chunk independently is incorrect because tags can be split (for
// example "<oai-mem-" followed by "citation>"). These parsers keep state across
// chunks, return visible text safe to render immediately, and extract hidden
// payloads separately.
//
// The package provides:
//
//   - [StreamTextParser]: interface for incremental parsers that consume string chunks.
//   - [InlineHiddenTagParser]: generic parser that hides inline tags and extracts contents.
//   - [CitationStreamParser]: convenience wrapper for "<oai-mem-citation>...</oai-mem-citation>".
//   - [StripCitations]: one-shot helper for non-streamed strings.
//   - [Utf8StreamParser]: adapter for raw byte streams that may split UTF-8 code points.
//   - [ProposedPlanParser]: parser for "<proposed_plan>" blocks emitted in plan mode.
//   - [AssistantTextStreamParser]: combined citation + plan parser for assistant text.
//
// Known limitations (matching the Rust implementation):
//   - Tags are matched literally and case-sensitively.
//   - No nested tag support.
//   - A stream call can return empty results.
//
// All externally observable formats are preserved exactly so that callers relying
// on parser output behave identically across the two implementations.
package streamparser

// StreamTextChunk is the incremental parser result for one pushed chunk (or
// final flush).
//
// The type parameter T is the kind of payload extracted by the parser (for
// example a citation body string, or an [ExtractedInlineTag]).
type StreamTextChunk[T any] struct {
	// VisibleText is text safe to render immediately.
	VisibleText string
	// Extracted holds hidden payloads extracted from the chunk.
	Extracted []T
}

// IsEmpty reports whether no visible text and no extracted payloads were
// produced.
func (c StreamTextChunk[T]) IsEmpty() bool {
	return c.VisibleText == "" && len(c.Extracted) == 0
}

// StreamTextParser is implemented by parsers that consume streamed text and emit
// visible text plus extracted payloads of type T.
//
// Implementations are stateful: PushStr may buffer partial input across calls,
// and Finish flushes any buffered state at end-of-stream (or end-of-item).
type StreamTextParser[T any] interface {
	// PushStr feeds a new text chunk and returns the visible text and payloads
	// that can be emitted given the parser's current state.
	PushStr(chunk string) StreamTextChunk[T]
	// Finish flushes any buffered state at end-of-stream (or end-of-item).
	Finish() StreamTextChunk[T]
}
