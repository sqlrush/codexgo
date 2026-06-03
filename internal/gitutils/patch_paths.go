package gitutils

import (
	"sort"
	"strings"
	"unicode"
)

// ExtractPathsFromPatch collects every path referenced by the `diff --git`
// headers in a unified diff, returned sorted and de-duplicated. Quoted,
// C-escaped header paths are unescaped; `/dev/null` markers are ignored.
//
// Mirrors the Rust `extract_paths_from_patch`.
func ExtractPathsFromPatch(diffText string) []string {
	set := make(map[string]struct{})
	for _, rawLine := range strings.Split(diffText, "\n") {
		line := strings.TrimSpace(rawLine)
		rest, found := strings.CutPrefix(line, "diff --git ")
		if !found {
			continue
		}
		a, b, ok := parseDiffGitPaths(rest)
		if !ok {
			continue
		}
		if p, ok := normalizeDiffPath(a, "a/"); ok {
			set[p] = struct{}{}
		}
		if p, ok := normalizeDiffPath(b, "b/"); ok {
			set[p] = struct{}{}
		}
	}

	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// parseDiffGitPaths reads the two whitespace- or quote-delimited tokens that
// follow `diff --git `.
//
// Mirrors the Rust `parse_diff_git_paths`.
func parseDiffGitPaths(line string) (string, string, bool) {
	runes := []rune(line)
	pos := 0
	first, ok := readDiffGitToken(runes, &pos)
	if !ok {
		return "", "", false
	}
	second, ok := readDiffGitToken(runes, &pos)
	if !ok {
		return "", "", false
	}
	return first, second, true
}

// readDiffGitToken reads a single token, honouring `"`/`'` quoting and backslash
// escapes within quotes, advancing pos past the consumed runes.
//
// Mirrors the Rust `read_diff_git_token`.
func readDiffGitToken(runes []rune, pos *int) (string, bool) {
	// Skip leading whitespace.
	for *pos < len(runes) && unicode.IsSpace(runes[*pos]) {
		*pos++
	}

	var quote rune
	hasQuote := false
	if *pos < len(runes) && (runes[*pos] == '"' || runes[*pos] == '\'') {
		quote = runes[*pos]
		hasQuote = true
		*pos++
	}

	var out strings.Builder
	for *pos < len(runes) {
		c := runes[*pos]
		*pos++
		if hasQuote {
			if c == quote {
				break
			}
			if c == '\\' {
				out.WriteRune('\\')
				if *pos < len(runes) {
					out.WriteRune(runes[*pos])
					*pos++
				}
				continue
			}
		} else if unicode.IsSpace(c) {
			break
		}
		out.WriteRune(c)
	}

	raw := out.String()
	if raw == "" && !hasQuote {
		return "", false
	}
	if hasQuote {
		return unescapeCString(raw), true
	}
	return raw, true
}

// normalizeDiffPath strips the given `a/` or `b/` prefix and rejects empty or
// `/dev/null` paths.
//
// Mirrors the Rust `normalize_diff_path`.
func normalizeDiffPath(raw, prefix string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if trimmed == "/dev/null" || trimmed == prefix+"dev/null" {
		return "", false
	}
	trimmed = strings.TrimPrefix(trimmed, prefix)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

// unescapeCString decodes C-style escape sequences (\n, \t, octal, etc.) in a
// quoted git path.
//
// Mirrors the Rust `unescape_c_string`.
func unescapeCString(input string) string {
	runes := []rune(input)
	var out strings.Builder
	out.Grow(len(input))
	i := 0
	for i < len(runes) {
		c := runes[i]
		i++
		if c != '\\' {
			out.WriteRune(c)
			continue
		}
		if i >= len(runes) {
			out.WriteRune('\\')
			break
		}
		next := runes[i]
		i++
		switch next {
		case 'n':
			out.WriteRune('\n')
		case 'r':
			out.WriteRune('\r')
		case 't':
			out.WriteRune('\t')
		case 'b':
			out.WriteRune('')
		case 'f':
			out.WriteRune('')
		case 'a':
			out.WriteRune('')
		case 'v':
			out.WriteRune('')
		case '\\':
			out.WriteRune('\\')
		case '"':
			out.WriteRune('"')
		case '\'':
			out.WriteRune('\'')
		default:
			if next >= '0' && next <= '7' {
				value := int(next - '0')
				for k := 0; k < 2; k++ {
					if i < len(runes) && runes[i] >= '0' && runes[i] <= '7' {
						value = value*8 + int(runes[i]-'0')
						i++
					} else {
						break
					}
				}
				if value <= unicode.MaxRune {
					out.WriteRune(rune(value))
				}
			} else {
				out.WriteRune(next)
			}
		}
	}
	return out.String()
}
