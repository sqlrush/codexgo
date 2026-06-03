package applypatch

import (
	"fmt"
	"strings"
)

// ApplyPatchFileUpdate is the intended result of a file update, mirroring Rust
// `ApplyPatchFileUpdate`: the unified diff plus the original and resulting
// content.
type ApplyPatchFileUpdate struct {
	UnifiedDiff     string
	OriginalContent string
	Content         string
}

// UnifiedDiffFromChunks builds a unified diff (context radius 1) describing the
// effect of applying chunks to the file at pathAbs. It is a port of Rust
// `unified_diff_from_chunks`.
func UnifiedDiffFromChunks(pathAbs string, chunks []UpdateFileChunk, fsys FileSystem) (ApplyPatchFileUpdate, error) {
	return UnifiedDiffFromChunksWithContext(pathAbs, chunks, 1, fsys)
}

// UnifiedDiffFromChunksWithContext builds a unified diff with the given context
// radius. It is a port of Rust `unified_diff_from_chunks_with_context`.
func UnifiedDiffFromChunksWithContext(pathAbs string, chunks []UpdateFileChunk, context int, fsys FileSystem) (ApplyPatchFileUpdate, error) {
	applied, err := deriveNewContentsFromChunks(pathAbs, chunks, fsys)
	if err != nil {
		return ApplyPatchFileUpdate{}, err
	}
	diff := unifiedDiff(applied.originalContents, applied.newContents, context)
	return ApplyPatchFileUpdate{
		UnifiedDiff:     diff,
		OriginalContent: applied.originalContents,
		Content:         applied.newContents,
	}, nil
}

// opKind enumerates per-line diff operations.
type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

// lineOp is a single line-level diff operation. oldIndex/newIndex are 0-based
// indices into the respective line arrays (whichever applies for the kind).
type lineOp struct {
	kind     opKind
	oldIndex int
	newIndex int
}

// unifiedDiff computes a unified diff between original and updated text using a
// line-level Myers diff and `similar`-compatible formatting (context radius
// `context`). It mirrors the output of the Rust `similar` crate's
// `TextDiff::from_lines(...).unified_diff().context_radius(context).to_string()`.
func unifiedDiff(original, updated string, context int) string {
	oldLines := splitKeepingEnds(original)
	newLines := splitKeepingEnds(updated)

	ops := myersLineDiff(oldLines, newLines)
	groups := groupOps(ops, context)
	if len(groups) == 0 {
		return ""
	}

	var b strings.Builder
	for _, group := range groups {
		writeHunk(&b, group, oldLines, newLines)
	}
	return b.String()
}

// splitKeepingEnds splits text into lines, keeping the trailing newline on each
// line, matching `similar`'s line tokenization. A trailing newline does not
// produce a final empty line.
func splitKeepingEnds(text string) []string {
	if text == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// myersLineDiff computes a minimal edit script between two line slices using the
// classic Myers O(ND) algorithm, returning operations in source order.
func myersLineDiff(a, b []string) []lineOp {
	n, m := len(a), len(b)
	maxD := n + m
	if maxD == 0 {
		return nil
	}

	// trace records the furthest-reaching V array at each edit-distance d.
	offset := maxD
	v := make([]int, 2*maxD+1)
	var trace [][]int

	var found bool
	for d := 0; d <= maxD; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	return backtrack(trace, a, b, offset)
}

// backtrack reconstructs the edit script from the recorded Myers traces.
func backtrack(trace [][]int, a, b []string, offset int) []lineOp {
	n, m := len(a), len(b)
	x, y := n, m
	var rev []lineOp

	for d := len(trace) - 1; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[offset+prevK]
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			rev = append(rev, lineOp{kind: opEqual, oldIndex: x - 1, newIndex: y - 1})
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				rev = append(rev, lineOp{kind: opInsert, oldIndex: x, newIndex: y - 1})
			} else {
				rev = append(rev, lineOp{kind: opDelete, oldIndex: x - 1, newIndex: y})
			}
		}
		x, y = prevX, prevY
	}

	// Leading diagonal (d == 0).
	for x > 0 && y > 0 {
		rev = append(rev, lineOp{kind: opEqual, oldIndex: x - 1, newIndex: y - 1})
		x--
		y--
	}

	// Reverse into forward order.
	ops := make([]lineOp, len(rev))
	for i := range rev {
		ops[i] = rev[len(rev)-1-i]
	}
	return ops
}

// groupOps splits the full op list into hunks, each padded by up to `context`
// equal lines on either side of a run of changes, merging adjacent changes that
// fall within 2*context of each other. This mirrors `similar`'s grouped
// operations used by its unified diff writer.
func groupOps(ops []lineOp, context int) [][]lineOp {
	// Identify whether there are any changes at all.
	hasChange := false
	for _, op := range ops {
		if op.kind != opEqual {
			hasChange = true
			break
		}
	}
	if !hasChange {
		return nil
	}

	var groups [][]lineOp
	var current []lineOp

	flush := func() {
		if len(current) > 0 {
			groups = append(groups, current)
			current = nil
		}
	}

	i := 0
	for i < len(ops) {
		op := ops[i]
		if op.kind != opEqual {
			current = append(current, op)
			i++
			continue
		}

		// Count the run of equal ops.
		j := i
		for j < len(ops) && ops[j].kind == opEqual {
			j++
		}
		runLen := j - i

		switch {
		case len(current) == 0:
			// Leading equal run: keep only the trailing `context` lines as
			// pre-context for the next change (if any change follows).
			if j < len(ops) {
				keep := context
				if keep > runLen {
					keep = runLen
				}
				current = append(current, ops[j-keep:j]...)
			}
		case j >= len(ops):
			// Trailing equal run after the last change: keep up to `context`
			// lines, then end.
			keep := context
			if keep > runLen {
				keep = runLen
			}
			current = append(current, ops[i:i+keep]...)
			flush()
		case runLen <= 2*context:
			// Small gap between changes: keep all equal lines to merge hunks.
			current = append(current, ops[i:j]...)
		default:
			// Large gap: close the current hunk with `context` trailing lines,
			// and start the next hunk with `context` leading lines.
			current = append(current, ops[i:i+context]...)
			flush()
			current = append(current, ops[j-context:j]...)
		}
		i = j
	}
	flush()

	return groups
}

// writeHunk writes one unified-diff hunk for a group of operations.
func writeHunk(b *strings.Builder, group []lineOp, oldLines, newLines []string) {
	oldStart, oldLen, newStart, newLen := hunkRanges(group)

	b.WriteString("@@ -")
	b.WriteString(formatRange(oldStart, oldLen))
	b.WriteString(" +")
	b.WriteString(formatRange(newStart, newLen))
	b.WriteString(" @@\n")

	for _, op := range group {
		switch op.kind {
		case opEqual:
			writeLine(b, ' ', oldLines[op.oldIndex])
		case opDelete:
			writeLine(b, '-', oldLines[op.oldIndex])
		case opInsert:
			writeLine(b, '+', newLines[op.newIndex])
		}
	}
}

// hunkRanges computes the 1-based start lines and lengths for the old and new
// sides of a hunk.
func hunkRanges(group []lineOp) (oldStart, oldLen, newStart, newLen int) {
	oldStart, newStart = -1, -1
	for _, op := range group {
		switch op.kind {
		case opEqual:
			if oldStart < 0 {
				oldStart = op.oldIndex
			}
			if newStart < 0 {
				newStart = op.newIndex
			}
			oldLen++
			newLen++
		case opDelete:
			if oldStart < 0 {
				oldStart = op.oldIndex
			}
			oldLen++
		case opInsert:
			if newStart < 0 {
				newStart = op.newIndex
			}
			newLen++
		}
	}
	// Convert to 1-based. An empty side starts at 0.
	if oldLen == 0 {
		oldStart = 0
	} else {
		oldStart++
	}
	if newLen == 0 {
		newStart = 0
	} else {
		newStart++
	}
	return oldStart, oldLen, newStart, newLen
}

// formatRange renders a unified-diff range, omitting the length when it is 1,
// matching `similar` (and GNU diff) conventions.
func formatRange(start, length int) string {
	if length == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, length)
}

// writeLine writes a single diff line with the given prefix, ensuring the output
// line carries a trailing newline (the tokenized line already includes its own
// newline except possibly for a final line without one).
func writeLine(b *strings.Builder, prefix byte, line string) {
	b.WriteByte(prefix)
	b.WriteString(line)
	if !strings.HasSuffix(line, "\n") {
		b.WriteByte('\n')
	}
}
