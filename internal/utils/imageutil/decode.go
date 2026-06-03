package imageutil

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// decodeFromMemory decodes image bytes into an [image.Image].
//
// It returns the decoded image, whether the bytes were recognized as a known
// image container (by signature), and an error. The recognized flag is used by
// the error-classification logic to distinguish a true decode failure (bytes
// looked like an image but could not be decoded) from an unsupported-format
// failure (bytes did not look like any image we handle), matching upstream
// semantics.
//
// The input slice is treated as read-only and is never mutated.
func decodeFromMemory(data []byte) (img image.Image, recognized bool, err error) {
	format := guessFormat(data)
	switch format {
	case FormatPNG:
		img, err = png.Decode(bytes.NewReader(data))
		return img, true, err
	case FormatJPEG:
		img, err = jpeg.Decode(bytes.NewReader(data))
		return img, true, err
	case FormatGIF:
		// Decode only the first frame, matching the upstream contract that
		// only non-animated GIFs are supported and that re-encoding collapses
		// to a still image.
		img, err = gif.Decode(bytes.NewReader(data))
		return img, true, err
	case FormatWebP:
		// The Go standard library cannot decode WebP. The signature is
		// recognized, but decoding is unsupported; report it as an
		// unsupported format rather than a decode failure.
		return nil, false, errWebPUnsupported
	default:
		// Fall back to the generic registry in case some other decoder was
		// registered by the importing program; otherwise this fails and is
		// classified as an unsupported format.
		img, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, false, err
		}
		return img, false, nil
	}
}

// errWebPUnsupported is the sentinel cause used when WebP bytes are encountered.
var errWebPUnsupported = errSentinel("webp decoding is not supported by the standard library")

// errSentinel is a tiny string-backed error type for package-internal sentinels.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }
