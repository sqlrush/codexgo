package config

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/gitutils/gitroot"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// ProjectTrustContext captures the data needed to decide whether a directory is
// a trusted project. It mirrors the Rust `ProjectTrustContext` (config/src/
// loader/mod.rs): the project-root and repo-root lookup keys plus the parsed
// `[projects]` trust map. Build one with [BuildProjectTrustContext] and query it
// with [ProjectTrustContext.DecisionForDir].
type ProjectTrustContext struct {
	projectRoot           abspath.AbsolutePathBuf
	projectRootKey        string
	projectRootLookupKeys []string
	repoRoot              *abspath.AbsolutePathBuf
	repoRootKey           string
	repoRootLookupKeys    []string
	projectsTrust         map[string]protocol.TrustLevel
}

// ProjectTrustDecision is the outcome of resolving a directory's trust. It
// mirrors the Rust `ProjectTrustDecision`. TrustLevel is nil when no `[projects]`
// entry matched (an implicit "not trusted").
type ProjectTrustDecision struct {
	TrustLevel *protocol.TrustLevel
	TrustKey   string
}

// IsTrusted reports whether the decision resolved to an explicit "trusted"
// level, mirroring Rust's `ProjectTrustDecision::is_trusted`.
func (d ProjectTrustDecision) IsTrusted() bool {
	return d.TrustLevel != nil && *d.TrustLevel == protocol.TrustLevelTrusted
}

// IsUntrusted reports whether the decision resolved to an explicit "untrusted"
// level.
func (d ProjectTrustDecision) IsUntrusted() bool {
	return d.TrustLevel != nil && *d.TrustLevel == protocol.TrustLevelUntrusted
}

// BuildProjectTrustContext assembles a [ProjectTrustContext] for cwd from the
// merged config's `[projects]` table, mirroring the Rust
// `project_trust_context`. projectRootMarkers selects the project root (default
// [".git"] when empty per the Rust default); pass the value resolved from
// project_root_markers config. The repo root (worktree-aware) is resolved via
// [gitroot.ResolveRootGitProjectForTrust].
func BuildProjectTrustContext(merged TomlValue, cwd abspath.AbsolutePathBuf, projectRootMarkers []string) ProjectTrustContext {
	projectRoot := findProjectRootForTrust(cwd, projectRootMarkers)
	projectsTrust := projectsTrustFromMerged(merged)

	projectRootLookupKeys := normalizedProjectTrustKeys(projectRoot)
	projectRootKey := projectTrustKey(projectRoot)
	if len(projectRootLookupKeys) > 0 {
		projectRootKey = projectRootLookupKeys[0]
	}

	var repoRoot *abspath.AbsolutePathBuf
	if root, ok := gitroot.ResolveRootGitProjectForTrust(cwd); ok {
		repoRoot = &root
	}
	var repoRootLookupKeys []string
	repoRootKey := ""
	if repoRoot != nil {
		repoRootLookupKeys = normalizedProjectTrustKeys(*repoRoot)
		if len(repoRootLookupKeys) > 0 {
			repoRootKey = repoRootLookupKeys[0]
		}
	}

	return ProjectTrustContext{
		projectRoot:           projectRoot,
		projectRootKey:        projectRootKey,
		projectRootLookupKeys: projectRootLookupKeys,
		repoRoot:              repoRoot,
		repoRootKey:           repoRootKey,
		repoRootLookupKeys:    repoRootLookupKeys,
		projectsTrust:         projectsTrust,
	}
}

// DecisionForDir resolves the trust decision for dir, mirroring the Rust
// `ProjectTrustContext::decision_for_dir`. Precedence: the directory's own
// normalized keys, then the project-root keys, then the repo-root keys; the
// first matching `[projects]` entry wins. When nothing matches the decision has
// a nil TrustLevel and a TrustKey of the repo-root key (or project-root key).
func (c ProjectTrustContext) DecisionForDir(dir abspath.AbsolutePathBuf) ProjectTrustDecision {
	for _, dirKey := range normalizedProjectTrustKeys(dir) {
		if key, level, ok := c.lookupTrust(dirKey); ok {
			return ProjectTrustDecision{TrustLevel: &level, TrustKey: key}
		}
	}
	for _, projectRootKey := range c.projectRootLookupKeys {
		if key, level, ok := c.lookupTrust(projectRootKey); ok {
			return ProjectTrustDecision{TrustLevel: &level, TrustKey: key}
		}
	}
	for _, repoRootKey := range c.repoRootLookupKeys {
		if key, level, ok := c.lookupTrust(repoRootKey); ok {
			return ProjectTrustDecision{TrustLevel: &level, TrustKey: key}
		}
	}

	trustKey := c.projectRootKey
	if c.repoRootKey != "" {
		trustKey = c.repoRootKey
	}
	return ProjectTrustDecision{TrustLevel: nil, TrustKey: trustKey}
}

// DecisionForCwd resolves the trust decision for the project as a whole using
// the project root, the natural entry point when deciding whether to load
// project-layer features for a session.
func (c ProjectTrustContext) DecisionForCwd() ProjectTrustDecision {
	return c.DecisionForDir(c.projectRoot)
}

// lookupTrust resolves a single lookup key against the projects-trust map,
// mirroring Rust's `project_trust_for_lookup_key`: an exact key match wins;
// otherwise the lowest-sorting key whose platform-normalized form equals the
// lookup key is used (case-insensitive matching on Windows).
func (c ProjectTrustContext) lookupTrust(lookupKey string) (string, protocol.TrustLevel, bool) {
	if level, ok := c.projectsTrust[lookupKey]; ok {
		return lookupKey, level, true
	}
	var matches []string
	for key := range c.projectsTrust {
		if normalizeProjectTrustLookupKey(key) == lookupKey {
			matches = append(matches, key)
		}
	}
	if len(matches) == 0 {
		return "", "", false
	}
	sort.Strings(matches)
	key := matches[0]
	return key, c.projectsTrust[key], true
}

// projectsTrustFromMerged extracts the `[projects]` trust map from the merged
// config tree, keeping only entries with an explicit trust_level (mirroring the
// `filter_map` in Rust's `project_trust_context`).
func projectsTrustFromMerged(merged TomlValue) map[string]protocol.TrustLevel {
	out := make(map[string]protocol.TrustLevel)
	table, ok := merged.(map[string]any)
	if !ok {
		return out
	}
	projects, ok := table["projects"].(map[string]any)
	if !ok {
		return out
	}
	for key, raw := range projects {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		levelRaw, ok := entry["trust_level"].(string)
		if !ok {
			continue
		}
		level, err := parseTrustLevel(levelRaw)
		if err != nil {
			continue
		}
		out[key] = level
	}
	return out
}

// parseTrustLevel parses a trust_level string into a [protocol.TrustLevel],
// matching the serde-deserialized values.
func parseTrustLevel(raw string) (protocol.TrustLevel, error) {
	switch raw {
	case string(protocol.TrustLevelTrusted):
		return protocol.TrustLevelTrusted, nil
	case string(protocol.TrustLevelUntrusted):
		return protocol.TrustLevelUntrusted, nil
	default:
		return "", os.ErrInvalid
	}
}

// ProjectTrustKey canonicalizes path into the primary projects-table key used to
// look up its trust level, mirroring Rust's public `project_trust_key`. Hosts
// use it to write or query `[projects."<key>"]` entries consistently.
func ProjectTrustKey(path abspath.AbsolutePathBuf) string {
	return projectTrustKey(path)
}

// projectTrustKey canonicalizes path into the primary projects-table key,
// mirroring Rust's `project_trust_key`.
func projectTrustKey(path abspath.AbsolutePathBuf) string {
	keys := normalizedProjectTrustKeys(path)
	if len(keys) > 0 {
		return keys[0]
	}
	return normalizeProjectTrustLookupKey(path.String())
}

// normalizedProjectTrustKeys returns the lookup keys for path, mirroring Rust's
// `normalized_project_trust_keys`: the canonical (symlink-resolved) key first,
// then the logical key when it differs. Both are platform-normalized.
func normalizedProjectTrustKeys(path abspath.AbsolutePathBuf) []string {
	logical := normalizeProjectTrustLookupKey(path.String())
	canonicalPath := path
	if c, err := path.Canonicalize(); err == nil {
		canonicalPath = c
	}
	canonical := normalizeProjectTrustLookupKey(canonicalPath.String())
	if logical == canonical {
		return []string{canonical}
	}
	return []string{canonical, logical}
}

// normalizeProjectTrustLookupKey lowercases the key on Windows (so paths that
// differ only by case map to the same entry) and is identity elsewhere,
// mirroring Rust's `normalize_project_trust_lookup_key`.
func normalizeProjectTrustLookupKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(key)
	}
	return key
}

// findProjectRootForTrust walks cwd's ancestors looking for the first directory
// containing any marker, mirroring Rust's `find_project_root`. Empty markers
// disable detection (returns cwd); the default marker set is [".git"].
func findProjectRootForTrust(cwd abspath.AbsolutePathBuf, markers []string) abspath.AbsolutePathBuf {
	if len(markers) == 0 {
		return cwd
	}
	for _, ancestor := range cwd.Ancestors() {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(ancestor.String(), marker)); err == nil {
				return ancestor
			}
		}
	}
	return cwd
}

// IsProjectTrusted reports whether cwd's project is explicitly trusted under the
// merged config's `[projects]` table, using the default project-root markers.
// It is the convenience entry point hosts use to gate project-layer feature
// loading (e.g. project `.codexgo/skills`). projectRootMarkers may be nil to use
// the default [".git"] markers.
func IsProjectTrusted(merged TomlValue, cwd abspath.AbsolutePathBuf, projectRootMarkers []string) bool {
	markers := projectRootMarkers
	if markers == nil {
		markers = DefaultProjectRootMarkers()
	}
	ctx := BuildProjectTrustContext(merged, cwd, markers)
	return ctx.DecisionForCwd().IsTrusted()
}
