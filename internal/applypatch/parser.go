package applypatch

import (
	"fmt"
	"strings"
	"unicode"
)

// ParseResult is the result of parsing a patch envelope. It mirrors the subset
// of the Rust `ApplyPatchArgs` struct that the parser produces: the parsed
// hunks, the canonical patch text (with any heredoc wrapper stripped), and an
// optional environment id from the `*** Environment ID:` preamble.
//
// Workdir is always nil here (it is populated elsewhere in Codex), retained for
// fidelity with `ApplyPatchArgs`.
type ParseResult struct {
	Patch         string
	Hunks         []Hunk
	Workdir       *string
	EnvironmentID *string
}

// ParsePatch parses a patch envelope using lenient mode, matching the default
// behaviour of Codex's `parse_patch` (Codex runs in lenient mode for all models
// because gpt-4.1 is known to require it).
//
// Lenient mode additionally tolerates a surrounding heredoc wrapper of the form
// `<<EOF` / `<<'EOF'` / `<<"EOF"` on the first line and a line ending in `EOF`
// on the last line; when detected, the wrapper is stripped before parsing.
func ParsePatch(patch string) (*ParseResult, error) {
	return parsePatchText(patch, parseModeLenient)
}

// parseMode selects strict vs lenient envelope-boundary handling, mirroring the
// Rust `ParseMode` enum.
type parseMode int

const (
	parseModeStrict parseMode = iota
	parseModeLenient
)

// parsePatchText is the core parser, mirroring Rust `parse_patch_text`.
func parsePatchText(patch string, mode parseMode) (*ParseResult, error) {
	lines := splitLines(strings.TrimSpace(patch))

	var patchLines, hunkLines []string
	var err error
	switch mode {
	case parseModeStrict:
		patchLines, hunkLines, err = checkPatchBoundariesStrict(lines)
	default:
		patchLines, hunkLines, err = checkPatchBoundariesLenient(lines)
	}
	if err != nil {
		return nil, err
	}

	environmentID, remaining, lineNumber, err := parseEnvironmentIDPreamble(hunkLines)
	if err != nil {
		return nil, err
	}

	var hunks []Hunk
	for len(remaining) > 0 {
		hunk, consumed, err := parseOneHunk(remaining, lineNumber)
		if err != nil {
			return nil, err
		}
		hunks = append(hunks, hunk)
		lineNumber += consumed
		remaining = remaining[consumed:]
	}

	return &ParseResult{
		Patch:         strings.Join(patchLines, "\n"),
		Hunks:         hunks,
		Workdir:       nil,
		EnvironmentID: environmentID,
	}, nil
}

// splitLines mirrors Rust's `str::lines`: it splits on '\n', treats a trailing
// "\r\n" as a single line ending, and does NOT yield a trailing empty line for
// input that ends in a newline. An empty string yields no lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	// A trailing newline produces a final empty element in Go's Split; Rust's
	// `lines` omits it.
	if n := len(raw); n > 0 && raw[n-1] == "" {
		raw = raw[:n-1]
	}
	for i, line := range raw {
		raw[i] = strings.TrimSuffix(line, "\r")
	}
	return raw
}

// parseEnvironmentIDPreamble mirrors Rust `parse_environment_id_preamble`. It
// returns the parsed environment id (or nil), the remaining hunk lines, and the
// 1-based line number at which hunk parsing should begin.
func parseEnvironmentIDPreamble(hunkLines []string) (*string, []string, int, error) {
	if len(hunkLines) == 0 {
		return nil, hunkLines, 2, nil
	}
	trimmedStart := strings.TrimLeftFunc(hunkLines[0], unicode.IsSpace)
	rest, ok := strings.CutPrefix(trimmedStart, environmentIDMarker)
	if !ok {
		return nil, hunkLines, 2, nil
	}
	environmentID := strings.TrimSpace(rest)
	if environmentID == "" {
		return nil, nil, 0, newInvalidPatchError("apply_patch environment_id cannot be empty")
	}
	return &environmentID, hunkLines[1:], 3, nil
}

// checkPatchBoundariesStrict mirrors Rust `check_patch_boundaries_strict`. It
// validates the begin/end markers and returns (allLines, innerHunkLines).
func checkPatchBoundariesStrict(lines []string) ([]string, []string, error) {
	var first, last *string
	switch len(lines) {
	case 0:
		// first and last remain nil.
	case 1:
		first, last = &lines[0], &lines[0]
	default:
		first, last = &lines[0], &lines[len(lines)-1]
	}
	if err := checkStartAndEndLinesStrict(first, last); err != nil {
		return nil, nil, err
	}
	return lines, lines[1 : len(lines)-1], nil
}

// checkPatchBoundariesLenient mirrors Rust `check_patch_boundaries_lenient`. It
// first tries strict parsing; failing that, it strips a recognized heredoc
// wrapper and retries strictly on the inner lines.
func checkPatchBoundariesLenient(originalLines []string) ([]string, []string, error) {
	patchLines, hunkLines, strictErr := checkPatchBoundariesStrict(originalLines)
	if strictErr == nil {
		return patchLines, hunkLines, nil
	}

	if len(originalLines) >= 2 {
		first := originalLines[0]
		last := originalLines[len(originalLines)-1]
		if (first == "<<EOF" || first == "<<'EOF'" || first == "<<\"EOF\"") &&
			strings.HasSuffix(last, "EOF") &&
			len(originalLines) >= 4 {
			inner := originalLines[1 : len(originalLines)-1]
			return checkPatchBoundariesStrict(inner)
		}
	}
	return nil, nil, strictErr
}

// checkStartAndEndLinesStrict mirrors Rust `check_start_and_end_lines_strict`.
func checkStartAndEndLinesStrict(firstLine, lastLine *string) error {
	var first, last *string
	if firstLine != nil {
		t := strings.TrimSpace(*firstLine)
		first = &t
	}
	if lastLine != nil {
		t := strings.TrimSpace(*lastLine)
		last = &t
	}

	if first != nil && last != nil && *first == beginPatchMarker && *last == endPatchMarker {
		return nil
	}
	if first != nil && *first != beginPatchMarker {
		return newInvalidPatchError("The first line of the patch must be '*** Begin Patch'")
	}
	return newInvalidPatchError("The last line of the patch must be '*** End Patch'")
}

// parseOneHunk parses a single hunk from the start of lines, mirroring Rust
// `parse_one_hunk`. It returns the parsed hunk and the number of lines consumed.
func parseOneHunk(lines []string, lineNumber int) (Hunk, int, error) {
	firstLine := strings.TrimSpace(lines[0])

	if path, ok := strings.CutPrefix(firstLine, addFileMarker); ok {
		var contents strings.Builder
		parsed := 1
		for _, addLine := range lines[1:] {
			lineToAdd, ok := strings.CutPrefix(addLine, "+")
			if !ok {
				break
			}
			contents.WriteString(lineToAdd)
			contents.WriteByte('\n')
			parsed++
		}
		return Hunk{
			Kind:     HunkAddFile,
			Path:     path,
			Contents: contents.String(),
		}, parsed, nil
	}

	if path, ok := strings.CutPrefix(firstLine, deleteFileMarker); ok {
		return Hunk{Kind: HunkDeleteFile, Path: path}, 1, nil
	}

	if path, ok := strings.CutPrefix(firstLine, updateFileMarker); ok {
		remaining := lines[1:]
		parsed := 1

		var movePath string
		hasMovePath := false
		if len(remaining) > 0 {
			if dest, ok := strings.CutPrefix(remaining[0], moveToMarker); ok {
				movePath = dest
				hasMovePath = true
				remaining = remaining[1:]
				parsed++
			}
		}

		var chunks []UpdateFileChunk
		for len(remaining) > 0 {
			if strings.TrimSpace(remaining[0]) == "" {
				parsed++
				remaining = remaining[1:]
				continue
			}
			if strings.HasPrefix(remaining[0], "*") {
				break
			}
			chunk, chunkLines, err := parseUpdateFileChunk(
				remaining,
				lineNumber+parsed,
				len(chunks) == 0,
			)
			if err != nil {
				return Hunk{}, 0, err
			}
			chunks = append(chunks, chunk)
			parsed += chunkLines
			remaining = remaining[chunkLines:]
		}

		if len(chunks) == 0 {
			return Hunk{}, 0, newInvalidHunkError(
				fmt.Sprintf("Update file hunk for path '%s' is empty", displayPath(path)),
				lineNumber,
			)
		}

		return Hunk{
			Kind:        HunkUpdateFile,
			Path:        path,
			MovePath:    movePath,
			HasMovePath: hasMovePath,
			Chunks:      chunks,
		}, parsed, nil
	}

	return Hunk{}, 0, newInvalidHunkError(
		fmt.Sprintf(
			"'%s' is not a valid hunk header. Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'",
			firstLine,
		),
		lineNumber,
	)
}

// parseUpdateFileChunk parses a single change chunk, mirroring Rust
// `parse_update_file_chunk`. allowMissingContext permits the first chunk of an
// update hunk to omit the leading `@@` context marker.
func parseUpdateFileChunk(lines []string, lineNumber int, allowMissingContext bool) (UpdateFileChunk, int, error) {
	if len(lines) == 0 {
		return UpdateFileChunk{}, 0, newInvalidHunkError(
			"Update hunk does not contain any lines", lineNumber)
	}

	var changeContext string
	hasChangeContext := false
	startIndex := 0
	switch {
	case lines[0] == emptyChangeContextMarker:
		startIndex = 1
	case strings.HasPrefix(lines[0], changeContextMarker):
		changeContext = strings.TrimPrefix(lines[0], changeContextMarker)
		hasChangeContext = true
		startIndex = 1
	default:
		if !allowMissingContext {
			return UpdateFileChunk{}, 0, newInvalidHunkError(
				fmt.Sprintf("Expected update hunk to start with a @@ context marker, got: '%s'", lines[0]),
				lineNumber,
			)
		}
		startIndex = 0
	}

	if startIndex >= len(lines) {
		return UpdateFileChunk{}, 0, newInvalidHunkError(
			"Update hunk does not contain any lines", lineNumber+1)
	}

	chunk := UpdateFileChunk{
		ChangeContext:    changeContext,
		HasChangeContext: hasChangeContext,
	}
	parsedLines := 0

	for _, line := range lines[startIndex:] {
		if line == eofMarker {
			if parsedLines == 0 {
				return UpdateFileChunk{}, 0, newInvalidHunkError(
					"Update hunk does not contain any lines", lineNumber+1)
			}
			chunk.IsEndOfFile = true
			parsedLines++
			break
		}

		// Inspect the first rune to classify the line.
		first, ok := firstRune(line)
		switch {
		case !ok:
			// Empty line.
			chunk.OldLines = append(chunk.OldLines, "")
			chunk.NewLines = append(chunk.NewLines, "")
		case first == ' ':
			chunk.OldLines = append(chunk.OldLines, line[1:])
			chunk.NewLines = append(chunk.NewLines, line[1:])
		case first == '+':
			chunk.NewLines = append(chunk.NewLines, line[1:])
		case first == '-':
			chunk.OldLines = append(chunk.OldLines, line[1:])
		default:
			if parsedLines == 0 {
				return UpdateFileChunk{}, 0, newInvalidHunkError(
					fmt.Sprintf(
						"Unexpected line found in update hunk: '%s'. Every line should start with ' ' (context line), '+' (added line), or '-' (removed line)",
						line,
					),
					lineNumber+1,
				)
			}
			// Assume this is the start of the next hunk.
			return chunk, parsedLines + startIndex, nil
		}
		parsedLines++
	}

	return chunk, parsedLines + startIndex, nil
}

// firstRune returns the first rune of s and whether s was non-empty. It mirrors
// Rust's `chars().next()`. The classification logic only ever compares against
// single-byte ASCII characters (' ', '+', '-'), so byte indexing of line[1:] in
// the caller is safe for those cases.
func firstRune(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}
