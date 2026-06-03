package filesearch

import (
	"os"
	"path/filepath"
	"sort"
)

// entry is a discovered filesystem entry: its path relative to the search root
// (forward slashes) and whether it is a directory.
type entry struct {
	rel   string
	isDir bool
}

// walker walks a single root directory tree, honoring gitignore rules and the
// exclude overrides, and collects entries.
type walker struct {
	root             string
	respectGitignore bool
	excludes         []gitignorePattern
	cancel           func() bool
}

// newWalker constructs a walker for root. The excludes are compiled
// gitignore-style override patterns; any entry matching one is dropped. cancel,
// when non-nil, is polled periodically and aborts the walk when it returns true.
func newWalker(root string, respectGitignore bool, excludes []gitignorePattern, cancel func() bool) walker {
	return walker{
		root:             root,
		respectGitignore: respectGitignore,
		excludes:         excludes,
		cancel:           cancel,
	}
}

// walk traverses the tree rooted at w.root and returns the surviving entries.
// Entries are returned with paths relative to w.root using forward slashes.
//
// Gitignore handling follows git's "require_git" semantics: .gitignore files
// are applied only when a .git directory exists at or above the root. When no
// git context is present, or RespectGitignore is false, gitignore files are
// ignored entirely. Symlinked directories are followed (matching the Rust
// follow_links(true)) while guarding against cycles.
func (w walker) walk() ([]entry, error) {
	gitContext := w.respectGitignore && hasGitContext(w.root)

	var out []entry
	visited := map[string]bool{}
	// stack holds the active gitignore files from root down to the current dir.
	var stack []gitignore
	if gitContext {
		// Seed with the root .gitignore if present.
		if gi, ok := loadGitignore(filepath.Join(w.root, ".gitignore"), ""); ok {
			stack = append(stack, gi)
		}
	}

	var n int
	var rec func(dirRel string, stack []gitignore) error
	rec = func(dirRel string, stack []gitignore) error {
		if w.cancel != nil {
			n++
			if n%1024 == 0 && w.cancel() {
				return errCancelled
			}
		}

		absDir := w.root
		if dirRel != "" {
			absDir = filepath.Join(w.root, filepath.FromSlash(dirRel))
		}
		realDir, err := filepath.EvalSymlinks(absDir)
		if err != nil {
			realDir = absDir
		}
		if visited[realDir] {
			return nil
		}
		visited[realDir] = true

		names, err := readDirNames(absDir)
		if err != nil {
			// Unreadable directory: skip it rather than failing the whole walk.
			return nil
		}

		for _, name := range names {
			if name == ".git" {
				continue
			}
			childRel := joinRel(dirRel, name)
			abs := filepath.Join(absDir, name)
			isDir, err := isDirEntry(abs)
			if err != nil {
				continue
			}

			if w.excluded(childRel, isDir) {
				continue
			}
			if gitContext && ignored(stack, childRel, isDir) {
				continue
			}

			out = append(out, entry{rel: childRel, isDir: isDir})

			if isDir {
				childStack := stack
				if gitContext {
					if gi, ok := loadGitignore(filepath.Join(abs, ".gitignore"), childRel); ok {
						childStack = append(append([]gitignore(nil), stack...), gi)
					}
				}
				if err := rec(childRel, childStack); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := rec("", stack); err != nil {
		if err == errCancelled {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

// excluded reports whether relPath is dropped by the exclude override patterns.
// The override semantics mirror the Rust OverrideBuilder used with "!pattern":
// a path is excluded when it matches any exclude pattern.
func (w walker) excluded(relPath string, isDir bool) bool {
	for _, p := range w.excludes {
		if p.dirOnly && !isDir {
			continue
		}
		if patternMatches(p, relPath) {
			return true
		}
	}
	return false
}

// ignored evaluates the gitignore stack against relPath. The most specific
// (deepest) matching file's last matching pattern wins; a negation re-includes.
// When no pattern in any file matches, the entry is kept.
func ignored(stack []gitignore, relPath string, isDir bool) bool {
	result := false
	for _, gi := range stack {
		if negate, matched := gi.matches(relPath, isDir); matched {
			result = !negate
		}
	}
	return result
}

// hasGitContext reports whether dir is inside a git repository, i.e. a .git
// entry exists at dir or any ancestor. This mirrors the Rust require_git(true)
// behavior that scopes gitignore processing to git repositories.
func hasGitContext(dir string) bool {
	cur := dir
	for {
		if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

// readDirNames returns the sorted child names of dir for deterministic walks.
func readDirNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// isDirEntry reports whether abs is a directory, following symlinks so that
// symlinked directories are descended into (matching follow_links(true)).
func isDirEntry(abs string) (bool, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
