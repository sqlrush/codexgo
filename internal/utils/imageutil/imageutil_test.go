package imageutil

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// solidImage builds a width x height image filled with a single color.
func solidImage(width, height int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// encodePNG encodes an image to PNG bytes.
func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// encodeJPEG encodes an image to JPEG bytes.
func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// encodeGIF encodes an image to GIF bytes.
func encodeGIF(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func TestReturnsOriginalImageWhenWithinBounds(t *testing.T) {
	clearImageCache()

	tests := []struct {
		name string
		mode PromptImageMode
	}{
		{name: "resize-to-fit", mode: ResizeToFit},
		{name: "original", mode: Original},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearImageCache()
			img := solidImage(64, 32, color.RGBA{10, 20, 30, 255})
			original := encodePNG(t, img)

			got, err := LoadForPromptBytes("in-memory-image", original, tc.mode)
			if err != nil {
				t.Fatalf("process image: %v", err)
			}
			if got.Width != 64 || got.Height != 32 {
				t.Errorf("dimensions = %dx%d, want 64x32", got.Width, got.Height)
			}
			if got.MIME != "image/png" {
				t.Errorf("mime = %q, want image/png", got.MIME)
			}
			if !bytes.Equal(got.Bytes, original) {
				t.Errorf("bytes were not passed through unchanged")
			}
		})
	}
}

func TestPassthroughJPEGWithinBounds(t *testing.T) {
	clearImageCache()
	img := solidImage(48, 24, color.RGBA{120, 30, 200, 255})
	original := encodeJPEG(t, img)

	got, err := LoadForPromptBytes("photo.jpg", original, ResizeToFit)
	if err != nil {
		t.Fatalf("process image: %v", err)
	}
	if got.MIME != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg", got.MIME)
	}
	if !bytes.Equal(got.Bytes, original) {
		t.Errorf("jpeg bytes were not passed through unchanged")
	}
}

func TestGIFIsNormalizedToPNG(t *testing.T) {
	clearImageCache()
	img := solidImage(40, 20, color.RGBA{200, 50, 50, 255})
	original := encodeGIF(t, img)

	got, err := LoadForPromptBytes("animation.gif", original, ResizeToFit)
	if err != nil {
		t.Fatalf("process image: %v", err)
	}
	if got.MIME != "image/png" {
		t.Errorf("mime = %q, want image/png (gif normalized)", got.MIME)
	}
	if got.Width != 40 || got.Height != 20 {
		t.Errorf("dimensions = %dx%d, want 40x20", got.Width, got.Height)
	}
	if guessFormat(got.Bytes) != FormatPNG {
		t.Errorf("output bytes are not PNG-encoded")
	}
	// The re-encoded bytes must round-trip back to a valid image.
	if _, _, err := decodeFromMemory(got.Bytes); err != nil {
		t.Errorf("re-encoded gif->png did not decode: %v", err)
	}
}

func TestDownscalesLargeImage(t *testing.T) {
	clearImageCache()

	tests := []struct {
		name   string
		encode func(*testing.T, image.Image) []byte
		format ImageFormat
		mime   string
	}{
		{name: "png", encode: encodePNG, format: FormatPNG, mime: "image/png"},
		{name: "jpeg", encode: encodeJPEG, format: FormatJPEG, mime: "image/jpeg"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearImageCache()
			img := solidImage(4096, 2048, color.RGBA{200, 10, 10, 255})
			original := tc.encode(t, img)

			got, err := LoadForPromptBytes("in-memory-image", original, ResizeToFit)
			if err != nil {
				t.Fatalf("process image: %v", err)
			}
			if got.Width > MaxDimension || got.Height > MaxDimension {
				t.Errorf("dimensions %dx%d exceed MaxDimension %d", got.Width, got.Height, MaxDimension)
			}
			// 4096x2048 fit to 2048x2048 -> ratio 0.5 -> 2048x1024.
			if got.Width != 2048 || got.Height != 1024 {
				t.Errorf("dimensions = %dx%d, want 2048x1024", got.Width, got.Height)
			}
			if got.MIME != tc.mime {
				t.Errorf("mime = %q, want %q", got.MIME, tc.mime)
			}
			if guessFormat(got.Bytes) != tc.format {
				t.Errorf("resized output format = %v, want %v", guessFormat(got.Bytes), tc.format)
			}
			decoded, _, err := decodeFromMemory(got.Bytes)
			if err != nil {
				t.Fatalf("read resized bytes back: %v", err)
			}
			db := decoded.Bounds()
			if uint32(db.Dx()) != got.Width || uint32(db.Dy()) != got.Height {
				t.Errorf("decoded dims = %dx%d, want %dx%d", db.Dx(), db.Dy(), got.Width, got.Height)
			}
		})
	}
}

func TestDownscalesTallImageToFitSquareBounds(t *testing.T) {
	clearImageCache()
	img := solidImage(1024, 4096, color.RGBA{200, 10, 10, 255})
	original := encodePNG(t, img)

	got, err := LoadForPromptBytes("in-memory-image", original, ResizeToFit)
	if err != nil {
		t.Fatalf("process image: %v", err)
	}
	if got.Width != 512 || got.Height != MaxDimension {
		t.Errorf("dimensions = %dx%d, want 512x%d", got.Width, got.Height, MaxDimension)
	}
	if got.MIME != "image/png" {
		t.Errorf("mime = %q, want image/png", got.MIME)
	}
}

func TestPreservesLargeImageInOriginalMode(t *testing.T) {
	clearImageCache()
	img := solidImage(4096, 2048, color.RGBA{180, 30, 30, 255})
	original := encodePNG(t, img)

	got, err := LoadForPromptBytes("in-memory-image", original, Original)
	if err != nil {
		t.Fatalf("process image: %v", err)
	}
	if got.Width != 4096 || got.Height != 2048 {
		t.Errorf("dimensions = %dx%d, want 4096x2048", got.Width, got.Height)
	}
	if got.MIME != "image/png" {
		t.Errorf("mime = %q, want image/png", got.MIME)
	}
	if !bytes.Equal(got.Bytes, original) {
		t.Errorf("bytes were not passed through unchanged in Original mode")
	}
}

func TestFailsCleanlyForInvalidImages(t *testing.T) {
	clearImageCache()

	tests := []struct {
		name  string
		bytes []byte
	}{
		{name: "garbage", bytes: []byte("not an image")},
		{name: "empty", bytes: []byte{}},
		{name: "truncated png", bytes: append([]byte{}, pngMagic...)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearImageCache()
			_, err := LoadForPromptBytes("in-memory-image", tc.bytes, ResizeToFit)
			if err == nil {
				t.Fatalf("expected error for invalid image")
			}
			ipe, ok := err.(*ImageProcessingError)
			if !ok {
				t.Fatalf("error type = %T, want *ImageProcessingError", err)
			}
			if !ipe.IsDecode() && !ipe.IsUnsupportedFormat() {
				t.Errorf("error kind not decode/unsupported: %v", ipe)
			}
		})
	}
}

func TestUnsupportedFormatMIMEFromPath(t *testing.T) {
	clearImageCache()
	// Bytes are not a recognized image; classification should use the path's
	// extension to guess the MIME type.
	_, err := LoadForPromptBytes("/tmp/foo.gif", []byte("definitely not a gif"), ResizeToFit)
	if err == nil {
		t.Fatalf("expected error")
	}
	ipe, ok := err.(*ImageProcessingError)
	if !ok {
		t.Fatalf("error type = %T, want *ImageProcessingError", err)
	}
	if !ipe.IsUnsupportedFormat() {
		t.Fatalf("expected unsupported-format error, got %v", ipe)
	}
	if !strings.Contains(ipe.Error(), "image/gif") {
		t.Errorf("error %q does not mention guessed mime image/gif", ipe.Error())
	}
}

func TestWebPRecognizedButUnsupported(t *testing.T) {
	clearImageCache()
	// Minimal RIFF/WEBP header. Not a valid image; the standard library cannot
	// decode it, so it must surface as unsupported, not a panic.
	header := append([]byte{}, riffMagic...)
	header = append(header, 0, 0, 0, 0) // size
	header = append(header, webpMagic...)
	if guessFormat(header) != FormatWebP {
		t.Fatalf("header not detected as WebP")
	}
	_, err := LoadForPromptBytes("image.webp", header, ResizeToFit)
	if err == nil {
		t.Fatalf("expected error for webp")
	}
	ipe := err.(*ImageProcessingError)
	if !ipe.IsUnsupportedFormat() {
		t.Errorf("webp error kind = %v, want unsupported format", ipe)
	}
}

func TestReprocessesUpdatedFileContents(t *testing.T) {
	clearImageCache()

	first := encodePNG(t, solidImage(32, 16, color.RGBA{20, 120, 220, 255}))
	gotFirst, err := LoadForPromptBytes("in-memory-image", first, ResizeToFit)
	if err != nil {
		t.Fatalf("process first image: %v", err)
	}

	second := encodePNG(t, solidImage(96, 48, color.RGBA{50, 60, 70, 255}))
	gotSecond, err := LoadForPromptBytes("in-memory-image", second, ResizeToFit)
	if err != nil {
		t.Fatalf("process second image: %v", err)
	}

	if gotFirst.Width != 32 || gotFirst.Height != 16 {
		t.Errorf("first dims = %dx%d, want 32x16", gotFirst.Width, gotFirst.Height)
	}
	if gotSecond.Width != 96 || gotSecond.Height != 48 {
		t.Errorf("second dims = %dx%d, want 96x48", gotSecond.Width, gotSecond.Height)
	}
	if bytes.Equal(gotFirst.Bytes, gotSecond.Bytes) {
		t.Errorf("updated contents produced identical bytes")
	}
}

func TestCacheReturnsSameResultForSameInput(t *testing.T) {
	clearImageCache()
	original := encodePNG(t, solidImage(50, 50, color.RGBA{1, 2, 3, 255}))

	a, err := LoadForPromptBytes("a.png", original, ResizeToFit)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	// Same content under a different path/mode-equal key should hit the cache
	// and return identical bytes.
	b, err := LoadForPromptBytes("b.png", original, ResizeToFit)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if !bytes.Equal(a.Bytes, b.Bytes) || a.MIME != b.MIME {
		t.Errorf("cache returned divergent results for identical input")
	}
	// Same content under a different mode must NOT collide.
	c, err := LoadForPromptBytes("a.png", original, Original)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if c.Width != 50 || c.Height != 50 {
		t.Errorf("mode-keyed entry has wrong dims %dx%d", c.Width, c.Height)
	}
}

func TestIntoDataURL(t *testing.T) {
	tests := []struct {
		name  string
		enc   EncodedImage
		want  string
		check func(string) bool
	}{
		{
			name: "png header",
			enc:  EncodedImage{Bytes: []byte{1, 2, 3}, MIME: "image/png", Width: 1, Height: 1},
			want: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{1, 2, 3}),
		},
		{
			name: "jpeg header",
			enc:  EncodedImage{Bytes: []byte("abc"), MIME: "image/jpeg", Width: 2, Height: 2},
			want: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("abc")),
		},
		{
			name: "empty bytes",
			enc:  EncodedImage{Bytes: nil, MIME: "image/png"},
			want: "data:image/png;base64,",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.enc.IntoDataURL(); got != tc.want {
				t.Errorf("IntoDataURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIntoDataURLDoesNotMutate(t *testing.T) {
	src := []byte{9, 8, 7}
	enc := EncodedImage{Bytes: src, MIME: "image/png"}
	_ = enc.IntoDataURL()
	if !bytes.Equal(src, []byte{9, 8, 7}) {
		t.Errorf("IntoDataURL mutated underlying bytes: %v", src)
	}
}

func TestLoadForPromptBytesDoesNotMutateInput(t *testing.T) {
	clearImageCache()
	original := encodePNG(t, solidImage(20, 20, color.RGBA{5, 5, 5, 255}))
	cp := append([]byte{}, original...)
	if _, err := LoadForPromptBytes("x.png", original, ResizeToFit); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !bytes.Equal(original, cp) {
		t.Errorf("LoadForPromptBytes mutated the input slice")
	}
}

func TestGuessFormat(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want ImageFormat
	}{
		{name: "png", data: pngMagic, want: FormatPNG},
		{name: "jpeg", data: jpegMagic, want: FormatJPEG},
		{name: "gif87a", data: gif87a, want: FormatGIF},
		{name: "gif89a", data: gif89a, want: FormatGIF},
		{name: "empty", data: nil, want: FormatUnknown},
		{name: "random", data: []byte("hello world!!"), want: FormatUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := guessFormat(tc.data); got != tc.want {
				t.Errorf("guessFormat = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResizeDimensions(t *testing.T) {
	tests := []struct {
		name             string
		w, h, maxW, maxH uint32
		wantW, wantH     uint32
	}{
		{name: "wide", w: 4096, h: 2048, maxW: 2048, maxH: 2048, wantW: 2048, wantH: 1024},
		{name: "tall", w: 1024, h: 4096, maxW: 2048, maxH: 2048, wantW: 512, wantH: 2048},
		{name: "square", w: 4096, h: 4096, maxW: 2048, maxH: 2048, wantW: 2048, wantH: 2048},
		{name: "no-scale-needed-still-computed", w: 1000, h: 500, maxW: 2048, maxH: 2048, wantW: 2048, wantH: 1024},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gw, gh := resizeDimensions(tc.w, tc.h, tc.maxW, tc.maxH)
			if gw != tc.wantW || gh != tc.wantH {
				t.Errorf("resizeDimensions = %dx%d, want %dx%d", gw, gh, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestErrorPredicatesAndMessages(t *testing.T) {
	tests := []struct {
		name           string
		err            *ImageProcessingError
		wantContains   string
		isDecode       bool
		isEncode       bool
		isUnsupported  bool
		isRead         bool
		isInvalidImage bool
	}{
		{
			name:         "read",
			err:          newReadError("/a/b.png", errSentinel("boom")),
			wantContains: "failed to read image at /a/b.png",
			isRead:       true,
		},
		{
			name:           "decode",
			err:            newDecodeError("/a/b.png", errSentinel("bad")),
			wantContains:   "failed to decode image at /a/b.png",
			isDecode:       true,
			isInvalidImage: true,
		},
		{
			name:         "encode",
			err:          newEncodeError(FormatJPEG, errSentinel("nope")),
			wantContains: "failed to encode image as Jpeg",
			isEncode:     true,
		},
		{
			name:          "unsupported",
			err:           newUnsupportedFormatError("image/tiff"),
			wantContains:  "unsupported image `image/tiff`",
			isUnsupported: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.err.Error(), tc.wantContains) {
				t.Errorf("Error() = %q, want contains %q", tc.err.Error(), tc.wantContains)
			}
			if tc.err.IsDecode() != tc.isDecode {
				t.Errorf("IsDecode = %v, want %v", tc.err.IsDecode(), tc.isDecode)
			}
			if tc.err.IsEncode() != tc.isEncode {
				t.Errorf("IsEncode = %v, want %v", tc.err.IsEncode(), tc.isEncode)
			}
			if tc.err.IsUnsupportedFormat() != tc.isUnsupported {
				t.Errorf("IsUnsupportedFormat = %v, want %v", tc.err.IsUnsupportedFormat(), tc.isUnsupported)
			}
			if tc.err.IsRead() != tc.isRead {
				t.Errorf("IsRead = %v, want %v", tc.err.IsRead(), tc.isRead)
			}
			if tc.err.IsInvalidImage() != tc.isInvalidImage {
				t.Errorf("IsInvalidImage = %v, want %v", tc.err.IsInvalidImage(), tc.isInvalidImage)
			}
		})
	}
}

func TestMIMEFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "x.png", want: "image/png"},
		{path: "x.JPG", want: "image/jpeg"},
		{path: "x.jpeg", want: "image/jpeg"},
		{path: "x.gif", want: "image/gif"},
		{path: "x.webp", want: "image/webp"},
		{path: "x.unknownext", want: "unknown"},
		{path: "noext", want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := mimeFromPath(tc.path); got != tc.want {
				t.Errorf("mimeFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestLRUCacheEvicts(t *testing.T) {
	c := newLRUCache[int, int](2)
	put := func(k, v int) {
		c.mu.Lock()
		c.putLocked(k, v)
		c.mu.Unlock()
	}
	put(1, 10)
	put(2, 20)
	if _, ok := c.get(1); !ok { // touch 1 so 2 is LRU
		t.Fatal("key 1 missing")
	}
	put(3, 30) // evicts 2
	if _, ok := c.get(2); ok {
		t.Errorf("key 2 should have been evicted")
	}
	if v, ok := c.get(1); !ok || v != 10 {
		t.Errorf("key 1 = %v,%v want 10,true", v, ok)
	}
	if v, ok := c.get(3); !ok || v != 30 {
		t.Errorf("key 3 = %v,%v want 30,true", v, ok)
	}
}

func TestLRUCacheFactoryErrorNotCached(t *testing.T) {
	c := newLRUCache[int, int](2)
	boom := errSentinel("boom")
	_, err := c.getOrTryInsertWith(1, func() (int, error) { return 0, boom })
	if err != boom {
		t.Fatalf("err = %v, want boom", err)
	}
	if _, ok := c.get(1); ok {
		t.Errorf("failed factory result must not be cached")
	}
	v, err := c.getOrTryInsertWith(1, func() (int, error) { return 42, nil })
	if err != nil || v != 42 {
		t.Fatalf("retry = %v,%v want 42,nil", v, err)
	}
}

// TestResampleProducesValidPNG ensures a non-uniform image survives the resize
// pipeline and decodes to the expected dimensions.
func TestResampleProducesValidPNG(t *testing.T) {
	clearImageCache()
	img := image.NewRGBA(image.Rect(0, 0, 3000, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 3000; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	original := encodePNG(t, img)
	got, err := LoadForPromptBytes("grad.png", original, ResizeToFit)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// 3000x1000 -> ratio min(2048/3000, 2048/1000)=0.6826 -> ~2048x683.
	if got.Width != 2048 {
		t.Errorf("width = %d, want 2048", got.Width)
	}
	if got.Height < 600 || got.Height > 700 {
		t.Errorf("height = %d, want roughly 683", got.Height)
	}
	decoded, err := png.Decode(bytes.NewReader(got.Bytes))
	if err != nil {
		t.Fatalf("decode resized png: %v", err)
	}
	db := decoded.Bounds()
	if uint32(db.Dx()) != got.Width || uint32(db.Dy()) != got.Height {
		t.Errorf("decoded dims %dx%d != reported %dx%d", db.Dx(), db.Dy(), got.Width, got.Height)
	}
}
