package memories

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// search searches memory files for substring matches across the requested scope,
// mirroring local::search.
func (b *LocalBackend) search(_ context.Context, req SearchRequest) (SearchResponse, error) {
	queries := make([]string, 0, len(req.Queries))
	for _, q := range req.Queries {
		queries = append(queries, strings.TrimSpace(q))
	}
	if len(queries) == 0 || containsEmpty(queries) {
		return SearchResponse{}, errEmptyQuery()
	}
	if req.MatchMode.Kind == MatchAllWithinLines && req.MatchMode.LineCount == 0 {
		return SearchResponse{}, errInvalidMatchWindow()
	}

	maxResults := req.MaxResults
	if maxResults > MaxSearchResults {
		maxResults = MaxSearchResults
	}

	start, err := b.resolveScopedPath(req.Path)
	if err != nil {
		return SearchResponse{}, err
	}
	startIndex, err := parseCursor(req.Cursor)
	if err != nil {
		return SearchResponse{}, err
	}

	info, ok, err := metadataOrNone(start)
	if err != nil {
		return SearchResponse{}, err
	}
	if !ok {
		return SearchResponse{}, errNotFound(derefOr(req.Path, ""))
	}
	if symErr := rejectSymlink(displayRelativePath(b.root, start), info); symErr != nil {
		return SearchResponse{}, symErr
	}

	matcher, err := newSearchMatcher(queries, req.MatchMode, req.CaseSensitive, req.Normalized)
	if err != nil {
		return SearchResponse{}, err
	}

	var matches []MemorySearchMatch
	if searchErr := b.searchEntries(start, info, matcher, req.ContextLines, &matches); searchErr != nil {
		return SearchResponse{}, searchErr
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].MatchLineNumber < matches[j].MatchLineNumber
	})

	if startIndex > len(matches) {
		return SearchResponse{}, errInvalidCursor(strconv.Itoa(startIndex), "exceeds result count")
	}
	endIndex := saturatingAdd(startIndex, maxResults)
	if endIndex > len(matches) {
		endIndex = len(matches)
	}
	var nextCursor *string
	if endIndex < len(matches) {
		cursor := strconv.Itoa(endIndex)
		nextCursor = &cursor
	}
	page := append([]MemorySearchMatch(nil), matches[startIndex:endIndex]...)
	return SearchResponse{
		Queries:    queries,
		MatchMode:  req.MatchMode,
		Path:       req.Path,
		Matches:    page,
		NextCursor: nextCursor,
		Truncated:  nextCursor != nil,
	}, nil
}

// searchEntries walks the scope, dispatching files to searchFile, mirroring
// search_entries (an iterative directory walk, hidden/symlinked entries skipped).
func (b *LocalBackend) searchEntries(
	current string,
	currentInfo os.FileInfo,
	matcher *searchMatcher,
	contextLines int,
	matches *[]MemorySearchMatch,
) error {
	if currentInfo.Mode().IsRegular() {
		return b.searchFile(current, matcher, contextLines, matches)
	}
	if !currentInfo.IsDir() {
		return nil
	}

	pending := []string{current}
	for len(pending) > 0 {
		dirPath := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		paths, err := readSortedDirPaths(dirPath)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if isHiddenPath(path) {
				continue
			}
			info, ok, metaErr := metadataOrNone(path)
			if metaErr != nil {
				return metaErr
			}
			if !ok {
				continue
			}
			if info.Mode()&modeSymlink != 0 {
				continue
			}
			if info.IsDir() {
				pending = append(pending, path)
			} else if info.Mode().IsRegular() {
				if fileErr := b.searchFile(path, matcher, contextLines, matches); fileErr != nil {
					return fileErr
				}
			}
		}
	}
	return nil
}

// searchFile applies the matcher to a single file, mirroring search_file.
// Non-UTF-8 files are skipped silently, mirroring the InvalidData early return.
func (b *LocalBackend) searchFile(
	path string,
	matcher *searchMatcher,
	contextLines int,
	matches *[]MemorySearchMatch,
) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errIO(err)
	}
	if !utf8.Valid(data) {
		return nil
	}
	content := string(data)
	lines := splitLines(content)
	lineMatches := make([][]bool, len(lines))
	for i, line := range lines {
		lineMatches[i] = matcher.matchedQueryFlags(line)
	}

	switch matcher.matchMode.Kind {
	case MatchAny:
		for idx, flags := range lineMatches {
			if anyTrue(flags) {
				*matches = append(*matches, buildSearchMatch(b.root, path, lines, idx, idx, contextLines, matcher.matchedQueries(flags)))
			}
		}
	case MatchAllOnSameLine:
		for idx, flags := range lineMatches {
			if allTrue(flags) {
				*matches = append(*matches, buildSearchMatch(b.root, path, lines, idx, idx, contextLines, matcher.matchedQueries(flags)))
			}
		}
	case MatchAllWithinLines:
		searchAllWithinLines(b.root, path, lines, lineMatches, matcher, contextLines, matches)
	}
	return nil
}

// searchAllWithinLines implements the windowed all-queries match mode, mirroring
// the AllWithinLines branch of search_file.
func searchAllWithinLines(
	root, path string,
	lines []string,
	lineMatches [][]bool,
	matcher *searchMatcher,
	contextLines int,
	matches *[]MemorySearchMatch,
) {
	lineCount := matcher.matchMode.LineCount
	type window struct {
		start, end int
		flags      []bool
	}
	var windows []window
	for startIndex := 0; startIndex < len(lines); startIndex++ {
		if !anyTrue(lineMatches[startIndex]) {
			continue
		}
		lastAllowed := startIndex + saturatingSub(lineCount, 1)
		if lastAllowed > len(lines)-1 {
			lastAllowed = len(lines) - 1
		}
		accumulated := make([]bool, len(matcher.queries))
		for endIndex := startIndex; endIndex <= lastAllowed; endIndex++ {
			for i, matched := range lineMatches[endIndex] {
				accumulated[i] = accumulated[i] || matched
			}
			if allTrue(accumulated) {
				windows = append(windows, window{start: startIndex, end: endIndex, flags: append([]bool(nil), accumulated...)})
				break
			}
		}
	}
	for idx, w := range windows {
		stricterContains := false
		for otherIdx, other := range windows {
			if idx == otherIdx {
				continue
			}
			if w.start <= other.start && w.end >= other.end && (w.start != other.start || w.end != other.end) {
				stricterContains = true
				break
			}
		}
		if stricterContains {
			continue
		}
		*matches = append(*matches, buildSearchMatch(root, path, lines, w.start, w.end, contextLines, matcher.matchedQueries(w.flags)))
	}
}

// buildSearchMatch assembles one MemorySearchMatch with context lines, mirroring
// build_search_match.
func buildSearchMatch(
	root, path string,
	lines []string,
	matchStartIndex, matchEndIndex, contextLines int,
	matchedQueries []string,
) MemorySearchMatch {
	contentStartIndex := saturatingSub(matchStartIndex, contextLines)
	contentEndIndex := saturatingAdd(saturatingAdd(matchEndIndex, contextLines), 1)
	if contentEndIndex > len(lines) {
		contentEndIndex = len(lines)
	}
	return MemorySearchMatch{
		Path:                   displayRelativePath(root, path),
		MatchLineNumber:        matchStartIndex + 1,
		ContentStartLineNumber: contentStartIndex + 1,
		Content:                strings.Join(lines[contentStartIndex:contentEndIndex], "\n"),
		MatchedQueries:         matchedQueries,
	}
}

// searchMatcher holds prepared queries and comparison settings, mirroring
// SearchMatcher.
type searchMatcher struct {
	queries         []string
	preparedQueries []string
	comparison      searchComparison
	matchMode       SearchMatchMode
}

func newSearchMatcher(queries []string, matchMode SearchMatchMode, caseSensitive, normalized bool) (*searchMatcher, error) {
	comparison := searchComparison{caseSensitive: caseSensitive, normalized: normalized}
	prepared := make([]string, len(queries))
	for i, q := range queries {
		prepared[i] = comparison.prepare(q)
	}
	if containsEmpty(prepared) {
		return nil, errEmptyQuery()
	}
	return &searchMatcher{
		queries:         queries,
		preparedQueries: prepared,
		comparison:      comparison,
		matchMode:       matchMode,
	}, nil
}

func (m *searchMatcher) matchedQueryFlags(line string) []bool {
	prepared := m.comparison.prepare(line)
	flags := make([]bool, len(m.preparedQueries))
	for i, q := range m.preparedQueries {
		flags[i] = strings.Contains(prepared, q)
	}
	return flags
}

func (m *searchMatcher) matchedQueries(flags []bool) []string {
	var out []string
	for i, q := range m.queries {
		if i < len(flags) && flags[i] {
			out = append(out, q)
		}
	}
	return out
}

// searchComparison normalizes case and separators, mirroring SearchComparison.
type searchComparison struct {
	caseSensitive bool
	normalized    bool
}

func (c searchComparison) prepare(value string) string {
	if c.caseSensitive && !c.normalized {
		return value
	}
	if !c.caseSensitive {
		value = strings.ToLower(value)
	}
	if !c.normalized {
		return value
	}
	var b strings.Builder
	for _, ch := range value {
		if unicode.IsLetter(ch) || unicode.IsNumber(ch) {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// splitLines splits content into lines the way Rust's str::lines does: it splits
// on '\n', strips a trailing '\r', and yields no final empty line for a trailing
// newline.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	parts := strings.Split(content, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}

func containsEmpty(values []string) bool {
	for _, v := range values {
		if v == "" {
			return true
		}
	}
	return false
}

func anyTrue(flags []bool) bool {
	for _, f := range flags {
		if f {
			return true
		}
	}
	return false
}

func allTrue(flags []bool) bool {
	for _, f := range flags {
		if !f {
			return false
		}
	}
	return true
}

func saturatingSub(a, b int) int {
	if b > a {
		return 0
	}
	return a - b
}
