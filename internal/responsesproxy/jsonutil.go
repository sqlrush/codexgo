package responsesproxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// marshalNoHTMLEscape marshals v compactly without escaping <, >, and &, so the
// output matches serde_json's default escaping behavior.
func marshalNoHTMLEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// json.Encoder.Encode appends a trailing newline; trim it for raw use.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// marshalIndentNoHTMLEscape marshals v with 2-space indentation and no HTML
// escaping, matching serde_json::to_vec_pretty.
func marshalIndentNoHTMLEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// toValidUTF8 returns a copy of b with invalid UTF-8 sequences replaced by the
// Unicode replacement character (U+FFFD), mirroring Rust's
// String::from_utf8_lossy.
func toValidUTF8(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	return []byte(strings.ToValidUTF8(string(b), string(utf8.RuneError)))
}
