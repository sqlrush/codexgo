package pathutil

import "strings"

// This file holds the Windows-specific path helpers. The functions are written
// portably (no build tags) so the package compiles everywhere, but they are only
// reached at runtime when runtime.GOOS == "windows". The logic mirrors the
// Windows arms of codex-utils-absolute-path's path_with_base / normalize_path
// and codex-utils-path's dunce::simplified usage.

// windowsVolumeLen returns the length of the leading volume name (drive prefix
// or UNC share root) in a Windows-style path, or 0 if there is none. It is a
// self-contained reimplementation of the relevant parts of filepath's volume
// detection so behavior is identical regardless of the host platform.
func windowsVolumeLen(path string) int {
	if len(path) < 2 {
		return 0
	}
	// Drive letter, e.g. "C:".
	if isASCIIAlpha(path[0]) && path[1] == ':' {
		return 2
	}
	// UNC or device path, e.g. "\\server\share" or "//server/share".
	if isWindowsSep(path[0]) && isWindowsSep(path[1]) {
		// Find the end of the host component, then the share component.
		rest := path[2:]
		i := indexWindowsSep(rest)
		if i <= 0 {
			return len(path)
		}
		host := rest[:i]
		shareStart := i + 1
		j := indexWindowsSep(rest[shareStart:])
		if j < 0 {
			return len(path)
		}
		_ = host
		return 2 + shareStart + j
	}
	return 0
}

func isWindowsSep(b byte) bool { return b == '\\' || b == '/' }

func indexWindowsSep(s string) int {
	for i := 0; i < len(s); i++ {
		if isWindowsSep(s[i]) {
			return i
		}
	}
	return -1
}

// normalizeWindowsPath performs component normalization for Windows-style paths,
// preserving the volume name and any rooting separator while folding "." and
// resolving "..".
func normalizeWindowsPath(path string) string {
	vol := path[:windowsVolumeLen(path)]
	rest := path[len(vol):]
	rooted := len(rest) > 0 && isWindowsSep(rest[0])

	var out []string
	for _, comp := range splitWindows(rest) {
		switch comp {
		case "", ".":
		case "..":
			if n := len(out); n > 0 && out[n-1] != ".." {
				out = out[:n-1]
			} else if !rooted {
				out = append(out, "..")
			}
		default:
			out = append(out, comp)
		}
	}

	joined := strings.Join(out, `\`)
	prefix := vol
	if rooted {
		prefix += `\`
	}
	result := prefix + joined
	if result == "" {
		return "."
	}
	return result
}

// splitWindows splits a Windows path tail on either separator.
func splitWindows(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '\\' || r == '/'
	})
}

// normalizeWindowsPathWithBase mirrors the Windows arm of path_with_base.
//
// Absolute or rooted paths are joined onto the base; for paths beginning with a
// bare drive prefix the drive's tail is composed with the base's non-prefix
// components, matching the original adaptation of path-absolutize.
func normalizeWindowsPathWithBase(path string, base string) string {
	if isAbsolutePath(path) || windowsHasRoot(path) {
		return windowsJoin(base, path)
	}

	prefixLen := windowsVolumeLen(path)
	if prefixLen == 0 {
		// No drive prefix: ordinary relative path joined onto base.
		return windowsJoin(base, path)
	}

	prefix := path[:prefixLen]
	tail := path[prefixLen:]
	if tail == "" {
		// Bare drive prefix such as "C:".
		return prefix + `\`
	}

	// Drive-relative: compose the path's prefix with the base's tail and the
	// path's own tail.
	baseTail := base
	if bl := windowsVolumeLen(base); bl > 0 {
		baseTail = base[bl:]
	}
	return windowsJoin(prefix+strings.TrimLeft(baseTail, `\/`), tail)
}

// windowsHasRoot reports whether path begins with a rooting separator after any
// volume name (mirrors Path::has_root on Windows).
func windowsHasRoot(path string) bool {
	rest := path[windowsVolumeLen(path):]
	return len(rest) > 0 && isWindowsSep(rest[0])
}

// windowsJoin joins two Windows path fragments with a backslash, collapsing
// duplicate separators at the seam.
func windowsJoin(base string, path string) string {
	if path == "" {
		return base
	}
	if base == "" {
		return path
	}
	b := strings.TrimRight(base, `\/`)
	p := strings.TrimLeft(path, `\/`)
	return b + `\` + p
}
