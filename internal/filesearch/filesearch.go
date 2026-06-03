// Package filesearch provides fuzzy file search over a directory tree.
//
// It is a Go port of codex's file-search crate. Given a search pattern and one
// or more root directories, it walks the tree (honoring .gitignore files when
// inside a git repository), fuzzy-matches discovered paths against the pattern,
// ranks the matches, and returns the top results.
//
// Deviations from the Rust crate:
//
//   - The Rust crate uses the `ignore` crate for directory walking and gitignore
//     handling, and the `nucleo` crate for fuzzy matching. Neither has a Go
//     equivalent in this project's allowed dependencies, so both are hand-rolled.
//   - Gitignore matching is a hand-rolled, reasonable subset (see gitignore.go).
//     It supports the common cases: comments, negation (!), anchored patterns
//     (leading or embedded slash), directory-only patterns (trailing slash),
//     the `*`, `?` and `**` wildcards, and character classes. It is not a
//     bit-for-bit reimplementation of git's algorithm.
//   - Scoring uses internal/utils/fuzzymatch instead of nucleo. fuzzymatch
//     reports a "lower is better" score; this package inverts it so that the
//     exported FileMatch.Score follows the Rust convention of "higher is
//     better". Absolute score values therefore differ from nucleo, but the
//     resulting ranking (best matches first) is preserved.
//   - The Rust crate exposes an asynchronous streaming session API
//     (create_session / SessionReporter). This port provides the synchronous
//     Run entry point, which is the behavior callers actually consume; the
//     streaming session is omitted as a deliberate simplification.
package filesearch

import "path/filepath"

// MatchType distinguishes file matches from directory matches.
type MatchType int

const (
	// MatchTypeFile indicates the matched entry is a regular file.
	MatchTypeFile MatchType = iota
	// MatchTypeDirectory indicates the matched entry is a directory.
	MatchTypeDirectory
)

// String returns the lowercase name of the match type, matching the Rust
// crate's serialized representation ("file" / "directory").
func (m MatchType) String() string {
	switch m {
	case MatchTypeDirectory:
		return "directory"
	default:
		return "file"
	}
}

// FileMatch is a single ranked search result.
//
// Score follows the Rust/nucleo convention: higher is better. Path is relative
// to Root. Indices, when present (only when FileSearchOptions.ComputeIndices is
// set), holds the rune positions in Path that matched the query, sorted
// ascending and de-duplicated, suitable for highlighting.
type FileMatch struct {
	Score     uint32
	Path      string
	MatchType MatchType
	Root      string
	// Indices is non-nil only when indices were requested. An empty (but
	// non-nil) slice means indices were requested but the needle was empty.
	Indices []int
}

// FullPath returns Root joined with the relative Path.
func (f FileMatch) FullPath() string {
	return filepath.Join(f.Root, f.Path)
}

// FileSearchResults is the result of a Run: the ranked, limited matches plus the
// total number of items that matched before the limit was applied.
type FileSearchResults struct {
	Matches         []FileMatch
	TotalMatchCount int
}

// FileNameFromPath returns the final path component of path, falling back to the
// full path when there is no separator. It mirrors the Rust file_name_from_path.
func FileNameFromPath(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return path
	}
	return base
}
