package filesearch

import (
	"bufio"
	"os"
	"path"
	"strings"
)

// gitignorePattern is a single compiled .gitignore line.
type gitignorePattern struct {
	// negate is true for "!" patterns, which re-include a previously excluded
	// path.
	negate bool
	// dirOnly is true when the pattern ends with "/", meaning it only matches
	// directories.
	dirOnly bool
	// anchored is true when the pattern is anchored to the directory of the
	// gitignore file (it contained a non-trailing slash). Anchored patterns
	// match against the full relative path; unanchored ones match against the
	// final path component as well.
	anchored bool
	// segments holds the slash-separated glob segments of the pattern. A "**"
	// segment matches zero or more path segments.
	segments []string
}

// gitignore is the set of patterns sourced from one .gitignore file, scoped to
// the directory that contained it. base is the directory path (relative to the
// search root, using forward slashes) where the file lives; matches are tested
// against paths relative to this base.
type gitignore struct {
	base     string
	patterns []gitignorePattern
}

// loadGitignore reads and compiles the .gitignore file at filePath, scoping its
// patterns to base (a forward-slash relative directory path; "" for the root).
// It returns ok=false when the file does not exist or cannot be read.
func loadGitignore(filePath, base string) (gitignore, bool) {
	f, err := os.Open(filePath)
	if err != nil {
		return gitignore{}, false
	}
	defer f.Close()

	gi := gitignore{base: base}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if p, ok := compilePattern(scanner.Text()); ok {
			gi.patterns = append(gi.patterns, p)
		}
	}
	return gi, true
}

// compilePattern parses a single .gitignore line into a gitignorePattern. It
// returns ok=false for blank lines and comments.
func compilePattern(raw string) (gitignorePattern, bool) {
	line := raw
	// A leading '#' is a comment unless escaped with '\'.
	if strings.HasPrefix(line, "#") {
		return gitignorePattern{}, false
	}
	if strings.HasPrefix(line, `\#`) {
		line = line[1:]
	}
	// Trailing whitespace is stripped unless escaped; we handle the common
	// unescaped case.
	line = strings.TrimRight(line, " \t")
	if line == "" {
		return gitignorePattern{}, false
	}

	var p gitignorePattern
	if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
	} else if strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}
	if line == "" {
		return gitignorePattern{}, false
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	if line == "" {
		return gitignorePattern{}, false
	}

	// A slash anywhere except a trailing one anchors the pattern to the
	// gitignore's directory. A leading slash anchors but is otherwise dropped.
	trimmed := strings.TrimPrefix(line, "/")
	if strings.Contains(trimmed, "/") || strings.HasPrefix(line, "/") {
		p.anchored = true
	}
	line = trimmed

	p.segments = strings.Split(line, "/")
	return p, true
}

// matches reports whether the entry at relPath (relative to the search root,
// forward slashes) matching the given isDir status is matched by this
// gitignore. The returned negate flag indicates whether the matching pattern
// was a negation (re-include). matched is false when no pattern applied.
//
// The last matching pattern wins, mirroring git's semantics.
func (g gitignore) matches(relPath string, isDir bool) (negate bool, matched bool) {
	rel, ok := relativeTo(relPath, g.base)
	if !ok {
		return false, false
	}
	for _, p := range g.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if patternMatches(p, rel) {
			negate = p.negate
			matched = true
		}
	}
	return negate, matched
}

// relativeTo returns relPath made relative to base (both forward-slash). ok is
// false when relPath is not within base.
func relativeTo(relPath, base string) (string, bool) {
	if base == "" {
		return relPath, true
	}
	if relPath == base {
		return "", true
	}
	prefix := base + "/"
	if strings.HasPrefix(relPath, prefix) {
		return strings.TrimPrefix(relPath, prefix), true
	}
	return "", false
}

// patternMatches reports whether rel (a forward-slash relative path) is matched
// by p. For anchored patterns the whole path must match the segments; for
// unanchored patterns the segments may match any trailing subsequence of the
// path components.
func patternMatches(p gitignorePattern, rel string) bool {
	if rel == "" {
		return false
	}
	pathSegs := strings.Split(rel, "/")

	if p.anchored {
		return matchSegments(p.segments, pathSegs)
	}
	// Unanchored: the pattern may begin at any path component. git treats an
	// unanchored single-segment pattern as matching that name at any depth.
	for start := 0; start <= len(pathSegs); start++ {
		if matchSegments(p.segments, pathSegs[start:]) {
			return true
		}
	}
	return false
}

// matchSegments matches pattern segments against path segments, honoring the
// "**" segment which matches zero or more path segments. A non-"**" pattern
// that runs out before the path still matches because gitignore semantics treat
// a matched directory prefix as excluding everything beneath it.
func matchSegments(pattern, pathSegs []string) bool {
	switch {
	case len(pattern) == 0:
		// Matched a prefix directory: everything under it is covered.
		return true
	case pattern[0] == "**":
		// "**" matches zero or more path segments; try each split point.
		for i := 0; i <= len(pathSegs); i++ {
			if matchSegments(pattern[1:], pathSegs[i:]) {
				return true
			}
		}
		return false
	case len(pathSegs) == 0:
		return false
	default:
		if !matchGlob(pattern[0], pathSegs[0]) {
			return false
		}
		return matchSegments(pattern[1:], pathSegs[1:])
	}
}

// matchGlob reports whether a single path component name is matched by a single
// glob pattern segment supporting '*', '?' and '[...]' character classes. '*'
// and '?' do not match the path separator (already excluded since name is a
// single component). The implementation is an iterative backtracking matcher to
// avoid catastrophic recursion on adversarial input.
func matchGlob(pattern, name string) bool {
	pr := []rune(pattern)
	nr := []rune(name)
	pi, ni := 0, 0
	star, mark := -1, 0
	for ni < len(nr) {
		if pi < len(pr) {
			switch pr[pi] {
			case '*':
				star = pi
				mark = ni
				pi++
				continue
			case '?':
				pi++
				ni++
				continue
			case '[':
				if newPi, ok := matchClass(pr, pi, nr[ni]); ok {
					pi = newPi
					ni++
					continue
				}
			default:
				if pr[pi] == nr[ni] {
					pi++
					ni++
					continue
				}
			}
		}
		if star >= 0 {
			pi = star + 1
			mark++
			ni = mark
			continue
		}
		return false
	}
	for pi < len(pr) && pr[pi] == '*' {
		pi++
	}
	return pi == len(pr)
}

// matchClass evaluates a '[...]' character class in pattern starting at the '['
// at index start against rune c. It returns the index just past the closing
// ']' and whether c matched. A leading '!' or '^' negates the class. If the
// class is malformed (no closing ']'), it is treated as a literal '['.
func matchClass(pattern []rune, start int, c rune) (int, bool) {
	i := start + 1
	negate := false
	if i < len(pattern) && (pattern[i] == '!' || pattern[i] == '^') {
		negate = true
		i++
	}
	matched := false
	first := true
	for i < len(pattern) {
		if pattern[i] == ']' && !first {
			if negate {
				return i + 1, !matched
			}
			return i + 1, matched
		}
		first = false
		// Range a-z.
		if i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] != ']' {
			lo, hi := pattern[i], pattern[i+2]
			if c >= lo && c <= hi {
				matched = true
			}
			i += 3
			continue
		}
		if pattern[i] == c {
			matched = true
		}
		i++
	}
	// Malformed class: treat the '[' as a literal character.
	return start + 1, c == '['
}

// joinRel joins a base relative directory (forward slashes, "" for root) with a
// child name, producing a forward-slash relative path.
func joinRel(base, name string) string {
	if base == "" {
		return name
	}
	return path.Join(base, name)
}
