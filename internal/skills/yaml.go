package skills

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// This file implements a focused YAML subset parser sufficient for the shapes
// Codex skills produce: SKILL.md front-matter (name/description/metadata.short-
// description) and the optional agents/openai.yaml metadata sidecar
// (interface/dependencies/policy). The Rust loader parses these with
// serde_yaml; we mirror the subset of YAML 1.x those structures use.
//
// Supported:
//   - block mappings (nested by indentation)
//   - block sequences ("- " items, including "- key: value" inline maps)
//   - plain, single-quoted, and double-quoted scalars
//   - block scalars: literal ("|", "|-", "|+") and folded (">", ">-", ">+")
//   - JSON documents (serde_yaml also accepts JSON; the sidecar may be JSON)
//   - booleans, integers, and null for typed fields
//
// Unsupported YAML features (anchors, flow collections beyond JSON, complex
// keys, multi-document streams) are not used by skills and are rejected.

// yamlValue is a parsed YAML node: either a scalar (string/bool/int/null),
// a mapping, or a sequence.
type yamlValue struct {
	// kind discriminates the variant.
	kind yamlKind
	// str holds scalar string values.
	str string
	// boolean / integer hold typed scalar values when kind is yamlBool / yamlInt.
	boolean bool
	integer int64
	// mapping preserves insertion order of keys.
	mapping []yamlPair
	// sequence holds list elements.
	sequence []yamlValue
}

type yamlKind int

const (
	yamlNull yamlKind = iota
	yamlString
	yamlBool
	yamlInt
	yamlMapping
	yamlSequence
)

type yamlPair struct {
	key   string
	value yamlValue
}

// errInvalidYaml is the sentinel wrapped by all parse failures, mirroring the
// Rust `SkillParseError::InvalidYaml` branch surfaced to callers.
var errInvalidYaml = errors.New("invalid YAML")

// get returns the value for key in a mapping, or ok=false otherwise.
func (v yamlValue) get(key string) (yamlValue, bool) {
	if v.kind != yamlMapping {
		return yamlValue{}, false
	}
	for _, pair := range v.mapping {
		if pair.key == key {
			return pair.value, true
		}
	}
	return yamlValue{}, false
}

// asString returns the scalar as a string when the node is a string scalar.
// Booleans, ints, and null do not coerce to string (matching serde's typed
// Option<String> deserialization, which rejects non-string scalars).
func (v yamlValue) asString() (string, bool) {
	if v.kind == yamlString {
		return v.str, true
	}
	return "", false
}

// asBool returns the scalar as a bool when the node is a boolean scalar.
func (v yamlValue) asBool() (bool, bool) {
	if v.kind == yamlBool {
		return v.boolean, true
	}
	return false, false
}

// parseYAML parses a YAML (or JSON) document into a yamlValue. An empty document
// yields a null node.
func parseYAML(input string) (yamlValue, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return yamlValue{kind: yamlNull}, nil
	}
	// serde_yaml accepts JSON; detect a JSON document by its opening brace or
	// bracket and parse it with the JSON-compatible scalar rules.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		value, _, err := parseJSONValue([]rune(trimmed), 0)
		if err != nil {
			return yamlValue{}, fmt.Errorf("%w: %v", errInvalidYaml, err)
		}
		return value, nil
	}

	lines := splitYAMLLines(input)
	parser := &yamlParser{lines: lines}
	value, err := parser.parseBlock(0)
	if err != nil {
		return yamlValue{}, fmt.Errorf("%w: %v", errInvalidYaml, err)
	}
	if !parser.atEnd() {
		return yamlValue{}, fmt.Errorf("%w: unexpected content at line %d", errInvalidYaml, parser.index+1)
	}
	return value, nil
}

// yamlLine is a single significant (non-blank, non-comment) line with its
// leading indentation precomputed.
type yamlLine struct {
	indent  int
	content string
}

// splitYAMLLines tokenizes the document into significant lines, dropping blank
// lines and full-line comments. Lines inside block scalars are handled by the
// block-scalar reader, so here we keep raw content for them too; the reader
// re-derives indentation from the original text.
func splitYAMLLines(input string) []yamlLine {
	raw := strings.Split(input, "\n")
	out := make([]yamlLine, 0, len(raw))
	for _, line := range raw {
		stripped := strings.TrimRight(line, "\r")
		trimmed := strings.TrimLeft(stripped, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, yamlLine{indent: -1, content: ""})
			continue
		}
		out = append(out, yamlLine{indent: len(stripped) - len(trimmed), content: stripped})
	}
	return out
}

type yamlParser struct {
	lines []yamlLine
	index int
}

func (p *yamlParser) atEnd() bool {
	for p.index < len(p.lines) {
		if p.lines[p.index].indent < 0 {
			p.index++
			continue
		}
		return false
	}
	return true
}

// peek returns the next significant line without consuming it.
func (p *yamlParser) peek() (yamlLine, bool) {
	for p.index < len(p.lines) {
		if p.lines[p.index].indent < 0 {
			p.index++
			continue
		}
		return p.lines[p.index], true
	}
	return yamlLine{}, false
}

// parseBlock parses a mapping or sequence whose items are indented at exactly
// minIndent.
func (p *yamlParser) parseBlock(minIndent int) (yamlValue, error) {
	line, ok := p.peek()
	if !ok {
		return yamlValue{kind: yamlNull}, nil
	}
	if line.indent < minIndent {
		return yamlValue{kind: yamlNull}, nil
	}
	if isSequenceItem(line.content) {
		return p.parseSequence(line.indent)
	}
	return p.parseMapping(line.indent)
}

func (p *yamlParser) parseMapping(indent int) (yamlValue, error) {
	result := yamlValue{kind: yamlMapping}
	for {
		line, ok := p.peek()
		if !ok || line.indent != indent || isSequenceItem(line.content) {
			break
		}
		body := strings.TrimLeft(line.content, " ")
		key, rest, err := splitMappingKey(body)
		if err != nil {
			return yamlValue{}, err
		}
		p.index++

		rest = strings.TrimSpace(rest)
		var value yamlValue
		if rest == "" {
			value, err = p.parseNestedValue(indent)
			if err != nil {
				return yamlValue{}, err
			}
		} else if marker, chomp, isBlock := blockScalarHeader(rest); isBlock {
			value = yamlValue{kind: yamlString, str: p.readBlockScalar(indent, marker, chomp)}
		} else {
			value, err = parseScalar(rest)
			if err != nil {
				return yamlValue{}, err
			}
		}
		result.mapping = append(result.mapping, yamlPair{key: key, value: value})
	}
	return result, nil
}

// parseNestedValue parses the value that follows a "key:" with nothing on the
// same line: either a deeper block mapping/sequence or a null.
func (p *yamlParser) parseNestedValue(parentIndent int) (yamlValue, error) {
	line, ok := p.peek()
	if !ok || line.indent <= parentIndent {
		return yamlValue{kind: yamlNull}, nil
	}
	if isSequenceItem(line.content) {
		// Block sequences may be indented at the parent level or deeper.
		return p.parseSequence(line.indent)
	}
	return p.parseMapping(line.indent)
}

func (p *yamlParser) parseSequence(indent int) (yamlValue, error) {
	result := yamlValue{kind: yamlSequence}
	for {
		line, ok := p.peek()
		if !ok || line.indent != indent || !isSequenceItem(line.content) {
			break
		}
		body := strings.TrimLeft(line.content, " ")
		item := strings.TrimSpace(body[1:]) // drop leading '-'
		p.index++

		if item == "" {
			// Nested block under the dash.
			value, err := p.parseBlock(indent + 1)
			if err != nil {
				return yamlValue{}, err
			}
			result.sequence = append(result.sequence, value)
			continue
		}

		// "- key: value" introduces an inline mapping whose remaining keys are
		// indented to align with the text after the dash.
		if key, rest, err := tryInlineMappingKey(item); err == nil && key != "" {
			itemIndent := indent + (len(body) - len(strings.TrimLeft(body[1:], " ")) - 1) + 1
			value, err := p.parseInlineMapping(itemIndent, key, rest)
			if err != nil {
				return yamlValue{}, err
			}
			result.sequence = append(result.sequence, value)
			continue
		}

		value, err := parseScalar(item)
		if err != nil {
			return yamlValue{}, err
		}
		result.sequence = append(result.sequence, value)
	}
	return result, nil
}

// parseInlineMapping parses a mapping that began on a sequence-item line ("-
// key: value"); firstKey/firstRest are the key/value already split from that
// line, and itemIndent is the column at which subsequent keys appear.
func (p *yamlParser) parseInlineMapping(itemIndent int, firstKey, firstRest string) (yamlValue, error) {
	result := yamlValue{kind: yamlMapping}
	firstRest = strings.TrimSpace(firstRest)
	var firstValue yamlValue
	var err error
	if firstRest == "" {
		firstValue, err = p.parseNestedValue(itemIndent)
		if err != nil {
			return yamlValue{}, err
		}
	} else {
		firstValue, err = parseScalar(firstRest)
		if err != nil {
			return yamlValue{}, err
		}
	}
	result.mapping = append(result.mapping, yamlPair{key: firstKey, value: firstValue})

	for {
		line, ok := p.peek()
		if !ok || line.indent != itemIndent || isSequenceItem(line.content) {
			break
		}
		body := strings.TrimLeft(line.content, " ")
		key, rest, err := splitMappingKey(body)
		if err != nil {
			return yamlValue{}, err
		}
		p.index++
		rest = strings.TrimSpace(rest)
		var value yamlValue
		if rest == "" {
			value, err = p.parseNestedValue(itemIndent)
			if err != nil {
				return yamlValue{}, err
			}
		} else {
			value, err = parseScalar(rest)
			if err != nil {
				return yamlValue{}, err
			}
		}
		result.mapping = append(result.mapping, yamlPair{key: key, value: value})
	}
	return result, nil
}

// readBlockScalar consumes the indented lines of a literal/folded block scalar
// following a "key: |" header at the given parent indent.
func (p *yamlParser) readBlockScalar(parentIndent int, folded bool, chomp byte) string {
	var collected []string
	blockIndent := -1
	for p.index < len(p.lines) {
		line := p.lines[p.index]
		if line.indent < 0 {
			collected = append(collected, "")
			p.index++
			continue
		}
		if line.indent <= parentIndent {
			break
		}
		if blockIndent < 0 {
			blockIndent = line.indent
		}
		content := line.content
		if len(content) >= blockIndent {
			content = content[blockIndent:]
		} else {
			content = strings.TrimLeft(content, " ")
		}
		collected = append(collected, content)
		p.index++
	}
	return assembleBlockScalar(collected, folded, chomp)
}

// assembleBlockScalar joins block-scalar lines per the literal/folded and
// chomping rules, then applies the chomping indicator.
func assembleBlockScalar(lines []string, folded bool, chomp byte) string {
	// Drop trailing empty lines for clip/strip; keep for keep.
	var text string
	if folded {
		var b strings.Builder
		for i, line := range lines {
			if i > 0 {
				if line == "" || lines[i-1] == "" {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
			}
			b.WriteString(line)
		}
		text = b.String()
	} else {
		text = strings.Join(lines, "\n")
	}

	switch chomp {
	case '-': // strip: remove all trailing newlines
		text = strings.TrimRight(text, "\n")
	case '+': // keep: leave as-is, ensure a trailing newline for content
		text = strings.TrimRight(text, "\n") + strings.Repeat("\n", trailingNewlines(lines))
	default: // clip: single trailing newline
		text = strings.TrimRight(text, "\n")
	}
	return text
}

func trailingNewlines(lines []string) int {
	count := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == "" {
			count++
		} else {
			break
		}
	}
	return count + 1
}

// blockScalarHeader reports whether rest is a block-scalar header ("|", ">",
// optionally with a chomping indicator) and returns whether it is folded and
// the chomping byte.
func blockScalarHeader(rest string) (folded bool, chomp byte, ok bool) {
	if rest == "" {
		return false, 0, false
	}
	switch rest[0] {
	case '|':
		folded = false
	case '>':
		folded = true
	default:
		return false, 0, false
	}
	chomp = 0
	indicator := strings.TrimSpace(rest[1:])
	switch indicator {
	case "":
	case "-":
		chomp = '-'
	case "+":
		chomp = '+'
	default:
		return false, 0, false
	}
	return folded, chomp, true
}

// isSequenceItem reports whether a line's content begins a block sequence item.
func isSequenceItem(content string) bool {
	trimmed := strings.TrimLeft(content, " ")
	return trimmed == "-" || strings.HasPrefix(trimmed, "- ")
}

// splitMappingKey splits "key: value" into key and the remaining value text.
func splitMappingKey(body string) (key, rest string, err error) {
	idx := mappingColonIndex(body)
	if idx < 0 {
		return "", "", fmt.Errorf("expected mapping key in %q", body)
	}
	key = strings.TrimSpace(body[:idx])
	key = unquoteKey(key)
	rest = body[idx+1:]
	return key, rest, nil
}

// tryInlineMappingKey splits an inline "key: value" but does not error when no
// colon is present (used to distinguish scalar list items from inline maps).
func tryInlineMappingKey(item string) (key, rest string, err error) {
	idx := mappingColonIndex(item)
	if idx < 0 {
		return "", "", fmt.Errorf("not a mapping")
	}
	key = unquoteKey(strings.TrimSpace(item[:idx]))
	rest = item[idx+1:]
	return key, rest, nil
}

// mappingColonIndex returns the index of the ": " (or trailing ":") that
// separates a key from its value, skipping colons inside quotes.
func mappingColonIndex(body string) int {
	inSingle := false
	inDouble := false
	runes := []rune(body)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			if c == '\\' {
				i++
			} else if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == ':':
			if i+1 >= len(runes) || runes[i+1] == ' ' {
				return len(string(runes[:i]))
			}
		}
	}
	return -1
}

func unquoteKey(key string) string {
	if len(key) >= 2 {
		if (key[0] == '"' && key[len(key)-1] == '"') || (key[0] == '\'' && key[len(key)-1] == '\'') {
			if value, err := parseScalar(key); err == nil {
				if s, ok := value.asString(); ok {
					return s
				}
			}
		}
	}
	return key
}

// parseScalar parses a single-line scalar value (plain or quoted), stripping any
// trailing line comment from plain scalars.
func parseScalar(raw string) (yamlValue, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return yamlValue{kind: yamlNull}, nil
	}
	if trimmed[0] == '"' {
		s, err := parseDoubleQuoted(trimmed)
		if err != nil {
			return yamlValue{}, err
		}
		return yamlValue{kind: yamlString, str: s}, nil
	}
	if trimmed[0] == '\'' {
		s, err := parseSingleQuoted(trimmed)
		if err != nil {
			return yamlValue{}, err
		}
		return yamlValue{kind: yamlString, str: s}, nil
	}
	// Plain scalar: strip an inline comment (" #...").
	plain := stripPlainComment(trimmed)
	switch plain {
	case "null", "~", "Null", "NULL":
		return yamlValue{kind: yamlNull}, nil
	case "true", "True", "TRUE":
		return yamlValue{kind: yamlBool, boolean: true}, nil
	case "false", "False", "FALSE":
		return yamlValue{kind: yamlBool, boolean: false}, nil
	}
	if n, err := strconv.ParseInt(plain, 10, 64); err == nil {
		return yamlValue{kind: yamlInt, integer: n}, nil
	}
	return yamlValue{kind: yamlString, str: plain}, nil
}

func stripPlainComment(s string) string {
	runes := []rune(s)
	for i := 1; i < len(runes); i++ {
		if runes[i] == '#' && runes[i-1] == ' ' {
			return strings.TrimRight(string(runes[:i]), " ")
		}
	}
	return strings.TrimRight(s, " ")
}

func parseSingleQuoted(s string) (string, error) {
	if len(s) < 2 || s[len(s)-1] != '\'' {
		return "", fmt.Errorf("unterminated single-quoted scalar")
	}
	inner := s[1 : len(s)-1]
	// In single quotes, '' is an escaped single quote.
	return strings.ReplaceAll(inner, "''", "'"), nil
}

func parseDoubleQuoted(s string) (string, error) {
	if len(s) < 2 || s[len(s)-1] != '"' {
		return "", fmt.Errorf("unterminated double-quoted scalar")
	}
	inner := []rune(s[1 : len(s)-1])
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c != '\\' {
			b.WriteRune(c)
			continue
		}
		i++
		if i >= len(inner) {
			return "", fmt.Errorf("trailing escape in double-quoted scalar")
		}
		switch inner[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case '0':
			b.WriteByte(0)
		default:
			b.WriteRune(inner[i])
		}
	}
	return b.String(), nil
}
