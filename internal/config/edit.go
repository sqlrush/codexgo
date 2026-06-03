package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// SetConfigValue sets a top-level key to value in a config.toml document while
// preserving comments and formatting of the rest of the file. The value is
// encoded as a TOML value (string, number, bool, array, or inline table).
//
// When the key already exists at the top level, only its value bytes are
// rewritten in place. When it does not exist, a new `key = value` line is
// appended. For nested/dotted paths, callers should pass a single segment key
// or use full document re-encoding; this helper targets the common top-level
// scalar/value edits.
func SetConfigValue(contents []byte, key string, value any) ([]byte, error) {
	encoded, err := encodeTomlValue(value)
	if err != nil {
		return nil, err
	}

	loc, found, err := findTopLevelValueRange(contents, key)
	if err != nil {
		return nil, err
	}
	if found {
		out := make([]byte, 0, len(contents)+len(encoded))
		out = append(out, contents[:loc.start]...)
		out = append(out, encoded...)
		out = append(out, contents[loc.end:]...)
		return out, nil
	}

	return appendTopLevelKey(contents, key, encoded), nil
}

type valueRange struct {
	start int
	end   int
}

// findTopLevelValueRange locates the byte range of the value for a top-level
// (root-table) key=value pair, returning found=false if the key is not present
// at the root before the first table header.
func findTopLevelValueRange(contents []byte, key string) (valueRange, bool, error) {
	parser := &unstable.Parser{}
	parser.Reset(contents)
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			// Past the root table; top-level key would have appeared already.
			return valueRange{}, false, nil
		case unstable.KeyValue:
			name, ok := simpleKeyName(parser, node)
			if !ok || name != key {
				continue
			}
			valNode := node.Value()
			r := valNode.Raw
			return valueRange{start: int(r.Offset), end: int(r.Offset + r.Length)}, true, nil
		}
	}
	if err := parser.Error(); err != nil {
		return valueRange{}, false, fmt.Errorf("parse config for edit: %w", err)
	}
	return valueRange{}, false, nil
}

// simpleKeyName returns the dotted key name for a KeyValue node if it is a
// single (non-dotted) key.
func simpleKeyName(parser *unstable.Parser, node *unstable.Node) (string, bool) {
	keys := node.Key()
	var parts []string
	for keys.Next() {
		parts = append(parts, string(parser.Raw(keys.Node().Raw)))
	}
	if len(parts) != 1 {
		return strings.Join(parts, "."), false
	}
	return parts[0], true
}

// appendTopLevelKey appends a new `key = value` line before the first table
// header, or at the end of the document when there are no table headers, so the
// new key remains part of the root table.
func appendTopLevelKey(contents []byte, key string, encoded []byte) []byte {
	insertAt := firstTableHeaderOffset(contents)
	line := []byte(fmt.Sprintf("%s = %s\n", key, encoded))
	if insertAt < 0 {
		out := make([]byte, 0, len(contents)+len(line)+1)
		out = append(out, contents...)
		if len(contents) > 0 && contents[len(contents)-1] != '\n' {
			out = append(out, '\n')
		}
		out = append(out, line...)
		return out
	}
	out := make([]byte, 0, len(contents)+len(line))
	out = append(out, contents[:insertAt]...)
	out = append(out, line...)
	out = append(out, contents[insertAt:]...)
	return out
}

// firstTableHeaderOffset returns the byte offset of the line containing the
// first table or array-table header, or -1 if there are none. The Table node's
// own Raw range is empty, so the first key node's offset is used and walked back
// to the start of its line (which contains the leading `[`).
func firstTableHeaderOffset(contents []byte) int {
	parser := &unstable.Parser{}
	parser.Reset(contents)
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.Table && node.Kind != unstable.ArrayTable {
			continue
		}
		keys := node.Key()
		if !keys.Next() {
			continue
		}
		return lineStart(contents, int(keys.Node().Raw.Offset))
	}
	return -1
}

func lineStart(contents []byte, off int) int {
	if off > len(contents) {
		off = len(contents)
	}
	for off > 0 && contents[off-1] != '\n' {
		off--
	}
	return off
}

// encodeTomlValue encodes a single value to its inline TOML representation,
// suitable for use as the right-hand side of a key=value assignment. Strings are
// emitted as basic (double-quoted) strings to match the conventions of the
// upstream toml_edit-based writer used by Codex.
func encodeTomlValue(value any) ([]byte, error) {
	s, err := encodeInlineValue(value)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func encodeInlineValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return encodeBasicString(v), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			encoded, err := encodeInlineValue(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, encoded)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case []string:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, encodeBasicString(item))
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(v))
		for _, k := range keys {
			encoded, err := encodeInlineValue(v[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, fmt.Sprintf("%s = %s", encodeBareOrQuotedKey(k), encoded))
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported config value type %T", value)
	}
}

// encodeBasicString encodes s as a TOML basic (double-quoted) string with the
// standard escape sequences.
func encodeBasicString(s string) string {
	var b strings.Builder
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
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04X`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// encodeBareOrQuotedKey returns a bare key when it matches the TOML bare-key
// grammar, otherwise a quoted key.
func encodeBareOrQuotedKey(key string) string {
	if key == "" {
		return encodeBasicString(key)
	}
	for _, r := range key {
		isBare := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if !isBare {
			return encodeBasicString(key)
		}
	}
	return key
}
