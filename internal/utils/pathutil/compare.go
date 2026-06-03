package pathutil

import "path/filepath"

// NormalizeForPathComparison canonicalizes path (resolving symlinks and "."/".."
// components against the live filesystem) and then applies WSL normalization.
//
// It mirrors normalize_for_path_comparison and therefore requires path to exist;
// the error from canonicalization is propagated to the caller.
func NormalizeForPathComparison(path string) (string, error) {
	canonical, err := canonicalize(path)
	if err != nil {
		return "", err
	}
	return normalizeForWSL(canonical), nil
}

// canonicalize resolves a path to its real, absolute, symlink-free form. It is
// the Go analogue of std::fs::canonicalize: filepath.EvalSymlinks resolves
// symlinks and requires every component to exist, and filepath.Abs ensures the
// result is absolute.
func canonicalize(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// PathsMatchAfterNormalization reports whether two paths refer to the same
// location after Codex's filesystem normalization.
//
// If either path cannot be normalized (for example, it does not exist), the
// comparison falls back to raw string equality, mirroring
// paths_match_after_normalization.
func PathsMatchAfterNormalization(left string, right string) bool {
	normLeft, errLeft := NormalizeForPathComparison(left)
	normRight, errRight := NormalizeForPathComparison(right)
	if errLeft == nil && errRight == nil {
		return normLeft == normRight
	}
	return left == right
}
