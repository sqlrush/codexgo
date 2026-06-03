package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// errLegacyWriteOutsideWorkspace mirrors the io::Error returned by
// FileSystemSandboxPolicy::to_legacy_sandbox_policy when a profile requests
// writes outside the workspace root that the legacy model cannot express.
var errLegacyWriteOutsideWorkspace = errors.New(
	"permissions profile requests filesystem writes outside the workspace root, which is not supported until the runtime enforces FileSystemSandboxPolicy directly",
)

// resolveCandidatePath mirrors resolve_candidate_path: absolute inputs are
// normalized; relative inputs are joined against cwd. Returns false when the
// path cannot be made absolute.
func resolveCandidatePath(target, cwd string) (string, bool) {
	if filepath.IsAbs(target) {
		abs, err := abspath.FromAbsolutePath(target)
		if err != nil {
			return "", false
		}
		return abs.Path(), true
	}
	base, err := abspath.FromAbsolutePath(cwd)
	if err != nil {
		return "", false
	}
	return base.Join(target).Path(), true
}

// resolvePathAgainstBase mirrors AbsolutePathBuf::resolve_path_against_base.
func resolvePathAgainstBase(target, base string) string {
	return abspath.ResolvePathAgainstBase(target, base).Path()
}

// normalizeAbsolute lexically normalizes an absolute path, falling back to the
// input on failure.
func normalizeAbsolute(p string) string {
	if abs, err := abspath.FromAbsolutePath(p); err == nil {
		return abs.Path()
	}
	return p
}

// pathStartsWith reports whether child is path-prefixed by parent (component
// aware). Mirrors Rust Path::starts_with semantics.
func pathStartsWith(child, parent string) bool {
	if parent == "" {
		return false
	}
	if child == parent {
		return true
	}
	if parent == "/" {
		return strings.HasPrefix(child, "/")
	}
	return strings.HasPrefix(child, parent+"/")
}

// stripPrefixPath returns the remainder of child relative to parent when child
// is prefixed by parent, mirroring Path::strip_prefix.
func stripPrefixPath(child, parent string) (string, bool) {
	if !pathStartsWith(child, parent) {
		return "", false
	}
	if child == parent {
		return "", true
	}
	if parent == "/" {
		return strings.TrimPrefix(child, "/"), true
	}
	return strings.TrimPrefix(child, parent+"/"), true
}

// componentCount counts path components, mirroring Path::components().count().
// The leading root counts as one component, matching Rust's Component::RootDir.
func componentCount(p string) int {
	if p == "" {
		return 0
	}
	count := 0
	if strings.HasPrefix(p, "/") {
		count++
	}
	for _, part := range strings.Split(strings.Trim(p, "/"), "/") {
		if part != "" {
			count++
		}
	}
	return count
}

// parentless reports whether p has no parent (it is a filesystem root).
func parentless(p string) bool {
	return p == "/" || (len(p) == 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/'))
}

// filesystemRootForPath returns the topmost ancestor (filesystem root) of an
// absolute path. Mirrors absolute_root_path_for_cwd.
func filesystemRootForPath(p string) string {
	if abs, err := abspath.FromAbsolutePath(p); err == nil {
		ancestors := abs.Ancestors()
		if len(ancestors) > 0 {
			return ancestors[len(ancestors)-1].Path()
		}
	}
	if strings.HasPrefix(p, "/") {
		return "/"
	}
	return p
}

// dedupPaths mirrors dedup_absolute_paths with normalize_effective_paths=true:
// each path is normalized for symlink-effective comparison before deduping,
// preserving first-seen order. The Go port normalizes lexically and resolves
// existing symlinks via canonicalize where possible.
func dedupPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		key := normalizeEffectiveAbsolutePath(p)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

// dedupPathsNoNormalize mirrors dedup_absolute_paths with
// normalize_effective_paths=false: dedup on the raw path, preserving order.
func dedupPathsNoNormalize(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// normalizeEffectiveAbsolutePath mirrors normalize_effective_absolute_path: it
// walks ancestors and rewrites the longest existing prefix to its canonical
// (symlink-resolved) form, leaving the suffix untouched. Missing paths keep
// their lexical form.
func normalizeEffectiveAbsolutePath(p string) string {
	for ancestor := p; ; {
		if _, err := os.Lstat(ancestor); err == nil {
			if canonical, err := filepath.EvalSymlinks(ancestor); err == nil {
				suffix := strings.TrimPrefix(p, ancestor)
				joined := canonical + suffix
				if abs, err := abspath.FromAbsolutePath(joined); err == nil {
					return abs.Path()
				}
			}
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	if abs, err := abspath.FromAbsolutePath(p); err == nil {
		return abs.Path()
	}
	return p
}

// isDir reports whether p exists and is a directory.
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// isFile reports whether p exists and is a regular file.
func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}
