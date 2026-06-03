package imageutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// errorKind enumerates the categories of failure produced by this package. It
// mirrors the variants of the Rust `ImageProcessingError` enum.
type errorKind int

const (
	kindRead errorKind = iota
	kindDecode
	kindEncode
	kindUnsupportedFormat
)

// ImageProcessingError is the error type returned by the image loading and
// encoding routines. It is a faithful port of the Rust `ImageProcessingError`
// enum: a single error type that captures the failure category alongside the
// contextual data relevant to that category.
//
// Use the predicate methods ([ImageProcessingError.IsDecode],
// [ImageProcessingError.IsEncode], [ImageProcessingError.IsUnsupportedFormat],
// [ImageProcessingError.IsRead]) to branch on the failure category, and
// [ImageProcessingError.IsInvalidImage] to detect a genuine decode failure.
type ImageProcessingError struct {
	kind errorKind

	// path is the source path associated with read/decode failures.
	path string
	// format is the target format associated with encode failures.
	format ImageFormat
	// mime is the detected MIME type associated with unsupported-format errors.
	mime string
	// err is the underlying cause, when one exists.
	err error
}

// Error implements the error interface. The rendered messages mirror the
// upstream `thiserror` format strings.
func (e *ImageProcessingError) Error() string {
	switch e.kind {
	case kindRead:
		return fmt.Sprintf("failed to read image at %s: %v", e.path, e.err)
	case kindDecode:
		return fmt.Sprintf("failed to decode image at %s: %v", e.path, e.err)
	case kindEncode:
		return fmt.Sprintf("failed to encode image as %s: %v", e.format, e.err)
	case kindUnsupportedFormat:
		return fmt.Sprintf("unsupported image `%s`", e.mime)
	default:
		return "image processing error"
	}
}

// Unwrap returns the underlying cause so that errors.Is/errors.As can traverse
// the chain. Unsupported-format errors carry no cause and return nil.
func (e *ImageProcessingError) Unwrap() error {
	return e.err
}

// IsRead reports whether the error is a read failure.
func (e *ImageProcessingError) IsRead() bool { return e.kind == kindRead }

// IsDecode reports whether the error is a decode failure.
func (e *ImageProcessingError) IsDecode() bool { return e.kind == kindDecode }

// IsEncode reports whether the error is an encode failure.
func (e *ImageProcessingError) IsEncode() bool { return e.kind == kindEncode }

// IsUnsupportedFormat reports whether the error is an unsupported-format
// failure.
func (e *ImageProcessingError) IsUnsupportedFormat() bool {
	return e.kind == kindUnsupportedFormat
}

// IsInvalidImage reports whether the error represents a genuine decoding failure
// (as opposed to, for example, an unsupported format). It mirrors the Rust
// `ImageProcessingError::is_invalid_image` helper.
func (e *ImageProcessingError) IsInvalidImage() bool {
	return e.kind == kindDecode
}

// newReadError constructs a read failure for the given path.
func newReadError(path string, source error) *ImageProcessingError {
	return &ImageProcessingError{kind: kindRead, path: path, err: source}
}

// newDecodeError constructs a decode failure for the given path.
func newDecodeError(path string, source error) *ImageProcessingError {
	return &ImageProcessingError{kind: kindDecode, path: path, err: source}
}

// newEncodeError constructs an encode failure for the given target format.
func newEncodeError(format ImageFormat, source error) *ImageProcessingError {
	return &ImageProcessingError{kind: kindEncode, format: format, err: source}
}

// newUnsupportedFormatError constructs an unsupported-format failure for the
// given MIME type.
func newUnsupportedFormatError(mime string) *ImageProcessingError {
	return &ImageProcessingError{kind: kindUnsupportedFormat, mime: mime}
}

// decodeError classifies a decoding failure.
//
// It mirrors the Rust `ImageProcessingError::decode_error`: a true decode error
// (the bytes could be recognized as an image but failed to decode) is reported
// as a decode failure, while a failure to recognize the format at all is
// reported as an unsupported-format error keyed by the MIME type guessed from
// the file extension.
//
// recognized indicates whether the bytes matched a known image signature; when
// false, the failure is treated as an unsupported format rather than a decode
// error, matching upstream semantics.
func decodeError(path string, recognized bool, source error) *ImageProcessingError {
	if recognized {
		return newDecodeError(path, source)
	}
	return newUnsupportedFormatError(mimeFromPath(path))
}

// mimeFromPath guesses a MIME type from a file extension. It mirrors the subset
// of `mime_guess` behavior exercised by the upstream crate, falling back to
// "unknown" when no mapping is found.
func mimeFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := extensionMIME[ext]; ok {
		return mime
	}
	return "unknown"
}

// extensionMIME maps common image file extensions to their MIME types.
var extensionMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".jpe":  "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".ico":  "image/vnd.microsoft.icon",
	".svg":  "image/svg+xml",
	".avif": "image/avif",
}
