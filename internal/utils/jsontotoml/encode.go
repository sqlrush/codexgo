package jsontotoml

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrTopLevelNotTable is returned by [Value.Encode] when the value is not a
// table. A TOML document must have a table at its root; scalar, array, and
// other non-table values cannot stand alone as a document.
var ErrTopLevelNotTable = errors.New("jsontotoml: top-level TOML value must be a table")

// Encode renders the value as a TOML document and returns the resulting text.
//
// The root value must be a table ([KindTable]); otherwise [ErrTopLevelNotTable]
// is returned, matching the constraint that a TOML document is a table at its
// root. The encoder is intentionally minimal: it emits dotted-key lines for
// scalar and array values and [table] / nested headers for sub-tables, which is
// sufficient for the values this package produces.
func (v Value) Encode() (string, error) {
	if v.kind != KindTable {
		return "", ErrTopLevelNotTable
	}
	var b strings.Builder
	if err := encodeTable(&b, v, nil); err != nil {
		return "", err
	}
	return b.String(), nil
}

// encodeTable writes the entries of a table. Scalar and array values are
// written first as key/value lines, then sub-tables are written as their own
// [header] sections, mirroring how toml::to_string groups output.
func encodeTable(b *strings.Builder, table Value, path []string) error {
	var subTables []TableEntry
	for _, entry := range table.table {
		if entry.Value.kind == KindTable {
			subTables = append(subTables, entry)
			continue
		}
		rendered, err := encodeInline(entry.Value)
		if err != nil {
			return err
		}
		b.WriteString(encodeKey(entry.Key))
		b.WriteString(" = ")
		b.WriteString(rendered)
		b.WriteString("\n")
	}
	for _, entry := range subTables {
		childPath := append(append([]string(nil), path...), entry.Key)
		b.WriteString("\n[")
		b.WriteString(encodeKeyPath(childPath))
		b.WriteString("]\n")
		if err := encodeTable(b, entry.Value, childPath); err != nil {
			return err
		}
	}
	return nil
}

// encodeInline renders a non-table value as a single TOML token. Tables are not
// supported here because they are emitted as header sections by encodeTable.
func encodeInline(v Value) (string, error) {
	switch v.kind {
	case KindString:
		return encodeString(v.str), nil
	case KindBoolean:
		return strconv.FormatBool(v.boolean), nil
	case KindInteger:
		return strconv.FormatInt(v.integer, 10), nil
	case KindFloat:
		return encodeFloat(v.float), nil
	case KindArray:
		return encodeArray(v)
	case KindTable:
		return encodeInlineTable(v)
	default:
		return "", fmt.Errorf("jsontotoml: unknown value kind %d", v.kind)
	}
}

// encodeArray renders an array as an inline TOML array.
func encodeArray(v Value) (string, error) {
	parts := make([]string, len(v.array))
	for i, item := range v.array {
		rendered, err := encodeInline(item)
		if err != nil {
			return "", err
		}
		parts[i] = rendered
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

// encodeInlineTable renders a table as an inline TOML table. This is used when a
// table appears inside an array, where a [header] section is not possible.
func encodeInlineTable(v Value) (string, error) {
	parts := make([]string, len(v.table))
	for i, entry := range v.table {
		rendered, err := encodeInline(entry.Value)
		if err != nil {
			return "", err
		}
		parts[i] = encodeKey(entry.Key) + " = " + rendered
	}
	return "{ " + strings.Join(parts, ", ") + " }", nil
}

// encodeFloat renders a float using TOML's required formatting. TOML has no
// representation for non-finite floats, so they are emitted using TOML's
// special tokens (inf, -inf, nan), which round-trip with most TOML readers.
func encodeFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	// TOML floats must contain a fractional or exponent part to be parsed as
	// floats rather than integers; add a trailing ".0" when neither is present.
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// encodeKey renders a table key, quoting it as a basic string when it is not a
// bare key.
func encodeKey(key string) string {
	if isBareKey(key) {
		return key
	}
	return encodeString(key)
}

// encodeKeyPath renders a dotted key path used in table headers.
func encodeKeyPath(path []string) string {
	parts := make([]string, len(path))
	for i, segment := range path {
		parts[i] = encodeKey(segment)
	}
	return strings.Join(parts, ".")
}

// isBareKey reports whether key may be written without quoting. Bare keys
// contain only ASCII letters, digits, underscores, and hyphens, and must be
// non-empty.
func isBareKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// encodeString renders a TOML basic string with the required escaping.
func encodeString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				b.WriteString(fmt.Sprintf(`\u%04X`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
