package imageutil

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
)

// jpegQuality is the JPEG encoder quality used when re-encoding images. It
// matches the upstream `JpegEncoder::new_with_quality(.., 85)`.
const jpegQuality = 85

// encodeImage re-encodes an image to a target format derived from a preferred
// format, returning the encoded bytes alongside the format actually used.
//
// It mirrors the Rust `encode_image`: the preferred format is normalized to one
// of the supported encode targets. JPEG stays JPEG; everything else (including
// WebP, which the Go standard library cannot encode) falls back to PNG, which is
// also the upstream default arm. The input image is never mutated.
func encodeImage(img image.Image, preferred ImageFormat) ([]byte, ImageFormat, error) {
	target := normalizeEncodeFormat(preferred)

	var buf bytes.Buffer
	switch target {
	case FormatJPEG:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, target, newEncodeError(target, err)
		}
	default: // FormatPNG
		target = FormatPNG
		if err := png.Encode(&buf, img); err != nil {
			return nil, target, newEncodeError(target, err)
		}
	}

	// Return a copy so the buffer's backing array is not retained or shared.
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, target, nil
}

// normalizeEncodeFormat maps a preferred format onto the set of formats this
// package can actually emit. JPEG is preserved; all other inputs (PNG, GIF,
// WebP, unknown) normalize to PNG.
//
// Upstream also preserves WebP here, but because the Go standard library cannot
// encode WebP and such inputs cannot be decoded in the first place, this path is
// unreachable for WebP in practice; PNG is the safe, lossless fallback.
func normalizeEncodeFormat(preferred ImageFormat) ImageFormat {
	switch preferred {
	case FormatJPEG:
		return FormatJPEG
	default:
		return FormatPNG
	}
}
