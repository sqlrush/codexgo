package abspath

import "strings"

// This file implements the minimal path grammar required to reproduce the parts
// of Rust's `std::path` that the absolute-path crate depends on. It is split by
// platform via isWindows() so behavior matches `cfg!(...)` gating in the source.

// isSeparator reports whether b separates path segments on the active platform.
// Windows accepts both '\\' and '/'; Unix only '/'.
func isSeparator(b byte) bool {
	if b == '/' {
		return true
	}
	return isWindows() && b == '\\'
}

// nativeSeparator returns the platform's primary separator (MAIN_SEPARATOR).
func nativeSeparator() byte {
	if isWindows() {
		return '\\'
	}
	return '/'
}

// isAbsolute mirrors Path::is_absolute.
//
// On Unix a path is absolute iff it starts with '/'. On Windows a path is
// absolute iff it has both a prefix (drive or UNC) and a root separator, e.g.
// "C:\\x" or "\\\\server\\share". A bare root like "\\x" has a root but no
// prefix and is therefore not absolute (it is "drive-relative").
func isAbsolute(path string) bool {
	if isWindows() {
		comps := parseComponents(path)
		hasPrefix := len(comps) > 0 && comps[0].kind == compPrefix
		hasRootSep := false
		for _, c := range comps {
			if c.kind == compRootDir {
				hasRootSep = true
				break
			}
		}
		return hasPrefix && hasRootSep
	}
	return len(path) > 0 && path[0] == '/'
}

// hasRoot mirrors Path::has_root: the path contains a root separator regardless
// of whether it carries a prefix.
func hasRoot(path string) bool {
	for _, c := range parseComponents(path) {
		if c.kind == compRootDir {
			return true
		}
	}
	return false
}

// parseComponents splits path into ordered components using the active
// platform's grammar. It dispatches to the Unix or Windows parser.
func parseComponents(path string) []component {
	if isWindows() {
		return parseComponentsWindows(path)
	}
	return parseComponentsUnix(path)
}

// parseComponentsUnix parses a Unix path. A leading '/' yields a single root
// component; remaining segments split on '/' with empty segments collapsed,
// mirroring how std::path treats repeated separators.
func parseComponentsUnix(path string) []component {
	var out []component
	rest := path
	if len(rest) > 0 && rest[0] == '/' {
		out = append(out, component{kind: compRootDir})
		rest = strings.TrimLeft(rest, "/")
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "" {
			continue
		}
		out = append(out, component{kind: compNormal, text: seg})
	}
	return out
}

// parseComponentsWindows parses a Windows path, recognizing drive ("C:") and
// verbatim/UNC-style prefixes, the root separator, and normal segments. Both
// '\\' and '/' act as separators.
func parseComponentsWindows(path string) []component {
	var out []component
	rest := path

	// UNC prefix: \\server\share (consumes server and share as part of the
	// prefix, matching std::path::Prefix::UNC).
	if len(rest) >= 2 && isSeparator(rest[0]) && isSeparator(rest[1]) {
		body := rest[2:]
		server, afterServer := splitFirstSegment(body)
		if server != "" {
			share, afterShare := splitFirstSegment(afterServer)
			if share != "" {
				prefixText := `\\` + server + `\` + share
				out = append(out, component{kind: compPrefix, text: prefixText})
				rest = afterShare
				// A UNC prefix implies a root.
				out = append(out, component{kind: compRootDir})
				return appendNormalSegments(out, rest)
			}
		}
		// Fall through: treat as a rooted (drive-relative) path with two leading
		// separators, i.e. just a root.
		out = append(out, component{kind: compRootDir})
		return appendNormalSegments(out, strings.TrimLeft(rest, `\/`))
	}

	// Drive prefix: "C:".
	if len(rest) >= 2 && isASCIIAlpha(rest[0]) && rest[1] == ':' {
		out = append(out, component{kind: compPrefix, text: rest[0:2]})
		rest = rest[2:]
		if len(rest) > 0 && isSeparator(rest[0]) {
			out = append(out, component{kind: compRootDir})
			rest = trimLeftSeparators(rest)
		}
		return appendNormalSegments(out, rest)
	}

	// Rooted but prefix-less (drive-relative): "\\x".
	if len(rest) > 0 && isSeparator(rest[0]) {
		out = append(out, component{kind: compRootDir})
		rest = trimLeftSeparators(rest)
	}
	return appendNormalSegments(out, rest)
}

// appendNormalSegments splits rest on separators and appends each non-empty
// segment as a normal component.
func appendNormalSegments(out []component, rest string) []component {
	for _, seg := range splitOnSeparators(rest) {
		if seg == "" {
			continue
		}
		out = append(out, component{kind: compNormal, text: seg})
	}
	return out
}

// splitFirstSegment returns the first separator-delimited segment of s and the
// remainder after the separator.
func splitFirstSegment(s string) (seg, rest string) {
	for i := 0; i < len(s); i++ {
		if isSeparator(s[i]) {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// splitOnSeparators splits s on any active-platform separator.
func splitOnSeparators(s string) []string {
	var segs []string
	start := 0
	for i := 0; i < len(s); i++ {
		if isSeparator(s[i]) {
			segs = append(segs, s[start:i])
			start = i + 1
		}
	}
	segs = append(segs, s[start:])
	return segs
}

// trimLeftSeparators removes leading separators.
func trimLeftSeparators(s string) string {
	i := 0
	for i < len(s) && isSeparator(s[i]) {
		i++
	}
	return s[i:]
}

// renderComponents reassembles components into a path string using the native
// separator, mirroring how PathBuf prints.
func renderComponents(comps []component) string {
	var b strings.Builder
	sep := nativeSeparator()
	needSep := false
	for _, c := range comps {
		switch c.kind {
		case compPrefix:
			b.WriteString(c.text)
			// A drive prefix is not followed by an implicit separator unless a
			// root component follows; a UNC prefix already includes separators.
			needSep = false
		case compRootDir:
			b.WriteByte(sep)
			needSep = false
		case compNormal:
			if needSep {
				b.WriteByte(sep)
			}
			b.WriteString(c.text)
			needSep = true
		}
	}
	return b.String()
}

// joinUnix joins two Unix path fragments, mirroring Path::join: an absolute
// right-hand side replaces base; otherwise base and path are separated by '/'.
func joinUnix(base, path string) string {
	if isAbsolute(path) {
		return path
	}
	if base == "" {
		return path
	}
	if strings.HasSuffix(base, "/") {
		return base + path
	}
	return base + "/" + path
}

// joinWindows joins two Windows path fragments, mirroring PathBuf::push:
//
//   - If path is absolute or carries its own prefix, it replaces base entirely.
//   - If path is rooted but prefix-less (e.g. "\\dir"), it keeps base's prefix
//     and replaces everything after it (so C:\base + \dir = C:\dir).
//   - Otherwise path is appended to base with a separator.
func joinWindows(base, path string) string {
	if isAbsolute(path) || pathHasPrefix(path) {
		return path
	}
	if base == "" {
		return path
	}
	if hasRoot(path) {
		// Keep base's prefix, drop base's root/tail, then append path which
		// itself begins with a root.
		return prefixOf(base) + path
	}
	if endsWithSeparator(base) {
		return base + path
	}
	return base + `\` + path
}

// pathHasPrefix reports whether path begins with a Windows prefix component.
func pathHasPrefix(path string) bool {
	comps := parseComponents(path)
	return len(comps) > 0 && comps[0].kind == compPrefix
}

// prefixOf returns the leading prefix component text of path, or "" if none.
func prefixOf(path string) string {
	comps := parseComponents(path)
	if len(comps) > 0 && comps[0].kind == compPrefix {
		return comps[0].text
	}
	return ""
}

// endsWithSeparator reports whether s ends with an active-platform separator.
func endsWithSeparator(s string) bool {
	return len(s) > 0 && isSeparator(s[len(s)-1])
}
