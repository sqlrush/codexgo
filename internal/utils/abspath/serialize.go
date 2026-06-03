package abspath

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotAbsolute is returned (wrapped) when a path that must be absolute is not.
// It mirrors the Rust `InvalidInput` error from
// `from_absolute_path_checked` and the "deserialized without a base path" path.
var ErrNotAbsolute = errors.New("path is not absolute")

// ErrNoBasePath is returned (wrapped) by [AbsolutePathBuf.UnmarshalJSON] when a
// relative path is decoded without a base, mirroring Rust's
// "AbsolutePathBuf deserialized without a base path" error.
var ErrNoBasePath = errors.New("AbsolutePathBuf deserialized without a base path")

// MarshalJSON serializes the path as a single JSON string, matching serde's
// representation of the wrapped `PathBuf` in Codex's `AbsolutePathBuf`.
func (a AbsolutePathBuf) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.path)
}

// UnmarshalJSON decodes a JSON string into an [AbsolutePathBuf].
//
// This mirrors the "no base path" branch of Rust's `Deserialize`: the decoded
// path (after home expansion and platform normalization) must already be
// absolute. Relative inputs fail with [ErrNoBasePath]; use [Unmarshal] or
// [Decoder] to supply a base path for relative resolution.
func (a *AbsolutePathBuf) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("abspath: decode path string: %w", err)
	}
	expanded := normalizePathForPlatform(maybeExpandHomeDirectory(raw))
	if !isAbsolute(expanded) {
		return fmt.Errorf("%w: %s", ErrNoBasePath, raw)
	}
	resolved, err := FromAbsolutePath(raw)
	if err != nil {
		return err
	}
	*a = resolved
	return nil
}

// Unmarshal decodes a JSON string into an [AbsolutePathBuf], resolving relative
// inputs against basePath. This mirrors the "guarded" branch of Rust's
// `Deserialize` (the role played by `AbsolutePathBufGuard`), but passes the base
// explicitly instead of via thread-local state.
func Unmarshal(data []byte, basePath string) (AbsolutePathBuf, error) {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return AbsolutePathBuf{}, fmt.Errorf("abspath: decode path string: %w", err)
	}
	return ResolvePathAgainstBase(raw, basePath), nil
}

// Decoder resolves relative [AbsolutePathBuf] values against a fixed base path.
//
// It is the idiomatic Go analog of holding an `AbsolutePathBufGuard`: construct
// one with [NewDecoder] and reuse it for multiple decodes. A Decoder is
// immutable and safe to share.
type Decoder struct {
	basePath string
}

// NewDecoder returns a [Decoder] that resolves relative paths against basePath.
func NewDecoder(basePath string) Decoder {
	return Decoder{basePath: basePath}
}

// Decode decodes a JSON path string, resolving relative inputs against the
// decoder's base path.
func (d Decoder) Decode(data []byte) (AbsolutePathBuf, error) {
	return Unmarshal(data, d.basePath)
}
