package tui

import "strings"

// MarkdownStreamCollector buffers streaming markdown deltas and exposes commit
// boundaries at newlines. It is a faithful port of
// codex-rs/tui/src/markdown_stream.rs `MarkdownStreamCollector`.
//
// The collector does not parse markdown; it only defines stable source
// boundaries. The stream controller re-renders the entire accumulated source on
// each commit so width changes (resize) can reflow from one source string while
// only appending newly completed content. This newline-gating prevents the live
// stream from rendering an incomplete markdown block whose meaning could still
// change when the rest of the line arrives.
//
// The collector follows the immutability rule loosely for performance — it is a
// mutable accumulator by design (mirroring the Rust struct), but it is owned by
// a single stream and never shared.
type MarkdownStreamCollector struct {
	buffer             strings.Builder
	committedSourceLen int
}

// NewMarkdownStreamCollector creates an empty collector.
func NewMarkdownStreamCollector() *MarkdownStreamCollector {
	return &MarkdownStreamCollector{}
}

// Clear resets all buffered source and commit bookkeeping.
func (c *MarkdownStreamCollector) Clear() {
	c.buffer.Reset()
	c.committedSourceLen = 0
}

// PushDelta appends a raw streaming delta to the internal source buffer.
func (c *MarkdownStreamCollector) PushDelta(delta string) {
	c.buffer.WriteString(delta)
}

// CommitCompleteSource returns the newly completed raw markdown source up to the
// last newline, or ("", false) when nothing new has completed. Calling it after
// a delta without a newline returns ("", false), which prevents the live stream
// from committing an incomplete markdown block.
//
// Port of MarkdownStreamCollector::commit_complete_source.
func (c *MarkdownStreamCollector) CommitCompleteSource() (string, bool) {
	buf := c.buffer.String()
	idx := strings.LastIndexByte(buf, '\n')
	if idx < 0 {
		return "", false
	}
	commitEnd := idx + 1
	if commitEnd <= c.committedSourceLen {
		return "", false
	}
	out := buf[c.committedSourceLen:commitEnd]
	c.committedSourceLen = commitEnd
	return out, true
}

// FinalizeAndDrainSource flushes whatever remains (the last line, which may lack
// a trailing newline) and clears the collector. The returned chunk is
// newline-terminated when non-empty so callers can safely run block parsing on
// it.
//
// Port of MarkdownStreamCollector::finalize_and_drain_source.
func (c *MarkdownStreamCollector) FinalizeAndDrainSource() string {
	buf := c.buffer.String()
	if c.committedSourceLen >= len(buf) {
		c.Clear()
		return ""
	}
	out := buf[c.committedSourceLen:]
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	c.Clear()
	return out
}

// CommittedSource returns the full source committed so far (everything up to and
// including the last committed newline). Stream controllers re-render this to
// reflow committed content on resize.
func (c *MarkdownStreamCollector) CommittedSource() string {
	return c.buffer.String()[:c.committedSourceLen]
}
