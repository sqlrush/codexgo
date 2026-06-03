package imageutil

import "bytes"

// ImageFormat identifies an image container format that this package is able to
// reason about. It mirrors the subset of the Rust `image::ImageFormat` enum the
// upstream codex utility cares about.
type ImageFormat int

const (
	// FormatUnknown indicates that the bytes did not match any recognized
	// image signature.
	FormatUnknown ImageFormat = iota
	// FormatPNG is the PNG container format.
	FormatPNG
	// FormatJPEG is the JPEG container format.
	FormatJPEG
	// FormatGIF is the GIF container format.
	FormatGIF
	// FormatWebP is the WebP container format.
	//
	// The Go standard library cannot decode or encode WebP, so this package
	// only recognizes its signature for completeness; images using it will be
	// reported as unsupported during decoding. See package documentation.
	FormatWebP
)

// String returns a stable, human-readable name for the format. The values are
// chosen to read naturally inside error messages and debug output.
func (f ImageFormat) String() string {
	switch f {
	case FormatPNG:
		return "Png"
	case FormatJPEG:
		return "Jpeg"
	case FormatGIF:
		return "Gif"
	case FormatWebP:
		return "WebP"
	default:
		return "Unknown"
	}
}

// Magic byte signatures for the supported container formats. These mirror the
// detection logic used by the `image` crate's `guess_format` for the relevant
// formats.
var (
	pngMagic  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	gif87a    = []byte("GIF87a")
	gif89a    = []byte("GIF89a")
	riffMagic = []byte("RIFF")
	webpMagic = []byte("WEBP")
)

// guessFormat inspects the leading bytes of data and returns the detected
// [ImageFormat]. It returns [FormatUnknown] when no signature matches.
//
// The input slice is never mutated.
func guessFormat(data []byte) ImageFormat {
	switch {
	case bytes.HasPrefix(data, pngMagic):
		return FormatPNG
	case bytes.HasPrefix(data, jpegMagic):
		return FormatJPEG
	case bytes.HasPrefix(data, gif87a), bytes.HasPrefix(data, gif89a):
		return FormatGIF
	case isWebP(data):
		return FormatWebP
	default:
		return FormatUnknown
	}
}

// isWebP reports whether data begins with a RIFF/WEBP container header.
// The WebP layout is: "RIFF" <4-byte little-endian size> "WEBP".
func isWebP(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	return bytes.Equal(data[0:4], riffMagic) && bytes.Equal(data[8:12], webpMagic)
}

// canPreserveSourceBytes reports whether bytes of the given format can be passed
// through verbatim instead of being re-encoded.
//
// This mirrors the upstream behavior: only PNG, JPEG, and WebP are eligible for
// byte-for-byte passthrough. GIF is deliberately excluded so that animated GIFs
// are normalized to a single still frame on re-encode.
func canPreserveSourceBytes(format ImageFormat) bool {
	switch format {
	case FormatPNG, FormatJPEG, FormatWebP:
		return true
	default:
		return false
	}
}

// formatToMIME maps a format to its canonical MIME type. Any format that is not
// explicitly recognized falls back to "image/png", matching the upstream
// default-arm behavior.
func formatToMIME(format ImageFormat) string {
	switch format {
	case FormatJPEG:
		return "image/jpeg"
	case FormatGIF:
		return "image/gif"
	case FormatWebP:
		return "image/webp"
	default:
		return "image/png"
	}
}
