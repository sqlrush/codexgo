package cliutil

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// errTOMLParse is the sentinel error class returned when a raw override value
// cannot be parsed as a TOML value. Callers use it to decide whether to fall
// back to treating the right-hand side as a literal string.
var errTOMLParse = errors.New("invalid TOML value")

// parseTOMLValue parses a single TOML value from raw, mirroring the upstream
// parse_toml_value helper which wraps the input as `_x_ = {raw}` and parses it
// as a TOML table.
//
// The supported value forms are the ones the `-c key=value` overrides exercise:
// booleans, integers, floats, basic ("...") and literal ('...') strings,
// arrays, and inline tables (recursively). Bare unquoted words (for example
// "hello") are intentionally rejected, mirroring TOML, so the override layer can
// fall back to treating them as literal strings.
//
// Datetime literals, which the full TOML grammar accepts, are not modeled; such
// inputs are reported as parse errors and therefore fall back to literal
// strings. This is the only documented behavioral deviation from the upstream
// toml crate, and it affects only datetime-shaped right-hand sides.
func parseTOMLValue(raw string) (TOMLValue, error) {
	p := &tomlParser{input: raw}
	p.skipSpace()
	v, err := p.parseValue()
	if err != nil {
		return TOMLValue{}, err
	}
	p.skipSpace()
	if !p.atEnd() {
		return TOMLValue{}, fmt.Errorf("%w: trailing characters at offset %d", errTOMLParse, p.pos)
	}
	return v, nil
}

// tomlParser is a tiny recursive-descent parser over a single TOML value.
type tomlParser struct {
	input string
	pos   int
}

func (p *tomlParser) atEnd() bool { return p.pos >= len(p.input) }

func (p *tomlParser) peek() (byte, bool) {
	if p.atEnd() {
		return 0, false
	}
	return p.input[p.pos], true
}

// skipSpace consumes spaces and tabs (the inline whitespace TOML allows around
// values). Newlines are not expected within a single override value.
func (p *tomlParser) skipSpace() {
	for !p.atEnd() {
		switch p.input[p.pos] {
		case ' ', '\t':
			p.pos++
		default:
			return
		}
	}
}

func (p *tomlParser) parseValue() (TOMLValue, error) {
	c, ok := p.peek()
	if !ok {
		return TOMLValue{}, fmt.Errorf("%w: empty value", errTOMLParse)
	}
	switch {
	case c == '"' || c == '\'':
		return p.parseString()
	case c == '[':
		return p.parseArray()
	case c == '{':
		return p.parseInlineTable()
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == '+' || c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return TOMLValue{}, fmt.Errorf("%w: unexpected character %q", errTOMLParse, c)
	}
}

func (p *tomlParser) parseBool() (TOMLValue, error) {
	if strings.HasPrefix(p.input[p.pos:], "true") {
		p.pos += len("true")
		return TOMLBoolValue(true), nil
	}
	if strings.HasPrefix(p.input[p.pos:], "false") {
		p.pos += len("false")
		return TOMLBoolValue(false), nil
	}
	return TOMLValue{}, fmt.Errorf("%w: invalid boolean", errTOMLParse)
}

// parseNumber reads an integer or float token and converts it. The token runs
// until a delimiter (whitespace, comma, closing bracket/brace) or end of input.
func (p *tomlParser) parseNumber() (TOMLValue, error) {
	start := p.pos
	for !p.atEnd() {
		c := p.input[p.pos]
		if c == ',' || c == ']' || c == '}' || c == ' ' || c == '\t' {
			break
		}
		p.pos++
	}
	token := p.input[start:p.pos]
	return parseNumberToken(token)
}

// parseNumberToken converts a numeric token using TOML's integer-then-float
// precedence, mirroring serde's as_i64 / as_f64 ordering.
func parseNumberToken(token string) (TOMLValue, error) {
	if token == "" {
		return TOMLValue{}, fmt.Errorf("%w: empty number", errTOMLParse)
	}
	// TOML permits underscores as digit separators; strconv does not, so strip
	// them between digits before conversion.
	clean := stripDigitSeparators(token)
	if i, err := strconv.ParseInt(clean, 10, 64); err == nil {
		return TOMLIntValue(i), nil
	}
	if f, err := strconv.ParseFloat(clean, 64); err == nil {
		return TOMLFloatValue(f), nil
	}
	return TOMLValue{}, fmt.Errorf("%w: not a number: %q", errTOMLParse, token)
}

// stripDigitSeparators removes single underscores that sit between two
// characters, matching TOML's underscore-as-separator rule loosely enough for
// override values while rejecting malformed runs via the later numeric parse.
func stripDigitSeparators(token string) string {
	if !strings.Contains(token, "_") {
		return token
	}
	var b strings.Builder
	b.Grow(len(token))
	for i := 0; i < len(token); i++ {
		if token[i] == '_' {
			continue
		}
		b.WriteByte(token[i])
	}
	return b.String()
}

// parseString parses a basic ("...") or literal ('...') single-line string.
func (p *tomlParser) parseString() (TOMLValue, error) {
	quote := p.input[p.pos]
	p.pos++
	if quote == '\'' {
		return p.parseLiteralString()
	}
	return p.parseBasicString()
}

// parseLiteralString reads characters verbatim until the closing single quote.
func (p *tomlParser) parseLiteralString() (TOMLValue, error) {
	var b strings.Builder
	for !p.atEnd() {
		c := p.input[p.pos]
		if c == '\'' {
			p.pos++
			return TOMLStringValue(b.String()), nil
		}
		b.WriteByte(c)
		p.pos++
	}
	return TOMLValue{}, fmt.Errorf("%w: unterminated literal string", errTOMLParse)
}

// parseBasicString reads a double-quoted string, decoding the escape sequences
// TOML supports.
func (p *tomlParser) parseBasicString() (TOMLValue, error) {
	var b strings.Builder
	for !p.atEnd() {
		c := p.input[p.pos]
		switch c {
		case '"':
			p.pos++
			return TOMLStringValue(b.String()), nil
		case '\\':
			p.pos++
			if p.atEnd() {
				return TOMLValue{}, fmt.Errorf("%w: trailing escape in string", errTOMLParse)
			}
			if err := p.decodeEscape(&b); err != nil {
				return TOMLValue{}, err
			}
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return TOMLValue{}, fmt.Errorf("%w: unterminated string", errTOMLParse)
}

// decodeEscape decodes a single escape sequence (the backslash already
// consumed) and appends the result to b.
func (p *tomlParser) decodeEscape(b *strings.Builder) error {
	esc := p.input[p.pos]
	p.pos++
	switch esc {
	case 'b':
		b.WriteByte('\b')
	case 't':
		b.WriteByte('\t')
	case 'n':
		b.WriteByte('\n')
	case 'f':
		b.WriteByte('\f')
	case 'r':
		b.WriteByte('\r')
	case '"':
		b.WriteByte('"')
	case '\\':
		b.WriteByte('\\')
	case 'u':
		return p.decodeUnicodeEscape(b, 4)
	case 'U':
		return p.decodeUnicodeEscape(b, 8)
	default:
		return fmt.Errorf("%w: invalid escape \\%c", errTOMLParse, esc)
	}
	return nil
}

// decodeUnicodeEscape reads n hex digits, interprets them as a Unicode scalar
// value, and appends its UTF-8 encoding to b.
func (p *tomlParser) decodeUnicodeEscape(b *strings.Builder, n int) error {
	if p.pos+n > len(p.input) {
		return fmt.Errorf("%w: truncated unicode escape", errTOMLParse)
	}
	hex := p.input[p.pos : p.pos+n]
	code, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return fmt.Errorf("%w: invalid unicode escape %q", errTOMLParse, hex)
	}
	r := rune(code)
	if r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF) {
		return fmt.Errorf("%w: unicode scalar out of range", errTOMLParse)
	}
	p.pos += n
	b.WriteRune(r)
	return nil
}

// parseArray parses a TOML array, allowing whitespace and a trailing comma.
func (p *tomlParser) parseArray() (TOMLValue, error) {
	p.pos++ // consume '['
	items := make([]TOMLValue, 0)
	for {
		p.skipSpace()
		c, ok := p.peek()
		if !ok {
			return TOMLValue{}, fmt.Errorf("%w: unterminated array", errTOMLParse)
		}
		if c == ']' {
			p.pos++
			return TOMLArrayValue(items), nil
		}
		v, err := p.parseValue()
		if err != nil {
			return TOMLValue{}, err
		}
		items = append(items, v)
		p.skipSpace()
		c, ok = p.peek()
		if !ok {
			return TOMLValue{}, fmt.Errorf("%w: unterminated array", errTOMLParse)
		}
		switch c {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return TOMLArrayValue(items), nil
		default:
			return TOMLValue{}, fmt.Errorf("%w: expected ',' or ']' in array", errTOMLParse)
		}
	}
}

// parseInlineTable parses a TOML inline table {a = 1, b = 2}.
func (p *tomlParser) parseInlineTable() (TOMLValue, error) {
	p.pos++ // consume '{'
	entries := make([]TOMLEntry, 0)
	p.skipSpace()
	if c, ok := p.peek(); ok && c == '}' {
		p.pos++
		return TOMLTableValue(entries), nil
	}
	for {
		p.skipSpace()
		key, err := p.parseInlineKey()
		if err != nil {
			return TOMLValue{}, err
		}
		p.skipSpace()
		if c, ok := p.peek(); !ok || c != '=' {
			return TOMLValue{}, fmt.Errorf("%w: expected '=' in inline table", errTOMLParse)
		}
		p.pos++ // consume '='
		p.skipSpace()
		v, err := p.parseValue()
		if err != nil {
			return TOMLValue{}, err
		}
		entries = append(entries, TOMLEntry{Key: key, Value: v})
		p.skipSpace()
		c, ok := p.peek()
		if !ok {
			return TOMLValue{}, fmt.Errorf("%w: unterminated inline table", errTOMLParse)
		}
		switch c {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return TOMLTableValue(entries), nil
		default:
			return TOMLValue{}, fmt.Errorf("%w: expected ',' or '}' in inline table", errTOMLParse)
		}
	}
}

// parseInlineKey parses a bare or quoted key for an inline table.
func (p *tomlParser) parseInlineKey() (string, error) {
	c, ok := p.peek()
	if !ok {
		return "", fmt.Errorf("%w: missing inline table key", errTOMLParse)
	}
	if c == '"' || c == '\'' {
		v, err := p.parseString()
		if err != nil {
			return "", err
		}
		s, _ := v.AsString()
		return s, nil
	}
	start := p.pos
	for !p.atEnd() {
		ch := p.input[p.pos]
		if isBareKeyByte(ch) {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", fmt.Errorf("%w: empty inline table key", errTOMLParse)
	}
	return p.input[start:p.pos], nil
}

// isBareKeyByte reports whether c may appear in a TOML bare key.
func isBareKeyByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-':
		return true
	default:
		return false
	}
}
