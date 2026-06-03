// Package imageutil decodes, normalizes, and base64-encodes images for use in
// prompt payloads (for example, the view_image tool and clipboard paste flows).
//
// It is a faithful Go port of the codex-utils-image Rust crate. The
// externally observable behavior and formats are preserved:
//
//   - Images within the [MaxDimension] bounds are passed through byte-for-byte
//     when their container format can be safely preserved (PNG, JPEG, WebP).
//   - Oversized images are downscaled to fit within a [MaxDimension] square
//     while preserving aspect ratio, using a triangle (linear) resampling
//     filter, then re-encoded.
//   - GIF inputs are normalized to a single still PNG frame.
//   - Results are memoized in a small content-addressed LRU cache keyed by the
//     SHA-1 digest of the input bytes together with the [PromptImageMode].
//
// Standard-library limitation: the Go standard library cannot decode or encode
// WebP. WebP inputs are recognized by signature but reported as an unsupported
// format. See externalDeps in the porting notes for the dependency that would
// restore full WebP support.
package imageutil

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
)

// MaxDimension is the maximum width or height (in pixels) permitted before an
// image is downscaled prior to upload. It matches the upstream constant.
const MaxDimension uint32 = 2048

// imageCacheCapacity is the number of processed images retained in the LRU
// cache. It matches the upstream capacity of 32.
const imageCacheCapacity = 32

// PromptImageMode controls whether oversized images are resized to fit within
// the [MaxDimension] bounds or preserved at their original dimensions.
type PromptImageMode int

const (
	// ResizeToFit downscales images larger than [MaxDimension] so that they fit
	// within a [MaxDimension] square, preserving aspect ratio.
	ResizeToFit PromptImageMode = iota
	// Original preserves the image at its source dimensions regardless of size.
	Original
)

// String returns a stable name for the mode, useful for debugging.
func (m PromptImageMode) String() string {
	switch m {
	case ResizeToFit:
		return "ResizeToFit"
	case Original:
		return "Original"
	default:
		return fmt.Sprintf("PromptImageMode(%d)", int(m))
	}
}

// EncodedImage is the result of processing an image for prompt upload. It holds
// the (possibly re-encoded) image bytes alongside the MIME type and final
// pixel dimensions.
//
// EncodedImage values are treated as immutable snapshots; callers must not
// mutate the Bytes slice. Helper methods never mutate the receiver.
type EncodedImage struct {
	// Bytes is the encoded image payload.
	Bytes []byte
	// MIME is the MIME type corresponding to Bytes (for example "image/png").
	MIME string
	// Width is the pixel width of the encoded image.
	Width uint32
	// Height is the pixel height of the encoded image.
	Height uint32
}

// IntoDataURL returns a base64 "data:" URL for the encoded image, of the form
// "data:{mime};base64,{base64}". It mirrors the Rust `EncodedImage::
// into_data_url`. The receiver is not mutated.
func (e EncodedImage) IntoDataURL() string {
	encoded := base64.StdEncoding.EncodeToString(e.Bytes)
	return fmt.Sprintf("data:%s;base64,%s", e.MIME, encoded)
}

// imageCacheKey is the LRU cache key: the content digest of the input bytes plus
// the processing mode. Using the content digest (rather than the path) avoids
// stale results when a file's contents change.
type imageCacheKey struct {
	digest [sha1.Size]byte
	mode   PromptImageMode
}

// imageCache memoizes processed images across calls.
var imageCache = newLRUCache[imageCacheKey, EncodedImage](imageCacheCapacity)

// clearImageCache empties the package-level cache. It exists primarily to make
// behavior deterministic in tests, mirroring the upstream `IMAGE_CACHE.clear()`
// usage.
func clearImageCache() {
	imageCache.clear()
}

// LoadForPromptBytes processes raw image bytes for inclusion in a prompt.
//
// The path is used only for error reporting (MIME guessing on unsupported
// formats); the image content itself comes from fileBytes. The returned
// [EncodedImage] is either a byte-for-byte passthrough of the input (when the
// format is preservable and the image is within bounds, or when mode is
// [Original]) or a freshly encoded image (when the input was downscaled or its
// format had to be normalized).
//
// Results are memoized by the SHA-1 digest of fileBytes and mode. The fileBytes
// slice is treated as read-only and is never mutated; on the passthrough path
// the EncodedImage references the same backing array, so callers must not mutate
// fileBytes after the call.
//
// This is a faithful port of the Rust `load_for_prompt_bytes`.
func LoadForPromptBytes(path string, fileBytes []byte, mode PromptImageMode) (EncodedImage, error) {
	key := imageCacheKey{
		digest: sha1.Sum(fileBytes),
		mode:   mode,
	}

	return imageCache.getOrTryInsertWith(key, func() (EncodedImage, error) {
		return processImage(path, fileBytes, mode)
	})
}

// processImage performs the actual decode/normalize/encode work for a single
// image. It contains no caching; LoadForPromptBytes wraps it.
func processImage(path string, fileBytes []byte, mode PromptImageMode) (EncodedImage, error) {
	format := guessFormat(fileBytes)

	img, recognized, err := decodeFromMemory(fileBytes)
	if err != nil {
		return EncodedImage{}, decodeError(path, recognized, err)
	}
	if img == nil {
		return EncodedImage{}, newUnsupportedFormatError(mimeFromPath(path))
	}

	b := img.Bounds()
	width := uint32(b.Dx())
	height := uint32(b.Dy())

	withinBounds := width <= MaxDimension && height <= MaxDimension

	if mode == Original || withinBounds {
		// Pass the source bytes through unchanged when we can preserve the
		// container format; otherwise normalize to PNG.
		if canPreserveSourceBytes(format) {
			return EncodedImage{
				Bytes:  fileBytes,
				MIME:   formatToMIME(format),
				Width:  width,
				Height: height,
			}, nil
		}
		bytesOut, outFormat, encErr := encodeImage(img, FormatPNG)
		if encErr != nil {
			return EncodedImage{}, encErr
		}
		return EncodedImage{
			Bytes:  bytesOut,
			MIME:   formatToMIME(outFormat),
			Width:  width,
			Height: height,
		}, nil
	}

	// Oversized image in ResizeToFit mode: downscale, then re-encode preserving
	// the source format when possible (else PNG).
	resized := resizeToFit(img, MaxDimension, MaxDimension)

	targetFormat := FormatPNG
	if canPreserveSourceBytes(format) {
		targetFormat = format
	}

	bytesOut, outFormat, encErr := encodeImage(resized, targetFormat)
	if encErr != nil {
		return EncodedImage{}, encErr
	}

	rb := resized.Bounds()
	return EncodedImage{
		Bytes:  bytesOut,
		MIME:   formatToMIME(outFormat),
		Width:  uint32(rb.Dx()),
		Height: uint32(rb.Dy()),
	}, nil
}
