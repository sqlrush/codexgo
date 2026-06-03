package pathutil

import (
	"path/filepath"
	"runtime"
	"strings"
)

// This file ports the absolutize submodule of codex-utils-absolute-path, itself
// adapted from path-absolutize 3.1.1 (MIT, (c) 2018 magiclen.org). Only the
// explicit-base path joining and dot normalization needed by this package are
// reproduced.

// pathWithBase mirrors path_with_base.
//
// On non-Windows, an absolute path is returned as-is; a relative path is joined
// onto the base. On Windows, root-relative and drive-relative inputs are handled
// to match the Rust behavior (see normalizeWindowsPathWithBase).
func pathWithBase(path string, base string) string {
	if runtime.GOOS == "windows" {
		return normalizeWindowsPathWithBase(path, base)
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return joinUnix(base, path)
}

// joinUnix joins two POSIX-style path fragments without invoking filepath, which
// on Windows would rewrite separators. The base is treated as a prefix and the
// path is appended with a single "/" separator.
func joinUnix(base string, path string) string {
	if path == "" {
		return base
	}
	if base == "" {
		return path
	}
	if strings.HasSuffix(base, "/") {
		return base + path
	}
	return base + "/" + path
}

// normalizePath mirrors normalize_path: it folds away "." components and
// resolves ".." by popping the previous component, preserving any root/prefix.
// An empty result collapses to ".".
func normalizePath(path string) string {
	if runtime.GOOS == "windows" {
		return normalizeWindowsPath(path)
	}
	return normalizeUnixPath(path)
}

// normalizeUnixPath performs component normalization for POSIX-style paths.
func normalizeUnixPath(path string) string {
	rooted := strings.HasPrefix(path, "/")

	var out []string
	for _, comp := range strings.Split(path, "/") {
		switch comp {
		case "", ".":
			// Skip empty (collapsed separators) and current-dir components.
		case "..":
			// Pop the previous normal component, but never past the root or an
			// already-leading "..".
			if n := len(out); n > 0 && out[n-1] != ".." {
				out = out[:n-1]
			} else if !rooted {
				out = append(out, "..")
			}
		default:
			out = append(out, comp)
		}
	}

	joined := strings.Join(out, "/")
	if rooted {
		joined = "/" + joined
	}
	if joined == "" {
		return "."
	}
	return joined
}

// isAbsolutePath reports whether path is absolute using platform-appropriate
// rules, mirroring Rust's Path::is_absolute.
func isAbsolutePath(path string) bool {
	if runtime.GOOS == "windows" {
		return filepath.IsAbs(path)
	}
	return strings.HasPrefix(path, "/")
}

// isASCIIAlpha reports whether b is an ASCII letter.
func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
