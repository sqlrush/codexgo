package appserverproto

import (
	"bytes"
	"encoding/json"
)

// EmptyPathAsNone normalizes an optional path so that an empty string is treated
// as absent. Mirrors the Rust serde_helpers::deserialize_empty_path_as_none,
// which filters out empty OsStr paths after deserialization.
//
// Returns nil when the input is nil or points at the empty string; otherwise it
// returns the original pointer unchanged.
func EmptyPathAsNone(path *string) *string {
	if path == nil || *path == "" {
		return nil
	}
	return path
}

// DoubleOption distinguishes the three JSON states serde's double_option models:
// a field that is absent, a field explicitly set to null, and a field set to a
// value. Mirrors Option<Option<T>> with serde_with::rust::double_option.
//
//   - Absent field  => Present == false (Set == false implied)
//   - null          => Present == true, Set == false (inner None)
//   - value         => Present == true, Set == true, Value populated
//
// IMPORTANT: embed this as a VALUE field (DoubleOption[T]), not a pointer.
// Go's encoding/json sets a pointer field to nil for a JSON null instead of
// calling UnmarshalJSON, which would collapse the absent/null distinction. As a
// value field, an absent key leaves the zero value (Present == false), while
// both null and a concrete value invoke UnmarshalJSON.
//
// On serialization the outer absence is the caller's responsibility (omit the
// field). When emitted, Present means the key appears; Set chooses value vs null.
type DoubleOption[T any] struct {
	// Present reports whether the JSON key was supplied at all.
	Present bool
	// Set reports whether the supplied value was non-null.
	Set bool
	// Value holds the inner value when Set is true.
	Value T
}

// NewDoubleOptionValue builds a DoubleOption holding a concrete value.
func NewDoubleOptionValue[T any](v T) DoubleOption[T] {
	return DoubleOption[T]{Present: true, Set: true, Value: v}
}

// NewDoubleOptionNull builds a DoubleOption representing an explicit null.
func NewDoubleOptionNull[T any]() DoubleOption[T] {
	return DoubleOption[T]{Present: true, Set: false}
}

// nullLiteral is the canonical JSON null token (trimmed) used for comparison.
var nullLiteral = []byte("null")

// UnmarshalJSON records that the key was present and decodes the inner value,
// treating an explicit null as the inner-None case.
func (d *DoubleOption[T]) UnmarshalJSON(data []byte) error {
	d.Present = true
	if bytes.Equal(bytes.TrimSpace(data), nullLiteral) {
		d.Set = false
		var zero T
		d.Value = zero
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	d.Set = true
	d.Value = v
	return nil
}

// IsZero reports whether the double option is absent (the JSON key was never
// supplied). It exists so a value-embedded DoubleOption field can use the
// encoding/json `omitzero` struct tag to omit the key when absent while still
// distinguishing an explicit null (Present, !Set) from a value (Present, Set).
// This preserves serde's `default + double_option + skip_serializing_if`
// three-state behaviour for fields such as serviceTier.
func (d DoubleOption[T]) IsZero() bool { return !d.Present }

// MarshalJSON emits the inner value, or JSON null when Present but not Set.
//
// Note: an absent (Present == false) double option should be omitted by the
// enclosing struct (e.g. via a value field with the `omitzero` tag); when
// marshaled directly it is rendered as null to remain valid JSON.
func (d DoubleOption[T]) MarshalJSON() ([]byte, error) {
	if !d.Present || !d.Set {
		return nullLiteral, nil
	}
	return json.Marshal(d.Value)
}
