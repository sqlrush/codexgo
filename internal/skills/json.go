package skills

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// parseJSONValue parses a JSON value into a yamlValue starting at runes[pos],
// returning the value and the index after it. serde_yaml accepts JSON, so the
// optional agents/openai.yaml sidecar may be written as JSON; this mirrors that
// acceptance using the same yamlValue shape as the YAML path.
func parseJSONValue(runes []rune, pos int) (yamlValue, int, error) {
	pos = skipJSONSpace(runes, pos)
	if pos >= len(runes) {
		return yamlValue{}, pos, fmt.Errorf("unexpected end of JSON")
	}
	switch runes[pos] {
	case '{':
		return parseJSONObject(runes, pos)
	case '[':
		return parseJSONArray(runes, pos)
	case '"':
		s, next, err := parseJSONString(runes, pos)
		if err != nil {
			return yamlValue{}, pos, err
		}
		return yamlValue{kind: yamlString, str: s}, next, nil
	default:
		return parseJSONLiteral(runes, pos)
	}
}

func parseJSONObject(runes []rune, pos int) (yamlValue, int, error) {
	pos++ // consume '{'
	result := yamlValue{kind: yamlMapping}
	pos = skipJSONSpace(runes, pos)
	if pos < len(runes) && runes[pos] == '}' {
		return result, pos + 1, nil
	}
	for {
		pos = skipJSONSpace(runes, pos)
		if pos >= len(runes) || runes[pos] != '"' {
			return yamlValue{}, pos, fmt.Errorf("expected JSON object key")
		}
		key, next, err := parseJSONString(runes, pos)
		if err != nil {
			return yamlValue{}, pos, err
		}
		pos = skipJSONSpace(runes, next)
		if pos >= len(runes) || runes[pos] != ':' {
			return yamlValue{}, pos, fmt.Errorf("expected ':' in JSON object")
		}
		pos++
		value, next, err := parseJSONValue(runes, pos)
		if err != nil {
			return yamlValue{}, pos, err
		}
		result.mapping = append(result.mapping, yamlPair{key: key, value: value})
		pos = skipJSONSpace(runes, next)
		if pos >= len(runes) {
			return yamlValue{}, pos, fmt.Errorf("unterminated JSON object")
		}
		if runes[pos] == ',' {
			pos++
			continue
		}
		if runes[pos] == '}' {
			return result, pos + 1, nil
		}
		return yamlValue{}, pos, fmt.Errorf("expected ',' or '}' in JSON object")
	}
}

func parseJSONArray(runes []rune, pos int) (yamlValue, int, error) {
	pos++ // consume '['
	result := yamlValue{kind: yamlSequence}
	pos = skipJSONSpace(runes, pos)
	if pos < len(runes) && runes[pos] == ']' {
		return result, pos + 1, nil
	}
	for {
		value, next, err := parseJSONValue(runes, pos)
		if err != nil {
			return yamlValue{}, pos, err
		}
		result.sequence = append(result.sequence, value)
		pos = skipJSONSpace(runes, next)
		if pos >= len(runes) {
			return yamlValue{}, pos, fmt.Errorf("unterminated JSON array")
		}
		if runes[pos] == ',' {
			pos++
			continue
		}
		if runes[pos] == ']' {
			return result, pos + 1, nil
		}
		return yamlValue{}, pos, fmt.Errorf("expected ',' or ']' in JSON array")
	}
}

func parseJSONString(runes []rune, pos int) (string, int, error) {
	pos++ // consume opening quote
	var b strings.Builder
	for pos < len(runes) {
		c := runes[pos]
		switch c {
		case '"':
			return b.String(), pos + 1, nil
		case '\\':
			pos++
			if pos >= len(runes) {
				return "", pos, fmt.Errorf("trailing escape in JSON string")
			}
			switch runes[pos] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '/':
				b.WriteByte('/')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if pos+4 >= len(runes) {
					return "", pos, fmt.Errorf("invalid \\u escape")
				}
				code, err := strconv.ParseUint(string(runes[pos+1:pos+5]), 16, 32)
				if err != nil {
					return "", pos, fmt.Errorf("invalid \\u escape: %w", err)
				}
				b.WriteRune(rune(code))
				pos += 4
			default:
				return "", pos, fmt.Errorf("invalid JSON escape \\%c", runes[pos])
			}
		default:
			b.WriteRune(c)
		}
		pos++
	}
	return "", pos, fmt.Errorf("unterminated JSON string")
}

func parseJSONLiteral(runes []rune, pos int) (yamlValue, int, error) {
	start := pos
	for pos < len(runes) {
		c := runes[pos]
		if c == ',' || c == '}' || c == ']' || unicode.IsSpace(c) {
			break
		}
		pos++
	}
	token := string(runes[start:pos])
	switch token {
	case "true":
		return yamlValue{kind: yamlBool, boolean: true}, pos, nil
	case "false":
		return yamlValue{kind: yamlBool, boolean: false}, pos, nil
	case "null":
		return yamlValue{kind: yamlNull}, pos, nil
	}
	if n, err := strconv.ParseInt(token, 10, 64); err == nil {
		return yamlValue{kind: yamlInt, integer: n}, pos, nil
	}
	// Non-integer numbers (floats) are not used by the skills sidecar; surface
	// them as string scalars so they round-trip without crashing.
	if _, err := strconv.ParseFloat(token, 64); err == nil {
		return yamlValue{kind: yamlString, str: token}, pos, nil
	}
	return yamlValue{}, start, fmt.Errorf("invalid JSON literal %q", token)
}

func skipJSONSpace(runes []rune, pos int) int {
	for pos < len(runes) && unicode.IsSpace(runes[pos]) {
		pos++
	}
	return pos
}
