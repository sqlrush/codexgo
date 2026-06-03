package filesearch

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/fuzzymatch"
)

// errCancelled is the internal sentinel signaling a cancelled walk.
var errCancelled = errors.New("filesearch: cancelled")

// CancelFunc reports whether an in-progress search should abort. It is polled
// periodically during the directory walk. It corresponds to the Rust
// cancel_flag (Arc<AtomicBool>).
type CancelFunc func() bool

// Run performs a fuzzy file search for pattern across roots and returns the
// ranked, limited matches plus the total number of matched items.
//
// roots must be non-empty. When a path lives under more than one root, it is
// attributed to the deepest (most specific) root, matching the Rust
// get_file_path logic. cancel, when non-nil, is polled during the walk; if it
// reports true the search returns empty results without error, mirroring the
// Rust cancel behavior.
//
// Run never mutates its arguments.
func Run(pattern string, roots []string, options FileSearchOptions, cancel CancelFunc) (FileSearchResults, error) {
	if len(roots) == 0 {
		return FileSearchResults{}, fmt.Errorf("filesearch: at least one search directory is required")
	}
	opts := options.normalized()

	excludes, err := compileExcludes(opts.Exclude)
	if err != nil {
		return FileSearchResults{}, fmt.Errorf("filesearch: invalid exclude pattern: %w", err)
	}

	cancelPoll := func() bool { return cancel != nil && cancel() }
	if cancelPoll() {
		return FileSearchResults{}, nil
	}

	cleanRoots := make([]string, len(roots))
	for i, r := range roots {
		cleanRoots[i] = filepath.Clean(r)
	}

	// Collect candidate (relativePath, rootIndex, isDir) tuples across roots,
	// de-duplicating paths attributed to the most specific root.
	candidates, err := collectCandidates(cleanRoots, opts, excludes, cancelPoll)
	if err != nil {
		return FileSearchResults{}, err
	}
	if cancelPoll() {
		return FileSearchResults{}, nil
	}

	matches := scoreAndRank(pattern, candidates, cleanRoots, opts.ComputeIndices)
	total := len(matches)

	limit := opts.Limit
	if limit > total {
		limit = total
	}
	return FileSearchResults{Matches: matches[:limit], TotalMatchCount: total}, nil
}

// candidate is a discovered path attributed to a specific root.
type candidate struct {
	rel       string
	rootIndex int
	isDir     bool
}

// collectCandidates walks every root and returns the merged candidate set. A
// path reachable from multiple roots is attributed to the deepest root (the one
// with the most path components), matching the Rust get_file_path tie-breaking.
func collectCandidates(roots []string, opts FileSearchOptions, excludes []gitignorePattern, cancel func() bool) ([]candidate, error) {
	// Map absolute clean path -> chosen candidate, keeping the deepest root.
	chosen := map[string]candidate{}
	rootDepth := make([]int, len(roots))
	for i, r := range roots {
		rootDepth[i] = componentCount(r)
	}

	for i, root := range roots {
		w := newWalker(root, opts.RespectGitignore, excludes, cancel)
		entries, err := w.walk()
		if err != nil {
			return nil, fmt.Errorf("filesearch: walking %q: %w", root, err)
		}
		for _, e := range entries {
			abs := filepath.Join(root, filepath.FromSlash(e.rel))
			if prev, ok := chosen[abs]; ok {
				// Keep the attribution to the deeper root.
				if rootDepth[prev.rootIndex] >= rootDepth[i] {
					continue
				}
			}
			chosen[abs] = candidate{rel: e.rel, rootIndex: i, isDir: e.isDir}
		}
	}

	out := make([]candidate, 0, len(chosen))
	for _, c := range chosen {
		out = append(out, c)
	}
	return out, nil
}

// scoreAndRank fuzzy-matches every candidate against pattern, builds FileMatch
// values for the matches, and sorts them by descending score then ascending
// path (matching cmp_by_score_desc_then_path_asc).
func scoreAndRank(pattern string, candidates []candidate, roots []string, computeIndices bool) []FileMatch {
	matches := make([]FileMatch, 0, len(candidates))
	for _, c := range candidates {
		// Match against the relative path using the platform-native separator so
		// indices line up with FileMatch.Path as the Rust crate reports it.
		haystack := filepath.FromSlash(c.rel)
		m, ok := fuzzymatch.FuzzyMatch(haystack, pattern)
		if !ok {
			continue
		}
		fm := FileMatch{
			Score:     invertScore(m.Score),
			Path:      haystack,
			MatchType: matchTypeFor(c.isDir),
			Root:      roots[c.rootIndex],
		}
		if computeIndices {
			fm.Indices = m.Indices
			if fm.Indices == nil {
				fm.Indices = []int{}
			}
		}
		matches = append(matches, fm)
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Path < matches[j].Path
	})
	return matches
}

// invertScore converts the fuzzymatch "lower is better" score into the Rust
// "higher is better" uint32 convention. fuzzymatch scores range from a strong
// negative prefix-bonus value up through positive window sizes (and MaxInt32 for
// an empty needle). We clamp into uint32 range and invert, preserving order.
func invertScore(lower int) uint32 {
	// Map so that the best (smallest) fuzzymatch score yields the largest
	// uint32. Offset by the prefix bonus so negative scores remain orderable.
	const base = math.MaxInt32
	inv := base - lower
	if inv < 0 {
		return 0
	}
	if inv > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(inv)
}

func matchTypeFor(isDir bool) MatchType {
	if isDir {
		return MatchTypeDirectory
	}
	return MatchTypeFile
}

// componentCount counts the path components in p (cleaned, native separators).
func componentCount(p string) int {
	p = filepath.Clean(p)
	if p == string(filepath.Separator) || p == "." {
		return 0
	}
	trimmed := strings.Trim(p, string(filepath.Separator))
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, string(filepath.Separator)) + 1
}

// compileExcludes turns user-supplied exclude globs into gitignore patterns.
func compileExcludes(globs []string) ([]gitignorePattern, error) {
	if len(globs) == 0 {
		return nil, nil
	}
	out := make([]gitignorePattern, 0, len(globs))
	for _, g := range globs {
		p, ok := compilePattern(g)
		if !ok {
			return nil, fmt.Errorf("empty or comment-only exclude %q", g)
		}
		out = append(out, p)
	}
	return out, nil
}
