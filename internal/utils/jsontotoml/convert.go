package jsontotoml

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
)

// JSONToTOML converts a decoded JSON value into a semantically equivalent TOML
// [Value]. It is the Go analogue of the reference crate's json_to_toml.
//
// The accepted input is a value produced by decoding JSON with the standard
// library, for example via [encoding/json.Unmarshal] into an any or via a
// [encoding/json.Decoder]. The mapping is:
//
//   - nil (JSON null) -> empty TOML string, matching the reference behavior of
//     mapping Null onto an empty string.
//   - bool            -> TOML boolean.
//   - json.Number     -> TOML integer when the value parses as a signed 64-bit
//     integer; otherwise a TOML float when it parses as a finite float;
//     otherwise a TOML string of the original token. This reproduces serde_json's
//     as_i64 / as_f64 fallback chain exactly.
//   - float64         -> TOML integer when the value is an exact, in-range
//     integer; otherwise a TOML float. (encoding/json decodes all JSON numbers
//     as float64 unless UseNumber is enabled; this branch keeps that common
//     path faithful.)
//   - integer kinds   -> TOML integer.
//   - string          -> TOML string.
//   - []any           -> TOML array (recursive).
//   - map[string]any  -> TOML table (recursive), keys in sorted order to mirror
//     serde_json's default sorted object map.
//   - map[string]json.RawMessage and json.RawMessage are decoded and converted
//     recursively for convenience.
//
// Any other input type is converted to its string form so the function remains
// total and never returns an error, mirroring the infallible Rust signature.
func JSONToTOML(v any) Value {
	switch val := v.(type) {
	case nil:
		return StringValue("")
	case bool:
		return BoolValue(val)
	case json.Number:
		return numberToTOML(string(val))
	case string:
		return StringValue(val)
	case json.RawMessage:
		return rawToTOML(val)
	case []byte:
		// Treat raw JSON bytes the same as json.RawMessage.
		return rawToTOML(val)
	case []any:
		return sliceToTOML(val)
	case []json.RawMessage:
		items := make([]Value, len(val))
		for i, item := range val {
			items[i] = rawToTOML(item)
		}
		return Value{kind: KindArray, array: items}
	case map[string]any:
		return mapToTOML(val)
	case map[string]json.RawMessage:
		return rawMapToTOML(val)
	case float64:
		return floatToTOML(val)
	case float32:
		return floatToTOML(float64(val))
	case int:
		return IntValue(int64(val))
	case int8:
		return IntValue(int64(val))
	case int16:
		return IntValue(int64(val))
	case int32:
		return IntValue(int64(val))
	case int64:
		return IntValue(val)
	case uint:
		return uintToTOML(uint64(val))
	case uint8:
		return IntValue(int64(val))
	case uint16:
		return IntValue(int64(val))
	case uint32:
		return IntValue(int64(val))
	case uint64:
		return uintToTOML(val)
	default:
		// Fallback: stringify unknown values so the function stays total.
		return StringValue(stringifyUnknown(val))
	}
}

// numberToTOML replicates serde_json's number resolution: prefer a signed
// 64-bit integer, then a finite float, then fall back to the original token as
// a string.
func numberToTOML(token string) Value {
	if i, err := strconv.ParseInt(token, 10, 64); err == nil {
		return IntValue(i)
	}
	if f, err := strconv.ParseFloat(token, 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return FloatValue(f)
	}
	return StringValue(token)
}

// floatToTOML maps a float64 onto an integer when it is an exact, in-range
// whole number, otherwise onto a float. This mirrors as_i64 taking precedence
// over as_f64 for JSON numbers that happen to be integral.
func floatToTOML(f float64) Value {
	if isExactInt64(f) {
		return IntValue(int64(f))
	}
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return StringValue(strconv.FormatFloat(f, 'g', -1, 64))
	}
	return FloatValue(f)
}

// uintToTOML maps an unsigned integer onto a TOML integer when it fits in an
// int64, otherwise onto a string, mirroring the as_i64 / fallback chain for
// large unsigned numbers.
func uintToTOML(u uint64) Value {
	if u <= math.MaxInt64 {
		return IntValue(int64(u))
	}
	return StringValue(strconv.FormatUint(u, 10))
}

// isExactInt64 reports whether f is a whole number that fits in an int64
// without loss.
func isExactInt64(f float64) bool {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return false
	}
	if f != math.Trunc(f) {
		return false
	}
	// math.MaxInt64 is not exactly representable as a float64; use a strict
	// upper bound (2^63) and a lower bound of -2^63.
	return f >= math.MinInt64 && f < 9.223372036854776e18
}

// sliceToTOML converts a JSON array.
func sliceToTOML(arr []any) Value {
	items := make([]Value, len(arr))
	for i, item := range arr {
		items[i] = JSONToTOML(item)
	}
	return Value{kind: KindArray, array: items}
}

// mapToTOML converts a JSON object, sorting keys to mirror serde_json's
// deterministic object ordering.
func mapToTOML(m map[string]any) Value {
	keys := sortedKeys(m)
	entries := make([]TableEntry, len(keys))
	for i, k := range keys {
		entries[i] = TableEntry{Key: k, Value: JSONToTOML(m[k])}
	}
	return Value{kind: KindTable, table: entries}
}

// rawMapToTOML converts a map of raw JSON messages.
func rawMapToTOML(m map[string]json.RawMessage) Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]TableEntry, len(keys))
	for i, k := range keys {
		entries[i] = TableEntry{Key: k, Value: rawToTOML(m[k])}
	}
	return Value{kind: KindTable, table: entries}
}

// rawToTOML decodes a raw JSON message using json.Number semantics and then
// converts it, so number precision matches the reference implementation.
func rawToTOML(raw []byte) Value {
	if len(raw) == 0 {
		return StringValue("")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		// Not decodable JSON: treat the raw bytes as a string so the function
		// stays total.
		return StringValue(string(raw))
	}
	return JSONToTOML(decoded)
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// stringifyUnknown renders an unrecognized value via JSON marshaling, falling
// back to a fixed token if marshaling fails.
func stringifyUnknown(v any) string {
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return ""
}
