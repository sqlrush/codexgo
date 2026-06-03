package modelsmanager

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// This file ports the minimal strict templating engine from
// `codex_utils_template` so collaboration-mode templates render identically to
// the Rust implementation.
//
// Supported syntax:
//   - `{{ name }}` placeholder interpolation
//   - `{{{{` for a literal `{{`
//   - `}}}}` for a literal `}}`

// templateSegment is one literal or placeholder chunk of a parsed template.
type templateSegment struct {
	literal       string
	placeholder   string
	isPlaceholder bool
}

// template is a parsed strict text template. Rust: Template.
type template struct {
	placeholders []string // sorted, unique
	segments     []templateSegment
}

// parseTemplate parses source into a reusable template. Rust: Template::parse.
func parseTemplate(source string) (*template, error) {
	placeholderSet := map[string]struct{}{}
	var segments []templateSegment
	literalStart := 0
	cursor := 0

	for cursor < len(source) {
		rest := source[cursor:]
		switch {
		case strings.HasPrefix(rest, "{{{{"):
			pushLiteral(&segments, source[literalStart:cursor])
			pushLiteral(&segments, "{{")
			cursor += len("{{{{")
			literalStart = cursor
		case strings.HasPrefix(rest, "}}}}"):
			pushLiteral(&segments, source[literalStart:cursor])
			pushLiteral(&segments, "}}")
			cursor += len("}}}}")
			literalStart = cursor
		case strings.HasPrefix(rest, "{{"):
			pushLiteral(&segments, source[literalStart:cursor])
			placeholder, nextCursor, err := parsePlaceholder(source, cursor)
			if err != nil {
				return nil, err
			}
			placeholderSet[placeholder] = struct{}{}
			segments = append(segments, templateSegment{placeholder: placeholder, isPlaceholder: true})
			cursor = nextCursor
			literalStart = cursor
		case strings.HasPrefix(rest, "}}"):
			return nil, fmt.Errorf("template contains an unmatched `}}` at byte %d", cursor)
		default:
			_, size := decodeRune(rest)
			if size == 0 {
				cursor = len(source)
				continue
			}
			cursor += size
		}
	}

	pushLiteral(&segments, source[literalStart:])

	placeholders := make([]string, 0, len(placeholderSet))
	for name := range placeholderSet {
		placeholders = append(placeholders, name)
	}
	sort.Strings(placeholders)

	return &template{placeholders: placeholders, segments: segments}, nil
}

// render renders the template with the supplied variables. It enforces the same
// strict rules as Rust: every placeholder must be supplied, every supplied value
// must be used, and no duplicate keys are permitted. Rust: Template::render.
func (t *template) render(variables map[string]string) (string, error) {
	for _, placeholder := range t.placeholders {
		if _, ok := variables[placeholder]; !ok {
			return "", fmt.Errorf("template placeholder `%s` is missing a value", placeholder)
		}
	}
	for name := range variables {
		if !t.hasPlaceholder(name) {
			return "", fmt.Errorf("template value `%s` is not used by this template", name)
		}
	}

	var builder strings.Builder
	for _, segment := range t.segments {
		if segment.isPlaceholder {
			value, ok := variables[segment.placeholder]
			if !ok {
				return "", fmt.Errorf("template placeholder `%s` is missing a value", segment.placeholder)
			}
			builder.WriteString(value)
		} else {
			builder.WriteString(segment.literal)
		}
	}
	return builder.String(), nil
}

// hasPlaceholder reports whether name is one of the template's placeholders.
func (t *template) hasPlaceholder(name string) bool {
	for _, placeholder := range t.placeholders {
		if placeholder == name {
			return true
		}
	}
	return false
}

// pushLiteral appends a literal chunk, merging with a trailing literal segment.
func pushLiteral(segments *[]templateSegment, literal string) {
	if literal == "" {
		return
	}
	if n := len(*segments); n > 0 && !(*segments)[n-1].isPlaceholder {
		(*segments)[n-1].literal += literal
		return
	}
	*segments = append(*segments, templateSegment{literal: literal})
}

// parsePlaceholder parses a `{{ name }}` placeholder starting at start, returning
// the trimmed name and the cursor just past the closing `}}`.
func parsePlaceholder(source string, start int) (string, int, error) {
	placeholderStart := start + len("{{")
	cursor := placeholderStart

	for cursor < len(source) {
		rest := source[cursor:]
		if strings.HasPrefix(rest, "{{") {
			return "", 0, fmt.Errorf("template placeholder starting at byte %d contains a nested `{{`", start)
		}
		if strings.HasPrefix(rest, "}}") {
			placeholder := strings.TrimSpace(source[placeholderStart:cursor])
			if placeholder == "" {
				return "", 0, fmt.Errorf("template placeholder at byte %d is empty", start)
			}
			return placeholder, cursor + len("}}"), nil
		}
		_, size := decodeRune(rest)
		if size == 0 {
			break
		}
		cursor += size
	}

	return "", 0, fmt.Errorf("template placeholder starting at byte %d is missing `}}`", start)
}

// decodeRune returns the first rune of s and its byte width, or (0, 0) when s is
// empty.
func decodeRune(s string) (rune, int) {
	if s == "" {
		return 0, 0
	}
	r, size := utf8.DecodeRuneInString(s)
	return r, size
}
