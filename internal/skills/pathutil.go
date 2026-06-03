package skills

import (
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// canonicalizeForSkillIdentity mirrors the Rust `canonicalize_for_skill_identity`:
// it canonicalizes the path when possible and otherwise returns the path
// unchanged.
func canonicalizeForSkillIdentity(path abspath.AbsolutePathBuf) abspath.AbsolutePathBuf {
	if c, err := path.Canonicalize(); err == nil {
		return c
	}
	return path
}

// canonicalizeIfExists mirrors the Rust `canonicalize_if_exists` used by implicit
// invocation: it canonicalizes when possible and otherwise returns the input.
func canonicalizeIfExists(path abspath.AbsolutePathBuf) abspath.AbsolutePathBuf {
	if c, err := path.Canonicalize(); err == nil {
		return c
	}
	return path
}

// toStringLossy renders an absolute path with forward slashes, mirroring the
// `to_string_lossy().replace('\\', "/")` pattern used throughout render.rs.
func toStringLossy(path abspath.AbsolutePathBuf) string {
	return strings.ReplaceAll(path.String(), "\\", "/")
}

// comparePaths orders two absolute paths by their string representation,
// mirroring Rust's `AbsolutePathBuf` (PathBuf) `Ord`, which compares the
// underlying path strings.
func comparePaths(a, b abspath.AbsolutePathBuf) int {
	return strings.Compare(a.String(), b.String())
}

// pathStartsWith reports whether path is equal to or a descendant of prefix,
// mirroring Rust's `Path::starts_with`, which matches on whole path components.
func pathStartsWith(path, prefix abspath.AbsolutePathBuf) bool {
	pathComps := pathComponents(path.String())
	prefixComps := pathComponents(prefix.String())
	if len(prefixComps) > len(pathComps) {
		return false
	}
	for i, comp := range prefixComps {
		if pathComps[i] != comp {
			return false
		}
	}
	return true
}

// stripPathPrefix removes prefix from path component-wise and returns the
// remaining components joined with "/", mirroring Rust's
// `Path::strip_prefix(...).to_string_lossy().replace('\\', "/")`. ok is false
// when path is not under prefix.
func stripPathPrefix(path, prefix abspath.AbsolutePathBuf) (string, bool) {
	pathComps := pathComponents(path.String())
	prefixComps := pathComponents(prefix.String())
	if len(prefixComps) > len(pathComps) {
		return "", false
	}
	for i, comp := range prefixComps {
		if pathComps[i] != comp {
			return "", false
		}
	}
	return strings.Join(pathComps[len(prefixComps):], "/"), true
}

// pathComponents splits a normalized absolute path into its normal components,
// dropping the leading root/prefix and any empty segments. It accepts both "/"
// and "\\" as separators so Windows paths normalize the same way Rust's
// component iterator does.
func pathComponents(path string) []string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(normalized, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
