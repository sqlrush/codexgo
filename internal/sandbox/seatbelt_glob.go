package sandbox

import "strings"

// seatbeltRegexForUnreadableGlob mirrors seatbelt_regex_for_unreadable_glob: it
// translates the supported git-style glob subset into an anchored Seatbelt regex.
// "*" and "?" stay within one path component, "**/" consumes zero or more
// components, and closed character classes remain character classes. A pattern
// with no glob metacharacters matches the exact path plus its subtree. Returns
// ok=false for an empty pattern.
func seatbeltRegexForUnreadableGlob(pattern string) (string, bool) {
	if pattern == "" {
		return "", false
	}

	chars := []rune(pattern)
	var b strings.Builder
	b.WriteByte('^')
	sawGlob := false

	for i := 0; i < len(chars); {
		ch := chars[i]
		i++
		switch ch {
		case '*':
			sawGlob = true
			if i < len(chars) && chars[i] == '*' {
				i++ // consume second '*'
				if i < len(chars) && chars[i] == '/' {
					i++ // consume '/'
					b.WriteString("(.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			sawGlob = true
			b.WriteString("[^/]")
		case '[':
			sawGlob = true
			var class []rune
			closed := false
			for i < len(chars) {
				classCh := chars[i]
				i++
				if classCh == ']' {
					closed = true
					break
				}
				class = append(class, classCh)
			}
			if !closed {
				// Unterminated class: treat '[' as a literal and reprocess the
				// captured characters from where the class started.
				b.WriteString(`\[`)
				i -= len(class)
				continue
			}

			b.WriteByte('[')
			if len(class) > 0 {
				switch class[0] {
				case '!':
					b.WriteByte('^')
				case '^':
					b.WriteString(`\^`)
				default:
					b.WriteRune(class[0])
				}
				for _, classCh := range class[1:] {
					if classCh == '\\' {
						b.WriteString(`\\`)
					} else {
						b.WriteRune(classCh)
					}
				}
			}
			b.WriteByte(']')
		case ']':
			sawGlob = true
			b.WriteString(`\]`)
		default:
			b.WriteString(regexEscape(string(ch)))
		}
	}

	if !sawGlob {
		b.WriteString("(/.*)?")
	}
	b.WriteByte('$')
	return b.String(), true
}

// canonicalizeGlobStaticPrefixForSandbox mirrors
// canonicalize_glob_static_prefix_for_sandbox: it rewrites the static (glob-free)
// directory prefix of a pattern to its canonicalized (symlink-resolved) form,
// returning ok=false when the prefix cannot be canonicalized, has no directory
// component, or the result is unchanged.
func canonicalizeGlobStaticPrefixForSandbox(pattern string) (string, bool) {
	firstGlobIndex := -1
	for i, ch := range pattern {
		if ch == '*' || ch == '?' || ch == '[' || ch == ']' {
			firstGlobIndex = i
			break
		}
	}
	if firstGlobIndex < 0 {
		normalized, ok := normalizePathForSandbox(pattern)
		if !ok {
			return "", false
		}
		return normalized, true
	}

	staticPrefix := pattern[:firstGlobIndex]
	var prefixEnd int
	if strings.HasSuffix(staticPrefix, "/") {
		prefixEnd = len(staticPrefix) - 1
	} else if idx := strings.LastIndexByte(staticPrefix, '/'); idx >= 0 {
		prefixEnd = idx
	} else {
		prefixEnd = 0
	}
	if prefixEnd == 0 {
		return "", false
	}

	root, ok := normalizePathForSandbox(pattern[:prefixEnd])
	if !ok {
		return "", false
	}
	suffix := pattern[prefixEnd:]
	normalizedPattern := root + suffix
	if normalizedPattern == pattern {
		return "", false
	}
	return normalizedPattern, true
}
