package memories

import (
	"context"
	"os"

	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

// read reads a memory file, optionally from a 1-indexed line offset and limited
// by a line count, then truncates to a token budget, mirroring local::read.
func (b *LocalBackend) read(_ context.Context, req ReadRequest) (ReadResponse, error) {
	if req.LineOffset == 0 {
		return ReadResponse{}, errInvalidLineOffset()
	}
	if req.MaxLines != nil && *req.MaxLines == 0 {
		return ReadResponse{}, errInvalidMaxLines()
	}

	path, err := b.resolveScopedPath(&req.Path)
	if err != nil {
		return ReadResponse{}, err
	}

	info, ok, err := metadataOrNone(path)
	if err != nil {
		return ReadResponse{}, err
	}
	if !ok {
		return ReadResponse{}, errNotFound(req.Path)
	}
	if symErr := rejectSymlink(req.Path, info); symErr != nil {
		return ReadResponse{}, symErr
	}
	if !info.Mode().IsRegular() {
		return ReadResponse{}, errNotFile(req.Path)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return ReadResponse{}, errIO(readErr)
	}
	original := string(data)

	startByte, offErr := lineStartByteOffset(original, req.LineOffset)
	if offErr != nil {
		return ReadResponse{}, offErr
	}
	endByte := lineEndByteOffset(original, startByte, req.MaxLines)
	contentFromOffset := original[startByte:endByte]

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = DefaultReadMaxTokens
	}
	content := truncation.TruncateText(contentFromOffset, truncation.TokensPolicy(maxTokens))
	truncated := endByte < len(original) || content != contentFromOffset

	return ReadResponse{
		Path:            req.Path,
		StartLineNumber: req.LineOffset,
		Content:         content,
		Truncated:       truncated,
	}, nil
}

// lineStartByteOffset returns the byte offset of the 1-indexed lineOffset's
// first byte, mirroring line_start_byte_offset.
func lineStartByteOffset(content string, lineOffset int) (int, error) {
	if lineOffset == 1 {
		return 0, nil
	}
	currentLine := 1
	for idx := 0; idx < len(content); idx++ {
		if content[idx] == '\n' {
			currentLine++
			if currentLine == lineOffset {
				return idx + 1, nil
			}
		}
	}
	return 0, errLineOffsetExceedsFileLength()
}

// lineEndByteOffset returns the byte offset just past the maxLines-th line
// starting at startByte, or the content length when maxLines is nil, mirroring
// line_end_byte_offset.
func lineEndByteOffset(content string, startByte int, maxLines *int) int {
	if maxLines == nil {
		return len(content)
	}
	limit := *maxLines
	linesSeen := 1
	for relativeIdx := startByte; relativeIdx < len(content); relativeIdx++ {
		if content[relativeIdx] == '\n' {
			if linesSeen == limit {
				return relativeIdx + 1
			}
			linesSeen++
		}
	}
	return len(content)
}
