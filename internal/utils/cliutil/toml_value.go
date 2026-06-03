package cliutil

// TOMLKind enumerates the TOML value variants modeled by [TOMLValue].
//
// The variants mirror the subset of toml::Value that the `-c key=value`
// override machinery produces and consumes: strings, booleans, integers,
// floats, arrays, and tables.
type TOMLKind int

const (
	// TOMLString is a TOML string value.
	TOMLString TOMLKind = iota
	// TOMLBool is a TOML boolean value.
	TOMLBool
	// TOMLInteger is a TOML 64-bit signed integer value.
	TOMLInteger
	// TOMLFloat is a TOML 64-bit floating point value.
	TOMLFloat
	// TOMLArray is a TOML array value.
	TOMLArray
	// TOMLTable is a TOML table (ordered key/value map) value.
	TOMLTable
)

// TOMLEntry is a single key/value pair within a [TOMLValue] of kind
// [TOMLTable]. Entries are stored as an ordered slice so that insertion order
// is deterministic and stable.
type TOMLEntry struct {
	// Key is the table key.
	Key string
	// Value is the value associated with Key.
	Value TOMLValue
}

// TOMLValue is an immutable model of a TOML value sufficient for representing
// configuration override values and the configuration tree they are applied to.
//
// The zero TOMLValue is an empty string. Construct values via the package
// constructors ([TOMLStringValue], [TOMLBoolValue], [TOMLIntValue],
// [TOMLFloatValue], [TOMLArrayValue], [TOMLTableValue]); do not mutate a
// TOMLValue after construction.
type TOMLValue struct {
	kind    TOMLKind
	str     string
	boolean bool
	integer int64
	float   float64
	array   []TOMLValue
	table   []TOMLEntry
}

// TOMLStringValue returns a TOML string value.
func TOMLStringValue(s string) TOMLValue {
	return TOMLValue{kind: TOMLString, str: s}
}

// TOMLBoolValue returns a TOML boolean value.
func TOMLBoolValue(b bool) TOMLValue {
	return TOMLValue{kind: TOMLBool, boolean: b}
}

// TOMLIntValue returns a TOML integer value.
func TOMLIntValue(i int64) TOMLValue {
	return TOMLValue{kind: TOMLInteger, integer: i}
}

// TOMLFloatValue returns a TOML float value.
func TOMLFloatValue(f float64) TOMLValue {
	return TOMLValue{kind: TOMLFloat, float: f}
}

// TOMLArrayValue returns a TOML array value. The provided slice is copied so the
// returned value does not alias caller-owned storage.
func TOMLArrayValue(items []TOMLValue) TOMLValue {
	cp := make([]TOMLValue, len(items))
	copy(cp, items)
	return TOMLValue{kind: TOMLArray, array: cp}
}

// TOMLTableValue returns a TOML table value. The provided entries are copied so
// the returned value does not alias caller-owned storage.
func TOMLTableValue(entries []TOMLEntry) TOMLValue {
	cp := make([]TOMLEntry, len(entries))
	copy(cp, entries)
	return TOMLValue{kind: TOMLTable, table: cp}
}

// EmptyTOMLTable returns an empty TOML table value.
func EmptyTOMLTable() TOMLValue {
	return TOMLValue{kind: TOMLTable}
}

// Kind reports the variant of the value.
func (v TOMLValue) Kind() TOMLKind { return v.kind }

// AsString reports the underlying string and whether the value is a string.
func (v TOMLValue) AsString() (string, bool) {
	if v.kind == TOMLString {
		return v.str, true
	}
	return "", false
}

// AsBool reports the underlying boolean and whether the value is a boolean.
func (v TOMLValue) AsBool() (bool, bool) {
	if v.kind == TOMLBool {
		return v.boolean, true
	}
	return false, false
}

// AsInteger reports the underlying integer and whether the value is an integer.
func (v TOMLValue) AsInteger() (int64, bool) {
	if v.kind == TOMLInteger {
		return v.integer, true
	}
	return 0, false
}

// AsFloat reports the underlying float and whether the value is a float.
func (v TOMLValue) AsFloat() (float64, bool) {
	if v.kind == TOMLFloat {
		return v.float, true
	}
	return 0, false
}

// AsArray returns a copy of the underlying array elements and whether the value
// is an array. A copy is returned to preserve immutability.
func (v TOMLValue) AsArray() ([]TOMLValue, bool) {
	if v.kind != TOMLArray {
		return nil, false
	}
	cp := make([]TOMLValue, len(v.array))
	copy(cp, v.array)
	return cp, true
}

// AsTable returns a copy of the underlying table entries, in insertion order,
// and whether the value is a table. A copy is returned to preserve immutability.
func (v TOMLValue) AsTable() ([]TOMLEntry, bool) {
	if v.kind != TOMLTable {
		return nil, false
	}
	cp := make([]TOMLEntry, len(v.table))
	copy(cp, v.table)
	return cp, true
}

// Get returns the value associated with key in a table value, and whether the
// key was present. It always reports false for non-table values.
func (v TOMLValue) Get(key string) (TOMLValue, bool) {
	if v.kind != TOMLTable {
		return TOMLValue{}, false
	}
	for _, e := range v.table {
		if e.Key == key {
			return e.Value, true
		}
	}
	return TOMLValue{}, false
}

// withTableEntry returns a new table value with key set to value, preserving
// the order of existing keys and appending new keys at the end. The receiver is
// not mutated.
func (v TOMLValue) withTableEntry(key string, value TOMLValue) TOMLValue {
	entries := make([]TOMLEntry, 0, len(v.table)+1)
	replaced := false
	if v.kind == TOMLTable {
		for _, e := range v.table {
			if e.Key == key {
				entries = append(entries, TOMLEntry{Key: key, Value: value})
				replaced = true
			} else {
				entries = append(entries, e)
			}
		}
	}
	if !replaced {
		entries = append(entries, TOMLEntry{Key: key, Value: value})
	}
	return TOMLValue{kind: TOMLTable, table: entries}
}

// Equal reports whether two values are structurally equal. Float comparison
// uses ordinary equality; NaN values are therefore not considered equal.
func (v TOMLValue) Equal(other TOMLValue) bool {
	if v.kind != other.kind {
		return false
	}
	switch v.kind {
	case TOMLString:
		return v.str == other.str
	case TOMLBool:
		return v.boolean == other.boolean
	case TOMLInteger:
		return v.integer == other.integer
	case TOMLFloat:
		return v.float == other.float
	case TOMLArray:
		if len(v.array) != len(other.array) {
			return false
		}
		for i := range v.array {
			if !v.array[i].Equal(other.array[i]) {
				return false
			}
		}
		return true
	case TOMLTable:
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
