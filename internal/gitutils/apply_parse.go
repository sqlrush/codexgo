package gitutils

import (
	"regexp"
	"sort"
	"strings"
)

// regexCI compiles a case-insensitive regular expression, panicking on an
// invalid pattern (these are all compile-time constants).
//
// Mirrors the Rust `regex_ci`.
func regexCI(pat string) *regexp.Regexp {
	return regexp.MustCompile("(?i)" + pat)
}

// Compiled patterns mirroring the Rust `parse_git_apply_output` statics.
var (
	reAppliedClean        = regexCI(`^Applied patch(?: to)?\s+(?P<path>.+?)\s+cleanly\.?$`)
	reAppliedConflicts    = regexCI(`^Applied patch(?: to)?\s+(?P<path>.+?)\s+with conflicts\.?$`)
	reApplyingWithRejects = regexCI(`^Applying patch\s+(?P<path>.+?)\s+with\s+\d+\s+rejects?\.{0,3}$`)
	reCheckingPatch       = regexCI(`^Checking patch\s+(?P<path>.+?)\.\.\.$`)
	reUnmergedLine        = regexCI(`^U\s+(?P<path>.+)$`)
	rePatchFailed         = regexCI(`^error:\s+patch failed:\s+(?P<path>.+?)(?::\d+)?(?:\s|$)`)
	reDoesNotApply        = regexCI(`^error:\s+(?P<path>.+?):\s+patch does not apply$`)
	reThreeWayStart       = regexCI(`^(?:Performing three-way merge|Falling back to three-way merge)\.\.\.$`)
	reThreeWayFailed      = regexCI(`^Failed to perform three-way merge\.\.\.$`)
	reFallbackDirect      = regexCI(`^Falling back to direct application\.\.\.$`)
	reLacksBlob           = regexCI(`^(?:error: )?repository lacks the necessary blob to (?:perform|fall back on) 3-?way merge\.?$`)
	reIndexMismatch       = regexCI(`^error:\s+(?P<path>.+?):\s+does not match index\b`)
	reNotInIndex          = regexCI(`^error:\s+(?P<path>.+?):\s+does not exist in index\b`)
	reAlreadyExistsWT     = regexCI(`^error:\s+(?P<path>.+?)\s+already exists in (?:the )?working directory\b`)
	reFileExists          = regexCI(`^error:\s+patch failed:\s+(?P<path>.+?)\s+File exists`)
	reRenamedDeleted      = regexCI(`^error:\s+path\s+(?P<path>.+?)\s+has been renamed/deleted`)
	reCannotApplyBinary   = regexCI(`^error:\s+cannot apply binary patch to\s+['"]?(?P<path>.+?)['"]?\s+without full index line$`)
	reBinaryDoesNotApply  = regexCI(`^error:\s+binary patch does not apply to\s+['"]?(?P<path>.+?)['"]?$`)
	reBinaryIncorrect     = regexCI(`^error:\s+binary patch to\s+['"]?(?P<path>.+?)['"]?\s+creates incorrect result\b`)
	reCannotReadCurrent   = regexCI(`^error:\s+cannot read the current contents of\s+['"]?(?P<path>.+?)['"]?$`)
	reSkippedPatch        = regexCI(`^Skipped patch\s+['"]?(?P<path>.+?)['"]\.$`)
	reCannotMergeBinary   = regexCI(`^warning:\s*Cannot merge binary files:\s+(?P<path>.+?)\s+\(ours\s+vs\.\s+theirs\)`)
)

// pathSet is an ordered set keyed by path, kept sorted for deterministic output.
type pathSet map[string]struct{}

func (s pathSet) add(raw string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}
	r := []rune(trimmed)
	first := r[0]
	last := r[len(r)-1]
	var unquoted string
	if (first == '"' || first == '\'') && last == first && len(r) >= 2 {
		unquoted = unescapeCString(string(r[1 : len(r)-1]))
	} else {
		unquoted = trimmed
	}
	if unquoted != "" {
		s[unquoted] = struct{}{}
	}
}

func (s pathSet) remove(p string) { delete(s, p) }

func (s pathSet) sortedSlice() []string {
	out := make([]string, 0, len(s))
	for p := range s {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ParseGitApplyOutput parses `git apply` stdout/stderr into applied, skipped,
// and conflicted path groupings, each returned sorted and de-duplicated.
//
// Mirrors the Rust `parse_git_apply_output`, including its precedence rules:
// conflicted > applied > skipped.
func ParseGitApplyOutput(stdout, stderr string) (applied, skipped, conflicted []string) {
	parts := make([]string, 0, 2)
	if stdout != "" {
		parts = append(parts, stdout)
	}
	if stderr != "" {
		parts = append(parts, stderr)
	}
	combined := strings.Join(parts, "\n")

	appliedSet := pathSet{}
	skippedSet := pathSet{}
	conflictedSet := pathSet{}
	var lastSeenPath string
	haveLastSeen := false

	for _, rawLine := range strings.Split(combined, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		// "Checking patch <path>..." tracking.
		if m := matchPath(reCheckingPatch, line); m != "" {
			lastSeenPath, haveLastSeen = m, true
			continue
		}

		// Status lines.
		if m := matchPath(reAppliedClean, line); m != "" {
			appliedSet.add(m)
			p := lastAdded(appliedSet, m)
			conflictedSet.remove(p)
			skippedSet.remove(p)
			lastSeenPath, haveLastSeen = p, true
			continue
		}
		if m := matchPath(reAppliedConflicts, line); m != "" {
			conflictedSet.add(m)
			p := lastAdded(conflictedSet, m)
			appliedSet.remove(p)
			skippedSet.remove(p)
			lastSeenPath, haveLastSeen = p, true
			continue
		}
		if m := matchPath(reApplyingWithRejects, line); m != "" {
			conflictedSet.add(m)
			p := lastAdded(conflictedSet, m)
			appliedSet.remove(p)
			skippedSet.remove(p)
			lastSeenPath, haveLastSeen = p, true
			continue
		}

		// "U <path>" after conflicts.
		if m := matchPath(reUnmergedLine, line); m != "" {
			conflictedSet.add(m)
			p := lastAdded(conflictedSet, m)
			appliedSet.remove(p)
			skippedSet.remove(p)
			lastSeenPath, haveLastSeen = p, true
			continue
		}

		// Early hints: patch failed / does not apply.
		if rePatchFailed.MatchString(line) || reDoesNotApply.MatchString(line) {
			m := matchPath(rePatchFailed, line)
			if m == "" {
				m = matchPath(reDoesNotApply, line)
			}
			if m != "" {
				skippedSet.add(m)
				lastSeenPath, haveLastSeen = m, true
			}
			continue
		}

		// Ignore narration.
		if reThreeWayStart.MatchString(line) || reFallbackDirect.MatchString(line) {
			continue
		}

		// 3-way failed entirely; attribute to last_seen_path.
		if reThreeWayFailed.MatchString(line) || reLacksBlob.MatchString(line) {
			if haveLastSeen {
				skippedSet.add(lastSeenPath)
				appliedSet.remove(lastSeenPath)
				conflictedSet.remove(lastSeenPath)
			}
			continue
		}

		// Skips / I/O problems.
		if m := firstMatch(line,
			reIndexMismatch, reNotInIndex, reAlreadyExistsWT, reFileExists,
			reRenamedDeleted, reCannotApplyBinary, reBinaryDoesNotApply,
			reBinaryIncorrect, reCannotReadCurrent, reSkippedPatch,
		); m != "" {
			skippedSet.add(m)
			p := lastAdded(skippedSet, m)
			appliedSet.remove(p)
			conflictedSet.remove(p)
			lastSeenPath, haveLastSeen = p, true
			continue
		}

		// Warnings that imply conflicts.
		if m := matchPath(reCannotMergeBinary, line); m != "" {
			conflictedSet.add(m)
			p := lastAdded(conflictedSet, m)
			appliedSet.remove(p)
			skippedSet.remove(p)
			lastSeenPath, haveLastSeen = p, true
			continue
		}
	}

	// Final precedence: conflicts > applied > skipped.
	for p := range conflictedSet {
		appliedSet.remove(p)
		skippedSet.remove(p)
	}
	for p := range appliedSet {
		skippedSet.remove(p)
	}

	return appliedSet.sortedSlice(), skippedSet.sortedSlice(), conflictedSet.sortedSlice()
}

// matchPath returns the unescaped `path` capture of re against line, or "" when
// the line does not match. The returned value is the unescaped form that `add`
// would store, so callers can use it for set bookkeeping.
func matchPath(re *regexp.Regexp, line string) string {
	groups := re.FindStringSubmatch(line)
	if groups == nil {
		return ""
	}
	idx := re.SubexpIndex("path")
	if idx < 0 || idx >= len(groups) {
		// Pattern with no path capture (used only for MatchString checks).
		return ""
	}
	return groups[idx]
}

// firstMatch returns the first non-empty path capture among res.
func firstMatch(line string, res ...*regexp.Regexp) string {
	for _, re := range res {
		if m := matchPath(re, line); m != "" {
			return m
		}
	}
	return ""
}

// lastAdded returns the lexicographically greatest element currently in s.
//
// This faithfully mirrors the Rust code, which reads back
// `set.iter().next_back()` (the max element of a BTreeSet) after inserting a
// path. The `raw` argument is unused but kept to document the call site that
// just inserted a value. Returns "" for an empty set.
func lastAdded(s pathSet, _ string) string {
	max := ""
	have := false
	for p := range s {
		if !have || p > max {
			max = p
			have = true
		}
	}
	return max
}
