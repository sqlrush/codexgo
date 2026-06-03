package memories

import "fmt"

// BackendErrorKind classifies a BackendError, mirroring the variants of the Rust
// MemoriesBackendError enum. The classification drives whether a failure is
// reported back to the model or treated as fatal.
type BackendErrorKind int

const (
	// ErrInvalidFilename indicates the ad-hoc note filename is malformed.
	ErrInvalidFilename BackendErrorKind = iota
	// ErrEmptyAdHocNote indicates the ad-hoc note body was empty.
	ErrEmptyAdHocNote
	// ErrAdHocNoteAlreadyExists indicates the ad-hoc note already exists.
	ErrAdHocNoteAlreadyExists
	// ErrInvalidPath indicates the requested path is not permitted.
	ErrInvalidPath
	// ErrInvalidCursor indicates the supplied pagination cursor is invalid.
	ErrInvalidCursor
	// ErrNotFound indicates the requested path does not exist.
	ErrNotFound
	// ErrInvalidLineOffset indicates line_offset was not a 1-indexed line.
	ErrInvalidLineOffset
	// ErrInvalidMaxLines indicates max_lines was not a positive integer.
	ErrInvalidMaxLines
	// ErrLineOffsetExceedsFileLength indicates line_offset is past EOF.
	ErrLineOffsetExceedsFileLength
	// ErrNotFile indicates the requested path is not a file.
	ErrNotFile
	// ErrEmptyQuery indicates the query list was empty or held empty strings.
	ErrEmptyQuery
	// ErrInvalidMatchWindow indicates all_within_lines.line_count was not positive.
	ErrInvalidMatchWindow
	// ErrIO indicates an underlying filesystem error (fatal).
	ErrIO
	// ErrArgsParse indicates the tool arguments failed validation/parsing. The
	// model-visible message is carried verbatim, mirroring parse_args errors that
	// map to FunctionCallError::RespondToModel.
	ErrArgsParse
)

// BackendError is the error type returned by Backend operations. Its Error()
// string matches the Rust thiserror messages byte-for-byte because the message
// is surfaced to the model.
type BackendError struct {
	Kind         BackendErrorKind
	filename     string
	path         string
	cursor       string
	reason       string
	modelMessage string
	err          error
}

// Error renders the user-facing message, matching the Rust #[error(...)] strings.
func (e *BackendError) Error() string {
	switch e.Kind {
	case ErrInvalidFilename:
		return fmt.Sprintf("filename '%s' %s", e.filename, e.reason)
	case ErrEmptyAdHocNote:
		return "ad-hoc note must not be empty"
	case ErrAdHocNoteAlreadyExists:
		return fmt.Sprintf("ad-hoc note '%s' already exists", e.filename)
	case ErrInvalidPath:
		return fmt.Sprintf("path '%s' %s", e.path, e.reason)
	case ErrInvalidCursor:
		return fmt.Sprintf("cursor '%s' %s", e.cursor, e.reason)
	case ErrNotFound:
		return fmt.Sprintf("path '%s' was not found", e.path)
	case ErrInvalidLineOffset:
		return "line_offset must be a 1-indexed line number"
	case ErrInvalidMaxLines:
		return "max_lines must be a positive integer"
	case ErrLineOffsetExceedsFileLength:
		return "line_offset exceeds file length"
	case ErrNotFile:
		return fmt.Sprintf("path '%s' is not a file", e.path)
	case ErrEmptyQuery:
		return "queries must not be empty or contain empty strings"
	case ErrInvalidMatchWindow:
		return "all_within_lines.line_count must be a positive integer"
	case ErrIO:
		return fmt.Sprintf("I/O error while reading memories: %v", e.err)
	case ErrArgsParse:
		return e.modelMessage
	default:
		return "unknown memories backend error"
	}
}

// Unwrap exposes the wrapped IO error for errors.Is/As.
func (e *BackendError) Unwrap() error { return e.err }

// IsFatal reports whether the error should abort the turn rather than be sent to
// the model, mirroring the Rust backend_error_to_function_call mapping (only IO
// errors are fatal).
func (e *BackendError) IsFatal() bool { return e.Kind == ErrIO }

func errInvalidFilename(filename, reason string) *BackendError {
	return &BackendError{Kind: ErrInvalidFilename, filename: filename, reason: reason}
}

func errEmptyAdHocNote() *BackendError {
	return &BackendError{Kind: ErrEmptyAdHocNote}
}

func errAdHocNoteAlreadyExists(filename string) *BackendError {
	return &BackendError{Kind: ErrAdHocNoteAlreadyExists, filename: filename}
}

func errInvalidPath(path, reason string) *BackendError {
	return &BackendError{Kind: ErrInvalidPath, path: path, reason: reason}
}

func errInvalidCursor(cursor, reason string) *BackendError {
	return &BackendError{Kind: ErrInvalidCursor, cursor: cursor, reason: reason}
}

func errNotFound(path string) *BackendError {
	return &BackendError{Kind: ErrNotFound, path: path}
}

func errInvalidLineOffset() *BackendError {
	return &BackendError{Kind: ErrInvalidLineOffset}
}

func errInvalidMaxLines() *BackendError {
	return &BackendError{Kind: ErrInvalidMaxLines}
}

func errLineOffsetExceedsFileLength() *BackendError {
	return &BackendError{Kind: ErrLineOffsetExceedsFileLength}
}

func errNotFile(path string) *BackendError {
	return &BackendError{Kind: ErrNotFile, path: path}
}

func errEmptyQuery() *BackendError {
	return &BackendError{Kind: ErrEmptyQuery}
}

func errInvalidMatchWindow() *BackendError {
	return &BackendError{Kind: ErrInvalidMatchWindow}
}

func errIO(err error) *BackendError {
	return &BackendError{Kind: ErrIO, err: err}
}
