// Package jsontotoml converts decoded JSON values into a semantically
// equivalent TOML value model and renders that model as TOML text.
//
// It is a faithful Go port of the codex-utils-json-to-toml Rust crate. The
// Rust crate exposes a single function, json_to_toml, that maps a
// serde_json::Value onto a toml::Value. Because the Go standard library has no
// TOML value type or encoder, this package provides:
//
//   - [Value], a minimal model of the TOML value variants that mirrors
//     toml::Value (String, Boolean, Integer, Float, Array, Table).
//   - [JSONToTOML], which performs the same variant-by-variant mapping the
//     reference implementation performs.
//   - A small, hand-rolled TOML encoder ([Value.Encode]) so a converted value
//     can be serialized to a TOML document, matching the externally observable
//     behavior of toml::to_string for the values this crate produces.
//
// The conversion is total: every JSON value maps onto exactly one TOML value
// and the function never returns an error, mirroring the infallible Rust
// signature.
package jsontotoml

// Kind enumerates the TOML value variants modeled by [Value]. The variants
// mirror those of the Rust toml::Value enum that the reference crate produces.
type Kind int

const (
	// KindString is a TOML string value.
	KindString Kind = iota
	// KindBoolean is a TOML boolean value.
	KindBoolean
	// KindInteger is a TOML 64-bit signed integer value.
	KindInteger
	// KindFloat is a TOML 64-bit floating point value.
	KindFloat
	// KindArray is a TOML array value.
	KindArray
	// KindTable is a TOML table (map) value.
	KindTable
)

// TableEntry is a single key/value pair within a [Value] of kind
// [KindTable]. Entries are stored as an ordered slice so that the iteration
// order of the source JSON object is preserved, matching the deterministic,
// sorted-key behavior of serde_json's default object map (a BTreeMap) used by
// the reference crate.
type TableEntry struct {
	// Key is the table key.
	Key string
	// Value is the value associated with Key.
	Value Value
}

// Value is an immutable model of a TOML value. The zero Value is an empty
// string. Construct values via the package constructors ([StringValue],
// [BoolValue], [IntValue], [FloatValue], [ArrayValue], [TableValue]) or via
// [JSONToTOML]; do not mutate a Value after construction.
type Value struct {
	kind    Kind
	str     string
	boolean bool
	integer int64
	float   float64
	array   []Value
	table   []TableEntry
}

// StringValue returns a TOML string value.
func StringValue(s string) Value {
	return Value{kind: KindString, str: s}
}

// BoolValue returns a TOML boolean value.
func BoolValue(b bool) Value {
	return Value{kind: KindBoolean, boolean: b}
}

// IntValue returns a TOML integer value.
func IntValue(i int64) Value {
	return Value{kind: KindInteger, integer: i}
}

// FloatValue returns a TOML float value.
func FloatValue(f float64) Value {
	return Value{kind: KindFloat, float: f}
}

// ArrayValue returns a TOML array value. The provided slice is copied so that
// the returned Value does not alias caller-owned storage.
func ArrayValue(items []Value) Value {
	cp := make([]Value, len(items))
	copy(cp, items)
	return Value{kind: KindArray, array: cp}
}

// TableValue returns a TOML table value. The provided entries are copied so
// that the returned Value does not alias caller-owned storage.
func TableValue(entries []TableEntry) Value {
	cp := make([]TableEntry, len(entries))
	copy(cp, entries)
	return Value{kind: KindTable, table: cp}
}

// Kind reports the variant of the value.
func (v Value) Kind() Kind { return v.kind }

// String reports the underlying string. It is only meaningful when
// Kind reports [KindString].
func (v Value) String() string { return v.str }

// Bool reports the underlying boolean. It is only meaningful when Kind reports
// [KindBoolean].
func (v Value) Bool() bool { return v.boolean }

// Int reports the underlying integer. It is only meaningful when Kind reports
// [KindInteger].
func (v Value) Int() int64 { return v.integer }

// Float reports the underlying float. It is only meaningful when Kind reports
// [KindFloat].
func (v Value) Float() float64 { return v.float }

// Array returns a copy of the underlying array elements. It is only
// meaningful when Kind reports [KindArray]. A copy is returned to preserve the
// immutability of the value.
func (v Value) Array() []Value {
	cp := make([]Value, len(v.array))
	copy(cp, v.array)
	return cp
}

// Table returns a copy of the underlying table entries, in insertion order. It
// is only meaningful when Kind reports [KindTable]. A copy is returned to
// preserve the immutability of the value.
func (v Value) Table() []TableEntry {
	cp := make([]TableEntry, len(v.table))
	copy(cp, v.table)
	return cp
}

// Equal reports whether two values are structurally equal. Float comparison
// uses ordinary equality; NaN values are therefore not considered equal,
// matching IEEE-754 semantics.
func (v Value) Equal(other Value) bool {
	if v.kind != other.kind {
		return false
	}
	switch v.kind {
	case KindString:
		return v.str == other.str
	case KindBoolean:
		return v.boolean == other.boolean
	case KindInteger:
		return v.integer == other.integer
	case KindFloat:
		return v.float == other.float
	case KindArray:
		if len(v.array) != len(other.array) {
			return false
		}
		for i := range v.array {
			if !v.array[i].Equal(other.array[i]) {
				return false
			}
		}
		return true
	case KindTable:
		if len(v.table) != len(other.table) {
			return false
		}
		for i := range v.table {
			if v.table[i].Key != other.table[i].Key {
				return false
			}
			if !v.table[i].Value.Equal(other.table[i].Value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
