package applypatch

import (
	"fmt"
	"sort"
	"strings"
)

// appliedContents holds the original and resulting file contents for an update
// hunk, mirroring Rust `AppliedPatch`.
type appliedContents struct {
	originalContents string
	newContents      string
}

// deriveNewContentsFromChunks reads the file at pathAbs and returns its original
// contents alongside the contents that result from applying chunks. It is a port
// of Rust `derive_new_contents_from_chunks`.
func deriveNewContentsFromChunks(pathAbs string, chunks []UpdateFileChunk, fsys FileSystem) (appliedContents, error) {
	original, err := fsys.ReadFileText(pathAbs)
	if err != nil {
		return appliedContents{}, &IOError{
			Context: fmt.Sprintf("Failed to read file to update %s", displayPath(pathAbs)),
			Err:     err,
		}
	}

	originalLines := strings.Split(original, "\n")
	// Drop the trailing empty element from the final newline so line counts match
	// standard diff behaviour.
	if n := len(originalLines); n > 0 && originalLines[n-1] == "" {
		originalLines = originalLines[:n-1]
	}

	replacements, err := computeReplacements(originalLines, pathAbs, chunks)
	if err != nil {
		return appliedContents{}, err
	}
	newLines := applyReplacements(originalLines, replacements)
	if n := len(newLines); n == 0 || newLines[n-1] != "" {
		newLines = append(newLines, "")
	}
	return appliedContents{
		originalContents: original,
		newContents:      strings.Join(newLines, "\n"),
	}, nil
}

// replacement is a scheduled edit: replace oldLen lines beginning at startIndex
// with newLines. It mirrors the Rust `(usize, usize, Vec<String>)` tuple.
type replacement struct {
	startIndex int
	oldLen     int
	newLines   []string
}

// computeReplacements computes the list of replacements needed to transform
// originalLines per the patch chunks. It is a port of Rust
// `compute_replacements`.
func computeReplacements(originalLines []string, path string, chunks []UpdateFileChunk) ([]replacement, error) {
	var replacements []replacement
	lineIndex := 0

	for _, chunk := range chunks {
		// If a chunk has a change context, locate it and continue from there.
		if chunk.HasChangeContext {
			idx, ok := seekSequence(originalLines, []string{chunk.ChangeContext}, lineIndex, false)
			if !ok {
				return nil, &ComputeReplacementsError{
					Message: fmt.Sprintf(
						"Failed to find context '%s' in %s",
						chunk.ChangeContext, displayPath(path),
					),
				}
			}
			lineIndex = idx + 1
		}

		if len(chunk.OldLines) == 0 {
			// Pure addition: insert at end, or just before the final empty line
			// if one exists.
			insertionIdx := len(originalLines)
			if n := len(originalLines); n > 0 && originalLines[n-1] == "" {
				insertionIdx = n - 1
			}
			replacements = append(replacements, replacement{
				startIndex: insertionIdx,
				oldLen:     0,
				newLines:   cloneStrings(chunk.NewLines),
			})
			continue
		}

		// Try to locate old_lines verbatim. If that fails and the pattern ends
		// with an empty string (representing the file's terminating newline,
		// which is stripped from originalLines), retry without that final
		// element.
		pattern := chunk.OldLines
		newSlice := chunk.NewLines
		found, ok := seekSequence(originalLines, pattern, lineIndex, chunk.IsEndOfFile)

		if !ok && len(pattern) > 0 && pattern[len(pattern)-1] == "" {
			pattern = pattern[:len(pattern)-1]
			if len(newSlice) > 0 && newSlice[len(newSlice)-1] == "" {
				newSlice = newSlice[:len(newSlice)-1]
			}
			found, ok = seekSequence(originalLines, pattern, lineIndex, chunk.IsEndOfFile)
		}

		if ok {
			replacements = append(replacements, replacement{
				startIndex: found,
				oldLen:     len(pattern),
				newLines:   cloneStrings(newSlice),
			})
			lineIndex = found + len(pattern)
		} else {
			return nil, &ComputeReplacementsError{
				Message: fmt.Sprintf(
					"Failed to find expected lines in %s:\n%s",
					displayPath(path), strings.Join(chunk.OldLines, "\n"),
				),
			}
		}
	}

	// Stable sort by start index, matching Rust's `sort_by_key`.
	sort.SliceStable(replacements, func(i, j int) bool {
		return replacements[i].startIndex < replacements[j].startIndex
	})

	return replacements, nil
}

// applyReplacements applies the replacements to lines, returning the modified
// lines. It is a port of Rust `apply_replacements`. The input slice is not
// mutated; a copy is edited and returned.
func applyReplacements(lines []string, replacements []replacement) []string {
	out := cloneStrings(lines)

	// Apply in descending order so earlier replacements don't shift later ones.
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		startIdx := r.startIndex

		// Remove old lines.
		for k := 0; k < r.oldLen; k++ {
			if startIdx < len(out) {
				out = append(out[:startIdx], out[startIdx+1:]...)
			}
		}

		// Insert new lines.
		for offset, newLine := range r.newLines {
			pos := startIdx + offset
			out = append(out, "")
			copy(out[pos+1:], out[pos:])
			out[pos] = newLine
		}
	}

	return out
}

// cloneStrings returns a shallow copy of s. A nil input yields nil to mirror
// Rust's handling of empty vectors.
func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}
